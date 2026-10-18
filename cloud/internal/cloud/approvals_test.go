package cloud

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postForm submits the activation page's form the way a browser would.
func postForm(t *testing.T, target string, values url.Values) (*http.Response, string) {
	t.Helper()

	resp, err := http.PostForm(target, values)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(raw)
}

// newDeviceCode starts a device authorisation and returns its user code.
func newDeviceCode(t *testing.T, ts *httptest.Server) string {
	t.Helper()

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: status %d body %v", resp.StatusCode, body)
	}
	code, _ := body["user_code"].(string)
	if code == "" {
		t.Fatalf("no user code in %v", body)
	}
	return code
}

// redeem exchanges a device code for an access token, proving it was approved.
func redeem(t *testing.T, ts *httptest.Server, deviceCode string) string {
	t.Helper()

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/token", "", map[string]string{"device_code": deviceCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token: status %d body %v", resp.StatusCode, body)
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatalf("no access token in %v", body)
	}
	return token
}

func TestActivatePageRendersAForm(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/activate")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	page := string(raw)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	for _, want := range []string{`name="user_code"`, `name="account_id"`, `action="/activate"`, "Approve this device"} {
		if !strings.Contains(page, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	// The page must not render untrusted input as markup.
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("the activation page must not be cached, got %q", resp.Header.Get("Cache-Control"))
	}
}

// The whole device flow, finished in the browser with the code and account id —
// this is the bootstrap path, where no machine has a session yet.
func TestActivateFormApprovesWithCodeAndAccountID(t *testing.T) {
	_, ts := newTestServer(t)

	salt := "MDEyMzQ1Njc4OWFiY2RlZg=="
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/accounts", "", map[string]string{"salt": salt})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create account: %d %v", resp.StatusCode, body)
	}
	accountID, _ := body["account_id"].(string)

	deviceCodeRawResp, deviceBody := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if deviceCodeRawResp.StatusCode != http.StatusOK {
		t.Fatalf("device code: %d %v", deviceCodeRawResp.StatusCode, deviceBody)
	}
	userCode, _ := deviceBody["user_code"].(string)
	deviceCode, _ := deviceBody["device_code"].(string)

	resp2, page := postForm(t, ts.URL+"/activate", url.Values{
		"user_code":  {userCode},
		"account_id": {accountID},
	})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("approve: status %d page %s", resp2.StatusCode, page)
	}
	if !strings.Contains(page, "Device approved") {
		t.Errorf("expected a success page, got:\n%s", page)
	}

	redeem(t, ts, deviceCode)
}

// The link path: a signed-in client mints a one-time link, the browser clicks
// once, and the token is burned.
func TestApprovalLinkApprovesInTheBrowser(t *testing.T) {
	_, ts := newTestServer(t)
	token, _ := signIn(t, ts)

	resp, deviceBody := doJSON(t, http.MethodPost, ts.URL+"/v1/device/code", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: %d %v", resp.StatusCode, deviceBody)
	}
	userCode, _ := deviceBody["user_code"].(string)
	deviceCode, _ := deviceBody["device_code"].(string)

	resp, linkBody := doJSON(t, http.MethodPost, ts.URL+"/v1/device/approvals", token, map[string]string{"user_code": userCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approvals: %d %v", resp.StatusCode, linkBody)
	}
	link, _ := linkBody["url"].(string)
	approvalToken, _ := linkBody["approval_token"].(string)
	if link == "" || approvalToken == "" {
		t.Fatalf("no approval link in %v", linkBody)
	}
	if !strings.HasSuffix(link, "/activate?token="+approvalToken) {
		t.Errorf("unexpected link %q", link)
	}

	// Opening the link shows what is about to be approved, without consuming it.
	pageResp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(pageResp.Body)
	pageResp.Body.Close()
	page := string(raw)
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("activate page: status %d", pageResp.StatusCode)
	}
	if !strings.Contains(page, userCode) {
		t.Errorf("the page should show the code being approved:\n%s", page)
	}
	if !strings.Contains(page, `name="token"`) {
		t.Errorf("the page should carry the approval token:\n%s", page)
	}

	// One click approves.
	postResp, approvedPage := postForm(t, ts.URL+"/activate", url.Values{"token": {approvalToken}})
	if postResp.StatusCode != http.StatusOK || !strings.Contains(approvedPage, "Device approved") {
		t.Fatalf("approve: status %d page %s", postResp.StatusCode, approvedPage)
	}
	redeem(t, ts, deviceCode)

	// The link is single-use: replaying it must fail and must not approve again.
	replayResp, replayPage := postForm(t, ts.URL+"/activate", url.Values{"token": {approvalToken}})
	if replayResp.StatusCode == http.StatusOK && strings.Contains(replayPage, "Device approved") {
		t.Fatal("an approval link must not work twice")
	}
}

func TestApprovalLinkRequiresASession(t *testing.T) {
	_, ts := newTestServer(t)
	userCode := newDeviceCode(t, ts)

	resp, _ := doJSON(t, http.MethodPost, ts.URL+"/v1/device/approvals", "", map[string]string{"user_code": userCode})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestApprovalLinkRejectsAnUnknownCode(t *testing.T) {
	_, ts := newTestServer(t)
	token, _ := signIn(t, ts)

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/approvals", token, map[string]string{"user_code": "ZZZZ-ZZZZ"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body %v", resp.StatusCode, body)
	}
}

func TestActivatePageRejectsAnUnknownToken(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/activate?token=nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(raw), "not valid") {
		t.Errorf("expected an invalid-link message, got:\n%s", raw)
	}
}

// A signed-in client can approve without repeating its account id.
func TestDeviceApproveWorksWithASession(t *testing.T) {
	_, ts := newTestServer(t)
	token, _ := signIn(t, ts)
	userCode := newDeviceCode(t, ts)

	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/device/approve", token, map[string]string{"user_code": userCode})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve with a session: %d %v", resp.StatusCode, body)
	}
}

func TestActivateFormRejectsMissingAccount(t *testing.T) {
	_, ts := newTestServer(t)
	userCode := newDeviceCode(t, ts)

	resp, page := postForm(t, ts.URL+"/activate", url.Values{"user_code": {userCode}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if !strings.Contains(page, "Enter an account id") {
		t.Errorf("expected guidance about the account id, got:\n%s", page)
	}
}
