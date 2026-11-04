package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// --- reader: every step of the fetch chain can fail on its own ---

func TestGetMailMessageDetailUnknownMessage(t *testing.T) {
	setupMailDB(t)
	if _, err := GetMailMessageDetail(context.Background(), 999, true); err == nil {
		t.Fatal("expected not-found")
	}
}

func TestFetchBodyFailsWhenFolderOrAccountIsGone(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	installFakeDialer(t, newFakeClient())

	// Folder row removed: the message is unreachable.
	if _, err := store.WriteExec(`DELETE FROM mail_folders WHERE id = ?`, msg.FolderID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMailMessageDetail(context.Background(), msg.ID, false); err == nil {
		t.Fatal("expected a missing folder to surface")
	}
}

func TestFetchBodyFailsWhenAccountIsGone(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	installFakeDialer(t, newFakeClient())

	if _, err := store.WriteExec(`DELETE FROM mail_accounts WHERE id = ?`, acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMailMessageDetail(context.Background(), msg.ID, false); err == nil {
		t.Fatal("expected a missing account to surface")
	}
}

func TestFetchBodyDialFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) {
		return nil, errors.New("dial failed")
	}
	t.Cleanup(func() { mailDialer = prev })

	if _, err := GetMailMessageDetail(context.Background(), msg.ID, false); err == nil {
		t.Fatal("expected the dial failure to surface")
	}
}

func TestFetchBodyParseFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{msg.UID: []byte("not a message")}
	installFakeDialer(t, f)

	if _, err := GetMailMessageDetail(context.Background(), msg.ID, false); err == nil {
		t.Fatal("an undecodable body must be reported, not shown empty")
	}
}

func TestFetchBodySeenPushFailureAborts(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{msg.UID: []byte(
		"From: a@example.com\r\nSubject: s\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n\r\nbody\r\n")}
	f.failOn = "store"
	installFakeDialer(t, f)

	if _, err := GetMailMessageDetail(context.Background(), msg.ID, true); err == nil {
		t.Fatal("a rejected \\Seen push must surface rather than be swallowed")
	}
}

func TestOpenMailAttachmentDanglingFolderAndAccount(t *testing.T) {
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
		t.Fatalf("attachments: %v %d", err, len(atts))
	}

	if _, err := store.WriteExec(`DELETE FROM mail_folders WHERE id = ?`, msg.FolderID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := OpenMailAttachment(context.Background(), atts[0].ID); err == nil {
		t.Fatal("expected a missing folder to surface")
	}
}

func TestOpenMailAttachmentWithoutMatchingPart(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	// The message body no longer contains the attachment the row describes
	// (e.g. the server replaced it): the open must report not-found.
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{msg.UID: []byte(
		"From: a@example.com\r\nSubject: s\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=utf-8\r\n\r\nbody\r\n")}
	installFakeDialer(t, f)
	if err := SaveMailAttachments(msg.ID, []MailAttachment{{
		MessageID: msg.ID, Filename: "gone.pdf", ContentType: "application/pdf", Size: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	atts, _ := ListMailAttachments(msg.ID)

	_, _, err := OpenMailAttachment(context.Background(), atts[0].ID)
	if !errors.Is(err, ErrMailAttachmentNotFound) {
		t.Fatalf("expected ErrMailAttachmentNotFound, got %v", err)
	}
}

func TestOpenMailAttachmentRawFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{msg.UID: []byte("x")}
	installFakeDialer(t, f)
	if err := SaveMailAttachments(msg.ID, []MailAttachment{{
		MessageID: msg.ID, Filename: "a.txt", Size: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	atts, _ := ListMailAttachments(msg.ID)
	f.failOn = "raw"
	if _, _, err := OpenMailAttachment(context.Background(), atts[0].ID); err == nil {
		t.Fatal("expected the raw fetch failure to surface")
	}
}

func TestSameAttachmentMatching(t *testing.T) {
	row := MailAttachment{PartID: "1.2", ContentID: "cid1", Filename: "a.pdf"}
	if !sameAttachment(row, mail.Attachment{PartID: "1.2"}) {
		t.Error("part ids should match")
	}
	if sameAttachment(row, mail.Attachment{PartID: "9"}) {
		t.Error("different part ids must not match")
	}
	byCID := MailAttachment{ContentID: "cid1", Filename: "a.pdf"}
	if !sameAttachment(byCID, mail.Attachment{ContentID: "cid1"}) {
		t.Error("content ids should match when no part id is available")
	}
	byName := MailAttachment{Filename: "a.pdf"}
	if !sameAttachment(byName, mail.Attachment{Filename: "a.pdf"}) {
		t.Error("filenames are the last resort")
	}
	if sameAttachment(byName, mail.Attachment{Filename: "b.pdf"}) {
		t.Error("different filenames must not match")
	}
}

func TestWriteMailAttachmentFileReportsMkdirFailure(t *testing.T) {
	prev := model.DataDir
	// A file where the data dir should be: MkdirAll has to fail.
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	model.DataDir = blocked
	t.Cleanup(func() { model.DataDir = prev })

	if _, err := writeMailAttachmentFile(7, "a.txt", []byte("x")); err == nil {
		t.Fatal("expected the write to fail")
	}
}

func TestStoreMailBodyRecordsInlineWithoutWritingFiles(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)
	raw := []byte("From: a@example.com\r\nSubject: s\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n" +
		"--B\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody\r\n" +
		"--B\r\nContent-Type: image/png\r\nContent-ID: <pic@x>\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\niVBORw0KGgo=\r\n--B--\r\n")

	// writeFiles=false is the prefetch path: metadata only, nothing on disk.
	if err := storeMailBody(msg, raw, false); err != nil {
		t.Fatalf("store body: %v", err)
	}
	atts, err := ListMailAttachments(msg.ID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments: %v %d", err, len(atts))
	}
	if atts[0].LocalPath != "" {
		t.Fatalf("prefetch must not write files, got %q", atts[0].LocalPath)
	}
}

// --- store: cascade delete and upsert updates ---

func TestDeleteMailAccountRemovesFoldersMessagesAndDrafts(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	if err := SaveMailAttachments(msg.ID, []MailAttachment{{
		MessageID: msg.ID, Filename: "a.txt", Size: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveMailDraft(MailDraft{AccountID: acc.ID, Subject: "d"}); err != nil {
		t.Fatal(err)
	}

	if err := DeleteMailAccount(acc.ID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, err := GetMailFolder(folder.ID); err == nil {
		t.Error("folders should be gone")
	}
	if _, err := GetMailMessage(msg.ID); err == nil {
		t.Error("messages should be gone")
	}
	drafts, err := ListMailDrafts(acc.ID)
	if err != nil || len(drafts) != 0 {
		t.Errorf("drafts should be gone: %v %d", err, len(drafts))
	}
}

func TestUpsertMailFolderUpdatesInPlace(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	first, err := UpsertMailFolder(MailFolder{AccountID: acc.ID, Path: "INBOX", Name: "INBOX", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := UpsertMailFolder(MailFolder{
		AccountID: acc.ID, Path: "INBOX", Name: "收件箱", Delimiter: "/", SpecialUse: mail.SpecialUseInbox,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("the same path must keep its row: %d vs %d", first, second)
	}
	got, err := GetMailFolder(first)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "收件箱" || got.SpecialUse != mail.SpecialUseInbox {
		t.Fatalf("upsert did not update: %+v", got)
	}
}

func TestUpsertMailMessagesUpdatesExistingRow(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)

	rows, err := UpsertMailMessages([]MailMessage{{
		AccountID: acc.ID, FolderID: folder.ID, UID: msg.UID, MessageID: "m1@example.com",
		Subject: "改过", FromAddr: "boss@example.com", SentAt: msg.SentAt, Size: 42, Seen: true,
	}})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != msg.ID {
		t.Fatalf("a re-sync must update the same row, got %+v", rows)
	}
	if rows[0].Subject != "改过" || !rows[0].Seen {
		t.Fatalf("update not applied: %+v", rows[0])
	}
}

func TestUpdateMailMessageFlagsByUID(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)

	if err := UpdateMailMessageFlagsByUID(folder.ID, msg.UID, true, true, true); err != nil {
		t.Fatalf("update by uid: %v", err)
	}
	got, err := GetMailMessage(msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Seen || !got.Flagged || !got.Answered {
		t.Fatalf("flags not applied: %+v", got)
	}
}

func TestListMailAccountsOrdersBySortOrder(t *testing.T) {
	setupMailDB(t)
	first, err := CreateMailAccount(MailAccount{
		Name: "B", Email: "b@example.com", Username: "b@example.com",
		IMAPHost: "imap", SMTPHost: "smtp", Enabled: true, SortOrder: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateMailAccount(MailAccount{
		Name: "A", Email: "a@example.com", Username: "a@example.com",
		IMAPHost: "imap", SMTPHost: "smtp", Enabled: true, SortOrder: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	accounts, err := ListMailAccounts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != 2 || accounts[0].ID != second.ID || accounts[1].ID != first.ID {
		t.Fatalf("sort_order not applied: %+v", accounts)
	}
}

func TestUpdateMailAccountKeepsPasswordWhenBlank(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)

	acc.Name = "改名"
	acc.Password = ""
	updated, err := UpdateMailAccount(acc)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "改名" {
		t.Fatalf("name not updated: %+v", updated)
	}
	full, err := GetMailAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Password != "secret" {
		t.Fatalf("a blank password must keep the stored one, got %q", full.Password)
	}
}

// --- sync: a dead connection is retried once, then the pass continues ---

func TestSyncReconnectsAfterDeadConnection(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)

	broken := newFakeClient()
	seedFolder(broken, "INBOX", mail.SpecialUseInbox)

	healthy := newFakeClient()
	seedFolder(healthy, "INBOX", mail.SpecialUseInbox)
	seedMessages(healthy, 1, time.Now())

	// The first client reports the connection itself as gone, which is what
	// triggers the re-dial instead of failing every remaining folder.
	dead := &deadClient{fakeMailClient: broken}
	prev := mailDialer
	calls := 0
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) {
		calls++
		if calls == 1 {
			return dead, nil
		}
		return healthy, nil
	}
	t.Cleanup(func() { mailDialer = prev })

	res, err := SyncMailAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if calls < 2 {
		t.Fatalf("a dead connection should be re-dialled, dials = %d", calls)
	}
	// The failed folder is recorded and retried on the next pass; the point of
	// the re-dial is that the rest of the account does not inherit the failure.
	if len(res.Errors) == 0 {
		t.Fatalf("the dead folder should be reported, got %+v", res)
	}
}

// deadClient wraps the fake and makes every status call look like a dropped
// connection.
type deadClient struct {
	*fakeMailClient
}

func (d *deadClient) Status(context.Context, string) (mail.Status, error) {
	return mail.Status{}, &mail.Error{Kind: mail.KindConnect, Err: errors.New("connection reset")}
}
