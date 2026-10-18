package sync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Team keys are wrapped per member device instead of being derived from a shared
// passphrase.
//
// Each device has an X25519 keypair. The team key is 32 random bytes that never
// leave the clients; it is sealed to each member's public key with an ephemeral
// ECDH exchange (X25519 → HKDF-SHA256 → AES-256-GCM). The service stores only
// the sealed blobs, so it cannot read a shared server, and removing a member is a
// matter of re-wrapping under a new team key rather than changing a passphrase
// everybody shares.

const (
	// MemberKeyEntry is the keychain entry holding this device's X25519 private
	// key. The environment can supply it instead, for headless machines.
	MemberKeyEntry = "neuron-member-key"
	// MemberKeyEnv overrides the keychain for CI and containers.
	MemberKeyEnv = "NEURON_MEMBER_KEY"

	teamKeyLen   = 32
	memberKeyLen = 32
)

// KeyValue is the slice of the OS keychain this file needs.
type KeyValue interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

// MemberKey is this device's X25519 identity.
type MemberKey struct {
	private *ecdh.PrivateKey
}

// NewMemberKey generates a fresh device keypair.
func NewMemberKey() (*MemberKey, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate member key: %w", err)
	}
	return &MemberKey{private: private}, nil
}

// PublicKeyBase64 returns the public half, ready to register with a service.
//
// It is URL-safe base64 (no padding, no '+', no '/'), because a public key is
// addressed in a path — `/v1/teams/{id}/keys/{public_key}` — and a standard
// encoding would split the segment.
func (k *MemberKey) PublicKeyBase64() string {
	return base64.RawURLEncoding.EncodeToString(k.private.PublicKey().Bytes())
}

// decodeKeyBytes accepts any base64 variant a key might arrive in: URL-safe or
// standard, padded or not. Keys written by this package are always URL-safe.
func decodeKeyBytes(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if raw, err := encoding.DecodeString(trimmed); err == nil {
			return raw, nil
		}
	}
	return nil, errors.New("not base64")
}

// PrivateKeyBase64 returns the private half for storage in the keychain.
func (k *MemberKey) PrivateKeyBase64() string {
	return base64.StdEncoding.EncodeToString(k.private.Bytes())
}

// MemberKeyFromBase64 restores a device keypair.
func MemberKeyFromBase64(value string) (*MemberKey, error) {
	raw, err := decodeKeyBytes(value)
	if err != nil {
		return nil, fmt.Errorf("member key is not base64: %w", err)
	}
	if len(raw) != memberKeyLen {
		return nil, fmt.Errorf("member key must be %d bytes, got %d", memberKeyLen, len(raw))
	}
	private, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("member key is not a valid X25519 key: %w", err)
	}
	return &MemberKey{private: private}, nil
}

// LoadOrCreateMemberKey returns this device's keypair, creating and storing one
// when absent. NEURON_MEMBER_KEY wins over the keychain so headless machines and
// CI can supply a stable identity.
func LoadOrCreateMemberKey(kv KeyValue) (*MemberKey, error) {
	if fromEnv := os.Getenv(MemberKeyEnv); fromEnv != "" {
		return MemberKeyFromBase64(fromEnv)
	}

	if kv != nil {
		if stored, err := kv.Get(MemberKeyEntry); err == nil && stored != "" {
			return MemberKeyFromBase64(stored)
		}
	}

	key, err := NewMemberKey()
	if err != nil {
		return nil, err
	}
	if kv != nil {
		if err := kv.Set(MemberKeyEntry, key.PrivateKeyBase64()); err != nil {
			return nil, fmt.Errorf("store the member key: %w", err)
		}
	}
	return key, nil
}

// NewTeamKey returns a fresh random team key.
func NewTeamKey() ([]byte, error) {
	key := make([]byte, teamKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate team key: %w", err)
	}
	return key, nil
}

// TeamKeyEntry is the keychain entry holding a team key on this device.
func TeamKeyEntry(teamID string) string { return "neuron-team-key:" + teamID }

// SaveTeamKey stores a team key in this device's keychain.
func SaveTeamKey(kv KeyValue, teamID string, teamKey []byte) error {
	if kv == nil {
		return errors.New("no keychain available to store the team key")
	}
	return kv.Set(TeamKeyEntry(teamID), base64.StdEncoding.EncodeToString(teamKey))
}

// LoadTeamKey reads this device's team key.
func LoadTeamKey(kv KeyValue, teamID string) ([]byte, error) {
	if kv == nil {
		return nil, errors.New("no keychain available to read the team key")
	}
	stored, err := kv.Get(TeamKeyEntry(teamID))
	if err != nil {
		return nil, fmt.Errorf("this device has no team key for %s: %w", teamID, err)
	}
	key, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return nil, fmt.Errorf("team key is not base64: %w", err)
	}
	if len(key) != teamKeyLen {
		return nil, fmt.Errorf("team key must be %d bytes, got %d", teamKeyLen, len(key))
	}
	return key, nil
}

// WrapTeamKey seals the team key to a member's public key.
//
// The output is ephemeralPublicKey || nonce || ciphertext: the recipient needs no
// record of who wrapped it, and the sealed blob is bound to the team it belongs
// to.
func WrapTeamKey(recipientPublicKeyBase64 string, teamKey []byte, teamID string) (string, error) {
	if len(teamKey) != teamKeyLen {
		return "", fmt.Errorf("team key must be %d bytes, got %d", teamKeyLen, len(teamKey))
	}

	recipientBytes, err := decodeKeyBytes(recipientPublicKeyBase64)
	if err != nil {
		return "", fmt.Errorf("recipient public key is not base64: %w", err)
	}
	recipient, err := ecdh.X25519().NewPublicKey(recipientBytes)
	if err != nil {
		return "", fmt.Errorf("recipient public key is not a valid X25519 key: %w", err)
	}

	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return "", fmt.Errorf("derive the wrapping secret: %w", err)
	}

	aead, err := teamKeyAEAD(shared, teamID, recipientBytes)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := aead.Seal(nil, nonce, teamKey, teamKeyAAD(teamID, recipientBytes))
	out := make([]byte, 0, len(ephemeral.PublicKey().Bytes())+len(nonce)+len(sealed))
	out = append(out, ephemeral.PublicKey().Bytes()...)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// UnwrapTeamKey opens a wrapped team key with this device's private key.
func (k *MemberKey) UnwrapTeamKey(wrappedBase64, teamID string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(wrappedBase64))
	if err != nil {
		return nil, fmt.Errorf("wrapped key is not base64: %w", err)
	}

	ephemeralLen := 32
	if len(raw) < ephemeralLen+12 {
		return nil, errors.New("wrapped key is too short")
	}
	ephemeralPub := raw[:ephemeralLen]
	nonce := raw[ephemeralLen : ephemeralLen+12]
	ciphertext := raw[ephemeralLen+12:]

	ephemeral, err := ecdh.X25519().NewPublicKey(ephemeralPub)
	if err != nil {
		return nil, fmt.Errorf("wrapped key has an invalid ephemeral key: %w", err)
	}
	shared, err := k.private.ECDH(ephemeral)
	if err != nil {
		return nil, fmt.Errorf("derive the unwrapping secret: %w", err)
	}

	ownPublic := k.private.PublicKey().Bytes()
	aead, err := teamKeyAEAD(shared, teamID, ownPublic)
	if err != nil {
		return nil, err
	}

	teamKey, err := aead.Open(nil, nonce, ciphertext, teamKeyAAD(teamID, ownPublic))
	if err != nil {
		return nil, errors.New("could not unwrap the team key: it was not wrapped for this device, or the team does not match")
	}
	if len(teamKey) != teamKeyLen {
		return nil, errors.New("unwrapped team key has the wrong length")
	}
	return teamKey, nil
}

// teamKeyAEAD derives the wrapping key. The salt binds the key to the team, and
// the recipient key is mixed in so a sealed blob cannot be re-targeted.
func teamKeyAEAD(sharedSecret []byte, teamID string, recipient []byte) (cipher.AEAD, error) {
	salt := append([]byte(teamID+"\x00"), recipient...)
	key, err := hkdf.Key(sha256.New, sharedSecret, salt, "neuron-team-key-v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive the wrapping key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create the wrapping cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create the wrapping AEAD: %w", err)
	}
	return aead, nil
}

func teamKeyAAD(teamID string, recipient []byte) []byte {
	return append(append([]byte(teamID+"\x00"), recipient...), []byte("\x00team-key")...)
}
