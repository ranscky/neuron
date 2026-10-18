// Package policy decides whether a tool call may reach an MCP server.
//
// A policy is optional: with no file, every call is allowed. One JSON file can
// grant named scopes (groups of tool patterns), deny individual tools, and cap
// the call rate per server. It is enforced in the proxy before a call is
// forwarded, so a refused tool never reaches the server and the refusal is
// recorded like any other failed call.
package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ranscky/neuron/internal/proxy"
)

// Decision is what a policy says about a server by default.
type Decision string

const (
	// Allow lets calls through unless a rule refuses them.
	Allow Decision = "allow"
	// Deny refuses calls unless a rule allows them.
	Deny Decision = "deny"
)

// RateLimit caps how many calls a server accepts in a rolling window.
type RateLimit struct {
	Calls int    `json:"calls"`
	Per   string `json:"per"` // any Go duration: "30s", "1m", "1h"
}

// Server is the policy for one MCP server.
type Server struct {
	// Grant names scopes (see Policy.Scopes) whose tool patterns are allowed.
	Grant []string `json:"grant,omitempty"`
	// Allow adds individual tool patterns on top of the granted scopes.
	Allow []string `json:"allow,omitempty"`
	// Deny always wins, whatever a scope or allow pattern says.
	Deny []string `json:"deny,omitempty"`
	// RateLimit caps calls per window for this server.
	RateLimit *RateLimit `json:"rate_limit,omitempty"`
}

// Policy is the on-disk shape of ~/.neuron/policy.json.
//
//	{
//	  "default": "allow",
//	  "scopes": {
//	    "read":  ["get_*", "list_*", "read_*"],
//	    "write": ["create_*", "update_*", "delete_*"]
//	  },
//	  "servers": {
//	    "github":   { "grant": ["read"], "deny": ["delete_repo"],
//	                  "rate_limit": {"calls": 60, "per": "1m"} },
//	    "filesystem": { "grant": ["read"] }
//	  }
//	}
type Policy struct {
	Default Decision            `json:"default,omitempty"`
	Scopes  map[string][]string `json:"scopes,omitempty"`
	Servers map[string]Server   `json:"servers,omitempty"`
}

// ErrNoPolicy means no policy file exists, so nothing is restricted.
var ErrNoPolicy = errors.New("no policy file")

// Verdict is the outcome of a policy decision, with the reason for display.
type Verdict struct {
	Allowed bool
	Reason  string
}

// Engine evaluates a policy and tracks rate-limit state.
type Engine struct {
	policy Policy
	path   string
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	start time.Time
	count int
}

// DefaultPath is where the CLI looks for a policy unless NEURON_POLICY is set.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "policy.json"
	}
	return filepath.Join(home, ".neuron", "policy.json")
}

// Load reads a policy file. An empty path means DefaultPath. A missing file is
// not an error: it returns (nil, ErrNoPolicy) so callers can allow everything.
func Load(path string) (*Engine, error) {
	if path == "" {
		path = os.Getenv("NEURON_POLICY")
	}
	if path == "" {
		path = DefaultPath()
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoPolicy
		}
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}

	var p Policy
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}

	engine, err := New(p)
	if err != nil {
		return nil, fmt.Errorf("policy %s: %w", path, err)
	}
	engine.path = path
	return engine, nil
}

// New validates a policy and builds an engine.
func New(p Policy) (*Engine, error) {
	if p.Default != "" && p.Default != Allow && p.Default != Deny {
		return nil, fmt.Errorf("default must be %q or %q, got %q", Allow, Deny, p.Default)
	}
	for name, patterns := range p.Scopes {
		if len(patterns) == 0 {
			return nil, fmt.Errorf("scope %q has no tool patterns", name)
		}
		for _, pattern := range patterns {
			if strings.TrimSpace(pattern) == "" {
				return nil, fmt.Errorf("scope %q has an empty tool pattern", name)
			}
		}
	}
	for name, srv := range p.Servers {
		for _, scope := range srv.Grant {
			if _, ok := p.Scopes[scope]; !ok {
				return nil, fmt.Errorf("server %q grants unknown scope %q", name, scope)
			}
		}
		if srv.RateLimit != nil {
			if srv.RateLimit.Calls <= 0 {
				return nil, fmt.Errorf("server %q: rate_limit.calls must be positive", name)
			}
			if _, err := time.ParseDuration(srv.RateLimit.Per); err != nil {
				return nil, fmt.Errorf("server %q: rate_limit.per %q: %w", name, srv.RateLimit.Per, err)
			}
		}
	}

	return &Engine{policy: p, now: time.Now, windows: map[string]*window{}}, nil
}

// Path is where the policy was loaded from, for display.
func (e *Engine) Path() string { return e.path }

// Policy returns a copy of the loaded policy.
func (e *Engine) Policy() Policy { return e.policy }

// Decide reports whether a tool may be called, without touching rate limits.
// It is what `neuron policy check` uses.
func (e *Engine) Decide(server, tool string) Verdict {
	srv, listed := e.policy.Servers[server]

	// A server with no entry follows the policy default.
	if !listed {
		if e.policy.Default == Deny {
			return Verdict{Reason: fmt.Sprintf("%s is not listed and the default is deny", server)}
		}
		return Verdict{Allowed: true, Reason: fmt.Sprintf("%s is not listed and the default is allow", server)}
	}

	for _, pattern := range srv.Deny {
		if matchTool(pattern, tool) {
			return Verdict{Reason: fmt.Sprintf("%q is denied for %s by rule %q", tool, server, pattern)}
		}
	}

	for _, scope := range srv.Grant {
		for _, pattern := range e.policy.Scopes[scope] {
			if matchTool(pattern, tool) {
				return Verdict{Allowed: true, Reason: fmt.Sprintf("%q is allowed by scope %q", tool, scope)}
			}
		}
	}

	for _, pattern := range srv.Allow {
		if matchTool(pattern, tool) {
			return Verdict{Allowed: true, Reason: fmt.Sprintf("%q is allowed by rule %q", tool, pattern)}
		}
	}

	// An entry with no positive rules means "everything except what is denied".
	if len(srv.Grant) == 0 && len(srv.Allow) == 0 {
		return Verdict{Allowed: true, Reason: fmt.Sprintf("%s has no allow rules, so only its deny rules apply", server)}
	}

	return Verdict{Reason: fmt.Sprintf("%q matches no scope or allow rule for %s", tool, server)}
}

// Admit decides and, when allowed, consumes rate-limit quota.
func (e *Engine) Admit(server, tool string) Verdict {
	verdict := e.Decide(server, tool)
	if !verdict.Allowed {
		return verdict
	}

	srv, listed := e.policy.Servers[server]
	if !listed || srv.RateLimit == nil {
		return verdict
	}
	if !e.take(server, srv.RateLimit) {
		return Verdict{Reason: fmt.Sprintf(
			"%s is rate limited by policy: at most %d calls per %s",
			server, srv.RateLimit.Calls, srv.RateLimit.Per)}
	}
	return verdict
}

// take consumes one call from the server's window, reporting false when the
// limit for the current window is already spent.
func (e *Engine) take(server string, limit *RateLimit) bool {
	period, err := time.ParseDuration(limit.Per)
	if err != nil {
		// Validated at load time; treat an impossible policy as no limit.
		return true
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	w, ok := e.windows[server]
	if !ok || now.Sub(w.start) >= period {
		e.windows[server] = &window{start: now, count: 1}
		return true
	}
	if w.count >= limit.Calls {
		return false
	}
	w.count++
	return true
}

// Guard adapts the policy to one server's proxy session. A nil engine returns a
// nil guard, which lets everything through.
func (e *Engine) Guard(server string) proxy.Guard {
	if e == nil {
		return nil
	}
	return &guard{engine: e, server: server}
}

type guard struct {
	engine *Engine
	server string
}

// Check implements proxy.Guard. Only tools/call is policed: the policy speaks
// about tools, and blocking protocol chatter would break the connection.
func (g *guard) Check(method string, params json.RawMessage) error {
	if method != "tools/call" {
		return nil
	}

	var call struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &call); err != nil || call.Name == "" {
		return &proxy.BlockedError{Code: proxy.CodePolicyDenied, Message: "tools/call without a tool name"}
	}

	verdict := g.engine.Admit(g.server, call.Name)
	if verdict.Allowed {
		return nil
	}

	code := proxy.CodePolicyDenied
	if strings.Contains(verdict.Reason, "rate limited") {
		code = proxy.CodePolicyRateLimited
	}
	return &proxy.BlockedError{Code: code, Message: "blocked by Neuron policy: " + verdict.Reason}
}

// matchTool matches a tool name against a pattern, case-insensitively. '*' is a
// wildcard that spans any characters, so "get_*" covers "get_file".
func matchTool(pattern, tool string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	tool = strings.ToLower(tool)
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}

	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == tool
	}

	// The first part must be a prefix, the last a suffix, and the middle parts
	// must appear in order.
	if !strings.HasPrefix(tool, parts[0]) {
		return false
	}
	rest := tool[len(parts[0]):]
	for i := 1; i < len(parts)-1; i++ {
		idx := strings.Index(rest, parts[i])
		if idx < 0 {
			return false
		}
		rest = rest[idx+len(parts[i]):]
	}
	last := parts[len(parts)-1]
	if last == "" {
		return true
	}
	return strings.HasSuffix(rest, last)
}
