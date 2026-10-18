package cloud

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// signInWithEmail creates an account with an email (Paystack requires one) and
// returns a session token for it.
func signInWithEmail(t *testing.T, ts *httptest.Server, email string) (token, accountID string) {
	t.Helper()

	salt := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/accounts", "",
		map[string]string{"salt": salt, "email": email})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create account: %d %v", resp.StatusCode, body)
	}
	accountID, _ = body["account_id"].(string)

	resp, body = doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: %d", resp.StatusCode)
	}
	deviceCode, _ := body["device_code"].(string)
	userCode, _ := body["user_code"].(string)

	if resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/device/approve", "",
		map[string]string{"user_code": userCode, "account_id": accountID}); resp.StatusCode != http.StatusOK {
		t.Fatalf("approve: %d", resp.StatusCode)
	}

	resp, body = doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "", map[string]string{"device_code": deviceCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token: %d %v", resp.StatusCode, body)
	}
	token, _ = body["access_token"].(string)
	return token, accountID
}

// fakePaystack records the initialize request and replies like Paystack does.
type fakePaystack struct {
	ts       *httptest.Server
	calls    int
	auth     string
	payload  map[string]any
	status   int
	response string
}

func newFakePaystack(t *testing.T) *fakePaystack {
	t.Helper()

	f := &fakePaystack{
		status:   http.StatusOK,
		response: `{"status":true,"data":{"authorization_url":"https://checkout.paystack.test/pay/abc","access_code":"abc","reference":"ref_1"}}`,
	}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transaction/initialize" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &f.payload)

		f.calls++
		f.auth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.response)
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func configurePaystack(server *Server, f *fakePaystack) {
	server.BillingProviderName = "paystack"
	server.PaystackSecretKey = "sk_test_paystack"
	server.PaystackPlanCode = "PLN_pro_monthly"
	server.PaystackCurrency = "GHS"
	server.PaystackAmount = 2000
	server.PaystackAPIBase = f.ts.URL
	server.PublicBaseURL = "https://app.neuron.test"
}

// paystackSignature builds the x-paystack-signature header for a body.
func paystackSignature(secret string, body []byte) string {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func postPaystackWebhook(t *testing.T, ts *httptest.Server, secret string, body []byte) (*http.Response, map[string]any) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/billing/webhook", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Paystack-Signature", paystackSignature(secret, body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &decoded)
	return resp, decoded
}

func TestPaystackCheckoutInitialisesASubscription(t *testing.T) {
	server, ts := newTestServer(t)
	paystack := newFakePaystack(t)
	configurePaystack(server, paystack)

	token, accountID := signInWithEmail(t, ts, "dev@example.com")

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout: %d %v", resp.StatusCode, body)
	}
	if body["url"] != "https://checkout.paystack.test/pay/abc" {
		t.Errorf("checkout url = %v", body["url"])
	}
	if body["provider"] != "paystack" {
		t.Errorf("provider = %v", body["provider"])
	}

	if paystack.calls != 1 {
		t.Fatalf("expected one initialize call, got %d", paystack.calls)
	}
	if paystack.auth != "Bearer sk_test_paystack" {
		t.Errorf("authorization = %q", paystack.auth)
	}
	if paystack.payload["email"] != "dev@example.com" {
		t.Errorf("email = %v", paystack.payload["email"])
	}
	if paystack.payload["plan"] != "PLN_pro_monthly" {
		t.Errorf("plan = %v", paystack.payload["plan"])
	}
	if paystack.payload["currency"] != "GHS" || paystack.payload["amount"] != float64(2000) {
		t.Errorf("amount/currency = %v / %v", paystack.payload["amount"], paystack.payload["currency"])
	}
	if paystack.payload["callback_url"] != "https://app.neuron.test/billing/success" {
		t.Errorf("callback_url = %v", paystack.payload["callback_url"])
	}
	metadata, _ := paystack.payload["metadata"].(map[string]any)
	if metadata["account_id"] != accountID {
		t.Errorf("metadata account_id = %v, want %v", metadata["account_id"], accountID)
	}
}

// Paystack identifies the customer by email, so an account without one cannot
// subscribe — and must not reach the API.
func TestPaystackCheckoutNeedsAnEmail(t *testing.T) {
	server, ts := newTestServer(t)
	paystack := newFakePaystack(t)
	configurePaystack(server, paystack)

	token, _ := signIn(t, ts) // created without an email

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d %v", resp.StatusCode, body)
	}
	if paystack.calls != 0 {
		t.Errorf("no API call should be made without an email, got %d", paystack.calls)
	}
}

func TestPaystackWebhookActivatesAndIsIdempotent(t *testing.T) {
	server, ts := newTestServer(t)
	paystack := newFakePaystack(t)
	configurePaystack(server, paystack)

	_, accountID := signInWithEmail(t, ts, "dev@example.com")

	body := []byte(`{"event":"charge.success","data":{` +
		`"id":4242,"status":"success","reference":"ref_1",` +
		`"plan":{"plan_code":"PLN_pro_monthly"},` +
		`"customer":{"email":"dev@example.com","customer_code":"CUS_abc"},` +
		`"metadata":{"account_id":"` + accountID + `"}}}`)

	resp, decoded := postPaystackWebhook(t, ts, server.PaystackSecretKey, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: %d %v", resp.StatusCode, decoded)
	}
	account, err := server.Store.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != PlanPro {
		t.Errorf("plan = %q, want pro", account.Plan)
	}
	if account.BillingCustomerID != "CUS_abc" {
		t.Errorf("billing customer = %q", account.BillingCustomerID)
	}

	// The same event delivered again is a no-op.
	resp, decoded = postPaystackWebhook(t, ts, server.PaystackSecretKey, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: %d %v", resp.StatusCode, decoded)
	}
	if decoded["status"] != "already processed" {
		t.Errorf("replay response = %v", decoded)
	}
}

func TestPaystackWebhookRejectsABadSignature(t *testing.T) {
	server, ts := newTestServer(t)
	paystack := newFakePaystack(t)
	configurePaystack(server, paystack)

	body := []byte(`{"event":"subscription.create","data":{"id":1}}`)
	resp, _ := postPaystackWebhook(t, ts, "wrong-secret", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad signature: got %d, want 400", resp.StatusCode)
	}
}

func TestPaystackWebhookDowngradesOnDisable(t *testing.T) {
	server, ts := newTestServer(t)
	paystack := newFakePaystack(t)
	configurePaystack(server, paystack)

	_, accountID := signInWithEmail(t, ts, "dev@example.com")
	if err := server.Store.SetPlan(accountID, PlanPro); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"event":"subscription.disable","data":{` +
		`"id":5252,"subscription_code":"SUB_abc","status":"cancelled",` +
		`"customer":{"email":"dev@example.com","customer_code":"CUS_abc"},` +
		`"metadata":{"account_id":"` + accountID + `"}}}`)

	if resp, decoded := postPaystackWebhook(t, ts, server.PaystackSecretKey, body); resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: %d %v", resp.StatusCode, decoded)
	}

	account, err := server.Store.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != PlanFree {
		t.Errorf("plan = %q, want free", account.Plan)
	}
}

// A charge for a different plan must not grant the paid plan.
func TestPaystackIgnoresOtherPlans(t *testing.T) {
	server, ts := newTestServer(t)
	paystack := newFakePaystack(t)
	configurePaystack(server, paystack)

	_, accountID := signInWithEmail(t, ts, "dev@example.com")

	body := []byte(`{"event":"charge.success","data":{` +
		`"id":999,"status":"success","plan":{"plan_code":"PLN_something_else"},` +
		`"customer":{"email":"dev@example.com"},` +
		`"metadata":{"account_id":"` + accountID + `"}}}`)

	if resp, decoded := postPaystackWebhook(t, ts, server.PaystackSecretKey, body); resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: %d %v", resp.StatusCode, decoded)
	}
	account, err := server.Store.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != PlanFree {
		t.Errorf("a different plan must not upgrade the account, got %q", account.Plan)
	}
}

func TestBillingProviderSelection(t *testing.T) {
	cases := []struct {
		name      string
		configure func(*Server)
		want      string
	}{
		{
			name: "explicit paystack",
			configure: func(s *Server) {
				s.BillingProviderName = "paystack"
				s.PaystackSecretKey = "k"
				s.PaystackPlanCode = "p"
			},
			want: "paystack",
		},
		{
			name:      "explicit stripe",
			configure: func(s *Server) { s.BillingProviderName = "stripe"; s.StripeSecretKey = "k"; s.StripePriceID = "p" },
			want:      "stripe",
		},
		{
			name: "auto prefers stripe when both are configured",
			configure: func(s *Server) {
				s.StripeSecretKey, s.StripePriceID = "k", "p"
				s.PaystackSecretKey, s.PaystackPlanCode = "k", "p"
			},
			want: "stripe",
		},
		{
			name:      "auto falls back to paystack",
			configure: func(s *Server) { s.PaystackSecretKey, s.PaystackPlanCode = "k", "p" },
			want:      "paystack",
		},
		{
			name:      "nothing configured",
			configure: func(s *Server) {},
			want:      "",
		},
		{
			name:      "unknown name does not fall back",
			configure: func(s *Server) { s.BillingProviderName = "paypal"; s.StripeSecretKey, s.StripePriceID = "k", "p" },
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{}
			tc.configure(server)
			provider := server.billingProvider()
			if tc.want == "" {
				if provider != nil {
					t.Fatalf("expected no provider, got %q", provider.Name())
				}
				return
			}
			if provider == nil || provider.Name() != tc.want {
				t.Fatalf("got %v, want %q", provider, tc.want)
			}
		})
	}
}

// Webhooks only need signing material, not a price.
func TestWebhookProviderWorksWithoutCheckoutConfigured(t *testing.T) {
	stripeOnly := &Server{WebhookSecret: "whsec_x"}
	if provider := stripeOnly.webhookProvider(); provider == nil || provider.Name() != "stripe" {
		t.Errorf("a Stripe webhook secret should be enough to verify Stripe events")
	}

	paystackOnly := &Server{PaystackSecretKey: "sk_x"}
	if provider := paystackOnly.webhookProvider(); provider == nil || provider.Name() != "paystack" {
		t.Errorf("a Paystack secret key should be enough to verify Paystack events")
	}
}
