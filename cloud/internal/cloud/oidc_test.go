package cloud

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeIDP is a minimal OpenID provider: discovery, token exchange and JWKS.
type fakeIDP struct {
	ts        *httptest.Server
	key       *rsa.PrivateKey
	client    *http.Client
	nonce     string
	challenge string
	audience  string
	code      string
	expiry    time.Time
	email     string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	idp := &fakeIDP{
		key:      key,
		audience: "neuron-test",
		code:     "good-code",
		expiry:   time.Now().Add(5 * time.Minute),
		email:    "dev@example.com",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"issuer":                 idp.ts.URL,
			"authorization_endpoint": idp.ts.URL + "/authorize",
			"token_endpoint":         idp.ts.URL + "/token",
			"jwks_uri":               idp.ts.URL + "/jwks",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostFormValue("grant_type") != "authorization_code" || r.PostFormValue("code") != idp.code {
			writeError(w, http.StatusBadRequest, "bad grant")
			return
		}
		// PKCE: the verifier must match the challenge the browser was given.
		if idp.challenge != "" && pkceChallenge(r.PostFormValue("code_verifier")) != idp.challenge {
			writeError(w, http.StatusBadRequest, "pkce mismatch")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"id_token":   idp.sign(),
			"token_type": "Bearer",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": "k1",
				"n":   base64.RawURLEncoding.EncodeToString(idp.key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(idp.key.E)).Bytes()),
			}},
		})
	})

	idp.ts = httptest.NewServer(mux)
	t.Cleanup(idp.ts.Close)
	idp.client = idp.ts.Client()
	return idp
}

func (f *fakeIDP) sign() string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	claims, _ := json.Marshal(map[string]any{
		"iss":            f.ts.URL,
		"sub":            "user-1",
		"aud":            f.audience,
		"exp":            f.expiry.Unix(),
		"iat":            time.Now().Unix(),
		"nonce":          f.nonce,
		"email":          f.email,
		"email_verified": true,
	})
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// noRedirect keeps the test on the first response so it can read Location.
func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// startSignIn drives /v1/auth/oidc/start and returns the authorization query.
func startSignIn(t *testing.T, base, userCode string) url.Values {
	t.Helper()

	resp, err := noRedirect().Get(base + "/v1/auth/oidc/start?user_code=" + url.QueryEscape(userCode))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("start: status %d body %s", resp.StatusCode, body)
	}
	location := resp.Header.Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query()
}

func TestOIDCApprovesADeviceCode(t *testing.T) {
	server, ts := newTestServer(t)
	idp := newFakeIDP(t)
	server.OIDC = OIDCConfig{Issuer: idp.ts.URL, ClientID: idp.audience, ClientSecret: "s3cret"}
	server.OIDCHTTP = idp.client

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: %d %v", resp.StatusCode, body)
	}
	userCode, _ := body["user_code"].(string)
	deviceCode, _ := body["device_code"].(string)

	query := startSignIn(t, ts.URL, userCode)
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Errorf("PKCE challenge missing from the authorization request: %v", query)
	}
	if query.Get("scope") == "" || !strings.Contains(query.Get("scope"), "openid") {
		t.Errorf("scope should request openid: %v", query)
	}

	idp.nonce = query.Get("nonce")
	idp.challenge = query.Get("code_challenge")
	state := query.Get("state")

	cb, err := noRedirect().Get(ts.URL + "/v1/auth/oidc/callback?code=good-code&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(cb.Body)
	cb.Body.Close()
	if cb.StatusCode != http.StatusOK || !strings.Contains(string(page), "Device approved") {
		t.Fatalf("callback: status %d page %s", cb.StatusCode, page)
	}

	// The CLI keeps polling exactly as before.
	resp, tokenBody := doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "", map[string]string{"device_code": deviceCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token: %d %v", resp.StatusCode, tokenBody)
	}
	token, _ := tokenBody["access_token"].(string)

	// The identity's email is linked to an account.
	resp, account := doJSON(t, http.MethodGet, ts.URL+"/v1/account", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("account: %d %v", resp.StatusCode, account)
	}
	if account["email"] != idp.email {
		t.Errorf("account email = %v, want %v", account["email"], idp.email)
	}
}

// A second sign-in with the same verified email lands on the same account.
func TestOIDCLinksToAnExistingAccount(t *testing.T) {
	server, ts := newTestServer(t)
	idp := newFakeIDP(t)
	server.OIDC = OIDCConfig{Issuer: idp.ts.URL, ClientID: idp.audience, ClientSecret: "s3cret"}
	server.OIDCHTTP = idp.client

	approve := func() string {
		_, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
		userCode := body["user_code"].(string)
		deviceCode := body["device_code"].(string)
		query := startSignIn(t, ts.URL, userCode)
		idp.nonce = query.Get("nonce")
		idp.challenge = query.Get("code_challenge")
		if _, err := noRedirect().Get(ts.URL + "/v1/auth/oidc/callback?code=good-code&state=" + url.QueryEscape(query.Get("state"))); err != nil {
			t.Fatal(err)
		}
		_, tokenBody := doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "", map[string]string{"device_code": deviceCode})
		token, _ := tokenBody["access_token"].(string)
		return token
	}

	firstToken := approve()
	_, first := doJSON(t, http.MethodGet, ts.URL+"/v1/account", firstToken, nil)
	secondToken := approve()
	_, second := doJSON(t, http.MethodGet, ts.URL+"/v1/account", secondToken, nil)

	if first["account_id"] != second["account_id"] {
		t.Errorf("the same email should link to one account: %v vs %v", first["account_id"], second["account_id"])
	}
}

func TestOIDCRejectsBadFlows(t *testing.T) {
	server, ts := newTestServer(t)
	idp := newFakeIDP(t)
	server.OIDC = OIDCConfig{Issuer: idp.ts.URL, ClientID: idp.audience, ClientSecret: "s3cret"}
	server.OIDCHTTP = idp.client

	_, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	userCode := body["user_code"].(string)

	// An unknown state is refused without touching the provider.
	cb, err := noRedirect().Get(ts.URL + "/v1/auth/oidc/callback?code=good-code&state=nope")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(cb.Body)
	cb.Body.Close()
	if cb.StatusCode != http.StatusBadRequest || !strings.Contains(string(page), "unknown or has expired") {
		t.Errorf("unknown state: %d %s", cb.StatusCode, page)
	}

	// A token issued for another nonce is not trusted.
	query := startSignIn(t, ts.URL, userCode)
	idp.nonce = "not-the-nonce"
	idp.challenge = query.Get("code_challenge")
	cb, err = noRedirect().Get(ts.URL + "/v1/auth/oidc/callback?code=good-code&state=" + url.QueryEscape(query.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	cb.Body.Close()
	if cb.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad nonce: got %d, want 401", cb.StatusCode)
	}

	// An expired token is refused too.
	query = startSignIn(t, ts.URL, userCode)
	idp.nonce = query.Get("nonce")
	idp.challenge = query.Get("code_challenge")
	idp.expiry = time.Now().Add(-time.Minute)
	cb, err = noRedirect().Get(ts.URL + "/v1/auth/oidc/callback?code=good-code&state=" + url.QueryEscape(query.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	cb.Body.Close()
	if cb.StatusCode != http.StatusUnauthorized {
		t.Errorf("expired token: got %d, want 401", cb.StatusCode)
	}

	// A token for a different client is refused.
	idp.expiry = time.Now().Add(5 * time.Minute)
	idp.audience = "someone-else"
	query = startSignIn(t, ts.URL, userCode)
	idp.nonce = query.Get("nonce")
	idp.challenge = query.Get("code_challenge")
	cb, err = noRedirect().Get(ts.URL + "/v1/auth/oidc/callback?code=good-code&state=" + url.QueryEscape(query.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	cb.Body.Close()
	if cb.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong audience: got %d, want 401", cb.StatusCode)
	}
}

func TestOIDCIsUnavailableWhenNotConfigured(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/v1/auth/oidc/start?user_code=ABCD-EFGH")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("start: got %d, want 503", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/v1/auth/oidc/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("callback: got %d, want 503", resp.StatusCode)
	}
}

// The activation page offers provider sign-in only when it is configured.
func TestActivatePageOffersSSOWhenConfigured(t *testing.T) {
	server, ts := newTestServer(t)

	get := func() string {
		resp, err := http.Get(ts.URL + "/activate")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		page, _ := io.ReadAll(resp.Body)
		return string(page)
	}

	if strings.Contains(get(), "/v1/auth/oidc/start") {
		t.Error("the page must not offer SSO when it is not configured")
	}

	idp := newFakeIDP(t)
	server.OIDC = OIDCConfig{Issuer: idp.ts.URL, ClientID: idp.audience, ClientSecret: "s3cret"}
	if !strings.Contains(get(), "/v1/auth/oidc/start") {
		t.Error("the page should offer SSO when configured")
	}
}
