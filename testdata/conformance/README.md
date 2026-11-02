# Conformance fixtures

Language-neutral test vectors every Unicom360 SDK runs in its own test runner. They encode the
rules in [`docs/sdk-design.md`](../docs/sdk-design.md); section numbers below refer to it.

| File | Vectors of | Schema |
|---|---|---|
| `requests.json` | an SDK call and the exact HTTP request it must make | `schema/requests.schema.json` |
| `responses.json` | a canned HTTP response and the result or error it must produce | `schema/responses.schema.json` |
| `retries.json` | a sequence of responses and transport failures, and the attempts, sleeps and outcome | `schema/retries.schema.json` |
| `error-codes.json` | a raw server message and its normalised code | `schema/error-codes.schema.json` |
| `callbacks.json` | an unsigned SMS or email delivery report and its parsed form | `schema/callbacks.schema.json` |
| `gsm7.json` | a text and every GSM helper's answer | `schema/gsm7.schema.json` |
| `webhooks/signatures.json` | a signed (or tampered) delivery and the verify outcome | `schema/signatures.schema.json` |
| `webhooks/events.json` | a signed delivery and the event it parses to | `schema/events.schema.json` |

`VERSION` holds the fixture version (in this repo, `0.1.0`; in an SDK's vendored copy, the
`VERSION` that `scripts/sync-conformance` wrote: see the end of this file).

Every file is a JSON object with `version`, `description` and `vectors` (an array). Every vector
has a unique `id` (lower-case, digits and hyphens) and usually a `description`. Harnesses MUST
run every vector, report failures by `id`, and fail when a file has a vector they do not
understand rather than skipping it.

## Operations

Vectors name operations canonically. The harness maps each to the SDK's method (section 5.1)
and each `params` object (API field names, snake_case) to the SDK's arguments:

`sms.send`, `email.send`, `whatsapp.send_template`, `whatsapp.send_session`, `whatsapp.status`,
`whatsapp.templates`, `balance.get`, `balance.all`, `health`.

For `whatsapp.send_session`, build the message with the factory for `params.type` (section 5.3)
and set the common fields (`from`, `reply_to`, `user_reference_id`, `callback_url`) from
`params`. For `whatsapp.send_template`, build headers, bodies and buttons with the
`TemplateHeader`, `TemplateBody` and `TemplateButton` factories: a string `header` is
`TemplateHeader.text`, an object is the media factory for its `type`; a list `body` is
`TemplateBody.of`, an object is `TemplateBody.named`.

## Canonical form and comparison

Results, events and callback reports are compared in **canonical form**: a JSON value built
from the SDK's typed model (not from its `raw` copy), using the API's snake_case field names,
plus every unknown field the model kept. Per operation:

| Operation | Canonical result |
|---|---|
| `sms.send`, `email.send` | `{"api_reference", "message", "results": [...]}` |
| `whatsapp.send_template` | `{"api_reference", "message", "accepted", "rejected", "results": [...]}` (the model's `accepted_count`/`rejected_count` under their API names) |
| `whatsapp.send_session` | `{"api_reference", "message", "message_id", "to", "user_reference_id", "status", "category"}` |
| `whatsapp.status` | `{"api_reference", "message", "results": [...]}` |
| `whatsapp.templates` | `{"api_reference", "message", "templates": [...]}` |
| `balance.get` | `{"message", "balance", "bundle_type", "client_id", "client_name"}` |
| `balance.all` | `{"message", "balances": {...}, "client_id", "client_name"}` |
| `health` | `{"status", "timestamp", "version", "environment"}` |

That is: the envelope without `success` (or the balance `status` flag), with `data` flattened in,
or carried as `results` when it is a list. Unknown fields appear at the level they were found.
`message_id`, and a template result's `to` and `user_reference_id`, are strings.

Comparison of canonical JSON (`expected` against `actual`):

1. Remove every object member whose value is `null`, recursively, on both sides (absent and
   null are the same).
2. Objects: the same set of keys, each value equal. Key order is irrelevant.
3. Arrays: the same length and order.
4. Numbers: equal by numeric value (`37.5` equals a decimal `37.50`; `1250` equals `1250.0`).
5. Strings and booleans: exactly equal.

`scripts/lib/reference.mjs` (`canonicalEqual`) is the reference implementation.

## requests.json

```json
{
  "id": "sms-to-string-minimal",
  "operation": "sms.send",
  "params": { "from": "Unicom360", "to": "260971234567", "message": "..." },
  "expect": {
    "method": "POST",
    "path": "/api/v1/json/bulk/sms",
    "headers": { "accept": "application/json", "x-api-key": "<KEY>", "content-type": "application/json" },
    "absent_headers": ["authorization"],
    "user_agent_prefix": "unicom360-",
    "json": { "from": "Unicom360", "to": "260971234567", "message": "..." },
    "absent_keys": ["/user_reference_id", "/callback_url", "/source"]
  }
}
```

The file also has `canned_responses`: one `{status, headers, body_text}` per operation, a valid
success the fake transport can answer with so the call completes.

Harness, per vector:

1. Build a client with API key `test-api-key` (any non-empty key works; `<KEY>` stands for it),
   the default base URL, `max_retries` 0, and a fake transport that records the request and
   answers `canned_responses[operation]`.
2. Call the operation with `params`.
3. Assert, on the single recorded request: the method; the URL path equals `expect.path` (the
   base URL's own path prefix, if any, removed) with no query string; every `expect.headers`
   entry is present with exactly that value (names case-insensitive, `<KEY>` replaced); every
   `absent_headers` name is missing; `User-Agent` starts with `user_agent_prefix`.
4. Body: when `expect.json` is null, there is no body (or an empty one). Otherwise the body
   parses as JSON and is **deeply equal** to `expect.json` (no extra keys, no `null` for unset
   fields, numbers by value, strings exact), and no JSON Pointer in `absent_keys` exists.

## responses.json

```json
{
  "id": "sms-insufficient-bundle-docs-example",
  "operation": "sms.send",
  "now_unix": 1767225630,
  "http": { "status": 200, "headers": { "content-type": "application/json; charset=utf-8" }, "body_text": "{...}" },
  "expect": { "error": { "type": "api_error", "class": "InsufficientBundleError", "code": "INSUFFICIENT_BUNDLE",
                         "message": "INSUFFICIENT_BUNDLE", "api_reference": "1764184470578998353",
                         "http_status": 200, "data_present": false } }
}
```

`expect` is either `{"result": <canonical result>, "helpers"?: {...}}` or `{"error": {...}}`.
`off_spec: true` marks a body that deliberately breaks `openapi.yaml` (robustness cases: a
numeric `message_id`, a 2xx without an envelope); treat it like any other vector.

Harness, per vector:

1. Build a client with `max_retries` 0 (so a 5xx is not retried), a clock fixed at `now_unix`
   when present, and a fake transport answering `http`. Call the operation with any valid
   parameters (for example the `params` of a `requests.json` vector for that operation).
2. `result`: the call succeeds and the canonical form of the result equals `expect.result`.
3. `helpers` (send operations only): each list is the 0-based positions, in `results`, that
   `accepted()`, `invalid()` or `rejected()` return; each map gives the argument passed to
   `by_number()` or `by_address()` and the position it must return (`null` for no match).
4. `error`: the call fails, and:
   * `type` is the error's type string (8.1); `class` its most specific canonical class (map
     `Exception`/`Api` names back; Go `Class()`; Elixir `Unicom360.Error.class/1`);
   * `code`, `api_reference`, `http_status` equal;
   * `message` equals, unless `expect.error.message` is null, in which case the SDK's message is
     any non-empty string;
   * `data_present` is whether the error's `data` is non-null;
   * `retry_after`, when given, equals the `RateLimitError`'s `retry_after` in seconds.

## retries.json

```json
{
  "id": "429-then-success",
  "operation": "sms.send",
  "params": { ... },
  "now_unix": 1767225630,
  "config": { "max_retries": 2, "max_retry_wait": 60 },
  "steps": [ { "http": { "status": 429, ... } }, { "http": { "status": 200, ... } } ],
  "expect": { "attempts": 2, "sleeps": [ { "min": 30, "max": 31 } ], "outcome": { "kind": "result" } }
}
```

A step is either `{"http": {...}}` or `{"transport": kind}`:

| `transport` | Harness raises (3.4) |
|---|---|
| `connect_refused` | failure with `sent: false`, `timeout: false` |
| `dns_failure` | failure with `sent: false`, `timeout: false` |
| `timeout_after_send` | failure with `sent: true`, `timeout: true` |
| `reset_after_send` | failure with `sent: true`, `timeout: false` |

Harness, per vector:

1. Build a client with `config`'s `max_retries` and `max_retry_wait`, a fake clock starting at
   `now_unix` that moves **only** when the fake sleeper sleeps (by exactly the seconds slept),
   a fake sleeper that records each wait in seconds, and a fake transport that answers attempt
   *n* with `steps[n-1]`. The random source is the SDK's own (or any seam value in [0, 1)).
2. Call the operation with `params`.
3. Assert: the transport was called exactly `attempts` times; the recorded sleeps have the same
   count as `expect.sleeps` and each lies within its `[min, max]` (inclusive, 1e-6 tolerance);
   the outcome is a result (`"kind": "result"`) or an error with that `type`, `class`, `code`
   (null for transport errors) and `http_status` (null for transport errors), whose `attempts`
   field equals `expect.attempts`.

## error-codes.json

`{"id", "message", "code", "source", "note"?}`: the SDK's normalisation function (section 9),
called directly with `message`, returns `code`. `source` says where the gateway produces the
message and is documentation only.

## callbacks.json

`{"id", "kind": "sms" | "email", "body_text", "expect"}`. Call `parse_sms_callback` or
`parse_email_callback` with `body_text`. `expect.result` is the canonical report
(`message_id` a string, unknown fields kept); `expect.error: true` means the call raises
`Unicom360Error` (any class).

## gsm7.json

The file carries `charset` (the gateway's exact set, to embed verbatim), `max_length` (1200)
and `unit_length` (160). Each vector:
`{"id", "text", "is_gsm7", "length", "units", "non_gsm": [{"char", "index"}], "validate": [...]}`.
Assert each helper (section 12) returns exactly these values: `index` is a 0-based code point
index; `validate` lists the issues in order (`empty`, `too_long`, `non_gsm`). The vectors were
cross-checked against the gateway's own `SmsPartCounter.detect_encoding/1`, `String.length/1`
and `SmsLogs.msg_count/1`.

## webhooks/signatures.json

`{"id", "description", "secret", "headers", "body_base64", "body_text", "now_unix",
"tolerance", "expect": {"valid": true} | {"valid": false, "reason"}}`.

Call `verify(raw_body, headers, secret, tolerance, now = now_unix)` with the bytes of
`body_base64` (`body_text` is the same bytes as UTF-8, for SDKs whose verify takes a string) and
`headers` exactly as given (their name case varies on purpose). Valid: no error. Invalid:
`SignatureVerificationError` with that `reason`.

Generated by `scripts/gen-signatures`; the vectors were cross-checked with Elixir's
`:crypto.mac(:hmac, :sha256, secret, "<ts>.<body>")`, the gateway signer's exact code.

## webhooks/events.json

`{"id", "secret", "headers", "body_text", "now_unix", "tolerance", "expect"}` where `expect` is
`{"kind", "event", "delivery_id", "created_at", "data"}` and `kind` is `message_status`,
`message_received`, `test` or `unknown`.

Call `construct_event(body_text bytes, headers, secret, tolerance, now = now_unix)`. Assert the
event's model is the one for `kind` (`MessageStatusEvent`, `MessageReceivedEvent`, `TestEvent`,
`UnknownEvent`), and that `event`, `delivery_id`, `created_at` and the canonical form of `data`
equal `expect` (canonical comparison above).

## Vendoring into an SDK

From this repository:

```sh
scripts/sync-conformance ../unicom360-node/test/conformance
```

copies every file here into the target directory (replacing a previous copy) and writes the
target's `VERSION`:

```
source=unicom360-openapi
version=0.1.0
tag=v0.1.0
commit=<git commit of this repository, or unknown>
sha256=<tree hash>
```

`tag` is the exact git tag at `HEAD`, or `untagged`. The tree hash is the SHA-256 of a listing
in `sha256sum` format: for every file except `VERSION`, sorted by relative path (byte order,
`/` separators), one line made of the file's lowercase hex SHA-256, two spaces, the path and a
newline. An SDK's CI can recompute it to prove its copy is unmodified;
`scripts/sync-conformance --check <dir>` does exactly that.
