package proxy

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleCalls(t *testing.T) []Call {
	t.Helper()

	base := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	return []Call{
		{Time: base, Server: "github", Method: "tools/call", Tool: "get_file", Status: "ok", DurationMs: 12, TokensIn: 5, TokensOut: 7, Args: `{"name":"get_file"}`, Result: `{"ok":true}`},
		{Time: base.Add(time.Minute), Server: "github", Method: "tools/call", Tool: "delete_repo", Status: "error", Error: "blocked by Neuron policy", Args: `{"name":"delete_repo"}`},
		{Time: base.Add(2 * time.Minute), Server: "filesystem", Method: "tools/call", Tool: "read_file", Status: "ok", Args: `{"path":"a,b"}`},
	}
}

func TestReadCallsAtRoundTripsTheHistoryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	history := NewHistoryAt(path)
	for _, c := range sampleCalls(t) {
		history.Record(c)
	}

	calls, err := ReadCallsAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}
	if calls[0].Tool != "get_file" || calls[2].Server != "filesystem" {
		t.Errorf("unexpected order/content: %+v", calls)
	}
}

func TestReadCallsAtMissingFileIsEmpty(t *testing.T) {
	calls, err := ReadCallsAt(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatalf("a missing history is not an error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected no calls, got %d", len(calls))
	}
}

func TestFilter(t *testing.T) {
	calls := sampleCalls(t)
	base := calls[0].Time

	cases := []struct {
		name   string
		filter Filter
		want   int
	}{
		{"everything", Filter{}, 3},
		{"by server", Filter{Server: "github"}, 2},
		{"by server, case-insensitive", Filter{Server: "GITHUB"}, 2},
		{"by status", Filter{Status: "error"}, 1},
		{"by tool", Filter{Tool: "read_file"}, 1},
		{"since excludes the first", Filter{Since: base.Add(30 * time.Second)}, 2},
		{"until excludes the last", Filter{Until: base.Add(90 * time.Second)}, 2},
		{"window", Filter{Since: base.Add(30 * time.Second), Until: base.Add(90 * time.Second)}, 1},
		{"no match", Filter{Server: "nope"}, 0},
	}
	for _, tc := range cases {
		if got := len(FilterCalls(calls, tc.filter)); got != tc.want {
			t.Errorf("%s: got %d calls, want %d", tc.name, got, tc.want)
		}
	}
}

func TestWriteCallsJSONL(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCalls(&buf, sampleCalls(t), "jsonl"); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, line := range lines {
		var c Call
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("line is not valid JSON: %v (%s)", err, line)
		}
	}
}

func TestWriteCallsCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCalls(&buf, sampleCalls(t), "csv"); err != nil {
		t.Fatal(err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 {
		t.Fatalf("got %d rows (including header), want 4", len(records))
	}
	if strings.Join(records[0], ",") != "time,server,method,tool,status,duration_ms,tokens_in,tokens_out,error,args,result" {
		t.Errorf("header = %v", records[0])
	}
	// The third call's args contain a comma; csv quoting must keep it in one field.
	if !strings.Contains(records[3][9], `"path":"a,b"`) {
		t.Errorf("args column = %q", records[3][9])
	}
	if records[1][0] != "2026-10-18T12:00:00Z" {
		t.Errorf("time column = %q", records[1][0])
	}
}

func TestWriteCallsRejectsUnknownFormat(t *testing.T) {
	if err := WriteCalls(&bytes.Buffer{}, nil, "xml"); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
	if err := WriteCalls(&bytes.Buffer{}, nil, ""); err != nil {
		t.Errorf("an empty format should default to jsonl: %v", err)
	}
}

func TestDefaultHistoryPath(t *testing.T) {
	path := DefaultHistoryPath()
	if path == "" || !strings.HasSuffix(filepath.ToSlash(path), ".neuron/history.jsonl") {
		t.Errorf("DefaultHistoryPath() = %q", path)
	}
}
