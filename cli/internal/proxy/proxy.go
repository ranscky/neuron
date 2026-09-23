// Package proxy implements a transparent MCP JSON-RPC proxy over stdio.
//
// It sits between an MCP client and an MCP server, forwarding every message
// unmodified while recording each request/response pair for observability.
package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
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

type pump struct {
	server  string
	rec     Recorder
	mu      sync.Mutex
	pending map[string]*pendingCall
}

// Pump proxies newline-delimited JSON-RPC between a client and a server.
//
// clientIn/clientOut are the client's view (usually os.Stdin/os.Stdout).
// serverIn/serverOut are the server process's stdin/stdout. Bytes are forwarded
// untouched in both directions. Pump returns when the server side closes or the
// write to the client fails, which is when the MCP session is over.
func Pump(
	server string,
	clientIn io.Reader,
	clientOut io.Writer,
	serverIn io.WriteCloser,
	serverOut io.Reader,
	rec Recorder,
) error {
	if clientIn == nil {
		clientIn = strings.NewReader("")
	}
	p := &pump{server: server, rec: rec, pending: map[string]*pendingCall{}}

	// The request direction is not waited on: a real client holds stdin open
	// for the life of the session, and we must return as soon as the server
	// closes rather than blocking on that read forever.
	go p.forwardRequests(clientIn, serverIn)

	return p.forwardResponses(serverOut, clientOut)
}

func (p *pump) forwardRequests(in io.Reader, out io.WriteCloser) {
	defer out.Close()

	scanner := newScanner(in)
	w := bufio.NewWriter(out)
	for scanner.Scan() {
		raw := scanner.Bytes()
		p.observeRequest(stripNewline(raw))

		// Write the bytes exactly as received, terminator included.
		if _, err := w.Write(raw); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func (p *pump) forwardResponses(in io.Reader, out io.Writer) error {
	scanner := newScanner(in)
	w := bufio.NewWriter(out)
	for scanner.Scan() {
		raw := scanner.Bytes()
		p.observeResponse(stripNewline(raw))

		// Write the bytes exactly as received, terminator included.
		if _, err := w.Write(raw); err != nil {
			return err
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
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

func (p *pump) observeRequest(line []byte) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil || m.Method == "" {
		return
	}
	key := idKey(m.ID)
	if key == "" {
		// A notification has no response to pair with.
		return
	}

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

	p.mu.Lock()
	p.pending[key] = pc
	p.mu.Unlock()
}

func (p *pump) observeResponse(line []byte) {
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	key := idKey(m.ID)
	if key == "" {
		return
	}

	p.mu.Lock()
	pc, ok := p.pending[key]
	if ok {
		delete(p.pending, key)
	}
	p.mu.Unlock()
	if !ok {
		return
	}

	call := Call{
		Time:       pc.start,
		Server:     p.server,
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

	if p.rec != nil {
		p.rec.Record(call)
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
