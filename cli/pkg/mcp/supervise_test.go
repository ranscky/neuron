package mcp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ranscky/neuron/internal/proxy"
)

// syncWriter is a goroutine-safe client stdout stand-in.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// waitFor blocks until the output contains substr, failing the test on timeout.
func (w *syncWriter) waitFor(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := w.String(); strings.Contains(got, substr) {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in client output:\n%s", substr, w.String())
	return ""
}

// fakeGeneration is an in-memory server process: handler decides what happens
// to each message, and returning false ends that process (closing stdout).
func fakeGeneration(handler func(line []byte, out io.Writer) bool) *generation {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})

	go func() {
		defer close(done)
		// Closing both ends is what a real dead process looks like: stdout goes
		// away (ending the generation) and writing to stdin fails.
		defer outW.Close()
		defer inR.Close()

		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			if !handler(scanner.Bytes(), outW) {
				break
			}
		}
	}()

	return &generation{
		in:   inW,
		out:  outR,
		wait: func() error { <-done; return nil },
	}
}

// dyingGeneration is a process that closes stdout at once and exits non-zero.
func dyingGeneration() *generation {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	_ = outW.Close()
	return &generation{
		in:  inW,
		out: outR,
		wait: func() error {
			_ = inR.Close()
			return errors.New("exit status 1")
		},
	}
}

// scriptedStarter returns the nth generation from the list, repeating the last.
func scriptedStarter(attempts *int, gens ...*generation) starter {
	var mu sync.Mutex
	return func() (*generation, error) {
		mu.Lock()
		defer mu.Unlock()
		(*attempts)++
		idx := *attempts - 1
		if idx >= len(gens) {
			idx = len(gens) - 1
		}
		return gens[idx], nil
	}
}

func TestSuperviseRestartsACrashedServerAndFailsTheAbandonedCall(t *testing.T) {
	clientInR, clientInW := io.Pipe()
	defer clientInW.Close()
	out := &syncWriter{}

	attempts := 0
	start := func() (*generation, error) {
		attempts++
		if attempts == 1 {
			// Reads the request and dies without answering it.
			return fakeGeneration(func(line []byte, w io.Writer) bool { return false }), nil
		}
		return fakeGeneration(func(line []byte, w io.Writer) bool {
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"content":[]}}`+"\n")
			return true
		}), nil
	}

	var recorded []proxy.Call
	var recMu sync.Mutex
	rec := proxy.RecorderFunc(func(c proxy.Call) {
		recMu.Lock()
		defer recMu.Unlock()
		recorded = append(recorded, c)
	})

	policy := RestartPolicy{Max: 3, Backoff: 5 * time.Millisecond, MaxDelay: 20 * time.Millisecond, ResetAfter: time.Hour}
	done := make(chan error, 1)
	go func() {
		done <- supervise("flaky", proxy.NewSession("flaky", out, rec), proxy.NewReaderSource(clientInR), io.Discard, start, policy)
	}()

	if _, err := io.WriteString(clientInW, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}`+"\n"); err != nil {
		t.Fatal(err)
	}

	// The request the dead process abandoned must come back as an error.
	got := out.waitFor(t, `"id":1`)
	if !strings.Contains(got, `-32000`) {
		t.Errorf("expected a JSON-RPC server error for the abandoned call, got:\n%s", got)
	}

	// A request sent after the restart must be served by the new process.
	if _, err := io.WriteString(clientInW, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, `"id":2`)

	var sawAbandoned bool
	for _, c := range recorded {
		if c.Status == "error" && strings.Contains(c.Error, "restarting") {
			sawAbandoned = true
		}
	}
	if !sawAbandoned {
		t.Errorf("the abandoned call should be recorded as an error, got %+v", recorded)
	}

	_ = clientInW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervise returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervise did not return after the client hung up")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

// A message that arrives while the server is being restarted must be delivered
// to the new process, not dropped.
func TestSuperviseDeliversMessagesSentDuringBackoff(t *testing.T) {
	clientInR, clientInW := io.Pipe()
	defer clientInW.Close()
	out := &syncWriter{}

	attempts := 0
	start := func() (*generation, error) {
		attempts++
		n := attempts
		if n == 1 {
			return fakeGeneration(func(line []byte, w io.Writer) bool { return false }), nil
		}
		return fakeGeneration(func(line []byte, w io.Writer) bool {
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`+"\n")
			return true
		}), nil
	}

	// A long backoff, so the second message is certainly written mid-restart.
	policy := RestartPolicy{Max: 3, Backoff: 150 * time.Millisecond, ResetAfter: time.Hour}
	done := make(chan error, 1)
	go func() {
		done <- supervise("flaky", proxy.NewSession("flaky", out, nil), proxy.NewReaderSource(clientInR), io.Discard, start, policy)
	}()

	// Kill the first generation with one request, wait until that request has
	// been failed, then queue a second one while the supervisor is backing off.
	if _, err := io.WriteString(clientInW, `{"jsonrpc":"2.0","id":6,"method":"ping"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, `"id":6`)

	if _, err := io.WriteString(clientInW, `{"jsonrpc":"2.0","id":7,"method":"ping"}`+"\n"); err != nil {
		t.Fatal(err)
	}

	// Served by the second process, not dropped and not failed.
	out.waitFor(t, `"id":7,"result"`)
	if attempts != 2 {
		t.Errorf("expected the second generation to serve the queued request, attempts=%d", attempts)
	}

	_ = clientInW.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervise did not return")
	}
}

func TestSuperviseGivesUpAfterTheRestartCap(t *testing.T) {
	clientInR, clientInW := io.Pipe()
	defer clientInW.Close()
	out := &syncWriter{}

	attempts := 0
	var stderr bytes.Buffer
	policy := RestartPolicy{Max: 2, Backoff: time.Millisecond, MaxDelay: 2 * time.Millisecond, ResetAfter: time.Hour}
	done := make(chan error, 1)
	go func() {
		done <- supervise("doomed", proxy.NewSession("doomed", out, nil), proxy.NewReaderSource(clientInR), &stderr,
			scriptedStarter(&attempts, dyingGeneration()), policy)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("giving up is not an error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervise kept restarting past the cap")
	}

	if attempts != 3 {
		t.Errorf("expected 1 start + 2 restarts, got %d", attempts)
	}
	if !strings.Contains(stderr.String(), "giving up") {
		t.Errorf("the operator should be told we gave up, stderr:\n%s", stderr.String())
	}
}

func TestSuperviseWithNoRestartPolicyStartsOnce(t *testing.T) {
	clientInR, clientInW := io.Pipe()
	defer clientInW.Close()
	out := &syncWriter{}

	attempts := 0
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- supervise("once", proxy.NewSession("once", out, nil), proxy.NewReaderSource(clientInR), &stderr,
			scriptedStarter(&attempts, dyingGeneration()), NoRestart)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervise returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervise did not return")
	}
	if attempts != 1 {
		t.Errorf("--no-restart must not restart, attempts=%d", attempts)
	}
	if stderr.Len() != 0 {
		t.Errorf("nothing should be logged when the policy is off, got %q", stderr.String())
	}
}

// A client that hangs up and a server that exits cleanly is a normal shutdown,
// not a crash: it must not trigger a restart.
func TestSuperviseDoesNotRestartWhenTheClientHungUp(t *testing.T) {
	out := &syncWriter{}
	attempts := 0

	cleanExit := func() *generation {
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		_ = outW.Close()
		return &generation{in: inW, out: outR, wait: func() error {
			_ = inR.Close()
			return nil
		}}
	}

	done := make(chan error, 1)
	go func() {
		done <- supervise("clean", proxy.NewSession("clean", out, nil), proxy.NewReaderSource(strings.NewReader("")), io.Discard,
			scriptedStarter(&attempts, cleanExit()), DefaultRestartPolicy)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervise returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervise did not return")
	}
	if attempts != 1 {
		t.Errorf("a clean exit after the client left must not restart, attempts=%d", attempts)
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	policy := RestartPolicy{Backoff: 100 * time.Millisecond, MaxDelay: 500 * time.Millisecond}

	want := map[int]time.Duration{
		1: 100 * time.Millisecond,
		2: 200 * time.Millisecond,
		3: 400 * time.Millisecond,
		4: 500 * time.Millisecond, // capped
		9: 500 * time.Millisecond,
	}
	for attempt, expected := range want {
		if got := backoffFor(policy, attempt); got != expected {
			t.Errorf("attempt %d: got %s, want %s", attempt, got, expected)
		}
	}

	// A policy without an explicit backoff still restarts, just slowly.
	if got := backoffFor(RestartPolicy{Max: 1}, 1); got != 250*time.Millisecond {
		t.Errorf("default backoff = %s", got)
	}
}

func TestServerSlotBlocksUntilAServerExists(t *testing.T) {
	slot := newServerSlot()

	done := make(chan bool, 1)
	go func() { done <- slot.Write([]byte("hello\n")) }()

	select {
	case <-done:
		t.Fatal("the write must wait for a server")
	case <-time.After(20 * time.Millisecond):
	}

	pr, pw := io.Pipe()
	slot.set(pw)

	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 6)
		_, _ = io.ReadFull(pr, buf)
		got <- string(buf)
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("the write should have gone through")
		}
	case <-time.After(time.Second):
		t.Fatal("the write did not resume after a server appeared")
	}
	if msg := <-got; msg != "hello\n" {
		t.Errorf("server received %q", msg)
	}

	// Closing the slot releases a blocked writer rather than hanging it.
	slot.close()
	if slot.Write([]byte("x\n")) {
		t.Error("writes after close must report the session is over")
	}
}
