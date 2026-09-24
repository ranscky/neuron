package cloud

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testWebhookSecret = "whsec_test_secret"

func TestVerifyStripeSignatureAcceptsAValidHeader(t *testing.T) {
	payload := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Now()

	header := SignStripePayload(payload, testWebhookSecret, now)
	if err := VerifyStripeSignature(payload, header, testWebhookSecret, DefaultSignatureTolerance, now); err != nil {
		t.Fatalf("a correctly signed payload must verify: %v", err)
	}
}

func TestVerifyStripeSignatureRejectsATamperedPayload(t *testing.T) {
	payload := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Now()
	header := SignStripePayload(payload, testWebhookSecret, now)

	tampered := []byte(`{"type":"checkout.session.completed","amount":0}`)
	if err := VerifyStripeSignature(tampered, header, testWebhookSecret, DefaultSignatureTolerance, now); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature for a tampered body, got %v", err)
	}
}

func TestVerifyStripeSignatureRejectsTheWrongSecret(t *testing.T) {
	payload := []byte(`{"type":"invoice.paid"}`)
	now := time.Now()
	header := SignStripePayload(payload, "whsec_someone_else", now)

	if err := VerifyStripeSignature(payload, header, testWebhookSecret, DefaultSignatureTolerance, now); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature for the wrong secret, got %v", err)
	}
}

func TestVerifyStripeSignatureRejectsAReplay(t *testing.T) {
	payload := []byte(`{"type":"invoice.paid"}`)
	signedAt := time.Now().Add(-time.Hour)
	header := SignStripePayload(payload, testWebhookSecret, signedAt)

	if err := VerifyStripeSignature(payload, header, testWebhookSecret, DefaultSignatureTolerance, time.Now()); !errors.Is(err, ErrStaleSignature) {
		t.Errorf("expected ErrStaleSignature for an old webhook, got %v", err)
	}
}

func TestVerifyStripeSignatureRejectsMalformedHeaders(t *testing.T) {
	payload := []byte(`{}`)
	now := time.Now()

	valid := SignStripePayload(payload, testWebhookSecret, now)
	notHex := valid[:len(valid)-2] + "zz"

	cases := map[string]string{
		"empty":         "",
		"no timestamp":  "v1=deadbeef",
		"no signature":  "t=1700000000",
		"bad timestamp": "t=notanumber,v1=deadbeef",
		"not hex":       notHex,
	}

	for name, header := range cases {
		if err := VerifyStripeSignature(payload, header, testWebhookSecret, DefaultSignatureTolerance, now); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

// During a secret rotation Stripe sends signatures for both secrets; a match on
// any one of them is valid.
func TestVerifyStripeSignatureAcceptsAnyMatchingSignature(t *testing.T) {
	payload := []byte(`{"type":"invoice.paid"}`)
	now := time.Now()
	ts := now.Unix()

	valid := SignStripePayload(payload, testWebhookSecret, now)
	validHex := valid[strings.Index(valid, "v1=")+len("v1="):]

	header := fmt.Sprintf("t=%d,v1=%s,v1=%s", ts, strings.Repeat("0", 64), validHex)
	if err := VerifyStripeSignature(payload, header, testWebhookSecret, DefaultSignatureTolerance, now); err != nil {
		t.Errorf("a header carrying one valid signature must verify: %v", err)
	}
}

func TestVerifyStripeSignatureRequiresASecret(t *testing.T) {
	if err := VerifyStripeSignature([]byte("{}"), "t=1,v1=00", "", DefaultSignatureTolerance, time.Now()); err == nil {
		t.Error("expected an error when no webhook secret is configured")
	}
}
