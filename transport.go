package unicom360

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
)

// TransportError is a transport failure that states whether the request may have reached the
// server. The SDK uses it to decide whether a failed send is safe to retry.
//
// A custom http.RoundTripper can return a *TransportError to state the facts itself. Without
// one, the SDK classifies the error: a DNS failure, a refused connection, a TLS handshake
// failure or a failure before net/http obtained a connection is Sent: false; anything else,
// including every failure it cannot classify, is Sent: true.
type TransportError struct {
	// Sent is whether any byte of the request may have reached the server.
	Sent bool
	// Timeout is whether the failure was a timeout.
	Timeout bool
	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *TransportError) Error() string {
	msg := "unicom360: transport failure"
	switch {
	case e.Timeout && e.Sent:
		msg = "unicom360: timed out after the request was sent"
	case e.Timeout:
		msg = "unicom360: timed out before the request was sent"
	case e.Sent:
		msg = "unicom360: connection failed after the request was sent"
	default:
		msg = "unicom360: connection failed before the request was sent"
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying error.
func (e *TransportError) Unwrap() error { return e.Err }

// classifyTransportError works out whether a failed attempt may have reached the server and
// whether it was a timeout. connected is true once net/http reported a connection (or a
// response arrived); traceable is true when that report can be trusted.
func classifyTransportError(err error, connected, traceable bool) (sent, timeout bool) {
	var te *TransportError
	if errors.As(err, &te) {
		return te.Sent, te.Timeout
	}
	var ne net.Error
	timeout = (errors.As(err, &ne) && ne.Timeout()) || errors.Is(err, context.DeadlineExceeded)
	switch {
	case connected:
		sent = true
	case failedBeforeSending(err):
		sent = false
	case traceable:
		sent = false
	default:
		sent = true
	}
	return sent, timeout
}

// failedBeforeSending recognises errors that can only happen before a request is written.
func failedBeforeSending(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && (opErr.Op == "dial" || opErr.Op == "proxyconnect") {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	var certErr *tls.CertificateVerificationError
	var recordErr tls.RecordHeaderError
	var authorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidErr x509.CertificateInvalidError
	return errors.As(err, &certErr) || errors.As(err, &recordErr) || errors.As(err, &authorityErr) ||
		errors.As(err, &hostnameErr) || errors.As(err, &invalidErr)
}
