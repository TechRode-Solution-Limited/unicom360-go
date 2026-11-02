package unicom360

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"
)

// Defaults for the client options.
const (
	DefaultBaseURL        = "https://api.unicom360.com"
	DefaultTimeout        = 60 * time.Second
	DefaultConnectTimeout = 10 * time.Second
	DefaultMaxRetries     = 2
	DefaultMaxRetryWait   = 60 * time.Second
)

// Environment variables NewClient reads when the matching option is not given.
const (
	EnvAPIKey  = "UNICOM360_API_KEY"
	EnvBaseURL = "UNICOM360_BASE_URL"
)

// AppInfo names the application using the SDK. It is appended to the User-Agent header as
// "name/version (+url)".
type AppInfo struct {
	Name    string
	Version string
	URL     string
}

// Option configures a Client. Options are applied in order; a later option overrides an
// earlier one.
type Option func(*config) error

type config struct {
	apiKey         string
	apiKeySet      bool
	baseURL        string
	baseURLSet     bool
	timeout        time.Duration
	connectTimeout time.Duration
	maxRetries     int
	maxRetryWait   time.Duration
	autoRefs       bool
	appInfo        *AppInfo
	httpClient     *http.Client
	transport      http.RoundTripper
	logger         *slog.Logger
	clock          func() time.Time
	sleeper        func(context.Context, time.Duration) error
	random         func() float64

	// httpChanged records that an option affecting the HTTP client was applied, so
	// WithOptions builds a new one instead of sharing the original's.
	httpChanged bool
}

func defaultConfig() config {
	return config{
		timeout:        DefaultTimeout,
		connectTimeout: DefaultConnectTimeout,
		maxRetries:     DefaultMaxRetries,
		maxRetryWait:   DefaultMaxRetryWait,
		clock:          time.Now,
		sleeper:        sleepContext,
		random:         rand.Float64,
	}
}

// WithAPIKey sets the API key. Without it, NewClient reads UNICOM360_API_KEY. An explicit
// empty key is an error; it does not fall back to the environment.
func WithAPIKey(key string) Option {
	return func(c *config) error {
		c.apiKey, c.apiKeySet = key, true
		return nil
	}
}

// WithBaseURL sets the API's base URL. Without it, NewClient reads UNICOM360_BASE_URL, then
// uses DefaultBaseURL. The URL must be https, or http to localhost, 127.0.0.0/8 or ::1. A path
// prefix is kept (for a proxy); trailing slashes are removed.
func WithBaseURL(baseURL string) Option {
	return func(c *config) error {
		c.baseURL, c.baseURLSet = baseURL, true
		return nil
	}
}

// WithTimeout sets the time allowed for each attempt, from sending the request to receiving
// the whole response body. The default is 60 seconds.
func WithTimeout(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return configError("timeout must be positive")
		}
		c.timeout = d
		return nil
	}
}

// WithConnectTimeout sets the time allowed to establish a connection, TLS included. The
// default is 10 seconds. It applies to the SDK's own HTTP client only: with WithHTTPClient or
// WithTransport, configure the dialer yourself.
func WithConnectTimeout(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return configError("connect timeout must be positive")
		}
		c.connectTimeout, c.httpChanged = d, true
		return nil
	}
}

// WithMaxRetries sets how many times a failed attempt may be retried (so at most n+1
// attempts). The default is 2; 0 disables retries.
func WithMaxRetries(n int) Option {
	return func(c *config) error {
		if n < 0 {
			return configError("max retries must not be negative")
		}
		c.maxRetries = n
		return nil
	}
}

// WithMaxRetryWait caps any single wait between attempts. A rate-limit wait longer than this
// is not taken: the RateLimitError is returned instead. The default is 60 seconds.
func WithMaxRetryWait(d time.Duration) Option {
	return func(c *config) error {
		if d < 0 {
			return configError("max retry wait must not be negative")
		}
		c.maxRetryWait = d
		return nil
	}
}

// WithAutoReferenceIDs makes the client give every send without a user_reference_id a
// generated one ("u360-" and 32 hex characters), reused on each retry, so SMS, WhatsApp
// session and template sends become safe to retry after a timeout or a 5xx. A reference you
// set is never replaced. Off by default.
//
// Email is the exception: the gateway checks an email reference without a unique index, so a
// generated reference still refuses a later separate repeat, but email is never retried after
// a timeout or a 5xx.
func WithAutoReferenceIDs(on bool) Option {
	return func(c *config) error {
		c.autoRefs = on
		return nil
	}
}

// WithAppInfo appends your application's name, and optionally its version and URL, to the
// User-Agent header.
func WithAppInfo(info AppInfo) Option {
	return func(c *config) error {
		if info.Name == "" {
			return configError("app info needs a name")
		}
		c.appInfo = &info
		return nil
	}
}

// WithHTTPClient sends requests through hc, for example one with your proxy or instrumented
// transport. The SDK uses a shallow copy whose redirect policy is replaced: redirects are
// never followed, so the API key is never replayed to another host. Its other settings,
// Timeout included, are kept. A transport failure is classified from the error (net.OpError,
// net.DNSError, timeouts, or a *TransportError your transport returns).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) error {
		if hc == nil {
			return configError("http client must not be nil")
		}
		c.httpClient, c.httpChanged = hc, true
		return nil
	}
}

// WithTransport sends requests through rt (on the HTTP client given with WithHTTPClient, if
// any). A RoundTripper that knows whether a failed request reached the server can say so by
// returning a *TransportError.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *config) error {
		if rt == nil {
			return configError("transport must not be nil")
		}
		c.transport, c.httpChanged = rt, true
		return nil
	}
}

// WithLogger logs each attempt (debug) and each scheduled retry (info) to logger. Only the
// method, path, status, api_reference, attempt, duration and wait are logged: never bodies,
// headers, parameters, numbers, addresses or the API key. By default nothing is logged.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) error {
		c.logger = logger
		return nil
	}
}

// WithClock replaces the clock the client reads when it computes a rate-limit wait. It is a
// test seam.
func WithClock(now func() time.Time) Option {
	return func(c *config) error {
		if now == nil {
			return configError("clock must not be nil")
		}
		c.clock = now
		return nil
	}
}

// WithSleeper replaces how the client waits between attempts. sleep must return early with
// ctx's error when ctx is done. It is a test seam.
func WithSleeper(sleep func(ctx context.Context, d time.Duration) error) Option {
	return func(c *config) error {
		if sleep == nil {
			return configError("sleeper must not be nil")
		}
		c.sleeper = sleep
		return nil
	}
}

// WithRandom replaces the source of jitter, a function returning values in [0, 1). It is a
// test seam.
func WithRandom(random func() float64) Option {
	return func(c *config) error {
		if random == nil {
			return configError("random must not be nil")
		}
		c.random = random
		return nil
	}
}

// RequestOption overrides a client option for one call.
type RequestOption func(*requestConfig) error

type requestConfig struct {
	timeout    time.Duration
	maxRetries int
}

// WithRequestTimeout sets the per-attempt timeout for one call.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(r *requestConfig) error {
		if d <= 0 {
			return configError("request timeout must be positive")
		}
		r.timeout = d
		return nil
	}
}

// WithRequestMaxRetries sets the number of retries for one call.
func WithRequestMaxRetries(n int) RequestOption {
	return func(r *requestConfig) error {
		if n < 0 {
			return configError("request max retries must not be negative")
		}
		r.maxRetries = n
		return nil
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
