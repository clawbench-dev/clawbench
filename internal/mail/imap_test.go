package mail

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// startTestIMAP boots an in-memory IMAP server on a random local port.
func startTestIMAP(t *testing.T) (Account, func()) {
	t.Helper()
	srv := imapmemserver.New()
	user := imapmemserver.NewUser("tester", "secret")
	for _, box := range []string{"INBOX", "Sent", "Trash", "Drafts"} {
		if err := user.Create(box, nil); err != nil {
			t.Fatalf("create %s: %v", box, err)
		}
	}
	srv.AddUser(user)

	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return srv.NewSession(), &imapserver.GreetingData{}, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}, imap.CapMove: {}},
		InsecureAuth: true,
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	addr := ln.Addr().(*net.TCPAddr)
	return Account{
		Username:     "tester",
		Password:     "secret",
		IMAPHost:     "127.0.0.1",
		IMAPPort:     addr.Port,
		IMAPSecurity: SecurityNone,
	}, func() { _ = server.Close() }
}

func rawMail(subject, from, body string) []byte {
	return []byte(fmt.Sprintf(
		"From: %s\r\nTo: me@example.com\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@example.com>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		from, subject, time.Now().Format(time.RFC1123Z), strings.ReplaceAll(subject, " ", "-"), body,
	))
}

func TestIMAPEndToEnd(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Append two messages into INBOX.
	for i, subj := range []string{"第一封 主题", "Second message"} {
		if err := c.Append(ctx, "INBOX", rawMail(subj, "alice@example.com", fmt.Sprintf("body %d", i)), false); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	folders, err := c.ListFolders(ctx)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	var inbox *Folder
	for i := range folders {
		if folders[i].SpecialUse == "inbox" || folders[i].Path == "INBOX" {
			inbox = &folders[i]
		}
	}
	if inbox == nil {
		t.Fatalf("INBOX missing from %+v", folders)
	}

	st, err := c.Status(ctx, "INBOX")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Messages != 2 {
		t.Fatalf("expected 2 messages, got %d", st.Messages)
	}
	if st.UIDValidity == 0 || st.UIDNext == 0 {
		t.Fatalf("expected uidvalidity/uidnext, got %+v", st)
	}

	headers, err := c.FetchNewest(ctx, "INBOX", 10)
	if err != nil {
		t.Fatalf("fetch newest: %v", err)
	}
	if len(headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(headers))
	}
	if headers[0].Subject != "第一封 主题" {
		t.Fatalf("subject not decoded: %q", headers[0].Subject)
	}
	if headers[0].FromAddr != "alice@example.com" {
		t.Fatalf("from addr: %q", headers[0].FromAddr)
	}
	if headers[0].Seen {
		t.Fatalf("new message should be unseen")
	}

	uid := headers[0].UID
	raw, err := c.FetchRaw(ctx, "INBOX", uid)
	if err != nil {
		t.Fatalf("fetch raw: %v", err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(parsed.Text, "body 0") {
		t.Fatalf("body not parsed: %q", parsed.Text)
	}

	if err := c.StoreFlags(ctx, "INBOX", uid, []string{"\\Seen"}, nil); err != nil {
		t.Fatalf("store flags: %v", err)
	}
	headers, err = c.FetchHeaders(ctx, "INBOX", []uint32{uid})
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if !headers[0].Seen {
		t.Fatalf("seen flag not applied")
	}

	if err := c.Move(ctx, "INBOX", uid, "Trash"); err != nil {
		t.Fatalf("move: %v", err)
	}
	st, _ = c.Status(ctx, "INBOX")
	if st.Messages != 1 {
		t.Fatalf("after move INBOX should have 1, got %d", st.Messages)
	}
	st, _ = c.Status(ctx, "Trash")
	if st.Messages != 1 {
		t.Fatalf("after move Trash should have 1, got %d", st.Messages)
	}

	trashHeaders, err := c.FetchNewest(ctx, "Trash", 10)
	if err != nil || len(trashHeaders) != 1 {
		t.Fatalf("trash fetch: %v %d", err, len(trashHeaders))
	}
	if err := c.Delete(ctx, "Trash", trashHeaders[0].UID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	st, _ = c.Status(ctx, "Trash")
	if st.Messages != 0 {
		t.Fatalf("after delete Trash should be empty, got %d", st.Messages)
	}
}

func TestIMAPWrongPasswordIsClassified(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	acc.Password = "nope"

	_, err := DialIMAP(context.Background(), acc)
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if KindOf(err) != KindAuth {
		t.Fatalf("expected auth kind, got %v (%v)", KindOf(err), err)
	}
}

func TestIMAPConnectFailureIsClassified(t *testing.T) {
	acc := Account{IMAPHost: "127.0.0.1", IMAPPort: 1, IMAPSecurity: SecurityNone}
	_, err := DialIMAP(context.Background(), acc)
	if err == nil {
		t.Fatal("expected connect failure")
	}
	if k := KindOf(err); k != KindConnect && k != KindTimeout {
		t.Fatalf("expected connect/timeout kind, got %v (%v)", k, err)
	}
}

// A header block is the fallback that replaced ENVELOPE. It must decode
// RFC 2047 subjects, split addresses, and — most importantly — never panic or
// abort on a header the parser dislikes.
func TestApplyHeaderSection(t *testing.T) {
	raw := []byte(
		"From: =?utf-8?B?5byg5LiJ?= <zhangsan@example.com>\r\n" +
			"To: Li Si <lisi@example.com>, wang@example.com\r\n" +
			"Cc: cc@example.com\r\n" +
			"Subject: =?utf-8?B?5ZGo5oql?=\r\n" +
			"Date: Mon, 3 Nov 2025 10:24:00 +0800\r\n" +
			"Message-ID: <abc@example.com>\r\n" +
			"In-Reply-To: <parent@example.com>\r\n" +
			"References: <root@example.com> <parent@example.com>\r\n" +
			"\r\n")

	var h MessageHeader
	applyHeaderSection(&h, raw)

	if h.Subject != "周报" {
		t.Fatalf("subject = %q", h.Subject)
	}
	if h.FromName != "张三" || h.FromAddr != "zhangsan@example.com" {
		t.Fatalf("from = %q <%q>", h.FromName, h.FromAddr)
	}
	if len(h.To) != 2 || h.To[0].Addr != "lisi@example.com" || h.To[1].Addr != "wang@example.com" {
		t.Fatalf("to = %+v", h.To)
	}
	if len(h.Cc) != 1 || h.Cc[0].Addr != "cc@example.com" {
		t.Fatalf("cc = %+v", h.Cc)
	}
	if h.MessageID != "abc@example.com" {
		t.Fatalf("message id = %q", h.MessageID)
	}
	if h.InReplyTo != "parent@example.com" {
		t.Fatalf("in-reply-to = %q", h.InReplyTo)
	}
	if len(h.References) != 2 || h.References[0] != "root@example.com" {
		t.Fatalf("references = %+v", h.References)
	}
	if h.Date.Year() != 2025 || h.Date.Month() != time.November {
		t.Fatalf("date = %v", h.Date)
	}
}

// The header block must be requested WITHOUT a HEADER.FIELDS filter. QQ Mail
// accepts the filtered form and answers with an empty block (measured: 2 bytes,
// just CRLF), which silently produced messages with no subject and no sender.
// Re-adding a field list here would bring that bug straight back, and it is
// invisible in the parser tests because they feed a header block directly.
func TestHeaderSectionItemAsksForWholeHeader(t *testing.T) {
	item := headerSectionItem()
	if item == nil {
		t.Fatal("headerSectionItem returned nil")
	}
	if item.Specifier != imap.PartSpecifierHeader {
		t.Fatalf("specifier = %v, want HEADER", item.Specifier)
	}
	if len(item.HeaderFields) != 0 {
		t.Fatalf("header filter must stay empty (QQ returns an empty block for it), got %v", item.HeaderFields)
	}
	if !item.Peek {
		t.Fatal("must use BODY.PEEK: a fetch that sets \\Seen would mark every message read")
	}
}

// Garbage in the header block must cost nothing: the caller keeps whatever the
// FETCH response already provided (uid/size/flags), so a broken header cannot
// take down a folder sync.
func TestApplyHeaderSectionGarbageKeepsGoing(t *testing.T) {
	var h MessageHeader
	h.Subject = "keep me"
	applyHeaderSection(&h, []byte("this is not a header block at all"))
	if h.Subject != "keep me" {
		t.Fatalf("subject was clobbered: %q", h.Subject)
	}
}
