// Package webhook verifies and parses Unicom360 webhooks, and parses the unsigned SMS and
// email delivery reports.
//
// WhatsApp events (message.status, message.received) and the portal's webhook.test are
// signed. Each delivery carries four headers:
//
//	X-Unicom360-Event      the event type (informational; the body's "event" decides)
//	X-Unicom360-Delivery   the delivery id: drop a repeat you have already handled
//	X-Unicom360-Timestamp  unix seconds when it was signed
//	X-Unicom360-Signature  "sha256=" + lowercase hex HMAC-SHA256
//
// The HMAC key is the webhook secret's own UTF-8 bytes (never base64-decode it) and the signed
// message is the timestamp header, ".", then the raw body. Verify checks all of that, rejects
// more than 5 minutes of clock skew, and compares in constant time. ConstructEvent verifies
// and parses; Handler does both behind an http.Handler.
//
// Deliveries are retried (30s, 2m, 10m, 30m, 1h, 3h, 6h, 12h) on any non-2xx answer, so answer
// 2xx quickly and de-duplicate by the delivery id.
//
// SMS and email delivery reports (ParseSMSCallback, ParseEmailCallback) are NOT signed and are
// never retried: anyone who knows your callback URL can post one, and a lost report is not
// sent again. Treat them as hints.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

// Header names.
const (
	HeaderEvent     = "X-Unicom360-Event"
	HeaderDelivery  = "X-Unicom360-Delivery"
	HeaderTimestamp = "X-Unicom360-Timestamp"
	HeaderSignature = "X-Unicom360-Signature"
)

// DefaultTolerance is the clock skew Verify accepts by default, in either direction.
const DefaultTolerance = 300 * time.Second

// Reasons a verification fails (Error.Reason). Checks run in this order; the first failure
// wins.
const (
	ReasonMissingSignature        = "missing_signature"
	ReasonMissingTimestamp        = "missing_timestamp"
	ReasonInvalidTimestamp        = "invalid_timestamp"
	ReasonMalformedSignature      = "malformed_signature"
	ReasonTimestampOutOfTolerance = "timestamp_out_of_tolerance"
	ReasonSignatureMismatch       = "signature_mismatch"
)

// Option configures Verify, ConstructEvent and Handler.
type Option func(*options)

type options struct {
	tolerance time.Duration
	now       func() time.Time
}

// WithTolerance sets the clock skew accepted between the timestamp header and now, in either
// direction (inclusive). The default is 300 seconds.
func WithTolerance(d time.Duration) Option {
	return func(o *options) { o.tolerance = d }
}

// WithClock replaces the clock used for the tolerance check. It is a test seam.
func WithClock(now func() time.Time) Option {
	return func(o *options) {
		if now != nil {
			o.now = now
		}
	}
}

func resolve(opts []Option) options {
	o := options{tolerance: DefaultTolerance, now: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

var (
	timestampRE = regexp.MustCompile(`^[0-9]+$`)
	signatureRE = regexp.MustCompile(`^sha256=[0-9a-f]{64}$`)
)

// Verify checks a delivery's signature. body is the raw request body, exactly as received and
// before any JSON parsing; headers are the request's headers (names are matched without
// regard to case, and the first value of a repeated header is used); secret is the webhook
// secret from the portal.
//
// It returns nil when the delivery is authentic, else an *unicom360.Error of type
// signature_verification_error (errors.Is(err, unicom360.ErrSignatureVerification)) whose
// Reason is one of the Reason* constants.
func Verify(body []byte, headers http.Header, secret string, opts ...Option) error {
	o := resolve(opts)
	signature := header(headers, HeaderSignature)
	if signature == "" {
		return failure(ReasonMissingSignature)
	}
	timestamp := header(headers, HeaderTimestamp)
	if timestamp == "" {
		return failure(ReasonMissingTimestamp)
	}
	if !timestampRE.MatchString(timestamp) {
		return failure(ReasonInvalidTimestamp)
	}
	if !signatureRE.MatchString(signature) {
		return failure(ReasonMalformedSignature)
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		// Too large for 64 bits: certainly outside any tolerance.
		return failure(ReasonTimestampOutOfTolerance)
	}
	skew := o.now().Sub(time.Unix(ts, 0))
	if skew > o.tolerance || skew < -o.tolerance {
		return failure(ReasonTimestampOutOfTolerance)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature[len("sha256="):])) {
		return failure(ReasonSignatureMismatch)
	}
	return nil
}

// Sign returns the X-Unicom360-Signature value for body signed at timestamp (unix seconds)
// with secret. It is what the gateway computes, for testing your own handler.
func Sign(body []byte, secret string, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

var reasonMessages = map[string]string{
	ReasonMissingSignature:        "the X-Unicom360-Signature header is missing or empty",
	ReasonMissingTimestamp:        "the X-Unicom360-Timestamp header is missing or empty",
	ReasonInvalidTimestamp:        "the X-Unicom360-Timestamp header is not a whole number of seconds",
	ReasonMalformedSignature:      "the X-Unicom360-Signature header is not sha256= and 64 lowercase hex digits",
	ReasonTimestampOutOfTolerance: "the delivery's timestamp is outside the allowed clock skew",
	ReasonSignatureMismatch:       "the signature does not match the body and the secret",
}

func failure(reason string) error {
	return &unicom360.Error{
		Type:    unicom360.TypeSignatureVerificationError,
		Reason:  reason,
		Message: reasonMessages[reason],
	}
}

// header returns the first value of the named header, matching the name without regard to
// case even when the map was built by hand with non-canonical keys.
func header(h http.Header, name string) string {
	if v, ok := h[http.CanonicalHeaderKey(name)]; ok {
		return first(v)
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		if strings.EqualFold(k, name) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return first(h[keys[0]])
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
