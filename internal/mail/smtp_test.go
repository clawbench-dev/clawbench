package mail

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

type captureBackend struct {
	messages chan []byte
	user     string
	pass     string
}

func (b *captureBackend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &captureSession{backend: b}, nil
}

type captureSession struct {
	backend *captureBackend
}

func (s *captureSession) Mail(string, *smtp.MailOptions) error { return nil }
func (s *captureSession) Rcpt(string, *smtp.RcptOptions) error { return nil }
func (s *captureSession) Reset()                               {}
func (s *captureSession) Logout() error                        { return nil }

func (s *captureSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.backend.messages <- raw
	return nil
}

func (s *captureSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *captureSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, password string) error {
		if username != s.backend.user || password != s.backend.pass {
			return smtp.ErrAuthFailed
		}
		return nil
	}), nil
}

func startTestSMTP(t *testing.T) (Account, *captureBackend, func()) {
	t.Helper()
	backend := &captureBackend{messages: make(chan []byte, 4), user: "tester", pass: "secret"}
	srv := smtp.NewServer(backend)
	srv.Domain = "example.com"
	srv.AllowInsecureAuth = true

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
	}, backend, func() { _ = srv.Close() }
}

func TestSMTPSend(t *testing.T) {
	acc, backend, stop := startTestSMTP(t)
	defer stop()

	_, err := SMTPSender{}.Send(context.Background(), acc, SendRequest{
		From:    "tester@example.com",
		To:      []string{"bob@example.com"},
		Cc:      []string{"carol@example.com"},
		Subject: "测试发信",
		Text:    "正文内容",
		Attachments: []SendAttachment{{
			Filename:    "note.txt",
			ContentType: "text/plain",
			Data:        []byte("attached"),
		}},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case raw := <-backend.messages:
		parsed, perr := Parse(raw)
		if perr != nil {
			t.Fatalf("parse sent message: %v", perr)
		}
		if parsed.Subject != "测试发信" {
			t.Fatalf("subject = %q", parsed.Subject)
		}
		if !strings.Contains(parsed.Text, "正文内容") {
			t.Fatalf("body = %q", parsed.Text)
		}
		var found bool
		for _, a := range parsed.Attachments {
			if a.Filename == "note.txt" {
				found = true
			}
		}
		if !found {
			t.Fatalf("attachment missing: %+v", parsed.Attachments)
		}
	default:
		t.Fatal("server received no message")
	}
}

func TestSMTPAuthFailureIsClassified(t *testing.T) {
	acc, _, stop := startTestSMTP(t)
	defer stop()
	acc.Password = "wrong"

	_, err := SMTPSender{}.Send(context.Background(), acc, SendRequest{
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
