package proxy

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultHistoryPath is where recorded calls live unless the caller points
// somewhere else (`NEURON_HISTORY`).
func DefaultHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "history.jsonl"
	}
	return filepath.Join(home, ".neuron", "history.jsonl")
}

// Filter selects recorded calls for an export. A zero value matches everything.
type Filter struct {
	Since  time.Time
	Until  time.Time
	Server string
	Status string
	Tool   string
}

// Match reports whether a call passes the filter.
func (f Filter) Match(c Call) bool {
	if !f.Since.IsZero() && c.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && c.Time.After(f.Until) {
		return false
	}
	if f.Server != "" && !strings.EqualFold(c.Server, f.Server) {
		return false
	}
	if f.Status != "" && !strings.EqualFold(c.Status, f.Status) {
		return false
	}
	if f.Tool != "" && !strings.EqualFold(c.Tool, f.Tool) {
		return false
	}
	return true
}

// FilterCalls returns only the calls a filter matches, preserving order.
func FilterCalls(calls []Call, f Filter) []Call {
	out := make([]Call, 0, len(calls))
	for _, c := range calls {
		if f.Match(c) {
			out = append(out, c)
		}
	}
	return out
}

// ReadCallsAt reads every record from a history file. A missing file yields an
// empty slice: nothing has been recorded yet, which is not an error.
func ReadCallsAt(path string) ([]Call, error) {
	history := NewHistoryAt(path)
	lines, err := history.readLines()
	if err != nil {
		return nil, fmt.Errorf("read history %s: %w", path, err)
	}

	calls := make([]Call, 0, len(lines))
	for _, line := range lines {
		var c Call
		if err := json.Unmarshal(line, &c); err != nil {
			// A torn write at the end of the file should not hide the rest.
			continue
		}
		calls = append(calls, c)
	}
	return calls, nil
}

// ExportFormats lists the formats WriteCalls understands.
var ExportFormats = []string{"jsonl", "csv"}

// WriteCalls writes calls in the requested format. Fields are redacted and
// truncated before they are recorded, so an export never widens what is stored.
func WriteCalls(w io.Writer, calls []Call, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "jsonl":
		encoder := json.NewEncoder(w)
		for _, c := range calls {
			if err := encoder.Encode(c); err != nil {
				return fmt.Errorf("write jsonl: %w", err)
			}
		}
		return nil

	case "csv":
		writer := csv.NewWriter(w)
		header := []string{
			"time", "server", "method", "tool", "status",
			"duration_ms", "tokens_in", "tokens_out", "error", "args", "result",
		}
		if err := writer.Write(header); err != nil {
			return fmt.Errorf("write csv header: %w", err)
		}
		for _, c := range calls {
			row := []string{
				c.Time.UTC().Format(time.RFC3339Nano),
				c.Server,
				c.Method,
				c.Tool,
				c.Status,
				strconv.FormatInt(c.DurationMs, 10),
				strconv.Itoa(c.TokensIn),
				strconv.Itoa(c.TokensOut),
				c.Error,
				c.Args,
				c.Result,
			}
			if err := writer.Write(row); err != nil {
				return fmt.Errorf("write csv row: %w", err)
			}
		}
		writer.Flush()
		return writer.Error()

	default:
		return fmt.Errorf("unknown export format %q: use %s", format, strings.Join(ExportFormats, " or "))
	}
}
