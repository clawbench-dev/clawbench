package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gomail "github.com/wneessen/go-mail"
	smtpsend "github.com/wneessen/go-mail/smtp"
)

// opSend is the operation label used in classified send errors.
const opSend = "send"

// SMTPSender sends mail through the account's SMTP server.
type SMTPSender struct{}

// Send implements Sender. It returns the exact RFC 5322 bytes so the caller can
// APPEND the same message to the Sent folder.
func (SMTPSender) Send(ctx context.Context, acc Account, req SendRequest) ([]byte, error) {
	// Internationalized domains become punycode before anything is built, so
	// the envelope and the headers agree and only a non-ASCII local part is
	// left as an SMTPUTF8 requirement.
	req = normalizeAddresses(req)
	msg, err := buildOutgoingMessage(req)
	if err != nil {
		return nil, err
	}

	var sendErr error
	envelope := envelopeAddresses(req)
	for _, auth := range smtpAuthSequence(acc.SMTPSecurity) {
		client, cerr := newSMTPClient(acc, auth)
		if cerr != nil {
			return nil, classify("dial smtp", cerr)
		}
		sendErr = dialAndSend(ctx, client, msg, envelope)
		if sendErr == nil {
			break
		}
		if !retryableAuthFailure(sendErr) || ctx.Err() != nil {
			return nil, sendErr
		}
	}
	if sendErr != nil {
		return nil, sendErr
	}

	var raw bytes.Buffer
	if _, err := msg.WriteTo(&raw); err != nil {
		return nil, classify("serialize message", err)
	}
	return raw.Bytes(), nil
}

// smtpAuthSequence lists the AUTH mechanisms to try, in order.
//
// On an encrypted connection AutoDiscover picks the strongest mechanism the
// server advertises. It deliberately refuses PLAIN and LOGIN over plaintext, so
// those have to be named explicitly — and naming exactly one is not enough:
// servers exist that advertise PLAIN but reject every attempt with 535 while
// LOGIN works, and go-mail does not fall back on its own. LOGIN goes first
// because that is the mechanism such servers accept; a server that only
// advertises PLAIN rejects LOGIN before any credentials are sent, so the retry
// costs a connection setup and nothing more.
func smtpAuthSequence(sec Security) []gomail.SMTPAuthType {
	if sec != SecurityNone {
		return []gomail.SMTPAuthType{gomail.SMTPAuthAutoDiscover}
	}
	return []gomail.SMTPAuthType{gomail.SMTPAuthLoginNoEnc, gomail.SMTPAuthPlainNoEnc}
}

// retryableAuthFailure reports whether err came from the AUTH step, i.e.
// whether trying another mechanism is both safe and meaningful.
//
// A failure after DATA may have delivered the message already, so retrying it
// would send a duplicate; only the AUTH step is eligible. go-mail prefixes that
// step with a fixed string, and the two "mechanism not advertised" errors are
// returned before any credential is sent.
func retryableAuthFailure(err error) bool {
	if errors.Is(err, gomail.ErrLoginAuthNotSupported) || errors.Is(err, gomail.ErrPlainAuthNotSupported) {
		return true
	}
	return strings.Contains(err.Error(), "SMTP AUTH failed")
}

func buildOutgoingMessage(req SendRequest) (*gomail.Msg, error) {
	msg := gomail.NewMsg(gomail.WithNoDefaultUserAgent())
	if err := msg.From(req.From); err != nil {
		return nil, &Error{Kind: KindServer, Op: opSend, Err: fmt.Errorf("invalid sender address: %w", err)}
	}
	if err := msg.To(req.To...); err != nil {
		return nil, &Error{Kind: KindServer, Op: opSend, Err: fmt.Errorf("invalid recipient address: %w", err)}
	}
	if len(req.Cc) > 0 {
		if err := msg.Cc(req.Cc...); err != nil {
			return nil, &Error{Kind: KindServer, Op: opSend, Err: fmt.Errorf("invalid cc address: %w", err)}
		}
	}
	if len(req.Bcc) > 0 {
		if err := msg.Bcc(req.Bcc...); err != nil {
			return nil, &Error{Kind: KindServer, Op: opSend, Err: fmt.Errorf("invalid bcc address: %w", err)}
		}
	}
	msg.Subject(req.Subject)
	setThreadHeaders(msg, req)

	text := req.Text
	if text == "" && req.HTML != "" {
		text = plainFromHTML(req.HTML)
	}
	msg.SetBodyString(gomail.TypeTextPlain, text)
	if req.HTML != "" {
		msg.AddAlternativeString(gomail.TypeTextHTML, req.HTML)
	}

	for _, a := range req.Attachments {
		opts := []gomail.FileOption{}
		if a.ContentType != "" {
			opts = append(opts, gomail.WithFileContentType(gomail.ContentType(a.ContentType)))
		}
		if err := msg.AttachReader(a.Filename, bytes.NewReader(a.Data), opts...); err != nil {
			return nil, &Error{Kind: KindServer, Op: opSend, Err: fmt.Errorf("attach %s: %w", a.Filename, err)}
		}
	}
	return msg, nil
}

func setThreadHeaders(msg *gomail.Msg, req SendRequest) {
	if req.InReplyTo != "" {
		msg.SetGenHeader(gomail.HeaderInReplyTo, ensureAngle(req.InReplyTo))
	}
	if len(req.References) == 0 {
		return
	}
	refs := make([]string, 0, len(req.References))
	for _, r := range req.References {
		refs = append(refs, ensureAngle(r))
	}
	msg.SetGenHeader(gomail.HeaderReferences, strings.Join(refs, " "))
}

func newSMTPClient(acc Account, auth gomail.SMTPAuthType) (*gomail.Client, error) {
	opts := []gomail.Option{
		gomail.WithPort(acc.SMTPPort),
		gomail.WithTimeout(dialTimeout),
		gomail.WithUsername(acc.Username),
		gomail.WithPassword(acc.Password),
		gomail.WithTLSConfig(&tls.Config{
			ServerName: acc.SMTPHost,
			MinVersion: tls.VersionTLS12,
			// Certificate verification is opt-in per account (self-hosted
			// servers with a self-signed certificate).
			InsecureSkipVerify: acc.InsecureTLS,
		}),
	}
	switch acc.SMTPSecurity {
	case SecurityTLS:
		opts = append(opts, gomail.WithSSLPort(false))
	case SecurityStartTLS:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	default:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	opts = append(opts, gomail.WithSMTPAuth(auth))
	return gomail.NewClient(acc.SMTPHost, opts...)
}

// dialAndSend bounds the exchange: go-mail has no context support, so a dead
// server would otherwise pin the request goroutine forever.
//
// The connection is opened separately from the send so the server's advertised
// extensions can be read before the envelope is written (see
// ensureEnvelopeEncodable).
func dialAndSend(ctx context.Context, client *gomail.Client, msg *gomail.Msg, envelope []string) error {
	done := make(chan error, 1)
	var conn smtpConn

	go func() {
		c, err := client.DialToSMTPClientWithContext(ctx)
		if err != nil {
			done <- fmt.Errorf("dial failed: %w", err)
			return
		}
		conn.set(c)
		done <- exchange(client, c, msg, envelope)
	}()

	select {
	case err := <-done:
		if err != nil {
			return classify(opSend, err)
		}
		return nil
	case <-ctx.Done():
		conn.close(client)
		return classify(opSend, ctx.Err())
	case <-time.After(opTimeout):
		conn.close(client)
		return timeoutError(opSend, opTimeout)
	}
}

// smtpConn hands the open connection from the worker goroutine to the timeout
// path without racing it. A timeout can fire before the dial finishes, in which
// case there is nothing to close — the dial itself is bounded by its own
// timeout and closes what it opened on failure.
type smtpConn struct {
	mu sync.Mutex
	c  *smtpsend.Client
}

func (s *smtpConn) set(c *smtpsend.Client) {
	s.mu.Lock()
	s.c = c
	s.mu.Unlock()
}

func (s *smtpConn) close(client *gomail.Client) {
	s.mu.Lock()
	c := s.c
	s.mu.Unlock()
	if c != nil {
		_ = client.CloseWithSMTPClient(c)
	}
}

// exchange runs the rest of the SMTP dialog on an open connection.
func exchange(client *gomail.Client, conn *smtpsend.Client, msg *gomail.Msg, envelope []string) error {
	defer func() { _ = client.CloseWithSMTPClient(conn) }()
	if err := ensureEnvelopeEncodable(conn, envelope); err != nil {
		return err
	}
	if err := client.SendWithSMTPClient(conn, msg); err != nil {
		return fmt.Errorf("send failed: %w", err)
	}
	return nil
}

// ensureEnvelopeEncodable refuses a message the server cannot represent.
//
// SMTPUTF8 (RFC 6531) is what allows a non-ASCII address in MAIL FROM / RCPT TO.
// Without it the address goes out as raw UTF-8 bytes, which the server either
// rejects with a 5xx that says nothing about encoding, or accepts after
// mangling it into a different mailbox. Both are worse than a clear refusal,
// and both only surface after the message body has already been transmitted.
func ensureEnvelopeEncodable(conn *smtpsend.Client, addresses []string) error {
	if ok, _ := conn.Extension("SMTPUTF8"); ok {
		return nil
	}
	for _, addr := range addresses {
		if isASCII(addr) {
			continue
		}
		return &Error{Kind: KindUnsupported, Op: opSend, Err: fmt.Errorf(
			"the server does not support non-ASCII addresses (no SMTPUTF8): %s", addr)}
	}
	return nil
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// envelopeAddresses lists every address that ends up in MAIL FROM or RCPT TO.
func envelopeAddresses(req SendRequest) []string {
	out := make([]string, 0, 1+len(req.To)+len(req.Cc)+len(req.Bcc))
	out = append(out, req.From)
	out = append(out, req.To...)
	out = append(out, req.Cc...)
	out = append(out, req.Bcc...)
	return out
}

// ensureAngle wraps a Message-ID in angle brackets when the server stored it
// without them (go-imap strips them from ENVELOPE).
func ensureAngle(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") {
		return id
	}
	return "<" + id + ">"
}
