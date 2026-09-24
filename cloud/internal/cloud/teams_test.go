package cloud

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func saltB64() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
}

func signInPro(t *testing.T, server *Server, ts *httptest.Server) (token, accountID string) {
	t.Helper()
	token, accountID = signIn(t, ts)
	if err := server.Store.SetPlan(accountID, PlanPro); err != nil {
		t.Fatal(err)
	}
	return token, accountID
}

func TestStoreTeamMembershipAndInvites(t *testing.T) {
	store := testStore(t)
	owner, err := store.CreateAccount("owner@example.com", []byte("salt-owner"))
	if err != nil {
		t.Fatal(err)
	}
	member, err := store.CreateAccount("member@example.com", []byte("salt-member"))
	if err != nil {
		t.Fatal(err)
	}

	team, err := store.CreateTeam(owner.ID, "Acme", []byte("team-salt-1234"))
	if err != nil {
		t.Fatal(err)
	}
	if !team.IsMember(owner.ID) {
		t.Error("the creator should be a member")
	}
	if team.InviteCode == "" {
		t.Error("a team should have an invite code")
	}

	// A team is only visible to its members.
	teams, err := store.ListTeams(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 0 {
		t.Errorf("expected no teams before joining, got %d", len(teams))
	}

	joined, err := store.JoinTeamByInvite(member.ID, team.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	if !joined.IsMember(member.ID) {
		t.Error("joining should add the member")
	}

	// Joining twice is a no-op, not a duplicate membership.
	again, err := store.JoinTeamByInvite(member.ID, team.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Members) != 2 {
		t.Errorf("expected exactly two members, got %v", again.Members)
	}

	if _, err := store.JoinTeamByInvite(member.ID, "NOPE-NOPE"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for a bad invite, got %v", err)
	}
}

func TestStoreTeamBlobsRequireMembership(t *testing.T) {
	store := testStore(t)
	owner, _ := store.CreateAccount("owner@example.com", []byte("salt-owner"))
	outsider, _ := store.CreateAccount("outsider@example.com", []byte("salt-outsider"))

	team, err := store.CreateTeam(owner.ID, "Acme", []byte("team-salt-1234"))
	if err != nil {
		t.Fatal(err)
	}

	blob := &Blob{Name: "github", Version: 1, Envelope: json.RawMessage(`{"v":1}`)}
	if err := store.PutTeamBlob(team.ID, owner.ID, blob); err != nil {
		t.Fatal(err)
	}

	// A non-member must be refused on every team-scoped read and write.
	if _, err := store.ListTeamBlobs(team.ID, outsider.ID); !errors.Is(err, ErrUnauthorized) {
		t.Error("an outsider must not list team blobs")
	}
	if _, err := store.GetTeamBlob(team.ID, outsider.ID, "github"); !errors.Is(err, ErrUnauthorized) {
		t.Error("an outsider must not read team blobs")
	}
	if err := store.PutTeamBlob(team.ID, outsider.ID, blob); !errors.Is(err, ErrUnauthorized) {
		t.Error("an outsider must not write team blobs")
	}
}

func TestTeamBlobVersionsMustIncrease(t *testing.T) {
	store := testStore(t)
	owner, _ := store.CreateAccount("owner@example.com", []byte("salt-owner"))
	team, _ := store.CreateTeam(owner.ID, "Acme", []byte("team-salt-1234"))

	if err := store.PutTeamBlob(team.ID, owner.ID, &Blob{Name: "fs", Version: 2, Envelope: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutTeamBlob(team.ID, owner.ID, &Blob{Name: "fs", Version: 2, Envelope: json.RawMessage(`{}`)}); !errors.Is(err, ErrStaleVersion) {
		t.Error("an equal version must be refused")
	}
	if err := store.PutTeamBlob(team.ID, owner.ID, &Blob{Name: "fs", Version: 3, Envelope: json.RawMessage(`{}`)}); err != nil {
		t.Errorf("a newer version must be accepted: %v", err)
	}
}

func TestTeamBlobsAreIsolatedPerTeam(t *testing.T) {
	store := testStore(t)
	owner, _ := store.CreateAccount("owner@example.com", []byte("salt-owner"))

	first, _ := store.CreateTeam(owner.ID, "First", []byte("team-salt-aaaa"))
	second, _ := store.CreateTeam(owner.ID, "Second", []byte("team-salt-bbbb"))

	if err := store.PutTeamBlob(first.ID, owner.ID, &Blob{Name: "github", Version: 1, Envelope: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetTeamBlob(second.ID, owner.ID, "github"); !errors.Is(err, ErrNotFound) {
		t.Error("one team must not see another team's blobs")
	}
}

func TestTeamEndpoints(t *testing.T) {
	server, ts := newTestServer(t)
	ownerToken, _ := signInPro(t, server, ts)
	memberToken, _ := signInPro(t, server, ts)

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/teams", ownerToken,
		map[string]string{"name": "Acme", "salt": saltB64()})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create team: %d %v", resp.StatusCode, body)
	}
	teamID, _ := body["id"].(string)
	invite, _ := body["invite_code"].(string)
	if teamID == "" || invite == "" {
		t.Fatalf("incomplete team payload: %v", body)
	}

	// A non-member must be refused, and must not learn the team exists.
	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID, memberToken, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 before joining, got %d", resp.StatusCode)
	}

	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/teams/join", memberToken,
		map[string]string{"invite_code": invite})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d", resp.StatusCode)
	}

	// The owner publishes a shared server; the member pulls it.
	put := map[string]any{"version": 1, "envelope": map[string]any{"v": 1, "data": "ZGF0YQ=="}}
	resp, _ = doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/blobs/github", ownerToken, put)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner put: %d", resp.StatusCode)
	}

	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID+"/blobs/github", memberToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member get: %d", resp.StatusCode)
	}
	if body["name"] != "github" {
		t.Errorf("unexpected shared blob: %v", body)
	}

	resp, _ = doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/blobs/github", ownerToken, put)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for a stale version, got %d", resp.StatusCode)
	}

	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID+"/blobs", memberToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list blobs: %d", resp.StatusCode)
	}
	if names, _ := body["names"].([]any); len(names) != 1 || names[0] != "github" {
		t.Errorf("unexpected shared blob list: %v", names)
	}

	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/teams", memberToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list teams: %d", resp.StatusCode)
	}
	teams, _ := body["teams"].([]any)
	if len(teams) != 1 {
		t.Fatalf("expected one team after joining, got %v", teams)
	}
	if teams[0].(map[string]any)["name"] != "Acme" {
		t.Errorf("unexpected team: %v", teams[0])
	}
}

func TestTeamEndpointsRejectABadInviteAndFreePlan(t *testing.T) {
	server, ts := newTestServer(t)

	freeToken, _ := signIn(t, ts)
	resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/teams", freeToken,
		map[string]string{"name": "Acme", "salt": saltB64()})
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Errorf("teams should be part of the paid plan, got %d", resp.StatusCode)
	}

	proToken, _ := signInPro(t, server, ts)
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/teams/join", proToken,
		map[string]string{"invite_code": "NOPE-NOPE"})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an invalid invite, got %d", resp.StatusCode)
	}

	// Unauthenticated access is rejected before anything else.
	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/v1/teams", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d", resp.StatusCode)
	}
}
