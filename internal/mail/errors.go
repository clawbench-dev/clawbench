package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

// Kind classifies a failure so the HTTP layer can pick a localized message
// instead of showing a raw protocol string to the user.
type Kind string

const (
	KindAuth    Kind = "auth"
	KindConnect Kind = "connect"
	KindTLS     Kind = "tls"
	KindTimeout Kind = "timeout"
	KindServer  Kind = "server"
)

// Error is a classified mail failure.
type Error struct {
	Kind Kind
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e.Op == "" {
		return e.Err.Error()
	}
	return e.Op + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf reports the classification of err, or KindServer when it is not one of
// ours. Callers use it to choose an i18n key.
func KindOf(err error) Kind {
	var me *Error
	if errors.As(err, &me) {
		return me.Kind
	}
	return KindServer
}

// classify wraps err with a kind. It inspects both the Go error chain (TLS,
// network, timeouts) and the textual hint servers put in their rejection
// replies, because there is no protocol-neutral way to say "wrong password".
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	var me *Error
	if errors.As(err, &me) {
		return err
	}
	if kind, ok := classifyTransport(err); ok {
		return &Error{Kind: kind, Op: op, Err: err}
	}
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		if looksLikeAuth(string(imapErr.Code) + " " + imapErr.Text) {
			return &Error{Kind: KindAuth, Op: op, Err: err}
		}
		return &Error{Kind: KindServer, Op: op, Err: err}
	}
	if looksLikeAuth(err.Error()) {
		return &Error{Kind: KindAuth, Op: op, Err: err}
	}
	if isConnectFailure(err) {
		return &Error{Kind: KindConnect, Op: op, Err: err}
	}
	return &Error{Kind: KindServer, Op: op, Err: err}
}

// classifyTransport covers the failures that come from the connection itself
// rather than from the mail protocol.
func classifyTransport(err error) (Kind, bool) {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return KindTimeout, true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout, true
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return KindTLS, true
	}
	var unknownAuthority x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthority) {
		return KindTLS, true
	}
	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return KindTLS, true
	}
	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return KindTLS, true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return KindConnect, true
	}
	return "", false
}

func isConnectFailure(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection refused",
		"no such host",
		"network is unreachable",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func looksLikeAuth(s string) bool {
	s = strings.ToLower(s)
	for _, needle := range []string{
		"authentication failed",
		"authenticationfailed",
		"invalid credentials",
		"invalid login",
		"login failed",
		"auth failed",
		"535",
		"5.7.8",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// ErrClosed is returned when a command is attempted on a closed client.
var ErrClosed = errors.New("mail: client closed")

func timeoutError(op string, d time.Duration) error {
	return &Error{Kind: KindTimeout, Op: op, Err: fmt.Errorf("timed out after %s", d)}
}
