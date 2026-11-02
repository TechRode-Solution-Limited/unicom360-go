package unicom360

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"
)

// maxResponseBytes bounds how much of a response body is read.
const maxResponseBytes = 32 << 20

// operation describes one API call.
type operation struct {
	method    string
	path      string
	auth      bool // send X-API-Key
	balance   bool // the envelope uses status, not success
	health    bool // the body is the result
	body      any  // nil: no body
	retrySafe bool
	// generatedRefs are the user_reference_id values auto_reference_ids generated, for the
	// debug log.
	generatedRefs []string
}

type attemptResult struct {
	body        []byte
	err         *Error
	retryable   bool
	rateLimited bool
	// untilNextMinute is the 429 wait base in seconds, from the clock when the 429 arrived.
	untilNextMinute float64
}

// do runs op, with retries, and returns the body of a successful response or an *Error.
func (k *core) do(ctx context.Context, op *operation, opts []RequestOption) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rc := requestConfig{timeout: k.cfg.timeout, maxRetries: k.cfg.maxRetries}
	for _, o := range opts {
		if o == nil {
			continue
		}
		if err := o(&rc); err != nil {
			return nil, err
		}
	}
	var payload []byte
	if op.body != nil {
		var err error
		if payload, err = marshalJSON(op.body); err != nil {
			return nil, &Error{Type: TypeUnicom360Error, Message: "cannot encode the request: " + err.Error(), Err: err}
		}
	}
	target := k.cfg.baseURL + op.path
	maxWait := k.cfg.maxRetryWait.Seconds()

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			e, _ := k.transportFailure(ctx, ctx, err, false, false)
			e.Attempts = attempt - 1
			return nil, e
		}
		out := k.attempt(ctx, op, target, payload, rc.timeout, attempt)
		if out.err == nil {
			return out.body, nil
		}
		e := out.err
		e.Attempts = attempt
		if !out.retryable || attempt > rc.maxRetries || ctx.Err() != nil {
			return nil, e
		}
		var wait float64
		if out.rateLimited {
			if out.untilNextMinute > maxWait {
				return nil, e
			}
			wait = math.Max(1, out.untilNextMinute+k.cfg.random())
		} else {
			wait = k.cfg.random() * math.Min(8, math.Min(0.5*math.Pow(2, float64(attempt)), maxWait))
		}
		if k.cfg.logger != nil {
			k.cfg.logger.LogAttrs(ctx, slog.LevelInfo, "unicom360 retry scheduled",
				slog.Int("attempt", attempt),
				slog.Float64("wait_s", wait),
				slog.String("cause", retryCause(e)),
			)
		}
		if err := k.cfg.sleeper(ctx, time.Duration(wait*float64(time.Second))); err != nil {
			if e.Err == nil {
				e.Err = err
			} else {
				e.Err = errors.Join(e.Err, err)
			}
			return nil, e
		}
	}
}

func retryCause(e *Error) string {
	if e.HTTPStatus != 0 {
		if e.Type == TypeAPIError {
			return fmt.Sprintf("%s %s (HTTP %d)", e.Type, e.Code, e.HTTPStatus)
		}
		return fmt.Sprintf("%s (HTTP %d)", e.Type, e.HTTPStatus)
	}
	return e.Type
}

// attempt makes one HTTP request and interprets its outcome.
func (k *core) attempt(ctx context.Context, op *operation, target string, payload []byte, timeout time.Duration, n int) attemptResult {
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var connected atomic.Bool
	actx = httptrace.WithClientTrace(actx, &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	})

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(actx, op.method, target, body)
	if err != nil {
		return attemptResult{err: &Error{Type: TypeUnicom360Error, Message: "cannot build the request: " + err.Error(), Err: err}}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", k.userAgent)
	if op.auth {
		req.Header.Set("X-API-Key", k.cfg.apiKey)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := k.http.Do(req)
	var respBody []byte
	if err == nil {
		respBody, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		_ = resp.Body.Close()
		if err == nil && len(respBody) > maxResponseBytes {
			err = fmt.Errorf("response body larger than %d bytes", maxResponseBytes)
		}
	}
	elapsed := time.Since(start)

	if err != nil {
		e, retryable := k.transportFailure(ctx, actx, err, connected.Load() || resp != nil, op.retrySafe)
		k.logAttempt(ctx, op, n, elapsed, slog.String("error", e.Type))
		return attemptResult{err: e, retryable: retryable}
	}

	out := interpret(op, resp.StatusCode, resp.Header, respBody, k.cfg.clock())
	ref := ""
	if out.err != nil {
		ref = out.err.APIReference
	} else if !op.balance && !op.health {
		ref = string(decodeEnvelope(respBody).APIReference)
	}
	k.logAttempt(ctx, op, n, elapsed, slog.Int("status", resp.StatusCode), slog.String("api_reference", ref))
	return out
}

func (k *core) logAttempt(ctx context.Context, op *operation, n int, elapsed time.Duration, attrs ...slog.Attr) {
	if k.cfg.logger == nil {
		return
	}
	all := []slog.Attr{slog.String("method", op.method), slog.String("path", op.path)}
	all = append(all, attrs...)
	all = append(all, slog.Int("attempt", n), slog.Int64("duration_ms", elapsed.Milliseconds()))
	if len(op.generatedRefs) > 0 {
		all = append(all, slog.Any("generated_user_reference_ids", op.generatedRefs))
	}
	k.cfg.logger.LogAttrs(ctx, slog.LevelDebug, "unicom360 request", all...)
}

// transportFailure turns a failed attempt into an *Error and says whether it may be retried.
func (k *core) transportFailure(parent, actx context.Context, err error, connected, retrySafe bool) (*Error, bool) {
	if perr := parent.Err(); perr != nil {
		typ := TypeConnectionError
		if errors.Is(perr, context.DeadlineExceeded) {
			typ = TypeTimeoutError
		}
		return &Error{Type: typ, Message: "request cancelled: " + perr.Error(), Err: perr}, false
	}
	sent, timeout := classifyTransportError(err, connected, k.traceable)
	if errors.Is(actx.Err(), context.DeadlineExceeded) {
		timeout = true
	}
	e := &Error{Type: TypeConnectionError, Message: "connection failed: " + err.Error(), Err: err}
	if timeout {
		e.Type, e.Message = TypeTimeoutError, "request timed out: "+err.Error()
	}
	return e, !sent || retrySafe
}

// interpret applies the response rules: HTTP 200 is not success, the envelope's success (or
// status, on balances) decides, and every non-2xx maps to a class by status.
func interpret(op *operation, status int, header http.Header, body []byte, now time.Time) attemptResult {
	var obj map[string]json.RawMessage
	isObject := isJSONObject(body) && json.Unmarshal(body, &obj) == nil && obj != nil

	if status >= 200 && status <= 299 {
		if !isObject {
			return attemptResult{err: unusable(status, header, body, "is not a JSON object")}
		}
		if op.health {
			return attemptResult{body: body}
		}
		flagName := "success"
		if op.balance {
			flagName = "status"
		}
		switch string(bytes.TrimSpace(obj[flagName])) {
		case "true":
			return attemptResult{body: body}
		case "false":
			e := apiError(status, header, body, obj)
			return attemptResult{err: e, retryable: e.Code == CodeSendFailed && op.retrySafe}
		default:
			return attemptResult{err: unusable(status, header, body, "has no boolean "+flagName)}
		}
	}

	e := &Error{Type: statusType(status), Code: CodeUnknown, HTTPStatus: status, RawBody: string(body), Headers: header}
	messageGiven := false
	if isObject {
		if raw, ok := obj["message"]; ok {
			if text, ok := messageText(raw); ok {
				e.Message, e.Code, messageGiven = text, NormalizeCode(text), true
			}
			e.APIReference = jsonString(obj["api_reference"])
			e.Data = rawOrNil(obj["data"])
		} else if detail, ok := phoenixDetail(obj); ok {
			e.Message, messageGiven = detail, true
		}
	}
	if !messageGiven {
		e.Message = strings.TrimSpace(fmt.Sprintf("HTTP %d %s", status, http.StatusText(status)))
	}
	out := attemptResult{err: e}
	switch {
	case status == http.StatusTooManyRequests:
		out.untilNextMinute = secondsToNextMinute(now)
		e.RetryAfter = time.Duration(out.untilNextMinute * float64(time.Second))
		out.retryable, out.rateLimited = true, true
	case status >= 500 && status <= 599:
		out.retryable = op.retrySafe
	}
	return out
}

func statusType(status int) string {
	switch {
	case status == http.StatusBadRequest:
		return TypeInvalidRequestError
	case status == http.StatusUnauthorized:
		return TypeAuthenticationError
	case status == http.StatusTooManyRequests:
		return TypeRateLimitError
	case status >= 500 && status <= 599:
		return TypeAPIServerError
	}
	return TypeAPIStatusError
}

// unusable is the error for a 2xx response without a usable envelope.
func unusable(status int, header http.Header, body []byte, why string) *Error {
	return &Error{
		Type: TypeAPIStatusError, Code: CodeUnknown, HTTPStatus: status,
		Message: fmt.Sprintf("HTTP %d response %s", status, why),
		RawBody: string(body), Headers: header,
	}
}

// apiError is the error for a 2xx refusal (success: false, or status: false on balances).
func apiError(status int, header http.Header, body []byte, obj map[string]json.RawMessage) *Error {
	e := &Error{Type: TypeAPIError, Code: CodeUnknown, HTTPStatus: status, RawBody: string(body), Headers: header}
	if text, ok := messageText(obj["message"]); ok {
		e.Message, e.Code = text, NormalizeCode(text)
	} else {
		e.Message = fmt.Sprintf("HTTP %d refusal without a message", status)
	}
	e.APIReference = jsonString(obj["api_reference"])
	e.Data = rawOrNil(obj["data"])
	switch e.Code {
	case CodeDuplicateReference:
		var d struct {
			MessageID json.RawMessage `json:"message_id"`
		}
		if decodeObject(e.Data, &d) {
			e.OriginalMessageID = jsonString(d.MessageID)
		}
	case CodeNoValidRecipients:
		var d templateSendData
		if decodeObject(e.Data, &d) {
			e.Results = d.Results
		}
	}
	return e
}

func phoenixDetail(obj map[string]json.RawMessage) (string, bool) {
	var errs struct {
		Detail json.RawMessage `json:"detail"`
	}
	if !decodeObject(obj["errors"], &errs) || firstByte(errs.Detail) != '"' {
		return "", false
	}
	return jsonString(errs.Detail), true
}

// secondsToNextMinute is 60 - (now mod 60): the wait until the gateway's fixed UTC-minute
// rate-limit window resets, in (0, 60].
func secondsToNextMinute(now time.Time) float64 {
	sec := now.Unix() % 60
	if sec < 0 {
		sec += 60
	}
	return 60 - (float64(sec) + float64(now.Nanosecond())/1e9)
}
