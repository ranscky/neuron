package sync

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ranscky/neuron/pkg/mcp"
)

// fakeTeamRemote is an in-memory stand-in for a team's shared store.
type fakeTeamRemote struct {
	mu    sync.Mutex
	blobs map[string]map[string]*RemoteBlob
}

func newFakeTeamRemote() *fakeTeamRemote {
	return &fakeTeamRemote{blobs: map[string]map[string]*RemoteBlob{}}
}

func (f *fakeTeamRemote) CreateTeam(ctx context.Context, name string, salt []byte) (*Team, error) {
	return &Team{ID: "team_1", Name: name}, nil
}

func (f *fakeTeamRemote) ListTeams(ctx context.Context) ([]Team, error) { return nil, nil }

func (f *fakeTeamRemote) JoinTeam(ctx context.Context, inviteCode string) (*Team, error) {
	return &Team{ID: "team_1"}, nil
}

func (f *fakeTeamRemote) GetTeam(ctx context.Context, teamID string) (*Team, error) {
	return &Team{ID: teamID}, nil
}

func (f *fakeTeamRemote) TeamBlobs(ctx context.Context, teamID string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	names := make([]string, 0, len(f.blobs[teamID]))
	for name := range f.blobs[teamID] {
		names = append(names, name)
	}
	return names, nil
}

func (f *fakeTeamRemote) FetchTeamBlob(ctx context.Context, teamID, name string) (*RemoteBlob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	blob, ok := f.blobs[teamID][name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *blob
	return &cp, nil
}

func (f *fakeTeamRemote) PushTeamBlob(ctx context.Context, teamID string, blob *RemoteBlob) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.blobs[teamID] == nil {
		f.blobs[teamID] = map[string]*RemoteBlob{}
	}
	if existing, ok := f.blobs[teamID][blob.Name]; ok && blob.Version <= existing.Version {
		return ErrStaleVersion
	}
	cp := *blob
	f.blobs[teamID][blob.Name] = &cp
	return nil
}

func TestSyncTeamSharesServersAndSecrets(t *testing.T) {
	ctx := context.Background()
	remote := newFakeTeamRemote()

	// Both members derive the same key from the same team salt and passphrase.
	salt, err := NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	teamKey, err := DeriveKey("team-passphrase", salt, testIterations)
	if err != nil {
		t.Fatal(err)
	}

	// Member A publishes one of their servers to the team.
	engineA, storeA, secretsA := machine(t, newFakeRemote(), teamKey)
	if err := storeA.Add("team-github", mcp.ServerSpec{
		Command: "npx",
		Secrets: map[string]string{"GITHUB_TOKEN": "team-token"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := secretsA.Set("team-token", "ghp_teamsecret"); err != nil {
		t.Fatal(err)
	}

	resA, err := engineA.SyncTeam(ctx, remote, "team_1", teamKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(resA.Pushed) != 1 || resA.Pushed[0] != "team-github" {
		t.Fatalf("expected a push, got %+v", resA)
	}

	// The shared store must only ever hold ciphertext.
	blob, err := remote.FetchTeamBlob(ctx, "team_1", "team-github")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := blob.Envelope.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("ghp_teamsecret")) {
		t.Fatal("team secret leaked into the shared store")
	}

	// Member B pulls it.
	engineB, storeB, secretsB := machine(t, newFakeRemote(), teamKey)
	resB, err := engineB.SyncTeam(ctx, remote, "team_1", teamKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(resB.Pulled) != 1 || resB.Pulled[0] != "team-github" {
		t.Fatalf("expected a pull, got %+v", resB)
	}

	if _, err := storeB.Get("team-github"); err != nil {
		t.Fatal(err)
	}
	value, err := secretsB.Get("team-token")
	if err != nil {
		t.Fatal(err)
	}
	if value != "ghp_teamsecret" {
		t.Errorf("team secret did not sync: %q", value)
	}
}

func TestTeamAADPreventsCrossTeamReplay(t *testing.T) {
	key := testKey(t, "team-passphrase")
	env, err := key.Seal([]byte("team-x-only"), TeamAAD("team_x", "github", 1))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := key.Open(env, TeamAAD("team_y", "github", 1)); err == nil {
		t.Error("a blob bound to one team must not open under another team")
	}
	if _, err := key.Open(env, TeamAAD("team_x", "filesystem", 1)); err == nil {
		t.Error("a blob bound to one server must not open as another server")
	}
	if _, err := key.Open(env, TeamAAD("team_x", "github", 2)); err == nil {
		t.Error("a blob bound to version 1 must not open as version 2")
	}
}

func TestTeamStoreRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := LoadTeams()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 {
		t.Error("expected no teams initially")
	}

	ref := TeamRef{TeamID: "team_1", Name: "Acme", Salt: "c2FsdA=="}
	if err := store.Add(ref); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadTeams()
	if err != nil {
		t.Fatal(err)
	}

	byID, err := reloaded.Get("team_1")
	if err != nil {
		t.Fatal(err)
	}
	if byID.Name != "Acme" {
		t.Errorf("unexpected team: %+v", byID)
	}

	byName, err := reloaded.Get("Acme")
	if err != nil {
		t.Fatal(err)
	}
	if byName.TeamID != "team_1" {
		t.Errorf("lookup by name failed: %+v", byName)
	}

	salt, err := byID.SaltBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(salt) != "salt" {
		t.Errorf("unexpected salt %q", salt)
	}

	if _, err := reloaded.Get("nope"); err == nil {
		t.Error("expected an error for an unknown team")
	}
}

func TestClientTeamEndpoints(t *testing.T) {
	var created bool

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/teams", func(w http.ResponseWriter, r *http.Request) {
		created = true
		writeStub(w, http.StatusCreated, map[string]any{
			"id": "team_1", "name": "Acme", "invite_code": "ABCD-EFGH",
			"salt": "c2FsdA==", "members": []string{"acct_1"},
		})
	})
	mux.HandleFunc("GET /v1/teams", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, map[string]any{"teams": []any{
			map[string]any{"id": "team_1", "name": "Acme", "salt": "c2FsdA=="},
		}})
	})
	mux.HandleFunc("POST /v1/teams/join", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, map[string]any{"id": "team_1", "name": "Acme", "salt": "c2FsdA=="})
	})
	mux.HandleFunc("GET /v1/teams/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "name": "Acme", "members": []string{"acct_1"}})
	})
	mux.HandleFunc("GET /v1/teams/{id}/blobs", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, map[string]any{"names": []string{"github"}})
	})
	mux.HandleFunc("GET /v1/teams/{id}/blobs/{name}", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, RemoteBlob{Name: r.PathValue("name"), Version: 3})
	})
	mux.HandleFunc("PUT /v1/teams/{id}/blobs/{name}", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, http.StatusOK, map[string]any{"ok": true})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx := context.Background()
	client := NewClient(ts.URL).WithToken("tok_1")

	team, err := client.CreateTeam(ctx, "Acme", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if !created || team.InviteCode != "ABCD-EFGH" {
		t.Errorf("unexpected team: %+v", team)
	}

	teams, err := client.ListTeams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 1 || teams[0].Name != "Acme" {
		t.Errorf("unexpected teams: %+v", teams)
	}

	if _, err := client.JoinTeam(ctx, "ABCD-EFGH"); err != nil {
		t.Fatal(err)
	}

	got, err := client.GetTeam(ctx, "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "team_1" {
		t.Errorf("unexpected team: %+v", got)
	}

	names, err := client.TeamBlobs(ctx, "team_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "github" {
		t.Errorf("unexpected names: %v", names)
	}

	blob, err := client.FetchTeamBlob(ctx, "team_1", "github")
	if err != nil {
		t.Fatal(err)
	}
	if blob.Version != 3 {
		t.Errorf("unexpected blob: %+v", blob)
	}

	if err := client.PushTeamBlob(ctx, "team_1", &RemoteBlob{Name: "github", Version: 1}); err != nil {
		t.Fatal(err)
	}
}
