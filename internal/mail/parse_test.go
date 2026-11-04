package mail

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParsePlainText(t *testing.T) {
	raw := []byte("From: Alice <alice@example.com>\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: =?UTF-8?B?5rWL6K+V5Li76aKY?=\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello\r\nWorld\r\n")

	pm, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pm.Subject != "测试主题" {
		t.Fatalf("subject = %q", pm.Subject)
	}
	if !strings.Contains(pm.Text, "Hello") || !strings.Contains(pm.Text, "World") {
		t.Fatalf("text = %q", pm.Text)
	}
	if len(pm.Attachments) != 0 {
		t.Fatalf("unexpected attachments: %d", len(pm.Attachments))
	}
	if got := Snippet(pm.Text, pm.HTML, 5); got != "Hello" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestParseHTMLOnlySnippet(t *testing.T) {
	raw := []byte("From: a@example.com\r\n" +
		"Subject: html\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><head><style>p{color:red}</style><script>alert(1)</script></head>" +
		"<body><p>第一段</p><p>第二段</p></body></html>\r\n")

	pm, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pm.HTML, "第一段") {
		t.Fatalf("html = %q", pm.HTML)
	}
	snippet := Snippet(pm.Text, pm.HTML, 40)
	if !strings.Contains(snippet, "第一段") || !strings.Contains(snippet, "第二段") {
		t.Fatalf("snippet = %q", snippet)
	}
	if strings.Contains(snippet, "alert") || strings.Contains(snippet, "color") {
		t.Fatalf("snippet leaked script/style: %q", snippet)
	}
}

func TestParseAttachmentAndInlineImage(t *testing.T) {
	raw := []byte("From: a@example.com\r\n" +
		"Subject: with files\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=BOUND\r\n" +
		"\r\n" +
		"--BOUND\r\n" +
		"Content-Type: multipart/related; boundary=REL\r\n" +
		"\r\n" +
		"--REL\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>see <img src=\"cid:logo123\"></p>\r\n" +
		"--REL\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-ID: <logo123>\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: inline; filename=\"logo.png\"\r\n" +
		"\r\n" +
		"iVBORw0KGgo=\r\n" +
		"--REL--\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/pdf; name=\"report.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"JVBERi0xLjQ=\r\n" +
		"--BOUND--\r\n")

	pm, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pm.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d: %+v", len(pm.Attachments), pm.Attachments)
	}
	var inline, attach *Attachment
	for i := range pm.Attachments {
		switch pm.Attachments[i].Filename {
		case "logo.png":
			inline = &pm.Attachments[i]
		case "report.pdf":
			attach = &pm.Attachments[i]
		}
	}
	if inline == nil || !inline.Inline || inline.ContentID == "" {
		t.Fatalf("inline image not captured: %+v", pm.Attachments)
	}
	if attach == nil || attach.Inline {
		t.Fatalf("pdf attachment not captured: %+v", pm.Attachments)
	}
	if len(attach.Content) == 0 {
		t.Fatalf("attachment content empty")
	}
}

func TestParseGBKBodyAndSubject(t *testing.T) {
	gbk := simplifiedchinese.GBK.NewEncoder()
	body, err := gbk.Bytes([]byte("这是国标编码的正文"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var buf bytes.Buffer
	buf.WriteString("From: a@example.com\r\n")
	// "=?GBK?B?...?=" for 中文标题
	buf.WriteString("Subject: =?GBK?B?1tDOxLHYzOKhIw==?=\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=gbk\r\n")
	buf.WriteString("\r\n")
	buf.Write(body)

	pm, err := Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pm.Text, "这是国标编码的正文") {
		t.Fatalf("gbk body not decoded: %q", pm.Text)
	}
	if pm.Subject == "" || strings.Contains(pm.Subject, "=?") {
		t.Fatalf("gbk subject not decoded: %q", pm.Subject)
	}
}

func TestParseQuotedPrintable(t *testing.T) {
	raw := []byte("From: a@example.com\r\n" +
		"Subject: qp\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"line one=0D=0Aline two with =E4=B8=AD=E6=96=87\r\n")

	pm, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(pm.Text, "line two with 中文") {
		t.Fatalf("quoted-printable not decoded: %q", pm.Text)
	}
}
