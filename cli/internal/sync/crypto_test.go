package sync

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Cheap Argon2id parameters keep the suite fast; production uses the RFC 9106
// defaults. testIterations does the same for the v1 PBKDF2 path.
const (
	testIterations = 1000
	testTime       = 1
	testMemory     = 8 * 1024
	testThreads    = 1
)

func testKey(t *testing.T, passphrase string) *Key {
	t.Helper()
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	return testKeyWithSalt(t, passphrase, salt)
}

func testKeyWithSalt(t *testing.T, passphrase string, salt []byte) *Key {
	t.Helper()
	key, err := testKeyFor(passphrase, salt)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// testKeyFor is the error-returning form, for helpers that are not *testing.T.
func testKeyFor(passphrase string, salt []byte) (*Key, error) {
	return DeriveKeyParams(passphrase, Params{
		KDF:     KDFArgon2id,
		Salt:    salt,
		Time:    testTime,
		Memory:  testMemory,
		Threads: testThreads,
	})
}

// v1Key derives a key the way the pre-Argon2id code did.
func v1Key(t *testing.T, passphrase string, salt []byte) *Key {
	t.Helper()
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

	a, err := testKeyFor("passphrase", salt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := testKeyFor("passphrase", salt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.key, b.key) {
		t.Error("same passphrase and salt must derive the same key")
	}

	c, err := testKeyFor("passphrase", []byte("fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.key, c.key) {
		t.Error("different salts must derive different keys")
	}
}

func TestDeriveKeyRejectsEmptyInputs(t *testing.T) {
	if _, err := testKeyFor("", []byte("salt")); err == nil {
		t.Error("expected an error for an empty passphrase")
	}
	if _, err := testKeyFor("passphrase", nil); err == nil {
		t.Error("expected an error for an empty salt")
	}
}

func TestDeriveKeyRejectsUnknownKDF(t *testing.T) {
	if _, err := DeriveKeyParams("passphrase", Params{KDF: "scrypt", Salt: []byte("salt")}); err == nil {
		t.Error("expected an error for an unknown kdf")
	}
}

func TestParseEnvelopeRejectsGarbage(t *testing.T) {
	if _, err := ParseEnvelope([]byte("not json")); err == nil {
		t.Error("expected a parse error for non-JSON input")
	}
}

func TestNewKeyUsesArgon2id(t *testing.T) {
	key, err := NewKey("passphrase", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if key.KDF() != KDFArgon2id {
		t.Fatalf("NewKey must derive with %s, got %s", KDFArgon2id, key.KDF())
	}

	env, err := key.Seal([]byte("x"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Version != envelopeV2 {
		t.Errorf("new payloads must be sealed as v%d, got v%d", envelopeV2, env.Version)
	}
	// RFC 9106 defaults, not the cheap test parameters.
	if env.Time != DefaultArgon2Time || env.Memory != DefaultArgon2Memory || env.Threads != DefaultArgon2Threads {
		t.Errorf("unexpected default argon2id parameters: t=%d m=%d p=%d", env.Time, env.Memory, env.Threads)
	}
	if env.KDF != KDFArgon2id {
		t.Errorf("envelope must name its kdf, got %q", env.KDF)
	}
}

func TestEnvelopeCarriesKDFParameters(t *testing.T) {
	key := testKey(t, "passphrase")
	env, err := key.Seal([]byte("x"), AAD("s", "1"))
	if err != nil {
		t.Fatal(err)
	}
	if env.Salt == "" || env.Version != envelopeV2 || env.KDF != KDFArgon2id {
		t.Fatalf("envelope must be self-describing, got %+v", env)
	}
	if env.Time != testTime || env.Memory != testMemory || env.Threads != testThreads {
		t.Errorf("envelope must record the derivation parameters, got t=%d m=%d p=%d",
			env.Time, env.Memory, env.Threads)
	}
}

// An account synced before this change holds v1 (PBKDF2) blobs. They must keep
// decrypting: an upgrade may not strand a user's data.
func TestV1EnvelopeStillOpensWithPassphrase(t *testing.T) {
	salt := []byte("0123456789abcdef")
	key := v1Key(t, "legacy-passphrase", salt)
	aad := AAD("github", "1")

	env, err := key.Seal([]byte("legacy-secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if env.Version != envelopeV1 {
		t.Fatalf("a PBKDF2 key must seal as v1, got v%d", env.Version)
	}

	got, err := OpenWithPassphrase("legacy-passphrase", env, aad)
	if err != nil {
		t.Fatalf("v1 envelope must still decrypt: %v", err)
	}
	if string(got) != "legacy-secret" {
		t.Errorf("got %q", got)
	}

	if _, err := OpenWithPassphrase("wrong-passphrase", env, aad); err == nil {
		t.Error("v1 envelopes must still reject a wrong passphrase")
	}
}

// The v1 wire format predates the kdf field. Keeping it byte-identical means an
// older client can still read what a newer one writes when a key is
// deliberately derived with PBKDF2.
func TestV1EnvelopeFormatIsUnchanged(t *testing.T) {
	key := v1Key(t, "passphrase", []byte("0123456789abcdef"))
	env, err := key.Seal([]byte("x"), AAD("github", "1"))
	if err != nil {
		t.Fatal(err)
	}

	raw, err := env.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, present := fields["kdf"]; present {
		t.Errorf("v1 envelope must not carry a kdf field: %s", raw)
	}
	if fields["iter"] != float64(testIterations) {
		t.Errorf("v1 envelope must carry its iteration count: %s", raw)
	}
	for _, absent := range []string{"t", "m", "p"} {
		if _, present := fields[absent]; present {
			t.Errorf("v1 envelope must not carry %q: %s", absent, raw)
		}
	}
}

func TestKeyKDFMustMatchEnvelopeVersion(t *testing.T) {
	salt := []byte("0123456789abcdef")
	aad := AAD("github", "1")

	v2Env, err := testKeyWithSalt(t, "passphrase", salt).Seal([]byte("x"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1Key(t, "passphrase", salt).Open(v2Env, aad); err == nil {
		t.Error("a PBKDF2 key must refuse a v2 envelope")
	} else if !strings.Contains(err.Error(), "v2") {
		t.Errorf("error should name the version mismatch, got: %v", err)
	}

	v1Env, err := v1Key(t, "passphrase", salt).Seal([]byte("x"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testKeyWithSalt(t, "passphrase", salt).Open(v1Env, aad); err == nil {
		t.Error("an Argon2id key must refuse a v1 envelope")
	} else if !strings.Contains(err.Error(), "v1") {
		t.Errorf("error should name the version mismatch, got: %v", err)
	}
}

func TestOpenWithPassphraseRejectsUnsupportedVersions(t *testing.T) {
	env := &Envelope{Version: 99, Salt: "AAAA", Nonce: "AAAA", Data: "AAAA"}
	if _, err := OpenWithPassphrase("passphrase", env, AAD("github", "1")); err == nil {
		t.Fatal("expected an unsupported envelope version error")
	}
}

func TestOpenWithPassphraseRejectsUnknownKDFInV2(t *testing.T) {
	env := &Envelope{Version: envelopeV2, KDF: "md5", Salt: "AAAA", Time: 1, Memory: testMemory, Threads: 1}
	if _, err := OpenWithPassphrase("passphrase", env, AAD("github", "1")); err == nil {
		t.Fatal("expected an unsupported kdf error")
	}
}

// The envelope is attacker-controlled: anyone who can write to the sync service
// could ask for a 4 GiB derivation. That must fail cleanly, not OOM the client.
func TestHostileDerivationParametersAreRejected(t *testing.T) {
	cases := []struct {
		name   string
		params Params
	}{
		{"argon2 memory too large", Params{KDF: KDFArgon2id, Salt: []byte("salt"), Memory: 4 * 1024 * 1024}},
		{"argon2 memory too small", Params{KDF: KDFArgon2id, Salt: []byte("salt"), Memory: 1024}},
		{"argon2 time too large", Params{KDF: KDFArgon2id, Salt: []byte("salt"), Time: 5000}},
		{"argon2 threads too many", Params{KDF: KDFArgon2id, Salt: []byte("salt"), Threads: 4096}},
		{"argon2 memory below 8 KiB per lane", Params{KDF: KDFArgon2id, Salt: []byte("salt"), Memory: 32, Threads: 8}},
		{"pbkdf2 iterations too large", Params{KDF: KDFPBKDF2, Salt: []byte("salt"), Iter: 100_000_000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DeriveKeyParams("passphrase", tc.params); err == nil {
				t.Error("expected the parameters to be rejected")
			}
		})
	}
}

// The same guard has to hold when the parameters arrive inside an envelope.
func TestHostileEnvelopeParametersAreRejected(t *testing.T) {
	env := &Envelope{
		Version: envelopeV2,
		KDF:     KDFArgon2id,
		Salt:    "MDEyMzQ1Njc4OWFiY2RlZg==",
		Memory:  4 * 1024 * 1024,
		Time:    1,
		Threads: 1,
		Nonce:   "AAAA",
		Data:    "AAAA",
	}
	if _, err := OpenWithPassphrase("passphrase", env, AAD("github", "1")); err == nil {
		t.Fatal("expected hostile envelope parameters to be rejected")
	}
}

func TestPBKDF2IterationsDefaultWhenZero(t *testing.T) {
	key, err := DeriveKey("passphrase", []byte("0123456789abcdef"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if key.Iterations() != DefaultIterations {
		t.Errorf("got %d iterations, want %d", key.Iterations(), DefaultIterations)
	}
}

func TestArgon2idDefaultsWhenZero(t *testing.T) {
	env, err := func() (*Envelope, error) {
		key, err := DeriveKeyParams("passphrase", Params{Salt: []byte("0123456789abcdef")})
		if err != nil {
			return nil, err
		}
		return key.Seal([]byte("x"), AAD("github", "1"))
	}()
	if err != nil {
		t.Fatal(err)
	}
	if env.Time != DefaultArgon2Time || env.Memory != DefaultArgon2Memory || env.Threads != DefaultArgon2Threads {
		t.Errorf("zero params must select the defaults, got t=%d m=%d p=%d", env.Time, env.Memory, env.Threads)
	}
}
