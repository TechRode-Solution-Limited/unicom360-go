package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

// MaxBodyBytes is the largest body Handler reads (1 MiB).
const MaxBodyBytes = 1 << 20

// Handler returns an http.Handler that receives Unicom360 webhooks: it reads the raw body (at
// most 1 MiB), verifies and parses it with ConstructEvent, and calls handle with the request's
// context and the event.
//
// It answers 200 when handle returns nil, 500 when handle returns an error (the gateway then
// retries the delivery), 401 when verification fails, 400 for a verified body that is not an
// event, 405 for a method other than POST and 413 for a body over 1 MiB. Deliveries are
// retried, so make handle idempotent: de-duplicate by ev.Meta().DeliveryID.
//
//	http.Handle("POST /webhooks/unicom360", webhook.Handler(secret,
//		func(ctx context.Context, ev webhook.Event) error {
//			switch e := ev.(type) {
//			case *webhook.MessageStatusEvent:
//				return markStatus(ctx, e.Data.MessageID, e.Data.Status)
//			}
//			return nil
//		}))
func Handler(secret string, handle func(context.Context, Event) error, opts ...Option) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}
		ev, err := ConstructEvent(body, r.Header, secret, opts...)
		if err != nil {
			if errors.Is(err, unicom360.ErrSignatureVerification) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}
		if err := handle(r.Context(), ev); err != nil {
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
