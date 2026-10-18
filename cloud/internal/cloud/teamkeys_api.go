package cloud

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

// Team key endpoints.
//
// A member device registers its X25519 public key and receives the team key
// wrapped for it. The service never sees the team key itself: every wrapped_key
// it stores is opaque ciphertext, so a compromised service cannot read a shared
// server either.

// handlePutTeamKey records the caller's device public key and, when supplied,
// its wrapped team key.
func (s *Server) handlePutTeamKey(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	var req struct {
		PublicKey  string `json:"public_key"`
		WrappedKey string `json:"wrapped_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	publicKey, err := decodePublicKey(req.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wrapped, err := decodeWrappedKey(req.WrappedKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Registering our own device: never needs force, since it is our own record.
	if err := s.Store.SetTeamKey(teamID, accountID, accountID, publicKey, wrapped, true); err != nil {
		writeKeyError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"public_key": publicKey,
		"has_key":    wrapped != "",
	})
}

// handleGrantTeamKey wraps the team key for another member device. Any member may
// grant — that is how a newcomer receives the key — but replacing an existing
// wrapped key needs force and ownership (see SetTeamKey).
func (s *Server) handleGrantTeamKey(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	publicKey, err := decodePublicKey(r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req struct {
		WrappedKey string `json:"wrapped_key"`
		Force      bool   `json:"force"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	wrapped, err := decodeWrappedKey(req.WrappedKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if wrapped == "" {
		writeError(w, http.StatusBadRequest, "wrapped_key is required")
		return
	}

	records, err := s.Store.TeamKeys(teamID)
	if err != nil {
		writeKeyError(w, err)
		return
	}
	target := findKeyRecord(records, publicKey)
	if target == nil {
		writeError(w, http.StatusNotFound, "no such device in that team")
		return
	}

	if err := s.Store.SetTeamKey(teamID, accountID, target.AccountID, publicKey, wrapped, req.Force); err != nil {
		writeKeyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"granted":    true,
		"account_id": target.AccountID,
		"public_key": publicKey,
	})
}

// handleListTeamKeys returns the roster. A wrapped key is only ever returned to
// the account it belongs to.
func (s *Server) handleListTeamKeys(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	records, err := s.Store.TeamKeys(teamID)
	if err != nil {
		writeKeyError(w, err)
		return
	}

	keys := make([]map[string]any, 0, len(records))
	for _, record := range records {
		item := map[string]any{
			"account_id": record.AccountID,
			"public_key": record.PublicKey,
			"has_key":    record.HasKey(),
		}
		if record.AccountID == accountID {
			item["wrapped_key"] = record.WrappedKey
		}
		keys = append(keys, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func findKeyRecord(records []TeamKeyRecord, publicKey string) *TeamKeyRecord {
	for i := range records {
		if records[i].PublicKey == publicKey {
			return &records[i]
		}
	}
	return nil
}

func writeKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "no such team")
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusForbidden, "you are not a member of that team")
	case errors.Is(err, ErrKeyExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "could not update the team key")
	}
}

// decodePublicKey validates and normalises an X25519 public key.
//
// The canonical form is URL-safe base64, because a public key appears in a path
// segment. Standard base64 is accepted on the way in and converted, so a client
// using the wrong alphabet is not silently broken.
func decodePublicKey(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	var raw []byte
	var lastErr error
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		decoded, err := encoding.DecodeString(trimmed)
		if err != nil {
			lastErr = err
			continue
		}
		raw = decoded
		lastErr = nil
		break
	}
	if lastErr != nil {
		return "", errors.New("public_key must be base64")
	}
	if len(raw) != 32 {
		return "", errors.New("public_key must be a 32-byte X25519 key")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// decodeWrappedKey validates a wrapped key. An empty value means "register the
// device only".
func decodeWrappedKey(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", errors.New("wrapped_key must be base64")
	}
	if len(raw) < 32 || len(raw) > 512 {
		return "", errors.New("wrapped_key has an implausible size")
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
