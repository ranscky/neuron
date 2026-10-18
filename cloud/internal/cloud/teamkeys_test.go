package cloud

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// b64Bytes returns n random bytes, base64 encoded — enough to act like a wrapped
// key in these tests.
func b64Bytes(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// publicKeyB64 returns a 32-byte key in the URL-safe form the API canonicalises
// to, so it can be placed in a path segment.
func publicKeyB64(t *testing.T) string { return b64KeyBytes(t, 32) }

// b64KeyBytes returns n random bytes, URL-safe base64 encoded.
func b64KeyBytes(t *testing.T, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func createWrappedTeam(t *testing.T, ts *httptest.Server, token, name string) string {
	t.Helper()
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/teams", token, map[string]string{"name": name})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create team: %d %v", resp.StatusCode, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("no team id in %v", body)
	}
	return id
}

func TestTeamKeyRecordsRoundTrip(t *testing.T) {
	server, ts := newTestServer(t)
	ownerToken, _ := signInPro(t, server, ts)
	teamID := createWrappedTeam(t, ts, ownerToken, "acme")

	publicKey := publicKeyB64(t)
	wrapped := b64Bytes(t, 92)

	// Register the device without a key first, the way `team join` does.
	resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", ownerToken,
		map[string]string{"public_key": publicKey})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register device: %d %v", resp.StatusCode, body)
	}
	if body["has_key"] != false {
		t.Errorf("a freshly registered device has no key yet: %v", body)
	}

	// Then attach the wrapped team key.
	resp, body = doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", ownerToken,
		map[string]string{"public_key": publicKey, "wrapped_key": wrapped})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("store wrapped key: %d %v", resp.StatusCode, body)
	}
	if body["has_key"] != true {
		t.Errorf("expected has_key after storing: %v", body)
	}

	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID+"/keys", ownerToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list keys: %d %v", resp.StatusCode, body)
	}
	keys, _ := body["keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("expected one device, got %v", body)
	}
	record, _ := keys[0].(map[string]any)
	if record["public_key"] != publicKey || record["has_key"] != true {
		t.Errorf("record = %v", record)
	}
	if record["wrapped_key"] != wrapped {
		t.Errorf("the owner should see their own wrapped key: %v", record)
	}
}

// A newcomer registers a device; any member who holds the key wraps it for them,
// and the wrapped key is only ever shown to the device it belongs to.
func TestTeamKeyGrantedToANewMember(t *testing.T) {
	server, ts := newTestServer(t)
	ownerToken, _ := signInPro(t, server, ts)
	teamID := createWrappedTeam(t, ts, ownerToken, "acme")

	ownerKey := publicKeyB64(t)
	ownerWrapped := b64Bytes(t, 92)
	if resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", ownerToken,
		map[string]string{"public_key": ownerKey, "wrapped_key": ownerWrapped}); resp.StatusCode != http.StatusOK {
		t.Fatalf("owner key: %d %v", resp.StatusCode, body)
	}

	// A second account joins with the invite code.
	_, team := doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID, ownerToken, nil)
	invite, _ := team["invite_code"].(string)
	memberToken, _ := signInPro(t, server, ts)
	resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/teams/join", memberToken, map[string]string{"invite_code": invite})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %v", resp.StatusCode, body)
	}

	memberKey := publicKeyB64(t)
	if resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", memberToken,
		map[string]string{"public_key": memberKey}); resp.StatusCode != http.StatusOK {
		t.Fatalf("member registers device: %d %v", resp.StatusCode, body)
	}

	memberWrapped := b64Bytes(t, 92)
	resp, body = doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys/"+url.PathEscape(memberKey), ownerToken,
		map[string]string{"wrapped_key": memberWrapped})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d %v", resp.StatusCode, body)
	}

	// The member sees their own wrapped key...
	resp, body = doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID+"/keys", memberToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member list: %d %v", resp.StatusCode, body)
	}
	var memberRecord, ownerRecord map[string]any
	for _, raw := range body["keys"].([]any) {
		record := raw.(map[string]any)
		if record["public_key"] == memberKey {
			memberRecord = record
		}
		if record["public_key"] == ownerKey {
			ownerRecord = record
		}
	}
	if memberRecord == nil || memberRecord["wrapped_key"] != memberWrapped {
		t.Errorf("member should see their wrapped key: %v", memberRecord)
	}
	// ...but never anyone else's.
	if ownerRecord == nil {
		t.Fatal("the roster should include the owner's device")
	}
	if _, leaked := ownerRecord["wrapped_key"]; leaked {
		t.Errorf("a member must not see another device's wrapped key: %v", ownerRecord)
	}
	if ownerRecord["has_key"] != true {
		t.Errorf("the roster should say the owner has a key: %v", ownerRecord)
	}
}

func TestTeamKeyOverwriteNeedsForceFromTheOwner(t *testing.T) {
	server, ts := newTestServer(t)
	ownerToken, _ := signInPro(t, server, ts)
	teamID := createWrappedTeam(t, ts, ownerToken, "acme")

	_, team := doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID, ownerToken, nil)
	memberToken, _ := signInPro(t, server, ts)
	if resp, body := doJSON(t, http.MethodPost, ts.URL+"/v1/teams/join", memberToken,
		map[string]string{"invite_code": team["invite_code"].(string)}); resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %v", resp.StatusCode, body)
	}

	publicKey := publicKeyB64(t)
	first := b64Bytes(t, 92)
	if resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", memberToken,
		map[string]string{"public_key": publicKey, "wrapped_key": first}); resp.StatusCode != http.StatusOK {
		t.Fatalf("member key: %d %v", resp.StatusCode, body)
	}

	// Another member may not overwrite it, even with force.
	second := b64Bytes(t, 92)
	resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys/"+url.PathEscape(publicKey), ownerToken,
		map[string]string{"wrapped_key": second})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 without force, got %d %v", resp.StatusCode, body)
	}

	// The owner may.
	resp, body = doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys/"+url.PathEscape(publicKey), ownerToken,
		map[string]any{"wrapped_key": second, "force": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner force: %d %v", resp.StatusCode, body)
	}
}

func TestTeamKeysRejectOutsidersAndBadKeys(t *testing.T) {
	server, ts := newTestServer(t)
	ownerToken, _ := signInPro(t, server, ts)
	teamID := createWrappedTeam(t, ts, ownerToken, "acme")

	outsiderToken, _ := signInPro(t, server, ts)
	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/v1/teams/"+teamID+"/keys", outsiderToken, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("outsider list: got %d, want 403", resp.StatusCode)
	}

	for _, bad := range []string{"", "not-base64!!", b64KeyBytes(t, 16)} {
		resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", ownerToken,
			map[string]string{"public_key": bad})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("public key %q: got %d, want 400 (%v)", bad, resp.StatusCode, body)
		}
	}

	// A wrapped key of an implausible size is refused too.
	publicKey := publicKeyB64(t)
	resp, body := doJSON(t, http.MethodPut, ts.URL+"/v1/teams/"+teamID+"/keys", ownerToken,
		map[string]string{"public_key": publicKey, "wrapped_key": b64Bytes(t, 4096)})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized wrapped key: got %d, want 400 (%v)", resp.StatusCode, body)
	}
}
