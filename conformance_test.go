package unicom360_test

// The conformance suite: every vector of every vendored fixture file, run through the public
// API. See testdata/conformance/README.md for the formats.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
	"github.com/TechRode-Solution-Limited/unicom360-go/gsm"
	"github.com/TechRode-Solution-Limited/unicom360-go/webhook"
)

const testAPIKey = "test-api-key"

// loadVectors reads a fixture file, checks its envelope and returns its vectors.
func loadVectors(t *testing.T, name string, extra any) []json.RawMessage {
	t.Helper()
	b := readFixture(t, name)
	var f fixtureFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if f.Version != unicom360.ContractVersion {
		t.Fatalf("%s: version %q, want %q", name, f.Version, unicom360.ContractVersion)
	}
	if len(f.Vectors) == 0 {
		t.Fatalf("%s: no vectors", name)
	}
	// The file's top level must hold only what the harness knows.
	var top map[string]json.RawMessage
	_ = json.Unmarshal(b, &top)
	known := map[string]bool{"version": true, "description": true, "vectors": true}
	if extra != nil {
		if err := json.Unmarshal(b, extra); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var ek map[string]json.RawMessage
		eb, _ := json.Marshal(extra)
		_ = json.Unmarshal(eb, &ek)
		for k := range ek {
			known[k] = true
		}
	}
	for k := range top {
		if !known[k] {
			t.Fatalf("%s: unknown top-level key %q", name, k)
		}
	}
	return f.Vectors
}

// runVectors runs fn for every vector as a subtest named by its id, and checks ids are unique.
func runVectors[V any](t *testing.T, name string, vectors []json.RawMessage, id func(V) string, fn func(*testing.T, V)) {
	t.Helper()
	seen := map[string]bool{}
	ran := 0
	for i, raw := range vectors {
		var v V
		if err := strictDecode(raw, &v); err != nil {
			t.Fatalf("%s: vector %d is not understood by the harness: %v", name, i, err)
		}
		vid := id(v)
		if vid == "" || seen[vid] {
			t.Fatalf("%s: vector %d has a missing or repeated id %q", name, i, vid)
		}
		seen[vid] = true
		t.Run(vid, func(t *testing.T) { fn(t, v) })
		ran++
	}
	t.Logf("%s: %d vectors", name, ran)
}

func newTestClient(t *testing.T, ft *fakeTransport, opts ...unicom360.Option) *unicom360.Client {
	t.Helper()
	all := append([]unicom360.Option{
		unicom360.WithAPIKey(testAPIKey),
		unicom360.WithBaseURL(unicom360.DefaultBaseURL),
		unicom360.WithTransport(ft),
	}, opts...)
	c, err := unicom360.NewClient(all...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func unixTime(n json.Number) time.Time {
	f, err := n.Float64()
	if err != nil {
		panic(err)
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(math.Round(frac*1e9)))
}

func fixedClock(n json.Number) func() time.Time {
	at := unixTime(n)
	return func() time.Time { return at }
}

// ---------------------------------------------------------------------------------------
// requests.json
// ---------------------------------------------------------------------------------------

type requestVector struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Operation   string          `json:"operation"`
	Params      json.RawMessage `json:"params"`
	Expect      struct {
		Method          string            `json:"method"`
		Path            string            `json:"path"`
		Headers         map[string]string `json:"headers"`
		AbsentHeaders   []string          `json:"absent_headers"`
		UserAgentPrefix string            `json:"user_agent_prefix"`
		JSON            json.RawMessage   `json:"json"`
		AbsentKeys      []string          `json:"absent_keys"`
	} `json:"expect"`
}

type requestsFile struct {
	CannedResponses map[string]httpReply `json:"canned_responses"`
}

func TestConformanceRequests(t *testing.T) {
	var extra requestsFile
	vectors := loadVectors(t, "requests.json", &extra)
	runVectors(t, "requests.json", vectors, func(v requestVector) string { return v.ID }, func(t *testing.T, v requestVector) {
		canned, ok := extra.CannedResponses[v.Operation]
		if !ok {
			t.Fatalf("no canned response for %s", v.Operation)
		}
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){canned.respond}}
		c := newTestClient(t, ft, unicom360.WithMaxRetries(0))
		if _, err := invoke(context.Background(), c, v.Operation, v.Params); err != nil {
			t.Fatalf("call failed: %v", err)
		}
		if ft.calls() != 1 {
			t.Fatalf("made %d requests, want 1", ft.calls())
		}
		got := ft.requests[0]
		e := v.Expect
		if got.req.Method != e.Method {
			t.Errorf("method %s, want %s", got.req.Method, e.Method)
		}
		if got.req.URL.Path != e.Path {
			t.Errorf("path %q, want %q", got.req.URL.Path, e.Path)
		}
		if got.req.URL.RawQuery != "" || got.req.URL.ForceQuery {
			t.Errorf("query string %q, want none", got.req.URL.RawQuery)
		}
		for name, want := range e.Headers {
			want = strings.ReplaceAll(want, "<KEY>", testAPIKey)
			if vals := got.req.Header.Values(name); len(vals) != 1 || vals[0] != want {
				t.Errorf("header %s = %q, want %q", name, vals, want)
			}
		}
		for _, name := range e.AbsentHeaders {
			if vals := got.req.Header.Values(name); len(vals) > 0 {
				t.Errorf("header %s = %q, want absent", name, vals)
			}
		}
		if ua := got.req.Header.Get("User-Agent"); !strings.HasPrefix(ua, e.UserAgentPrefix) {
			t.Errorf("User-Agent %q, want prefix %q", ua, e.UserAgentPrefix)
		}
		if string(e.JSON) == "null" || len(e.JSON) == 0 {
			if len(got.body) != 0 {
				t.Errorf("body %q, want none", got.body)
			}
		} else {
			if !json.Valid(got.body) {
				t.Fatalf("body is not JSON: %q", got.body)
			}
			want, have := decodeAny(e.JSON), decodeAny(got.body)
			if !deepEqualJSON(want, have) {
				t.Errorf("body\n got %s\nwant %s", got.body, e.JSON)
			}
			for _, ptr := range e.AbsentKeys {
				if pointerExists(have, ptr) {
					t.Errorf("body has %s, want it absent", ptr)
				}
			}
		}
	})
}

// pointerExists reports whether a JSON Pointer (RFC 6901) resolves in v.
func pointerExists(v any, ptr string) bool {
	if ptr == "" {
		return true
	}
	for _, tok := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[tok]
			if !ok {
				return false
			}
			v = next
		case []any:
			var i int
			if _, err := fmt.Sscanf(tok, "%d", &i); err != nil || i < 0 || i >= len(x) {
				return false
			}
			v = x[i]
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------------------
// responses.json
// ---------------------------------------------------------------------------------------

type expectedError struct {
	Type         string       `json:"type"`
	Class        string       `json:"class"`
	Code         *string      `json:"code"`
	Message      *string      `json:"message"`
	APIReference *string      `json:"api_reference"`
	HTTPStatus   *int         `json:"http_status"`
	DataPresent  *bool        `json:"data_present"`
	RetryAfter   *json.Number `json:"retry_after"`
}

type responseVector struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Operation   string       `json:"operation"`
	OffSpec     bool         `json:"off_spec"`
	NowUnix     *json.Number `json:"now_unix"`
	HTTP        httpReply    `json:"http"`
	Expect      struct {
		Result  json.RawMessage `json:"result"`
		Helpers *struct {
			Accepted  *[]int          `json:"accepted"`
			Invalid   *[]int          `json:"invalid"`
			Rejected  *[]int          `json:"rejected"`
			ByNumber  map[string]*int `json:"by_number"`
			ByAddress map[string]*int `json:"by_address"`
		} `json:"helpers"`
		Error *expectedError `json:"error"`
	} `json:"expect"`
}

// sampleParams gives each operation valid params: the first requests.json vector's.
func sampleParams(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for _, raw := range loadVectors(t, "requests.json", &requestsFile{}) {
		var v requestVector
		if err := strictDecode(raw, &v); err != nil {
			t.Fatal(err)
		}
		if _, ok := out[v.Operation]; !ok {
			out[v.Operation] = v.Params
		}
	}
	return out
}

func TestConformanceResponses(t *testing.T) {
	params := sampleParams(t)
	vectors := loadVectors(t, "responses.json", nil)
	runVectors(t, "responses.json", vectors, func(v responseVector) string { return v.ID }, func(t *testing.T, v responseVector) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){v.HTTP.respond}}
		opts := []unicom360.Option{unicom360.WithMaxRetries(0)}
		if v.NowUnix != nil {
			opts = append(opts, unicom360.WithClock(fixedClock(*v.NowUnix)))
		}
		c := newTestClient(t, ft, opts...)
		p, ok := params[v.Operation]
		if !ok {
			t.Fatalf("no sample params for %s", v.Operation)
		}
		result, err := invoke(context.Background(), c, v.Operation, p)
		var he *harnessErr
		if errors.As(err, &he) {
			t.Fatal(err)
		}
		switch {
		case v.Expect.Error != nil:
			if v.Expect.Result != nil || v.Expect.Helpers != nil {
				t.Fatal("vector expects both a result and an error")
			}
			checkError(t, err, *v.Expect.Error, 1, true)
		case v.Expect.Result != nil:
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := canonicalResult(v.Operation, result)
			want := decodeAny(v.Expect.Result)
			if !canonicalEqual(want, got) {
				t.Errorf("result\n got %s\nwant %s", toJSON(got), toJSON(want))
			}
			if v.Expect.Helpers != nil {
				checkHelpers(t, v.Operation, result, v)
			}
		default:
			t.Fatal("vector expects neither a result nor an error")
		}
	})
}

// checkError asserts an error against a fixture's expectation. full is true for
// responses.json, which states every field (null meaning "none"); retries.json states only
// type, class, code and http_status.
func checkError(t *testing.T, err error, want expectedError, attempts int, full bool) {
	t.Helper()
	if err == nil {
		t.Fatalf("succeeded, want %s", want.Class)
	}
	var e *unicom360.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %T %v is not an *unicom360.Error", err, err)
	}
	if e.Type != want.Type {
		t.Errorf("type %q, want %q", e.Type, want.Type)
	}
	if e.Class() != want.Class {
		t.Errorf("class %q, want %q", e.Class(), want.Class)
	}
	wantCode := ""
	if want.Code != nil {
		wantCode = *want.Code
	}
	if e.Code != wantCode {
		t.Errorf("code %q, want %q", e.Code, wantCode)
	}
	switch {
	case full && want.Message != nil:
		if e.Message != *want.Message {
			t.Errorf("message %q, want %q", e.Message, *want.Message)
		}
	case e.Message == "":
		t.Errorf("message is empty, want a message")
	}
	wantRef := ""
	if want.APIReference != nil {
		wantRef = *want.APIReference
	}
	if full && e.APIReference != wantRef {
		t.Errorf("api_reference %q, want %q", e.APIReference, wantRef)
	}
	wantStatus := 0
	if want.HTTPStatus != nil {
		wantStatus = *want.HTTPStatus
	}
	if e.HTTPStatus != wantStatus {
		t.Errorf("http_status %d, want %d", e.HTTPStatus, wantStatus)
	}
	if full && want.DataPresent == nil {
		t.Errorf("vector does not state data_present")
	}
	if want.DataPresent != nil && (e.Data != nil) != *want.DataPresent {
		t.Errorf("data present %t, want %t", e.Data != nil, *want.DataPresent)
	}
	if want.RetryAfter != nil {
		ra, _ := want.RetryAfter.Float64()
		if math.Abs(e.RetryAfter.Seconds()-ra) > 1e-6 {
			t.Errorf("retry_after %v, want %vs", e.RetryAfter, ra)
		}
	}
	if attempts > 0 && e.Attempts != attempts {
		t.Errorf("attempts %d, want %d", e.Attempts, attempts)
	}
	// Every error class matches its own sentinel through errors.Is.
	sentinel := map[string]error{
		"APIError": unicom360.ErrAPI, "InsufficientBundleError": unicom360.ErrInsufficientBundle,
		"DuplicateReferenceError": unicom360.ErrDuplicateReference, "NoValidRecipientsError": unicom360.ErrNoValidRecipients,
		"SessionWindowClosedError": unicom360.ErrSessionWindowClosed, "APIStatusError": unicom360.ErrStatus,
		"AuthenticationError": unicom360.ErrAuthentication, "RateLimitError": unicom360.ErrRateLimit,
		"InvalidRequestError": unicom360.ErrInvalidRequest, "APIServerError": unicom360.ErrServer,
		"APIConnectionError": unicom360.ErrConnection, "APITimeoutError": unicom360.ErrTimeout,
	}[want.Class]
	if sentinel != nil && !errors.Is(err, sentinel) {
		t.Errorf("errors.Is(err, sentinel of %s) is false", want.Class)
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error text contains the API key: %s", err)
	}
}

// positions maps helper results back to their index in the result list.
func positions[T any](all []T, picked []*T) []int {
	out := []int{}
	for _, p := range picked {
		idx := -1
		for i := range all {
			if &all[i] == p {
				idx = i
			}
		}
		out = append(out, idx)
	}
	return out
}

func position[T any](all []T, picked *T) *int {
	if picked == nil {
		return nil
	}
	p := positions(all, []*T{picked})[0]
	return &p
}

func checkHelpers(t *testing.T, op string, result any, v responseVector) {
	t.Helper()
	h := v.Expect.Helpers
	eqInts := func(name string, got []int, want *[]int) {
		if want == nil {
			return
		}
		if toJSON(got) != toJSON(*want) {
			t.Errorf("%s() positions %v, want %v", name, got, *want)
		}
	}
	eqPos := func(name, arg string, got, want *int) {
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%s(%q) = %s, want %s", name, arg, toJSON(got), toJSON(want))
		}
	}
	switch op {
	case "sms.send":
		r := result.(*unicom360.SMSSendResult)
		if h.Rejected != nil || h.ByAddress != nil {
			t.Fatal("helpers not defined for sms.send")
		}
		eqInts("accepted", positions(r.Results, r.Accepted()), h.Accepted)
		eqInts("invalid", positions(r.Results, r.Invalid()), h.Invalid)
		for arg, want := range h.ByNumber {
			eqPos("by_number", arg, position(r.Results, r.ByNumber(arg)), want)
		}
	case "email.send":
		r := result.(*unicom360.EmailSendResult)
		if h.Rejected != nil || h.ByNumber != nil {
			t.Fatal("helpers not defined for email.send")
		}
		eqInts("accepted", positions(r.Results, r.Accepted()), h.Accepted)
		eqInts("invalid", positions(r.Results, r.Invalid()), h.Invalid)
		for arg, want := range h.ByAddress {
			eqPos("by_address", arg, position(r.Results, r.ByAddress(arg)), want)
		}
	case "whatsapp.send_template":
		r := result.(*unicom360.TemplateSendResult)
		if h.Invalid != nil || h.ByAddress != nil {
			t.Fatal("helpers not defined for whatsapp.send_template")
		}
		eqInts("accepted", positions(r.Results, r.Accepted()), h.Accepted)
		eqInts("rejected", positions(r.Results, r.Rejected()), h.Rejected)
		for arg, want := range h.ByNumber {
			eqPos("by_number", arg, position(r.Results, r.ByNumber(arg)), want)
		}
	default:
		t.Fatalf("helpers on %s, which has none", op)
	}
}

// ---------------------------------------------------------------------------------------
// retries.json
// ---------------------------------------------------------------------------------------

type retryVector struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Operation   string          `json:"operation"`
	Params      json.RawMessage `json:"params"`
	NowUnix     json.Number     `json:"now_unix"`
	Config      struct {
		MaxRetries   int         `json:"max_retries"`
		MaxRetryWait json.Number `json:"max_retry_wait"`
	} `json:"config"`
	Steps []struct {
		HTTP      *httpReply `json:"http"`
		Transport string     `json:"transport"`
	} `json:"steps"`
	Expect struct {
		Attempts int `json:"attempts"`
		Sleeps   []struct {
			Min json.Number `json:"min"`
			Max json.Number `json:"max"`
		} `json:"sleeps"`
		Outcome struct {
			Kind       string  `json:"kind"`
			Type       string  `json:"type"`
			Class      string  `json:"class"`
			Code       *string `json:"code"`
			HTTPStatus *int    `json:"http_status"`
		} `json:"outcome"`
	} `json:"expect"`
}

// transportFailure builds the failure a retries.json step names, either as the net errors a
// real dial or read produces (mode "net") or as an explicit *unicom360.TransportError.
func transportFailure(kind, mode string) (error, error) {
	facts := map[string][2]bool{
		"connect_refused":    {false, false},
		"dns_failure":        {false, false},
		"timeout_after_send": {true, true},
		"reset_after_send":   {true, false},
	}
	f, ok := facts[kind]
	if !ok {
		return nil, fmt.Errorf("unknown transport step %q", kind)
	}
	if mode == "transport-error" {
		return &unicom360.TransportError{Sent: f[0], Timeout: f[1], Err: errors.New(kind)}, nil
	}
	switch kind {
	case "connect_refused":
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, nil
	case "dns_failure":
		return &net.DNSError{Err: "no such host", Name: "api.unicom360.com", IsNotFound: true}, nil
	case "timeout_after_send":
		return &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, nil
	default:
		return &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}, nil
	}
}

func TestConformanceRetries(t *testing.T) {
	vectors := loadVectors(t, "retries.json", nil)
	runVectors(t, "retries.json", vectors, func(v retryVector) string { return v.ID }, func(t *testing.T, v retryVector) {
		randoms := map[string]func() float64{
			"random-0":       func() float64 { return 0 },
			"random-max":     func() float64 { return math.Nextafter(1, 0) },
			"random-default": nil, // the SDK's own source
		}
		for _, mode := range []string{"net", "transport-error"} {
			for rname, random := range randoms {
				t.Run(mode+"/"+rname, func(t *testing.T) { runRetryVector(t, v, mode, random) })
			}
		}
	})
}

func runRetryVector(t *testing.T, v retryVector, mode string, random func() float64) {
	var mu sync.Mutex
	clock := unixTime(v.NowUnix)
	var sleeps []float64
	ft := &fakeTransport{}
	for i, step := range v.Steps {
		switch {
		case step.HTTP != nil && step.Transport == "":
			ft.replies = append(ft.replies, step.HTTP.respond)
		case step.HTTP == nil && step.Transport != "":
			failure, err := transportFailure(step.Transport, mode)
			if err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			ft.replies = append(ft.replies, func(*http.Request) (*http.Response, error) { return nil, failure })
		default:
			t.Fatalf("step %d is neither http nor transport", i)
		}
	}
	// Any attempt past the last step is a harness failure, not a reply.
	ft.replies = append(ft.replies, func(*http.Request) (*http.Response, error) {
		return nil, &unicom360.TransportError{Err: errors.New("the SDK made more attempts than the vector has steps")}
	})
	wait, _ := v.Config.MaxRetryWait.Float64()
	opts := []unicom360.Option{
		unicom360.WithMaxRetries(v.Config.MaxRetries),
		unicom360.WithMaxRetryWait(time.Duration(wait * float64(time.Second))),
		unicom360.WithClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }),
		unicom360.WithSleeper(func(_ context.Context, d time.Duration) error {
			mu.Lock()
			defer mu.Unlock()
			sleeps = append(sleeps, d.Seconds())
			clock = clock.Add(d)
			return nil
		}),
	}
	if random != nil {
		opts = append(opts, unicom360.WithRandom(random))
	}
	c := newTestClient(t, ft, opts...)
	_, err := invoke(context.Background(), c, v.Operation, v.Params)
	var he *harnessErr
	if errors.As(err, &he) {
		t.Fatal(err)
	}
	if ft.calls() != v.Expect.Attempts {
		t.Errorf("attempts %d, want %d", ft.calls(), v.Expect.Attempts)
	}
	if len(sleeps) != len(v.Expect.Sleeps) {
		t.Fatalf("sleeps %v, want %d of them", sleeps, len(v.Expect.Sleeps))
	}
	for i, s := range v.Expect.Sleeps {
		lo, _ := s.Min.Float64()
		hi, _ := s.Max.Float64()
		if sleeps[i] < lo-1e-6 || sleeps[i] > hi+1e-6 {
			t.Errorf("sleep %d is %.6fs, want within [%v, %v]", i, sleeps[i], lo, hi)
		}
	}
	o := v.Expect.Outcome
	switch o.Kind {
	case "result":
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case "error":
		checkError(t, err, expectedError{Type: o.Type, Class: o.Class, Code: o.Code, HTTPStatus: o.HTTPStatus}, v.Expect.Attempts, false)
	default:
		t.Fatalf("unknown outcome kind %q", o.Kind)
	}
}

// ---------------------------------------------------------------------------------------
// error-codes.json
// ---------------------------------------------------------------------------------------

type errorCodeVector struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Code    string `json:"code"`
	Source  string `json:"source"`
	Note    string `json:"note"`
}

func TestConformanceErrorCodes(t *testing.T) {
	vectors := loadVectors(t, "error-codes.json", nil)
	runVectors(t, "error-codes.json", vectors, func(v errorCodeVector) string { return v.ID }, func(t *testing.T, v errorCodeVector) {
		if got := unicom360.NormalizeCode(v.Message); got != v.Code {
			t.Errorf("NormalizeCode(%q) = %q, want %q", v.Message, got, v.Code)
		}
	})
}

// ---------------------------------------------------------------------------------------
// callbacks.json
// ---------------------------------------------------------------------------------------

type callbackVector struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	OffSpec     bool   `json:"off_spec"`
	BodyText    string `json:"body_text"`
	Expect      struct {
		Result json.RawMessage `json:"result"`
		Error  *bool           `json:"error"`
	} `json:"expect"`
}

func TestConformanceCallbacks(t *testing.T) {
	vectors := loadVectors(t, "callbacks.json", nil)
	runVectors(t, "callbacks.json", vectors, func(v callbackVector) string { return v.ID }, func(t *testing.T, v callbackVector) {
		var result any
		var err error
		switch v.Kind {
		case "sms":
			result, err = webhook.ParseSMSCallback([]byte(v.BodyText))
		case "email":
			result, err = webhook.ParseEmailCallback([]byte(v.BodyText))
		default:
			t.Fatalf("unknown callback kind %q", v.Kind)
		}
		switch {
		case v.Expect.Error != nil && *v.Expect.Error:
			var e *unicom360.Error
			if err == nil || !errors.As(err, &e) {
				t.Fatalf("got %v, want an *unicom360.Error", err)
			}
		case v.Expect.Result != nil:
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, want := canonical(result, nil), decodeAny(v.Expect.Result)
			if !canonicalEqual(want, got) {
				t.Errorf("report\n got %s\nwant %s", toJSON(got), toJSON(want))
			}
		default:
			t.Fatal("vector expects neither a result nor an error")
		}
	})
}

// ---------------------------------------------------------------------------------------
// gsm7.json
// ---------------------------------------------------------------------------------------

type gsmVector struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Text        string `json:"text"`
	IsGSM7      bool   `json:"is_gsm7"`
	Length      int    `json:"length"`
	Units       int    `json:"units"`
	NonGSM      []struct {
		Char  string `json:"char"`
		Index int    `json:"index"`
	} `json:"non_gsm"`
	Validate []string `json:"validate"`
}

type gsmFile struct {
	Charset    string `json:"charset"`
	MaxLength  int    `json:"max_length"`
	UnitLength int    `json:"unit_length"`
}

func TestConformanceGSM7(t *testing.T) {
	var extra gsmFile
	vectors := loadVectors(t, "gsm7.json", &extra)
	if gsm.Charset != extra.Charset {
		t.Fatalf("gsm.Charset differs from the fixture's charset")
	}
	if gsm.MaxLength != extra.MaxLength || gsm.UnitLength != extra.UnitLength {
		t.Fatalf("MaxLength %d / UnitLength %d, want %d / %d", gsm.MaxLength, gsm.UnitLength, extra.MaxLength, extra.UnitLength)
	}
	runVectors(t, "gsm7.json", vectors, func(v gsmVector) string { return v.ID }, func(t *testing.T, v gsmVector) {
		if got := gsm.IsGSM7(v.Text); got != v.IsGSM7 {
			t.Errorf("IsGSM7 = %t, want %t", got, v.IsGSM7)
		}
		if got := gsm.Length(v.Text); got != v.Length {
			t.Errorf("Length = %d, want %d", got, v.Length)
		}
		if got := gsm.Units(v.Text); got != v.Units {
			t.Errorf("Units = %d, want %d", got, v.Units)
		}
		var got []string
		for _, c := range gsm.NonGSMCharacters(v.Text) {
			got = append(got, fmt.Sprintf("%q@%d", string(c.Char), c.Index))
		}
		var want []string
		for _, c := range v.NonGSM {
			want = append(want, fmt.Sprintf("%q@%d", c.Char, c.Index))
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("NonGSMCharacters = %v, want %v", got, want)
		}
		var issues []string
		for _, i := range gsm.ValidateSMS(v.Text) {
			issues = append(issues, string(i))
		}
		if strings.Join(issues, ",") != strings.Join(v.Validate, ",") {
			t.Errorf("ValidateSMS = %v, want %v", issues, v.Validate)
		}
	})
}

// ---------------------------------------------------------------------------------------
// webhooks/signatures.json
// ---------------------------------------------------------------------------------------

type signatureVector struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Secret      string            `json:"secret"`
	Headers     map[string]string `json:"headers"`
	BodyBase64  string            `json:"body_base64"`
	BodyText    string            `json:"body_text"`
	NowUnix     json.Number       `json:"now_unix"`
	Tolerance   json.Number       `json:"tolerance"`
	Expect      struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	} `json:"expect"`
}

type signaturesFile struct {
	Algorithm string `json:"algorithm"`
}

// rawHeaders keeps each name exactly as the fixture spells it (no canonicalisation), so the
// case-insensitive lookup is really exercised.
func rawHeaders(in map[string]string) http.Header {
	h := http.Header{}
	for k, v := range in {
		h[k] = []string{v}
	}
	return h
}

func webhookOptions(now, tolerance json.Number) []webhook.Option {
	tol, _ := tolerance.Float64()
	return []webhook.Option{
		webhook.WithTolerance(time.Duration(tol * float64(time.Second))),
		webhook.WithClock(fixedClock(now)),
	}
}

func TestConformanceWebhookSignatures(t *testing.T) {
	vectors := loadVectors(t, "webhooks/signatures.json", &signaturesFile{})
	runVectors(t, "webhooks/signatures.json", vectors, func(v signatureVector) string { return v.ID }, func(t *testing.T, v signatureVector) {
		body, err := base64.StdEncoding.DecodeString(v.BodyBase64)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != v.BodyText {
			t.Fatal("body_base64 and body_text differ")
		}
		err = webhook.Verify(body, rawHeaders(v.Headers), v.Secret, webhookOptions(v.NowUnix, v.Tolerance)...)
		if v.Expect.Valid {
			if err != nil {
				t.Fatalf("Verify: %v, want valid", err)
			}
			return
		}
		var e *unicom360.Error
		if !errors.As(err, &e) {
			t.Fatalf("Verify: %v, want a SignatureVerificationError", err)
		}
		if e.Class() != "SignatureVerificationError" || e.Type != unicom360.TypeSignatureVerificationError || !errors.Is(err, unicom360.ErrSignatureVerification) {
			t.Errorf("error %s / %s, want SignatureVerificationError", e.Class(), e.Type)
		}
		if e.Reason != v.Expect.Reason {
			t.Errorf("reason %q, want %q", e.Reason, v.Expect.Reason)
		}
		if strings.Contains(err.Error(), v.Secret) {
			t.Errorf("error text contains the secret")
		}
	})
}

// ---------------------------------------------------------------------------------------
// webhooks/events.json
// ---------------------------------------------------------------------------------------

type eventVector struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Secret      string            `json:"secret"`
	Headers     map[string]string `json:"headers"`
	BodyText    string            `json:"body_text"`
	NowUnix     json.Number       `json:"now_unix"`
	Tolerance   json.Number       `json:"tolerance"`
	Expect      struct {
		Kind       string          `json:"kind"`
		Event      string          `json:"event"`
		DeliveryID *string         `json:"delivery_id"`
		CreatedAt  *string         `json:"created_at"`
		Data       json.RawMessage `json:"data"`
	} `json:"expect"`
}

func TestConformanceWebhookEvents(t *testing.T) {
	vectors := loadVectors(t, "webhooks/events.json", nil)
	runVectors(t, "webhooks/events.json", vectors, func(v eventVector) string { return v.ID }, func(t *testing.T, v eventVector) {
		ev, err := webhook.ConstructEvent([]byte(v.BodyText), rawHeaders(v.Headers), v.Secret, webhookOptions(v.NowUnix, v.Tolerance)...)
		if err != nil {
			t.Fatalf("ConstructEvent: %v", err)
		}
		var kind string
		var data any
		switch e := ev.(type) {
		case *webhook.MessageStatusEvent:
			kind, data = "message_status", canonical(e.Data, nil)
		case *webhook.MessageReceivedEvent:
			kind, data = "message_received", canonical(e.Data, nil)
		case *webhook.TestEvent:
			kind, data = "test", canonical(e.Data, nil)
		case *webhook.UnknownEvent:
			kind, data = "unknown", canonical(e.Data, nil)
		default:
			t.Fatalf("unexpected event type %T", ev)
		}
		if kind != v.Expect.Kind {
			t.Errorf("kind %q, want %q", kind, v.Expect.Kind)
		}
		m := ev.Meta()
		if m.Event != v.Expect.Event {
			t.Errorf("event %q, want %q", m.Event, v.Expect.Event)
		}
		if want := deref(v.Expect.DeliveryID); m.DeliveryID != want {
			t.Errorf("delivery_id %q, want %q", m.DeliveryID, want)
		}
		if want := deref(v.Expect.CreatedAt); m.CreatedAt != want {
			t.Errorf("created_at %q, want %q", m.CreatedAt, want)
		}
		if want := decodeAny(v.Expect.Data); !canonicalEqual(want, data) {
			t.Errorf("data\n got %s\nwant %s", toJSON(data), toJSON(want))
		}
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------------------------------------------------------------------------------
// The vendored copy itself
// ---------------------------------------------------------------------------------------

func TestConformanceFixturesUnmodified(t *testing.T) {
	version := string(readFixture(t, "VERSION"))
	fields := map[string]string{}
	for _, line := range strings.Split(version, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			fields[k] = v
		}
	}
	if fields["source"] != "unicom360-openapi" {
		t.Fatalf("VERSION was not written by sync-conformance: %q", version)
	}
	if fields["version"] != unicom360.ContractVersion || fields["tag"] != "v"+unicom360.ContractVersion {
		t.Fatalf("fixtures are %s (%s), want v%s", fields["version"], fields["tag"], unicom360.ContractVersion)
	}
	got, err := treeHash(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != fields["sha256"] {
		t.Fatalf("fixture tree hash %s, VERSION says %s: the vendored copy was modified", got, fields["sha256"])
	}
}
