// Command webhook-server receives Unicom360 webhooks and delivery reports with net/http.
//
//	UNICOM360_WEBHOOK_SECRET=... go run ./examples/webhook-server
//
// It serves:
//
//	POST /webhooks/unicom360  signed WhatsApp events (message.status, message.received,
//	                          webhook.test), verified with webhook.Handler
//	POST /callbacks/sms       unsigned SMS delivery reports
//	POST /callbacks/email     unsigned email delivery reports
//
// Use the first URL as the webhook endpoint in the Unicom360 portal, and the others as the
// callback_url of SMS and email sends. Expose it over https (a reverse proxy or a tunnel) in
// production.
package main

import (
	"context"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/TechRode-Solution-Limited/unicom360-go/webhook"
)

func main() {
	secret := os.Getenv("UNICOM360_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set UNICOM360_WEBHOOK_SECRET to the webhook secret from the Unicom360 portal")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	seen := newDeliveryLog(10_000)
	mux := http.NewServeMux()

	// Signed events. Handler reads the raw body, verifies the signature and the timestamp
	// (5 minutes of skew by default) and answers 401 to anything forged or stale.
	mux.Handle("POST /webhooks/unicom360", webhook.Handler(secret, func(ctx context.Context, ev webhook.Event) error {
		meta := ev.Meta()
		// Failed deliveries are retried (30s, 2m, 10m, ... up to 12h): handle each once.
		if !seen.first(meta.DeliveryID) {
			return nil
		}
		switch e := ev.(type) {
		case *webhook.MessageStatusEvent:
			slog.Info("whatsapp status", "message_id", e.Data.MessageID, "status", e.Data.Status,
				"user_reference_id", e.Data.UserReferenceID)
			if e.Data.Error != nil {
				slog.Warn("whatsapp delivery failed", "message_id", e.Data.MessageID, "reason", e.Data.Error.Message)
			}
		case *webhook.MessageReceivedEvent:
			// The session window is open until e.Data.WindowExpiresAt: reply with a session
			// message (client.WhatsApp.SendSession) before then.
			slog.Info("whatsapp message", "type", e.Data.Type, "message_id", e.Data.MessageID,
				"window_expires_at", e.Data.WindowExpiresAt)
		case *webhook.TestEvent:
			slog.Info("webhook test received", "message", e.Data.Message)
		case *webhook.UnknownEvent:
			slog.Info("unknown event type, ignored", "event", e.Event)
		}
		return nil // 200; an error answers 500 and the gateway retries
	}))

	// Unsigned delivery reports: anyone who knows these URLs can post to them, and a lost
	// report is never resent, so treat them as hints (check the message id is one you sent).
	mux.HandleFunc("POST /callbacks/sms", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		report, err := webhook.ParseSMSCallback(body)
		if err != nil {
			http.Error(w, "not a delivery report", http.StatusBadRequest)
			return
		}
		slog.Info("sms report", "message_id", report.MessageID, "status", report.Status)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /callbacks/email", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		report, err := webhook.ParseEmailCallback(body)
		if err != nil {
			http.Error(w, "not a delivery report", http.StatusBadRequest)
			return
		}
		slog.Info("email report", "message_id", report.MessageID, "status", report.Status, "detail", report.Message)
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	slog.Info("listening", "addr", addr)
	log.Fatal(srv.ListenAndServe())
}

// deliveryLog remembers recent delivery ids, so a retried delivery is handled once. A real
// service would keep them in its database.
type deliveryLog struct {
	mu    sync.Mutex
	limit int
	ids   map[string]struct{}
	order []string
}

func newDeliveryLog(limit int) *deliveryLog {
	return &deliveryLog{limit: limit, ids: map[string]struct{}{}}
}

// first reports whether id has not been seen before, and records it.
func (d *deliveryLog) first(id string) bool {
	if id == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.ids[id]; ok {
		return false
	}
	d.ids[id] = struct{}{}
	d.order = append(d.order, id)
	if len(d.order) > d.limit {
		delete(d.ids, d.order[0])
		d.order = d.order[1:]
	}
	return true
}
