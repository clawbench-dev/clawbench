package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/model"
)

// appendInboxHeader adds one unseen message to the fake INBOX and keeps the
// folder STATUS counters consistent with it. fakeMailClient.FetchNewest sorts
// by UID, so appending the highest UID is enough to make it "the newest".
func appendInboxHeader(f *fakeMailClient, folder string, uid uint32, subject string, seen bool, sentAt time.Time) {
	f.headers[folder] = append(f.headers[folder], mail.MessageHeader{
		UID: uid, Subject: subject,
		FromName: "Bob", FromAddr: "bob@example.com",
		Date: sentAt, Size: 2048, Seen: seen,
	})
	st := f.status[folder]
	st.Messages++
	if !seen {
		st.Unseen++
	}
	st.UIDNext = uid + 1
	f.status[folder] = st
}

func mailNotifications(t *testing.T) []Notification {
	t.Helper()
	rows, err := ListNotifications(NotificationFilter{Kind: NotificationKindMail})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	return rows
}

// waitForFires polls until the email-trigger hook has recorded `want` fires.
// FireMailTaskTriggers evaluates in a goroutine, so a sync that correctly
// fires would otherwise pass a "nothing fired" test by racing ahead of it.
// With want == 0 it waits a short settling window instead.
func waitForFires(t *testing.T, fired *[]firedMail, want int) {
	t.Helper()
	if want == 0 {
		time.Sleep(50 * time.Millisecond)
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(*fired) < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSyncMailAccount_NotifiesOnlyNewUnseenInboxMail covers the whole gate:
// the first sync is history, a read-elsewhere message is not news, and a
// non-inbox folder is not "new mail".
func TestSyncMailAccount_NotifiesOnlyNewUnseenInboxMail(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedFolder(f, "Sent", mail.SpecialUseSent)
	seedMessages(f, 1, time.Now().Add(-time.Hour))
	appendInboxHeader(f, "Sent", 1, "旧发件", true, time.Now().Add(-time.Hour))
	installFakeDialer(t, f)

	// First pass: the initial window is history, not a notification.
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if rows := mailNotifications(t); len(rows) != 0 {
		t.Fatalf("first sync must not notify, got %d rows", len(rows))
	}

	// A read-elsewhere message arrives: cached, but must not notify.
	appendInboxHeader(f, "INBOX", 2, "已经读过", true, time.Now())
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if rows := mailNotifications(t); len(rows) != 0 {
		t.Fatalf("an already-seen message must not notify, got %d rows", len(rows))
	}

	// A genuinely new unseen message arrives.
	appendInboxHeader(f, "INBOX", 3, "季度报告", false, time.Now().Add(time.Minute))
	// A new message in Sent is not incoming mail.
	appendInboxHeader(f, "Sent", 4, "我发出的", false, time.Now().Add(time.Minute))
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("third sync: %v", err)
	}

	rows := mailNotifications(t)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one mail notification, got %d: %+v", len(rows), rows)
	}
	n := rows[0]
	if n.Title != "季度报告" {
		t.Errorf("title = %q, want the newest subject", n.Title)
	}
	if n.EventType != MailEventReceived {
		t.Errorf("eventType = %q, want %q", n.EventType, MailEventReceived)
	}
	if n.MailMessageID == 0 {
		t.Error("mailMessageId must point at the cached message")
	}
	if !strings.Contains(n.Body, "Bob") {
		t.Errorf("body %q should name the sender", n.Body)
	}

	// Deleting the cached message makes the row stale, so the panel refuses to
	// navigate instead of opening a dead message id.
	if err := DeleteMailMessageRow(n.MailMessageID); err != nil {
		t.Fatalf("delete message: %v", err)
	}
	MarkMissingSources(rows)
	if !rows[0].SourceMissing {
		t.Error("a notification whose message row is gone must be marked sourceMissing")
	}
}

// TestSyncMailAccount_NotifiesFirstMailInAnInitiallyEmptyInbox guards the
// first-sync exception from swallowing real news: syncing an EMPTY inbox leaves
// LastSeenUID at 0, so a "LastSeenUID == 0 means first sync" test would treat
// the first message ever received as history and never announce it.
func TestSyncMailAccount_NotifiesFirstMailInAnInitiallyEmptyInbox(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 0, time.Now())
	installFakeDialer(t, f)

	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("empty sync: %v", err)
	}
	appendInboxHeader(f, "INBOX", 1, "第一封邮件", false, time.Now())
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	rows := mailNotifications(t)
	if len(rows) != 1 || rows[0].Title != "第一封邮件" {
		t.Fatalf("the first message in a previously empty inbox must notify, got %+v", rows)
	}
}

// TestSyncMailAccount_UIDValidityResetDoesNotNotify covers a server that
// rebuilt the mailbox: UIDVALIDITY changes, the cache is cleared and the newest
// window is fetched again. Those re-fetched messages are history just like the
// first sync — they must not raise a "N new messages" alert (and a push) nor
// fire one email task per pre-existing message.
func TestSyncMailAccount_UIDValidityResetDoesNotNotify(t *testing.T) {
	setupMailTriggerDB(t)
	acc := newTestAccount(t)
	insertEmailTask(t, acc.ID, "", "", "", false)
	fired := captureMailFires(t)

	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now().Add(-time.Hour))
	installFakeDialer(t, f)

	// First pass: history, so neither a notification nor a trigger.
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if rows := mailNotifications(t); len(rows) != 0 {
		t.Fatalf("first sync must not notify, got %d rows", len(rows))
	}

	// The server rebuilt the mailbox: new UIDVALIDITY, one unseen message
	// reusing UID 1.
	f.headers["INBOX"] = nil
	seedMessages(f, 1, time.Now())
	f.status["INBOX"] = mail.Status{Messages: 1, Unseen: 1, UIDNext: 2, UIDValidity: 2}

	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("resync: %v", err)
	}

	if rows := mailNotifications(t); len(rows) != 0 {
		t.Fatalf("a UIDVALIDITY reset re-fetches history, got %d notification rows: %+v", len(rows), rows)
	}
	// FireMailTaskTriggers evaluates in a goroutine, so wait until it has
	// settled before asserting nothing reached the trigger hook.
	waitForFires(t, fired, 0)
	if n := len(*fired); n != 0 {
		t.Fatalf("a UIDVALIDITY reset must not fire email tasks, got %d", n)
	}

	// The cache itself is still rebuilt from the new UIDVALIDITY window.
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if folder.UIDValidity != 2 {
		t.Fatalf("folder UIDVALIDITY = %d, want the new 2", folder.UIDValidity)
	}
	msgs, err := ListMailMessages(MailMessageFilter{FolderID: folder.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].UID != 1 {
		t.Fatalf("cache not rebuilt from the new window: %+v", msgs)
	}

	// Positive control: a genuinely new message on top of the rebuilt window
	// must still reach the trigger hook, so the "nothing fired" assertion above
	// is not just observing a hook that nothing ever reaches.
	appendInboxHeader(f, "INBOX", 2, "真正的新邮件", false, time.Now().Add(time.Minute))
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("third sync: %v", err)
	}
	waitForFires(t, fired, 1)
	if n := len(*fired); n != 1 {
		t.Fatalf("a genuinely new message must fire the email task once, got %d", n)
	}
}

// TestSyncMailAccount_MailNotifySwitchSilences covers the switch: with
// mail.notify off the same new mail syncs and counts as unread, but produces
// no notification and no push.
func TestSyncMailAccount_MailNotifySwitchSilences(t *testing.T) {
	setupMailDB(t)
	model.ConfigInstance.Mail.Notify = false
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now().Add(-time.Hour))
	installFakeDialer(t, f)

	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	appendInboxHeader(f, "INBOX", 2, "新的邮件", false, time.Now())
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if rows := mailNotifications(t); len(rows) != 0 {
		t.Fatalf("mail.notify=false must suppress notifications, got %+v", rows)
	}
	// The message itself is still cached: the switch silences the alert, it
	// does not stop fetching.
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := ListMailMessages(MailMessageFilter{FolderID: folder.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 {
		t.Fatalf("new mail must still be cached, got %d messages", len(msgs))
	}
}

// TestRenderMailNotification_BatchesSeveralMessages guards the wording of a
// batch: one title with a count, and a body that still names the newest subject
// and sender rather than dumping every row.
func TestRenderMailNotification_BatchesSeveralMessages(t *testing.T) {
	msgs := []MailMessage{
		{ID: 2, Subject: "最新一封", FromName: "Carol", SentAt: 200},
		{ID: 1, Subject: "较早一封", FromName: "Dave", SentAt: 100},
	}
	title, body := renderMailNotification(msgs)
	wantTitle := mailNotifyT("MailNotifyManyTitle", map[string]any{"Count": 2})
	if title != wantTitle {
		t.Errorf("title = %q, want %q", title, wantTitle)
	}
	if !strings.Contains(body, "最新一封") || !strings.Contains(body, "Carol") {
		t.Errorf("body = %q, want the newest subject and sender", body)
	}
}

// The newest message is the one the click opens, so a tie on SentAt (a batch
// delivered in the same second) must fall back to the higher row id instead of
// whatever order the sync happened to return.
func TestNotifyNewInboxMail_NewestTieBreaksOnID(t *testing.T) {
	setupMailDB(t)
	origEnabled, origNotify := model.ConfigInstance.Mail.Enabled, model.ConfigInstance.Mail.Notify
	model.ConfigInstance.Mail.Enabled, model.ConfigInstance.Mail.Notify = true, true
	t.Cleanup(func() {
		model.ConfigInstance.Mail.Enabled, model.ConfigInstance.Mail.Notify = origEnabled, origNotify
	})

	notifyNewInboxMail(1, mail.Folder{Path: "INBOX", SpecialUse: mail.SpecialUseInbox}, []MailMessage{
		{ID: 5, Subject: "较早", FromName: "", FromAddr: "a@example.com", SentAt: 100},
		{ID: 7, Subject: "较新", FromName: "", FromAddr: "b@example.com", SentAt: 100},
	}, false)

	rows := mailNotifications(t)
	if len(rows) != 1 {
		t.Fatalf("expected one notification, got %d", len(rows))
	}
	if rows[0].MailMessageID != 7 {
		t.Fatalf("notification must point at the newest message (id 7), got %d", rows[0].MailMessageID)
	}
}

// A batch where every message was read elsewhere is not news: nothing may be
// stored, and no title/body is rendered for it.
func TestNotifyNewInboxMail_AllSeenIsNotNews(t *testing.T) {
	setupMailDB(t)
	origEnabled, origNotify := model.ConfigInstance.Mail.Enabled, model.ConfigInstance.Mail.Notify
	model.ConfigInstance.Mail.Enabled, model.ConfigInstance.Mail.Notify = true, true
	t.Cleanup(func() {
		model.ConfigInstance.Mail.Enabled, model.ConfigInstance.Mail.Notify = origEnabled, origNotify
	})

	notifyNewInboxMail(1, mail.Folder{Path: "INBOX"}, []MailMessage{
		{ID: 3, Subject: "已在手机上读过", Seen: true},
	}, false)

	if rows := mailNotifications(t); len(rows) != 0 {
		t.Fatalf("a read-elsewhere message must not notify, got %+v", rows)
	}
}

// The sender line falls back to the address when the display name is missing,
// and to a localized placeholder when the message carries neither.
func TestRenderMailNotification_SenderFallbacks(t *testing.T) {
	title, body := renderMailNotification([]MailMessage{
		{ID: 1, Subject: "主题", FromName: "", FromAddr: "only@example.com"},
	})
	if !strings.Contains(title, "主题") {
		t.Errorf("title = %q, want the subject", title)
	}
	if !strings.Contains(body, "only@example.com") {
		t.Errorf("body = %q, want the address when the name is empty", body)
	}

	title, body = renderMailNotification([]MailMessage{{ID: 2}})
	if title != mailNotifyT("MailNotifyNoSubject") {
		t.Errorf("title = %q, want the no-subject placeholder", title)
	}
	if !strings.Contains(body, mailNotifyT("MailNotifyUnknownSender")) {
		t.Errorf("body = %q, want the unknown-sender placeholder", body)
	}

	// A batch names the newest subject in the body, so the placeholder is used
	// there too.
	_, body = renderMailNotification([]MailMessage{{ID: 3}, {ID: 2}})
	if !strings.Contains(body, mailNotifyT("MailNotifyNoSubject")) {
		t.Errorf("batch body = %q, want the no-subject placeholder", body)
	}
}
