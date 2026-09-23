package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServer wires an in-memory server side that replies to each request with
// the supplied response line, then closes.
func fakeServer(t *testing.T, resp string) (io.WriteCloser, io.Reader) {
	t.Helper()
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	go func() {
		scanner := bufio.NewScanner(serverInR)
		for scanner.Scan() {
			if _, err := serverOutW.Write([]byte(resp)); err != nil {
				break
			}
		}
		_ = serverOutW.Close()
	}()

	return serverInW, serverOutR
}

func TestPumpRecordsToolCallAndForwards(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/tmp/x","token":"s3cr3t"}}}` + "\n"
	resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello world"}]}}` + "\n"

	var clientOut bytes.Buffer
	serverIn, serverOut := fakeServer(t, resp)

	var got []Call
	err := Pump("fs", strings.NewReader(req), &clientOut, serverIn, serverOut,
		RecorderFunc(func(c Call) { got = append(got, c) }))
	if err != nil {
		t.Fatalf("Pump returned error: %v", err)
	}

	if clientOut.String() != resp {
		t.Errorf("proxy must forward the response untouched:\n got %q\nwant %q", clientOut.String(), resp)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(got))
	}

	c := got[0]
	if c.Method != "tools/call" || c.Tool != "read_file" || c.Status != "ok" {
		t.Errorf("unexpected call: %+v", c)
	}
	if strings.Contains(c.Args, "s3cr3t") {
		t.Error("credential value leaked into recorded args")
	}
	if !strings.Contains(c.Args, "[redacted]") {
		t.Errorf("expected a redaction marker in args, got %q", c.Args)
	}
	if !strings.Contains(c.Args, "/tmp/x") {
		t.Errorf("non-secret args should be preserved, got %q", c.Args)
	}
	if !strings.Contains(c.Result, "hello world") {
		t.Errorf("result not recorded, got %q", c.Result)
	}
	if c.TokensOut == 0 {
		t.Error("expected a non-zero token estimate for the result")
	}
	if c.DurationMs < 0 {
		t.Errorf("duration should not be negative, got %d", c.DurationMs)
	}
}

func TestPumpMatchesStringIDsAndRecordsErrors(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"name":"boom"}}` + "\n"
	resp := `{"jsonrpc":"2.0","id":"abc","error":{"code":-32601,"message":"method not found"}}` + "\n"

	var clientOut bytes.Buffer
	serverIn, serverOut := fakeServer(t, resp)

	var got []Call
	if err := Pump("s", strings.NewReader(req), &clientOut, serverIn, serverOut,
		RecorderFunc(func(c Call) { got = append(got, c) })); err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 call, got %d", len(got))
	}
	if got[0].Status != "error" {
		t.Errorf("expected error status, got %q", got[0].Status)
	}
	if !strings.Contains(got[0].Error, "method not found") {
		t.Errorf("error message not recorded: %q", got[0].Error)
	}
}

func TestPumpIgnoresNotifications(t *testing.T) {
	clientIn := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")

	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, serverInR)
		_ = serverOutW.Close()
	}()

	var got []Call
	var clientOut bytes.Buffer
	if err := Pump("n", clientIn, &clientOut, serverInW, serverOutR,
		RecorderFunc(func(c Call) { got = append(got, c) })); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("notifications must not be recorded, got %+v", got)
	}
}

func TestPumpPreservesBytesWithoutTrailingNewline(t *testing.T) {
	// Deliberately unterminated: the proxy must not invent a terminator.
	resp := `{"jsonrpc":"2.0","id":1,"result":{}}`

	var clientOut bytes.Buffer
	serverIn, serverOut := fakeServer(t, resp)

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"
	if err := Pump("s", strings.NewReader(req), &clientOut, serverIn, serverOut, nil); err != nil {
		t.Fatal(err)
	}
	if clientOut.String() != resp {
		t.Errorf("proxy altered the bytes:\n got %q\nwant %q", clientOut.String(), resp)
	}
}

func TestPumpThroughRealProcessUnterminatedOutput(t *testing.T) {
	cmd := exec.Command("sh", "-c", `printf %s hi`)
	serverIn, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	serverOut, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	pumpErr := Pump("d", nil, &out, serverIn, serverOut, nil)
	_ = serverIn.Close()
	waitErr := cmd.Wait()

	t.Logf("pumpErr=%v waitErr=%v out=%q", pumpErr, waitErr, out.String())
	if out.String() != "hi" {
		t.Errorf("expected %q, got %q", "hi", out.String())
	}
}

func TestRedactJSON(t *testing.T) {
	got := redactJSON(json.RawMessage(`{"path":"/x","token":"abc","nested":{"api_key":"k","ok":1}}`))

	if strings.Contains(got, "abc") || strings.Contains(got, `"k"`) {
		t.Errorf("secret values leaked: %s", got)
	}
	if !strings.Contains(got, "/x") {
		t.Errorf("non-secret path lost: %s", got)
	}
	if !strings.Contains(got, `"ok":1`) {
		t.Errorf("non-secret field lost: %s", got)
	}
}

func TestHistoryRecordRecentAndStats(t *testing.T) {
	h := NewHistoryAt(filepath.Join(t.TempDir(), "history.jsonl"))

	h.Record(Call{Server: "a", Method: "tools/call", Tool: "t", Status: "ok", DurationMs: 10, TokensIn: 5, TokensOut: 7})
	h.Record(Call{Server: "a", Method: "tools/call", Status: "error", DurationMs: 20})
	h.Record(Call{Server: "b", Method: "initialize", Status: "ok", DurationMs: 1})

	recent := h.Recent(2)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent calls, got %d", len(recent))
	}
	if recent[len(recent)-1].Server != "b" {
		t.Errorf("expected the newest call last, got %+v", recent)
	}

	var a, b *Stats
	stats := h.Stats()
	for i := range stats {
		switch stats[i].Server {
		case "a":
			a = &stats[i]
		case "b":
			b = &stats[i]
		}
	}
	if a == nil || b == nil {
		t.Fatalf("missing stats: %+v", stats)
	}
	if a.Calls != 2 || a.Errors != 1 {
		t.Errorf("unexpected call/error counts: %+v", a)
	}
	if a.AvgMs != 15 {
		t.Errorf("expected 15ms average, got %v", a.AvgMs)
	}
	if a.TokensIn != 5 || a.TokensOut != 7 {
		t.Errorf("unexpected token totals: %+v", a)
	}
}

func TestHistoryMissingFileIsEmpty(t *testing.T) {
	h := NewHistoryAt(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if calls := h.Recent(10); len(calls) != 0 {
		t.Errorf("expected no calls for a missing file, got %d", len(calls))
	}
	if stats := h.Stats(); len(stats) != 0 {
		t.Errorf("expected no stats for a missing file, got %d", len(stats))
	}
}

func TestDashboardHandler(t *testing.T) {
	h := NewHistoryAt(filepath.Join(t.TempDir(), "history.jsonl"))
	h.Record(Call{Server: "srv", Method: "tools/call", Tool: "x", Status: "ok", DurationMs: 3})

	srv := httptest.NewServer(DashboardHandler(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/calls")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var calls []Call
	if err := json.NewDecoder(resp.Body).Decode(&calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Tool != "x" {
		t.Errorf("unexpected calls payload: %+v", calls)
	}

	page, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	body, _ := io.ReadAll(page.Body)
	if !strings.Contains(string(body), "MCP activity") {
		t.Error("dashboard HTML was not served")
	}
}
