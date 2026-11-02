package unicom360

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Error type strings, the values of Error.Type. Every API error class shares one type with its
// subclasses: an InsufficientBundleError has Type TypeAPIError and Code CodeInsufficientBundle.
const (
	TypeConfigurationError         = "configuration_error"
	TypeConnectionError            = "connection_error"
	TypeTimeoutError               = "timeout_error"
	TypeAPIStatusError             = "api_status_error"
	TypeAuthenticationError        = "authentication_error"
	TypeRateLimitError             = "rate_limit_error"
	TypeInvalidRequestError        = "invalid_request_error"
	TypeAPIServerError             = "api_server_error"
	TypeAPIError                   = "api_error"
	TypeSignatureVerificationError = "signature_verification_error"
	// TypeUnicom360Error is the base type, used for a webhook or callback body that is not
	// the JSON object it should be.
	TypeUnicom360Error = "unicom360_error"
)

// Sentinel errors for errors.Is. An *Error matches the sentinel of its own class and of every
// ancestor class: a timeout matches ErrTimeout and ErrConnection, an insufficient bundle matches
// ErrInsufficientBundle and ErrAPI, a 429 matches ErrRateLimit and ErrStatus.
var (
	ErrConfiguration         = errors.New("unicom360: configuration error")
	ErrConnection            = errors.New("unicom360: connection error")
	ErrTimeout               = errors.New("unicom360: timeout")
	ErrStatus                = errors.New("unicom360: unexpected HTTP response")
	ErrAuthentication        = errors.New("unicom360: authentication failed")
	ErrRateLimit             = errors.New("unicom360: rate limit exceeded")
	ErrInvalidRequest        = errors.New("unicom360: invalid request")
	ErrServer                = errors.New("unicom360: server error")
	ErrAPI                   = errors.New("unicom360: request refused")
	ErrInsufficientBundle    = errors.New("unicom360: insufficient bundle")
	ErrDuplicateReference    = errors.New("unicom360: duplicate reference")
	ErrNoValidRecipients     = errors.New("unicom360: no valid recipients")
	ErrSessionWindowClosed   = errors.New("unicom360: session window closed")
	ErrSignatureVerification = errors.New("unicom360: signature verification failed")
)

// Error is the one error type the SDK returns. Type and Class say what kind of error it is;
// use errors.Is with the Err* sentinels, or errors.As to read the fields:
//
//	var apiErr *unicom360.Error
//	if errors.As(err, &apiErr) {
//		log.Printf("%s %s (api_reference %s)", apiErr.Class(), apiErr.Code, apiErr.APIReference)
//	}
//
// Class names follow the cross-language hierarchy:
//
//	Unicom360Error
//	  ConfigurationError
//	  APIConnectionError
//	    APITimeoutError
//	  APIStatusError                 any non-2xx, and a 2xx without a usable envelope
//	    AuthenticationError          401
//	    RateLimitError               429
//	    InvalidRequestError          400
//	    APIServerError               5xx
//	  APIError                       2xx with success: false (status: false on balances)
//	    InsufficientBundleError
//	    DuplicateReferenceError
//	    NoValidRecipientsError
//	    SessionWindowClosedError
//	  SignatureVerificationError
//
// A DuplicateReferenceError with Attempts > 1 after a send means an earlier attempt of the same
// call very probably went through: the retry was refused because its user_reference_id had
// already been used. Treat the message as sent.
//
// An Error never contains the API key or a webhook secret.
type Error struct {
	// Type is one of the Type* constants.
	Type string
	// Code is the normalised code (see NormalizeCode) for API errors, CodeUnknown when the
	// response carried none, and "" for errors that did not come from a response.
	Code string
	// Message is human-readable text: the gateway's message for API errors.
	Message string
	// HTTPStatus is the response's status code, or 0 when there was no response.
	HTTPStatus int
	// APIReference is the gateway's reference for the request, when the response had one.
	// Quote it to support.
	APIReference string
	// RawBody is the response body text.
	RawBody string
	// Data is the envelope's data as raw JSON, or nil.
	Data json.RawMessage
	// Headers are the response headers.
	Headers http.Header
	// Attempts is the number of HTTP attempts the call made, retries included.
	Attempts int
	// RetryAfter is set on a RateLimitError: the time until the next UTC minute, when the
	// gateway's fixed one-minute window resets.
	RetryAfter time.Duration
	// Reason is set on a SignatureVerificationError: one of the webhook package's Reason*
	// constants.
	Reason string
	// OriginalMessageID is set on a DuplicateReferenceError from a WhatsApp session send: the
	// message_id the reference was first sent as (data.message_id), when the gateway gave it.
	OriginalMessageID string
	// Results is set on a NoValidRecipientsError: each recipient's result and reason
	// (data.results).
	Results []TemplateResult
	// Err is the underlying cause, for connection and timeout errors. Unwrap returns it.
	Err error
}

var apiErrorClasses = map[string]string{
	CodeInsufficientBundle:  "InsufficientBundleError",
	CodeDuplicateReference:  "DuplicateReferenceError",
	CodeNoValidRecipients:   "NoValidRecipientsError",
	CodeSessionWindowClosed: "SessionWindowClosedError",
}

var typeClasses = map[string]string{
	TypeConfigurationError:         "ConfigurationError",
	TypeConnectionError:            "APIConnectionError",
	TypeTimeoutError:               "APITimeoutError",
	TypeAPIStatusError:             "APIStatusError",
	TypeAuthenticationError:        "AuthenticationError",
	TypeRateLimitError:             "RateLimitError",
	TypeInvalidRequestError:        "InvalidRequestError",
	TypeAPIServerError:             "APIServerError",
	TypeSignatureVerificationError: "SignatureVerificationError",
}

var classSentinels = map[string][]error{
	"ConfigurationError":         {ErrConfiguration},
	"APIConnectionError":         {ErrConnection},
	"APITimeoutError":            {ErrTimeout, ErrConnection},
	"APIStatusError":             {ErrStatus},
	"AuthenticationError":        {ErrAuthentication, ErrStatus},
	"RateLimitError":             {ErrRateLimit, ErrStatus},
	"InvalidRequestError":        {ErrInvalidRequest, ErrStatus},
	"APIServerError":             {ErrServer, ErrStatus},
	"APIError":                   {ErrAPI},
	"InsufficientBundleError":    {ErrInsufficientBundle, ErrAPI},
	"DuplicateReferenceError":    {ErrDuplicateReference, ErrAPI},
	"NoValidRecipientsError":     {ErrNoValidRecipients, ErrAPI},
	"SessionWindowClosedError":   {ErrSessionWindowClosed, ErrAPI},
	"SignatureVerificationError": {ErrSignatureVerification},
}

// Class returns the error's canonical class name, the most specific one in the hierarchy
// (for example "InsufficientBundleError", "RateLimitError" or "APITimeoutError").
func (e *Error) Class() string {
	if e.Type == TypeAPIError {
		if c, ok := apiErrorClasses[e.Code]; ok {
			return c
		}
		return "APIError"
	}
	if c, ok := typeClasses[e.Type]; ok {
		return c
	}
	return "Unicom360Error"
}

// Error implements the error interface.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("unicom360: ")
	b.WriteString(e.Class())
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	var details []string
	if e.Code != "" && e.Code != e.Message {
		details = append(details, "code "+e.Code)
	}
	if e.Reason != "" && e.Reason != e.Message {
		details = append(details, "reason "+e.Reason)
	}
	if e.HTTPStatus != 0 {
		details = append(details, "HTTP "+strconv.Itoa(e.HTTPStatus))
	}
	if e.APIReference != "" {
		details = append(details, "api_reference "+e.APIReference)
	}
	if e.RetryAfter > 0 {
		details = append(details, "retry after "+e.RetryAfter.String())
	}
	if e.Attempts > 1 {
		details = append(details, strconv.Itoa(e.Attempts)+" attempts")
	}
	if len(details) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(details, ", "))
		b.WriteString(")")
	}
	return b.String()
}

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Err }

// Is reports whether target is the sentinel of e's class or of one of its ancestors.
func (e *Error) Is(target error) bool {
	for _, s := range classSentinels[e.Class()] {
		if target == s {
			return true
		}
	}
	return false
}

func configError(format string) *Error {
	return &Error{Type: TypeConfigurationError, Message: format}
}
