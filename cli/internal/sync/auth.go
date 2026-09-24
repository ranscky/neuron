package sync

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenKey is the keychain entry holding the sync session token. The token is
// deliberately not written to the auth file: a stolen file must not hand over a
// live session.
const TokenKey = "neuron-sync-token"

// Auth is the non-secret half of a signed-in session.
type Auth struct {
	BaseURL    string    `json:"base_url"`
	AccountID  string    `json:"account_id"`
	Salt       string    `json:"salt"`
	Plan       string    `json:"plan,omitempty"`
	SignedInAt time.Time `json:"signed_in_at"`
}

func authPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".neuron", "auth.json"), nil
}

// LoadAuth reads the stored session metadata.
func LoadAuth() (*Auth, error) {
	path, err := authPath()
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read auth file: %w", err)
	}

	var auth Auth
	if err := json.Unmarshal(raw, &auth); err != nil {
		return nil, fmt.Errorf("parse auth file: %w", err)
	}
	return &auth, nil
}

// SaveAuth writes the session metadata, readable only by the user.
func SaveAuth(auth *Auth) error {
	path, err := authPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// ClearAuth removes the stored session metadata.
func ClearAuth() error {
	path, err := authPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SaltBytes decodes the stored KDF salt.
func (a *Auth) SaltBytes() ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(a.Salt)
	if err != nil {
		return nil, fmt.Errorf("auth file has an invalid salt: %w", err)
	}
	if len(salt) == 0 {
		return nil, fmt.Errorf("auth file has an empty salt")
	}
	return salt, nil
}
