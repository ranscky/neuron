package mcp

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ranscky/neuron/internal/proxy"
)

// RestartPolicy controls how a managed server process is supervised.
type RestartPolicy struct {
	// Max is how many consecutive restarts are attempted before the session is
	// abandoned. Zero disables supervision: the first exit ends the session.
	Max int
	// Backoff is the delay before the first restart. It doubles per attempt.
	Backoff time.Duration
	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration
	// ResetAfter is how long a server must stay up before its crash count is
	// treated as recovered.
	ResetAfter time.Duration
}

// DefaultRestartPolicy restarts a crashed server up to five times, backing off
// from 250ms to 5s. A server that stays up for 30s is considered healthy again,
// so a long session survives the occasional crash indefinitely.
var DefaultRestartPolicy = RestartPolicy{
	Max:        5,
	Backoff:    250 * time.Millisecond,
	MaxDelay:   5 * time.Second,
	ResetAfter: 30 * time.Second,
}

// NoRestart ends the session when the server exits — the behaviour before
// supervision existed, and what `neuron mcp run --no-restart` selects.
var NoRestart = RestartPolicy{}

// cleanExitGrace is how long a cleanly-exited server has to prove its client is
// already gone before it is restarted. Without it, a client that closes stdin at
// the same moment the server exits would trigger one pointless restart.
const cleanExitGrace = 250 * time.Millisecond

// generation is one running server process.
type generation struct {
	in   io.WriteCloser
	out  io.Reader
	wait func() error
}

// starter launches one generation of the server.
type starter func() (*generation, error)

// serverSlot is where client messages are delivered. Between generations it has
// no server, so writes wait rather than being dropped: a message that arrives
// during a restart is delivered to the next process instead of lost.
type serverSlot struct {
	mu     sync.Mutex
	cond   *sync.Cond
	in     io.WriteCloser
	closed bool
}

func newServerSlot() *serverSlot {
	s := &serverSlot{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *serverSlot) set(in io.WriteCloser) {
	s.mu.Lock()
	s.in = in
	s.mu.Unlock()
	s.cond.Broadcast()
}

// drop detaches a generation that has ended, leaving writers waiting.
func (s *serverSlot) drop(in io.WriteCloser) {
	s.mu.Lock()
	if s.in == in {
		s.in = nil
	}
	s.mu.Unlock()
	s.cond.Broadcast()
}

// close ends the session: blocked writers give up, and the live server is told
// its client is gone so it can exit instead of lingering.
func (s *serverSlot) close() {
	s.mu.Lock()
	s.closed = true
	in := s.in
	s.in = nil
	s.mu.Unlock()
	s.cond.Broadcast()
	if in != nil {
		_ = in.Close()
	}
}

// Write implements proxy.ServerTarget.
func (s *serverSlot) Write(line []byte) bool {
	for {
		s.mu.Lock()
		for s.in == nil && !s.closed {
			s.cond.Wait()
		}
		if s.closed {
			s.mu.Unlock()
			return false
		}
		in := s.in
		s.mu.Unlock()

		if _, err := in.Write(line); err == nil {
			return true
		}
		// The process died between choosing it and writing this message. Drop
		// that generation and wait for the next one, so nothing is lost.
		s.drop(in)
	}
}

// supervise serves one MCP session, restarting the server when it exits while
// the client is still connected.
//
// Nothing is injected into the traffic: supervision is passive, watching the
// process rather than polling it, which keeps the proxy byte-transparent.
func supervise(
	server string,
	sess *proxy.Session,
	src proxy.LineSource,
	stderr io.Writer,
	start starter,
	policy RestartPolicy,
) error {
	slot := newServerSlot()
	defer slot.close()

	requestsDone := make(chan struct{})
	go func() {
		sess.ServeRequests(src, slot)
		slot.close()
		close(requestsDone)
	}()

	attempts := 0
	for {
		gen, err := start()
		if err != nil {
			return err
		}
		started := time.Now()
		slot.set(gen.in)

		serveErr := sess.ServeResponses(gen.out)

		slot.drop(gen.in)
		_ = gen.in.Close()
		waitErr := gen.wait()

		// A write to the client failed: the client is gone, so the session is
		// over whatever the server does next.
		if serveErr != nil {
			return nil
		}
		if clientGone(requestsDone, waitErr) {
			return nil
		}

		if policy.Max == 0 {
			sess.FailPending("the MCP server exited and --no-restart is set")
			return nil
		}

		// A server that ran for a while and then crashed has not proven itself
		// unstable; only rapid repeated crashes exhaust the policy.
		if time.Since(started) >= policy.ResetAfter {
			attempts = 0
		}
		attempts++
		if attempts > policy.Max {
			logf(stderr, "%s exited (%s) and could not be restarted after %d attempts; giving up\n",
				server, describeExit(waitErr), policy.Max)
			sess.FailPending(fmt.Sprintf(
				"the MCP server exited and was not restarted after %d attempts", policy.Max))
			return nil
		}

		delay := backoffFor(policy, attempts)
		logf(stderr, "%s exited (%s); restarting in %s (attempt %d/%d)\n",
			server, describeExit(waitErr), delay, attempts, policy.Max)

		// Answer whatever the dead process never answered before the delay, so
		// the client sees an immediate error instead of waiting on a restart.
		sess.FailPending(fmt.Sprintf(
			"the MCP server exited unexpectedly; restarting (attempt %d/%d)", attempts, policy.Max))

		if !waitOrClientGone(delay, requestsDone) {
			return nil
		}
	}
}

// clientGone reports whether the client has finished sending. A cleanly exited
// server gets a short grace period, because the client may simply have hung up
// at the same moment and the request pump may not have noticed yet.
func clientGone(requestsDone <-chan struct{}, waitErr error) bool {
	select {
	case <-requestsDone:
		return true
	default:
	}
	if waitErr != nil {
		// A crash while the client is still connected is a restart, not an exit.
		return false
	}
	select {
	case <-requestsDone:
		return true
	case <-time.After(cleanExitGrace):
		return false
	}
}

// waitOrClientGone sleeps for delay, reporting false if the client hung up.
func waitOrClientGone(delay time.Duration, requestsDone <-chan struct{}) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-requestsDone:
		return false
	case <-timer.C:
		return true
	}
}

// backoffFor returns the delay before the given restart attempt (1-based).
func backoffFor(policy RestartPolicy, attempt int) time.Duration {
	delay := policy.Backoff
	if delay <= 0 {
		delay = 250 * time.Millisecond
	}
	for i := 1; i < attempt; i++ {
		delay *= 2
		if policy.MaxDelay > 0 && delay >= policy.MaxDelay {
			return policy.MaxDelay
		}
	}
	if policy.MaxDelay > 0 && delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

// describeExit renders a process exit for the operator's stderr.
func describeExit(err error) string {
	if err == nil {
		return "exit status 0"
	}
	return err.Error()
}

func logf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format, args...)
}
