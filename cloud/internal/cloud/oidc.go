package cloud

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OIDC turns "paste your account id" into "sign in with your identity provider".
//
// The flow is the standard authorization code exchange with PKCE: the browser is
// sent to the provider, comes back with a code, the service exchanges it for an ID
// token, verifies that token against the provider's published keys, links it to an
// account by email, and approves the device code the CLI is waiting on. The CLI
// keeps polling `/v1/device/token` exactly as before.
//
// This is a scaffold: RS256 is the only ID-token signature accepted, and the
// client authenticates to the token endpoint with client_secret_post.

// OIDCConfig configures provider-backed sign-in. All three of issuer, client id
// and client secret are required for the flow; without them the endpoints answer
// 503 so an unconfigured service cannot be half-used.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is where the provider sends the browser back. Empty means
	// "<public base>/v1/auth/oidc/callback", resolved per request.
	RedirectURL string
}

// Enabled reports whether provider sign-in is configured.
func (c OIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.ClientSecret != ""
}

// oidcProvider is the part of the discovery document this service uses.
type oidcProvider struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// oidcFlow is one in-flight sign-in, kept in memory: a restart drops pending
// logins, which is a better trade than writing short-lived secrets to disk.
type oidcFlow struct {
	Verifier  string
	Nonce     string
	UserCode  string
	ExpiresAt time.Time
}

const oidcFlowTTL = 10 * time.Minute

// idTokenClaims is the subset of an ID token the service trusts.
type idTokenClaims struct {
	Issuer        string          `json:"iss"`
	Subject       string          `json:"sub"`
	Audience      json.RawMessage `json:"aud"`
	Expiry        int64           `json:"exp"`
	IssuedAt      int64           `json:"iat"`
	Nonce         string          `json:"nonce"`
	Email         string          `json:"email"`
	EmailVerified bool            `json:"email_verified"`
}

// Enabled reports whether provider sign-in is available on this server.
func (s *Server) oidcConfigured() bool { return s.OIDC.Enabled() }

// oidcRedirectURL resolves the callback URL, preferring explicit configuration.
func (s *Server) oidcRedirectURL(r *http.Request) string {
	if s.OIDC.RedirectURL != "" {
		return s.OIDC.RedirectURL
	}
	return strings.TrimRight(s.publicBaseURL(r), "/") + "/v1/auth/oidc/callback"
}

// handleOIDCStart sends the browser to the identity provider.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	if !s.oidcConfigured() {
		writeError(w, http.StatusServiceUnavailable, "provider sign-in is not configured")
		return
	}

	userCode := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("user_code")))
	if userCode == "" {
		writeError(w, http.StatusBadRequest, "user_code is required")
		return
	}

	provider, err := s.oidcDiscovery(r.Context())
	if err != nil {
		s.logOIDC("discovery failed: %v", err)
		writeError(w, http.StatusBadGateway, "could not reach the identity provider")
		return
	}

	state, err := randomID("", 24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}
	verifier, err := randomID("", 32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}
	nonce, err := randomID("", 16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start sign-in")
		return
	}

	s.rememberOIDCFlow(state, &oidcFlow{
		Verifier:  verifier,
		Nonce:     nonce,
		UserCode:  userCode,
		ExpiresAt: time.Now().UTC().Add(oidcFlowTTL),
	})

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", s.OIDC.ClientID)
	query.Set("redirect_uri", s.oidcRedirectURL(r))
	query.Set("scope", "openid email profile")
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("code_challenge", pkceChallenge(verifier))
	query.Set("code_challenge_method", "S256")

	http.Redirect(w, r, provider.AuthorizationEndpoint+"?"+query.Encode(), http.StatusFound)
}

// handleOIDCCallback finishes the exchange and approves the waiting device.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oidcConfigured() {
		writeError(w, http.StatusServiceUnavailable, "provider sign-in is not configured")
		return
	}

	flow, ok := s.takeOIDCFlow(r.URL.Query().Get("state"))
	if !ok {
		s.renderOIDC(w, http.StatusBadRequest, oidcView{
			Error: "That sign-in link is unknown or has expired. Start again from the terminal.",
		})
		return
	}
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		s.renderOIDC(w, http.StatusBadRequest, oidcView{
			Error: "The identity provider refused the sign-in: " + providerError,
		})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		s.renderOIDC(w, http.StatusBadRequest, oidcView{Error: "The provider returned no authorization code."})
		return
	}

	provider, err := s.oidcDiscovery(r.Context())
	if err != nil {
		s.logOIDC("discovery failed: %v", err)
		s.renderOIDC(w, http.StatusBadGateway, oidcView{Error: "Could not reach the identity provider."})
		return
	}

	idToken, err := s.oidcExchange(r.Context(), provider, code, flow.Verifier, s.oidcRedirectURL(r))
	if err != nil {
		s.logOIDC("token exchange failed: %v", err)
		s.renderOIDC(w, http.StatusBadGateway, oidcView{Error: "The identity provider rejected the sign-in."})
		return
	}

	claims, err := s.oidcVerifyIDToken(r.Context(), provider, idToken, flow.Nonce)
	if err != nil {
		s.logOIDC("id token rejected: %v", err)
		s.renderOIDC(w, http.StatusUnauthorized, oidcView{Error: "The identity provider's token could not be trusted."})
		return
	}

	accountID, err := s.linkOIDCAccount(claims)
	if err != nil {
		s.logOIDC("account linking failed: %v", err)
		s.renderOIDC(w, http.StatusInternalServerError, oidcView{Error: "Could not link that identity to an account."})
		return
	}

	if err := s.Store.ApproveDeviceCode(flow.UserCode, accountID); err != nil {
		s.renderOIDC(w, http.StatusGone, oidcView{
			Error: "Signed in, but the device code has expired. Run `neuron login` again.",
		})
		return
	}

	s.renderOIDC(w, http.StatusOK, oidcView{Approved: true, Email: claims.Email})
}

// --- discovery and keys -----------------------------------------------------

// oidcDiscovery fetches (and briefly caches) the provider's discovery document.
func (s *Server) oidcDiscovery(ctx context.Context) (*oidcProvider, error) {
	s.oidcMu.Lock()
	if s.oidcProvider != nil && time.Now().Before(s.oidcProviderUntil) {
		provider := s.oidcProvider
		s.oidcMu.Unlock()
		return provider, nil
	}
	s.oidcMu.Unlock()

	endpoint := strings.TrimRight(s.OIDC.Issuer, "/") + "/.well-known/openid-configuration"
	body, err := s.oidcGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	var provider oidcProvider
	if err := json.Unmarshal(body, &provider); err != nil {
		return nil, fmt.Errorf("parse discovery document: %w", err)
	}
	if provider.AuthorizationEndpoint == "" || provider.TokenEndpoint == "" || provider.JWKSURI == "" {
		return nil, errors.New("discovery document is missing endpoints")
	}
	if provider.Issuer == "" {
		provider.Issuer = s.OIDC.Issuer
	}
	if provider.Issuer != s.OIDC.Issuer {
		return nil, fmt.Errorf("discovery issuer %q does not match the configured issuer", provider.Issuer)
	}

	s.oidcMu.Lock()
	s.oidcProvider = &provider
	s.oidcProviderUntil = time.Now().Add(10 * time.Minute)
	s.oidcMu.Unlock()
	return &provider, nil
}

// oidcExchange swaps an authorization code for an ID token.
func (s *Server) oidcExchange(ctx context.Context, provider *oidcProvider, code, verifier, redirectURL string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURL)
	form.Set("client_id", s.OIDC.ClientID)
	form.Set("client_secret", s.OIDC.ClientSecret)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.oidcClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("call token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var token struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if token.IDToken == "" {
		return "", errors.New("token response has no id_token")
	}
	return token.IDToken, nil
}

// oidcVerifyIDToken checks the signature against the provider's JWKS and validates
// the claims that matter: issuer, audience, expiry and nonce.
func (s *Server) oidcVerifyIDToken(ctx context.Context, provider *oidcProvider, token, nonce string) (*idTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("id token is not a JWT")
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode id token header: %w", err)
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("parse id token header: %w", err)
	}
	if header.Algorithm != "RS256" {
		return nil, fmt.Errorf("unsupported id token algorithm %q", header.Algorithm)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode id token signature: %w", err)
	}

	key, err := s.oidcSigningKey(ctx, provider, header.KeyID)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return nil, errors.New("id token signature does not verify")
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode id token claims: %w", err)
	}
	var claims idTokenClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("parse id token claims: %w", err)
	}

	if claims.Issuer != provider.Issuer {
		return nil, fmt.Errorf("id token issuer %q does not match", claims.Issuer)
	}
	if !audienceContains(claims.Audience, s.OIDC.ClientID) {
		return nil, errors.New("id token audience does not include this client")
	}
	if claims.Expiry == 0 || time.Now().After(time.Unix(claims.Expiry, 0)) {
		return nil, errors.New("id token has expired")
	}
	if claims.Nonce == "" || claims.Nonce != nonce {
		return nil, errors.New("id token nonce does not match the sign-in")
	}
	if claims.Subject == "" {
		return nil, errors.New("id token has no subject")
	}
	return &claims, nil
}

// oidcSigningKey fetches the provider's JWKS and picks the RSA key to verify with.
func (s *Server) oidcSigningKey(ctx context.Context, provider *oidcProvider, keyID string) (*rsa.PublicKey, error) {
	body, err := s.oidcGet(ctx, provider.JWKSURI)
	if err != nil {
		return nil, err
	}

	var jwks struct {
		Keys []struct {
			Type string `json:"kty"`
			Key  string `json:"kid"`
			N    string `json:"n"`
			E    string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("parse jwks: %w", err)
	}

	for _, key := range jwks.Keys {
		if key.Type != "RSA" || (keyID != "" && key.Key != keyID) {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			continue
		}
		exponent, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			continue
		}
		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(modulus),
			E: int(new(big.Int).SetBytes(exponent).Int64()),
		}, nil
	}
	return nil, errors.New("no matching key in the provider's JWKS")
}

// linkOIDCAccount maps a verified identity to an account, creating one the first
// time it is seen. Only a verified email is used for linking, so one provider
// cannot claim another person's existing account.
func (s *Server) linkOIDCAccount(claims *idTokenClaims) (string, error) {
	if claims.Email != "" && claims.EmailVerified {
		if existing, err := s.Store.FindAccountByEmail(claims.Email); err == nil {
			return existing.ID, nil
		}
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	account, err := s.Store.CreateAccount(claims.Email, salt)
	if err != nil {
		return "", err
	}
	return account.ID, nil
}

// --- helpers ----------------------------------------------------------------

// rememberOIDCFlow stores a pending sign-in, pruning expired ones.
func (s *Server) rememberOIDCFlow(state string, flow *oidcFlow) {
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()

	if s.oidcFlows == nil {
		s.oidcFlows = map[string]*oidcFlow{}
	}
	now := time.Now()
	for key, pending := range s.oidcFlows {
		if now.After(pending.ExpiresAt) {
			delete(s.oidcFlows, key)
		}
	}
	s.oidcFlows[state] = flow
}

// takeOIDCFlow consumes a pending sign-in: single use, and never past its TTL.
func (s *Server) takeOIDCFlow(state string) (*oidcFlow, bool) {
	if state == "" {
		return nil, false
	}
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()

	flow, ok := s.oidcFlows[state]
	if !ok {
		return nil, false
	}
	delete(s.oidcFlows, state)
	if time.Now().After(flow.ExpiresAt) {
		return nil, false
	}
	return flow, true
}

func (s *Server) oidcGet(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.oidcClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", endpoint, resp.StatusCode)
	}
	return body, nil
}

func (s *Server) oidcClient() *http.Client {
	if s.OIDCHTTP != nil {
		return s.OIDCHTTP
	}
	return http.DefaultClient
}

func (s *Server) logOIDC(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf("oidc: "+format, args...)
	}
}

// pkceChallenge is S256(verifier), base64url without padding.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// audienceContains handles both the string and array forms of `aud`.
func audienceContains(raw json.RawMessage, clientID string) bool {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == clientID
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		for _, audience := range many {
			if audience == clientID {
				return true
			}
		}
	}
	return false
}

// oidcView is what the callback page renders.
type oidcView struct {
	Approved bool
	Email    string
	Error    string
}

func (s *Server) renderOIDC(w http.ResponseWriter, status int, view oidcView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = oidcTemplate.Execute(w, view)
}

var oidcTemplate = template.Must(template.New("oidc").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Provider sign-in — Neuron</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center;
         font: 16px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
         background: #f6f7f9; color: #16181d; }
  main { width: min(30rem, calc(100vw - 2rem)); background: #fff; border: 1px solid #e2e5ea;
         border-radius: 14px; padding: 2rem; box-shadow: 0 1px 2px rgba(16,24,40,.06); }
  h1 { font-size: 1.25rem; margin: 0 0 .5rem; letter-spacing: -.01em; }
  p { margin: 0; color: #47505f; }
  .error { background: #fdecec; border: 1px solid #f5c2c2; color: #8a1c1c;
           padding: .75rem .9rem; border-radius: 9px; margin: 0 0 1rem; }
  .ok { background: #eaf7ee; border: 1px solid #bfe5cb; color: #14532d;
        padding: .75rem .9rem; border-radius: 9px; margin: 0 0 1rem; }
  @media (prefers-color-scheme: dark) {
    body { background: #0f1115; color: #e8eaee; }
    main { background: #16181d; border-color: #262a32; }
    p { color: #9aa2b1; }
    .error { background: #2a1518; border-color: #5b2026; color: #ffb4b4; }
    .ok { background: #10241a; border-color: #1f4d34; color: #9ae6b4; }
  }
</style>
</head>
<body>
<main>
{{if .Approved}}
  <h1>Device approved</h1>
  <div class="ok">Signed in{{if .Email}} as <strong>{{.Email}}</strong>{{end}}.</div>
  <p>You can close this tab — the terminal finishes signing in on its own.</p>
{{else}}
  <h1>Sign-in failed</h1>
  <div class="error">{{.Error}}</div>
  <p>Run <code>neuron login</code> again to get a fresh code.</p>
{{end}}
</main>
</body>
</html>
`))
