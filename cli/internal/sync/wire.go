package sync

import (
	"context"
	"errors"
	"time"

	"github.com/ranscky/neuron/pkg/mcp"
)

// ErrNotFound is returned by a Remote when the account has no blob for a name.
var ErrNotFound = errors.New("no remote blob for that server")

// Remote is the server side of sync. Implementations only ever see ciphertext.
type Remote interface {
	// List returns the names of every server the account has a blob for.
	List(ctx context.Context) ([]string, error)
	// Fetch returns the blob for one server, or ErrNotFound.
	Fetch(ctx context.Context, name string) (*RemoteBlob, error)
	// Push stores a blob, replacing any previous version of the same name.
	Push(ctx context.Context, blob *RemoteBlob) error
}

// RemoteBlob is what the server stores: opaque ciphertext plus the metadata it
// needs to serve and version. Nothing here reveals a configuration or a secret.
type RemoteBlob struct {
	Name      string    `json:"name"`
	Version   int64     `json:"version"`
	Envelope  *Envelope `json:"envelope"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Payload is the plaintext that gets encrypted for one server.
type Payload struct {
	Version int64 `json:"version"`
	// Spec is the server definition, including the *names* of the secrets it
	// needs but never their values.
	Spec mcp.ServerSpec `json:"spec"`
	// SecretValues maps an environment variable name to its plaintext value.
	// This is the part that must never reach the server in the clear.
	SecretValues map[string]string `json:"secret_values,omitempty"`
}
