package sync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubState struct {
	plan      string
	approved  bool
	loggedOut bool
	blobs     map[string]RemoteBlob
}

func writeStub(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func authOK(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer tok_1"
}

// newStubCloud stands in for the real service so the client's HTTP contract is
// tested without a second module or a running server.
func newStubCloud(t *testing.T) (*httptest.Server, *stubState) {
	t.Helper()

	state := &stubState{plan: "free", blobs: map[string]RemoteBlob{}}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusCreated, map[string]any{"account_id": "acct_1", "salt": "c2FsdA==", "plan": "free"})
	})

	mux.HandleFunc("POST /v1/device/code", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, map[string]any{"device_code": "dev_1", "user_code": "ABCD-EFGH", "interval": 1})
	})

	mux.HandleFunc("POST /v1/device/approve", func(w http.ResponseWriter, r *http.Request) {
		state.approved = true
		writeStub(w, http.StatusOK, map[string]any{"approved": true})
	})

	mux.HandleFunc("POST /v1/device/token", func(w http.ResponseWriter, r *http.Request) {
		if !state.approved {
			writeStub(w, http.StatusPreconditionRequired, map[string]string{"error": "authorization_pending"})
			return
		}
		writeStub(w, http.StatusOK, map[string]any{"access_token": "tok_1", "token_type": "Bearer"})
	})

	mux.HandleFunc("GET /v1/account", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeStub(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeStub(w, http.StatusOK, map[string]any{"account_id": "acct_1", "plan": state.plan, "salt": "c2FsdA=="})
	})

	mux.HandleFunc("POST /v1/logout", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeStub(w, http.StatusUnauthorized, nil)
			return
		}
		state.loggedOut = true
		writeStub(w, http.StatusOK, map[string]any{"status": "logged out"})
	})

	mux.HandleFunc("GET /v1/sync/blobs", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeStub(w, http.StatusUnauthorized, nil)
			return
		}
		if state.plan != "pro" {
			writeStub(w, http.StatusPaymentRequired, map[string]string{"error": "paid plan required"})
			return
		}
		names := make([]string, 0, len(state.blobs))
		for name := range state.blobs {
			names = append(names, name)
		}
		writeStub(w, http.StatusOK, map[string]any{"names": names})
	})

	mux.HandleFunc("GET /v1/sync/blobs/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeStub(w, http.StatusUnauthorized, nil)
			return
		}
		if state.plan != "pro" {
			writeStub(w, http.StatusPaymentRequired, nil)
			return
		}
		blob, ok := state.blobs[r.PathValue("name")]
		if !ok {
			writeStub(w, http.StatusNotFound, map[string]string{"error": "missing"})
			return
		}
		writeStub(w, http.StatusOK, blob)
	})

	mux.HandleFunc("PUT /v1/sync/blobs/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeStub(w, http.StatusUnauthorized, nil)
			return
		}
		if state.plan != "pro" {
			writeStub(w, http.StatusPaymentRequired, nil)
			return
		}
		var body struct {
			Version  int64           `json:"version"`
			Envelope json.RawMessage `json:"envelope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeStub(w, http.StatusBadRequest, nil)
			return
		}
		name := r.PathValue("name")
		if existing, ok := state.blobs[name]; ok && body.Version <= existing.Version {
			writeStub(w, http.StatusConflict, map[string]string{"error": "stale"})
			return
		}
		state.blobs[name] = RemoteBlob{Name: name, Version: body.Version, Envelope: &Envelope{Version: 1}}
		writeStub(w, http.StatusOK, map[string]any{"name": name, "version": body.Version})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, state
}

func TestClientDeviceFlow(t *testing.T) {
	ts, _ := newStubCloud(t)
	ctx := context.Background()
	client := NewClient(ts.URL)

	code, err := client.RequestDeviceCode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if code.DeviceCode == "" || code.UserCode == "" {
		t.Fatalf("incomplete device code: %+v", code)
	}

	if _, err := client.PollDeviceToken(ctx, code.DeviceCode); !errors.Is(err, ErrPending) {
		t.Fatalf("expected ErrPending before approval, got %v", err)
	}

	if err := client.ApproveDeviceCode(ctx, code.UserCode, "acct_1"); err != nil {
		t.Fatal(err)
	}

	token, err := client.PollDeviceToken(ctx, code.DeviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if token != "tok_1" {
		t.Errorf("unexpected token %q", token)
	}
}

func TestClientCreateAccount(t *testing.T) {
	ts, _ := newStubCloud(t)
	client := NewClient(ts.URL)

	id, err := client.CreateAccount(context.Background(), "dev@example.com", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if id != "acct_1" {
		t.Errorf("unexpected account id %q", id)
	}
}

func TestClientRequiresAToken(t *testing.T) {
	ts, _ := newStubCloud(t)
	ctx := context.Background()

	if _, err := NewClient(ts.URL).GetAccount(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized without a token, got %v", err)
	}

	account, err := NewClient(ts.URL).WithToken("tok_1").GetAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != "free" {
		t.Errorf("unexpected plan %q", account.Plan)
	}
}

func TestClientSurfacesThePaywall(t *testing.T) {
	ts, state := newStubCloud(t)
	ctx := context.Background()
	client := NewClient(ts.URL).WithToken("tok_1")

	if _, err := client.List(ctx); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("expected ErrPaymentRequired on the free plan, got %v", err)
	}

	state.plan = "pro"
	names, err := client.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("expected no blobs yet, got %v", names)
	}
}

func TestClientPushFetchAndStaleConflict(t *testing.T) {
	ts, state := newStubCloud(t)
	state.plan = "pro"
	ctx := context.Background()
	client := NewClient(ts.URL).WithToken("tok_1")

	blob := &RemoteBlob{Name: "github", Version: 1, Envelope: &Envelope{Version: 1, Salt: "c2FsdA=="}}
	if err := client.Push(ctx, blob); err != nil {
		t.Fatal(err)
	}

	fetched, err := client.Fetch(ctx, "github")
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Name != "github" || fetched.Version != 1 {
		t.Errorf("unexpected blob: %+v", fetched)
	}

	// Pushing the same version again must surface as a stale conflict.
	if err := client.Push(ctx, blob); !errors.Is(err, ErrStaleVersion) {
		t.Errorf("expected ErrStaleVersion, got %v", err)
	}

	if _, err := client.Fetch(ctx, "absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for an unknown blob, got %v", err)
	}
}

func TestClientLogout(t *testing.T) {
	ts, state := newStubCloud(t)
	ctx := context.Background()

	if err := NewClient(ts.URL).WithToken("tok_1").Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if !state.loggedOut {
		t.Error("logout should have revoked the session")
	}
}
