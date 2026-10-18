package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrAlreadySubscribed means the account already pays, so there is nothing to
// buy. The CLI turns it into guidance rather than an error message.
var ErrAlreadySubscribed = errors.New("this account is already on the pro plan")

// Checkout is a pending Stripe Checkout Session: the customer opens URL to pay.
type Checkout struct {
	URL       string `json:"url"`
	SessionID string `json:"session_id"`
}

// CreateCheckout asks the service for a Checkout URL that upgrades this account
// to the paid plan. Nothing is charged until the customer completes checkout on
// Stripe, and the plan only flips when Stripe calls the service's webhook — so
// a client that abandons the flow stays on the free plan.
func (c *Client) CreateCheckout(ctx context.Context) (*Checkout, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/billing/checkout", nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusConflict:
		return nil, ErrAlreadySubscribed
	case http.StatusServiceUnavailable:
		return nil, errors.New("this Neuron service does not have billing configured")
	default:
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Error != "" {
			return nil, fmt.Errorf("could not start checkout: %s", apiErr.Error)
		}
		return nil, fmt.Errorf("could not start checkout: the server returned %d", resp.StatusCode)
	}

	var checkout Checkout
	if err := json.Unmarshal(body, &checkout); err != nil {
		return nil, fmt.Errorf("parse checkout response: %w", err)
	}
	if checkout.URL == "" {
		return nil, errors.New("the service returned no checkout url")
	}
	return &checkout, nil
}
