package cloud

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRequestBody bounds any request. Synced blobs are ciphertext and can be
// large, but 4 MB is already far beyond a sane MCP configuration.
const maxRequestBody = 4 << 20

// Server is the Neuron sync service.
type Server struct {
	Store *Store
	// WebhookSecret is the Stripe signing secret. An empty value disables
	// billing entirely rather than accepting unverified webhooks.
	WebhookSecret string
	// RequirePro gates encrypted sync behind the paid plan. It is a field so a
	// self-hoster can turn the paywall off.
	RequirePro bool

	logger    *log.Logger
	limiter   *RateLimiter
	accessTTL time.Duration
	deviceTTL time.Duration
	now       func() time.Time
}

// NewServer builds a service with production defaults.
func NewServer(store *Store, webhookSecret string, logger *log.Logger) *Server {
	return &Server{
		Store:         store,
		WebhookSecret: webhookSecret,
		RequirePro:    true,
		logger:        logger,
		limiter:       NewRateLimiter(5, 20),
		accessTTL:     time.Hour,
		deviceTTL:     10 * time.Minute,
		now:           time.Now,
	}
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/accounts", s.handleCreateAccount)
	mux.HandleFunc("POST /v1/device/code", s.handleDeviceCode)
	mux.HandleFunc("POST /v1/device/approve", s.handleDeviceApprove)
	mux.HandleFunc("POST /v1/device/token", s.handleDeviceToken)
	mux.HandleFunc("GET /v1/account", s.auth(s.handleAccount))
	mux.HandleFunc("POST /v1/logout", s.auth(s.handleLogout))
	mux.HandleFunc("GET /v1/sync/blobs", s.auth(s.handleListBlobs))
	mux.HandleFunc("GET /v1/sync/blobs/{name}", s.auth(s.handleGetBlob))
	mux.HandleFunc("PUT /v1/sync/blobs/{name}", s.auth(s.handlePutBlob))
	mux.HandleFunc("POST /v1/billing/webhook", s.handleWebhook)

	// Team-shared configuration.
	mux.HandleFunc("POST /v1/teams", s.auth(s.handleCreateTeam))
	mux.HandleFunc("GET /v1/teams", s.auth(s.handleListTeams))
	mux.HandleFunc("POST /v1/teams/join", s.auth(s.handleJoinTeam))
	mux.HandleFunc("GET /v1/teams/{id}", s.auth(s.handleGetTeam))
	mux.HandleFunc("GET /v1/teams/{id}/blobs", s.auth(s.handleListTeamBlobs))
	mux.HandleFunc("GET /v1/teams/{id}/blobs/{name}", s.auth(s.handleGetTeamBlob))
	mux.HandleFunc("PUT /v1/teams/{id}/blobs/{name}", s.auth(s.handlePutTeamBlob))

	return s.recoverPanics(mux)
}

// --- middleware -------------------------------------------------------------

type accountHandler func(w http.ResponseWriter, r *http.Request, accountID string)

func (s *Server) auth(next accountHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		accountID, err := s.Store.ResolveToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next(w, r, accountID)
	}
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if s.logger != nil {
					s.logger.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				}
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// allow applies rate limiting, writing a 429 and returning false when the
// caller has exhausted its bucket.
func (s *Server) allow(w http.ResponseWriter, r *http.Request, key string) bool {
	if s.limiter.Allow(key) {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(1/s.limiter.rate)+1))
	writeError(w, http.StatusTooManyRequests, "too many requests")
	return false
}

// --- handlers ---------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, "accounts:"+clientIP(r)) {
		return
	}

	var req struct {
		Email string `json:"email"`
		Salt  string `json:"salt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	salt, err := decodeSalt(req.Salt)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	account, err := s.Store.CreateAccount(req.Email, salt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"account_id": account.ID,
		"salt":       account.Salt,
		"plan":       account.Plan,
	})
}

func (s *Server) handleDeviceCode(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, "device:"+clientIP(r)) {
		return
	}

	code, err := s.Store.CreateDeviceCode(s.deviceTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create device code")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":      code.DeviceCode,
		"user_code":        code.UserCode,
		"verification_uri": "/activate",
		"expires_in":       int(s.deviceTTL.Seconds()),
		"interval":         2,
	})
}

func (s *Server) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, "approve:"+clientIP(r)) {
		return
	}

	var req struct {
		UserCode  string `json:"user_code"`
		AccountID string `json:"account_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// User codes are printed uppercase; accept whatever case was typed.
	err := s.Store.ApproveDeviceCode(strings.ToUpper(strings.TrimSpace(req.UserCode)), req.AccountID)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"approved": true})
	case errors.Is(err, ErrExpired):
		writeError(w, http.StatusGone, "that code has expired")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "unknown or already-used code")
	default:
		writeError(w, http.StatusInternalServerError, "could not approve code")
	}
}

func (s *Server) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, "token:"+clientIP(r)) {
		return
	}

	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	token, err := s.Store.PollDeviceCode(req.DeviceCode, s.accessTTL)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   int(s.accessTTL.Seconds()),
		})
	case errors.Is(err, ErrPending):
		// 428 tells the client to keep polling rather than treat it as fatal.
		writeError(w, http.StatusPreconditionRequired, "authorization_pending")
	case errors.Is(err, ErrExpired):
		writeError(w, http.StatusGone, "device code expired")
	default:
		writeError(w, http.StatusNotFound, "unknown device code")
	}
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request, accountID string) {
	account, err := s.Store.GetAccount(accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	// Minimum fields only. Stripe identifiers are deliberately not returned:
	// no client needs them, so they never leave the service.
	writeJSON(w, http.StatusOK, map[string]any{
		"account_id": account.ID,
		"email":      account.Email,
		"plan":       account.Plan,
		"salt":       account.Salt,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, accountID string) {
	if err := s.Store.RevokeToken(bearerToken(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "logged out"})
}

func (s *Server) handleListBlobs(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	names, err := s.Store.ListBlobs(accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list blobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"names": names})
}

func (s *Server) handleGetBlob(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
		return
	}
	blob, err := s.Store.GetBlob(accountID, r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no blob for that server")
		return
	}
	writeJSON(w, http.StatusOK, blob)
}

func (s *Server) handlePutBlob(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireSync(w, accountID) {
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

	err := s.Store.PutBlob(accountID, &Blob{Name: name, Version: req.Version, Envelope: req.Envelope})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "version": req.Version})
	case errors.Is(err, ErrStaleVersion):
		writeError(w, http.StatusConflict, "a newer version already exists")
	default:
		writeError(w, http.StatusInternalServerError, "could not store blob")
	}
}

// requireSync enforces the paywall. Sync is the paid feature; local
// configuration management is not, and never touches this service.
func (s *Server) requireSync(w http.ResponseWriter, accountID string) bool {
	account, err := s.Store.GetAccount(accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return false
	}
	if s.RequirePro && account.Plan != PlanPro {
		writeJSON(w, http.StatusPaymentRequired, map[string]any{
			"error": "encrypted sync requires a paid plan",
			"plan":  account.Plan,
		})
		return false
	}
	return true
}

type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ClientReferenceID string            `json:"client_reference_id"`
			Customer          string            `json:"customer"`
			Subscription      string            `json:"subscription"`
			Metadata          map[string]string `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}

	if err := VerifyStripeSignature(body, r.Header.Get("Stripe-Signature"), s.WebhookSecret, DefaultSignatureTolerance, s.now()); err != nil {
		// Deliberately generic: do not tell an attacker which check failed.
		writeError(w, http.StatusBadRequest, "invalid webhook signature")
		return
	}

	var event stripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid event payload")
		return
	}
	if event.ID == "" {
		writeError(w, http.StatusBadRequest, "event id is required")
		return
	}

	// Stripe retries aggressively; applying an event twice must be a no-op.
	if s.Store.HasProcessedEvent(event.ID) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "already processed"})
		return
	}

	accountID := s.resolveAccount(event)

	switch event.Type {
	case "checkout.session.completed", "customer.subscription.updated", "invoice.paid":
		if accountID != "" {
			if err := s.Store.SetPlan(accountID, PlanPro); err != nil && !errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusInternalServerError, "could not update plan")
				return
			}
			if event.Data.Object.Customer != "" {
				if err := s.Store.SetStripe(accountID, event.Data.Object.Customer, event.Data.Object.Subscription); err != nil && !errors.Is(err, ErrNotFound) {
					writeError(w, http.StatusInternalServerError, "could not link subscription")
					return
				}
			}
		}
	case "customer.subscription.deleted":
		if accountID != "" {
			if err := s.Store.SetPlan(accountID, PlanFree); err != nil && !errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusInternalServerError, "could not downgrade plan")
				return
			}
		}
	}

	if err := s.Store.MarkEventProcessed(event.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record event")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// resolveAccount finds the account a billing event belongs to, preferring our
// own metadata and falling back to the Stripe customer id.
func (s *Server) resolveAccount(event stripeEvent) string {
	if id := event.Data.Object.Metadata["account_id"]; id != "" {
		return id
	}
	if id := event.Data.Object.ClientReferenceID; id != "" {
		return id
	}
	if event.Data.Object.Customer != "" {
		if id, err := s.Store.FindAccountByStripeCustomer(event.Data.Object.Customer); err == nil {
			return id
		}
	}
	return ""
}

// --- helpers ----------------------------------------------------------------

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func decodeSalt(encoded string) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, errors.New("salt must be base64 encoded")
	}
	if len(salt) < 8 {
		return nil, errors.New("salt must decode to at least 8 bytes")
	}
	return salt, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
