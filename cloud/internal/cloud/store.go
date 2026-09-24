// Package cloud implements the Neuron sync service: device authorisation,
// end-to-end encrypted blob storage, and subscription state.
//
// It only ever stores ciphertext for server configurations. It has no ability
// to read a user's secrets, and deliberately so.
package cloud

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
	"sort"
	"sync"
	"time"
)

// Plans.
const (
	PlanFree = "free"
	PlanPro  = "pro"
)

// Errors surfaced to callers and mapped to status codes by the API layer.
var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrExpired      = errors.New("expired")
	ErrPending      = errors.New("authorization pending")
	ErrStaleVersion = errors.New("blob version is not newer than the stored one")
)

// Account is a sync account. Salt is key-derivation material and is not secret;
// the service never sees the passphrase or the derived key.
type Account struct {
	ID                   string    `json:"id"`
	Email                string    `json:"email,omitempty"`
	Salt                 string    `json:"salt"`
	Plan                 string    `json:"plan"`
	CreatedAt            time.Time `json:"created_at"`
	StripeCustomerID     string    `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID string    `json:"stripe_subscription_id,omitempty"`
}

// Blob is one server's encrypted payload plus the metadata the service needs to
// version and serve it.
type Blob struct {
	Name      string          `json:"name"`
	Version   int64           `json:"version"`
	Envelope  json.RawMessage `json:"envelope"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// DeviceCode is an in-progress device authorisation.
type DeviceCode struct {
	DeviceCode string    `json:"device_code"`
	UserCode   string    `json:"user_code"`
	AccountID  string    `json:"account_id,omitempty"`
	Approved   bool      `json:"approved"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type tokenRecord struct {
	AccountID string    `json:"account_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type persisted struct {
	// Tokens is keyed by the SHA-256 of the bearer token, never the token
	// itself, so a stolen database file does not hand over live sessions.
	Tokens      map[string]tokenRecord      `json:"tokens"`
	Accounts    map[string]*Account         `json:"accounts"`
	Blobs       map[string]map[string]*Blob `json:"blobs"`
	DeviceCodes map[string]*DeviceCode      `json:"device_codes"`
	// ProcessedEvents makes billing webhooks idempotent: Stripe retries, and a
	// retry must not re-apply a plan change.
	ProcessedEvents map[string]time.Time `json:"processed_events"`
}

// Store is a mutex-guarded, file-backed store. It is deliberately boring: one
// process, one file, atomic-ish writes.
type Store struct {
	path string
	mu   sync.Mutex
	data persisted
}

// Open loads a store from disk, creating an empty one if the file is absent.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: emptyData()}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read cloud store: %w", err)
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("parse cloud store: %w", err)
	}
	s.ensureMaps()
	return s, nil
}

func emptyData() persisted {
	return persisted{
		Tokens:          map[string]tokenRecord{},
		Accounts:        map[string]*Account{},
		Blobs:           map[string]map[string]*Blob{},
		DeviceCodes:     map[string]*DeviceCode{},
		ProcessedEvents: map[string]time.Time{},
	}
}

func (s *Store) ensureMaps() {
	if s.data.Tokens == nil {
		s.data.Tokens = map[string]tokenRecord{}
	}
	if s.data.Accounts == nil {
		s.data.Accounts = map[string]*Account{}
	}
	if s.data.Blobs == nil {
		s.data.Blobs = map[string]map[string]*Blob{}
	}
	if s.data.DeviceCodes == nil {
		s.data.DeviceCodes = map[string]*DeviceCode{}
	}
	if s.data.ProcessedEvents == nil {
		s.data.ProcessedEvents = map[string]time.Time{}
	}
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o600)
}

// CreateAccount creates an account from an email and a KDF salt.
func (s *Store) CreateAccount(email string, salt []byte) (*Account, error) {
	if len(salt) == 0 {
		return nil, errors.New("salt must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomID("acct_", 16)
	if err != nil {
		return nil, err
	}
	account := &Account{
		ID:        id,
		Email:     email,
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Plan:      PlanFree,
		CreatedAt: time.Now().UTC(),
	}
	s.data.Accounts[id] = account

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return account, nil
}

// GetAccount returns an account by id.
func (s *Store) GetAccount(id string) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.data.Accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *account
	return &cp, nil
}

// SetPlan changes an account's plan.
func (s *Store) SetPlan(accountID, plan string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.data.Accounts[accountID]
	if !ok {
		return ErrNotFound
	}
	account.Plan = plan
	return s.saveLocked()
}

// SetStripe links an account to its Stripe customer and subscription.
func (s *Store) SetStripe(accountID, customerID, subscriptionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.data.Accounts[accountID]
	if !ok {
		return ErrNotFound
	}
	account.StripeCustomerID = customerID
	account.StripeSubscriptionID = subscriptionID
	return s.saveLocked()
}

// FindAccountByStripeCustomer resolves an account from a Stripe customer id.
// Subscription lifecycle events do not always carry our metadata, so the
// customer id is the fallback key.
func (s *Store) FindAccountByStripeCustomer(customerID string) (string, error) {
	if customerID == "" {
		return "", ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, account := range s.data.Accounts {
		if account.StripeCustomerID == customerID {
			return id, nil
		}
	}
	return "", ErrNotFound
}

// ListBlobs returns the names of every blob an account has, sorted.
func (s *Store) ListBlobs(accountID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blobs := s.data.Blobs[accountID]
	names := make([]string, 0, len(blobs))
	for name := range blobs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// GetBlob returns one blob.
func (s *Store) GetBlob(accountID, name string) (*Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blob, ok := s.data.Blobs[accountID][name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *blob
	return &cp, nil
}

// PutBlob stores a blob. Versions must increase, so a stale or replayed push
// cannot roll a user's configuration backwards.
func (s *Store) PutBlob(accountID string, blob *Blob) error {
	if blob.Name == "" {
		return errors.New("blob name must not be empty")
	}
	if len(blob.Envelope) == 0 {
		return errors.New("blob envelope must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.Blobs[accountID] == nil {
		s.data.Blobs[accountID] = map[string]*Blob{}
	}
	if existing, ok := s.data.Blobs[accountID][blob.Name]; ok && blob.Version <= existing.Version {
		return ErrStaleVersion
	}

	if blob.UpdatedAt.IsZero() {
		blob.UpdatedAt = time.Now().UTC()
	}
	cp := *blob
	s.data.Blobs[accountID][blob.Name] = &cp
	return s.saveLocked()
}

// CreateDeviceCode starts a device authorisation.
func (s *Store) CreateDeviceCode(ttl time.Duration) (*DeviceCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deviceCode, err := randomID("dev_", 24)
	if err != nil {
		return nil, err
	}
	userCode, err := randomUserCode()
	if err != nil {
		return nil, err
	}

	code := &DeviceCode{
		DeviceCode: deviceCode,
		UserCode:   userCode,
		ExpiresAt:  time.Now().UTC().Add(ttl),
	}
	s.data.DeviceCodes[deviceCode] = code

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	cp := *code
	return &cp, nil
}

// ApproveDeviceCode binds a pending device code to an account.
func (s *Store) ApproveDeviceCode(userCode, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Accounts[accountID]; !ok {
		return ErrNotFound
	}
	for _, code := range s.data.DeviceCodes {
		if code.UserCode != userCode {
			continue
		}
		if time.Now().UTC().After(code.ExpiresAt) {
			return ErrExpired
		}
		code.AccountID = accountID
		code.Approved = true
		return s.saveLocked()
	}
	return ErrNotFound
}

// PollDeviceCode exchanges an approved device code for an access token.
func (s *Store) PollDeviceCode(deviceCode string, ttl time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code, ok := s.data.DeviceCodes[deviceCode]
	if !ok {
		return "", ErrNotFound
	}
	if time.Now().UTC().After(code.ExpiresAt) {
		return "", ErrExpired
	}
	if !code.Approved {
		return "", ErrPending
	}

	token, err := randomID("", 32)
	if err != nil {
		return "", err
	}
	s.data.Tokens[hashToken(token)] = tokenRecord{
		AccountID: code.AccountID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}

	// A device code is single-use.
	delete(s.data.DeviceCodes, deviceCode)

	if err := s.saveLocked(); err != nil {
		return "", err
	}
	return token, nil
}

// ResolveToken maps a bearer token to its account, rejecting expired sessions.
func (s *Store) ResolveToken(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.data.Tokens[hashToken(token)]
	if !ok {
		return "", ErrUnauthorized
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		delete(s.data.Tokens, hashToken(token))
		_ = s.saveLocked()
		return "", ErrExpired
	}
	return record.AccountID, nil
}

// RevokeToken deletes a session.
func (s *Store) RevokeToken(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data.Tokens, hashToken(token))
	return s.saveLocked()
}

// HasProcessedEvent reports whether a billing event was already applied.
func (s *Store) HasProcessedEvent(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data.ProcessedEvents[id]
	return ok
}

// MarkEventProcessed records that a billing event has been applied.
func (s *Store) MarkEventProcessed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.ProcessedEvents == nil {
		s.data.ProcessedEvents = map[string]time.Time{}
	}
	s.data.ProcessedEvents[id] = time.Now().UTC()
	return s.saveLocked()
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomID(prefix string, n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// randomUserCode returns a short, human-typable code like "K7QP-3M2X".
func randomUserCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous characters
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate user code: %w", err)
	}
	out := make([]byte, 0, 9)
	for i, b := range buf {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, alphabet[int(b)%len(alphabet)])
	}
	return string(out), nil
}
