package cloud

import (
	"errors"
	"time"
)

// Team key wrapping replaces the shared team passphrase.
//
// Each member device has an X25519 keypair. The team key is generated once, kept
// only by clients, and wrapped for each member's public key. The service stores
// the wrapped blobs — it cannot unwrap them, exactly as with team payloads.

// ErrKeyExists means another member already wrapped the team key for that device
// and the caller may not overwrite it.
var ErrKeyExists = errors.New("that device already has a wrapped team key")

// TeamKeyRecord is one member device's wrapped copy of the team key. A member
// signed in on two machines has two records, which is what lets each machine
// unwrap independently.
type TeamKeyRecord struct {
	AccountID  string    `json:"account_id"`
	PublicKey  string    `json:"public_key"`
	WrappedKey string    `json:"wrapped_key,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// HasKey reports whether the team key has been wrapped for this device.
func (r TeamKeyRecord) HasKey() bool { return r.WrappedKey != "" }

// ensureTeamKeys makes the per-team key map usable after loading an older store.
func (s *Store) ensureTeamKeysLocked(team *Team) {
	if team.Keys == nil {
		team.Keys = map[string]*TeamKeyRecord{}
	}
}

// SetTeamKey records (or updates) one device's public key and, when supplied,
// its wrapped team key.
//
// A member may always manage their own device. Granting to somebody else's
// device is allowed for any member — that is how a newcomer receives the key —
// but overwriting an existing wrapped key needs `force` and either ownership of
// the team or of that device, so one member cannot break another's access.
func (s *Store) SetTeamKey(teamID, callerID, accountID, publicKey, wrappedKey string, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	team, ok := s.data.Teams[teamID]
	if !ok {
		return ErrNotFound
	}
	if !team.IsMember(callerID) || !team.IsMember(accountID) {
		return ErrUnauthorized
	}
	s.ensureTeamKeysLocked(team)

	record, exists := team.Keys[publicKey]
	if !exists {
		record = &TeamKeyRecord{AccountID: accountID, PublicKey: publicKey}
		team.Keys[publicKey] = record
	} else if record.AccountID != accountID {
		// A public key belongs to exactly one account.
		return ErrUnauthorized
	}

	if wrappedKey != "" {
		if record.WrappedKey != "" && record.WrappedKey != wrappedKey {
			// Overwriting is how a rotation lands, and how a newcomer's key is
			// fixed if it was granted wrong. Only the team owner or the device's
			// own account may do it, so no member can lock another out.
			if !force || (callerID != team.OwnerID && callerID != record.AccountID) {
				return ErrKeyExists
			}
		}
		record.WrappedKey = wrappedKey
	}
	record.UpdatedAt = time.Now().UTC()

	return s.saveLocked()
}

// TeamKeys returns every device record for a team, oldest first.
func (s *Store) TeamKeys(teamID string) ([]TeamKeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	team, ok := s.data.Teams[teamID]
	if !ok {
		return nil, ErrNotFound
	}
	s.ensureTeamKeysLocked(team)

	records := make([]TeamKeyRecord, 0, len(team.Keys))
	for _, record := range team.Keys {
		records = append(records, *record)
	}
	sortRecords(records)
	return records, nil
}

func sortRecords(records []TeamKeyRecord) {
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && records[j].AccountID < records[j-1].AccountID; j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}
}
