# Unicom360 Go SDK

The official Go client for the [Unicom360](https://unicom360.com) messaging API: SMS, email,
WhatsApp templates and session messages, message status, balances and health, plus webhook
verification and GSM-7 helpers.

* Targets **Unicom360 API contract v0.1.0** (`openapi.yaml` and the conformance fixtures of
  `unicom360-openapi` v0.1.0), and passes every one of its conformance vectors.
* Standard library only (`net/http`, `encoding/json`, `crypto/hmac`, `log/slog`). Go 1.22 or
  later.
* Context-first methods, functional options, one `*unicom360.Error` type with sentinels for
  `errors.Is`, and retries that cannot send a message twice.

API reference: [unicom360.com/docs](https://unicom360.com/docs). Go docs:
[pkg.go.dev/github.com/TechRode-Solution-Limited/unicom360-go](https://pkg.go.dev/github.com/TechRode-Solution-Limited/unicom360-go).

## Install

```sh
go get github.com/TechRode-Solution-Limited/unicom360-go
```

The module path is case-sensitive: `TechRode-Solution-Limited` with capital T, R, S and L.
A lower-case path will not resolve.

```go
import (
	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
	"github.com/TechRode-Solution-Limited/unicom360-go/gsm"     // GSM-7 helpers
	"github.com/TechRode-Solution-Limited/unicom360-go/webhook" // webhooks and callbacks
)
```

## Quick start

```go
client, err := unicom360.NewClient(unicom360.WithAPIKey(os.Getenv("UNICOM360_API_KEY")))
if err != nil {
	log.Fatal(err) // a configuration problem, such as a missing key
}

res, err := client.SMS.Send(ctx, unicom360.SMSSendParams{
	From:    "Unicom360",
	ToOne:   "260971234567",
	Message: "Your order ZM4821 is ready for collection.",
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(res.Results[0].MessageID, res.Results[0].Status)
```

`NewClient()` with no options reads the key from `UNICOM360_API_KEY` (and an optional base URL
from `UNICOM360_BASE_URL`). A `Client` is immutable and safe for concurrent use: create one and
share it. There is **no sandbox**: every accepted send is real and billed.

### Client options

| Option | Default | |
|---|---|---|
| `WithAPIKey(key)` | `UNICOM360_API_KEY` | Required. An explicit empty key is an error. |
| `WithBaseURL(url)` | `UNICOM360_BASE_URL`, else `https://api.unicom360.com` | https only (http allowed for `localhost`, `127.0.0.0/8`, `::1`). A path prefix is kept. |
| `WithTimeout(d)` | 60 s | Per attempt, until the whole body is read. |
| `WithConnectTimeout(d)` | 10 s | Connection and TLS (the SDK's own HTTP client only). |
| `WithMaxRetries(n)` | 2 | Retries after the first attempt. |
| `WithMaxRetryWait(d)` | 60 s | Longest single wait between attempts. |
| `WithAutoReferenceIDs(true)` | off | See [Retries](#retries). |
| `WithAppInfo(AppInfo{Name, Version, URL})` | none | Appended to the User-Agent. |
| `WithHTTPClient(hc)`, `WithTransport(rt)` | own client | Proxies, instrumentation, tests. Redirects are never followed. |
| `WithLogger(*slog.Logger)` | silent | See [Logging](#logging). |
| `WithClock`, `WithSleeper`, `WithRandom` | real | Test seams. |

Per call: `client.SMS.Send(ctx, params, unicom360.WithRequestTimeout(10*time.Second), unicom360.WithRequestMaxRetries(0))`.
`client.WithOptions(opts...)` returns a modified copy that shares the connection pool.

## Every operation

### SMS

```go
res, err := client.SMS.Send(ctx, unicom360.SMSSendParams{
	From:            "Unicom360",
	To:              []string{"260971234567", "260961234567"}, // or ToOne: "260971234567"
	Message:         "Your verification code is 482193.",
	UserReferenceID: "order-10421",                              // optional; makes retries safe
	CallbackURL:     "https://example.com/callbacks/sms",       // optional delivery reports
})
for _, r := range res.Accepted() {
	fmt.Println(r.MobileNumber, r.MessageID)
}
r := res.ByNumber("+260 96 123 4567") // results are not in request order past the 10th
```

An `Invalid mobile number` entry is a result, not an error: see `res.Invalid()`.

### Email

```go
res, err := client.Email.Send(ctx, unicom360.EmailSendParams{
	From:      "noreply@yourdomain.com",
	To:        []string{"jane@example.com", "john@example.com"},
	Subject:   "Your order is confirmed",
	EmailBody: "<p>Thanks for your order.</p>", // HTML is detected
	ReplyTo:   "support@yourdomain.com",
})
r := res.ByAddress("Jane@Example.com") // case and surrounding spaces are ignored
```

### WhatsApp template

```go
res, err := client.WhatsApp.SendTemplate(ctx, unicom360.TemplateSendParams{
	Template: "order_ready",
	Language: "en_US",
	Recipients: []unicom360.TemplateRecipient{{
		To:              "260971234567",
		UserReferenceID: "inv-42",
		Header:          unicom360.TemplateHeaderImage("https://example.com/receipts/ZM-4821.png"),
		Body:            unicom360.TemplateBodyOf("Ada", "ZM-4821"), // or TemplateBodyNamed(map[string]string{...})
		Buttons:         []unicom360.TemplateButton{unicom360.TemplateButtonURL(0, "ZM-4821")},
	}},
})
fmt.Println(res.AcceptedCount, res.RejectedCount)
for _, r := range res.Rejected() {
	fmt.Println(r.To, r.Reason) // for example INVALID_NUMBER
}
```

Headers: `TemplateHeaderText`, `TemplateHeaderImage`, `TemplateHeaderVideo`,
`TemplateHeaderDocument(url, filename)`. Buttons: `TemplateButtonURL`,
`TemplateButtonQuickReply`. An authentication template takes `OTP: "482193"`.

### WhatsApp session message

```go
msg := unicom360.TextMessage("260971234567", "Hi Ada, your order is ready.")
msg.ReplyTo = "wamid.HBgM..." // optional: quote a message
msg.UserReferenceID = "s-1"
res, err := client.WhatsApp.SendSession(ctx, msg)
if errors.Is(err, unicom360.ErrSessionWindowClosed) {
	// the customer has not written in 24 hours: send a template instead
}
```

Factories: `TextMessage`, `ImageMessage(to, url, caption)`, `VideoMessage`,
`DocumentMessage(to, url, caption, filename)`, `AudioMessage`, `StickerMessage`,
`ButtonsMessage(to, body, buttons, header, footer)`, `ListMessage(to, body, button, sections,
header, footer)`, `LocationMessage(to, lat, lng, name, address)`, `ContactsMessage(to,
contacts)`, `ReactionMessage(to, messageID, emoji)` (an empty emoji removes the reaction).
Optional arguments are `""` to leave them out; set `From`, `ReplyTo`, `UserReferenceID` and
`CallbackURL` on the returned value.

### WhatsApp status and templates

```go
st, err := client.WhatsApp.Status(ctx, []string{"7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f"})
if s := st.ByID("7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f"); s != nil {
	fmt.Println(s.Status, s.DeliveredAt) // NOT_FOUND for an unknown id
}

list, err := client.WhatsApp.Templates(ctx)
t := list.ByName("order_ready", "en_US") // "" matches any language
fmt.Println(t.Body.Text, t.Body.Variables)
```

### Balance and health

```go
b, err := client.Balance.Get(ctx, unicom360.BundleWhatsApp) // "" for the default (SMS)
fmt.Println(b.Balance)             // unicom360.Decimal, exactly as sent: "37.5"
units, _ := b.Balance.Float64()    // or Int64(), Number(), String()

all, err := client.Balance.All(ctx)
fmt.Println(all.Balances[unicom360.BundleSMS])

h, err := client.Health(ctx) // sends no API key
fmt.Println(h.Status)
```

Balances are `unicom360.Decimal`: a string type holding the JSON number's own text, so nothing
is lost to float rounding. Convert with `Float64()`, `Int64()`, `Number()` (a `json.Number`) or
pass `String()` to a decimal library.

### Results

Every result keeps the JSON it came from in `Raw`, so a field the gateway adds later is readable
before the SDK knows it. A string the API sends as null is `""`; booleans, numbers and objects
that can be null are pointers. Statuses, categories, reasons and codes are open sets: the SDK
publishes constants for the known values (`unicom360.StatusAcknowledged`, ...) and passes any
other string through. `message_id` is always a string.

## Errors

Every error is an `*unicom360.Error`. HTTP 200 is not success: the gateway refuses a send with
HTTP 200 and `success: false`, which the SDK returns as an error.

```go
_, err := client.SMS.Send(ctx, params)
var e *unicom360.Error
switch {
case errors.Is(err, unicom360.ErrInsufficientBundle):
	// top up
case errors.Is(err, unicom360.ErrRateLimit):
	// still over the per-minute limit after the retries; e.RetryAfter says when it resets
case errors.As(err, &e):
	log.Printf("%s %s: %s (api_reference %s)", e.Class(), e.Code, e.Message, e.APIReference)
}
```

| Class (`e.Class()`) | Sentinel | When |
|---|---|---|
| `ConfigurationError` | `ErrConfiguration` | `NewClient` options, or `To` and `ToOne` both set |
| `APIConnectionError` | `ErrConnection` | DNS, refused, reset, malformed response |
| `APITimeoutError` | `ErrTimeout` (and `ErrConnection`) | an attempt timed out |
| `APIStatusError` | `ErrStatus` | any other non-2xx (3xx, 403, 404, ...), or a 2xx without a usable envelope |
| `AuthenticationError` | `ErrAuthentication` (and `ErrStatus`) | 401: `AUTHENTICATION_FAILED`, `IP_NOT_ALLOWED`, ... |
| `RateLimitError` | `ErrRateLimit` (and `ErrStatus`) | 429; `RetryAfter` is the time to the next UTC minute |
| `InvalidRequestError` | `ErrInvalidRequest` (and `ErrStatus`) | 400: the request's shape is wrong |
| `APIServerError` | `ErrServer` (and `ErrStatus`) | 5xx |
| `APIError` | `ErrAPI` | 200 with `success: false`, for example `INVALID_SENDER_ID` |
| `InsufficientBundleError` | `ErrInsufficientBundle` (and `ErrAPI`) | `INSUFFICIENT_BUNDLE` |
| `DuplicateReferenceError` | `ErrDuplicateReference` (and `ErrAPI`) | the `user_reference_id` was used already |
| `NoValidRecipientsError` | `ErrNoValidRecipients` (and `ErrAPI`) | template send with no valid recipient; `e.Results` has each reason |
| `SessionWindowClosedError` | `ErrSessionWindowClosed` (and `ErrAPI`) | no customer message in the last 24 hours |
| `SignatureVerificationError` | `ErrSignatureVerification` | a webhook failed verification; `e.Reason` says why |

`e.Code` is the normalised code (`unicom360.NormalizeCode`): sentences such as
`Invalid From Sender ID` become `INVALID_SENDER_ID`, `duplicate user reference id` becomes
`DUPLICATE_REFERENCE`, and request-shape sentences are `UNKNOWN`. The codes are published as
constants (`unicom360.CodeInvalidSenderID`, ...). `e.Attempts` counts the HTTP attempts,
retries included. Errors never contain the API key.

A `DuplicateReferenceError` with `e.Attempts > 1` means an earlier attempt of the same call
very probably went through and the retry was refused because its reference had been used:
treat the message as sent.

## Retries

The client retries up to `WithMaxRetries` times (2 by default), and only when a retry cannot
send a message twice:

| Outcome of an attempt | Retried |
|---|---|
| HTTP 429 | always (it is rejected before any processing) |
| failure before the request was sent (DNS, connection refused, TLS) | always |
| timeout or dropped connection after sending, HTTP 5xx, `SEND_FAILED` | reads always; SMS and WhatsApp sends only with a `user_reference_id` (on every template recipient); email never |
| 400, 401, other 4xx, any other refusal | never |

The gateway refuses a `user_reference_id` it has already used for the same recipient and
sender (under a unique index for SMS and WhatsApp), which is what makes those retries safe.
Email's check has no unique index, so email is retried only after a 429 or a failure to
connect, even with a reference.

A 429 waits until the next UTC minute (the gateway's fixed rate-limit window, sent without a
`Retry-After` header) plus up to a second of jitter; if that is longer than `WithMaxRetryWait`
the error is returned at once. Other retries use full-jitter exponential backoff capped at 8
seconds.

`WithAutoReferenceIDs(true)` gives every send without a reference a generated one
(`u360-` and 32 hex characters, reused on each retry; each template recipient gets its own), so
SMS and WhatsApp sends become retry-safe. Your own references are never replaced. The generated
values appear in the request, in template and session results, and in the debug log line.

## Webhooks

WhatsApp events (`message.status`, `message.received`) and the portal's `webhook.test` are signed.
`webhook.Handler` verifies and parses them behind an `http.Handler`:

```go
http.Handle("POST /webhooks/unicom360", webhook.Handler(os.Getenv("UNICOM360_WEBHOOK_SECRET"),
	func(ctx context.Context, ev webhook.Event) error {
		// Deliveries are retried on any non-2xx: drop repeats by delivery id.
		if alreadyHandled(ev.Meta().DeliveryID) {
			return nil
		}
		switch e := ev.(type) {
		case *webhook.MessageStatusEvent:
			return markStatus(ctx, e.Data.MessageID, e.Data.Status)
		case *webhook.MessageReceivedEvent:
			return storeInbound(ctx, e.Data.From, e.Data.Text)
		case *webhook.TestEvent:
			log.Println("test delivery:", e.Data.Message)
		}
		return nil // 200; an error answers 500 and the delivery is retried
	}))
```

The handler reads at most 1 MiB and answers 401 to anything forged or more than 5 minutes old.
Outside `net/http`, pass the raw body to `webhook.ConstructEvent(body, headers, secret)` or
`webhook.Verify(body, headers, secret)` yourself, before any JSON parsing. The key is the
secret's own bytes, never base64-decoded. `webhook.WithTolerance(d)` changes the allowed skew.

Facts worth knowing: a media caption arrives in `Data.Text`, not `Data.Media`; `Data.Payload`
is set only for location, contacts, reaction, sticker and unsupported messages; a reply-button
or list tap has the chosen title in `Data.Text` and `Data.ReplyTo` set to your message's
`wamid.`, and the button or row id is not passed on.

A complete server is in [`examples/webhook-server`](examples/webhook-server/main.go).

## SMS and email delivery reports

The reports posted to a send's `callback_url` are **not signed and never retried**: anyone who
knows the URL can post one, and a lost report is not sent again. Treat them as hints.

```go
sms, err := webhook.ParseSMSCallback(body)     // MessageID, Status, MobileNumber
email, err := webhook.ParseEmailCallback(body) // MessageID (a string), Status, EmailAddress, Message
```

## GSM-7 helpers

The gateway accepts only GSM-7 text (the basic set plus `^{}\[~]|€`, no form feed), at most
1,200 characters, and bills `max(1, ceil(length / 160))` units per recipient. The SDK never
checks a message itself; the `gsm` package is there when you want to:

```go
gsm.IsGSM7("Price: 5€")            // true
gsm.Length("Hi\r\nthere")          // 8: CR LF counts as one
gsm.Units(strings.Repeat("a", 161)) // 2
gsm.NonGSMCharacters("It’s done")  // [{Char: '’', Index: 2}]
gsm.ValidateSMS("")                // [empty]; also too_long and non_gsm
```

## Logging

```go
client, _ := unicom360.NewClient(unicom360.WithLogger(slog.Default()))
```

One debug line per attempt (`method`, `path`, `status` or the failure type, `api_reference`,
`attempt`, `duration_ms`) and one info line per scheduled retry (`attempt`, `wait_s`, `cause`).
Request and response bodies, headers, parameters, phone numbers and addresses are never logged.
The API key is redacted everywhere: `fmt.Print(client)`, `%#v` and `slog` show
`api_key: [REDACTED]`.

## Known differences from the published API docs

The SDK follows what the gateway does. Where that differs from
[unicom360.com/docs](https://unicom360.com/docs):

* **Validation messages can be lists.** An SMS or email request-shape error sends `message` as
  an array of sentences; the SDK joins them with `"; "` into `e.Message`.
* **WhatsApp balances are whole numbers today.** The docs say a WhatsApp balance can be
  fractional; the gateway currently rounds every balance. `Decimal` handles both.
* **Local numbers.** Only `095`, `096`, `097` and `076` local prefixes are normalised; any
  other local form comes back as `Invalid mobile number`. Send international numbers.
* **SMS and email `callback_url` are not validated.** Use https anyway.
* **Session text must not be blank**, and a WhatsApp `callback_url` is limited to 300 bytes,
  not characters.
* **A session `DUPLICATE_MSG_REF` can lack the original id** (`e.OriginalMessageID` is `""`) when
  two requests race for the same reference.
* **Rejected template recipients echo what you sent**, which can be a number; the SDK gives
  `To` and `UserReferenceID` as strings.
* **Balance refusals.** The balance endpoints can answer `CLIENT_NOT_FOUND` (HTTP 200,
  `status: false`, no `api_reference`): an `APIError` with an empty `APIReference`.
* **Email references do not protect concurrent duplicates**, so email is never retried after
  a timeout or a 5xx (see [Retries](#retries)).
* **Health timestamps have microseconds**, not milliseconds.

## Versioning

This module follows semantic versioning from v0.1.0 and will move to v1.0.0 once live use shows
no differences from the contract. `unicom360.Version` is the SDK version and
`unicom360.ContractVersion` the API contract it implements.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) (tests, the conformance fixtures, releases) and
[SECURITY.md](SECURITY.md) (report vulnerabilities privately).

## Licence

MIT, see [LICENSE](LICENSE). Copyright (c) 2026 Techrode Solutions Limited.
