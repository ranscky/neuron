package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ranscky/neuron-registry/pkg/store"
)

type fakeResolver struct {
	orgs map[string]*store.Org
}

func (f fakeResolver) OrgForKey(key string) (*store.Org, error) {
	if org, ok := f.orgs[key]; ok {
		return org, nil
	}
	return nil, store.ErrUnknownKey
}

func TestRequireOrgRejectsMissingOrBadKeys(t *testing.T) {
	resolver := fakeResolver{orgs: map[string]*store.Org{"good": {ID: "org_1", Name: "acme"}}}
	handler := RequireOrg(resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the handler must not run without a valid key")
	}))

	for _, header := range []string{"", "Bearer ", "Bearer nope", "Basic good"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/publish", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("header %q: got %d, want 401", header, rec.Code)
		}
	}
}

func TestRequireOrgPassesTheOrgToTheHandler(t *testing.T) {
	resolver := fakeResolver{orgs: map[string]*store.Org{"good": {ID: "org_1", Name: "acme"}}}

	var got *store.Org
	handler := RequireOrg(resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := OrgFrom(r.Context())
		if !ok {
			t.Error("the org should be in the context")
		}
		got = org
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/publish", nil)
	req.Header.Set("Authorization", "bearer good")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got == nil || got.Name != "acme" {
		t.Errorf("handler saw %+v", got)
	}
}
