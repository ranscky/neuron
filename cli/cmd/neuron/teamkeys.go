package main

// Team key distribution helpers.
//
// A team's key is random and lives only on member devices. The service stores one
// record per device: the device's X25519 public key and the team key wrapped for
// it. Joining registers a public key; any member who holds the key then wraps it
// for the newcomer on their next sync, so nobody has to share a passphrase.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ranscky/neuron/internal/sync"
	"github.com/ranscky/neuron/pkg/secrets"
)

// teamKeyFor resolves the key a team's shared payloads are sealed with.
//
// A team with a salt is a legacy passphrase team; a team without one wraps its
// key per member device.
func teamKeyFor(ctx context.Context, client *sync.Client, ref sync.TeamRef, kv *secrets.Store, passphraseFlag string) (*sync.Key, error) {
	if ref.Salt != "" {
		passphrase, err := teamPassphrase(passphraseFlag, ref.TeamID, kv)
		if err != nil {
			return nil, err
		}
		salt, err := ref.SaltBytes()
		if err != nil {
			return nil, err
		}
		return sync.NewKey(passphrase, salt)
	}

	teamKey, err := unwrapTeamKey(ctx, client, ref.TeamID, kv)
	if err != nil {
		return nil, err
	}
	return sync.KeyFromRaw(teamKey)
}

// unwrapTeamKey fetches this device's wrapped team key, unwraps it, and grants
// the key to any member device that does not have one yet.
func unwrapTeamKey(ctx context.Context, client *sync.Client, teamID string, kv *secrets.Store) ([]byte, error) {
	member, err := sync.LoadOrCreateMemberKey(kv)
	if err != nil {
		return nil, err
	}
	ownKey := member.PublicKeyBase64()

	keys, err := client.TeamKeys(ctx, teamID)
	if err != nil {
		return nil, err
	}

	var mine *sync.TeamKey
	for i := range keys {
		if keys[i].PublicKey == ownKey {
			mine = &keys[i]
			break
		}
	}
	if mine == nil {
		// This device is not registered yet (for example a team joined before
		// the device had a key). Register, then ask a teammate to sync.
		if err := client.PutTeamKey(ctx, teamID, ownKey, ""); err != nil {
			return nil, fmt.Errorf("could not register this device: %w", err)
		}
		return nil, errors.New("this device is registered but has no team key yet — ask a teammate to run `neuron team sync <team>`")
	}
	if mine.WrappedKey == "" {
		return nil, errors.New("no team key for this device yet — ask a teammate to run `neuron team sync <team>`")
	}

	teamKey, err := member.UnwrapTeamKey(mine.WrappedKey, teamID)
	if err != nil {
		return nil, err
	}

	// Any holder of the key can grant it. Proactive granting is what makes
	// joining work without a shared passphrase.
	for _, k := range keys {
		if k.HasKey || k.PublicKey == "" || k.PublicKey == ownKey {
			continue
		}
		wrapped, err := sync.WrapTeamKey(k.PublicKey, teamKey, teamID)
		if err != nil {
			continue
		}
		_ = client.GrantTeamKey(ctx, teamID, k.PublicKey, wrapped, false)
	}

	return teamKey, nil
}

// createWrappedTeam creates a team whose key is wrapped for this device, and
// records it locally.
func createWrappedTeam(ctx context.Context, client *sync.Client, kv *secrets.Store, name string, teams *sync.TeamStore) (*sync.Team, error) {
	member, err := sync.LoadOrCreateMemberKey(kv)
	if err != nil {
		return nil, err
	}
	teamKey, err := sync.NewTeamKey()
	if err != nil {
		return nil, err
	}

	team, err := client.CreateTeam(ctx, name, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create the team: %w", err)
	}

	wrapped, err := sync.WrapTeamKey(member.PublicKeyBase64(), teamKey, team.ID)
	if err != nil {
		return nil, err
	}
	if err := client.PutTeamKey(ctx, team.ID, member.PublicKeyBase64(), wrapped); err != nil {
		return nil, fmt.Errorf("could not register this device's team key: %w", err)
	}
	if err := sync.SaveTeamKey(kv, team.ID, teamKey); err != nil {
		return nil, err
	}
	if err := teams.Add(sync.TeamRef{TeamID: team.ID, Name: team.Name}); err != nil {
		return nil, fmt.Errorf("could not save the team locally: %w", err)
	}
	return team, nil
}

// registerWrappedMember records this device in a joined team and remembers the
// team locally. The team key itself arrives when a teammate syncs.
func registerWrappedMember(ctx context.Context, client *sync.Client, kv *secrets.Store, team *sync.Team, teams *sync.TeamStore) error {
	member, err := sync.LoadOrCreateMemberKey(kv)
	if err != nil {
		return err
	}
	if err := client.PutTeamKey(ctx, team.ID, member.PublicKeyBase64(), ""); err != nil {
		return fmt.Errorf("could not register this device: %w", err)
	}
	if err := teams.Add(sync.TeamRef{TeamID: team.ID, Name: team.Name}); err != nil {
		return fmt.Errorf("could not save the team locally: %w", err)
	}
	return nil
}

// rotateTeamKey replaces a wrapped team's key and re-wraps it for every device
// currently in the team.
//
// Payloads already shared stay sealed with the previous key: rotation protects
// what is shared from now on, so a member who left cannot read future updates.
// Re-encrypting history is a separate step and is not done here.
func rotateTeamKey(ctx context.Context, client *sync.Client, kv *secrets.Store, ref sync.TeamRef) (int, error) {
	if ref.Salt != "" {
		return 0, errors.New("this team derives its key from a shared passphrase; rotation applies to wrapped teams")
	}

	teamKey, err := sync.NewTeamKey()
	if err != nil {
		return 0, err
	}

	keys, err := client.TeamKeys(ctx, ref.TeamID)
	if err != nil {
		return 0, err
	}

	granted := 0
	for _, k := range keys {
		if k.PublicKey == "" {
			continue
		}
		wrapped, err := sync.WrapTeamKey(k.PublicKey, teamKey, ref.TeamID)
		if err != nil {
			return granted, fmt.Errorf("wrap the new key for device %s: %w", shortKey(k.PublicKey), err)
		}
		if err := client.GrantTeamKey(ctx, ref.TeamID, k.PublicKey, wrapped, true); err != nil {
			return granted, fmt.Errorf("deliver the new key to device %s: %w", shortKey(k.PublicKey), err)
		}
		granted++
	}
	if granted == 0 {
		return 0, errors.New("no member devices are registered for this team")
	}

	if err := sync.SaveTeamKey(kv, ref.TeamID, teamKey); err != nil {
		return granted, err
	}
	return granted, nil
}

// shortKey renders a public key for messages without printing all of it.
func shortKey(publicKey string) string {
	if len(publicKey) <= 8 {
		return publicKey
	}
	return publicKey[:8] + "…"
}

// teamKeyMode describes how a team's key is distributed, for `team list`.
func teamKeyMode(ref sync.TeamRef) string {
	if ref.Salt != "" {
		return "passphrase"
	}
	return "wrapped"
}

// memberKeyFromEnv reports whether this machine gets its identity from the
// environment rather than the keychain, which is worth telling the user once.
func memberKeyFromEnv() bool { return os.Getenv(sync.MemberKeyEnv) != "" }
