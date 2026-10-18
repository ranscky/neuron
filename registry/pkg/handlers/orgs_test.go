package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/store"
)

func TestCreateOrgIsDisabledWithoutAnAdminToken(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewOrgHandler(fs, "")

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", bytes.NewReader([]byte(`{"name":"acme"}`)))
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 — an unconfigured registry must not hand out orgs", rec.Code)
	}
}

func TestCreateOrgRejectsABadAdminToken(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewOrgHandler(fs, "s3cret")

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", bytes.NewReader([]byte(`{"name":"acme"}`)))
	req.Header.Set("X-Admin-Token", "guess")
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestCreateOrgIssuesAKeyThatWorks(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewOrgHandler(fs, "s3cret")

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", bytes.NewReader([]byte(`{"name":"acme"}`)))
	req.Header.Set("X-Admin-Token", "s3cret")
	rec := httptest.NewRecorder()
	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Org    map[string]string `json:"org"`
		APIKey string            `json:"api_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Org["name"] != "acme" || body.APIKey == "" {
		t.Fatalf("response = %s", rec.Body.String())
	}

	org, err := fs.OrgForKey(body.APIKey)
	if err != nil {
		t.Fatalf("the issued key must authenticate: %v", err)
	}
	if org.Name != "acme" {
		t.Errorf("key belongs to %q", org.Name)
	}

	// A duplicate name is a conflict, not a second org.
	req = httptest.NewRequest(http.MethodPost, "/v1/orgs", bytes.NewReader([]byte(`{"name":"acme"}`)))
	req.Header.Set("X-Admin-Token", "s3cret")
	rec = httptest.NewRecorder()
	handler.Create(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate org: got %d, want 409", rec.Code)
	}
}

func TestCreateKeyIssuesAnotherKeyForTheAuthenticatedOrg(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	org, first, err := fs.CreateOrg("acme")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewOrgHandler(fs, "s3cret")

	req := httptest.NewRequest(http.MethodPost, "/v1/keys", bytes.NewReader([]byte(`{"label":"ci"}`)))
	req = req.WithContext(auth.WithOrg(req.Context(), org))
	rec := httptest.NewRecorder()
	handler.CreateKey(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.APIKey == "" || body.APIKey == first {
		t.Fatalf("expected a fresh key, got %q", body.APIKey)
	}
	if _, err := fs.OrgForKey(body.APIKey); err != nil {
		t.Fatalf("the new key must authenticate: %v", err)
	}

	// Without an authenticated org there is nothing to issue a key for.
	req = httptest.NewRequest(http.MethodPost, "/v1/keys", bytes.NewReader(nil))
	rec = httptest.NewRecorder()
	handler.CreateKey(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated key creation: got %d, want 401", rec.Code)
	}
}
