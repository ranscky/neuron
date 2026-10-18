package mcp

// Remote MCP servers.
//
// A server registered with a URL is reached over HTTP instead of being spawned.
// The session, policy and recording are exactly the same as for a local server:
// the transport changes, nothing else does.
//
// Two transports are supported:
//
//   - Streamable HTTP (the current MCP transport): every client message is POSTed
//     to the server URL, and the response is either one JSON message or an SSE
//     stream of messages.
//   - Legacy SSE: a GET stream announces an `endpoint` event naming where to POST,
//     and every response arrives on that stream instead of the POST.
//
// A response header of Mcp-Session-Id is echoed on later requests, which is how
// servers keep a session across the two transports.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ranscky/neuron/internal/proxy"
)

// remoteClient is used for every request: no overall timeout, because an SSE
// response stream is long-lived. Per-request cancellation closes it instead.
func remoteClient() *http.Client { return &http.Client{} }

// runRemote proxies a remote server described by spec.
func runRemote(spec ServerSpec, sess *proxy.Session, src proxy.LineSource) error {
	switch strings.ToLower(strings.TrimSpace(spec.Type)) {
	case "sse":
		return runRemoteSSE(spec, sess, src)
	default:
		return runRemoteHTTP(spec, sess, src)
	}
}

// runRemoteHTTP implements the streamable HTTP transport.
func runRemoteHTTP(spec ServerSpec, sess *proxy.Session, src proxy.LineSource) error {
	target := &httpTarget{url: spec.URL, client: remoteClient(), sess: sess}
	sess.ServeRequests(src, target)
	return nil
}

// httpTarget POSTs each client message and feeds the response back into the
// session.
type httpTarget struct {
	url       string
	client    *http.Client
	sess      *proxy.Session
	sessionID string
}

// Write implements proxy.ServerTarget. It returns false only when the client can
// no longer be written to, since a failed request is answered with an error
// rather than ending the session.
func (t *httpTarget) Write(line []byte) bool {
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(line))
	if err != nil {
		return t.fail(line, "could not build the request: "+err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return t.fail(line, "cannot reach the server: "+err.Error())
	}
	defer resp.Body.Close()

	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		t.sessionID = id
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return t.fail(line, fmt.Sprintf("the server returned %d", resp.StatusCode))
	}

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return t.stream(resp.Body)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteMessage))
	if err != nil {
		return t.fail(line, "could not read the response: "+err.Error())
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	return t.sess.Deliver(body) == nil
}

// stream forwards every `data:` event in an SSE response as a message.
func (t *httpTarget) stream(r io.Reader) bool {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRemoteMessage)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if err := t.sess.Deliver([]byte(data)); err != nil {
			return false
		}
	}
	return true
}

// fail answers the request that could not be served with a JSON-RPC error, so the
// client is never left waiting on a transport failure.
func (t *httpTarget) fail(requestLine []byte, message string) bool {
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(requestLine, &req); err != nil {
		return true
	}
	id := strings.TrimSpace(string(req.ID))
	if id == "" || id == "null" {
		return true
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    proxy.CodeServerUnavailable,
			"message": message,
		},
	})
	if err != nil {
		return true
	}
	return t.sess.Deliver(payload) == nil
}

// runRemoteSSE implements the legacy SSE transport: one GET stream for responses,
// POSTs to the endpoint the stream announces.
func runRemoteSSE(spec ServerSpec, sess *proxy.Session, src proxy.LineSource) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	client := remoteClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", spec.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", spec.URL, resp.StatusCode)
	}

	events := make(chan sseEvent)
	go readSSE(ctx, resp.Body, events)

	// The first useful event names where messages go.
	endpoint := ""
	for endpoint == "" {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return fmt.Errorf("%s closed before announcing an endpoint", spec.URL)
			}
			switch event.Name {
			case "endpoint":
				endpoint = resolveEndpoint(spec.URL, event.Data)
			default:
				if event.Data != "" {
					if err := sess.Deliver([]byte(event.Data)); err != nil {
						return nil
					}
				}
			}
		}
	}

	// Responses keep arriving on the stream while requests are POSTed.
	go func() {
		for event := range events {
			if event.Data == "" || event.Name == "endpoint" {
				continue
			}
			if err := sess.Deliver([]byte(event.Data)); err != nil {
				return
			}
		}
	}()

	target := &postTarget{url: endpoint, client: client, sess: sess}
	sess.ServeRequests(src, target)
	return nil
}

// postTarget POSTs client messages to the SSE endpoint. Responses arrive on the
// stream, not in the POST response, so a POST body is ignored.
type postTarget struct {
	url    string
	client *http.Client
	sess   *proxy.Session
}

// Write implements proxy.ServerTarget.
func (t *postTarget) Write(line []byte) bool {
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(line))
	if err != nil {
		return true
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return true
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxRemoteMessage))
	return true
}

// sseEvent is one server-sent event.
type sseEvent struct {
	Name string
	Data string
}

// readSSE parses an event stream until the body ends or the context is cancelled.
func readSSE(ctx context.Context, body io.Reader, out chan<- sseEvent) {
	defer close(out)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRemoteMessage)

	name := ""
	var data []string
	flush := func() bool {
		if len(data) == 0 {
			name = ""
			return true
		}
		event := sseEvent{Name: name, Data: strings.Join(data, "\n")}
		name, data = "", nil
		select {
		case out <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if !flush() {
				return
			}
		case strings.HasPrefix(line, ":"):
			// A comment, often a keep-alive.
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
}

// resolveEndpoint resolves the SSE endpoint against the stream URL.
func resolveEndpoint(base, endpoint string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return endpoint
	}
	ref, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	return parsed.ResolveReference(ref).String()
}

// maxRemoteMessage bounds one remote message or event.
const maxRemoteMessage = 16 << 20
