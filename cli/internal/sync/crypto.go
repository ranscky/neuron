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
)

const (
	// DefaultIterations follows OWASP guidance for PBKDF2-HMAC-SHA256.
	DefaultIterations = 600_000
	saltLen           = 16
	keyLen            = 32
	envelopeVersion   = 1
)

// Envelope is the self-describing ciphertext the server stores. No field here
// is secret: the salt and iteration count only need to be reproduced on another
// machine to re-derive the key from the passphrase.
type Envelope struct {
	Version int    `json:"v"`
	Salt    string `json:"salt"`
	Iter    int    `json:"iter"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

// Key is a derived symmetric key held in memory for a sync session.
type Key struct {
	key  []byte
	salt []byte
	iter int
}

// NewSalt returns a fresh random salt for a new sync account.
func NewSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	return salt, nil
}

// DeriveKey derives an encryption key from a passphrase. A non-positive
// iteration count selects DefaultIterations.
func DeriveKey(passphrase string, salt []byte, iterations int) (*Key, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase must not be empty")
	}
	if len(salt) == 0 {
		return nil, errors.New("salt must not be empty")
	}
	if iterations <= 0 {
		iterations = DefaultIterations
	}

	key, err := pbkdf2.Key(sha256.New, passphrase, salt, iterations, keyLen)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return &Key{key: key, salt: salt, iter: iterations}, nil
}

// Iterations reports the work factor this key was derived with.
func (k *Key) Iterations() int { return k.iter }

// Seal encrypts plaintext with the derived key, authenticating aad alongside
// the ciphertext.
func (k *Key) Seal(plaintext, aad []byte) (*Envelope, error) {
	gcm, err := k.aead()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return &Envelope{
		Version: envelopeVersion,
		Salt:    base64.StdEncoding.EncodeToString(k.salt),
		Iter:    k.iter,
		Nonce:   base64.StdEncoding.EncodeToString(nonce),
		Data:    base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plaintext, aad)),
	}, nil
}

// Open decrypts an envelope with an already-derived key. A failure here means
// the passphrase is wrong, the additional data does not match, or the
// ciphertext was tampered with — this function cannot tell those apart, and
// deliberately does not guess.
func (k *Key) Open(env *Envelope, aad []byte) ([]byte, error) {
	if env == nil {
		return nil, errors.New("nil envelope")
	}
	if env.Version != envelopeVersion {
		return nil, fmt.Errorf("unsupported envelope version %d", env.Version)
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

// OpenWithPassphrase derives the key using the envelope's own parameters and
// decrypts. It is the entry point on a machine that has never synced before.
func OpenWithPassphrase(passphrase string, env *Envelope, aad []byte) ([]byte, error) {
	if env == nil {
		return nil, errors.New("nil envelope")
	}
	salt, err := base64.StdEncoding.DecodeString(env.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}
	key, err := DeriveKey(passphrase, salt, env.Iter)
	if err != nil {
		return nil, err
	}
	return key.Open(env, aad)
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
