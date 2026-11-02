package unicom360

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// Client is a Unicom360 API client. Create one with NewClient and share it: a Client is
// immutable and safe for concurrent use. WithOptions returns a copy with changed options.
//
// The operations are grouped by channel: Client.SMS, Client.Email, Client.WhatsApp and
// Client.Balance, plus Client.Health.
type Client struct {
	// SMS sends SMS messages.
	SMS *SMSService
	// Email sends email.
	Email *EmailService
	// WhatsApp sends WhatsApp templates and session messages, and reads message status and
	// the template list.
	WhatsApp *WhatsAppService
	// Balance reads bundle balances.
	Balance *BalanceService

	// core is a pointer so that printing a Client with %v never prints the API key.
	core *core
}

type core struct {
	cfg       config
	http      *http.Client
	userAgent string
	// traceable is true when the innermost transport is net/http's, which reports through
	// httptrace whether a connection was made before a failure.
	traceable bool
}

// NewClient returns a client configured by opts. A missing API key, an invalid base URL or
// another configuration problem is returned as an *Error of type configuration_error
// (errors.Is(err, ErrConfiguration)); NewClient never panics.
//
//	client, err := unicom360.NewClient(unicom360.WithAPIKey(os.Getenv("UNICOM360_API_KEY")))
//
// With no options, the API key comes from UNICOM360_API_KEY and the base URL from
// UNICOM360_BASE_URL (or DefaultBaseURL).
func NewClient(opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	return build(cfg, nil, opts)
}

// WithOptions returns a copy of c with opts applied on top of c's options. The copy shares c's
// HTTP client (and connection pool) unless opts change it.
func (c *Client) WithOptions(opts ...Option) (*Client, error) {
	cfg := c.core.cfg
	cfg.httpChanged = false
	return build(cfg, c.core, opts)
}

func build(cfg config, from *core, opts []Option) (*Client, error) {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if !cfg.apiKeySet {
		if v, ok := os.LookupEnv(EnvAPIKey); ok {
			cfg.apiKey = v
		}
		cfg.apiKeySet = true
	}
	if strings.TrimSpace(cfg.apiKey) == "" {
		return nil, configError("no API key: pass WithAPIKey or set " + EnvAPIKey)
	}
	if strings.IndexFunc(cfg.apiKey, func(r rune) bool { return (r < 0x20 && r != '\t') || r == 0x7f }) >= 0 {
		return nil, configError("the API key contains a control character")
	}
	if !cfg.baseURLSet {
		cfg.baseURL = DefaultBaseURL
		if v, ok := os.LookupEnv(EnvBaseURL); ok && v != "" {
			cfg.baseURL = v
		}
		cfg.baseURLSet = true
	}
	base, err := normalizeBaseURL(cfg.baseURL)
	if err != nil {
		return nil, err
	}
	cfg.baseURL = base

	k := &core{cfg: cfg, userAgent: userAgent(cfg.appInfo)}
	if from != nil && !cfg.httpChanged {
		k.http, k.traceable = from.http, from.traceable
	} else {
		k.http, k.traceable = newHTTPClient(cfg)
	}
	k.cfg.httpChanged = false

	c := &Client{core: k}
	c.SMS = &SMSService{client: c}
	c.Email = &EmailService{client: c}
	c.WhatsApp = &WhatsAppService{client: c}
	c.Balance = &BalanceService{client: c}
	return c, nil
}

// normalizeBaseURL applies the base URL rules: https, or http to a loopback host; no query,
// fragment or user info; trailing slashes removed.
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
		return "", configError(fmt.Sprintf("base URL %q is not an absolute URL with a host", raw))
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", configError(fmt.Sprintf("base URL %q must not have a query string or fragment", raw))
	}
	if u.User != nil {
		return "", configError("base URL must not contain user info: the API key is the only credential sent")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return "", configError(fmt.Sprintf("base URL %q uses plain http: use https (http is allowed only for localhost, 127.0.0.0/8 and ::1)", raw))
		}
	default:
		return "", configError(fmt.Sprintf("base URL %q must use https", raw))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String(), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	if addr.Is4() {
		return addr.As4()[0] == 127
	}
	return addr == netip.IPv6Loopback()
}

func userAgent(app *AppInfo) string {
	goVersion := strings.TrimPrefix(runtime.Version(), "go")
	ua := fmt.Sprintf("unicom360-go/%s (go %s; %s; %s)", Version, goVersion, runtime.GOOS, runtime.GOARCH)
	if app != nil {
		ua += " " + app.Name
		if app.Version != "" {
			ua += "/" + app.Version
		}
		if app.URL != "" {
			ua += " (+" + app.URL + ")"
		}
	}
	return ua
}

// newHTTPClient builds the client the SDK sends with: the caller's (copied) or its own, never
// following redirects.
func newHTTPClient(cfg config) (*http.Client, bool) {
	var hc http.Client
	if cfg.httpClient != nil {
		hc = *cfg.httpClient
	}
	switch {
	case cfg.transport != nil:
		hc.Transport = cfg.transport
	case cfg.httpClient == nil:
		hc.Transport = defaultTransport(cfg.connectTimeout)
	case hc.Transport == nil:
		// The caller's client relies on net/http's default transport: keep using it.
		hc.Transport = http.DefaultTransport
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	_, traceable := hc.Transport.(*http.Transport)
	return &hc, traceable
}

func defaultTransport(connectTimeout time.Duration) *http.Transport {
	var tr *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = dt.Clone()
	} else {
		tr = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
	tr.DialContext = dialer.DialContext
	tr.TLSHandshakeTimeout = connectTimeout
	return tr
}

// BaseURL returns the base URL the client sends to.
func (c *Client) BaseURL() string { return c.core.cfg.baseURL }

// UserAgent returns the User-Agent header the client sends.
func (c *Client) UserAgent() string { return c.core.userAgent }

// String describes the client with its API key redacted.
func (c *Client) String() string {
	if c == nil || c.core == nil {
		return "unicom360.Client(nil)"
	}
	cfg := c.core.cfg
	return fmt.Sprintf("unicom360.Client{base_url: %s, api_key: [REDACTED], timeout: %s, max_retries: %d, auto_reference_ids: %t}",
		cfg.baseURL, cfg.timeout, cfg.maxRetries, cfg.autoRefs)
}

// GoString implements fmt.GoStringer, so %#v redacts the API key too.
func (c *Client) GoString() string { return c.String() }

// LogValue implements slog.LogValuer, so a logged client never shows its API key.
func (c *Client) LogValue() slog.Value {
	if c == nil || c.core == nil {
		return slog.StringValue("unicom360.Client(nil)")
	}
	return slog.GroupValue(
		slog.String("base_url", c.core.cfg.baseURL),
		slog.String("api_key", "[REDACTED]"),
	)
}
