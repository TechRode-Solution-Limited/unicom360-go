package webhook_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
	"github.com/TechRode-Solution-Limited/unicom360-go/webhook"
)

// A delivery as the gateway sends it, signed here with the example secret.
func exampleDelivery() (*http.Request, string) {
	const secret = "_p_1O9_F0Ntqznvs5yv1hE8-EVvIBFrIt10-7xoqVB8"
	payload := `{"created_at":"2026-10-24T08:16:02Z","data":{"billable":true,"category":"UTILITY","channel":"whatsapp",` +
		`"error":null,"from":"260960123456","message_id":"7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f","status":"delivered",` +
		`"timestamp":"2026-10-24T08:16:01Z","to":"260971234567","user_reference_id":"inv-42"},"event":"message.status"}`
	ts := time.Now().Unix()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/unicom360", strings.NewReader(payload))
	r.Header.Set("X-Unicom360-Event", "message.status")
	r.Header.Set("X-Unicom360-Delivery", "1042")
	r.Header.Set("X-Unicom360-Timestamp", strconv.FormatInt(ts, 10))
	r.Header.Set("X-Unicom360-Signature", webhook.Sign([]byte(payload), secret, ts))
	return r, secret
}

func ExampleHandler() {
	secret := "_p_1O9_F0Ntqznvs5yv1hE8-EVvIBFrIt10-7xoqVB8" // os.Getenv("UNICOM360_WEBHOOK_SECRET")

	handler := webhook.Handler(secret, func(ctx context.Context, ev webhook.Event) error {
		// Deliveries are retried until you answer 2xx: drop repeats by delivery id.
		fmt.Println("delivery", ev.Meta().DeliveryID)
		switch e := ev.(type) {
		case *webhook.MessageStatusEvent:
			fmt.Println(e.Data.MessageID, e.Data.Status)
		case *webhook.MessageReceivedEvent:
			fmt.Println(e.Data.From, "wrote", e.Data.Text)
		case *webhook.TestEvent:
			fmt.Println("test:", e.Data.Message)
		}
		return nil
	})
	// In a server: http.Handle("POST /webhooks/unicom360", handler)

	r, _ := exampleDelivery()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	fmt.Println(w.Code)
	// Output:
	// delivery 1042
	// 7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f delivered
	// 200
}

func ExampleConstructEvent() {
	r, secret := exampleDelivery()

	// Read the raw body before any JSON parsing: the signature covers the exact bytes.
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		log.Fatal(err)
	}
	ev, err := webhook.ConstructEvent(payload, r.Header, secret)
	if errors.Is(err, unicom360.ErrSignatureVerification) {
		fmt.Println("rejected:", err)
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if e, ok := ev.(*webhook.MessageStatusEvent); ok {
		fmt.Println(e.Event, e.Data.UserReferenceID, e.Data.Status)
	}
	// Output: message.status inv-42 delivered
}

func ExampleVerify() {
	r, secret := exampleDelivery()
	payload, _ := io.ReadAll(r.Body)

	err := webhook.Verify(payload, r.Header, secret)
	fmt.Println("authentic:", err == nil)

	tampered := append([]byte(nil), payload...)
	tampered[len(tampered)-3] = 'X'
	err = webhook.Verify(tampered, r.Header, secret)
	var e *unicom360.Error
	if errors.As(err, &e) {
		fmt.Println("tampered:", e.Reason)
	}
	// Output:
	// authentic: true
	// tampered: signature_mismatch
}

func ExampleParseSMSCallback() {
	// SMS delivery reports are NOT signed and never retried: treat them as hints.
	report, err := webhook.ParseSMSCallback([]byte(
		`{"message_id":"dff31690-b450-4000-a6b7-141d8523ef10","status":"DELIVERED","mobile_number":"260971234567"}`))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(report.MessageID, report.Status)
	// Output: dff31690-b450-4000-a6b7-141d8523ef10 DELIVERED
}

func ExampleParseEmailCallback() {
	// The email report sends message_id as a JSON number; the SDK gives it as a string.
	report, err := webhook.ParseEmailCallback([]byte(
		`{"message_id":48213,"status":"DELIVERED","email_address":"jane@example.com","message":"success"}`))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(report.MessageID, report.Status, report.EmailAddress)
	// Output: 48213 DELIVERED jane@example.com
}
