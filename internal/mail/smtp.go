package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// opSend is the operation label used in classified send errors.
const opSend = "send"

// SMTPSender sends mail through the account's SMTP server.
type SMTPSender struct{}

// Send implements Sender. It returns the exact RFC 5322 bytes so the caller can
// APPEND the same message to the Sent folder.
func (SMTPSender) Send(ctx context.Context, acc Account, req SendRequest) ([]byte, error) {
	msg, err := buildOutgoingMessage(req)
	if err != nil {
		return nil, err
	}
	client, err := newSMTPClient(acc)
	if err != nil {
		return nil, classify("dial smtp", err)
	}
	if err := dialAndSend(ctx, client, msg); err != nil {
		return nil, err
	}

	var raw bytes.Buffer
	if _, err := msg.WriteTo(&raw); err != nil {
		return nil, classify("serialize message", err)
	}
	return raw.Bytes(), nil
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

func newSMTPClient(acc Account) (*gomail.Client, error) {
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
		opts = append(opts, gomail.WithSSLPort(false), gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover))
	case SecurityStartTLS:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory), gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover))
	default:
		// AutoDiscover deliberately refuses PLAIN/LOGIN on an unencrypted
		// connection, which would make a plaintext localhost relay unusable.
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS), gomail.WithSMTPAuth(gomail.SMTPAuthPlainNoEnc))
	}
	return gomail.NewClient(acc.SMTPHost, opts...)
}

// dialAndSend bounds the exchange: go-mail has no context support, so a dead
// server would otherwise pin the request goroutine forever.
func dialAndSend(ctx context.Context, client *gomail.Client, msg *gomail.Msg) error {
	done := make(chan error, 1)
	go func() { done <- client.DialAndSend(msg) }()
	select {
	case err := <-done:
		if err != nil {
			return classify(opSend, err)
		}
		return nil
	case <-ctx.Done():
		_ = client.Close()
		return classify(opSend, ctx.Err())
	case <-time.After(opTimeout):
		_ = client.Close()
		return timeoutError(opSend, opTimeout)
	}
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
