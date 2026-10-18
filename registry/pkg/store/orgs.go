package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Publishing is org-scoped: every API key belongs to exactly one org, and an org
// may only publish packages under its own name (`acme/web-search`). Reads stay
// public, so the registry is still a public catalogue.

var (
	// ErrInvalidOrgName means the name is not a usable registry namespace.
	ErrInvalidOrgName = errors.New("org names must be 2-32 characters of a-z, 0-9 or '-'")
	// ErrOrgExists means another org already holds that name.
	ErrOrgExists = errors.New("that org name is already taken")
	// ErrNoSuchOrg means the org id is unknown.
	ErrNoSuchOrg = errors.New("no such org")
	// ErrUnknownKey means the API key does not match any org.
	ErrUnknownKey = errors.New("unknown api key")
)

// Org is a publishing organisation. Its name is the package prefix it owns.
type Org struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// APIKey is a publishing credential. Only its hash is stored, so a leaked data
// directory does not hand over live keys.
type APIKey struct {
	Hash      string    `json:"hash"`
	OrgID     string    `json:"org_id"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type orgsFile struct {
	Orgs map[string]*Org    `json:"orgs"`
	Keys map[string]*APIKey `json:"keys"`
}

var orgNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

func orgsPath() string { return filepath.Join("data", "orgs.json") }

func (fs *FileStore) loadOrgs() (*orgsFile, error) {
	data := &orgsFile{Orgs: map[string]*Org{}, Keys: map[string]*APIKey{}}

	raw, err := os.ReadFile(orgsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, fmt.Errorf("read orgs: %w", err)
	}
	if len(raw) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(raw, data); err != nil {
		return nil, fmt.Errorf("parse orgs: %w", err)
	}
	if data.Orgs == nil {
		data.Orgs = map[string]*Org{}
	}
	if data.Keys == nil {
		data.Keys = map[string]*APIKey{}
	}
	return data, nil
}

func (fs *FileStore) saveOrgs(data *orgsFile) error {
	if err := os.MkdirAll(filepath.Dir(orgsPath()), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal orgs: %w", err)
	}
	if err := os.WriteFile(orgsPath(), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write orgs: %w", err)
	}
	return nil
}

// CreateOrg registers an org and returns its first API key. The plaintext key is
// returned exactly once; only its hash is ever written to disk.
func (fs *FileStore) CreateOrg(name string) (*Org, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !orgNameRE.MatchString(name) {
		return nil, "", ErrInvalidOrgName
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := fs.loadOrgs()
	if err != nil {
		return nil, "", err
	}
	for _, org := range data.Orgs {
		if org.Name == name {
			return nil, "", ErrOrgExists
		}
	}

	id, err := randomToken("org_", 12)
	if err != nil {
		return nil, "", err
	}
	key, err := randomToken("nrn_", 32)
	if err != nil {
		return nil, "", err
	}

	now := time.Now().UTC()
	org := &Org{ID: id, Name: name, CreatedAt: now}
	data.Orgs[id] = org
	data.Keys[hashKey(key)] = &APIKey{
		Hash:      hashKey(key),
		OrgID:     id,
		Label:     "initial",
		CreatedAt: now,
	}

	if err := fs.saveOrgs(data); err != nil {
		return nil, "", err
	}
	return org, key, nil
}

// CreateAPIKey mints another key for an existing org — for a CI runner, or to
// rotate a leaked one.
func (fs *FileStore) CreateAPIKey(orgID, label string) (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := fs.loadOrgs()
	if err != nil {
		return "", err
	}
	if _, ok := data.Orgs[orgID]; !ok {
		return "", ErrNoSuchOrg
	}

	key, err := randomToken("nrn_", 32)
	if err != nil {
		return "", err
	}
	label = strings.TrimSpace(label)
	if len(label) > 64 {
		label = label[:64]
	}
	data.Keys[hashKey(key)] = &APIKey{
		Hash:      hashKey(key),
		OrgID:     orgID,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}
	if err := fs.saveOrgs(data); err != nil {
		return "", err
	}
	return key, nil
}

// OrgForKey resolves a bearer key to its org. It is the registry's only
// authentication check.
func (fs *FileStore) OrgForKey(key string) (*Org, error) {
	if key == "" {
		return nil, ErrUnknownKey
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := fs.loadOrgs()
	if err != nil {
		return nil, err
	}
	record, ok := data.Keys[hashKey(key)]
	if !ok {
		return nil, ErrUnknownKey
	}
	org, ok := data.Orgs[record.OrgID]
	if !ok {
		return nil, ErrNoSuchOrg
	}
	cp := *org
	return &cp, nil
}

// OrgByID looks an org up by id.
func (fs *FileStore) OrgByID(id string) (*Org, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := fs.loadOrgs()
	if err != nil {
		return nil, err
	}
	org, ok := data.Orgs[id]
	if !ok {
		return nil, ErrNoSuchOrg
	}
	cp := *org
	return &cp, nil
}

// Owns reports whether a package name belongs to an org. Names are
// `<org>/<package>`, which is what makes publishing org-scoped without any extra
// bookkeeping.
func Owns(orgName, packageName string) bool {
	orgName = strings.ToLower(strings.TrimSpace(orgName))
	packageName = strings.ToLower(strings.TrimSpace(packageName))
	prefix := orgName + "/"
	return strings.HasPrefix(packageName, prefix) && len(packageName) > len(prefix)
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func randomToken(prefix string, n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}
