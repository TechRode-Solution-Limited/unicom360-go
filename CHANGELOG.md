# Changelog

All notable changes to this module are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-11-04

First release, targeting the Unicom360 API contract v0.1.0.

### Added

- `unicom360.NewClient` with functional options: API key and base URL (or
  `UNICOM360_API_KEY` and `UNICOM360_BASE_URL`), timeouts, retries, `WithAutoReferenceIDs`,
  `WithAppInfo`, `WithHTTPClient`, `WithTransport`, `WithLogger` (`*slog.Logger`) and test
  seams for the clock, sleeper and jitter. `Client.WithOptions` returns a modified copy.
- Every operation of the public API, context first: `SMS.Send`, `Email.Send`,
  `WhatsApp.SendTemplate`, `WhatsApp.SendSession`, `WhatsApp.Status`, `WhatsApp.Templates`,
  `Balance.Get`, `Balance.All` and `Health`, with per-call `WithRequestTimeout` and
  `WithRequestMaxRetries`.
- Session message factories (`TextMessage`, `ImageMessage`, `ButtonsMessage`, `ListMessage`,
  `LocationMessage`, `ContactsMessage`, `ReactionMessage`, ...) and template header, body and
  button factories.
- Typed results that keep the raw JSON, with helpers (`Accepted`, `Invalid`, `Rejected`,
  `ByNumber`, `ByAddress`, `ByID`, `ByName`) and an exact `Decimal` for balances.
- One `*unicom360.Error` type with `Class()`, normalised codes (`NormalizeCode`) and sentinels
  for `errors.Is`.
- Retries that are safe without idempotency keys: 429 waits for the next UTC minute, failures
  before sending are always retried, and sends are retried after a timeout or a 5xx only when
  they carry a `user_reference_id` (email never).
- Package `webhook`: `Verify`, `ConstructEvent`, `Handler` (an `http.Handler`), `Sign`, and
  `ParseSMSCallback` / `ParseEmailCallback` for the unsigned delivery reports. An empty or
  whitespace-only secret is refused, because anyone can forge a signature with an empty key:
  `Verify` and `ConstructEvent` return `ErrConfiguration` before any other check, and `Handler`
  panics when it is built.
- Package `gsm`: `IsGSM7`, `Length`, `Units`, `NonGSMCharacters`, `ValidateSMS`.
- The conformance suite of `unicom360-openapi` v0.1.0, run on every vector.
