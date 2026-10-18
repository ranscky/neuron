package cloud

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Billing is provider-independent.
//
// The service only needs three things from a payment processor: a hosted checkout
// URL, a way to authenticate its webhooks, and a normalised statement of what an
// event means for the plan. Everything provider-specific lives behind this
// interface, so the CLI and the rest of the service never learn which processor
// is in use.
//
// Stripe cannot onboard a business registered in every country (Ghana, for one),
// which is why a second implementation exists at all.

// PlanChange is what a billing event asks the service to do.
type PlanChange int

const (
	// PlanUnchanged is an event the service does not act on.
	PlanUnchanged PlanChange = iota
	// PlanActivate grants the paid plan.
	PlanActivate
	// PlanDeactivate returns the account to the free plan.
	PlanDeactivate
)

// CheckoutRequest is what a provider needs to start a subscription.
type CheckoutRequest struct {
	AccountID string
	Email     string
	// CustomerID is the provider-side customer, when this account already has one.
	CustomerID string
	SuccessURL string
	CancelURL  string
}

// CheckoutSession is a hosted checkout the customer is sent to.
type CheckoutSession struct {
	// ID is the provider's reference for the session.
	ID  string
	URL string
}

// BillingEvent is a provider-independent webhook event.
type BillingEvent struct {
	// ID makes delivery idempotent across retries.
	ID string
	// Change is what the event asks the service to do.
	Change PlanChange
	// AccountID is set when the provider carried our own identifier.
	AccountID string
	// CustomerID and SubscriptionID are the provider's identifiers.
	CustomerID     string
	SubscriptionID string
	// Email is used to link an account when no identifier came back.
	Email string
}

// BillingProvider is one payment processor.
type BillingProvider interface {
	// Name identifies the provider in API responses and logs.
	Name() string
	// Configured reports whether checkout can be offered at all.
	Configured() bool
	// Checkout creates a subscription checkout and returns its URL.
	Checkout(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error)
	// VerifyWebhook authenticates a webhook payload.
	VerifyWebhook(body []byte, header http.Header) error
	// ParseEvent normalises a webhook payload.
	ParseEvent(body []byte) (*BillingEvent, error)
}

// billingProvider resolves the configured processor.
//
// An explicit name always wins. With none set, Stripe is used when configured and
// Paystack otherwise, so a deployment that only has Paystack credentials just
// works.
func (s *Server) billingProvider() BillingProvider {
	switch strings.ToLower(strings.TrimSpace(s.BillingProviderName)) {
	case "stripe":
		return &stripeProvider{s: s}
	case "paystack":
		return &paystackProvider{s: s}
	case "":
		if provider := (&stripeProvider{s: s}); provider.Configured() {
			return provider
		}
		if provider := (&paystackProvider{s: s}); provider.Configured() {
			return provider
		}
		return nil
	default:
		// An unknown name is a configuration error, not a silent fallback.
		return nil
	}
}

// webhookProvider resolves the processor that signs incoming webhooks.
//
// It is deliberately looser than billingProvider: verifying a webhook needs only
// the signing material, so a service can act on subscription events even before
// checkout is fully configured (no price yet, for instance).
func (s *Server) webhookProvider() BillingProvider {
	switch strings.ToLower(strings.TrimSpace(s.BillingProviderName)) {
	case "stripe":
		return &stripeProvider{s: s}
	case "paystack":
		return &paystackProvider{s: s}
	case "":
		// Stripe signs with WebhookSecret, Paystack with its API secret key.
		if s.WebhookSecret != "" {
			return &stripeProvider{s: s}
		}
		if s.PaystackSecretKey != "" {
			return &paystackProvider{s: s}
		}
		return nil
	default:
		// An unknown name is a configuration error, not a silent fallback.
		return nil
	}
}

// errNoAccountForEvent means no account could be linked to a billing event.
var errNoAccountForEvent = errors.New("no account matches this billing event")

// resolveBillingAccount links an event to an account: our own identifier first,
// then the provider customer, then the email address.
func (s *Server) resolveBillingAccount(event *BillingEvent) string {
	if event.AccountID != "" {
		if _, err := s.Store.GetAccount(event.AccountID); err == nil {
			return event.AccountID
		}
	}
	if event.CustomerID != "" {
		if accountID, err := s.Store.FindAccountByBillingCustomer(event.CustomerID); err == nil {
			return accountID
		}
	}
	if event.Email != "" {
		if account, err := s.Store.FindAccountByEmail(event.Email); err == nil {
			return account.ID
		}
	}
	return ""
}
