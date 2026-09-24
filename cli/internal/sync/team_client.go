package sync

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
)

// Client implements TeamRemote, so the sync engine can reconcile a team's
// shared configuration with the same code path it uses for personal sync.

// CreateTeam creates a team owned by the signed-in account. The salt is
// key-derivation material for the team key and is not secret.
func (c *Client) CreateTeam(ctx context.Context, name string, salt []byte) (*Team, error) {
	var out Team
	payload := map[string]string{
		"name": name,
		"salt": base64.StdEncoding.EncodeToString(salt),
	}
	if err := c.do(ctx, http.MethodPost, "/v1/teams", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTeams returns the teams the account belongs to.
func (c *Client) ListTeams(ctx context.Context) ([]Team, error) {
	var out struct {
		Teams []Team `json:"teams"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/teams", nil, &out); err != nil {
		return nil, err
	}
	return out.Teams, nil
}

// JoinTeam joins a team with an invite code.
func (c *Client) JoinTeam(ctx context.Context, inviteCode string) (*Team, error) {
	var out Team
	payload := map[string]string{"invite_code": inviteCode}
	if err := c.do(ctx, http.MethodPost, "/v1/teams/join", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTeam returns one team.
func (c *Client) GetTeam(ctx context.Context, teamID string) (*Team, error) {
	var out Team
	if err := c.do(ctx, http.MethodGet, "/v1/teams/"+url.PathEscape(teamID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TeamBlobs lists the servers shared with a team.
func (c *Client) TeamBlobs(ctx context.Context, teamID string) ([]string, error) {
	var out struct {
		Names []string `json:"names"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/teams/"+url.PathEscape(teamID)+"/blobs", nil, &out); err != nil {
		return nil, err
	}
	return out.Names, nil
}

// FetchTeamBlob fetches one shared server.
func (c *Client) FetchTeamBlob(ctx context.Context, teamID, name string) (*RemoteBlob, error) {
	var out RemoteBlob
	if err := c.do(ctx, http.MethodGet, teamPath(teamID, name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PushTeamBlob publishes one shared server.
func (c *Client) PushTeamBlob(ctx context.Context, teamID string, blob *RemoteBlob) error {
	payload := map[string]interface{}{
		"version":  blob.Version,
		"envelope": blob.Envelope,
	}
	return c.do(ctx, http.MethodPut, teamPath(teamID, blob.Name), payload, nil)
}

func teamPath(teamID, name string) string {
	return "/v1/teams/" + url.PathEscape(teamID) + "/blobs/" + url.PathEscape(name)
}
