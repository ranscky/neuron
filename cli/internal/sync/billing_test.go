package sync

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func checkoutClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return NewClient(ts.URL).WithToken("session-token")
}

func TestCreateCheckoutReturnsTheURL(t *testing.T) {
	var gotAuth, gotMethod, gotPath string
	client := checkoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"https://checkout.stripe.test/cs_1","session_id":"cs_1"}`)
	})

	checkout, err := client.CreateCheckout(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if checkout.URL != "https://checkout.stripe.test/cs_1" {
		t.Errorf("url = %q", checkout.URL)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/billing/checkout" {
		t.Errorf("request was %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer session-token" {
		t.Errorf("auth header = %q", gotAuth)
	}
}

func TestCreateCheckoutMapsServiceErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantErr    error
		wantInText string
	}{
		{name: "expired session", status: http.StatusUnauthorized, body: `{}`, wantErr: ErrUnauthorized},
		{name: "already paying", status: http.StatusConflict, body: `{}`, wantErr: ErrAlreadySubscribed},
		{name: "billing off", status: http.StatusServiceUnavailable, body: `{}`, wantInText: "does not have billing configured"},
		{name: "bad gateway", status: http.StatusBadGateway, body: `{"error":"could not create a checkout session"}`, wantInText: "could not start checkout"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := checkoutClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})

			_, err := client.CreateCheckout(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
				return
			}
			if tc.wantInText != "" && !contains(err.Error(), tc.wantInText) {
				t.Errorf("error %q should mention %q", err, tc.wantInText)
			}
		})
	}
}

func TestCreateCheckoutRejectsAnEmptyURL(t *testing.T) {
	client := checkoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"session_id":"cs_1"}`)
	})

	if _, err := client.CreateCheckout(context.Background()); err == nil {
		t.Fatal("expected an error when the service returns no url")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
