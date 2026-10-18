package store

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// inTempDataDir isolates the store's relative "data/" directory.
func inTempDataDir(t *testing.T) {
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

func TestCreateOrgReturnsAWorkingKeyOnce(t *testing.T) {
	inTempDataDir(t)

	fs, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}

	org, key, err := fs.CreateOrg("Acme")
	if err != nil {
		t.Fatal(err)
	}
	if org.Name != "acme" {
		t.Errorf("org names are normalised to lower case, got %q", org.Name)
	}
	if !strings.HasPrefix(key, "nrn_") || len(key) < 20 {
		t.Errorf("unexpected key %q", key)
	}

	// The key resolves to the org...
	resolved, err := fs.OrgForKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != org.ID {
		t.Errorf("key resolved to %q, want %q", resolved.ID, org.ID)
	}

	// ...but the plaintext key is not on disk.
	raw, err := os.ReadFile(orgsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatal("the plaintext API key must never be written to disk")
	}
	if !strings.Contains(string(raw), hashKey(key)) {
		t.Error("the key hash should be stored")
	}
}

func TestCreateOrgRejectsDuplicatesAndBadNames(t *testing.T) {
	inTempDataDir(t)

	fs, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fs.CreateOrg("acme"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := fs.CreateOrg("ACME"); !errors.Is(err, ErrOrgExists) {
		t.Errorf("duplicate org: got %v, want ErrOrgExists", err)
	}
	for _, bad := range []string{"", "a", "has space", "UPPER!", "../etc", strings.Repeat("x", 33)} {
		if _, _, err := fs.CreateOrg(bad); !errors.Is(err, ErrInvalidOrgName) {
			t.Errorf("org name %q: got %v, want ErrInvalidOrgName", bad, err)
		}
	}
}

func TestCreateAPIKeyAddsASecondKeyForTheSameOrg(t *testing.T) {
	inTempDataDir(t)

	fs, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	org, first, err := fs.CreateOrg("acme")
	if err != nil {
		t.Fatal(err)
	}

	second, err := fs.CreateAPIKey(org.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("each key must be distinct")
	}
	for _, key := range []string{first, second} {
		resolved, err := fs.OrgForKey(key)
		if err != nil {
			t.Fatalf("key should resolve: %v", err)
		}
		if resolved.ID != org.ID {
			t.Errorf("resolved to %q", resolved.ID)
		}
	}

	if _, err := fs.CreateAPIKey("org_nope", "x"); !errors.Is(err, ErrNoSuchOrg) {
		t.Errorf("unknown org: got %v", err)
	}
}

func TestOrgForKeyRejectsUnknownKeys(t *testing.T) {
	inTempDataDir(t)

	fs, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.OrgForKey(""); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("empty key: got %v", err)
	}
	if _, err := fs.OrgForKey("nrn_madeup"); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown key: got %v", err)
	}
}

func TestOrgsPersistAcrossStoreInstances(t *testing.T) {
	inTempDataDir(t)

	fs, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	org, key, err := fs.CreateOrg("acme")
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := reopened.OrgForKey(key)
	if err != nil {
		t.Fatalf("a restart must not invalidate keys: %v", err)
	}
	if resolved.Name != org.Name {
		t.Errorf("got %q, want %q", resolved.Name, org.Name)
	}
}

func TestOwns(t *testing.T) {
	cases := []struct {
		org, pkg string
		want     bool
	}{
		{"acme", "acme/web-search", true},
		{"acme", "ACME/Web-Search", true},
		{"acme", "acme/", false},
		{"acme", "acmecorp/tool", false},
		{"acme", "web-search", false},
		{"acme", "other/web-search", false},
	}
	for _, tc := range cases {
		if got := Owns(tc.org, tc.pkg); got != tc.want {
			t.Errorf("Owns(%q, %q) = %v, want %v", tc.org, tc.pkg, got, tc.want)
		}
	}
}
