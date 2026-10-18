package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// stripeProvider keeps the original Stripe integration behind BillingProvider.
//
// Kept for self-hosters in countries Stripe serves; the hosted service uses
// Paystack because Stripe cannot onboard a Ghana-registered business.

const (
	// defaultStripeAPIBase is Stripe's live API. Tests point StripeAPIBase at a
	// fake so nothing here needs the network.
	defaultStripeAPIBase = "https://api.stripe.com"
	// maxBillingResponse bounds what we read back from a provider.
	maxBillingResponse = 1 << 20
)

// stripeSession is the part of a Checkout Session the CLI needs.
type stripeSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// stripeAPIError is Stripe's error envelope.
type stripeAPIError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// stripeEvent is the part of a Stripe webhook this service reads.
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

type stripeProvider struct{ s *Server }

func (p *stripeProvider) Name() string { return "stripe" }

func (p *stripeProvider) Configured() bool {
	return p.s.StripeSecretKey != "" && p.s.StripePriceID != ""
}

// Checkout creates a subscription Checkout Session. No Stripe SDK: the API is one
// form-encoded POST, and the service stays dependency-free.
func (p *stripeProvider) Checkout(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error) {
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("line_items[0][price]", p.s.StripePriceID)
	form.Set("line_items[0][quantity]", "1")
	// Both identifiers are set: the webhook prefers metadata and falls back to
	// client_reference_id, so either one is enough to find the account.
	form.Set("client_reference_id", req.AccountID)
	form.Set("metadata[account_id]", req.AccountID)
	form.Set("success_url", req.SuccessURL)
	form.Set("cancel_url", req.CancelURL)
	if req.CustomerID != "" {
		// Reuse the existing customer so a re-subscribe does not create a second
		// one (and keeps past invoices attached).
		form.Set("customer", req.CustomerID)
	} else if req.Email != "" {
		form.Set("customer_email", req.Email)
	}

	endpoint := p.s.stripeBase() + "/v1/checkout/sessions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build stripe request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.s.StripeSecretKey)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.s.billingHTTPClient(p.s.stripeHTTP).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call stripe: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBillingResponse))
	if err != nil {
		return nil, fmt.Errorf("read stripe response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr stripeAPIError
		_ = json.Unmarshal(body, &apiErr)
		message := apiErr.Error.Message
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("stripe rejected the session (status %d): %s", resp.StatusCode, message)
	}

	var session stripeSession
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("parse stripe response: %w", err)
	}
	if session.URL == "" {
		return nil, errors.New("stripe returned no checkout url")
	}
	return &CheckoutSession{ID: session.ID, URL: session.URL}, nil
}

func (p *stripeProvider) VerifyWebhook(body []byte, header http.Header) error {
	return VerifyStripeSignature(body, header.Get("Stripe-Signature"),
		p.s.WebhookSecret, DefaultSignatureTolerance, p.s.now())
}

func (p *stripeProvider) ParseEvent(body []byte) (*BillingEvent, error) {
	var event stripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("parse stripe event: %w", err)
	}
	if event.ID == "" {
		return nil, errors.New("event id is required")
	}

	out := &BillingEvent{
		ID:             "stripe:" + event.ID,
		AccountID:      event.Data.Object.ClientReferenceID,
		CustomerID:     event.Data.Object.Customer,
		SubscriptionID: event.Data.Object.Subscription,
	}
	if id := event.Data.Object.Metadata["account_id"]; id != "" {
		out.AccountID = id
	}

	switch event.Type {
	case "checkout.session.completed", "customer.subscription.updated", "invoice.paid":
		out.Change = PlanActivate
	case "customer.subscription.deleted":
		out.Change = PlanDeactivate
	default:
		out.Change = PlanUnchanged
	}
	return out, nil
}

// stripeBase resolves the Stripe API endpoint, preferring a test override.
func (s *Server) stripeBase() string {
	base := s.StripeAPIBase
	if base == "" {
		base = defaultStripeAPIBase
	}
	return strings.TrimRight(base, "/")
}
