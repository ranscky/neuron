package policy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ranscky/neuron/internal/proxy"
)

func testEngine(t *testing.T, p Policy) *Engine {
	t.Helper()
	engine, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestUnlistedServerFollowsTheDefault(t *testing.T) {
	allow := testEngine(t, Policy{})
	if v := allow.Decide("anything", "delete_everything"); !v.Allowed {
		t.Errorf("default allow should permit the call: %s", v.Reason)
	}

	deny := testEngine(t, Policy{Default: Deny})
	if v := deny.Decide("anything", "get_file"); v.Allowed {
		t.Errorf("default deny should refuse the call: %s", v.Reason)
	}
}

func TestDenyWinsOverGrantAndAllow(t *testing.T) {
	engine := testEngine(t, Policy{
		Scopes: map[string][]string{"read": {"get_*"}},
		Servers: map[string]Server{
			"github": {Grant: []string{"read"}, Allow: []string{"delete_repo"}, Deny: []string{"delete_repo"}},
		},
	})

	if v := engine.Decide("github", "get_file"); !v.Allowed {
		t.Errorf("granted scope should allow: %s", v.Reason)
	}
	if v := engine.Decide("github", "delete_repo"); v.Allowed {
		t.Errorf("deny must win: %s", v.Reason)
	}
}

func TestScopeGrantLimitsTools(t *testing.T) {
	engine := testEngine(t, Policy{
		Scopes: map[string][]string{
			"read":  {"get_*", "list_*"},
			"write": {"create_*"},
		},
		Servers: map[string]Server{"gh": {Grant: []string{"read"}}},
	})

	for _, tool := range []string{"get_file", "LIST_issues"} {
		if v := engine.Decide("gh", tool); !v.Allowed {
			t.Errorf("%s should be allowed: %s", tool, v.Reason)
		}
	}
	if v := engine.Decide("gh", "create_issue"); v.Allowed {
		t.Errorf("an ungranted scope must not allow the tool: %s", v.Reason)
	}
}

func TestServerWithOnlyDenyRulesAllowsTheRest(t *testing.T) {
	engine := testEngine(t, Policy{
		Servers: map[string]Server{"gh": {Deny: []string{"delete_*"}}},
	})

	if v := engine.Decide("gh", "get_file"); !v.Allowed {
		t.Errorf("only deny rules means everything else is allowed: %s", v.Reason)
	}
	if v := engine.Decide("gh", "delete_repo"); v.Allowed {
		t.Error("delete_repo should be denied")
	}
}

func TestRateLimitIsEnforcedAndResets(t *testing.T) {
	engine := testEngine(t, Policy{
		Servers: map[string]Server{"gh": {RateLimit: &RateLimit{Calls: 2, Per: "1m"}}},
	})

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if v := engine.Admit("gh", "get_file"); !v.Allowed {
			t.Fatalf("call %d should be admitted: %s", i+1, v.Reason)
		}
	}
	v := engine.Admit("gh", "get_file")
	if v.Allowed {
		t.Fatal("the third call in the window should be rate limited")
	}
	if want := "rate limited"; !contains(v.Reason, want) {
		t.Errorf("reason %q should mention %q", v.Reason, want)
	}

	// The next window starts fresh.
	now = now.Add(time.Minute)
	if v := engine.Admit("gh", "get_file"); !v.Allowed {
		t.Errorf("a new window should admit calls: %s", v.Reason)
	}

	// Decide never consumes quota.
	engine.now = func() time.Time { return now.Add(2 * time.Minute) }
	for i := 0; i < 10; i++ {
		if v := engine.Decide("gh", "get_file"); !v.Allowed {
			t.Fatalf("Decide must not be rate limited: %s", v.Reason)
		}
	}
}

func TestNewValidatesThePolicy(t *testing.T) {
	cases := map[string]Policy{
		"bad default":       {Default: "maybe"},
		"unknown scope":     {Servers: map[string]Server{"gh": {Grant: []string{"nope"}}}},
		"empty scope":       {Scopes: map[string][]string{"read": {}}},
		"bad rate duration": {Servers: map[string]Server{"gh": {RateLimit: &RateLimit{Calls: 1, Per: "often"}}}},
		"zero rate calls":   {Servers: map[string]Server{"gh": {RateLimit: &RateLimit{Calls: 0, Per: "1m"}}}},
	}
	for name, p := range cases {
		if _, err := New(p); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestMatchTool(t *testing.T) {
	cases := []struct {
		pattern, tool string
		want          bool
	}{
		{"get_*", "get_file", true},
		{"get_*", "GET_FILE", true},
		{"*_repo", "delete_repo", true},
		{"get_*_file", "get_my_file", true},
		{"get_*_file", "get_my_doc", false},
		{"*", "anything", true},
		{"exact", "exact", true},
		{"exact", "notexact", false},
		{"", "anything", false},
		{"get_*", "put_file", false},
	}
	for _, tc := range cases {
		if got := matchTool(tc.pattern, tc.tool); got != tc.want {
			t.Errorf("matchTool(%q, %q) = %v, want %v", tc.pattern, tc.tool, got, tc.want)
		}
	}
}

func TestLoadHandlesMissingAndMalformedFiles(t *testing.T) {
	dir := t.TempDir()

	if _, err := Load(filepath.Join(dir, "absent.json")); !errors.Is(err, ErrNoPolicy) {
		t.Errorf("missing file: got %v, want ErrNoPolicy", err)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"default":"sometimes"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("an invalid policy should fail to load")
	}

	unknown := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"servers":{"gh":{"grant":["read"]}},"typo":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(unknown); err == nil {
		t.Error("unknown fields should be rejected so typos do not silently weaken the policy")
	}

	good := filepath.Join(dir, "good.json")
	content := `{"default":"deny","scopes":{"read":["get_*"]},"servers":{"gh":{"grant":["read"]}}}`
	if err := os.WriteFile(good, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := Load(good)
	if err != nil {
		t.Fatal(err)
	}
	if engine.Path() != good {
		t.Errorf("path = %q", engine.Path())
	}
	if engine.Policy().Default != Deny {
		t.Errorf("default = %q", engine.Policy().Default)
	}
}

func TestGuardMapsVerdictsToJSONRPCCodes(t *testing.T) {
	engine := testEngine(t, Policy{
		Servers: map[string]Server{
			"gh": {Allow: []string{"get_*"}, RateLimit: &RateLimit{Calls: 1, Per: "1h"}},
		},
	})
	guard := engine.Guard("gh")

	params := json.RawMessage(`{"name":"get_file"}`)
	if err := guard.Check("tools/call", params); err != nil {
		t.Fatalf("an allowed tool should pass: %v", err)
	}
	err := guard.Check("tools/call", params)
	var blocked *proxy.BlockedError
	if !errors.As(err, &blocked) || blocked.Code != proxy.CodePolicyRateLimited {
		t.Fatalf("second call should be rate limited, got %v", err)
	}

	err = guard.Check("tools/call", json.RawMessage(`{"name":"delete_repo"}`))
	if !errors.As(err, &blocked) || blocked.Code != proxy.CodePolicyDenied {
		t.Fatalf("a denied tool should map to CodePolicyDenied, got %v", err)
	}

	// Only tools/call is policed; protocol chatter must keep flowing.
	if err := guard.Check("tools/list", nil); err != nil {
		t.Errorf("tools/list should not be policed: %v", err)
	}
}

func TestNilEngineGuardIsNil(t *testing.T) {
	var engine *Engine
	if engine.Guard("gh") != nil {
		t.Error("a nil engine must produce a nil guard")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
