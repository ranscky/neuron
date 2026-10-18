package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/store"
)

// isolateDataDir runs a test inside a temporary working directory, because the
// file store uses relative "data/" paths.
func isolateDataDir(t *testing.T) {
	t.Helper()

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// publishRequest builds the multipart request a client sends to /v1/publish.
func publishRequest(t *testing.T, name, version string) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("manifest", fmt.Sprintf(`{"name":%q,"version":%q}`, name, version)); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("tarball", "package.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("tarball-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/publish", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func withOrg(req *http.Request, name string) *http.Request {
	return req.WithContext(auth.WithOrg(req.Context(), &store.Org{ID: "org_1", Name: name}))
}

func TestPublishRequiresAnAPIKey(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewPublishHandler(fs).ServeHTTP(rec, publishRequest(t, "acme/tool", "1.0.0"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 — publishing must be authenticated", rec.Code)
	}
}

// The core of org-scoped publishing: a key may only write to its own namespace.
func TestPublishRejectsAnotherOrgsNamespace(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewPublishHandler(fs).ServeHTTP(rec, withOrg(publishRequest(t, "other/tool", "1.0.0"), "acme"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rec.Code)
	}

	// Nothing may have been written.
	if _, err := fs.GetManifest("other/tool", "1.0.0"); err == nil {
		t.Error("a rejected publish must not store anything")
	}
}

// Names and versions become file paths, so the HTTP boundary rejects anything
// that could escape the data directory — including within the org's own prefix.
func TestPublishRejectsUnsafeNamesAndVersions(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewPublishHandler(fs)

	cases := []struct{ name, version string }{
		{"acme/../../etc/passwd", "1.0.0"},
		{"acme/..", "1.0.0"},
		{"acme/web search", "1.0.0"},
		{"acme/tool", "../1.0.0"},
		{"acme/tool", "1.0.0/../../x"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, withOrg(publishRequest(t, tc.name, tc.version), "acme"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("publish(%q, %q): got %d, want 400", tc.name, tc.version, rec.Code)
		}
	}
}

func TestPublishAcceptsItsOwnNamespace(t *testing.T) {
	isolateDataDir(t)

	fs, err := store.NewFileStore()
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewPublishHandler(fs).ServeHTTP(rec, withOrg(publishRequest(t, "acme/tool", "1.0.0"), "acme"))

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["name"] != "acme/tool" {
		t.Errorf("response = %v", body)
	}

	manifest, err := fs.GetManifest("acme/tool", "1.0.0")
	if err != nil {
		t.Fatalf("the package should have been stored: %v", err)
	}
	if !bytes.Contains(manifest, []byte("acme/tool")) {
		t.Errorf("stored manifest = %s", manifest)
	}
}
