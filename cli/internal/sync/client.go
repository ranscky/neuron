package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Errors the CLI maps to friendly guidance.
var (
	ErrPaymentRequired = errors.New("encrypted sync requires a paid plan")
	ErrUnauthorized    = errors.New("not signed in, or the session has expired")
	ErrPending         = errors.New("authorization pending")
	ErrStaleVersion    = errors.New("the remote already holds a newer version")
)

// Client talks to a Neuron cloud service. With a token set it also implements
// Remote, which is what the sync engine consumes.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient returns a client for a service base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// WithToken returns a copy of the client authenticated as the given session.
func (c *Client) WithToken(token string) *Client {
	clone := *c
	clone.Token = token
	return &clone
}

func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusPaymentRequired:
		return ErrPaymentRequired
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrStaleVersion
	default:
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// CreateAccount registers a new sync account and returns its id. The salt is
// key-derivation material and is not secret.
func (c *Client) CreateAccount(ctx context.Context, email string, salt []byte) (string, error) {
	var out struct {
		AccountID string `json:"account_id"`
	}
	payload := map[string]string{
		"email": email,
		"salt":  base64.StdEncoding.EncodeToString(salt),
	}
	if err := c.do(ctx, http.MethodPost, "/v1/accounts", payload, &out); err != nil {
		return "", err
	}
	if out.AccountID == "" {
		return "", errors.New("server did not return an account id")
	}
	return out.AccountID, nil
}

// DeviceCode is a pending device authorisation.
type DeviceCode struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	Interval   int    `json:"interval"`
	ExpiresIn  int    `json:"expires_in"`
}

// RequestDeviceCode starts the device authorisation flow.
func (c *Client) RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	var out DeviceCode
	if err := c.do(ctx, http.MethodPost, "/v1/device/code", nil, &out); err != nil {
		return nil, err
	}
	if out.Interval <= 0 {
		out.Interval = 2
	}
	return &out, nil
}

// ApproveDeviceCode approves a pending code for an account. In a hosted
// deployment this is done in the browser; the CLI needs it for self-hosting.
func (c *Client) ApproveDeviceCode(ctx context.Context, userCode, accountID string) error {
	payload := map[string]string{"user_code": userCode, "account_id": accountID}
	return c.do(ctx, http.MethodPost, "/v1/device/approve", payload, nil)
}

// PollDeviceToken exchanges an approved device code for an access token. It
// returns ErrPending while the user has not approved yet.
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/device/token",
		strings.NewReader(fmt.Sprintf(`{"device_code":%q}`, deviceCode)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", err
		}
		if out.AccessToken == "" {
			return "", errors.New("server returned an empty access token")
		}
		return out.AccessToken, nil
	case http.StatusPreconditionRequired:
		return "", ErrPending
	case http.StatusGone:
		return "", errors.New("the device code expired; run `neuron login` again")
	case http.StatusNotFound:
		return "", errors.New("unknown device code; run `neuron login` again")
	default:
		return "", fmt.Errorf("server returned %d while polling for a token", resp.StatusCode)
	}
}

// Account is the subset of account state the CLI needs.
type Account struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Plan      string `json:"plan"`
	Salt      string `json:"salt"`
}

// GetAccount fetches the signed-in account.
func (c *Client) GetAccount(ctx context.Context) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodGet, "/v1/account", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Logout revokes the current token server-side.
func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/logout", nil, nil)
}

// List implements Remote.
func (c *Client) List(ctx context.Context) ([]string, error) {
	var out struct {
		Names []string `json:"names"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/sync/blobs", nil, &out); err != nil {
		return nil, err
	}
	return out.Names, nil
}

// Fetch implements Remote.
func (c *Client) Fetch(ctx context.Context, name string) (*RemoteBlob, error) {
	var out RemoteBlob
	if err := c.do(ctx, http.MethodGet, "/v1/sync/blobs/"+url.PathEscape(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Push implements Remote.
func (c *Client) Push(ctx context.Context, blob *RemoteBlob) error {
	payload := map[string]interface{}{
		"version":  blob.Version,
		"envelope": blob.Envelope,
	}
	return c.do(ctx, http.MethodPut, "/v1/sync/blobs/"+url.PathEscape(blob.Name), payload, nil)
}
