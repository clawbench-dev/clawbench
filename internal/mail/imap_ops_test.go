package mail

import (
	"context"
	"net/mail"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// The folder-bar ordering and special-use detection are pure functions but
// they decide which folder the panel opens and where "delete" sends a message,
// so they are pinned directly.
func TestSpecialUseOf(t *testing.T) {
	cases := []struct {
		attrs []imap.MailboxAttr
		name  string
		want  string
	}{
		{[]imap.MailboxAttr{imap.MailboxAttrSent}, "whatever", SpecialUseSent},
		{[]imap.MailboxAttr{imap.MailboxAttrDrafts}, "whatever", SpecialUseDrafts},
		{[]imap.MailboxAttr{imap.MailboxAttrTrash}, "whatever", SpecialUseTrash},
		{[]imap.MailboxAttr{imap.MailboxAttrJunk}, "whatever", SpecialUseJunk},
		{[]imap.MailboxAttr{imap.MailboxAttrArchive}, "whatever", SpecialUseArchive},
		{[]imap.MailboxAttr{imap.MailboxAttrAll}, "whatever", SpecialUseArchive},
		// No SPECIAL-USE (many Chinese providers): the localized name is all
		// there is to go on.
		{nil, "INBOX", SpecialUseInbox},
		{nil, "已发送", SpecialUseSent},
		{nil, "Sent Items", SpecialUseSent},
		{nil, "草稿箱", SpecialUseDrafts},
		{nil, "垃圾箱", SpecialUseTrash},
		{nil, "Spam", SpecialUseJunk},
		{nil, "归档", SpecialUseArchive},
		{nil, "Projects", ""},
	}
	for _, c := range cases {
		if got := specialUseOf(c.attrs, c.name); got != c.want {
			t.Errorf("specialUseOf(%v, %q) = %q, want %q", c.attrs, c.name, got, c.want)
		}
	}
}

func TestFolderRank(t *testing.T) {
	order := []Folder{
		{SpecialUse: SpecialUseInbox},
		{SpecialUse: SpecialUseSent},
		{SpecialUse: SpecialUseDrafts},
		{SpecialUse: SpecialUseArchive},
		{SpecialUse: SpecialUseJunk},
		{SpecialUse: SpecialUseTrash},
		{SpecialUse: ""},
	}
	for i := 1; i < len(order); i++ {
		if folderRank(order[i-1]) >= folderRank(order[i]) {
			t.Fatalf("folder order is not increasing at %d: %v", i, order)
		}
	}
}

func TestConvertAddressesSkipsGroups(t *testing.T) {
	got := convertAddresses([]imap.Address{
		{Name: "A", Mailbox: "a", Host: "x.com"},
		{Mailbox: "team"}, // group start
		{},                // group end
	})
	// Group markers are not real recipients; leaving them in would put a
	// bogus entry in the To line of a reply.
	if len(got) != 1 || got[0].Addr != "a@x.com" {
		t.Fatalf("convertAddresses = %+v", got)
	}

	std := convertMailAddresses([]*mail.Address{
		{Name: "A", Address: "a@x.com"},
		nil,
	})
	if len(std) != 1 || std[0].Addr != "a@x.com" {
		t.Fatalf("convertMailAddresses = %+v", std)
	}
}

func TestHasAttrAndBodyHasAttachments(t *testing.T) {
	if !hasAttr([]imap.MailboxAttr{imap.MailboxAttrSent}, imap.MailboxAttrSent) {
		t.Fatal("hasAttr should find an exact match")
	}
	if hasAttr([]imap.MailboxAttr{imap.MailboxAttrDrafts}, imap.MailboxAttrSent) {
		t.Fatal("hasAttr matched the wrong attribute")
	}

	if bodyHasAttachments(nil) {
		t.Fatal("a nil body structure has no attachments")
	}

	// multipart/mixed with a PDF child is an attachment…
	withPDF := &imap.BodyStructureMultiPart{
		Subtype: "mixed",
		Children: []imap.BodyStructure{
			&imap.BodyStructureSinglePart{
				Type: "text", Subtype: "plain",
				Extended: &imap.BodyStructureSinglePartExt{
					Disposition: &imap.BodyStructureDisposition{Value: "inline"},
				},
			},
			&imap.BodyStructureSinglePart{
				Type: "application", Subtype: "pdf",
				Extended: &imap.BodyStructureSinglePartExt{
					Disposition: &imap.BodyStructureDisposition{Value: "attachment", Params: map[string]string{"filename": "a.pdf"}},
				},
			},
		},
	}
	if !bodyHasAttachments(withPDF) {
		t.Fatal("a PDF part with Content-Disposition: attachment should count")
	}

	// …while text+html alternative is not.
	alternative := &imap.BodyStructureMultiPart{
		Subtype: "alternative",
		Children: []imap.BodyStructure{
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"},
			&imap.BodyStructureSinglePart{Type: "text", Subtype: "html"},
		},
	}
	if bodyHasAttachments(alternative) {
		t.Fatal("text+html alternative is not an attachment")
	}
}

// headerFromBuffer still understands an ENVELOPE: some servers (and test
// doubles) send one even though the client no longer asks for it.
func TestHeaderFromBufferUsesEnvelopeWhenPresent(t *testing.T) {
	buf := &imapclient.FetchMessageBuffer{
		UID:          7,
		RFC822Size:   99,
		InternalDate: time.Unix(1700000000, 0),
		Envelope: &imap.Envelope{
			Date:      time.Unix(1700000000, 0),
			Subject:   "来自 ENVELOPE",
			MessageID: "m7@example.com",
			InReplyTo: []string{"parent@example.com"},
			From:      []imap.Address{{Name: "Alice", Mailbox: "alice", Host: "example.com"}},
			To:        []imap.Address{{Name: "Me", Mailbox: "me", Host: "example.com"}},
			Cc:        []imap.Address{{Mailbox: "cc", Host: "example.com"}},
			ReplyTo:   []imap.Address{{Mailbox: "reply", Host: "example.com"}},
		},
		Flags: []imap.Flag{imap.FlagSeen, imap.FlagFlagged, imap.FlagAnswered},
	}
	h := headerFromBuffer(buf, nil)
	if h.Subject != "来自 ENVELOPE" || h.MessageID != "m7@example.com" {
		t.Fatalf("subject/id = %q / %q", h.Subject, h.MessageID)
	}
	if h.FromAddr != "alice@example.com" || len(h.To) != 1 || len(h.Cc) != 1 || len(h.ReplyTo) != 1 {
		t.Fatalf("addresses not mapped: %+v", h)
	}
	if !h.Seen || !h.Flagged || !h.Answered {
		t.Fatalf("flags not mapped: %+v", h)
	}
	if h.Date.Unix() != 1700000000 {
		t.Fatalf("date = %v", h.Date)
	}
}

// Every command on a closed connection must come back as an error instead of
// blocking or panicking: the sync worker hits this whenever a server drops the
// socket between two folders.
func TestIMAPOpsOnClosedClientFail(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := c.ListFolders(ctx); err == nil {
		t.Error("ListFolders on a closed client should fail")
	}
	if _, err := c.Status(ctx, "INBOX"); err == nil {
		t.Error("Status on a closed client should fail")
	}
	if _, err := c.FetchNewest(ctx, "INBOX", 5); err == nil {
		t.Error("FetchNewest on a closed client should fail")
	}
	if _, err := c.FetchHeaders(ctx, "INBOX", []uint32{1}); err == nil {
		t.Error("FetchHeaders on a closed client should fail")
	}
	if _, err := c.FetchBefore(ctx, "INBOX", 10, 5); err == nil {
		t.Error("FetchBefore on a closed client should fail")
	}
	if _, err := c.FetchRaw(ctx, "INBOX", 1); err == nil {
		t.Error("FetchRaw on a closed client should fail")
	}
	if _, err := c.FetchRawBatch(ctx, "INBOX", []uint32{1}); err == nil {
		t.Error("FetchRawBatch on a closed client should fail")
	}
	if err := c.StoreFlags(ctx, "INBOX", 1, []string{"\\Seen"}, nil); err == nil {
		t.Error("StoreFlags on a closed client should fail")
	}
	if err := c.MarkAllSeen(ctx, "INBOX"); err == nil {
		t.Error("MarkAllSeen on a closed client should fail")
	}
	if err := c.Move(ctx, "INBOX", 1, "Trash"); err == nil {
		t.Error("Move on a closed client should fail")
	}
	if err := c.Delete(ctx, "INBOX", 1); err == nil {
		t.Error("Delete on a closed client should fail")
	}
	if err := c.Append(ctx, "INBOX", []byte("x"), false); err == nil {
		t.Error("Append on a closed client should fail")
	}
}

func TestIMAPOpsOnUnknownFolderFail(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if _, err := c.FetchNewest(ctx, "NoSuchFolder", 5); err == nil {
		t.Error("FetchNewest on an unknown folder should fail")
	}
	if _, err := c.FetchRaw(ctx, "NoSuchFolder", 1); err == nil {
		t.Error("FetchRaw on an unknown folder should fail")
	}
	if err := c.MarkAllSeen(ctx, "NoSuchFolder"); err == nil {
		t.Error("MarkAllSeen on an unknown folder should fail")
	}
	if err := c.Move(ctx, "NoSuchFolder", 1, "Trash"); err == nil {
		t.Error("Move from an unknown folder should fail")
	}
	if err := c.Delete(ctx, "NoSuchFolder", 1); err == nil {
		t.Error("Delete from an unknown folder should fail")
	}
	// Moving into a folder that does not exist must not delete the source.
	if err := c.Move(ctx, "INBOX", 1, "NoSuchFolder"); err == nil {
		t.Error("Move to an unknown destination should fail")
	}
}

func TestIMAPEmptyInputs(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// No UIDs means no round trip: an empty set is not an error.
	if got, err := c.FetchHeaders(ctx, "INBOX", nil); err != nil || len(got) != 0 {
		t.Fatalf("FetchHeaders(nil) = %+v, %v", got, err)
	}
	if got, err := c.FetchRawBatch(ctx, "INBOX", nil); err != nil || len(got) != 0 {
		t.Fatalf("FetchRawBatch(nil) = %+v, %v", got, err)
	}
	if err := c.StoreFlags(ctx, "INBOX", 1, nil, nil); err != nil {
		t.Fatalf("an empty flag patch should be a no-op, got %v", err)
	}
	if got, err := c.FetchNewest(ctx, "INBOX", 0); err != nil || len(got) != 0 {
		t.Fatalf("FetchNewest(0) = %+v, %v", got, err)
	}
}

func TestIsClosedAfterClose(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	c, err := DialIMAP(context.Background(), acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close must be idempotent, got %v", err)
	}
}
