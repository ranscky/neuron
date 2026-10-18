package sync

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// memoryKeychain is a stand-in for the OS keychain.
type memoryKeychain struct{ values map[string]string }

func newMemoryKeychain() *memoryKeychain {
	return &memoryKeychain{values: map[string]string{}}
}

func (m *memoryKeychain) Get(key string) (string, error) {
	value, ok := m.values[key]
	if !ok {
		return "", os.ErrNotExist
	}
	return value, nil
}

func (m *memoryKeychain) Set(key, value string) error {
	m.values[key] = value
	return nil
}

func TestWrapUnwrapTeamKey(t *testing.T) {
	alice, err := NewMemberKey()
	if err != nil {
		t.Fatal(err)
	}
	bob, err := NewMemberKey()
	if err != nil {
		t.Fatal(err)
	}
	teamKey, err := NewTeamKey()
	if err != nil {
		t.Fatal(err)
	}

	wrapped, err := WrapTeamKey(bob.PublicKeyBase64(), teamKey, "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wrapped, base64.StdEncoding.EncodeToString(teamKey)) {
		t.Fatal("the wrapped key must not embed the team key")
	}

	got, err := bob.UnwrapTeamKey(wrapped, "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(teamKey) {
		t.Error("unwrapped key does not match the team key")
	}

	// Only the intended recipient can open it.
	if _, err := alice.UnwrapTeamKey(wrapped, "team_1"); err == nil {
		t.Error("a different device must not be able to unwrap the key")
	}
	// ...and only for the team it was wrapped for.
	if _, err := bob.UnwrapTeamKey(wrapped, "team_2"); err == nil {
		t.Error("a wrapped key must not open under another team id")
	}
}

func TestWrappedTeamKeyRejectsTampering(t *testing.T) {
	bob, err := NewMemberKey()
	if err != nil {
		t.Fatal(err)
	}
	teamKey, err := NewTeamKey()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapTeamKey(bob.PublicKeyBase64(), teamKey, "team_1")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, err := bob.UnwrapTeamKey(base64.StdEncoding.EncodeToString(raw), "team_1"); err == nil {
		t.Fatal("a tampered wrapped key must be rejected")
	}

	if _, err := bob.UnwrapTeamKey("not base64!!", "team_1"); err == nil {
		t.Error("garbage must be rejected")
	}
	if _, err := bob.UnwrapTeamKey("AAAA", "team_1"); err == nil {
		t.Error("a short wrapped key must be rejected")
	}
}

func TestMemberKeyRoundTrip(t *testing.T) {
	key, err := NewMemberKey()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := MemberKeyFromBase64(key.PrivateKeyBase64())
	if err != nil {
		t.Fatal(err)
	}
	if restored.PublicKeyBase64() != key.PublicKeyBase64() {
		t.Error("restored key has a different public half")
	}

	if _, err := MemberKeyFromBase64("nope"); err == nil {
		t.Error("invalid base64 must be rejected")
	}
	if _, err := MemberKeyFromBase64(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("a short key must be rejected")
	}
}

func TestLoadOrCreateMemberKey(t *testing.T) {
	kv := newMemoryKeychain()

	first, err := LoadOrCreateMemberKey(kv)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := kv.values[MemberKeyEntry]; !ok {
		t.Error("the key should have been stored")
	}

	second, err := LoadOrCreateMemberKey(kv)
	if err != nil {
		t.Fatal(err)
	}
	if second.PublicKeyBase64() != first.PublicKeyBase64() {
		t.Error("a second call must reuse the stored key")
	}

	// The environment wins, which is how CI and containers get a stable identity.
	override, err := NewMemberKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(MemberKeyEnv, override.PrivateKeyBase64())
	fromEnv, err := LoadOrCreateMemberKey(kv)
	if err != nil {
		t.Fatal(err)
	}
	if fromEnv.PublicKeyBase64() != override.PublicKeyBase64() {
		t.Error("the environment should override the keychain")
	}
}

func TestTeamKeyStorage(t *testing.T) {
	kv := newMemoryKeychain()
	teamKey, err := NewTeamKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveTeamKey(kv, "team_1", teamKey); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTeamKey(kv, "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(teamKey) {
		t.Error("stored team key does not round trip")
	}

	if _, err := LoadTeamKey(kv, "team_missing"); err == nil {
		t.Error("a missing team key must be an error")
	}
	if err := SaveTeamKey(nil, "team_1", teamKey); err == nil {
		t.Error("saving without a keychain must fail")
	}
}

// A team key is not derived from a passphrase, so its envelopes are v3 ("raw"):
// there is no salt or KDF for another machine to reproduce.
func TestRawTeamKeySealsV3(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	key, err := KeyFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if key.KDF() != KDFRaw {
		t.Errorf("kdf = %q", key.KDF())
	}

	aad := TeamAAD("team_1", "github", 3)
	env, err := key.Seal([]byte("shared-secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if env.Version != envelopeV3 {
		t.Errorf("version = %d, want %d", env.Version, envelopeV3)
	}
	if env.KDF != KDFRaw || env.Salt != "" || env.Iter != 0 {
		t.Errorf("a raw envelope must carry no derivation parameters: %+v", env)
	}

	got, err := key.Open(env, aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "shared-secret" {
		t.Errorf("got %q", got)
	}

	// A passphrase-derived key cannot open it, and the passphrase path says why.
	derived := testKey(t, "passphrase")
	if _, err := derived.Open(env, aad); err == nil {
		t.Error("a derived key must not open a raw envelope")
	}
	if _, err := OpenWithPassphrase("passphrase", env, aad); err == nil {
		t.Error("OpenWithPassphrase must refuse a team-key envelope")
	} else if !strings.Contains(err.Error(), "team key") {
		t.Errorf("the error should explain the envelope needs a team key, got: %v", err)
	}

	if _, err := KeyFromRaw(raw[:16]); err == nil {
		t.Error("a short raw key must be rejected")
	}
}
