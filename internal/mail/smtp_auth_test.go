package mail

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// authBackend is an SMTP server whose AUTH mechanisms can be made to fail
// individually. That is the situation on servers which advertise PLAIN but
// reject every attempt with 535 while LOGIN works — go-mail does not fall back
// on its own, so the client has to.
type authBackend struct {
	mechs      []string
	plainWorks bool
	loginWorks bool
	// enableSMTPUTF8 makes the server advertise SMTPUTF8, i.e. it can carry
	// non-ASCII addresses.
	enableSMTPUTF8 bool
	// messages collects the bodies that reached DATA.
	messages chan []byte

	mu    sync.Mutex
	rcpts []string
}

func (b *authBackend) recordRcpt(to string) {
	b.mu.Lock()
	b.rcpts = append(b.rcpts, to)
	b.mu.Unlock()
}

func (b *authBackend) recipients() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.rcpts...)
}

func (b *authBackend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &authSession{backend: b}, nil
}

type authSession struct{ backend *authBackend }

func (s *authSession) Mail(string, *smtp.MailOptions) error { return nil }
func (s *authSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.backend.recordRcpt(to)
	return nil
}
func (s *authSession) Reset()        {}
func (s *authSession) Logout() error { return nil }
func (s *authSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if s.backend.messages != nil {
		s.backend.messages <- raw
	}
	return nil
}

func (s *authSession) AuthMechanisms() []string { return s.backend.mechs }

func (s *authSession) Auth(mech string) (sasl.Server, error) {
	switch strings.ToUpper(mech) {
	case "PLAIN":
		return sasl.NewPlainServer(func(_, _, _ string) error {
			if !s.backend.plainWorks {
				return smtp.ErrAuthFailed
			}
			return nil
		}), nil
	case "LOGIN":
		return &loginServer{works: s.backend.loginWorks}, nil
	}
	return nil, smtp.ErrAuthFailed
}

// loginServer implements the server half of the LOGIN mechanism, which go-sasl
// only ships a client for.
type loginServer struct {
	works   bool
	gotUser bool
}

func (l *loginServer) Next(response []byte) ([]byte, bool, error) {
	if !l.gotUser {
		l.gotUser = true
		return []byte("Password:"), false, nil
	}
	if !l.works {
		return nil, true, smtp.ErrAuthFailed
	}
	return nil, true, nil
}

func startAuthSMTP(t *testing.T, backend *authBackend) Account {
	t.Helper()
	srv := smtp.NewServer(backend)
	srv.Domain = "example.com"
	srv.AllowInsecureAuth = true
	srv.EnableSMTPUTF8 = backend.enableSMTPUTF8

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	addr := ln.Addr().(*net.TCPAddr)
	return Account{
		Email:        "tester@example.com",
		Username:     "tester",
		Password:     "secret",
		SMTPHost:     "127.0.0.1",
		SMTPPort:     addr.Port,
		SMTPSecurity: SecurityNone,
	}
}

// The reported failure: the server advertises AUTH PLAIN LOGIN, PLAIN answers
// 535 every time, LOGIN works. Naming PLAIN explicitly (the old behavior) made
// every send fail.
func TestSMTPFallsBackToLoginWhenPlainIsRejected(t *testing.T) {
	acc := startAuthSMTP(t, &authBackend{
		mechs:      []string{sasl.Plain, sasl.Login},
		plainWorks: false,
		loginWorks: true,
	})

	if _, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"bob@example.com"},
		Text: "x",
	}); err != nil {
		t.Fatalf("LOGIN should have been tried after PLAIN was rejected: %v", err)
	}
}

// The mirror case: a server that only offers PLAIN rejects LOGIN before any
// credential is sent, and the send must still go through.
func TestSMTPFallsBackToPlainWhenLoginIsNotAdvertised(t *testing.T) {
	acc := startAuthSMTP(t, &authBackend{
		mechs:      []string{sasl.Plain},
		plainWorks: true,
	})

	if _, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"bob@example.com"},
		Text: "x",
	}); err != nil {
		t.Fatalf("PLAIN should have been tried after LOGIN was not advertised: %v", err)
	}
}

// When every mechanism fails the caller still gets a classified auth error, not
// a generic one — the UI picks its message from that.
func TestSMTPAllMechanismsRejectedIsAuthError(t *testing.T) {
	acc := startAuthSMTP(t, &authBackend{
		mechs: []string{sasl.Plain, sasl.Login},
	})

	_, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"bob@example.com"},
		Text: "x",
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if KindOf(err) != KindAuth {
		t.Fatalf("expected auth kind, got %v (%v)", KindOf(err), err)
	}
}

// --- SMTPUTF8 ---

// Without SMTPUTF8 a non-ASCII address goes out as raw UTF-8 bytes, which the
// server either rejects with an unrelated 5xx or accepts as a different
// mailbox. Refusing up front is the only honest answer.
func TestSMTPRejectsNonASCIIAddressWithoutSMTPUTF8(t *testing.T) {
	backend := &authBackend{
		mechs:      []string{sasl.Plain},
		plainWorks: true,
		messages:   make(chan []byte, 1),
	}
	acc := startAuthSMTP(t, backend)

	_, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"用户@example.com"},
		Text: "x",
	})
	if err == nil {
		t.Fatal("a non-ASCII recipient on a server without SMTPUTF8 must be refused")
	}
	if KindOf(err) != KindUnsupported {
		t.Fatalf("expected the unsupported kind, got %v (%v)", KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "用户@example.com") {
		t.Fatalf("the error should name the address: %v", err)
	}
	select {
	case raw := <-backend.messages:
		t.Fatalf("nothing should have been transmitted, got %q", raw)
	default:
	}
}

// The check must not fire when the server does support it — a Chinese address
// is then perfectly deliverable.
func TestSMTPAllowsNonASCIIAddressWithSMTPUTF8(t *testing.T) {
	backend := &authBackend{
		mechs:          []string{sasl.Plain},
		plainWorks:     true,
		enableSMTPUTF8: true,
		messages:       make(chan []byte, 1),
	}
	acc := startAuthSMTP(t, backend)

	if _, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"用户@example.com"},
		Text: "x",
	}); err != nil {
		t.Fatalf("SMTPUTF8 is advertised, the send should go through: %v", err)
	}
	select {
	case <-backend.messages:
	default:
		t.Fatal("the server received no message")
	}
}

// The check is about addresses only: a Chinese subject or body must not be
// mistaken for an unencodable envelope.
func TestSMTPNonASCIIBodyIsNotAnEnvelopeProblem(t *testing.T) {
	backend := &authBackend{
		mechs:      []string{sasl.Plain},
		plainWorks: true,
		messages:   make(chan []byte, 1),
	}
	acc := startAuthSMTP(t, backend)

	if _, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From:    "tester@example.com",
		To:      []string{"bob@example.com"},
		Subject: "周报：本周进展",
		Text:    "正文里有中文，还有 emoji 🙂",
	}); err != nil {
		t.Fatalf("only the envelope matters, got %v", err)
	}
}

// An internationalized domain is a DNS name, and DNS only speaks punycode. It
// must be converted rather than treated as an SMTPUTF8 problem, so that a
// server without SMTPUTF8 can still deliver to it.
func TestSMTPConvertsIDNRecipientToPunycode(t *testing.T) {
	backend := &authBackend{
		mechs:      []string{sasl.Plain},
		plainWorks: true,
		messages:   make(chan []byte, 1),
	}
	acc := startAuthSMTP(t, backend)

	raw, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"bob@例子.com"},
		Text: "x",
	})
	if err != nil {
		t.Fatalf("an IDN domain should be converted, not refused: %v", err)
	}

	rcpts := backend.recipients()
	if len(rcpts) != 1 || rcpts[0] != "bob@xn--fsqu00a.com" {
		t.Fatalf("RCPT TO = %v, want the punycode form", rcpts)
	}
	// The headers have to say the same thing as the envelope, otherwise the
	// recipient sees a different address from the one that was delivered to.
	if !strings.Contains(string(raw), "xn--fsqu00a.com") {
		t.Fatalf("the message header still carries the raw IDN: %s", raw)
	}
}

// Punycode only fixes the domain. A non-ASCII local part still needs SMTPUTF8.
func TestSMTPRejectsNonASCIILocalPartOnIDNDomain(t *testing.T) {
	backend := &authBackend{
		mechs:      []string{sasl.Plain},
		plainWorks: true,
		messages:   make(chan []byte, 1),
	}
	acc := startAuthSMTP(t, backend)

	_, err := (SMTPSender{}).Send(context.Background(), acc, SendRequest{
		From: "tester@example.com",
		To:   []string{"用户@例子.com"},
		Text: "x",
	})
	if err == nil {
		t.Fatal("a non-ASCII local part cannot be encoded without SMTPUTF8")
	}
	if KindOf(err) != KindUnsupported {
		t.Fatalf("expected the unsupported kind, got %v (%v)", KindOf(err), err)
	}
	if got := backend.recipients(); len(got) != 0 {
		t.Fatalf("nothing should have been addressed, got %v", got)
	}
}

func TestNormalizeAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"bob@example.com", "bob@example.com"},
		{"bob@例子.com", "bob@xn--fsqu00a.com"},
		{"用户@例子.com", "用户@xn--fsqu00a.com"},
		{"Name <bob@例子.com>", "Name <bob@xn--fsqu00a.com>"},
		{"bob@例子.com ", "bob@例子.com "}, // trailing space: not ours to fix
		{"not-an-address", "not-an-address"},
		{"bob@", "bob@"},
	}
	for _, c := range cases {
		if got := normalizeAddress(c.in); got != c.want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
