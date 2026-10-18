// Package sync implements Neuron's end-to-end encrypted sync of MCP server
// definitions and their secrets.
//
// The server only ever stores ciphertext. Keys are derived from a user
// passphrase on the client, and every blob is bound to the server name and
// version it belongs to, so a compromised or malicious server cannot swap one
// server's payload in place of another's.
package sync

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/crypto/argon2"
)

// Envelope versions. New payloads are always sealed with the current version;
// older versions stay readable forever, so upgrading the CLI never strands a
// synced account.
//
//	v1 — PBKDF2-HMAC-SHA256, parameters: salt, iter
//	v2 — Argon2id, parameters: salt, t, m, p
const (
	envelopeV1      = 1
	envelopeV2      = 2
	envelopeV3      = 3
	envelopeVersion = envelopeV2
)

// KDF names as they appear in an envelope. A v1 envelope predates the kdf
// field and is always PBKDF2; "raw" marks a key that was not derived from a
// passphrase at all (a team key unwrapped from a member's key record).
const (
	KDFArgon2id = "argon2id"
	KDFPBKDF2   = "pbkdf2-sha256"
	KDFRaw      = "raw"
)

const (
	// DefaultIterations follows OWASP guidance for PBKDF2-HMAC-SHA256. It only
	// applies to v1 envelopes, which are no longer written.
	DefaultIterations = 600_000

	// Argon2id defaults follow RFC 9106's second recommended configuration
	// (t=3, m=64 MiB, p=4). Memory hardness is what PBKDF2 cannot provide.
	DefaultArgon2Time    = 3
	DefaultArgon2Memory  = 64 * 1024 // KiB
	DefaultArgon2Threads = 4

	saltLen = 16
	keyLen  = 32
)

// Bounds on derivation parameters. A blob's envelope is attacker-controlled —
// anyone who can write to the sync service can hand the client a header asking
// for 4 GiB of memory. These caps turn that into an error instead of an OOM.
const (
	minArgon2Memory  = 8 * 1024    // 8 MiB, low enough for tests
	maxArgon2Memory  = 1024 * 1024 // 1 GiB
	maxArgon2Time    = 16          // passes
	maxArgon2Threads = 32          // lanes
	maxPBKDF2Iter    = 10_000_000  // iterations
)

// Envelope is the self-describing ciphertext the server stores. No field here
// is secret: the salt and KDF parameters only need to be reproduced on another
// machine to re-derive the key from the passphrase.
type Envelope struct {
	Version int    `json:"v"`
	KDF     string `json:"kdf,omitempty"`
	Salt    string `json:"salt"`
	Iter    int    `json:"iter,omitempty"`
	Time    int    `json:"t,omitempty"`
	Memory  int    `json:"m,omitempty"`
	Threads int    `json:"p,omitempty"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

// Params describes a key-derivation configuration. Zero values select the
// defaults for the chosen KDF.
type Params struct {
	KDF     string
	Salt    []byte
	Iter    int
	Time    int
	Memory  int
	Threads int
}

// Key is a derived symmetric key held in memory for a sync session.
type Key struct {
	key     []byte
	salt    []byte
	kdf     string
	iter    int
	time    int
	memory  int
	threads int
}

// KeyFromRaw wraps an existing 32-byte key — a team key unwrapped from a member's
// record — so it can seal and open payloads without a passphrase. Such payloads
// are written as envelope v3 ("raw"): there is no salt or KDF to reproduce, by
// design.
func KeyFromRaw(raw []byte) (*Key, error) {
	if len(raw) != keyLen {
		return nil, fmt.Errorf("raw key must be %d bytes, got %d", keyLen, len(raw))
	}
	key := make([]byte, keyLen)
	copy(key, raw)
	return &Key{key: key, kdf: KDFRaw}, nil
}

// NewSalt returns a fresh random salt for a new sync account.
func NewSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

// NewKey derives a key with the current default KDF (Argon2id) at its default
// work factor. Use it for anything new; DeriveKey exists for v1 compatibility.
func NewKey(passphrase string, salt []byte) (*Key, error) {
	return DeriveKeyParams(passphrase, Params{KDF: KDFArgon2id, Salt: salt})
}

// DeriveKey derives a PBKDF2-HMAC-SHA256 key — the v1 KDF. A non-positive
// iteration count selects DefaultIterations. Kept so v1 envelopes can still be
// reproduced and tested; new payloads should use NewKey.
func DeriveKey(passphrase string, salt []byte, iterations int) (*Key, error) {
	return DeriveKeyParams(passphrase, Params{KDF: KDFPBKDF2, Salt: salt, Iter: iterations})
}

// DeriveKeyParams derives a key with an explicit configuration.
func DeriveKeyParams(passphrase string, p Params) (*Key, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase must not be empty")
	}
	if len(p.Salt) == 0 {
		return nil, errors.New("salt must not be empty")
	}

	kdf := p.KDF
	if kdf == "" {
		kdf = KDFArgon2id
	}

	switch kdf {
	case KDFArgon2id:
		time, memory, threads, err := argon2Params(p.Time, p.Memory, p.Threads)
		if err != nil {
			return nil, err
		}
		key := argon2.IDKey([]byte(passphrase), p.Salt, uint32(time), uint32(memory), uint8(threads), keyLen)
		return &Key{key: key, salt: p.Salt, kdf: KDFArgon2id, time: time, memory: memory, threads: threads}, nil

	case KDFPBKDF2:
		iterations, err := pbkdf2Params(p.Iter)
		if err != nil {
			return nil, err
		}
		key, err := pbkdf2.Key(sha256.New, passphrase, p.Salt, iterations, keyLen)
		if err != nil {
			return nil, fmt.Errorf("derive key: %w", err)
		}
		return &Key{key: key, salt: p.Salt, kdf: KDFPBKDF2, iter: iterations}, nil

	default:
		return nil, fmt.Errorf("unsupported kdf %q", p.KDF)
	}
}

// Iterations reports the PBKDF2 work factor, or 0 when the key is not PBKDF2.
func (k *Key) Iterations() int { return k.iter }

// KDF reports the key-derivation function this key was derived with.
func (k *Key) KDF() string {
	if k.kdf == "" {
		return KDFArgon2id
	}
	return k.kdf
}

// Seal encrypts plaintext with the derived key, authenticating aad alongside
// the ciphertext. The envelope records the KDF and its parameters, so another
// machine can derive the same key from the passphrase alone.
func (k *Key) Seal(plaintext, aad []byte) (*Envelope, error) {
	gcm, err := k.aead()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	env := &Envelope{
		Version: envelopeVersion,
		KDF:     k.KDF(),
		Salt:    base64.StdEncoding.EncodeToString(k.salt),
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plaintext, aad)),
	}

	if env.KDF == KDFPBKDF2 {
		// v1 is the PBKDF2 format: no kdf field, iterations in "iter". Writing
		// this shape only happens when a key was deliberately derived with
		// DeriveKey; it keeps the v1 format byte-identical.
		env.Version = envelopeV1
		env.KDF = ""
		env.Iter = k.iter
	} else if env.KDF == KDFRaw {
		// A key that was never derived from a passphrase (a team key unwrapped
		// from a member's record). It has no salt to reproduce, so the envelope
		// only names the key type and drops the derivation fields.
		env.Version = envelopeV3
		env.Salt = ""
	} else {
		env.Time = k.time
		env.Memory = k.memory
		env.Threads = k.threads
	}

	return env, nil
}

// Open decrypts an envelope with an already-derived key. A failure here means
// the passphrase is wrong, the additional data does not match, or the
// ciphertext was tampered with — this function cannot tell those apart, and
// deliberately does not guess.
func (k *Key) Open(env *Envelope, aad []byte) ([]byte, error) {
	if env == nil {
		return nil, errors.New("nil envelope")
	}
	if err := k.matchesEnvelope(env); err != nil {
		return nil, err
	}

	gcm, err := k.aead()
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce length")
	}

	plaintext, err := gcm.Open(nil, nonce, data, aad)
	if err != nil {
		return nil, errors.New("decryption failed: wrong passphrase, mismatched server, or tampered data")
	}
	return plaintext, nil
}

// matchesEnvelope rejects a key that cannot possibly belong to the envelope's
// KDF, so the failure says which parameter is wrong instead of surfacing as a
// generic authentication error.
func (k *Key) matchesEnvelope(env *Envelope) error {
	switch env.Version {
	case envelopeV1:
		if k.KDF() != KDFPBKDF2 {
			return fmt.Errorf("envelope v1 is %s, but the key was derived with %s", KDFPBKDF2, k.KDF())
		}
	case envelopeV2:
		if k.KDF() != KDFArgon2id {
			return fmt.Errorf("envelope v2 is %s, but the key was derived with %s", KDFArgon2id, k.KDF())
		}
	case envelopeV3:
		if k.KDF() != KDFRaw {
			return fmt.Errorf("envelope v3 is %s, but the key was derived with %s", KDFRaw, k.KDF())
		}
	default:
		return fmt.Errorf("unsupported envelope version %d", env.Version)
	}
	return nil
}

// OpenWithPassphrase derives the key using the envelope's own parameters and
// decrypts. It is the entry point on a machine that has never synced before,
// and it is the only place that has to know about old envelope versions.
func OpenWithPassphrase(passphrase string, env *Envelope, aad []byte) ([]byte, error) {
	if env == nil {
		return nil, errors.New("nil envelope")
	}
	salt, err := base64.StdEncoding.DecodeString(env.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}

	params := Params{Salt: salt}
	switch env.Version {
	case envelopeV1:
		params.KDF = KDFPBKDF2
		params.Iter = env.Iter
	case envelopeV2:
		params.KDF = env.KDF
		if params.KDF == "" {
			params.KDF = KDFArgon2id
		}
		if params.KDF != KDFArgon2id {
			return nil, fmt.Errorf("unsupported kdf %q for envelope v2", env.KDF)
		}
		params.Time = env.Time
		params.Memory = env.Memory
		params.Threads = env.Threads
	case envelopeV3:
		return nil, errors.New("this payload is sealed with a team key, not a passphrase")
	default:
		return nil, fmt.Errorf("unsupported envelope version %d", env.Version)
	}

	key, err := DeriveKeyParams(passphrase, params)
	if err != nil {
		return nil, err
	}
	return key.Open(env, aad)
}

func argon2Params(time, memory, threads int) (int, int, int, error) {
	if time <= 0 {
		time = DefaultArgon2Time
	}
	if memory <= 0 {
		memory = DefaultArgon2Memory
	}
	if threads <= 0 {
		threads = DefaultArgon2Threads
	}
	if time > maxArgon2Time {
		return 0, 0, 0, fmt.Errorf("argon2id time %d exceeds the maximum of %d", time, maxArgon2Time)
	}
	if memory < minArgon2Memory || memory > maxArgon2Memory {
		return 0, 0, 0, fmt.Errorf("argon2id memory %d KiB is outside %d..%d", memory, minArgon2Memory, maxArgon2Memory)
	}
	if threads > maxArgon2Threads {
		return 0, 0, 0, fmt.Errorf("argon2id threads %d exceeds the maximum of %d", threads, maxArgon2Threads)
	}
	// argon2 requires at least 8 KiB per lane.
	if memory < 8*threads {
		return 0, 0, 0, fmt.Errorf("argon2id memory %d KiB is too small for %d threads", memory, threads)
	}
	return time, memory, threads, nil
}

func pbkdf2Params(iterations int) (int, error) {
	if iterations <= 0 {
		iterations = DefaultIterations
	}
	if iterations > maxPBKDF2Iter {
		return 0, fmt.Errorf("pbkdf2 iterations %d exceed the maximum of %d", iterations, maxPBKDF2Iter)
	}
	return iterations, nil
}

func (k *Key) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AEAD: %w", err)
	}
	return gcm, nil
}

// AAD binds a blob to the server it belongs to. Without this, a server that
// stores the blobs could serve one server's payload in place of another's and
// the client would decrypt it happily.
func AAD(name, version string) []byte {
	return []byte(name + "\x00" + version)
}

// TeamAAD binds a shared blob to its team as well as its server, so a blob
// cannot be replayed across teams or servers.
func TeamAAD(teamID, name string, version int64) []byte {
	return []byte(teamID + "\x00" + name + "\x00" + strconv.FormatInt(version, 10))
}

// Bytes marshals an envelope for transport.
func (e *Envelope) Bytes() ([]byte, error) { return json.Marshal(e) }

// ParseEnvelope parses an envelope received from the server.
func ParseEnvelope(data []byte) (*Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("parse envelope: %w", err)
	}
	return &e, nil
}
