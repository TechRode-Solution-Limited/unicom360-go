// Package unicom360 is the official Go client for the Unicom360 messaging API: SMS, email and
// WhatsApp (templates, session messages, status, template list), balances and health.
//
// It targets the Unicom360 API contract v0.1.0 (see ContractVersion) and uses only the
// standard library.
//
// # Getting started
//
//	client, err := unicom360.NewClient(unicom360.WithAPIKey(os.Getenv("UNICOM360_API_KEY")))
//	if err != nil {
//		log.Fatal(err)
//	}
//	res, err := client.SMS.Send(ctx, unicom360.SMSSendParams{
//		From:    "Unicom360",
//		ToOne:   "260971234567",
//		Message: "Your order ZM4821 is ready.",
//	})
//
// NewClient reads UNICOM360_API_KEY and UNICOM360_BASE_URL when the options are not given. A
// Client is safe for concurrent use; create one and share it.
//
// # Responses and errors
//
// An HTTP 200 is not necessarily a success: the gateway refuses a send (an unknown sender, an
// empty bundle) with HTTP 200 and success: false. The SDK reads the envelope and returns
// either a typed result or an *Error. Partial success is a result: an SMS with one invalid
// number among valid ones succeeds, and the invalid entry is in Invalid().
//
// Every error is an *Error. Check its kind with errors.Is and the Err* sentinels, or read its
// fields with errors.As:
//
//	_, err := client.SMS.Send(ctx, params)
//	switch {
//	case errors.Is(err, unicom360.ErrInsufficientBundle):
//		// top up
//	case errors.Is(err, unicom360.ErrRateLimit):
//		// over the per-minute limit, even after retries
//	case err != nil:
//		var e *unicom360.Error
//		if errors.As(err, &e) {
//			log.Printf("%s %s (api_reference %s)", e.Class(), e.Code, e.APIReference)
//		}
//	}
//
// Error codes are normalised (NormalizeCode): "Invalid From Sender ID" becomes
// INVALID_SENDER_ID, "duplicate user reference id" becomes DUPLICATE_REFERENCE, and so on.
//
// # Retries
//
// The client retries, up to WithMaxRetries times (2 by default):
//
//   - always: an HTTP 429 (the request was rejected before any processing), and a failure
//     before the request was sent (DNS, connection refused, TLS);
//   - for reads (status, templates, balances, health): also a timeout, a dropped connection
//     after sending, and any 5xx;
//   - for SMS and WhatsApp sends: also those, plus a SEND_FAILED refusal, but only when the
//     send carries a user_reference_id (every recipient, for templates). The gateway refuses
//     a reference it has already used, so a retry cannot send twice;
//   - email: never after a timeout or a 5xx, even with a reference (its check has no unique
//     index, so overlapping attempts are not de-duplicated).
//
// A 429 waits until the next UTC minute (the gateway's fixed window) plus up to a second of
// jitter; other retries use full-jitter exponential backoff capped at 8 seconds. With
// WithAutoReferenceIDs the client generates a reference for every send that lacks one.
//
// A DuplicateReferenceError on a call whose Attempts is above 1 means an earlier attempt very
// probably went through: treat the message as sent.
//
// # Webhooks, callbacks and GSM-7
//
// The webhook subpackage verifies signed WhatsApp webhooks (webhook.Verify,
// webhook.ConstructEvent, webhook.Handler) and parses the unsigned SMS and email delivery
// reports. The gsm subpackage counts characters and billing units the way the gateway does.
//
// # Logging
//
// WithLogger takes a *slog.Logger: one debug line per attempt (method, path, status,
// api_reference, attempt, duration) and one info line per scheduled retry. Bodies, headers,
// numbers, addresses and the API key are never logged, and a Client prints its key as
// [REDACTED].
package unicom360
