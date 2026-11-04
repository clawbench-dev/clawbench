package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/model"
)

// --- store primitives ---

func TestMailStoreLookupsReportNotFound(t *testing.T) {
	setupMailDB(t)

	if _, err := GetMailFolder(999); err == nil {
		t.Error("GetMailFolder should report not-found")
	}
	if _, err := GetMailFolderByPath(1, "INBOX"); err == nil {
		t.Error("GetMailFolderByPath should report not-found")
	}
	if _, err := GetMailMessage(999); err == nil {
		t.Error("GetMailMessage should report not-found")
	}
	if _, err := GetMailAttachment(999); err == nil {
		t.Error("GetMailAttachment should report not-found")
	}
	if _, err := GetMailDraft(999); err == nil {
		t.Error("GetMailDraft should report not-found")
	}
	if _, err := GetMailAccount(999); err == nil {
		t.Error("GetMailAccount should report not-found")
	}
	if _, err := ListMailFolders(999); err != nil {
		t.Errorf("an account with no folders is empty, not an error: %v", err)
	}
}

func TestUpdateMailFolderState(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	id, err := UpsertMailFolder(MailFolder{AccountID: acc.ID, Path: "INBOX", Name: "INBOX", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	// UIDVALIDITY / UIDNEXT / LASTSEEN are what the next sync compares against;
	// losing them silently re-downloads the whole mailbox.
	if err := UpdateMailFolderState(id, 11, 42, 40, 3, 100); err != nil {
		t.Fatalf("update state: %v", err)
	}
	got, err := GetMailFolder(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.UIDValidity != 11 || got.UIDNext != 42 || got.LastSeenUID != 40 {
		t.Fatalf("state not stored: %+v", got)
	}
	if got.Unread != 3 || got.Total != 100 {
		t.Fatalf("counters not stored: %+v", got)
	}
}

func TestUpsertMailMessagesEmptyIsANoop(t *testing.T) {
	setupMailDB(t)
	rows, err := UpsertMailMessages(nil)
	if err != nil {
		t.Fatalf("empty upsert: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %+v", rows)
	}
}

func TestMailBodyLookups(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)

	text, html, fetched, err := GetMailMessageBody(msg.ID)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	if fetched || text != "" || html != "" {
		t.Fatalf("a fresh row has no body: %q %q %v", text, html, fetched)
	}
	if err := SaveMailMessageBody(msg.ID, "正文", "<p>正文</p>", "正文"); err != nil {
		t.Fatal(err)
	}
	text, html, fetched, err = GetMailMessageBody(msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched || text != "正文" || html != "<p>正文</p>" {
		t.Fatalf("body not stored: %q %q %v", text, html, fetched)
	}
}

func TestMarkMailFolderReadLocally(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, _ := seedOneMessage(t, acc.ID)
	if err := AdjustMailFolderUnread(folder.ID, 5); err != nil {
		t.Fatal(err)
	}

	if err := MarkMailFolderReadLocally(folder.ID); err != nil {
		t.Fatalf("mark locally: %v", err)
	}
	after, err := GetMailFolder(folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Unread != 0 {
		t.Fatalf("unread should be cleared, got %d", after.Unread)
	}
}

func TestSaveMailDraftUpdatePath(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)

	d, err := SaveMailDraft(MailDraft{AccountID: acc.ID, Subject: "草稿", To: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	d.Subject = "改过"
	d.BodyText = "内容"
	updated, err := SaveMailDraft(d)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != d.ID || updated.Subject != "改过" || updated.BodyText != "内容" {
		t.Fatalf("update did not apply: %+v", updated)
	}

	if err := DeleteMailDraft(d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetMailDraft(d.ID); err == nil {
		t.Fatal("the draft should be gone")
	}
}

func TestMailAddrEncoding(t *testing.T) {
	if got := marshalAddrs(nil); got != "[]" {
		t.Fatalf("marshalAddrs(nil) = %q", got)
	}
	addrs := []MailAddr{{Name: "张三", Addr: "z@example.com"}, {Addr: "l@example.com"}}
	encoded := marshalAddrs(addrs)
	decoded := unmarshalAddrs(encoded)
	if len(decoded) != 2 || decoded[0].Name != "张三" || decoded[1].Addr != "l@example.com" {
		t.Fatalf("round trip = %+v", decoded)
	}
	if got := unmarshalAddrs(""); len(got) != 0 {
		t.Fatalf("empty string should decode to nothing, got %+v", got)
	}
	// Corrupt JSON must not take the whole message down.
	if got := unmarshalAddrs("{not json"); len(got) != 0 {
		t.Fatalf("corrupt json should decode to nothing, got %+v", got)
	}
	if got := FormatMailAddrs(addrs); got != "z@example.com, l@example.com" {
		t.Fatalf("FormatMailAddrs = %q", got)
	}
}

// --- body / attachments ---

func TestOpenMailAttachmentMissingMessage(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	if err := SaveMailAttachments(msg.ID, []MailAttachment{{
		MessageID: msg.ID, Filename: "a.txt", ContentType: "text/plain", Size: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	atts, err := ListMailAttachments(msg.ID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("list attachments: %v %d", err, len(atts))
	}
	// The message row disappears but the attachment row stays: opening must
	// report not-found rather than panic on a nil message.
	if err := DeleteMailMessageRow(msg.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenMailAttachment(context.Background(), atts[0].ID); err == nil {
		t.Fatal("expected not-found for a dangling attachment")
	}
}

func TestWriteMailAttachmentFileAndUniquePath(t *testing.T) {
	prev := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = prev })
	dir := model.DataDir
	path, err := writeMailAttachmentFile(1, "报告.pdf", []byte("x"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	// The file must land inside the data dir's attachments folder, never in
	// the process working directory.
	wantDir := filepath.Join(dir, "mail", "attachments", "1")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("path = %q, want it under %q", path, wantDir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing: %v", err)
	}

	// A second attachment with the same name must not overwrite the first.
	second := uniqueMailPath(filepath.Dir(path), "报告.pdf")
	if second == path {
		t.Fatalf("uniqueMailPath returned the taken path %q", second)
	}
	if err := os.WriteFile(second, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniqueMailPath(filepath.Dir(path), "报告.pdf"); got == path || got == second {
		t.Fatalf("third path collided: %q", got)
	}
}

func TestDeleteMailAttachmentFilesMissingDirIsFine(t *testing.T) {
	// Deleting an account that never cached an attachment must not fail.
	DeleteMailAttachmentFiles(12345)
}

func TestSanitizeMailFilenameStripsPathAndControlChars(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "passwd",
		"":                 "attachment",
		"  .  ":            "attachment",
	}
	for in, want := range cases {
		if got := sanitizeMailFilename(in); got != want {
			t.Errorf("sanitizeMailFilename(%q) = %q, want %q", in, got, want)
		}
	}
	// Windows separators are flattened rather than treated as a path: the file
	// is written into our own directory, so any separator would be a way out.
	win := sanitizeMailFilename(`C:\Windows\evil.exe`)
	if strings.ContainsAny(win, `\/:`) {
		t.Fatalf("separator survived: %q", win)
	}
	if !strings.HasSuffix(win, "evil.exe") {
		t.Fatalf("basename lost: %q", win)
	}

	// A very long name is truncated to the filesystem-friendly limit but keeps
	// its extension.
	long := sanitizeMailFilename(strings.Repeat("a", 300) + ".pdf")
	if len([]rune(long)) > 120 || !strings.HasSuffix(long, ".pdf") {
		t.Fatalf("long name = %q (len %d)", long, len([]rune(long)))
	}
}

// --- actions: error paths ---

func TestSetMailMessageFlagsEmptyPatchIsNoop(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	installFakeDialer(t, f)

	got, err := SetMailMessageFlags(context.Background(), msg.ID, MailFlagPatch{})
	if err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if got.ID != msg.ID {
		t.Fatalf("unexpected message %+v", got)
	}
	if len(f.flagOps) != 0 {
		t.Fatalf("an empty patch must not touch the server: %+v", f.flagOps)
	}
}

func TestMoveAndDeleteUnknownMessage(t *testing.T) {
	setupMailDB(t)
	if err := MoveMailMessage(context.Background(), 999, 1); err == nil {
		t.Error("moving an unknown message should fail")
	}
	if err := DeleteMailMessage(context.Background(), 999, false); err == nil {
		t.Error("deleting an unknown message should fail")
	}
}

func TestMoveMailMessageToUnknownFolder(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	installFakeDialer(t, newFakeClient())

	if err := MoveMailMessage(context.Background(), msg.ID, 999); err == nil {
		t.Fatal("moving into an unknown folder should fail")
	}
	if _, err := GetMailMessage(msg.ID); err != nil {
		t.Fatal("a failed move must not drop the local row")
	}
}

func TestFindSpecialFolderWithoutFolders(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	if _, ok := findSpecialFolder(acc.ID, mail.SpecialUseTrash); ok {
		t.Fatal("an account with no folders has no Trash")
	}
}

func TestSendMailWithoutSentFolderStillSucceeds(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	prevSender := mailSender
	mailSender = &captureSender{}
	t.Cleanup(func() { mailSender = prevSender })
	// No dialer installed: the account has no Sent folder, so the APPEND is
	// skipped entirely and the send still counts as delivered.
	err := SendMail(context.Background(), MailSendRequest{
		AccountID: acc.ID, To: []string{"bob@example.com"}, Subject: "x", Text: "y",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
}

// --- fixtures ---

// seedOneMessage inserts a folder and one message without touching the network.
func seedOneMessage(t *testing.T, accountID int64) (MailFolder, MailMessage) {
	t.Helper()
	folderID, err := UpsertMailFolder(MailFolder{
		AccountID: accountID, Path: "INBOX", Name: "INBOX", Delimiter: "/",
		SpecialUse: mail.SpecialUseInbox, Unread: 1, Total: 1,
	})
	if err != nil {
		t.Fatalf("upsert folder: %v", err)
	}
	folder, err := GetMailFolder(folderID)
	if err != nil {
		t.Fatalf("get folder: %v", err)
	}
	rows, err := UpsertMailMessages([]MailMessage{{
		AccountID: accountID, FolderID: folderID, UID: 1, MessageID: "m1@example.com",
		Subject: "周报", FromName: "Boss", FromAddr: "boss@example.com",
		SentAt: time.Now().Unix(), Size: 42,
	}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("upsert message: %v %d", err, len(rows))
	}
	return folder, rows[0]
}
