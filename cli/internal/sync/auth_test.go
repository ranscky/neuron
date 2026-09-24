package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := LoadAuth(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound before signing in, got %v", err)
	}

	auth := &Auth{
		BaseURL:    "http://127.0.0.1:8080",
		AccountID:  "acct_1",
		Salt:       "c2FsdA==",
		Plan:       "pro",
		SignedInAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := SaveAuth(auth); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccountID != "acct_1" || loaded.BaseURL != auth.BaseURL || loaded.Plan != "pro" {
		t.Errorf("unexpected auth: %+v", loaded)
	}
	if !loaded.SignedInAt.Equal(auth.SignedInAt) {
		t.Errorf("sign-in time did not survive the round trip: %v", loaded.SignedInAt)
	}

	salt, err := loaded.SaltBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(salt) != "salt" {
		t.Errorf("unexpected salt %q", salt)
	}

	if err := ClearAuth(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAuth(); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after clearing, got %v", err)
	}
}

func TestAuthFileIsNotWorldReadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SaveAuth(&Auth{BaseURL: "x", AccountID: "y", Salt: "c2FsdA=="}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(home, ".neuron", "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the auth file should be 0600, got %o", perm)
	}
}

func TestSaltBytesRejectsGarbage(t *testing.T) {
	if _, err := (&Auth{Salt: "not base64!!"}).SaltBytes(); err == nil {
		t.Error("expected an error for a malformed salt")
	}
	if _, err := (&Auth{Salt: ""}).SaltBytes(); err == nil {
		t.Error("expected an error for an empty salt")
	}
}
