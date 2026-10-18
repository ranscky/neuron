package cloud

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeStripe stands in for the Stripe API so checkout can be tested without a
// key or a network call.
type fakeStripe struct {
	ts *httptest.Server

	mu       sync.Mutex
	calls    int
	form     url.Values
	authHead string
	path     string

	status   int
	response string
}

func newFakeStripe(t *testing.T, status int, response string) *fakeStripe {
	t.Helper()
	f := &fakeStripe{status: status, response: response}
	f.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		f.mu.Lock()
		f.calls++
		f.form = r.PostForm
		f.authHead = r.Header.Get("Authorization")
		f.path = r.URL.Path
		status, response := f.status, f.response
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeStripe) snapshot() (calls int, form url.Values, auth, path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.form, f.authHead, f.path
}

const stripeSessionBody = `{"id":"cs_test_123","url":"https://checkout.stripe.test/pay/cs_test_123"}`

func configureCheckout(server *Server, f *fakeStripe) {
	server.StripeSecretKey = "sk_test_supersecret"
	server.StripePriceID = "price_pro_monthly"
	server.StripeAPIBase = f.ts.URL
	server.PublicBaseURL = "https://app.neuron.test"
}

func TestCheckoutCreatesASubscriptionSession(t *testing.T) {
	server, ts := newTestServer(t)
	stripe := newFakeStripe(t, http.StatusOK, stripeSessionBody)
	configureCheckout(server, stripe)

	token, accountID := signIn(t, ts)

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout: status %d body %v", resp.StatusCode, body)
	}
	if body["url"] != "https://checkout.stripe.test/pay/cs_test_123" {
		t.Errorf("checkout url not returned to the client: %v", body)
	}

	calls, form, auth, path := stripe.snapshot()
	if calls != 1 {
		t.Fatalf("expected 1 Stripe call, got %d", calls)
	}
	if path != "/v1/checkout/sessions" {
		t.Errorf("called %q", path)
	}
	if auth != "Bearer sk_test_supersecret" {
		t.Errorf("Stripe auth header = %q", auth)
	}
	if form.Get("mode") != "subscription" {
		t.Errorf("mode = %q", form.Get("mode"))
	}
	if form.Get("line_items[0][price]") != "price_pro_monthly" || form.Get("line_items[0][quantity]") != "1" {
		t.Errorf("line items = %v", form)
	}
	// The webhook resolves the account from either field, so both must be set.
	if form.Get("client_reference_id") != accountID {
		t.Errorf("client_reference_id = %q, want %q", form.Get("client_reference_id"), accountID)
	}
	if form.Get("metadata[account_id]") != accountID {
		t.Errorf("metadata[account_id] = %q, want %q", form.Get("metadata[account_id]"), accountID)
	}
	if !strings.HasPrefix(form.Get("success_url"), "https://app.neuron.test/billing/success") {
		t.Errorf("success_url = %q", form.Get("success_url"))
	}
	if !strings.HasPrefix(form.Get("cancel_url"), "https://app.neuron.test/billing/cancel") {
		t.Errorf("cancel_url = %q", form.Get("cancel_url"))
	}
}

// Buying a plan must not be reachable without a session.
func TestCheckoutRequiresASession(t *testing.T) {
	server, ts := newTestServer(t)
	stripe := newFakeStripe(t, http.StatusOK, stripeSessionBody)
	configureCheckout(server, stripe)

	resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if calls, _, _, _ := stripe.snapshot(); calls != 0 {
		t.Errorf("Stripe must not be called without a session, got %d calls", calls)
	}
}

// An unconfigured self-hosted service must say so rather than 500.
func TestCheckoutIsUnavailableWithoutStripeCredentials(t *testing.T) {
	server, ts := newTestServer(t)
	server.StripePriceID = "price_pro_monthly" // secret key deliberately unset

	token, _ := signIn(t, ts)
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body %v", resp.StatusCode, body)
	}
}

func TestCheckoutRejectsAnExistingSubscriber(t *testing.T) {
	server, ts := newTestServer(t)
	stripe := newFakeStripe(t, http.StatusOK, stripeSessionBody)
	configureCheckout(server, stripe)

	token, accountID := signIn(t, ts)
	if err := server.Store.SetPlan(accountID, PlanPro); err != nil {
		t.Fatal(err)
	}

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d body %v", resp.StatusCode, body)
	}
	if calls, _, _, _ := stripe.snapshot(); calls != 0 {
		t.Errorf("a paying account must not open another session, got %d calls", calls)
	}
}

// A Stripe failure is a bad gateway for the client, must not leak the API key,
// and must not upgrade the plan on its own.
func TestCheckoutReportsStripeFailureWithoutLeakingTheKey(t *testing.T) {
	server, ts := newTestServer(t)
	stripe := newFakeStripe(t, http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"No such price: price_pro_monthly"}}`)
	configureCheckout(server, stripe)

	token, accountID := signIn(t, ts)

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body %v", resp.StatusCode, body)
	}
	raw := strings.TrimSpace(strings.Join([]string{asString(body["error"])}, " "))
	if strings.Contains(raw, "sk_test_supersecret") {
		t.Errorf("response leaked the Stripe secret key: %v", body)
	}
	if strings.Contains(raw, "price_pro_monthly") {
		t.Errorf("response leaked internal price ids: %v", body)
	}

	account, err := server.Store.GetAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Plan != PlanFree {
		t.Errorf("a failed checkout must not change the plan, got %q", account.Plan)
	}
}

func TestCheckoutFallsBackToTheRequestOrigin(t *testing.T) {
	server, ts := newTestServer(t)
	stripe := newFakeStripe(t, http.StatusOK, stripeSessionBody)
	configureCheckout(server, stripe)
	server.PublicBaseURL = "" // nothing configured: derive from the request

	token, _ := signIn(t, ts)
	if resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout: status %d body %v", resp.StatusCode, body)
	}

	_, form, _, _ := stripe.snapshot()
	if !strings.HasPrefix(form.Get("success_url"), ts.URL+"/billing/success") {
		t.Errorf("success_url should use the request origin, got %q", form.Get("success_url"))
	}
}

// An existing Stripe customer is reused, so re-subscribing does not create a
// second customer and detach past invoices.
func TestCheckoutReusesAnExistingStripeCustomer(t *testing.T) {
	server, ts := newTestServer(t)
	stripe := newFakeStripe(t, http.StatusOK, stripeSessionBody)
	configureCheckout(server, stripe)

	token, accountID := signIn(t, ts)
	if err := server.Store.SetStripe(accountID, "cus_existing", "sub_old"); err != nil {
		t.Fatal(err)
	}

	if resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/billing/checkout", token, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("checkout: status %d body %v", resp.StatusCode, body)
	}

	_, form, _, _ := stripe.snapshot()
	if form.Get("customer") != "cus_existing" {
		t.Errorf("customer = %q, want cus_existing", form.Get("customer"))
	}
	if form.Get("customer_email") != "" {
		t.Errorf("customer and customer_email are mutually exclusive, got %q", form.Get("customer_email"))
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
