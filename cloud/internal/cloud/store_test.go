package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "cloud.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestAccountCreateAndGet(t *testing.T) {
	store := testStore(t)

	account, err := store.CreateAccount("dev@example.com", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if account.ID == "" || account.Plan != PlanFree {
		t.Fatalf("unexpected account: %+v", account)
	}
	if account.Salt == "" {
		t.Error("salt must be stored so another machine can derive the key")
	}

	got, err := store.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "dev@example.com" {
		t.Errorf("unexpected email: %q", got.Email)
	}

	if _, err := store.GetAccount("acct_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPutBlobRejectsStaleVersions(t *testing.T) {
	store := testStore(t)
	account, err := store.CreateAccount("", []byte("salt"))
	if err != nil {
		t.Fatal(err)
	}

	first := &Blob{Name: "github", Version: 2, Envelope: json.RawMessage(`{"v":1}`)}
	if err := store.PutBlob(account.ID, first); err != nil {
		t.Fatal(err)
	}

	// A replayed or stale push must not roll the configuration backwards.
	stale := &Blob{Name: "github", Version: 2, Envelope: json.RawMessage(`{"v":1}`)}
	if err := store.PutBlob(account.ID, stale); !errors.Is(err, ErrStaleVersion) {
		t.Errorf("expected ErrStaleVersion for an equal version, got %v", err)
	}

	older := &Blob{Name: "github", Version: 1, Envelope: json.RawMessage(`{"v":1}`)}
	if err := store.PutBlob(account.ID, older); !errors.Is(err, ErrStaleVersion) {
		t.Errorf("expected ErrStaleVersion for an older version, got %v", err)
	}

	newer := &Blob{Name: "github", Version: 3, Envelope: json.RawMessage(`{"v":1}`)}
	if err := store.PutBlob(account.ID, newer); err != nil {
		t.Errorf("a newer version should be accepted: %v", err)
	}
}

func TestBlobsAreIsolatedPerAccount(t *testing.T) {
	store := testStore(t)
	a, _ := store.CreateAccount("a@example.com", []byte("salt-a"))
	b, _ := store.CreateAccount("b@example.com", []byte("salt-b"))

	if err := store.PutBlob(a.ID, &Blob{Name: "github", Version: 1, Envelope: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetBlob(b.ID, "github"); !errors.Is(err, ErrNotFound) {
		t.Error("one account must not see another account's blob")
	}

	names, err := store.ListBlobs(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "github" {
		t.Errorf("unexpected blob list: %v", names)
	}
}

func TestDeviceFlowFromPendingToToken(t *testing.T) {
	store := testStore(t)
	account, _ := store.CreateAccount("dev@example.com", []byte("salt"))

	code, err := store.CreateDeviceCode(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(code.UserCode) != 9 || code.UserCode[4] != '-' {
		t.Errorf("user code should be human-typable, got %q", code.UserCode)
	}

	// Before approval the client must be told to keep waiting.
	if _, err := store.PollDeviceCode(code.DeviceCode, time.Hour); !errors.Is(err, ErrPending) {
		t.Errorf("expected ErrPending before approval, got %v", err)
	}

	if err := store.ApproveDeviceCode(code.UserCode, account.ID); err != nil {
		t.Fatal(err)
	}

	token, err := store.PollDeviceCode(code.DeviceCode, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected an access token")
	}

	accountID, err := store.ResolveToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if accountID != account.ID {
		t.Errorf("token resolved to %q, want %q", accountID, account.ID)
	}

	// A device code is single use.
	if _, err := store.PollDeviceCode(code.DeviceCode, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected the device code to be consumed, got %v", err)
	}
}

func TestExpiredDeviceCodeIsRejected(t *testing.T) {
	store := testStore(t)
	account, _ := store.CreateAccount("dev@example.com", []byte("salt"))

	code, err := store.CreateDeviceCode(-time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveDeviceCode(code.UserCode, account.ID); !errors.Is(err, ErrExpired) {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestRevokedTokenIsRejected(t *testing.T) {
	store := testStore(t)
	account, _ := store.CreateAccount("dev@example.com", []byte("salt"))

	code, _ := store.CreateDeviceCode(time.Hour)
	if err := store.ApproveDeviceCode(code.UserCode, account.ID); err != nil {
		t.Fatal(err)
	}
	token, err := store.PollDeviceCode(code.DeviceCode, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.RevokeToken(token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveToken(token); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected a revoked token to be rejected, got %v", err)
	}
}

func TestTokensAreNotStoredInPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := store.CreateAccount("dev@example.com", []byte("salt"))

	code, _ := store.CreateDeviceCode(time.Hour)
	if err := store.ApproveDeviceCode(code.UserCode, account.ID); err != nil {
		t.Fatal(err)
	}
	token, err := store.PollDeviceCode(code.DeviceCode, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveToken(token); err != nil {
		t.Fatalf("token should survive a restart: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token)) {
		t.Error("the raw access token must not appear in the store file")
	}
}

func TestSetPlanAndStripe(t *testing.T) {
	store := testStore(t)
	account, _ := store.CreateAccount("dev@example.com", []byte("salt"))

	if err := store.SetPlan(account.ID, PlanPro); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStripe(account.ID, "cus_123", "sub_456"); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Plan != PlanPro || got.StripeCustomerID != "cus_123" || got.StripeSubscriptionID != "sub_456" {
		t.Errorf("unexpected account after upgrade: %+v", got)
	}
}
