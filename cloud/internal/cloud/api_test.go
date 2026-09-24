package cloud

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "cloud.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(store, testWebhookSecret, log.New(io.Discard, "", 0))
	// Roomy by default so only the rate-limit test trips the limiter.
	server.limiter = NewRateLimiter(1000, 1000)

	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	return server, ts
}

// doJSON performs a request and decodes a JSON object response.
func doJSON(t *testing.T, method, url, token string, body interface{}) (*http.Response, map[string]any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if raw, _ := io.ReadAll(resp.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return resp, decoded
}

// signIn exercises the real device flow and returns a usable session.
func signIn(t *testing.T, ts *httptest.Server) (token, accountID string) {
	t.Helper()

	salt := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/accounts", "", map[string]string{"salt": salt})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create account: status %d body %v", resp.StatusCode, body)
	}
	accountID, _ = body["account_id"].(string)
	if accountID == "" {
		t.Fatal("account creation returned no id")
	}

	resp, body = doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: status %d", resp.StatusCode)
	}
	deviceCode, _ := body["device_code"].(string)
	userCode, _ := body["user_code"].(string)

	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/device/approve", "",
		map[string]string{"user_code": userCode, "account_id": accountID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve: status %d", resp.StatusCode)
	}

	resp, body = doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "",
		map[string]string{"device_code": deviceCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token: status %d", resp.StatusCode)
	}
	token, _ = body["access_token"].(string)
	if token == "" {
		t.Fatal("device flow returned no access token")
	}
	return token, accountID
}

func TestHealth(t *testing.T) {
	_, ts := newTestServer(t)
	resp, body := doJSON(t, http.MethodGet, ts.URL+"/healthz", "", nil)
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("health: %d %v", resp.StatusCode, body)
	}
}

func TestDeviceFlowOverHTTP(t *testing.T) {
	_, ts := newTestServer(t)

	salt := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/accounts", "",
		map[string]string{"email": "dev@example.com", "salt": salt})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create account: %d %v", resp.StatusCode, body)
	}
	accountID, _ := body["account_id"].(string)

	resp, body = doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: %d", resp.StatusCode)
	}
	deviceCode, _ := body["device_code"].(string)
	userCode, _ := body["user_code"].(string)
	if deviceCode == "" || userCode == "" {
		t.Fatalf("missing device code fields: %v", body)
	}

	// Before approval the client must be told to wait, not given a token.
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "",
		map[string]string{"device_code": deviceCode})
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("expected 428 before approval, got %d", resp.StatusCode)
	}

	// Approval accepts the code however the human typed it.
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/device/approve", "",
		map[string]string{"user_code": strings.ToLower(userCode), "account_id": accountID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve with lower-case code: %d", resp.StatusCode)
	}

	resp, body = doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "",
		map[string]string{"device_code": deviceCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token: %d", resp.StatusCode)
	}
	token, _ := body["access_token"].(string)

	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/account", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("account: %d", resp.StatusCode)
	}
	if body["plan"] != PlanFree {
		t.Errorf("expected a free plan on signup, got %v", body["plan"])
	}
	// The service must never hand Stripe identifiers back to a client.
	for _, leaked := range []string{"stripe_customer_id", "stripe_subscription_id"} {
		if _, present := body[leaked]; present {
			t.Errorf("account response leaked %s", leaked)
		}
	}
}

func TestAccountCreationRejectsABadSalt(t *testing.T) {
	_, ts := newTestServer(t)

	resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/accounts", "", map[string]string{"salt": "not base64!!"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed salt, got %d", resp.StatusCode)
	}

	short := base64.StdEncoding.EncodeToString([]byte("abc"))
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/accounts", "", map[string]string{"salt": short})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for a short salt, got %d", resp.StatusCode)
	}
}

func TestSyncEndpointsRequireAuth(t *testing.T) {
	_, ts := newTestServer(t)

	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d", resp.StatusCode)
	}

	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs", "not-a-real-token", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for a bogus token, got %d", resp.StatusCode)
	}
}

func TestSyncIsGatedOnThePaidPlan(t *testing.T) {
	server, ts := newTestServer(t)
	token, accountID := signIn(t, ts)

	resp, body := doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs", token, nil)
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("expected 402 on the free plan, got %d %v", resp.StatusCode, body)
	}

	if err := server.Store.SetPlan(accountID, PlanPro); err != nil {
		t.Fatal(err)
	}

	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on the paid plan, got %d", resp.StatusCode)
	}
}

func TestBlobRoundTripAndStaleRejection(t *testing.T) {
	server, ts := newTestServer(t)
	token, accountID := signIn(t, ts)
	if err := server.Store.SetPlan(accountID, PlanPro); err != nil {
		t.Fatal(err)
	}

	put := map[string]any{
		"version":  1,
		"envelope": map[string]any{"v": 1, "salt": "c2FsdA==", "iter": 1000, "nonce": "bm9uY2U=", "data": "ZGF0YQ=="},
	}

	resp, _ := doJSON(t, http.MethodPut, ts.URL+"/v1/sync/blobs/github", token, put)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	// A replayed push must not roll the blob backwards.
	resp, _ = doJSON(t, http.MethodPut, ts.URL+"/v1/sync/blobs/github", token, put)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for a stale version, got %d", resp.StatusCode)
	}

	resp, body := doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs/github", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	if body["name"] != "github" {
		t.Errorf("unexpected blob payload: %v", body)
	}

	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	names, _ := body["names"].([]any)
	if len(names) != 1 || names[0] != "github" {
		t.Errorf("unexpected blob list: %v", names)
	}

	resp, _ = doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs/absent", token, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown blob, got %d", resp.StatusCode)
	}
}

func TestOneAccountCannotReadAnothersBlobs(t *testing.T) {
	server, ts := newTestServer(t)

	alice, aliceID := signIn(t, ts)
	bob, bobID := signIn(t, ts)
	for _, id := range []string{aliceID, bobID} {
		if err := server.Store.SetPlan(id, PlanPro); err != nil {
			t.Fatal(err)
		}
	}

	put := map[string]any{"version": 1, "envelope": map[string]any{"v": 1}}
	if resp, _ := doJSON(t, http.MethodPut, ts.URL+"/v1/sync/blobs/github", alice, put); resp.StatusCode != http.StatusOK {
		t.Fatalf("alice put: %d", resp.StatusCode)
	}

	if resp, _ := doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs/github", alice, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("alice should read her own blob, got %d", resp.StatusCode)
	}
	if resp, _ := doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs/github", bob, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("bob must not read alice's blob, got %d", resp.StatusCode)
	}

	resp, body := doJSON(t, http.MethodGet, ts.URL+"/v1/sync/blobs", bob, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob list: %d", resp.StatusCode)
	}
	if names, _ := body["names"].([]any); len(names) != 0 {
		t.Errorf("bob's blob list should be empty, got %v", names)
	}
}

func TestLogoutRevokesTheToken(t *testing.T) {
	_, ts := newTestServer(t)
	token, _ := signIn(t, ts)

	if resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/logout", token, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ := doJSON(t, http.MethodGet, ts.URL+"/v1/account", token, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected the revoked token to be rejected, got %d", resp.StatusCode)
	}
}

func TestRateLimitingReturns429(t *testing.T) {
	server, ts := newTestServer(t)
	server.limiter = NewRateLimiter(0.001, 1)

	resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first request should pass, got %d", resp.StatusCode)
	}

	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 once the bucket is empty, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("a 429 must tell the caller when to retry")
	}
}

func postWebhook(t *testing.T, ts *httptest.Server, header string, body []byte) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/billing/webhook", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Stripe-Signature", header)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestWebhookUpgradesPlanAndIsIdempotent(t *testing.T) {
	server, ts := newTestServer(t)
	_, accountID := signIn(t, ts)

	event := map[string]any{
		"id":   "evt_checkout",
		"type": "checkout.session.completed",
		"data": map[string]any{"object": map[string]any{
			"client_reference_id": accountID,
			"customer":            "cus_123",
			"subscription":        "sub_456",
		}},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	header := SignStripePayload(raw, testWebhookSecret, time.Now())

	if code := postWebhook(t, ts, header, raw); code != http.StatusOK {
		t.Fatalf("webhook: %d", code)
	}

	account, err := server.Store.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != PlanPro {
		t.Errorf("expected the plan to upgrade to pro, got %s", account.Plan)
	}

	// Stripe retries; the same event must be a no-op, not a second upgrade.
	if code := postWebhook(t, ts, header, raw); code != http.StatusOK {
		t.Fatalf("replayed webhook: %d", code)
	}

	// An unsigned or badly signed webhook must never be trusted.
	if code := postWebhook(t, ts, "t=1,v1=00", raw); code != http.StatusBadRequest {
		t.Errorf("expected 400 for a bad signature, got %d", code)
	}
	if code := postWebhook(t, ts, "", raw); code != http.StatusBadRequest {
		t.Errorf("expected 400 for a missing signature, got %d", code)
	}
}

func TestWebhookDowngradesOnCancellation(t *testing.T) {
	server, ts := newTestServer(t)
	_, accountID := signIn(t, ts)
	if err := server.Store.SetPlan(accountID, PlanPro); err != nil {
		t.Fatal(err)
	}
	if err := server.Store.SetStripe(accountID, "cus_999", "sub_999"); err != nil {
		t.Fatal(err)
	}

	// No metadata on this event: resolution must fall back to the customer id.
	event := map[string]any{
		"id":   "evt_cancel",
		"type": "customer.subscription.deleted",
		"data": map[string]any{"object": map[string]any{"customer": "cus_999"}},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	header := SignStripePayload(raw, testWebhookSecret, time.Now())

	if code := postWebhook(t, ts, header, raw); code != http.StatusOK {
		t.Fatalf("webhook: %d", code)
	}

	account, err := server.Store.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != PlanFree {
		t.Errorf("expected the plan to fall back to free, got %s", account.Plan)
	}
}
