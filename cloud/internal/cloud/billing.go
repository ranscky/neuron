package cloud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Errors returned by webhook verification.
var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrStaleSignature   = errors.New("webhook timestamp outside tolerance")
)

// DefaultSignatureTolerance bounds how old a webhook may be, so a captured
// request cannot be replayed indefinitely.
const DefaultSignatureTolerance = 5 * time.Minute

// VerifyStripeSignature checks a Stripe-Signature header against the raw
// request body and the endpoint's signing secret.
//
// Stripe signs "timestamp.payload" with HMAC-SHA256. The header may carry
// several v1 signatures while a secret is being rotated, and any match is
// valid. The comparison is constant time.
func VerifyStripeSignature(payload []byte, header, secret string, tolerance time.Duration, now time.Time) error {
	if secret == "" {
		return errors.New("webhook secret is not configured")
	}
	if tolerance <= 0 {
		tolerance = DefaultSignatureTolerance
	}

	var timestamp int64
	var signatures []string

	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: bad timestamp", ErrInvalidSignature)
			}
			timestamp = parsed
		case "v1":
			signatures = append(signatures, value)
		}
	}

	if timestamp == 0 || len(signatures) == 0 {
		return fmt.Errorf("%w: missing timestamp or signature", ErrInvalidSignature)
	}

	signedAt := time.Unix(timestamp, 0)
	if now.Sub(signedAt) > tolerance || signedAt.Sub(now) > tolerance {
		return ErrStaleSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := mac.Sum(nil)

	for _, signature := range signatures {
		provided, err := hex.DecodeString(signature)
		if err != nil {
			continue
		}
		if hmac.Equal(expected, provided) {
			return nil
		}
	}
	return ErrInvalidSignature
}

// SignStripePayload produces a valid signature header. It exists for tests and
// local tooling; the service itself only ever verifies.
func SignStripePayload(payload []byte, secret string, at time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", at.Unix())
	mac.Write(payload)
	return fmt.Sprintf("t=%d,v1=%s", at.Unix(), hex.EncodeToString(mac.Sum(nil)))
}
