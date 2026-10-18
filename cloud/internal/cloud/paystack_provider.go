package cloud

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// paystackProvider integrates Paystack, which can onboard businesses in Ghana
// (and Nigeria, South Africa, Kenya) where Stripe cannot.
//
// Checkout is a `transaction/initialize` call carrying a plan code: Paystack keeps
// the subscription, this service only reacts to its webhooks. Webhooks are signed
// with HMAC-SHA512 of the raw body keyed by the secret key.

const defaultPaystackAPIBase = "https://api.paystack.co"

type paystackProvider struct{ s *Server }

func (p *paystackProvider) Name() string { return "paystack" }

func (p *paystackProvider) Configured() bool {
	return p.s.PaystackSecretKey != "" && p.s.PaystackPlanCode != ""
}

// paystackInitializeResponse is the subset of the initialize response we need.
type paystackInitializeResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		AuthorizationURL string `json:"authorization_url"`
		AccessCode       string `json:"access_code"`
		Reference        string `json:"reference"`
	} `json:"data"`
}

// Checkout initialises a subscription transaction and returns its hosted URL.
func (p *paystackProvider) Checkout(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error) {
	if strings.TrimSpace(req.Email) == "" {
		// Paystack identifies the customer by email; there is no way around it.
		return nil, errors.New("paystack needs the account to have an email address")
	}

	payload := map[string]any{
		"email":        req.Email,
		"plan":         p.s.PaystackPlanCode,
		"callback_url": req.SuccessURL,
		"metadata":     map[string]string{"account_id": req.AccountID},
	}
	if p.s.PaystackCurrency != "" {
		payload["currency"] = p.s.PaystackCurrency
	}
	if p.s.PaystackAmount > 0 {
		// Minor units (pesewas/cents). Optional: the plan carries its own price.
		payload["amount"] = p.s.PaystackAmount
	}
	if req.CancelURL != "" {
		// Paystack has no cancel URL field; the callback is where the browser
		// returns either way, so the cancel URL is recorded in metadata instead.
		payload["metadata"] = map[string]string{
			"account_id": req.AccountID,
			"cancel_url": req.CancelURL,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("build paystack request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.s.paystackBase()+"/transaction/initialize", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build paystack request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.s.PaystackSecretKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.s.billingHTTPClient(p.s.paystackHTTP).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call paystack: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBillingResponse))
	if err != nil {
		return nil, fmt.Errorf("read paystack response: %w", err)
	}

	var out paystackInitializeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse paystack response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || !out.Status {
		message := out.Message
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("paystack rejected the transaction (status %d): %s", resp.StatusCode, message)
	}
	if out.Data.AuthorizationURL == "" {
		return nil, errors.New("paystack returned no authorization url")
	}

	return &CheckoutSession{ID: out.Data.Reference, URL: out.Data.AuthorizationURL}, nil
}

// VerifyWebhook checks x-paystack-signature: hex HMAC-SHA512 of the raw body,
// keyed by the secret key.
func (p *paystackProvider) VerifyWebhook(body []byte, header http.Header) error {
	signature := strings.ToLower(strings.TrimSpace(header.Get("X-Paystack-Signature")))
	if signature == "" {
		return errors.New("missing paystack signature")
	}

	mac := hmac.New(sha512.New, []byte(p.s.PaystackSecretKey))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return errors.New("paystack signature mismatch")
	}
	return nil
}

// paystackEvent is the envelope of every Paystack webhook.
type paystackEvent struct {
	Event string `json:"event"`
	Data  struct {
		ID        json.Number     `json:"id"`
		Status    string          `json:"status"`
		Reference string          `json:"reference"`
		Plan      json.RawMessage `json:"plan"`
		PlanCode  string          `json:"plan_code"`
		// subscription.create and subscription.disable carry the code here.
		SubscriptionCode string `json:"subscription_code"`
		Customer         struct {
			Email        string `json:"email"`
			CustomerCode string `json:"customer_code"`
		} `json:"customer"`
		Metadata json.RawMessage `json:"metadata"`
	} `json:"data"`
}

// ParseEvent normalises a Paystack webhook.
func (p *paystackProvider) ParseEvent(body []byte) (*BillingEvent, error) {
	var event paystackEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("parse paystack event: %w", err)
	}

	planCode := event.Data.PlanCode
	if planCode == "" {
		planCode = paystackPlanCode(event.Data.Plan)
	}

	out := &BillingEvent{
		ID:             paystackEventID(event.Data.ID.String(), event.Data.Reference, body),
		AccountID:      paystackMetadataAccount(event.Data.Metadata),
		CustomerID:     event.Data.Customer.CustomerCode,
		SubscriptionID: event.Data.SubscriptionCode,
		Email:          event.Data.Customer.Email,
	}

	// A different plan code means this event is for something else we sell (or a
	// plan the operator changed): never touch the plan on a mismatch.
	if p.s.PaystackPlanCode != "" && planCode != "" && planCode != p.s.PaystackPlanCode {
		out.Change = PlanUnchanged
		return out, nil
	}

	switch event.Event {
	case "subscription.create":
		out.Change = PlanActivate
	case "charge.success":
		// The first charge of a subscription arrives as charge.success with the
		// plan attached; a one-off charge has no plan and is ignored.
		if planCode != "" && event.Data.Status == "success" {
			out.Change = PlanActivate
		} else {
			out.Change = PlanUnchanged
		}
	case "subscription.disable", "subscription.not_renew":
		out.Change = PlanDeactivate
	default:
		out.Change = PlanUnchanged
	}
	return out, nil
}

// paystackBase resolves the API endpoint, preferring a test override.
func (s *Server) paystackBase() string {
	base := s.PaystackAPIBase
	if base == "" {
		base = defaultPaystackAPIBase
	}
	return strings.TrimRight(base, "/")
}

// paystackPlanCode accepts both shapes Paystack uses for `plan`: a bare code and
// an object with plan_code.
func paystackPlanCode(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var code string
	if err := json.Unmarshal(raw, &code); err == nil {
		return code
	}
	var obj struct {
		PlanCode string `json:"plan_code"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.PlanCode
	}
	return ""
}

// paystackMetadataAccount reads our account id out of `metadata`, which may be an
// object or a JSON string depending on how it was sent.
func paystackMetadataAccount(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if id, ok := obj["account_id"].(string); ok {
			return id
		}
	}

	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		var inner map[string]any
		if json.Unmarshal([]byte(encoded), &inner) == nil {
			if id, ok := inner["account_id"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// paystackEventID builds a stable id for retry de-duplication. Paystack does not
// send an event id, so the transaction id (or the reference, or a digest of the
// body) stands in.
func paystackEventID(transactionID, reference string, body []byte) string {
	if transactionID != "" && transactionID != "0" {
		return "paystack:" + transactionID
	}
	if reference != "" {
		return "paystack:ref:" + reference
	}
	sum := sha256.Sum256(body)
	return "paystack:sha:" + hex.EncodeToString(sum[:8])
}
