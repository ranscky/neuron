package sync

import "context"

// reconcileTarget abstracts where blobs live: the account's own encrypted
// store, or a team's shared one. Keeping the reconciliation loop generic means
// personal and team sync cannot drift apart in behaviour.
type reconcileTarget struct {
	// namespace separates sync state, so a team's versions do not collide with
	// the same server name in the account's own store.
	namespace string
	list      func(ctx context.Context) ([]string, error)
	fetch     func(ctx context.Context, name string) (*RemoteBlob, error)
	push      func(ctx context.Context, blob *RemoteBlob) error
	aad       func(name string, version int64) []byte
}

func stateKey(namespace, name string) string {
	return namespace + "\x00" + name
}

// TeamRemote is the team-scoped half of a sync service.
type TeamRemote interface {
	CreateTeam(ctx context.Context, name string, salt []byte) (*Team, error)
	ListTeams(ctx context.Context) ([]Team, error)
	JoinTeam(ctx context.Context, inviteCode string) (*Team, error)
	GetTeam(ctx context.Context, teamID string) (*Team, error)
	TeamBlobs(ctx context.Context, teamID string) ([]string, error)
	FetchTeamBlob(ctx context.Context, teamID, name string) (*RemoteBlob, error)
	PushTeamBlob(ctx context.Context, teamID string, blob *RemoteBlob) error
}

// Team is a shared group as the service describes it. Salt is key-derivation
// material for the team key; the team passphrase never leaves the client.
type Team struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	OwnerID    string   `json:"owner_id"`
	InviteCode string   `json:"invite_code,omitempty"`
	Salt       string   `json:"salt"`
	Members    []string `json:"members"`
}
