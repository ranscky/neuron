// Package proxy implements a transparent MCP JSON-RPC proxy over stdio.
//
// It sits between an MCP client and an MCP server, forwarding every message
// unmodified while recording each request/response pair for observability.
package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// maxMessageBytes bounds a single JSON-RPC line. tools/call results with
	// large payloads are common, so this is deliberately generous.
	maxMessageBytes     = 16 << 20
	maxStoredArgBytes   = 4 << 10
	maxStoredResultSize = 4 << 10
	maxStoredErrorBytes = 1 << 10
)

// Call is one recorded JSON-RPC request/response pair.
type Call struct {
	Time       time.Time `json:"time"`
	Server     string    `json:"server"`
	Method     string    `json:"method"`
	Tool       string    `json:"tool,omitempty"`
	Args       string    `json:"args,omitempty"`
	Result     string    `json:"result,omitempty"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	TokensIn   int       `json:"tokens_in"`
	TokensOut  int       `json:"tokens_out"`
}

// Recorder receives completed calls. Implementations must be safe for
// concurrent use. A nil Recorder disables recording entirely.
type Recorder interface {
	Record(Call)
}

// RecorderFunc adapts a plain function to Recorder.
type RecorderFunc func(Call)

// Record implements Recorder.
func (f RecorderFunc) Record(c Call) { f(c) }

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type pendingCall struct {
	method   string
	tool     string
	args     string
	start    time.Time
	tokensIn int
}

// LineSource yields newline-terminated messages from an MCP client.
type LineSource interface {
	// Next returns the next message, or ok=false once the client is done.
	Next() (line []byte, ok bool)
}

// ReaderSource adapts an io.Reader to a LineSource. Messages are copied out, so
// the source can outlive the buffer a message was read into.
type ReaderSource struct{ scanner *bufio.Scanner }

// NewReaderSource wraps a reader. A nil reader yields no messages.
func NewReaderSource(r io.Reader) *ReaderSource {
	if r == nil {
		r = strings.NewReader("")
	}
	return &ReaderSource{scanner: newScanner(r)}
}

// Next implements LineSource.
func (r *ReaderSource) Next() ([]byte, bool) {
	if !r.scanner.Scan() {
		return nil, false
	}
	line := r.scanner.Bytes()
	out := make([]byte, len(line))
	copy(out, line)
	return out, true
}

// ServerTarget is where client messages go. An implementation may swap the
// underlying server (after a restart) or block until one is available again.
type ServerTarget interface {
	// Write delivers one message, reporting false once the session is over.
	Write(line []byte) bool
}

// WriteCloserTarget sends messages straight to one server process.
type WriteCloserTarget struct{ W io.WriteCloser }

// Write implements ServerTarget.
func (t WriteCloserTarget) Write(line []byte) bool {
	_, err := t.W.Write(line)
	return err == nil
}

// Session proxies one client's MCP session, possibly across several server
// processes. It owns the pending-request bookkeeping, so a request sent to a
// process that has since died can be answered rather than left hanging.
type Session struct {
	server  string
	rec     Recorder
	out     io.Writer
	writeMu sync.Mutex
	guard   Guard

	mu      sync.Mutex
	pending map[string]*pendingCall
}

// JSON-RPC error codes Neuron uses when it is the one refusing a call.
const (
	// CodeServerUnavailable means the server process is gone.
	CodeServerUnavailable = -32000
	// CodePolicyDenied means a policy refused the tool.
	CodePolicyDenied = -32001
	// CodePolicyRateLimited means a policy's rate limit is spent.
	CodePolicyRateLimited = -32002
)

// BlockedError refuses a request. The proxy answers the client with Code and
// Message, and records the call as failed.
type BlockedError struct {
	Code    int
	Message string
}

// Error implements error.
func (e *BlockedError) Error() string { return e.Message }

// Guard can refuse a client request before it reaches the server.
type Guard interface {
	// Check is called for requests that carry an id. Returning nil allows the
	// request; returning a *BlockedError refuses it with that code and message.
	Check(method string, params json.RawMessage) error
}

// GuardFunc adapts a plain function to Guard.
type GuardFunc func(method string, params json.RawMessage) error

// Check implements Guard.
func (f GuardFunc) Check(method string, params json.RawMessage) error {
	return f(method, params)
}

// NewSession starts a session for one server. A nil Recorder disables recording.
func NewSession(server string, clientOut io.Writer, rec Recorder) *Session {
	return &Session{server: server, rec: rec, out: clientOut, pending: map[string]*pendingCall{}}
}

// ServeRequests forwards client messages to the target until the source ends.
// A guard, when installed, may refuse a request before it is forwarded.
func (s *Session) ServeRequests(src LineSource, target ServerTarget) {
	for {
		line, ok := src.Next()
		if !ok {
			return
		}
		payload := stripNewline(line)

		if blocked := s.refusal(payload); blocked != nil {
			s.reject(payload, blocked)
			continue
		}

		s.observeRequest(payload)
		if !target.Write(line) {
			return
		}
	}
}

// SetGuard installs a guard that can refuse client requests. A nil guard allows
// everything, which is the default.
func (s *Session) SetGuard(g Guard) { s.guard = g }

// refusal consults the guard. Notifications are never refused: they have no id,
// so there is no way to tell the client why.
func (s *Session) refusal(line []byte) *BlockedError {
	if s.guard == nil {
		return nil
	}
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return nil
	}
	if idKey(m.ID) == "" {
		return nil
	}

	err := s.guard.Check(m.Method, m.Params)
	if err == nil {
		return nil
	}
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		return blocked
	}
	return &BlockedError{Code: CodePolicyDenied, Message: err.Error()}
}

// reject answers a refused request without forwarding it, and records it so the
// dashboard shows policy blocks rather than silence.
func (s *Session) reject(line []byte, blocked *BlockedError) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	key := idKey(m.ID)
	if key == "" {
		return
	}

	pc := describeRequest(m)
	pc.start = time.Now()
	if s.rec != nil {
		s.rec.Record(Call{
			Time:     pc.start,
			Server:   s.server,
			Method:   pc.method,
			Tool:     pc.tool,
			Args:     pc.args,
			Status:   "error",
			Error:    blocked.Message,
			TokensIn: pc.tokensIn,
		})
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(key),
		"error":   map[string]any{"code": blocked.Code, "message": blocked.Message},
	})
	if err != nil {
		return
	}
	_ = s.writeClient(append(payload, '\n'))
}

// ServeResponses forwards one server process's responses to the client until
// that process closes its stdout, which is what ends a generation.
func (s *Session) ServeResponses(serverOut io.Reader) error {
	scanner := newScanner(serverOut)
	for scanner.Scan() {
		raw := scanner.Bytes()
		s.observeResponse(stripNewline(raw))
		if err := s.writeClient(raw); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// FailPending answers every in-flight request with a JSON-RPC error, so a crash
// or a restart never leaves the client waiting forever. Each abandoned call is
// recorded as an error, so the dashboard shows what the crash cost.
func (s *Session) FailPending(message string) int {
	s.mu.Lock()
	stale := s.pending
	s.pending = map[string]*pendingCall{}
	s.mu.Unlock()

	now := time.Now()
	failed := 0
	for key, pc := range stale {
		if s.rec != nil {
			s.rec.Record(Call{
				Time:       pc.start,
				Server:     s.server,
				Method:     pc.method,
				Tool:       pc.tool,
				Args:       pc.args,
				Status:     "error",
				Error:      message,
				DurationMs: now.Sub(pc.start).Milliseconds(),
				TokensIn:   pc.tokensIn,
			})
		}

		payload, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(key),
			"error":   map[string]any{"code": CodeServerUnavailable, "message": message},
		})
		if err != nil {
			continue
		}
		if err := s.writeClient(append(payload, '\n')); err != nil {
			return failed
		}
		failed++
	}
	return failed
}

func (s *Session) writeClient(line []byte) error {
	if s.out == nil {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.out.Write(line)
	return err
}

// Deliver records one server-to-client message and forwards it verbatim. Transports
// that do not read a single stream — HTTP requests, SSE events — use it to feed
// responses into the same recording and write path as a child process's stdout.
func (s *Session) Deliver(line []byte) error {
	payload := stripNewline(line)
	s.observeResponse(payload)
	return s.writeClient(append(payload, '\n'))
}

// Pump proxies newline-delimited JSON-RPC between a client and a single server
// process.
//
// clientIn/clientOut are the client's view (usually os.Stdin/os.Stdout).
// serverIn/serverOut are the server process's stdin/stdout. Bytes are forwarded
// untouched in both directions. Pump returns when the server side closes or the
// write to the client fails, which is when the MCP session is over.
//
// Supervision across restarts lives in pkg/mcp, on top of Session.
func Pump(
	server string,
	clientIn io.Reader,
	clientOut io.Writer,
	serverIn io.WriteCloser,
	serverOut io.Reader,
	rec Recorder,
) error {
	sess := NewSession(server, clientOut, rec)

	// The request direction is not waited on: a real client holds stdin open
	// for the life of the session, and we must return as soon as the server
	// closes rather than blocking on that read forever.
	go func() {
		sess.ServeRequests(NewReaderSource(clientIn), WriteCloserTarget{W: serverIn})
		_ = serverIn.Close()
	}()

	return sess.ServeResponses(serverOut)
}

func newScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	s.Split(splitLinesPreservingNewline)
	return s
}

// splitLinesPreservingNewline is bufio.ScanLines, except it keeps the trailing
// '\n' so the proxy can forward bytes exactly as it received them.
func splitLinesPreservingNewline(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// stripNewline removes a single trailing newline before JSON parsing.
func stripNewline(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		return b[:n-1]
	}
	return b
}

func (s *Session) observeRequest(line []byte) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil || m.Method == "" {
		return
	}
	key := idKey(m.ID)
	if key == "" {
		// A notification has no response to pair with.
		return
	}

	s.mu.Lock()
	s.pending[key] = describeRequest(m)
	s.mu.Unlock()
}

// describeRequest captures what the session needs to know about a request: how
// to record it and, if it is refused, what to say.
func describeRequest(m rpcMessage) *pendingCall {
	pc := &pendingCall{
		method:   m.Method,
		args:     redactJSON(m.Params),
		start:    time.Now(),
		tokensIn: estimateTokens(m.Params),
	}
	if m.Method == "tools/call" {
		var params struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(m.Params, &params)
		pc.tool = params.Name
	}
	return pc
}

func (s *Session) observeResponse(line []byte) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	key := idKey(m.ID)
	if key == "" {
		return
	}

	s.mu.Lock()
	pc, ok := s.pending[key]
	if ok {
		delete(s.pending, key)
	}
	s.mu.Unlock()
	if !ok {
		return
	}

	call := Call{
		Time:       pc.start,
		Server:     s.server,
		Method:     pc.method,
		Tool:       pc.tool,
		Args:       pc.args,
		DurationMs: time.Since(pc.start).Milliseconds(),
		TokensIn:   pc.tokensIn,
	}
	if m.Error != nil {
		call.Status = "error"
		call.Error = truncate(m.Error.Message, maxStoredErrorBytes)
	} else {
		call.Status = "ok"
		call.Result = truncate(string(m.Result), maxStoredResultSize)
		call.TokensOut = estimateTokens(m.Result)
	}

	if s.rec != nil {
		s.rec.Record(call)
	}
}

// idKey normalises a JSON-RPC id so a request and its response match. JSON-RPC
// ids may be numbers or strings, and must round-trip identically.
func idKey(id json.RawMessage) string {
	trimmed := bytes.TrimSpace(id)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	return string(trimmed)
}

// estimateTokens is a rough character-based approximation (~4 characters per
// token). It is a signal for cost trends, not an exact count.
func estimateTokens(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	return (len(raw) + 3) / 4
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…(truncated)"
}

// redactJSON renders raw JSON for storage, replacing the values of
// credential-like keys and truncating the result.
func redactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncate(string(raw), maxStoredArgBytes)
	}

	redactValue(v)

	out, err := json.Marshal(v)
	if err != nil {
		return truncate(string(raw), maxStoredArgBytes)
	}
	return truncate(string(out), maxStoredArgBytes)
}

func redactValue(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = "[redacted]"
				continue
			}
			redactValue(val)
		}
	case []interface{}:
		for _, item := range t {
			redactValue(item)
		}
	}
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, marker := range []string{
		"token", "secret", "password", "passwd",
		"apikey", "api_key", "authorization", "credential", "private_key",
	} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}
