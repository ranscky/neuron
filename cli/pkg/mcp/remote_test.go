package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ranscky/neuron/internal/proxy"
)

// remoteStore builds a store holding one URL-registered server.
func remoteStore(t *testing.T, spec ServerSpec) *Store {
	t.Helper()
	store, err := newStoreAt(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("remote", spec); err != nil {
		t.Fatal(err)
	}
	return store
}

// syncWriter is a goroutine-safe client stdout stand-in.
type safeWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *safeWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *safeWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestRemoteHTTPProxiesAndRecords(t *testing.T) {
	var mu sync.Mutex
	var received []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	}))
	t.Cleanup(ts.Close)

	store := remoteStore(t, ServerSpec{URL: ts.URL})

	var out bytes.Buffer
	var recorded []proxy.Call
	var recMu sync.Mutex
	rec := proxy.RecorderFunc(func(c proxy.Call) {
		recMu.Lock()
		defer recMu.Unlock()
		recorded = append(recorded, c)
	})

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"remote_tool"}}` + "\n"
	if err := RunServerWithOptions(store, "remote", nil, rec, strings.NewReader(req),
		&out, io.Discard, Options{}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), `"result":{"ok":true}`) {
		t.Errorf("client output = %q", out.String())
	}

	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("expected one POST, got %d", count)
	}
	if !strings.Contains(received[0], "remote_tool") {
		t.Errorf("the request body was not forwarded verbatim: %q", received[0])
	}

	recMu.Lock()
	defer recMu.Unlock()
	if len(recorded) != 1 || recorded[0].Tool != "remote_tool" || recorded[0].Status != "ok" {
		t.Errorf("the call should be recorded like a local one: %+v", recorded)
	}
}

func TestRemoteHTTPStreamsSSEResponses(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, `data: {"jsonrpc":"2.0","method":"notifications/progress"}`+"\n\n")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, `data: {"jsonrpc":"2.0","id":1,"result":{"sse":true}}`+"\n\n")
	}))
	t.Cleanup(ts.Close)

	store := remoteStore(t, ServerSpec{URL: ts.URL})

	var out bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
	if err := RunServerWithOptions(store, "remote", nil, nil, strings.NewReader(req),
		&out, io.Discard, Options{}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, `"sse":true`) {
		t.Errorf("the SSE result should reach the client: %q", got)
	}
	if !strings.Contains(got, "notifications/progress") {
		t.Errorf("SSE notifications should be forwarded too: %q", got)
	}
}

func TestRemoteHTTPEchoesTheSessionID(t *testing.T) {
	var mu sync.Mutex
	var sessionIDs []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sessionIDs = append(sessionIDs, r.Header.Get("Mcp-Session-Id"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-1")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	t.Cleanup(ts.Close)

	store := remoteStore(t, ServerSpec{URL: ts.URL})

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	if err := RunServerWithOptions(store, "remote", nil, nil, strings.NewReader(req),
		io.Discard, io.Discard, Options{}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sessionIDs) != 2 {
		t.Fatalf("expected two POSTs, got %d", len(sessionIDs))
	}
	if sessionIDs[0] != "" {
		t.Errorf("the first request should not send a session id, got %q", sessionIDs[0])
	}
	if sessionIDs[1] != "sess-1" {
		t.Errorf("later requests must echo the server's session id, got %q", sessionIDs[1])
	}
}

func TestRemoteHTTPAnswersTransportFailures(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	store := remoteStore(t, ServerSpec{URL: ts.URL})

	var out bytes.Buffer
	var recorded []proxy.Call
	rec := proxy.RecorderFunc(func(c proxy.Call) { recorded = append(recorded, c) })

	req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"x"}}` + "\n"
	if err := RunServerWithOptions(store, "remote", nil, rec, strings.NewReader(req),
		&out, io.Discard, Options{}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	if !strings.Contains(got, `"id":7`) || !strings.Contains(got, "-32000") {
		t.Errorf("a transport failure should be answered with -32000: %q", got)
	}
	if len(recorded) != 1 || recorded[0].Status != "error" {
		t.Errorf("the failed call should be recorded: %+v", recorded)
	}
}

// The policy is enforced before the transport is touched: a denied call must not
// reach the remote server at all.
func TestRemotePolicyBlocksBeforeTheRequest(t *testing.T) {
	var mu sync.Mutex
	posts := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	t.Cleanup(ts.Close)

	store := remoteStore(t, ServerSpec{URL: ts.URL})

	var out bytes.Buffer
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"danger"}}` + "\n"

	blocking := proxy.GuardFunc(func(method string, params json.RawMessage) error {
		return &proxy.BlockedError{Code: proxy.CodePolicyDenied, Message: "blocked by policy"}
	})

	if err := RunServerWithOptions(store, "remote", nil, nil, strings.NewReader(req),
		&out, io.Discard, Options{Guard: blocking}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if posts != 0 {
		t.Errorf("a denied call must not be sent, got %d POST(s)", posts)
	}
	if !strings.Contains(out.String(), "-32001") {
		t.Errorf("the client should be told why: %q", out.String())
	}
}

// The legacy SSE transport: a GET stream announces where to POST, and responses
// arrive on the stream.
func TestRemoteSSETransport(t *testing.T) {
	posted := make(chan string, 4)
	ready := make(chan struct{}, 1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			_, _ = io.WriteString(w, "event: endpoint\n")
			_, _ = io.WriteString(w, "data: /messages\n\n")
			if flusher != nil {
				flusher.Flush()
			}

			select {
			case <-ready:
				_, _ = io.WriteString(w, "event: message\n")
				_, _ = io.WriteString(w, `data: {"jsonrpc":"2.0","id":1,"result":{"sse_legacy":true}}`+"\n\n")
				if flusher != nil {
					flusher.Flush()
				}
			case <-time.After(2 * time.Second):
			}

			<-r.Context().Done()
			return
		}

		body, _ := io.ReadAll(r.Body)
		posted <- string(body)
		select {
		case ready <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(ts.Close)

	store := remoteStore(t, ServerSpec{URL: ts.URL, Type: "sse"})

	clientInR, clientInW := io.Pipe()
	defer clientInW.Close()
	out := &safeWriter{}

	done := make(chan error, 1)
	go func() {
		done <- RunServerWithOptions(store, "remote", nil, nil, clientInR, out, io.Discard, Options{})
	}()

	if _, err := io.WriteString(clientInW, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(out.String(), "sse_legacy") {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "sse_legacy") {
		t.Fatalf("the SSE response should reach the client, got %q", out.String())
	}

	select {
	case got := <-posted:
		if !strings.Contains(got, "tools/list") {
			t.Errorf("the POST body was %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no message was POSTed to the announced endpoint")
	}

	_ = clientInW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the session did not end when the client hung up")
	}
}
