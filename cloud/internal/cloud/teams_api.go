package cloud

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// requireTeam resolves a team the caller belongs to, writing the appropriate
// error and returning false otherwise.
func (s *Server) requireTeam(w http.ResponseWriter, accountID, teamID string) bool {
	team, err := s.Store.GetTeam(teamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such team")
		return false
	}
	if !team.IsMember(accountID) {
		writeError(w, http.StatusForbidden, "you are not a member of that team")
		return false
	}
	return true
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}

	var req struct {
		Name string `json:"name"`
		Salt string `json:"salt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "team name is required")
		return
	}
	salt, err := decodeSalt(req.Salt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	team, err := s.Store.CreateTeam(accountID, name, salt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create team")
		return
	}
	writeJSON(w, http.StatusCreated, team)
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}

	teams, err := s.Store.ListTeams(accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list teams")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (s *Server) handleJoinTeam(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}

	var req struct {
		InviteCode string `json:"invite_code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	code := strings.ToUpper(strings.TrimSpace(req.InviteCode))
	if code == "" {
		writeError(w, http.StatusBadRequest, "invite code is required")
		return
	}

	team, err := s.Store.JoinTeamByInvite(accountID, code)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "that invite code is not valid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not join team")
		return
	}
	writeJSON(w, http.StatusOK, team)
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request, accountID string) {
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	team, err := s.Store.GetTeam(teamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such team")
		return
	}
	writeJSON(w, http.StatusOK, team)
}

func (s *Server) handleListTeamBlobs(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	names, err := s.Store.ListTeamBlobs(teamID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list shared servers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"names": names})
}

func (s *Server) handleGetTeamBlob(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	blob, err := s.Store.GetTeamBlob(teamID, accountID, r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no shared server with that name")
		return
	}
	writeJSON(w, http.StatusOK, blob)
}

func (s *Server) handlePutTeamBlob(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	teamID := r.PathValue("id")
	if !s.requireTeam(w, accountID, teamID) {
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "server name is required")
		return
	}

	var req struct {
		Version  int64           `json:"version"`
		Envelope json.RawMessage `json:"envelope"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version <= 0 {
		writeError(w, http.StatusBadRequest, "version must be positive")
		return
	}
	if len(req.Envelope) == 0 {
		writeError(w, http.StatusBadRequest, "envelope is required")
		return
	}

	err := s.Store.PutTeamBlob(teamID, accountID, &Blob{Name: name, Version: req.Version, Envelope: req.Envelope})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "version": req.Version})
	case errors.Is(err, ErrStaleVersion):
		writeError(w, http.StatusConflict, "a newer version already exists")
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusForbidden, "you are not a member of that team")
	default:
		writeError(w, http.StatusInternalServerError, "could not store shared server")
	}
}
