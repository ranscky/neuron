package sync

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// TeamRef is a team this machine has joined, remembered locally.
type TeamRef struct {
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
	Salt   string `json:"salt"`
}

// SaltBytes decodes the team's key-derivation salt.
func (r TeamRef) SaltBytes() ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(r.Salt)
	if err != nil {
		return nil, fmt.Errorf("team %s has an invalid salt: %w", r.Name, err)
	}
	if len(salt) == 0 {
		return nil, fmt.Errorf("team %s has an empty salt", r.Name)
	}
	return salt, nil
}

// TeamPassphraseKey is the keychain entry holding a team's passphrase. The
// passphrase is never written to a file.
func TeamPassphraseKey(teamID string) string {
	return "neuron-team-passphrase:" + teamID
}

// TeamStore is the local record of joined teams.
type TeamStore struct {
	path  string
	Teams map[string]TeamRef
}

func teamsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".neuron", "teams.json"), nil
}

// LoadTeams reads the local team list.
func LoadTeams() (*TeamStore, error) {
	path, err := teamsPath()
	if err != nil {
		return nil, err
	}
	store := &TeamStore{path: path, Teams: map[string]TeamRef{}}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read teams file: %w", err)
	}
	if len(raw) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(raw, &store.Teams); err != nil {
		return nil, fmt.Errorf("parse teams file: %w", err)
	}
	if store.Teams == nil {
		store.Teams = map[string]TeamRef{}
	}
	return store, nil
}

// Save writes the local team list, readable only by the user.
func (s *TeamStore) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.Teams, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(raw, '\n'), 0o600)
}

// Add records a joined team.
func (s *TeamStore) Add(ref TeamRef) error {
	s.Teams[ref.TeamID] = ref
	return s.Save()
}

// Remove forgets a team locally.
func (s *TeamStore) Remove(teamID string) error {
	delete(s.Teams, teamID)
	return s.Save()
}

// Get finds a team by id or by name, so the CLI can accept either.
func (s *TeamStore) Get(idOrName string) (TeamRef, error) {
	if ref, ok := s.Teams[idOrName]; ok {
		return ref, nil
	}
	for _, ref := range s.Teams {
		if ref.Name == idOrName {
			return ref, nil
		}
	}
	return TeamRef{}, fmt.Errorf("team %q is not known on this machine", idOrName)
}

// List returns joined teams ordered by name.
func (s *TeamStore) List() []TeamRef {
	out := make([]TeamRef, 0, len(s.Teams))
	for _, ref := range s.Teams {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
