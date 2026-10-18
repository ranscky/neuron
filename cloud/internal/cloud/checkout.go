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
	"time"
)

const (
	// defaultStripeAPIBase is Stripe's live API. Tests point StripeAPIBase at a
	// fake so nothing here needs the network.
	defaultStripeAPIBase = "https://api.stripe.com"
	// maxStripeResponse bounds what we read back from Stripe.
	maxStripeResponse = 1 << 20
	// stripeTimeout bounds a single Stripe API call, so a hung Stripe cannot
	// pin an HTTP handler open.
	stripeTimeout = 15 * time.Second
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

// createCheckoutSession asks Stripe for a subscription Checkout Session and
// returns it. No Stripe SDK: the API is one form-encoded POST, and the service
// stays dependency-free.
func (s *Server) createCheckoutSession(ctx context.Context, form url.Values) (*stripeSession, error) {
	endpoint := s.stripeBase() + "/v1/checkout/sessions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build stripe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.StripeSecretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := s.stripeHTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call stripe: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxStripeResponse))
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
	return &session, nil
}

// handleCheckout mints a Checkout Session for the signed-in account and returns
// its URL. The plan itself only changes when Stripe calls the webhook, so a
// client that never pays stays on the free plan.
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request, accountID string) {
	if s.StripeSecretKey == "" || s.StripePriceID == "" {
		// Same posture as an unset webhook secret: billing is off, not broken.
		writeError(w, http.StatusServiceUnavailable, "billing is not configured")
		return
	}

	account, err := s.Store.GetAccount(accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if account.Plan == PlanPro {
		writeError(w, http.StatusConflict, "this account is already on the pro plan")
		return
	}

	base := strings.TrimRight(s.publicBaseURL(r), "/")

	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("line_items[0][price]", s.StripePriceID)
	form.Set("line_items[0][quantity]", "1")
	// Both identifiers are set: the webhook prefers metadata and falls back to
	// client_reference_id, so either one is enough to find the account.
	form.Set("client_reference_id", account.ID)
	form.Set("metadata[account_id]", account.ID)
	form.Set("success_url", base+"/billing/success?session_id={CHECKOUT_SESSION_ID}")
	form.Set("cancel_url", base+"/billing/cancel")
	if account.StripeCustomerID != "" {
		// Reuse the existing customer so a re-subscribe does not create a
		// second one (and keeps past invoices attached).
		form.Set("customer", account.StripeCustomerID)
	} else if account.Email != "" {
		form.Set("customer_email", account.Email)
	}

	session, err := s.createCheckoutSession(r.Context(), form)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("stripe checkout for account %s: %v", account.ID, err)
		}
		// Generic to the client: the detail can name internal ids.
		writeError(w, http.StatusBadGateway, "could not create a checkout session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"url":        session.URL,
		"session_id": session.ID,
	})
}

// stripeBase resolves the Stripe API endpoint, preferring a test override.
func (s *Server) stripeBase() string {
	base := s.StripeAPIBase
	if base == "" {
		base = defaultStripeAPIBase
	}
	return strings.TrimRight(base, "/")
}

// publicBaseURL prefers the configured public origin, then reconstructs it from
// the request, honouring the header a reverse proxy sets.
func (s *Server) publicBaseURL(r *http.Request) string {
	if s.PublicBaseURL != "" {
		return s.PublicBaseURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" || proto == "http" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}
