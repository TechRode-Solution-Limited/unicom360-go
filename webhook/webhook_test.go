package webhook_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
	"github.com/TechRode-Solution-Limited/unicom360-go/webhook"
)

const (
	secret = "krISEvfh1iUWHg5sdWQd0ZnOvE6D6mrCsuFLNEuhGfI"
	body   = `{"created_at":"2026-10-24T09:30:00Z","data":{"message":"This is a test event from Unicom360."},"event":"webhook.test"}`
)

func signedRequest(t *testing.T, payload string, ts int64, sig string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/unicom360", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(webhook.HeaderEvent, "webhook.test")
	r.Header.Set(webhook.HeaderDelivery, "3001")
	r.Header.Set(webhook.HeaderTimestamp, strconv.FormatInt(ts, 10))
	if sig == "" {
		sig = webhook.Sign([]byte(payload), secret, ts)
	}
	r.Header.Set(webhook.HeaderSignature, sig)
	return r
}

func TestSignAndVerify(t *testing.T) {
	now := time.Now()
	sig := webhook.Sign([]byte(body), secret, now.Unix())
	h := http.Header{}
	h.Set(webhook.HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	h.Set(webhook.HeaderSignature, sig)
	if err := webhook.Verify([]byte(body), h, secret); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Several values: the first is used.
	h[webhook.HeaderSignature] = []string{sig, "sha256=" + strings.Repeat("0", 64)}
	if err := webhook.Verify([]byte(body), h, secret); err != nil {
		t.Fatalf("Verify with a repeated header: %v", err)
	}
	// A timestamp too large for 64 bits is out of tolerance, not a crash.
	h.Set(webhook.HeaderTimestamp, "99999999999999999999999")
	err := webhook.Verify([]byte(body), h, secret)
	var e *unicom360.Error
	if !errors.As(err, &e) || e.Reason != webhook.ReasonTimestampOutOfTolerance {
		t.Fatalf("got %v", err)
	}
	// The largest int64 is far in the future; the skew must not overflow into "valid".
	h.Set(webhook.HeaderTimestamp, "9223372036854775807")
	if err := webhook.Verify([]byte(body), h, secret); !errors.Is(err, unicom360.ErrSignatureVerification) {
		t.Fatalf("got %v", err)
	}
	h.Set(webhook.HeaderTimestamp, "0")
	if err := webhook.Verify([]byte(body), h, secret); !errors.Is(err, unicom360.ErrSignatureVerification) {
		t.Fatalf("got %v", err)
	}
}

func TestConstructEventInvalidPayload(t *testing.T) {
	ts := time.Now().Unix()
	for _, payload := range []string{`[1]`, `{"event": 7}`, `not json`, `{}`} {
		r := signedRequest(t, payload, ts, "")
		_, err := webhook.ConstructEvent([]byte(payload), r.Header, secret)
		var e *unicom360.Error
		if !errors.As(err, &e) || e.Type != unicom360.TypeUnicom360Error || e.Class() != "Unicom360Error" {
			t.Errorf("%s: got %v, want a base Unicom360Error", payload, err)
		}
		if errors.Is(err, unicom360.ErrSignatureVerification) {
			t.Errorf("%s: a payload error is not a signature error", payload)
		}
	}
}

func TestHandler(t *testing.T) {
	ts := time.Now().Unix()
	var got webhook.Event
	var fail bool
	h := webhook.Handler(secret, func(ctx context.Context, ev webhook.Event) error {
		got = ev
		if fail {
			return errors.New("database down")
		}
		return nil
	})

	serve := func(r *http.Request) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if code := serve(signedRequest(t, body, ts, "")); code != http.StatusOK {
		t.Fatalf("valid delivery: %d", code)
	}
	ev, ok := got.(*webhook.TestEvent)
	if !ok || ev.Data.Message != "This is a test event from Unicom360." || ev.Meta().DeliveryID != "3001" {
		t.Fatalf("handler got %#v", got)
	}

	got = nil
	if code := serve(signedRequest(t, body, ts, "sha256="+strings.Repeat("a", 64))); code != http.StatusUnauthorized || got != nil {
		t.Fatalf("bad signature: %d (handler called: %t)", code, got != nil)
	}
	if code := serve(signedRequest(t, body, ts-3600, "")); code != http.StatusUnauthorized {
		t.Fatalf("stale delivery: %d", code)
	}
	if code := serve(signedRequest(t, `[]`, ts, "")); code != http.StatusBadRequest {
		t.Fatalf("signed non-event: %d", code)
	}
	fail = true
	if code := serve(signedRequest(t, body, ts, "")); code != http.StatusInternalServerError {
		t.Fatalf("handler error: %d", code)
	}
	get := httptest.NewRequest(http.MethodGet, "/webhooks/unicom360", nil)
	if code := serve(get); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", code)
	}
	big := `{"event":"webhook.test","pad":"` + strings.Repeat("x", webhook.MaxBodyBytes) + `"}`
	if code := serve(signedRequest(t, big, ts, "")); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", code)
	}
}

func TestToleranceOption(t *testing.T) {
	ts := int64(1792829762)
	r := signedRequest(t, body, ts, "")
	at := func(sec int64) webhook.Option {
		return webhook.WithClock(func() time.Time { return time.Unix(sec, 0) })
	}
	if err := webhook.Verify([]byte(body), r.Header, secret, at(ts+299)); err != nil {
		t.Fatal(err)
	}
	if err := webhook.Verify([]byte(body), r.Header, secret, at(ts+299), webhook.WithTolerance(10*time.Second)); err == nil {
		t.Fatal("a 10 s tolerance accepted a 299 s skew")
	}
}
