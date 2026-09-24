package sync

import (
	"bytes"
	"testing"
)

// testIterations keeps the KDF cheap in tests; production uses DefaultIterations.
const testIterations = 1000

func testKey(t *testing.T, passphrase string) *Key {
	t.Helper()
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key, err := DeriveKey(passphrase, salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := testKey(t, "correct horse battery staple")
	secret := []byte(`{"env":{"GITHUB_TOKEN":"ghp_realsecret"}}`)
	aad := AAD("github", "3")

	env, err := key.Seal(secret, aad)
	if err != nil {
		t.Fatal(err)
	}

	got, err := key.Open(env, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("round trip mismatch: got %q want %q", got, secret)
	}
}

func TestEnvelopeLeaksNoPlaintext(t *testing.T) {
	key := testKey(t, "passphrase")
	plaintext := []byte(`GITHUB_TOKEN=ghp_realsecret`)

	env, err := key.Seal(plaintext, AAD("github", "3"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(raw, []byte("ghp_realsecret")) {
		t.Fatalf("plaintext value leaked into the envelope: %s", raw)
	}
	if bytes.Contains(raw, []byte("GITHUB_TOKEN")) {
		t.Fatalf("plaintext key name leaked into the envelope: %s", raw)
	}
}

func TestOpenWithWrongPassphraseFails(t *testing.T) {
	key := testKey(t, "right-passphrase")
	env, err := key.Seal([]byte("secret"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := OpenWithPassphrase("wrong-passphrase", env, AAD("github", "1")); err == nil {
		t.Fatal("expected decryption to fail with a wrong passphrase")
	}
}

func TestOpenWithCorrectPassphraseSucceeds(t *testing.T) {
	key := testKey(t, "right-passphrase")
	env, err := key.Seal([]byte("secret"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := OpenWithPassphrase("right-passphrase", env, AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Errorf("got %q", got)
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	key := testKey(t, "passphrase")
	env, err := key.Seal([]byte("secret"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}

	flipped := []byte(env.Data)
	if flipped[0] == 'A' {
		flipped[0] = 'B'
	} else {
		flipped[0] = 'A'
	}
	env.Data = string(flipped)

	if _, err := key.Open(env, AAD("github", "1")); err == nil {
		t.Fatal("expected tampered ciphertext to be rejected")
	}
}

// This is the attack AAD exists to stop: a compromised server swaps one
// server's blob in place of another's, and the client would otherwise decrypt
// it happily.
func TestBlobCannotBeReplayedUnderAnotherServerName(t *testing.T) {
	key := testKey(t, "passphrase")
	env, err := key.Seal([]byte("github-only-secret"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := key.Open(env, AAD("filesystem", "1")); err == nil {
		t.Fatal("expected a blob bound to github to be rejected under filesystem")
	}
}

func TestBlobCannotBeReplayedUnderAnotherVersion(t *testing.T) {
	key := testKey(t, "passphrase")
	env, err := key.Seal([]byte("v1-secret"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := key.Open(env, AAD("github", "2")); err == nil {
		t.Fatal("expected a blob bound to version 1 to be rejected as version 2")
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	key := testKey(t, "passphrase")
	aad := AAD("github", "1")

	first, err := key.Seal([]byte("same"), aad)
	if err != nil {
		t.Fatal(err)
	}
	second, err := key.Seal([]byte("same"), aad)
	if err != nil {
		t.Fatal(err)
	}

	if first.Nonce == second.Nonce || first.Data == second.Data {
		t.Error("sealing the same plaintext twice must not produce identical output")
	}
}

func TestDeriveKeyIsDeterministicForSameSalt(t *testing.T) {
	salt := []byte("0123456789abcdef")

	a, err := DeriveKey("passphrase", salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeriveKey("passphrase", salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.key, b.key) {
		t.Error("same passphrase and salt must derive the same key")
	}

	c, err := DeriveKey("passphrase", []byte("fedcba9876543210"), testIterations)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.key, c.key) {
		t.Error("different salts must derive different keys")
	}
}

func TestDeriveKeyRejectsEmptyInputs(t *testing.T) {
	if _, err := DeriveKey("", []byte("salt"), testIterations); err == nil {
		t.Error("expected an error for an empty passphrase")
	}
	if _, err := DeriveKey("passphrase", nil, testIterations); err == nil {
		t.Error("expected an error for an empty salt")
	}
}

func TestParseEnvelopeRejectsGarbage(t *testing.T) {
	if _, err := ParseEnvelope([]byte("not json")); err == nil {
		t.Error("expected a parse error for non-JSON input")
	}
}

func TestEnvelopeCarriesKDFParameters(t *testing.T) {
	key := testKey(t, "passphrase")
	env, err := key.Seal([]byte("x"), AAD("s", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Salt == "" || env.Iter != testIterations || env.Version != envelopeVersion {
		t.Errorf("envelope must be self-describing, got %+v", env)
	}
}
