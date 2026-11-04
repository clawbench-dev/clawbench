package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

func TestPresetByID(t *testing.T) {
	p, ok := PresetByID("qq")
	if !ok || p.IMAPHost != "imap.qq.com" {
		t.Fatalf("qq preset: %+v ok=%v", p, ok)
	}
	if _, ok := PresetByID("nope"); ok {
		t.Fatal("unknown id must not resolve")
	}
	// "custom" is the escape hatch and deliberately carries no servers.
	custom, ok := PresetByID("custom")
	if !ok || custom.IMAPHost != "" {
		t.Fatalf("custom preset: %+v", custom)
	}
}

func TestParseHTMLOnlyAndInlineImages(t *testing.T) {
	raw := []byte("From: a@example.com\r\nSubject: 图文\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=B\r\n\r\n" +
		"--B\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>你好 <img src=\"cid:logo@x\"></p>\r\n" +
		"--B\r\nContent-Type: image/png\r\nContent-ID: <logo@x>\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\niVBORw0KGgo=\r\n--B--\r\n")

	pm, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pm.HTML, "你好") {
		t.Fatalf("html body = %q", pm.HTML)
	}
	if len(pm.Attachments) != 1 {
		t.Fatalf("expected the inline image to be recorded, got %+v", pm.Attachments)
	}
	if !pm.Attachments[0].Inline || pm.Attachments[0].ContentID == "" {
		t.Fatalf("inline image metadata = %+v", pm.Attachments[0])
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	// Bytes that are not a message at all are an error, not an empty message:
	// the caller must be able to tell "nothing to show" from "could not read".
	_, err := Parse([]byte("not a message"))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if KindOf(err) != KindServer {
		t.Fatalf("parse failures are server-kind, got %v", KindOf(err))
	}
}

func TestSnippetFallsBackToHTML(t *testing.T) {
	html := `<html><head><style>p{color:red}</style></head>` +
		`<body><!-- comment --><p>第一段</p><p>第二段</p><script>alert(1)</script></body></html>`
	got := Snippet("", html, 100)
	if strings.Contains(got, "color:red") || strings.Contains(got, "alert") {
		t.Fatalf("script/style leaked into the preview: %q", got)
	}
	if !strings.Contains(got, "第一段") || !strings.Contains(got, "第二段") {
		t.Fatalf("preview = %q", got)
	}

	if got := Snippet("  plain   text  ", "<p>ignored</p>", 100); got != "plain text" {
		t.Fatalf("text body should win and be collapsed, got %q", got)
	}

	// Truncation counts runes, not bytes: a Chinese preview must not be cut in
	// the middle of a character.
	got = Snippet(strings.Repeat("字", 20), "", 5)
	if got != strings.Repeat("字", 5) {
		t.Fatalf("truncation = %q", got)
	}
	if Snippet("x", "", 0) != "x" {
		t.Fatal("maxRunes <= 0 must not truncate")
	}
}

func TestPlainFromHTML(t *testing.T) {
	if got := plainFromHTML(""); got != "" {
		t.Fatalf("empty html = %q", got)
	}
	got := plainFromHTML("<div>a &amp; b</div><div>c</div>")
	if !strings.Contains(got, "a & b") {
		t.Fatalf("entities should be unescaped: %q", got)
	}
	if !strings.Contains(got, "c") {
		t.Fatalf("block tags should separate text: %q", got)
	}
}

func TestClassifyKinds(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Kind
	}{
		{"auth text", errors.New("Authentication failed."), KindAuth},
		{"invalid credentials", errors.New("invalid credentials"), KindAuth},
		{"imap auth code", &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAuthenticationFailed, Text: "no"}, KindAuth},
		{"imap other", &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeNonExistent, Text: "no such mailbox"}, KindServer},
		{"connect text", errors.New("dial tcp: connection refused"), KindConnect},
		{"no such host", errors.New("no such host"), KindConnect},
		{"timeout", &net.DNSError{IsTimeout: true, Err: "timeout"}, KindTimeout},
		{"deadline", context.DeadlineExceeded, KindTimeout},
		{"tls record", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, KindTLS},
		{"unknown authority", x509.UnknownAuthorityError{}, KindTLS},
		{"already classified", &Error{Kind: KindAuth, Op: "login", Err: errors.New("x")}, KindAuth},
		{"plain", errors.New("something odd"), KindServer},
	}
	for _, c := range cases {
		if got := KindOf(classify("op", c.err)); got != c.want {
			t.Errorf("%s: classify -> %v, want %v (%v)", c.name, got, c.want, c.err)
		}
	}
	if err := classify("op", nil); err != nil {
		t.Fatalf("classify(nil) = %v", err)
	}
}

func TestErrorMessageAndTimeout(t *testing.T) {
	e := &Error{Kind: KindConnect, Op: "dial", Err: errors.New("refused")}
	if e.Error() != "dial: refused" {
		t.Fatalf("Error() = %q", e.Error())
	}
	if !errors.Is(e, e.Err) {
		t.Fatal("Error must unwrap to the underlying cause")
	}
	if bare := (&Error{Kind: KindServer, Err: errors.New("x")}); bare.Error() != "x" {
		t.Fatalf("an empty op should not add a prefix: %q", bare.Error())
	}

	te := timeoutError("op", 3*time.Second)
	if KindOf(te) != KindTimeout || !strings.Contains(te.Error(), "3s") {
		t.Fatalf("timeoutError = %v", te)
	}
}

func TestBuildOutgoingMessageRejectsBadAddresses(t *testing.T) {
	base := SendRequest{From: "me@example.com", To: []string{"you@example.com"}, Text: "x"}

	bad := base
	bad.From = "not-an-address"
	if _, err := buildOutgoingMessage(bad); err == nil {
		t.Fatal("expected an invalid sender to be rejected")
	}

	bad = base
	bad.To = []string{"not-an-address"}
	if _, err := buildOutgoingMessage(bad); err == nil {
		t.Fatal("expected an invalid recipient to be rejected")
	}

	bad = base
	bad.Cc = []string{"not-an-address"}
	if _, err := buildOutgoingMessage(bad); err == nil {
		t.Fatal("expected an invalid cc to be rejected")
	}

	bad = base
	bad.Bcc = []string{"not-an-address"}
	if _, err := buildOutgoingMessage(bad); err == nil {
		t.Fatal("expected an invalid bcc to be rejected")
	}
}

func TestBuildOutgoingMessageHTMLOnlyAndThreading(t *testing.T) {
	msg, err := buildOutgoingMessage(SendRequest{
		From:       "me@example.com",
		To:         []string{"you@example.com"},
		Cc:         []string{"cc@example.com"},
		Bcc:        []string{"bcc@example.com"},
		Subject:    "回复",
		HTML:       "<p>正文</p>",
		InReplyTo:  "m1@example.com",
		References: []string{"root@example.com", "<m1@example.com>"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	var buf strings.Builder
	if _, err := msg.WriteTo(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	raw := buf.String()
	if !strings.Contains(raw, "In-Reply-To: <m1@example.com>") {
		t.Fatalf("In-Reply-To missing or unquoted:\n%s", raw)
	}
	if !strings.Contains(raw, "<root@example.com> <m1@example.com>") {
		t.Fatalf("References should be angle-wrapped and space-joined:\n%s", raw)
	}
	// An HTML-only composer still needs a text part: some clients show nothing
	// otherwise. The parts are quoted-printable, so decode rather than grep.
	parsed, perr := Parse([]byte(raw))
	if perr != nil {
		t.Fatalf("parse built message: %v", perr)
	}
	if !strings.Contains(parsed.Text, "正文") {
		t.Fatalf("plain-text alternative missing: %q", parsed.Text)
	}
	if !strings.Contains(parsed.HTML, "正文") {
		t.Fatalf("html part missing: %q", parsed.HTML)
	}
}

func TestEnsureAngle(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"  ":               "",
		"m1@example.com":   "<m1@example.com>",
		"<m1@example.com>": "<m1@example.com>",
	}
	for in, want := range cases {
		if got := ensureAngle(in); got != want {
			t.Errorf("ensureAngle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSMTPSenderRejectsInvalidRecipient(t *testing.T) {
	_, err := SMTPSender{}.Send(context.Background(), Account{Email: "me@example.com"}, SendRequest{
		From: "me@example.com",
		To:   []string{"not-an-address"},
		Text: "x",
	})
	if err == nil {
		t.Fatal("expected the invalid recipient to abort the send")
	}
	if KindOf(err) != KindServer {
		t.Fatalf("invalid address should be a server kind, got %v", KindOf(err))
	}
}

func TestSMTPSendHTMLOnlyProducesTextPart(t *testing.T) {
	acc, backend, stop := startTestSMTP(t)
	defer stop()

	_, err := SMTPSender{}.Send(context.Background(), acc, SendRequest{
		From:    "tester@example.com",
		To:      []string{"bob@example.com"},
		Subject: "只有 HTML",
		HTML:    "<p>正文段落</p>",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case raw := <-backend.messages:
		parsed, perr := Parse(raw)
		if perr != nil {
			t.Fatalf("parse: %v", perr)
		}
		if !strings.Contains(parsed.Text, "正文段落") {
			t.Fatalf("the plain-text alternative is missing: %q", parsed.Text)
		}
		if !strings.Contains(parsed.HTML, "正文段落") {
			t.Fatalf("the html part is missing: %q", parsed.HTML)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server received no message")
	}
}

func TestSMTPSendToClosedPortIsClassified(t *testing.T) {
	_, err := SMTPSender{}.Send(context.Background(), Account{
		Email: "me@example.com", Username: "u", Password: "p",
		SMTPHost: "127.0.0.1", SMTPPort: 1, SMTPSecurity: SecurityNone,
	}, SendRequest{From: "me@example.com", To: []string{"you@example.com"}, Text: "x"})
	if err == nil {
		t.Fatal("expected the dial to fail")
	}
	if kind := KindOf(err); kind != KindConnect && kind != KindTimeout {
		t.Fatalf("expected connect/timeout, got %v (%v)", kind, err)
	}
}
