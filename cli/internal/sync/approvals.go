package sync

import (
	"context"
	"errors"
	"net/http"
)

// ApproveLink is a one-time browser link that approves a pending device code.
type ApproveLink struct {
	URL   string `json:"url"`
	Token string `json:"approval_token"`
}

// CreateApprovalLink asks the service for a link that approves a pending device
// code in a browser.
//
// The caller must be signed in: minting the link is what proves the account, and
// the link itself is single-use and short-lived. That is how approval reaches a
// phone or another browser without a web login.
func (c *Client) CreateApprovalLink(ctx context.Context, userCode string) (*ApproveLink, error) {
	var out ApproveLink
	if err := c.do(ctx, http.MethodPost, "/v1/device/approvals", map[string]string{"user_code": userCode}, &out); err != nil {
		return nil, err
	}
	if out.URL == "" {
		return nil, errors.New("the service returned no approval link")
	}
	return &out, nil
}

// ActivateURL resolves the service's activation page from a device-code
// response, which may carry either an absolute URL or a path.
func ActivateURL(serverURL, verificationURI string) string {
	base := trimSlash(serverURL)
	if verificationURI == "" {
		return base + "/activate"
	}
	if len(verificationURI) > 0 && verificationURI[0] == '/' {
		return base + verificationURI
	}
	return verificationURI
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
