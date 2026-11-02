package unicom360_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

const smsOK = `{"api_reference":"r-1","data":[{"message_id":"m-1","status":"ACKNOWLEDGED","mobile_number":"260971234567"}],"message":"successful","success":true}`

func reply(status int, body string) func(*http.Request) (*http.Response, error) {
	return httpReply{Status: status, Headers: map[string]string{"content-type": "application/json"}, BodyText: body}.respond
}

// ---------------------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------------------

func TestNewClientAPIKey(t *testing.T) {
	t.Setenv(unicom360.EnvAPIKey, "")
	t.Setenv(unicom360.EnvBaseURL, "")

	for _, tc := range []struct {
		name string
		env  string
		opts []unicom360.Option
		ok   bool
	}{
		{"missing", "", nil, false},
		{"from the environment", "env-key", nil, true},
		{"explicit", "", []unicom360.Option{unicom360.WithAPIKey("k")}, true},
		{"explicit empty does not fall back", "env-key", []unicom360.Option{unicom360.WithAPIKey("")}, false},
		{"whitespace only", "", []unicom360.Option{unicom360.WithAPIKey(" \t\n")}, false},
		{"control character", "", []unicom360.Option{unicom360.WithAPIKey("k\r\nX-Evil: 1")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(unicom360.EnvAPIKey, tc.env)
			_, err := unicom360.NewClient(tc.opts...)
			if tc.ok && err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if !tc.ok {
				if !errors.Is(err, unicom360.ErrConfiguration) {
					t.Fatalf("NewClient: %v, want a configuration error", err)
				}
				var e *unicom360.Error
				if errors.As(err, &e) && (e.Class() != "ConfigurationError" || e.Type != unicom360.TypeConfigurationError) {
					t.Fatalf("class %s type %s", e.Class(), e.Type)
				}
			}
		})
	}
}

func TestNewClientBaseURL(t *testing.T) {
	t.Setenv(unicom360.EnvBaseURL, "")
	for _, tc := range []struct {
		in, want string // want "" means a configuration error
	}{
		{"https://api.unicom360.com", "https://api.unicom360.com"},
		{"https://api.unicom360.com/", "https://api.unicom360.com"},
		{"https://proxy.example.com/unicom360//", "https://proxy.example.com/unicom360"},
		{"HTTPS://api.unicom360.com", "https://api.unicom360.com"},
		{"http://localhost:4010", "http://localhost:4010"},
		{"http://LOCALHOST", "http://LOCALHOST"},
		{"http://127.0.0.1:4010", "http://127.0.0.1:4010"},
		{"http://127.5.6.7", "http://127.5.6.7"},
		{"http://[::1]:8080/base", "http://[::1]:8080/base"},
		{"http://api.unicom360.com", ""},
		{"http://10.0.0.1", ""},
		{"http://128.0.0.1", ""},
		{"http://localhost.example.com", ""},
		{"ftp://api.unicom360.com", ""},
		{"api.unicom360.com", ""},
		{"https://", ""},
		{"https://api.unicom360.com?x=1", ""},
		{"https://api.unicom360.com/?", ""},
		{"https://api.unicom360.com#frag", ""},
		{"https://user:pass@api.unicom360.com", ""},
		{"", ""},
	} {
		c, err := unicom360.NewClient(unicom360.WithAPIKey("k"), unicom360.WithBaseURL(tc.in))
		switch {
		case tc.want == "" && !errors.Is(err, unicom360.ErrConfiguration):
			t.Errorf("%q: got %v, want a configuration error", tc.in, err)
		case tc.want != "" && err != nil:
			t.Errorf("%q: %v", tc.in, err)
		case tc.want != "" && c.BaseURL() != tc.want:
			t.Errorf("%q: base URL %q, want %q", tc.in, c.BaseURL(), tc.want)
		}
	}

	t.Run("environment", func(t *testing.T) {
		t.Setenv(unicom360.EnvBaseURL, "https://proxy.example.com/u360/")
		c, err := unicom360.NewClient(unicom360.WithAPIKey("k"))
		if err != nil || c.BaseURL() != "https://proxy.example.com/u360" {
			t.Fatalf("got %v %v", c, err)
		}
		c, err = unicom360.NewClient(unicom360.WithAPIKey("k"), unicom360.WithBaseURL("https://other.example.com"))
		if err != nil || c.BaseURL() != "https://other.example.com" {
			t.Fatalf("explicit option should win: %v %v", c, err)
		}
	})
	t.Run("default", func(t *testing.T) {
		c, err := unicom360.NewClient(unicom360.WithAPIKey("k"))
		if err != nil || c.BaseURL() != unicom360.DefaultBaseURL {
			t.Fatalf("got %v %v", c, err)
		}
	})
}

func TestOptionValidation(t *testing.T) {
	for name, opt := range map[string]unicom360.Option{
		"timeout":         unicom360.WithTimeout(0),
		"connect timeout": unicom360.WithConnectTimeout(-time.Second),
		"max retries":     unicom360.WithMaxRetries(-1),
		"max retry wait":  unicom360.WithMaxRetryWait(-time.Second),
		"app info":        unicom360.WithAppInfo(unicom360.AppInfo{Version: "1.0"}),
		"http client":     unicom360.WithHTTPClient(nil),
		"transport":       unicom360.WithTransport(nil),
		"clock":           unicom360.WithClock(nil),
		"sleeper":         unicom360.WithSleeper(nil),
		"random":          unicom360.WithRandom(nil),
	} {
		if _, err := unicom360.NewClient(unicom360.WithAPIKey("k"), opt); !errors.Is(err, unicom360.ErrConfiguration) {
			t.Errorf("%s: got %v, want a configuration error", name, err)
		}
	}
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, smsOK)}}
	c := newTestClient(t, ft)
	for name, opt := range map[string]unicom360.RequestOption{
		"request timeout":     unicom360.WithRequestTimeout(0),
		"request max retries": unicom360.WithRequestMaxRetries(-2),
	} {
		if _, err := c.Health(context.Background(), opt); !errors.Is(err, unicom360.ErrConfiguration) {
			t.Errorf("%s: got %v, want a configuration error", name, err)
		}
	}
	if ft.calls() != 0 {
		t.Errorf("an invalid request option still sent %d requests", ft.calls())
	}
}

func TestToAndToOneAreExclusive(t *testing.T) {
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, smsOK)}}
	c := newTestClient(t, ft)
	_, err := c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "A", To: []string{"1"}, ToOne: "2", Message: "x"})
	if !errors.Is(err, unicom360.ErrConfiguration) {
		t.Fatalf("got %v", err)
	}
	_, err = c.Email.Send(context.Background(), unicom360.EmailSendParams{From: "a@b", To: []string{"x@y"}, ToOne: "z@y"})
	if !errors.Is(err, unicom360.ErrConfiguration) {
		t.Fatalf("got %v", err)
	}
	if ft.calls() != 0 {
		t.Fatalf("sent %d requests", ft.calls())
	}
}

// ---------------------------------------------------------------------------------------
// Request details
// ---------------------------------------------------------------------------------------

func TestUserAgent(t *testing.T) {
	c, err := unicom360.NewClient(unicom360.WithAPIKey("k"),
		unicom360.WithAppInfo(unicom360.AppInfo{Name: "shopsync", Version: "2.3.0", URL: "https://shopsync.example"}))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^unicom360-go/` + regexp.QuoteMeta(unicom360.Version) + ` \(go [^;]+; [a-z0-9]+; [a-z0-9]+\) shopsync/2\.3\.0 \(\+https://shopsync\.example\)$`)
	if !re.MatchString(c.UserAgent()) {
		t.Fatalf("User-Agent %q", c.UserAgent())
	}
	c2, _ := c.WithOptions(unicom360.WithAppInfo(unicom360.AppInfo{Name: "plain"}))
	if !strings.HasSuffix(c2.UserAgent(), ") plain") {
		t.Fatalf("User-Agent %q", c2.UserAgent())
	}
}

func TestBaseURLPathPrefix(t *testing.T) {
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, `{"status":"ok"}`)}}
	c := newTestClient(t, ft, unicom360.WithBaseURL("https://proxy.example.com/unicom360/"))
	if _, err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ft.requests[0].req.URL.String(); got != "https://proxy.example.com/unicom360/health" {
		t.Fatalf("URL %s", got)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/health", http.StatusFound)
	}))
	defer srv.Close()

	// Both the SDK's own client and a caller's client that would follow redirects.
	follow := &http.Client{}
	for name, opts := range map[string][]unicom360.Option{
		"own client":    nil,
		"caller client": {unicom360.WithHTTPClient(follow)},
	} {
		c, err := unicom360.NewClient(append([]unicom360.Option{unicom360.WithAPIKey("secret-key"), unicom360.WithBaseURL(srv.URL)}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Balance.All(context.Background())
		var e *unicom360.Error
		if !errors.As(err, &e) || e.Class() != "APIStatusError" || e.HTTPStatus != http.StatusFound {
			t.Fatalf("%s: got %v, want an APIStatusError 302", name, err)
		}
		if e.Headers.Get("Location") == "" {
			t.Errorf("%s: the error should carry the response headers", name)
		}
	}
	if elsewhere.Load() != 0 {
		t.Fatal("a redirect was followed: the API key went to another host")
	}
	if follow.CheckRedirect != nil {
		t.Fatal("the caller's http.Client was modified")
	}
}

// ---------------------------------------------------------------------------------------
// Transport failures on a real network
// ---------------------------------------------------------------------------------------

func noSleep(context.Context, time.Duration) error { return nil }

func TestConnectionRefusedIsRetriedForAnyCall(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens there now

	c, err := unicom360.NewClient(unicom360.WithAPIKey("k"), unicom360.WithBaseURL("http://"+addr),
		unicom360.WithSleeper(noSleep), unicom360.WithMaxRetries(2))
	if err != nil {
		t.Fatal(err)
	}
	// An email send is never retry-safe, but a refused connection never reached the server.
	_, err = c.Email.Send(context.Background(), unicom360.EmailSendParams{From: "a@b.c", ToOne: "x@y.z", Subject: "s", EmailBody: "b"})
	var e *unicom360.Error
	if !errors.As(err, &e) || e.Type != unicom360.TypeConnectionError || e.Attempts != 3 {
		t.Fatalf("got %v (attempts %d), want a connection error after 3 attempts", err, attemptsOf(err))
	}
	if !errors.Is(err, unicom360.ErrConnection) || errors.Is(err, unicom360.ErrTimeout) {
		t.Fatal("sentinels do not match a connection error")
	}
}

func attemptsOf(err error) int {
	var e *unicom360.Error
	if errors.As(err, &e) {
		return e.Attempts
	}
	return -1
}

// dropAfterRequest is a server that reads a whole request, then closes the connection without
// answering: the request reached the server.
func dropAfterRequest(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.ReadAll(r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

func TestResetAfterSendRetriedOnlyWhenSafe(t *testing.T) {
	url, hits := dropAfterRequest(t)
	c, err := unicom360.NewClient(unicom360.WithAPIKey("k"), unicom360.WithBaseURL(url),
		unicom360.WithSleeper(noSleep), unicom360.WithMaxRetries(2))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "A", ToOne: "260971234567", Message: "hi"})
	if !errors.Is(err, unicom360.ErrConnection) || attemptsOf(err) != 1 || hits.Load() != 1 {
		t.Fatalf("unsafe send: %v, attempts %d, server saw %d", err, attemptsOf(err), hits.Load())
	}
	hits.Store(0)
	_, err = c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "A", ToOne: "260971234567", Message: "hi", UserReferenceID: "ref-1"})
	if !errors.Is(err, unicom360.ErrConnection) || attemptsOf(err) != 3 || hits.Load() != 3 {
		t.Fatalf("safe send: %v, attempts %d, server saw %d", err, attemptsOf(err), hits.Load())
	}
}

func TestTimeoutAfterSend(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c, err := unicom360.NewClient(unicom360.WithAPIKey("k"), unicom360.WithBaseURL(srv.URL),
		unicom360.WithSleeper(noSleep), unicom360.WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "A", ToOne: "1", Message: "hi"})
	var e *unicom360.Error
	if !errors.As(err, &e) || e.Class() != "APITimeoutError" || e.Attempts != 1 {
		t.Fatalf("got %v, want one APITimeoutError", err)
	}
	if !errors.Is(err, unicom360.ErrTimeout) || !errors.Is(err, unicom360.ErrConnection) {
		t.Fatal("a timeout must match ErrTimeout and ErrConnection")
	}
	// A read is retried after a timeout; the per-request option overrides the client's.
	hits.Store(0)
	_, err = c.Balance.All(context.Background(), unicom360.WithRequestMaxRetries(1), unicom360.WithRequestTimeout(30*time.Millisecond))
	if attemptsOf(err) != 2 || hits.Load() != 2 {
		t.Fatalf("read: %v attempts %d hits %d", err, attemptsOf(err), hits.Load())
	}
}

func TestContextCancellation(t *testing.T) {
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(503, "")}}
	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(t, ft, unicom360.WithSleeper(func(ctx context.Context, d time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}))
	_, err := c.Balance.All(ctx)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, unicom360.ErrServer) || ft.calls() != 1 {
		t.Fatalf("got %v after %d calls, want the 503 joined with context.Canceled after 1", err, ft.calls())
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	_, err = c.Balance.All(ctx2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// ---------------------------------------------------------------------------------------
// auto_reference_ids
// ---------------------------------------------------------------------------------------

var generatedRef = regexp.MustCompile(`^u360-[0-9a-f]{32}$`)

func bodies(ft *fakeTransport) []map[string]any {
	var out []map[string]any
	for _, r := range ft.requests {
		var m map[string]any
		_ = json.Unmarshal(r.body, &m)
		out = append(out, m)
	}
	return out
}

func TestAutoReferenceIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("sms gets one reference, reused on retry", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(500, ""), reply(200, smsOK)}}
		c := newTestClient(t, ft, unicom360.WithAutoReferenceIDs(true), unicom360.WithSleeper(noSleep))
		if _, err := c.SMS.Send(ctx, unicom360.SMSSendParams{From: "A", ToOne: "1", Message: "x"}); err != nil {
			t.Fatal(err)
		}
		b := bodies(ft)
		if len(b) != 2 {
			t.Fatalf("%d attempts, want 2 (a 500 is retried once the send has a reference)", len(b))
		}
		ref, _ := b[0]["user_reference_id"].(string)
		if !generatedRef.MatchString(ref) || b[1]["user_reference_id"] != ref {
			t.Fatalf("references %v / %v", b[0]["user_reference_id"], b[1]["user_reference_id"])
		}
		// Version 4 UUID bits.
		if ref[5+12] != '4' || !strings.ContainsRune("89ab", rune(ref[5+16])) {
			t.Fatalf("%s is not a UUID v4 without dashes", ref)
		}
	})

	t.Run("a caller's reference is kept", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, smsOK)}}
		c := newTestClient(t, ft, unicom360.WithAutoReferenceIDs(true))
		_, _ = c.SMS.Send(ctx, unicom360.SMSSendParams{From: "A", ToOne: "1", Message: "x", UserReferenceID: "mine"})
		if bodies(ft)[0]["user_reference_id"] != "mine" {
			t.Fatal("caller's reference replaced")
		}
	})

	t.Run("off by default", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, smsOK)}}
		c := newTestClient(t, ft)
		_, _ = c.SMS.Send(ctx, unicom360.SMSSendParams{From: "A", ToOne: "1", Message: "x"})
		if _, ok := bodies(ft)[0]["user_reference_id"]; ok {
			t.Fatal("a reference was generated without the option")
		}
	})

	t.Run("each template recipient gets its own; the caller's slice is untouched", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, `{"success":true,"message":"successful","api_reference":"r","data":{"accepted":3,"rejected":0,"results":[]}}`)}}
		c := newTestClient(t, ft, unicom360.WithAutoReferenceIDs(true))
		recipients := []unicom360.TemplateRecipient{{To: "1"}, {To: "2", UserReferenceID: "keep"}, {To: "3"}}
		if _, err := c.WhatsApp.SendTemplate(ctx, unicom360.TemplateSendParams{Template: "t", Recipients: recipients}); err != nil {
			t.Fatal(err)
		}
		if recipients[0].UserReferenceID != "" || recipients[2].UserReferenceID != "" {
			t.Fatal("the caller's recipients were modified")
		}
		rs := bodies(ft)[0]["recipients"].([]any)
		r0 := rs[0].(map[string]any)["user_reference_id"].(string)
		r1 := rs[1].(map[string]any)["user_reference_id"].(string)
		r2 := rs[2].(map[string]any)["user_reference_id"].(string)
		if !generatedRef.MatchString(r0) || !generatedRef.MatchString(r2) || r0 == r2 || r1 != "keep" {
			t.Fatalf("references %s %s %s", r0, r1, r2)
		}
	})

	t.Run("session", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, `{"success":true,"message":"successful","api_reference":"r","data":{"message_id":"m"}}`)}}
		c := newTestClient(t, ft, unicom360.WithAutoReferenceIDs(true))
		_, _ = c.WhatsApp.SendSession(ctx, unicom360.TextMessage("1", "hi"))
		if !generatedRef.MatchString(bodies(ft)[0]["user_reference_id"].(string)) {
			t.Fatal("no reference")
		}
	})

	t.Run("email gets one but is still not retried after a 5xx", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(502, ""), reply(200, smsOK)}}
		c := newTestClient(t, ft, unicom360.WithAutoReferenceIDs(true), unicom360.WithSleeper(noSleep))
		_, err := c.Email.Send(ctx, unicom360.EmailSendParams{From: "a@b.c", ToOne: "x@y.z", Subject: "s", EmailBody: "b"})
		if !errors.Is(err, unicom360.ErrServer) || ft.calls() != 1 {
			t.Fatalf("got %v after %d calls", err, ft.calls())
		}
		if !generatedRef.MatchString(bodies(ft)[0]["user_reference_id"].(string)) {
			t.Fatal("no reference")
		}
	})
}

// ---------------------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------------------

func TestErrorDetails(t *testing.T) {
	ctx := context.Background()
	t.Run("duplicate session reference", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){
			reply(200, `{"message":"DUPLICATE_MSG_REF","success":false,"api_reference":"r-9","data":{"message_id":"orig-1"}}`)}}
		c := newTestClient(t, ft)
		_, err := c.WhatsApp.SendSession(ctx, unicom360.TextMessage("1", "hi"))
		var e *unicom360.Error
		if !errors.As(err, &e) || e.OriginalMessageID != "orig-1" || !errors.Is(err, unicom360.ErrDuplicateReference) || !errors.Is(err, unicom360.ErrAPI) {
			t.Fatalf("got %#v", err)
		}
		want := "unicom360: DuplicateReferenceError: DUPLICATE_MSG_REF (code DUPLICATE_REFERENCE, HTTP 200, api_reference r-9)"
		if err.Error() != want {
			t.Fatalf("Error() = %q, want %q", err.Error(), want)
		}
	})
	t.Run("no valid recipients", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){
			reply(200, `{"message":"NO_VALID_RECIPIENTS","success":false,"api_reference":"r","data":{"accepted":0,"rejected":1,"results":[{"message_id":null,"to":12,"user_reference_id":7,"status":"REJECTED","reason":"INVALID_NUMBER","category":"UTILITY"}]}}`)}}
		c := newTestClient(t, ft)
		_, err := c.WhatsApp.SendTemplate(ctx, unicom360.TemplateSendParams{Template: "t", Recipients: []unicom360.TemplateRecipient{{To: "12"}}})
		var e *unicom360.Error
		if !errors.As(err, &e) || e.Class() != "NoValidRecipientsError" || len(e.Results) != 1 ||
			e.Results[0].Reason != "INVALID_NUMBER" || e.Results[0].To != "12" || e.Results[0].UserReferenceID != "7" {
			t.Fatalf("got %#v", e)
		}
	})
	t.Run("rate limit", func(t *testing.T) {
		ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){
			reply(429, `{"message":"RATE_LIMIT_EXCEEDED","success":false,"api_reference":"r"}`)}}
		at := time.Unix(1767225645, 0)
		c := newTestClient(t, ft, unicom360.WithMaxRetries(0), unicom360.WithClock(func() time.Time { return at }))
		_, err := c.Balance.Get(ctx, unicom360.BundleSMS)
		var e *unicom360.Error
		if !errors.As(err, &e) || e.RetryAfter != 15*time.Second || !errors.Is(err, unicom360.ErrRateLimit) || !errors.Is(err, unicom360.ErrStatus) {
			t.Fatalf("got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------------------
// Redaction and logging
// ---------------------------------------------------------------------------------------

func TestClientNeverPrintsTheAPIKey(t *testing.T) {
	const key = "sk_live_should_never_print"
	c, err := unicom360.NewClient(unicom360.WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	slog.New(slog.NewTextHandler(&logged, nil)).Info("client", "c", c)
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("client", "c", c)
	for _, s := range []string{
		fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s", c, c, c, c), fmt.Sprintf("%+v %#v", *c, *c),
		fmt.Sprintf("%+v", c.SMS), logged.String(),
	} {
		if strings.Contains(s, key) {
			t.Fatalf("API key printed: %s", s)
		}
	}
	if !strings.Contains(c.String(), "[REDACTED]") {
		t.Fatalf("String() = %s", c.String())
	}
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), key) {
		t.Fatalf("API key in JSON: %s", b)
	}
}

func TestLogging(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){
		reply(429, `{"message":"RATE_LIMIT_EXCEEDED","success":false,"api_reference":"r-429"}`), reply(200, smsOK)}}
	c := newTestClient(t, ft, unicom360.WithLogger(logger), unicom360.WithSleeper(noSleep), unicom360.WithAPIKey("key-xyz"))
	_, err := c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "A", ToOne: "260971234567", Message: "secret text"})
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, m)
	}
	if len(lines) != 3 {
		t.Fatalf("%d log lines, want 3 (attempt, retry, attempt):\n%s", len(lines), buf.String())
	}
	first, retry, second := lines[0], lines[1], lines[2]
	if first["level"] != "DEBUG" || first["method"] != "POST" || first["path"] != "/api/v1/json/bulk/sms" ||
		first["status"] != float64(429) || first["api_reference"] != "r-429" || first["attempt"] != float64(1) {
		t.Errorf("first attempt line %v", first)
	}
	if _, ok := first["duration_ms"]; !ok {
		t.Errorf("no duration_ms in %v", first)
	}
	if retry["level"] != "INFO" || retry["attempt"] != float64(1) || retry["wait_s"] == nil || retry["cause"] != "rate_limit_error (HTTP 429)" {
		t.Errorf("retry line %v", retry)
	}
	if second["status"] != float64(200) || second["api_reference"] != "r-1" || second["attempt"] != float64(2) {
		t.Errorf("second attempt line %v", second)
	}
	for _, secret := range []string{"key-xyz", "260971234567", "secret text", "X-Api-Key"} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("log contains %q:\n%s", secret, buf.String())
		}
	}
}

// ---------------------------------------------------------------------------------------
// Sharing a client
// ---------------------------------------------------------------------------------------

func TestConcurrentUse(t *testing.T) {
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, smsOK)}}
	c := newTestClient(t, ft, unicom360.WithAutoReferenceIDs(true))
	recipients := []unicom360.TemplateRecipient{{To: "1"}, {To: "2"}}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "A", ToOne: "1", Message: "x"}); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			// Shares one recipients slice across goroutines: the SDK must not write to it.
			_, _ = c.WhatsApp.SendTemplate(context.Background(), unicom360.TemplateSendParams{Template: "t", Recipients: recipients})
		}()
	}
	wg.Wait()
	if ft.calls() != 64 {
		t.Fatalf("%d calls", ft.calls())
	}
}

func TestWithOptions(t *testing.T) {
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){reply(200, `{"status":"ok"}`)}}
	c := newTestClient(t, ft, unicom360.WithMaxRetries(5))
	c2, err := c.WithOptions(unicom360.WithBaseURL("https://proxy.example.com/x"))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != unicom360.DefaultBaseURL || c2.BaseURL() != "https://proxy.example.com/x" {
		t.Fatalf("%s / %s", c.BaseURL(), c2.BaseURL())
	}
	if _, err := c2.Health(context.Background()); err != nil || ft.calls() != 1 {
		t.Fatalf("the copy should share the transport: %v, %d calls", err, ft.calls())
	}
	if _, err := c.WithOptions(unicom360.WithAPIKey("")); !errors.Is(err, unicom360.ErrConfiguration) {
		t.Fatalf("got %v", err)
	}
}

// ---------------------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------------------

func TestDecimal(t *testing.T) {
	ft := &fakeTransport{replies: []func(*http.Request) (*http.Response, error){
		reply(200, `{"status":true,"message":"ok","balances":{"SMS":1250,"WHATSAPP":37.50,"BIG":12345678901234567890.123}}`)}}
	c := newTestClient(t, ft)
	b, err := c.Balance.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Balances["WHATSAPP"].String() != "37.50" || b.Balances["BIG"] != "12345678901234567890.123" {
		t.Fatalf("decimals not kept exactly: %v", b.Balances)
	}
	if f, err := b.Balances["WHATSAPP"].Float64(); err != nil || f != 37.5 {
		t.Fatalf("Float64 %v %v", f, err)
	}
	if n, err := b.Balances["SMS"].Int64(); err != nil || n != 1250 {
		t.Fatalf("Int64 %v %v", n, err)
	}
	out, _ := json.Marshal(b.Balances["WHATSAPP"])
	if string(out) != "37.50" {
		t.Fatalf("MarshalJSON %s", out)
	}
}

func TestVersionMatchesChangelog(t *testing.T) {
	b, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindSubmatch(b)
	if m == nil {
		t.Fatal("CHANGELOG.md has no release heading")
	}
	if string(m[1]) != unicom360.Version {
		t.Fatalf("CHANGELOG.md's newest release is %s, Version is %s", m[1], unicom360.Version)
	}
}
