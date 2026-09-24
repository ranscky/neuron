package sync

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ranscky/neuron/pkg/mcp"
)

type fakeSecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{values: map[string]string{}} }

func (f *fakeSecrets) Get(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	if !ok {
		return "", errors.New("secret not found")
	}
	return v, nil
}

func (f *fakeSecrets) Set(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[key] = value
	return nil
}

type fakeRemote struct {
	mu    sync.Mutex
	blobs map[string]*RemoteBlob
}

func newFakeRemote() *fakeRemote { return &fakeRemote{blobs: map[string]*RemoteBlob{}} }

func (f *fakeRemote) List(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.blobs))
	for name := range f.blobs {
		names = append(names, name)
	}
	return names, nil
}

func (f *fakeRemote) Fetch(ctx context.Context, name string) (*RemoteBlob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	blob, ok := f.blobs[name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *blob
	return &cp, nil
}

func (f *fakeRemote) Push(ctx context.Context, blob *RemoteBlob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *blob
	f.blobs[blob.Name] = &cp
	return nil
}

// machine simulates one computer: a fresh HOME with its own Neuron store,
// secret store and sync state, pointed at a shared remote.
func machine(t *testing.T, remote Remote, key *Key) (*Engine, *mcp.Store, *fakeSecrets) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	store, err := mcp.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	secrets := newFakeSecrets()
	engine, err := NewEngine(store, secrets, remote, key)
	if err != nil {
		t.Fatal(err)
	}
	return engine, store, secrets
}

func testSyncKey(t *testing.T, passphrase string) *Key {
	t.Helper()
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	key, err := DeriveKey(passphrase, salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSyncPushesThenPullsAcrossMachines(t *testing.T) {
	ctx := context.Background()
	remote := newFakeRemote()
	key := testSyncKey(t, "correct horse battery staple")

	// Machine A: define a server that needs a credential, then sync.
	engineA, storeA, secretsA := machine(t, remote, key)
	if err := storeA.Add("github", mcp.ServerSpec{
		Command: "npx",
		Args:    []string{"-y", "server-github"},
		Secrets: map[string]string{"GITHUB_TOKEN": "github-token"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := secretsA.Set("github-token", "ghp_realsecret"); err != nil {
		t.Fatal(err)
	}

	resA, err := engineA.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resA.Pushed) != 1 || resA.Pushed[0] != "github" {
		t.Fatalf("expected github to be pushed, got %+v", resA)
	}

	// The remote must only ever hold ciphertext.
	blob, err := remote.Fetch(ctx, "github")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := blob.Envelope.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("ghp_realsecret")) {
		t.Fatal("plaintext secret reached the remote store")
	}

	// Machine B: brand new, same passphrase.
	engineB, storeB, secretsB := machine(t, remote, key)
	resB, err := engineB.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resB.Pulled) != 1 || resB.Pulled[0] != "github" {
		t.Fatalf("expected github to be pulled, got %+v", resB)
	}

	spec, err := storeB.Get("github")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "npx" || spec.Secrets["GITHUB_TOKEN"] != "github-token" {
		t.Errorf("pulled spec is wrong: %+v", spec)
	}
	value, err := secretsB.Get("github-token")
	if err != nil {
		t.Fatal(err)
	}
	if value != "ghp_realsecret" {
		t.Errorf("pulled secret value is wrong: %q", value)
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	ctx := context.Background()
	remote := newFakeRemote()
	key := testSyncKey(t, "passphrase")

	engine, store, _ := machine(t, remote, key)
	if err := store.Add("fs", mcp.ServerSpec{Command: "npx"}); err != nil {
		t.Fatal(err)
	}

	if _, err := engine.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	res, err := engine.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pushed) != 0 || len(res.Pulled) != 0 {
		t.Errorf("a second sync should change nothing, got %+v", res)
	}
	if len(res.Unchanged) != 1 || res.Unchanged[0] != "fs" {
		t.Errorf("expected fs to be reported unchanged, got %+v", res)
	}
}

// The important one: two machines editing the same server must not silently
// clobber each other.
func TestConcurrentEditsSurfaceAConflict(t *testing.T) {
	ctx := context.Background()
	remote := newFakeRemote()
	key := testSyncKey(t, "passphrase")

	engineA, storeA, _ := machine(t, remote, key)
	if err := storeA.Add("github", mcp.ServerSpec{Command: "npx"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engineA.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	engineB, storeB, _ := machine(t, remote, key)
	if _, err := engineB.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// Both machines edit the same server before either syncs again.
	specA, _ := storeA.Get("github")
	specA.Command = "npx-from-a"
	if err := storeA.Add("github", specA); err != nil {
		t.Fatal(err)
	}
	specB, _ := storeB.Get("github")
	specB.Command = "npx-from-b"
	if err := storeB.Add("github", specB); err != nil {
		t.Fatal(err)
	}

	resA, err := engineA.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resA.Conflicts) != 0 {
		t.Errorf("A moved first and should have pushed cleanly, got %+v", resA)
	}

	resB, err := engineB.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resB.Conflicts) != 1 || resB.Conflicts[0].Name != "github" {
		t.Fatalf("expected B to report a conflict on github, got %+v", resB)
	}
}

func TestTamperedRemoteBlobIsRejected(t *testing.T) {
	ctx := context.Background()
	remote := newFakeRemote()
	key := testSyncKey(t, "passphrase")

	engineA, storeA, _ := machine(t, remote, key)
	if err := storeA.Add("github", mcp.ServerSpec{Command: "npx"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engineA.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// A malicious server rewrites the ciphertext.
	blob, err := remote.Fetch(ctx, "github")
	if err != nil {
		t.Fatal(err)
	}
	flipped := []byte(blob.Envelope.Data)
	if flipped[0] == 'A' {
		flipped[0] = 'B'
	} else {
		flipped[0] = 'A'
	}
	blob.Envelope.Data = string(flipped)
	if err := remote.Push(ctx, blob); err != nil {
		t.Fatal(err)
	}

	engineB, _, _ := machine(t, remote, key)
	if _, err := engineB.Sync(ctx); err == nil {
		t.Fatal("expected sync to fail on a tampered blob rather than accept it")
	}
}

func TestServerWithoutASetSecretStillSyncsItsDefinition(t *testing.T) {
	ctx := context.Background()
	remote := newFakeRemote()
	key := testSyncKey(t, "passphrase")

	engineA, storeA, _ := machine(t, remote, key)
	if err := storeA.Add("github", mcp.ServerSpec{
		Command: "npx",
		Secrets: map[string]string{"GITHUB_TOKEN": "github-token"},
	}); err != nil {
		t.Fatal(err)
	}
	// Deliberately no secret value stored.

	res, err := engineA.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pushed) != 1 {
		t.Fatalf("expected the definition to sync anyway, got %+v", res)
	}

	engineB, storeB, _ := machine(t, remote, key)
	if _, err := engineB.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	spec, err := storeB.Get("github")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Secrets["GITHUB_TOKEN"] != "github-token" {
		t.Errorf("secret reference should survive sync, got %+v", spec)
	}
}
