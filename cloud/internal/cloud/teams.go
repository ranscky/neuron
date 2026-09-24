package cloud

import (
	"encoding/base64"
	"errors"
	"sort"
	"time"
)

// Team is a group of accounts that share a curated set of MCP servers.
//
// Salt is key-derivation material for the team key and is not secret. The team
// passphrase never reaches the service, so a shared blob is exactly as opaque
// to it as a personal one.
type Team struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OwnerID    string    `json:"owner_id"`
	InviteCode string    `json:"invite_code"`
	Salt       string    `json:"salt"`
	CreatedAt  time.Time `json:"created_at"`
	Members    []string  `json:"members"`
}

// IsMember reports whether an account belongs to the team.
func (t *Team) IsMember(accountID string) bool {
	for _, member := range t.Members {
		if member == accountID {
			return true
		}
	}
	return false
}

// CreateTeam creates a team owned by accountID and returns it, invite code
// included.
func (s *Store) CreateTeam(ownerID, name string, salt []byte) (*Team, error) {
	if len(salt) == 0 {
		return nil, errors.New("salt must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Accounts[ownerID]; !ok {
		return nil, ErrNotFound
	}

	id, err := randomID("team_", 12)
	if err != nil {
		return nil, err
	}
	invite, err := randomUserCode()
	if err != nil {
		return nil, err
	}

	team := &Team{
		ID:         id,
		Name:       name,
		OwnerID:    ownerID,
		InviteCode: invite,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		CreatedAt:  time.Now().UTC(),
		Members:    []string{ownerID},
	}
	s.data.Teams[id] = team
	s.data.TeamInvites[invite] = id

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneTeam(team), nil
}

// GetTeam returns a team by id.
func (s *Store) GetTeam(teamID string) (*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	team, ok := s.data.Teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneTeam(team), nil
}

// ListTeams returns the teams an account belongs to, ordered by name.
func (s *Store) ListTeams(accountID string) ([]*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*Team
	for _, team := range s.data.Teams {
		if team.IsMember(accountID) {
			out = append(out, cloneTeam(team))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// JoinTeamByInvite adds an account to the team that owns an invite code.
// Joining twice is a no-op rather than an error.
func (s *Store) JoinTeamByInvite(accountID, inviteCode string) (*Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Accounts[accountID]; !ok {
		return nil, ErrNotFound
	}

	teamID, ok := s.data.TeamInvites[inviteCode]
	if !ok {
		return nil, ErrNotFound
	}
	team, ok := s.data.Teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}

	if !team.IsMember(accountID) {
		team.Members = append(team.Members, accountID)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return cloneTeam(team), nil
}

// ListTeamBlobs returns the names shared with a team.
func (s *Store) ListTeamBlobs(teamID, accountID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.teamForMemberLocked(teamID, accountID); err != nil {
		return nil, err
	}

	blobs := s.data.TeamBlobs[teamID]
	names := make([]string, 0, len(blobs))
	for name := range blobs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// GetTeamBlob returns one shared blob.
func (s *Store) GetTeamBlob(teamID, accountID, name string) (*Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.teamForMemberLocked(teamID, accountID); err != nil {
		return nil, err
	}
	blob, ok := s.data.TeamBlobs[teamID][name]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *blob
	return &cp, nil
}

// PutTeamBlob stores a shared blob. Versions must strictly increase, the same
// rule as personal blobs, so a stale member cannot roll the team's config back.
func (s *Store) PutTeamBlob(teamID, accountID string, blob *Blob) error {
	if blob.Name == "" {
		return errors.New("blob name must not be empty")
	}
	if len(blob.Envelope) == 0 {
		return errors.New("blob envelope must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.teamForMemberLocked(teamID, accountID); err != nil {
		return err
	}
	if s.data.TeamBlobs[teamID] == nil {
		s.data.TeamBlobs[teamID] = map[string]*Blob{}
	}
	if existing, ok := s.data.TeamBlobs[teamID][blob.Name]; ok && blob.Version <= existing.Version {
		return ErrStaleVersion
	}

	if blob.UpdatedAt.IsZero() {
		blob.UpdatedAt = time.Now().UTC()
	}
	cp := *blob
	s.data.TeamBlobs[teamID][blob.Name] = &cp
	return s.saveLocked()
}

// teamForMemberLocked resolves a team and checks membership. Callers must hold
// the lock.
func (s *Store) teamForMemberLocked(teamID, accountID string) (*Team, error) {
	team, ok := s.data.Teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	if !team.IsMember(accountID) {
		// Deliberately not revealing whether the team exists.
		return nil, ErrUnauthorized
	}
	return team, nil
}

func cloneTeam(team *Team) *Team {
	cp := *team
	cp.Members = append([]string(nil), team.Members...)
	return &cp
}
