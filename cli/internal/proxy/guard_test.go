package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// recordingGuard refuses tools named in deny, and records what it saw.
type recordingGuard struct {
	deny []string
	seen []string
}

func (g *recordingGuard) Check(method string, params json.RawMessage) error {
	if method != "tools/call" {
		return nil
	}
	var call struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(params, &call)
	g.seen = append(g.seen, call.Name)
	for _, denied := range g.deny {
		if call.Name == denied {
			return &BlockedError{Code: CodePolicyDenied, Message: "blocked by test policy: " + call.Name}
		}
	}
	return nil
}

func TestGuardRefusesWithoutForwarding(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"delete_repo"}}` + "\n"

	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	forwarded := make(chan string, 1)
	go func() {
		scanner := newScanner(serverInR)
		if scanner.Scan() {
			forwarded <- string(scanner.Bytes())
		}
		_ = serverOutW.Close()
	}()

	var clientOut bytes.Buffer
	var recorded []Call
	sess := NewSession("gh", &clientOut, RecorderFunc(func(c Call) { recorded = append(recorded, c) }))
	guard := &recordingGuard{deny: []string{"delete_repo"}}
	sess.SetGuard(guard)

	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- sess.ServeResponses(serverOutR)
	}()
	sess.ServeRequests(NewReaderSource(strings.NewReader(req)), WriteCloserTarget{W: serverInW})
	_ = serverInW.Close()
	<-pumpDone

	// The client gets a JSON-RPC error naming the policy.
	out := clientOut.String()
	if !strings.Contains(out, `"code":-32001`) || !strings.Contains(out, "blocked by test policy") {
		t.Fatalf("expected a policy error, got %q", out)
	}
	if !strings.Contains(out, `"id":9`) {
		t.Errorf("the error must carry the request id, got %q", out)
	}

	// The server never saw the call.
	select {
	case line := <-forwarded:
		t.Fatalf("the refused call must not be forwarded, got %q", line)
	default:
	}

	// It is recorded as a failed call, so the dashboard shows the block.
	if len(recorded) != 1 || recorded[0].Status != "error" || recorded[0].Tool != "delete_repo" {
		t.Fatalf("expected one recorded error, got %+v", recorded)
	}
	if !strings.Contains(recorded[0].Error, "blocked by test policy") {
		t.Errorf("recorded error = %q", recorded[0].Error)
	}
}

func TestGuardLetsAllowedCallsThrough(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_file"}}` + "\n"

	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()
	forwarded := make(chan string, 1)
	go func() {
		scanner := newScanner(serverInR)
		if scanner.Scan() {
			forwarded <- string(scanner.Bytes())
			_, _ = serverOutW.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n"))
		}
		_ = serverOutW.Close()
	}()

	var clientOut bytes.Buffer
	sess := NewSession("gh", &clientOut, nil)
	sess.SetGuard(&recordingGuard{deny: []string{"delete_repo"}})

	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- sess.ServeResponses(serverOutR)
	}()
	sess.ServeRequests(NewReaderSource(strings.NewReader(req)), WriteCloserTarget{W: serverInW})
	_ = serverInW.Close()
	<-pumpDone

	select {
	case line := <-forwarded:
		if !strings.Contains(line, `"name":"get_file"`) {
			t.Errorf("forwarded line = %q", line)
		}
	default:
		t.Fatal("an allowed call must be forwarded")
	}
	if !strings.Contains(clientOut.String(), `"result":{"ok":true}`) {
		t.Errorf("client output = %q", clientOut.String())
	}
}

// A notification has no id, so refusing it would leave the client with no way to
// learn why. Notifications are never guarded.
func TestGuardIgnoresNotifications(t *testing.T) {
	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"

	serverInR, serverInW := io.Pipe()
	fwd := make(chan string, 1)
	go func() {
		scanner := newScanner(serverInR)
		if scanner.Scan() {
			fwd <- string(scanner.Bytes())
		}
	}()

	guarded := false
	sess := NewSession("gh", io.Discard, nil)
	sess.SetGuard(GuardFunc(func(string, json.RawMessage) error {
		guarded = true
		return &BlockedError{Code: CodePolicyDenied, Message: "should not run"}
	}))

	sess.ServeRequests(NewReaderSource(strings.NewReader(req)), WriteCloserTarget{W: serverInW})
	_ = serverInW.Close()

	select {
	case line := <-fwd:
		if !strings.Contains(line, "notifications/initialized") {
			t.Errorf("forwarded = %q", line)
		}
	case <-time.After(time.Second):
		t.Fatal("a notification must be forwarded")
	}
	if guarded {
		t.Error("a notification must not be sent to the guard")
	}
}

// A guard that returns a plain error is still a refusal, with the default code.
func TestGuardPlainErrorIsARefusal(t *testing.T) {
	var out bytes.Buffer
	sess := NewSession("gh", &out, nil)
	sess.SetGuard(GuardFunc(func(string, json.RawMessage) error {
		return errors.New("the policy file is broken")
	}))

	line := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x"}}`)
	if blocked := sess.refusal(line); blocked == nil || blocked.Code != CodePolicyDenied {
		t.Fatalf("a plain error should refuse with CodePolicyDenied, got %+v", blocked)
	}
}
