package sync

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ranscky/neuron/pkg/mcp"
)

// SecretStore is the local credential store — the OS keychain in production.
type SecretStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

// Engine reconciles the local MCP store with the encrypted remote store.
type Engine struct {
	Store     *mcp.Store
	Secrets   SecretStore
	Remote    Remote
	Key       *Key
	statePath string
}

// NewEngine wires an engine and locates its sync state file.
func NewEngine(store *mcp.Store, secrets SecretStore, remote Remote, key *Key) (*Engine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return &Engine{
		Store:     store,
		Secrets:   secrets,
		Remote:    remote,
		Key:       key,
		statePath: filepath.Join(home, ".neuron", "sync-state.json"),
	}, nil
}

// Result summarises one reconciliation.
type Result struct {
	Pushed    []string
	Pulled    []string
	Unchanged []string
	Conflicts []Conflict
}

// Conflict is a server that changed both locally and remotely since the last
// sync. The engine refuses to guess, because guessing loses somebody's secret.
type Conflict struct {
	Name   string
	Reason string
}

// stateFile records what we last synced so local edits can be told apart from
// remote edits without trusting either machine's clock.
type stateFile struct {
	Versions map[string]int64  `json:"versions"`
	Hashes   map[string]string `json:"hashes"`
}

// Sync reconciles every server known locally or remotely against the account's
// own encrypted store.
//
// It never deletes and never overwrites a change it did not make: a server that
// moved on both sides is reported as a conflict instead.
func (e *Engine) Sync(ctx context.Context) (Result, error) {
	return e.reconcile(ctx, reconcileTarget{
		namespace: "account",
		list:      e.Remote.List,
		fetch:     e.Remote.Fetch,
		push:      e.Remote.Push,
		aad:       func(name string, version int64) []byte { return AAD(name, fmt.Sprint(version)) },
	}, e.Key)
}

// SyncTeam reconciles this machine against a team's shared encrypted store,
// using the team key rather than the account key.
func (e *Engine) SyncTeam(ctx context.Context, remote TeamRemote, teamID string, teamKey *Key) (Result, error) {
	return e.reconcile(ctx, reconcileTarget{
		namespace: "team:" + teamID,
		list: func(ctx context.Context) ([]string, error) {
			return remote.TeamBlobs(ctx, teamID)
		},
		fetch: func(ctx context.Context, name string) (*RemoteBlob, error) {
			return remote.FetchTeamBlob(ctx, teamID, name)
		},
		push: func(ctx context.Context, blob *RemoteBlob) error {
			return remote.PushTeamBlob(ctx, teamID, blob)
		},
		aad: func(name string, version int64) []byte { return TeamAAD(teamID, name, version) },
	}, teamKey)
}

func (e *Engine) reconcile(ctx context.Context, t reconcileTarget, syncKey *Key) (Result, error) {
	var res Result

	st, err := e.loadState()
	if err != nil {
		return res, err
	}

	remoteNames, err := t.list(ctx)
	if err != nil {
		return res, fmt.Errorf("list remote: %w", err)
	}

	seen := map[string]bool{}
	for _, name := range e.Store.Names() {
		seen[name] = true
	}
	for _, name := range remoteNames {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		payload, hash, err := e.buildPayload(name)
		if err != nil {
			return res, err
		}
		localExists := payload != nil

		remote, err := t.fetch(ctx, name)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return res, fmt.Errorf("fetch %s: %w", name, err)
		}
		remoteExists := err == nil && remote != nil

		skey := stateKey(t.namespace, name)
		baseVersion := st.Versions[skey]
		localChanged := localExists && hash != st.Hashes[skey]
		remoteChanged := remoteExists && remote.Version != baseVersion

		switch {
		case !localExists && !remoteExists:
			continue

		case !localExists && remoteExists:
			if _, err := e.applyRemoteWith(syncKey, name, remote, t); err != nil {
				return res, err
			}
			st.Versions[skey] = remote.Version
			st.Hashes[skey] = e.hashIfExists(name)
			res.Pulled = append(res.Pulled, name)

		case localExists && !remoteExists:
			pushed, err := e.pushTo(ctx, t, syncKey, name, payload, baseVersion+1)
			if err != nil {
				return res, err
			}
			if !pushed {
				res.Conflicts = append(res.Conflicts, Conflict{
					Name:   name,
					Reason: "another machine pushed a newer version first",
				})
				continue
			}
			st.Versions[skey] = baseVersion + 1
			st.Hashes[skey] = hash
			res.Pushed = append(res.Pushed, name)

		case localChanged && remoteChanged:
			res.Conflicts = append(res.Conflicts, Conflict{
				Name:   name,
				Reason: fmt.Sprintf("changed locally and remotely since version %d", baseVersion),
			})

		case localChanged:
			pushed, err := e.pushTo(ctx, t, syncKey, name, payload, baseVersion+1)
			if err != nil {
				return res, err
			}
			if !pushed {
				res.Conflicts = append(res.Conflicts, Conflict{
					Name:   name,
					Reason: "another machine pushed a newer version first",
				})
				continue
			}
			st.Versions[skey] = baseVersion + 1
			st.Hashes[skey] = hash
			res.Pushed = append(res.Pushed, name)

		case remoteChanged:
			if _, err := e.applyRemoteWith(syncKey, name, remote, t); err != nil {
				return res, err
			}
			st.Versions[skey] = remote.Version
			st.Hashes[skey] = e.hashIfExists(name)
			res.Pulled = append(res.Pulled, name)

		default:
			res.Unchanged = append(res.Unchanged, name)
		}
	}

	if err := e.saveState(st); err != nil {
		return res, err
	}
	return res, nil
}

// buildPayload assembles the plaintext for a server, returning nil when the
// server does not exist locally.
func (e *Engine) buildPayload(name string) (*Payload, string, error) {
	spec, ok := e.Store.Servers[name]
	if !ok {
		return nil, "", nil
	}

	values := map[string]string{}
	for envName, keychainKey := range spec.Secrets {
		value, err := e.Secrets.Get(keychainKey)
		if err != nil {
			// A server whose credential is not set still syncs its definition;
			// the missing secret is what `neuron mcp doctor` is for.
			continue
		}
		values[envName] = value
	}

	p := &Payload{Spec: spec, SecretValues: values}
	hash, err := payloadHash(p)
	if err != nil {
		return nil, "", err
	}
	return p, hash, nil
}

func (e *Engine) hashIfExists(name string) string {
	_, hash, err := e.buildPayload(name)
	if err != nil {
		return ""
	}
	return hash
}

// payloadHash hashes the meaningful content of a payload, excluding the version
// counter so that a version bump alone does not look like an edit.
func payloadHash(p *Payload) (string, error) {
	clone := *p
	clone.Version = 0
	data, err := json.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("hash payload: %w", err)
	}
	sum := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

// pushTo uploads a payload to a target. It reports false when the remote
// already holds a newer version — a conflict to surface, not an error to fail on.
func (e *Engine) pushTo(ctx context.Context, t reconcileTarget, syncKey *Key, name string, p *Payload, version int64) (bool, error) {
	p.Version = version

	plaintext, err := json.Marshal(p)
	if err != nil {
		return false, fmt.Errorf("marshal payload for %s: %w", name, err)
	}
	envelope, err := syncKey.Seal(plaintext, t.aad(name, version))
	if err != nil {
		return false, fmt.Errorf("encrypt %s: %w", name, err)
	}

	err = t.push(ctx, &RemoteBlob{
		Name:      name,
		Version:   version,
		Envelope:  envelope,
		UpdatedAt: time.Now().UTC(),
	})
	if errors.Is(err, ErrStaleVersion) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("push %s: %w", name, err)
	}
	return true, nil
}

// applyRemoteWith decrypts a blob and writes it into the local store and keychain.
func (e *Engine) applyRemoteWith(syncKey *Key, name string, blob *RemoteBlob, t reconcileTarget) (bool, error) {
	plaintext, err := syncKey.Open(blob.Envelope, t.aad(name, blob.Version))
	if err != nil {
		return false, fmt.Errorf("decrypt %s: %w", name, err)
	}

	var p Payload
	if err := json.Unmarshal(plaintext, &p); err != nil {
		return false, fmt.Errorf("parse payload for %s: %w", name, err)
	}

	if err := e.Store.Add(name, p.Spec); err != nil {
		return false, fmt.Errorf("store %s: %w", name, err)
	}

	for envName, value := range p.SecretValues {
		keychainKey, ok := p.Spec.Secrets[envName]
		if !ok {
			continue
		}
		if err := e.Secrets.Set(keychainKey, value); err != nil {
			return false, fmt.Errorf("store secret %q for %s: %w", keychainKey, name, err)
		}
	}

	return true, nil
}

func (e *Engine) loadState() (*stateFile, error) {
	st := &stateFile{Versions: map[string]int64{}, Hashes: map[string]string{}}

	data, err := os.ReadFile(e.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, fmt.Errorf("read sync state: %w", err)
	}
	if len(data) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, fmt.Errorf("parse sync state: %w", err)
	}
	if st.Versions == nil {
		st.Versions = map[string]int64{}
	}
	if st.Hashes == nil {
		st.Hashes = map[string]string{}
	}
	return st, nil
}

func (e *Engine) saveState(st *stateFile) error {
	if err := os.MkdirAll(filepath.Dir(e.statePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(e.statePath, append(data, '\n'), 0o600)
}
