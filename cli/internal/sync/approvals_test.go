package sync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestActivateURL(t *testing.T) {
	cases := []struct{ server, uri, want string }{
		{"http://127.0.0.1:8080", "/activate", "http://127.0.0.1:8080/activate"},
		{"http://127.0.0.1:8080/", "/activate", "http://127.0.0.1:8080/activate"},
		{"https://sync.example.com", "", "https://sync.example.com/activate"},
		{"https://sync.example.com", "https://elsewhere.test/activate", "https://elsewhere.test/activate"},
	}
	for _, tc := range cases {
		if got := ActivateURL(tc.server, tc.uri); got != tc.want {
			t.Errorf("ActivateURL(%q, %q) = %q, want %q", tc.server, tc.uri, got, tc.want)
		}
	}
}

func TestCreateApprovalLink(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"https://sync.test/activate?token=abc","approval_token":"abc"}`)
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL).WithToken("session-token")
	link, err := client.CreateApprovalLink(context.Background(), "K7QP-3M2X")
	if err != nil {
		t.Fatal(err)
	}
	if link.URL != "https://sync.test/activate?token=abc" || link.Token != "abc" {
		t.Errorf("unexpected link %+v", link)
	}
	if gotPath != "/v1/device/approvals" {
		t.Errorf("posted to %q", gotPath)
	}
	if gotAuth != "Bearer session-token" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotBody == "" || !contains(gotBody, "K7QP-3M2X") {
		t.Errorf("body should carry the user code, got %q", gotBody)
	}
}

func TestCreateApprovalLinkRejectsAnEmptyURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(ts.Close)

	if _, err := NewClient(ts.URL).WithToken("t").CreateApprovalLink(context.Background(), "CODE"); err == nil {
		t.Fatal("expected an error when the service returns no url")
	}
}
