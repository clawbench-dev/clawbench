package service

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/mail"
)

// --- accounts: connectivity probe and cleanup ---

func TestTestMailAccount(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)

	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	installFakeDialer(t, f)
	if err := TestMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("test account: %v", err)
	}
	if !f.closed {
		t.Fatal("the probe must close its connection")
	}

	// A server that refuses the folder listing is exactly what "test" exists
	// to surface — the TCP connection alone proves nothing.
	f2 := newFakeClient()
	f2.failOn = "list"
	installFakeDialer(t, f2)
	if err := TestMailAccount(context.Background(), acc.ID); err == nil {
		t.Fatal("expected a folder-listing failure to be reported")
	}
}

func TestTestMailAccountUnknownID(t *testing.T) {
	setupMailDB(t)
	if err := TestMailAccount(context.Background(), 999); err == nil {
		t.Fatal("expected not-found")
	}
}

func TestTestMailAccountDialFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) {
		return nil, errors.New("connection refused")
	}
	t.Cleanup(func() { mailDialer = prev })

	err := TestMailAccount(context.Background(), acc.ID)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected the dial error to surface, got %v", err)
	}
}

func TestDeleteMailAccountAttachmentFilesRemovesDirs(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, err := listAccountMessages(t, acc.ID)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("list: %v %d", err, len(msgs))
	}

	dir := mailAttachmentsDir(msgs[0].ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	DeleteMailAccountAttachmentFiles(acc.ID)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("attachment dir should be gone, stat err = %v", err)
	}
}

// --- sync ---

func TestSyncMailAccountUnknownAccount(t *testing.T) {
	setupMailDB(t)
	if _, err := SyncMailAccount(context.Background(), 42); err == nil {
		t.Fatal("expected not-found")
	}
}

func TestSyncMailAccountDialFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) {
		return nil, errors.New("tls: handshake failure")
	}
	t.Cleanup(func() { mailDialer = prev })

	if _, err := SyncMailAccount(context.Background(), acc.ID); err == nil {
		t.Fatal("expected the dial failure to abort the sync")
	}
	// The failure is remembered so the settings row can show it.
	got, err := GetMailAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastError == "" {
		t.Fatal("last error should be recorded")
	}
}

func TestSyncMailAccountReportsBusy(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	mu := accountSyncLock(acc.ID)
	mu.Lock()
	defer mu.Unlock()

	_, err := SyncMailAccount(context.Background(), acc.ID)
	if !errors.Is(err, ErrMailSyncBusy) {
		t.Fatalf("expected ErrMailSyncBusy, got %v", err)
	}
}

func TestSyncMailAccountFolderListFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	f.failOn = "list"
	installFakeDialer(t, f)

	if _, err := SyncMailAccount(context.Background(), acc.ID); err == nil {
		t.Fatal("expected the folder listing failure to surface")
	}
}

func TestFetchOlderMailMessages(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 5, time.Now().Add(-5*time.Hour))
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the two oldest messages never having been cached: the sync only
	// pulls the newest page, so "load older" has to walk back through the UIDs
	// still sitting on the server.
	for range 2 {
		row, err := oldestCachedMessage(folder.ID)
		if err != nil || row == nil {
			t.Fatalf("oldest: %v", err)
		}
		if err := DeleteMailMessageRow(row.ID); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := FetchOlderMailMessages(context.Background(), folder.ID)
	if err != nil {
		t.Fatalf("fetch older: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected the two older messages, got %d", len(msgs))
	}
	seen := map[uint32]bool{}
	for _, m := range msgs {
		seen[m.UID] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("expected the two uncached UIDs, got %+v", seen)
	}
}

func TestFetchOlderMailMessagesEmptyCache(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := FetchOlderMailMessages(context.Background(), folder.ID)
	if err != nil {
		t.Fatalf("an empty cache is not an error: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected nothing, got %d", len(msgs))
	}
}

func TestFetchOlderMailMessagesFetchFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	f.failOn = "fetch"
	if _, err := FetchOlderMailMessages(context.Background(), folder.ID); err == nil {
		t.Fatal("expected the fetch failure to surface")
	}
}

func TestMailConnectionDead(t *testing.T) {
	cases := []struct {
		err  error
		dead bool
	}{
		// Only the classified transport failures count: a plain error string
		// that merely mentions "closed" is a per-folder problem, and treating
		// it as a dead connection would re-dial on every bad mailbox.
		{&mail.Error{Kind: mail.KindConnect, Err: errors.New("dial")}, true},
		{&mail.Error{Kind: mail.KindTimeout, Err: errors.New("timeout")}, true},
		{&mail.Error{Kind: mail.KindTLS, Err: errors.New("tls")}, true},
		{net.ErrClosed, true},
		{errors.New("use of closed network connection"), true},
		{errors.New("unexpected EOF"), false},
		{&mail.Error{Kind: mail.KindAuth, Err: errors.New("bad password")}, false},
		{errors.New("mailbox does not exist"), false},
	}
	for _, c := range cases {
		if got := mailConnectionDead(c.err); got != c.dead {
			t.Errorf("mailConnectionDead(%v) = %v, want %v", c.err, got, c.dead)
		}
	}
}

func TestHeaderToRowAndFirstMailAddr(t *testing.T) {
	h := mail.MessageHeader{
		UID: 7, Subject: "hi", FromName: "Alice", FromAddr: "alice@example.com",
		Date: time.Unix(1700000000, 0), Size: 42, Seen: true,
	}
	row := headerToRow(3, 9, h)
	if row.AccountID != 3 || row.FolderID != 9 || row.UID != 7 || row.Subject != "hi" {
		t.Fatalf("unexpected row %+v", row)
	}
	if row.SentAt != 1700000000 || row.Size != 42 || !row.Seen {
		t.Fatalf("metadata not carried: %+v", row)
	}

	if got := firstMailAddr([]mail.Address{{Name: "a", Addr: "a@x.com"}}); got != "a@x.com" {
		t.Fatalf("firstMailAddr = %q", got)
	}
	if got := firstMailAddr(nil); got != "" {
		t.Fatalf("firstMailAddr(nil) = %q", got)
	}
}

// --- reader ---

func TestGetMailMessageDetailFetchesBodyAndMarksSeen(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	// Over the prefetch ceiling, so the body is still unfetched after the sync —
	// that is the state the reader has to recover from.
	f.headers["INBOX"][0].Size = mailPrefetchMaxSize + 1
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, err := listAccountMessages(t, acc.ID)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("list: %v", err)
	}
	if msgs[0].BodyFetched {
		t.Fatal("fixture: a message over the prefetch ceiling must not be decoded by the sync")
	}
	// The seed marks even UIDs seen; force the unread path.
	if err := UpdateMailMessageFlags(msgs[0].ID, false, false, false); err != nil {
		t.Fatal(err)
	}

	detail, err := GetMailMessageDetail(context.Background(), msgs[0].ID, true)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !strings.Contains(detail.BodyText, "body") {
		t.Fatalf("body not decoded: %q", detail.BodyText)
	}
	if !detail.Seen {
		t.Fatal("opening the reader must mark the message seen")
	}
	if len(f.flagOps) == 0 {
		t.Fatal("the seen flag must be pushed to the server")
	}
}

func TestGetMailMessageDetailFetchFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	f.headers["INBOX"][0].Size = mailPrefetchMaxSize + 1
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)
	f.failOn = "raw"

	if _, err := GetMailMessageDetail(context.Background(), msgs[0].ID, true); err == nil {
		t.Fatal("a failed body fetch must surface, not return an empty reader")
	}
}

func TestGetMailMessageDetailAlreadyCachedOnlyPushesFlag(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)
	if err := SaveMailMessageBody(msgs[0].ID, "cached", "", "cached"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMailMessageFlags(msgs[0].ID, false, false, false); err != nil {
		t.Fatal(err)
	}
	f.flagOps = nil

	detail, err := GetMailMessageDetail(context.Background(), msgs[0].ID, true)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.BodyText != "cached" {
		t.Fatalf("cached body expected, got %q", detail.BodyText)
	}
	if len(f.flagOps) != 1 {
		t.Fatalf("expected exactly one flag push, got %+v", f.flagOps)
	}
}

func TestOpenMailAttachmentMaterializesMissingFile(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	f.headers["INBOX"] = []mail.MessageHeader{{
		UID: 1, Subject: "with attachment", FromAddr: "a@example.com", Date: time.Now(), Size: 100,
	}}
	f.raw["INBOX"] = map[uint32][]byte{1: []byte(
		"From: a@example.com\r\nSubject: with attachment\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: multipart/mixed; boundary=B\r\n\r\n" +
			"--B\r\nContent-Type: text/plain\r\n\r\nhello\r\n" +
			"--B\r\nContent-Type: application/pdf; name=\"doc.pdf\"\r\n" +
			"Content-Disposition: attachment; filename=\"doc.pdf\"\r\n" +
			"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n--B--\r\n")}
	f.status["INBOX"] = mail.Status{Messages: 1, UIDNext: 2, UIDValidity: 1}
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)
	detail, err := GetMailMessageDetail(context.Background(), msgs[0].ID, false)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %+v", detail.Attachments)
	}
	att := detail.Attachments[0]

	// Losing the local file must not lose the attachment: it is re-fetched.
	if att.LocalPath != "" {
		_ = os.Remove(att.LocalPath)
	}
	row, path, err := OpenMailAttachment(context.Background(), att.ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if row.Filename != "doc.pdf" {
		t.Fatalf("filename = %q", row.Filename)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("materialized file missing: %v", err)
	}
}

func TestOpenMailAttachmentLocalPathIsReused(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)

	dir := mailAttachmentsDir(msgs[0].ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveMailAttachments(msgs[0].ID, []MailAttachment{{
		MessageID: msgs[0].ID, Filename: "note.txt", ContentType: "text/plain",
		Size: 2, LocalPath: path,
	}}); err != nil {
		t.Fatal(err)
	}
	atts, err := ListMailAttachments(msgs[0].ID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("list attachments: %v %d", err, len(atts))
	}
	_, got, err := OpenMailAttachment(context.Background(), atts[0].ID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got != path {
		t.Fatalf("existing file should be reused, got %q", got)
	}
}

func TestOpenMailAttachmentUnknownID(t *testing.T) {
	setupMailDB(t)
	if _, _, err := OpenMailAttachment(context.Background(), 123); err == nil {
		t.Fatal("expected not-found")
	}
}

// --- flags / move / delete ---

func TestSetMailMessageFlagsServerFailureKeepsLocalRow(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)

	f.failOn = "store"
	seen := true
	if _, err := SetMailMessageFlags(context.Background(), msgs[0].ID, MailFlagPatch{Seen: &seen}); err == nil {
		t.Fatal("expected the server rejection to surface")
	}
	after, err := GetMailMessage(msgs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Seen != msgs[0].Seen {
		t.Fatal("a rejected flag change must not be mirrored locally")
	}
}

func TestMoveMailMessageToAnotherFolder(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedFolder(f, "Archive", "archive")
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)
	archive, err := GetMailFolderByPath(acc.ID, "Archive")
	if err != nil {
		t.Fatal(err)
	}

	if err := MoveMailMessage(context.Background(), msgs[0].ID, archive.ID); err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(f.moved) != 1 || !strings.Contains(f.moved[0], "->Archive") {
		t.Fatalf("move not pushed to the server: %+v", f.moved)
	}
	if _, err := GetMailMessage(msgs[0].ID); err == nil {
		t.Fatal("the local row should be dropped: the destination re-adds it with its own UID")
	}
}

func TestMoveMailMessageToSameFolderIsNoop(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)

	if err := MoveMailMessage(context.Background(), msgs[0].ID, msgs[0].FolderID); err != nil {
		t.Fatalf("same-folder move must be a no-op: %v", err)
	}
	if len(f.moved) != 0 {
		t.Fatalf("no server call expected, got %+v", f.moved)
	}
}

func TestDeleteMailMessageMovesToTrashWhenAvailable(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedFolder(f, "Trash", mail.SpecialUseTrash)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)

	if err := DeleteMailMessage(context.Background(), msgs[0].ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.moved) != 1 || !strings.Contains(f.moved[0], "->Trash") {
		t.Fatalf("expected a move to Trash, got %+v", f.moved)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("nothing should be expunged when Trash exists: %+v", f.deleted)
	}
}

func TestDeleteMailMessagePermanentExpunges(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedFolder(f, "Trash", mail.SpecialUseTrash)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)

	if err := DeleteMailMessage(context.Background(), msgs[0].ID, true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.deleted) != 1 {
		t.Fatalf("expected an expunge, got %+v", f.deleted)
	}
	if _, err := GetMailMessage(msgs[0].ID); err == nil {
		t.Fatal("the local row should be gone")
	}
}

func TestDeleteMailMessageWithoutTrashExpunges(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)

	if err := DeleteMailMessage(context.Background(), msgs[0].ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.deleted) != 1 {
		t.Fatalf("a mailbox without Trash must expunge: %+v", f.deleted)
	}
}

// --- compose ---

func TestSendMailCarriesAttachmentsAndDropsDraft(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedFolder(f, "Sent", mail.SpecialUseSent)
	installFakeDialer(t, f)
	// The Sent folder is discovered by the sync; without it the append is
	// skipped (best effort) and the test would pass for the wrong reason.
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	prevSender := mailSender
	sender := &captureSender{}
	mailSender = sender
	t.Cleanup(func() { mailSender = prevSender })

	dir := t.TempDir()
	attPath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(attPath, []byte("attached"), 0o644); err != nil {
		t.Fatal(err)
	}
	draft, err := SaveMailDraft(MailDraft{AccountID: acc.ID, Subject: "draft", To: "bob@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	err = SendMail(context.Background(), MailSendRequest{
		AccountID:   acc.ID,
		To:          []string{" bob@example.com ", ""},
		Subject:     " hello ",
		Text:        "body",
		Attachments: []MailOutgoingAttachment{{Filename: "note.txt", Path: attPath}},
		DraftID:     draft.ID,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one send, got %d", len(sender.sent))
	}
	got := sender.sent[0]
	if got.Subject != "hello" {
		t.Fatalf("subject should be trimmed, got %q", got.Subject)
	}
	if len(got.To) != 1 || got.To[0] != "bob@example.com" {
		t.Fatalf("empty recipients should be dropped: %+v", got.To)
	}
	if len(got.Attachments) != 1 || string(got.Attachments[0].Data) != "attached" {
		t.Fatalf("attachment not read from disk: %+v", got.Attachments)
	}
	if len(f.appended) != 1 || !strings.HasPrefix(f.appended[0], "Sent/") {
		t.Fatalf("the sent copy should be appended: %+v", f.appended)
	}
	if _, err := GetMailDraft(draft.ID); err == nil {
		t.Fatal("the draft should be deleted after a successful send")
	}
}

func TestSendMailMissingAttachmentFails(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	prevSender := mailSender
	mailSender = &captureSender{}
	t.Cleanup(func() { mailSender = prevSender })

	err := SendMail(context.Background(), MailSendRequest{
		AccountID:   acc.ID,
		To:          []string{"bob@example.com"},
		Attachments: []MailOutgoingAttachment{{Filename: "x", Path: filepath.Join(t.TempDir(), "nope")}},
	})
	if err == nil {
		t.Fatal("a missing attachment must abort the send")
	}
}

func TestSendMailSenderFailurePropagates(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	prevSender := mailSender
	mailSender = failingSender{}
	t.Cleanup(func() { mailSender = prevSender })

	err := SendMail(context.Background(), MailSendRequest{
		AccountID: acc.ID, To: []string{"bob@example.com"}, Subject: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "smtp rejected") {
		t.Fatalf("expected the SMTP error, got %v", err)
	}
}

func TestBuildMailReplyContextModes(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	f.headers["INBOX"] = []mail.MessageHeader{{
		UID: 1, Subject: "周报", FromAddr: "boss@example.com", FromName: "Boss",
		To:        []mail.Address{{Name: "Me", Addr: "me@example.com"}},
		MessageID: "m1@example.com", References: []string{"root@example.com"},
		Date: time.Now(), Size: 10,
	}}
	f.raw["INBOX"] = map[uint32][]byte{1: []byte(
		"From: Boss <boss@example.com>\r\nTo: me@example.com\r\nSubject: 周报\r\n" +
			"Message-ID: <m1@example.com>\r\nReferences: <root@example.com>\r\n" +
			"Date: Mon, 3 Nov 2025 10:24:00 +0800\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n\r\n原文\r\n")}
	f.status["INBOX"] = mail.Status{Messages: 1, UIDNext: 2, UIDValidity: 1}
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)
	if _, err := GetMailMessageDetail(context.Background(), msgs[0].ID, false); err != nil {
		t.Fatalf("detail: %v", err)
	}

	reply, err := BuildMailReplyContext(msgs[0].ID, "reply")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.Subject != "Re: 周报" || reply.To != "boss@example.com" {
		t.Fatalf("unexpected reply context %+v", reply)
	}
	if !strings.Contains(reply.References, "root@example.com") ||
		!strings.Contains(reply.References, "<m1@example.com>") {
		t.Fatalf("references must chain the original chain plus this message: %q", reply.References)
	}
	if !strings.Contains(reply.Quote, "原文") {
		t.Fatalf("quote should carry the body: %q", reply.Quote)
	}

	replyAll, err := BuildMailReplyContext(msgs[0].ID, "replyAll")
	if err != nil {
		t.Fatal(err)
	}
	if replyAll.Cc == "" {
		t.Fatal("replyAll must fill Cc from the original To")
	}

	fwd, err := BuildMailReplyContext(msgs[0].ID, "forward")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fwd.Subject, "Fwd: ") {
		t.Fatalf("forward subject = %q", fwd.Subject)
	}
	if fwd.InReplyTo != "" || fwd.References != "" {
		t.Fatal("a forward must not thread onto the original")
	}
}

func TestBuildMailReplyContextAlreadyPrefixed(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	f.headers["INBOX"] = []mail.MessageHeader{{
		UID: 1, Subject: "Re: 已有前缀", FromAddr: "a@example.com", Date: time.Now(), Size: 10,
	}}
	f.raw["INBOX"] = map[uint32][]byte{1: []byte("From: a@example.com\r\nSubject: Re: 已有前缀\r\n\r\nx\r\n")}
	f.status["INBOX"] = mail.Status{Messages: 1, UIDNext: 2, UIDValidity: 1}
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	msgs, _ := listAccountMessages(t, acc.ID)
	if _, err := GetMailMessageDetail(context.Background(), msgs[0].ID, false); err != nil {
		t.Fatalf("detail: %v", err)
	}

	reply, err := BuildMailReplyContext(msgs[0].ID, "reply")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Subject != "Re: 已有前缀" {
		t.Fatalf("must not double-prefix, got %q", reply.Subject)
	}
	// No Message-ID on the original: the reply must not invent a thread head.
	if reply.InReplyTo != "" {
		t.Fatalf("InReplyTo should stay empty, got %q", reply.InReplyTo)
	}
}

func TestBuildMailReplyContextUnknownMessage(t *testing.T) {
	setupMailDB(t)
	if _, err := BuildMailReplyContext(99, "reply"); err == nil {
		t.Fatal("expected not-found")
	}
}

// --- helpers ---

func TestCleanAddressesAndSplitReferences(t *testing.T) {
	got := cleanAddresses([]string{" a@x.com ", "", "b@x.com", "  "})
	if len(got) != 2 || got[0] != "a@x.com" || got[1] != "b@x.com" {
		t.Fatalf("cleanAddresses = %+v", got)
	}

	refs := splitReferences("<a@x.com>  <b@x.com>")
	if len(refs) != 2 || refs[0] != "<a@x.com>" || refs[1] != "<b@x.com>" {
		t.Fatalf("splitReferences = %+v", refs)
	}
	if refs := splitReferences("   "); len(refs) != 0 {
		t.Fatalf("blank references should yield nothing, got %+v", refs)
	}
}

func TestMailStoreFolderHelpers(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatalf("by path: %v", err)
	}
	if folder.Total == 0 {
		t.Fatal("folder counters should be populated by the sync")
	}
	if n, err := MailFolderCachedCount(folder.ID); err != nil || n == 0 {
		t.Fatalf("cached count = %d, %v", n, err)
	}

	if err := AdjustMailFolderUnread(folder.ID, -1); err != nil {
		t.Fatalf("adjust: %v", err)
	}
	if err := ClearMailFolderMessages(folder.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n, _ := MailFolderCachedCount(folder.ID); n != 0 {
		t.Fatalf("expected an empty folder, got %d", n)
	}
}

func TestMailUnreadTotals(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 4, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	rows, total, err := MailUnreadTotals()
	if err != nil {
		t.Fatalf("unread totals: %v", err)
	}
	if total == 0 || len(rows) != 1 {
		t.Fatalf("expected one account with unread mail, got %+v total=%d", rows, total)
	}
	if rows[0].AccountID != acc.ID || rows[0].Unread != total {
		t.Fatalf("row/total mismatch: %+v total=%d", rows, total)
	}
}

func TestMailDraftListFiltersByAccount(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	other, err := CreateMailAccount(MailAccount{
		Name: "其他", Email: "other@example.com", Username: "other@example.com",
		IMAPHost: "imap.example.com", SMTPHost: "smtp.example.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveMailDraft(MailDraft{AccountID: acc.ID, Subject: "mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveMailDraft(MailDraft{AccountID: other.ID, Subject: "theirs"}); err != nil {
		t.Fatal(err)
	}

	mine, err := ListMailDrafts(acc.ID)
	if err != nil {
		t.Fatalf("list drafts: %v", err)
	}
	if len(mine) != 1 || mine[0].Subject != "mine" {
		t.Fatalf("drafts leaked across accounts: %+v", mine)
	}
	// Drafts are account-scoped: an unset account id lists nothing rather
	// than leaking another mailbox's drafts into the composer.
	none, err := ListMailDrafts(0)
	if err != nil {
		t.Fatalf("list drafts for account 0: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("account 0 must not list drafts, got %d", len(none))
	}
}

func TestMarkMailFolderReadServerFailureKeepsLocal(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := MailUnreadTotals()
	if err != nil {
		t.Fatal(err)
	}
	f.failOn = "mark all seen"

	if err := MarkMailFolderRead(context.Background(), folder.ID); err == nil {
		t.Fatal("expected the server failure to surface")
	}
	after, _, err := MailUnreadTotals()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) || before[0].Unread != after[0].Unread {
		t.Fatal("a rejected mark-all-read must not clear the local counters")
	}
}

// --- worker ---

func TestMailSyncWorkerTicksEnabledAccountsOnly(t *testing.T) {
	setupMailDB(t)
	enabled := newTestAccount(t)
	disabled, err := CreateMailAccount(MailAccount{
		Name: "停用", Email: "off@example.com", Username: "off@example.com",
		IMAPHost: "imap.example.com", SMTPHost: "smtp.example.com", Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)

	w := &MailSyncWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: time.Hour,
		startup:  0,
	}
	w.tick()

	// Only the enabled account was synced; the disabled one keeps its zeroes.
	on, err := GetMailAccount(enabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	off, err := GetMailAccount(disabled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if on.LastSyncAt == 0 {
		t.Fatal("the enabled account should have been synced")
	}
	if off.LastSyncAt != 0 {
		t.Fatal("a disabled account must be left alone")
	}
}

func TestMailSyncWorkerStartStopIsIdempotent(t *testing.T) {
	setupMailDB(t)

	w := &MailSyncWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: time.Hour,
		startup:  time.Hour,
	}
	w.start()
	w.start() // second start must not spawn a second loop
	w.stop()
	w.stop() // stop is idempotent too

	// The global wrapper is a no-op when already running and safe to call twice.
	StartMailSyncWorker()
	StartMailSyncWorker()
	StopMailSyncWorker()
	StopMailSyncWorker()
}

// listAccountMessages reads the cached messages of an account through its
// folder: the message filter is folder-scoped on purpose (the list endpoint is
// always opened from a folder).
func listAccountMessages(t *testing.T, accountID int64) ([]MailMessage, error) {
	t.Helper()
	folders, err := ListMailFolders(accountID)
	if err != nil {
		return nil, err
	}
	var out []MailMessage
	for _, f := range folders {
		msgs, err := ListMailMessages(MailMessageFilter{FolderID: f.ID, Limit: 200})
		if err != nil {
			return nil, err
		}
		out = append(out, msgs...)
	}
	return out, nil
}

// --- fakes ---

type captureSender struct {
	sent []mail.SendRequest
}

func (c *captureSender) Send(_ context.Context, _ mail.Account, req mail.SendRequest) ([]byte, error) {
	c.sent = append(c.sent, req)
	return []byte("From: me@example.com\r\n\r\n"), nil
}

type failingSender struct{}

func (failingSender) Send(context.Context, mail.Account, mail.SendRequest) ([]byte, error) {
	return nil, errors.New("smtp rejected")
}
