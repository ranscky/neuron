package proxy

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// maxHistoryBytes bounds the history file; past this, older calls are dropped.
const maxHistoryBytes = 8 << 20

// History is an append-only JSONL store of recorded calls.
type History struct {
	path string
	mu   sync.Mutex
}

// NewHistory opens the default store at ~/.neuron/history.jsonl.
func NewHistory() (*History, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return NewHistoryAt(filepath.Join(home, ".neuron", "history.jsonl")), nil
}

// NewHistoryAt opens a store at an explicit path.
func NewHistoryAt(path string) *History {
	return &History{path: path}
}

// Record implements Recorder. It is best-effort: failing to persist telemetry
// must never break the server being proxied.
func (h *History) Record(c Call) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return
	}
	h.rotateIfNeeded()

	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// Recent returns up to n most recent calls, oldest first. n <= 0 returns all.
func (h *History) Recent(n int) []Call {
	h.mu.Lock()
	defer h.mu.Unlock()

	lines, err := h.readLines()
	if err != nil {
		return nil
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	calls := make([]Call, 0, len(lines))
	for _, line := range lines {
		var c Call
		if err := json.Unmarshal(line, &c); err == nil {
			calls = append(calls, c)
		}
	}
	return calls
}

// Stats aggregates recorded calls per server.
type Stats struct {
	Server    string  `json:"server"`
	Calls     int     `json:"calls"`
	Errors    int     `json:"errors"`
	AvgMs     float64 `json:"avg_ms"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
}

// Stats returns per-server aggregates over the stored history.
func (h *History) Stats() []Stats {
	calls := h.Recent(0)

	index := map[string]*Stats{}
	totals := map[string]int64{}
	var order []string

	for _, c := range calls {
		s, ok := index[c.Server]
		if !ok {
			s = &Stats{Server: c.Server}
			index[c.Server] = s
			order = append(order, c.Server)
		}
		s.Calls++
		if c.Status == "error" {
			s.Errors++
		}
		s.TokensIn += c.TokensIn
		s.TokensOut += c.TokensOut
		totals[c.Server] += c.DurationMs
	}

	out := make([]Stats, 0, len(order))
	for _, name := range order {
		s := index[name]
		if s.Calls > 0 {
			s.AvgMs = float64(totals[name]) / float64(s.Calls)
		}
		out = append(out, *s)
	}
	return out
}

func (h *History) readLines() ([][]byte, error) {
	f, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		out = append(out, append([]byte(nil), line...))
	}
	return out, scanner.Err()
}

// rotateIfNeeded keeps the file bounded by rewriting only the most recent
// calls once it grows past maxHistoryBytes.
func (h *History) rotateIfNeeded() {
	info, err := os.Stat(h.path)
	if err != nil || info.Size() < maxHistoryBytes {
		return
	}

	lines, err := h.readLines()
	if err != nil {
		return
	}
	if len(lines) > 1000 {
		lines = lines[len(lines)-1000:]
	}

	var buf []byte
	for _, line := range lines {
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	_ = os.WriteFile(h.path, buf, 0o600)
}
