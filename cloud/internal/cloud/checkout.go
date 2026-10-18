package cloud

import (
	"net/http"
	"strings"
	"time"
)

// billingTimeout bounds one call to a payment processor, so a hung provider cannot
// pin an HTTP handler open.
const billingTimeout = 15 * time.Second

// billingHTTPClient returns the client for a provider. Tests inject one; otherwise
// this is a plain client with a timeout.
func (s *Server) billingHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: billingTimeout}
}

// handleCheckout starts a subscription for the signed-in account and returns the
// hosted checkout URL. The plan itself only changes when the provider calls the
// webhook, so a client that never pays stays on the free plan.
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request, accountID string) {
	provider := s.billingProvider()
	if provider == nil {
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
	session, err := provider.Checkout(r.Context(), CheckoutRequest{
		AccountID:  account.ID,
		Email:      account.Email,
		CustomerID: account.BillingCustomerID,
		SuccessURL: base + "/billing/success",
		CancelURL:  base + "/billing/cancel",
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("%s checkout for account %s: %v", provider.Name(), account.ID, err)
		}
		// Generic to the client: the detail can name internal ids.
		writeError(w, http.StatusBadGateway, "could not create a checkout session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"url":        session.URL,
		"session_id": session.ID,
		"provider":   provider.Name(),
	})
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
