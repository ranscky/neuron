package cloud

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
)

// errNoAccount lets the approval layer distinguish "you must say which account"
// from "that account or code does not exist".
var errNoAccount = errors.New("an account or approval link is required")

// handleCreateApproval mints a one-time link that approves a pending device code
// in a browser. It requires a session, so only a signed-in machine can hand out
// approval — the browser never sees a session token or an account id.
func (s *Server) handleCreateApproval(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.allow(w, r, "approval:"+clientIP(r)) {
		return
	}

	var req struct {
		UserCode string `json:"user_code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	userCode := strings.ToUpper(strings.TrimSpace(req.UserCode))
	if userCode == "" {
		writeError(w, http.StatusBadRequest, "user_code is required")
		return
	}

	token, err := s.Store.CreateApproval(userCode, accountID, s.deviceTTL)
	switch {
	case err == nil:
	case errors.Is(err, ErrExpired):
		writeError(w, http.StatusGone, "that code has expired")
		return
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "unknown or already-used code")
		return
	default:
		writeError(w, http.StatusInternalServerError, "could not create an approval link")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"approval_token": token,
		"url":            strings.TrimRight(s.publicBaseURL(r), "/") + "/activate?token=" + token,
		"expires_in":     int(s.deviceTTL.Seconds()),
	})
}

// handleActivatePage serves the page the device flow advertises as its
// verification URI. It does not need a session: the page authorises through an
// approval link, or through the code plus account id when bootstrapping the very
// first device (there is no session anywhere yet at that point).
func (s *Server) handleActivatePage(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, "activate:"+clientIP(r)) {
		return
	}

	view := activateView{}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		approval, err := s.Store.PeekApproval(token)
		switch {
		case err == nil:
			view.Token = token
			view.UserCode = approval.UserCode
			if account, err := s.Store.GetAccount(approval.AccountID); err == nil {
				view.Email = account.Email
			}
		case errors.Is(err, ErrExpired):
			view.Error = "That approval link has expired. Ask the other machine for a new one."
		default:
			view.Error = "That approval link is not valid — it may already have been used."
		}
	}

	s.renderActivate(w, http.StatusOK, view)
}

// handleActivateForm approves a device from the page's form.
func (s *Server) handleActivateForm(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, r, "activate:"+clientIP(r)) {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderActivate(w, http.StatusBadRequest, activateView{Error: "That form could not be read."})
		return
	}

	token := strings.TrimSpace(r.PostFormValue("token"))
	userCode := strings.TrimSpace(r.PostFormValue("user_code"))
	accountID := strings.TrimSpace(r.PostFormValue("account_id"))

	switch err := s.approveDevice(userCode, accountID, token); {
	case err == nil:
		s.renderActivate(w, http.StatusOK, activateView{Approved: true, UserCode: userCode})
	case errors.Is(err, errNoAccount):
		s.renderActivate(w, http.StatusBadRequest, activateView{
			UserCode: userCode,
			Error:    "Enter an account id, or open the approval link from the machine that is already signed in.",
		})
	case errors.Is(err, ErrExpired):
		s.renderActivate(w, http.StatusGone, activateView{
			UserCode: userCode,
			Error:    "That code or link has expired. Start again to get a fresh one.",
		})
	case errors.Is(err, ErrNotFound):
		s.renderActivate(w, http.StatusNotFound, activateView{
			UserCode: userCode,
			Error:    "Unknown or already-used code. Check the code on the other machine.",
		})
	default:
		s.renderActivate(w, http.StatusInternalServerError, activateView{
			Error: "Could not approve that device. Try again.",
		})
	}
}

// approveDevice is the single place that decides what authorises an approval:
// a one-time link, or an account the caller already proved it knows.
func (s *Server) approveDevice(userCode, accountID, approvalToken string) error {
	if approvalToken != "" {
		_, err := s.Store.ConsumeApproval(approvalToken)
		return err
	}
	if accountID == "" {
		return errNoAccount
	}
	return s.Store.ApproveDeviceCode(strings.ToUpper(strings.TrimSpace(userCode)), accountID)
}

// activateView is what the activation page renders.
type activateView struct {
	Token    string // set when an approval link is valid
	UserCode string
	Email    string
	Error    string
	Approved bool
	Server   string
	// SSO is true when the service has an identity provider configured.
	SSO bool
}

func (s *Server) renderActivate(w http.ResponseWriter, status int, view activateView) {
	view.SSO = s.oidcConfigured()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = activateTemplate.Execute(w, view)
}

var activateTemplate = template.Must(template.New("activate").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Approve a device — Neuron</title>
<style>
  :root { color-scheme: light dark; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center;
         font: 16px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
         background: #f6f7f9; color: #16181d; }
  main { width: min(30rem, calc(100vw - 2rem)); background: #fff; border: 1px solid #e2e5ea;
         border-radius: 14px; padding: 2rem; box-shadow: 0 1px 2px rgba(16,24,40,.06); }
  h1 { font-size: 1.25rem; margin: 0 0 .25rem; letter-spacing: -.01em; }
  p { margin: 0 0 1rem; color: #47505f; }
  .code { font: 600 1.5rem/1.2 ui-monospace, SFMono-Regular, Menlo, monospace;
          letter-spacing: .08em; padding: .75rem 1rem; background: #f2f4f7;
          border-radius: 10px; text-align: center; margin: 0 0 1rem; }
  label { display: block; font-size: .8125rem; font-weight: 600; margin: 0 0 .35rem; }
  input { width: 100%; box-sizing: border-box; padding: .6rem .7rem; margin: 0 0 .9rem;
          border: 1px solid #d3d8e0; border-radius: 9px; font: inherit; background: #fff; color: inherit; }
  button { width: 100%; padding: .7rem 1rem; border: 0; border-radius: 9px;
           background: #16181d; color: #fff; font: inherit; font-weight: 600; cursor: pointer; }
  button:hover { background: #2a2d36; }
  .error { background: #fdecec; border: 1px solid #f5c2c2; color: #8a1c1c;
           padding: .75rem .9rem; border-radius: 9px; margin: 0 0 1rem; }
  .ok { background: #eaf7ee; border: 1px solid #bfe5cb; color: #14532d;
        padding: .75rem .9rem; border-radius: 9px; margin: 0 0 1rem; }
  hr { border: 0; border-top: 1px solid #e2e5ea; margin: 1.5rem 0; }
  .hint { font-size: .8125rem; color: #667085; }
  @media (prefers-color-scheme: dark) {
    body { background: #0f1115; color: #e8eaee; }
    main { background: #16181d; border-color: #262a32; }
    p, .hint { color: #9aa2b1; }
    .code { background: #1d2027; }
    input { background: #12141a; border-color: #2b303a; }
    button { background: #e8eaee; color: #12141a; }
    button:hover { background: #fff; }
    .error { background: #2a1518; border-color: #5b2026; color: #ffb4b4; }
    .ok { background: #10241a; border-color: #1f4d34; color: #9ae6b4; }
    hr { border-top-color: #262a32; }
  }
</style>
</head>
<body>
<main>
{{if .Approved}}
  <h1>Device approved</h1>
  <div class="ok">You can go back to your terminal — it will finish signing in on its own.</div>
  <p class="hint">You can close this tab.</p>
{{else if .Token}}
  <h1>Approve this device?</h1>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  {{if .Email}}<p>Signing in <strong>{{.Email}}</strong>.</p>{{else}}<p>A device is asking to sign in.</p>{{end}}
  <div class="code">{{.UserCode}}</div>
  <form method="post" action="/activate">
    <input type="hidden" name="token" value="{{.Token}}">
    <button type="submit">Approve this device</button>
  </form>
  <p class="hint">If you did not expect this, close the tab and do not approve.</p>
{{else}}
  <h1>Approve a device</h1>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  <p>Neuron is waiting for this device to be approved. Paste the code it shows, or
     open the approval link from a machine that is already signed in.</p>
  <form method="post" action="/activate">
    <label for="user_code">Code from the terminal</label>
    <input id="user_code" name="user_code" value="{{.UserCode}}" placeholder="K7QP-3M2X"
           autocomplete="off" autocapitalize="characters" spellcheck="false" required>
    <label for="account_id">Account id (only when signing in for the first time)</label>
    <input id="account_id" name="account_id" placeholder="acct_…" autocomplete="off" spellcheck="false">
    <button type="submit">Approve this device</button>
  </form>
  <hr>
  <form method="get" action="/activate">
    <label for="token">Approval link token</label>
    <input id="token" name="token" placeholder="paste from the other machine" autocomplete="off" spellcheck="false">
    <button type="submit">Open approval</button>
  </form>
  {{if .SSO}}
  <hr>
  <form method="get" action="/v1/auth/oidc/start">
    <label for="sso_code">Or sign in with your identity provider</label>
    <input id="sso_code" name="user_code" value="{{.UserCode}}" placeholder="K7QP-3M2X"
           autocomplete="off" autocapitalize="characters" spellcheck="false" required>
    <button type="submit">Sign in and approve</button>
  </form>
  {{end}}
  <p class="hint">Your passphrase never leaves your machine; the service only ever stores ciphertext.</p>
{{end}}
</main>
</body>
</html>
`))
