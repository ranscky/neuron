package secrets

import (
	"github.com/zalando/go-keyring"
)

const serviceName = "neuron"

// Store handles OS keychain integration
type Store struct{}

// NewStore creates a new Store instance
func NewStore() *Store {
	return &Store{}
}

// Set stores a secret in the OS keyring
func (s *Store) Set(key, value string) error {
	return keyring.Set(serviceName, key, value)
}

// Get retrieves a secret from the OS keyring
func (s *Store) Get(key string) (string, error) {
	return keyring.Get(serviceName, key)
}