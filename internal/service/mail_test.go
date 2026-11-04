package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// --- fixture ---

func setupMailDB(t *testing.T) {
	t.Helper()
	db, err := InitInMemoryDB()
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	if _, err := db.Exec(MailDDL); err != nil {
		t.Fatalf("mail ddl: %v", err)
	}
	if _, err := db.Exec(MailTaskTriggerDDL); err != nil {
		t.Fatalf("mail trigger ddl: %v", err)
	}
	cleanup := store.SetDBForTest(db, db)
	prevDataDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
		model.DataDir = prevDataDir
	})
}

func newTestAccount(t *testing.T) MailAccount {
	t.Helper()
	acc, err := CreateMailAccount(MailAccount{
		Name: "工作邮箱", Email: "me@example.com", Username: "me@example.com",
		Password: "secret", IMAPHost: "imap.example.com", IMAPPort: 993,
		IMAPSecurity: "tls", SMTPHost: "smtp.example.com", SMTPPort: 465,
		SMTPSecurity: "tls", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return acc
}

// fakeMailClient is an in-memory mail.Client.
type fakeMailClient struct {
	folders  []mail.Folder
	status   map[string]mail.Status
	headers  map[string][]mail.MessageHeader
	raw      map[string]map[uint32][]byte
	moved    []string
	deleted  []string
	flagOps  []string
	appended []string
	closed   bool
	failOn   string
	// failStatus makes Status fail for specific folders, simulating one broken
	// mailbox among healthy ones.
	failStatus map[string]bool
}

func newFakeClient() *fakeMailClient {
	return &fakeMailClient{
		status:  map[string]mail.Status{},
		headers: map[string][]mail.MessageHeader{},
		raw:     map[string]map[uint32][]byte{},
	}
}

func (f *fakeMailClient) err(op string) error {
	if f.failOn == op {
		return fmt.Errorf("fake failure on %s", op)
	}
	return nil
}

func (f *fakeMailClient) ListFolders(context.Context) ([]mail.Folder, error) {
	if err := f.err("list"); err != nil {
		return nil, err
	}
	return f.folders, nil
}

func (f *fakeMailClient) Status(_ context.Context, folder string) (mail.Status, error) {
	if err := f.err("status"); err != nil {
		return mail.Status{}, err
	}
	if f.failStatus[folder] {
		return mail.Status{}, fmt.Errorf("fake status failure for %s", folder)
	}
	return f.status[folder], nil
}

func (f *fakeMailClient) FetchHeaders(_ context.Context, folder string, uids []uint32) ([]mail.MessageHeader, error) {
	want := map[uint32]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var out []mail.MessageHeader
	for _, h := range f.headers[folder] {
		if want[h.UID] {
			out = append(out, h)
		}
	}
	return out, f.err("fetch")
}

func (f *fakeMailClient) FetchNewest(_ context.Context, folder string, n int) ([]mail.MessageHeader, error) {
	all := f.headers[folder]
	if len(all) > n {
		all = all[len(all)-n:]
	}
	out := append([]mail.MessageHeader(nil), all...)
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out, f.err("fetch")
}

func (f *fakeMailClient) FetchBefore(_ context.Context, folder string, beforeUID uint32, n int) ([]mail.MessageHeader, error) {
	var out []mail.MessageHeader
	for _, h := range f.headers[folder] {
		if h.UID < beforeUID {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, f.err("fetch")
}

func (f *fakeMailClient) FetchRaw(_ context.Context, folder string, uid uint32) ([]byte, error) {
	if err := f.err("raw"); err != nil {
		return nil, err
	}
	if m, ok := f.raw[folder]; ok {
		if b, ok := m[uid]; ok {
			return b, nil
		}
	}
	return nil, fmt.Errorf("no raw message")
}

func (f *fakeMailClient) FetchRawBatch(_ context.Context, folder string, uids []uint32) (map[uint32][]byte, error) {
	out := map[uint32][]byte{}
	for _, u := range uids {
		if b, ok := f.raw[folder][u]; ok {
			out[u] = b
		}
	}
	return out, nil
}

func (f *fakeMailClient) StoreFlags(_ context.Context, folder string, uid uint32, add, remove []string) error {
	f.flagOps = append(f.flagOps, fmt.Sprintf("%s/%d +%v -%v", folder, uid, add, remove))
	for i := range f.headers[folder] {
		if f.headers[folder][i].UID != uid {
			continue
		}
		for _, a := range add {
			if a == "\\Seen" {
				f.headers[folder][i].Seen = true
			}
			if a == "\\Flagged" {
				f.headers[folder][i].Flagged = true
			}
		}
		for _, r := range remove {
			if r == "\\Seen" {
				f.headers[folder][i].Seen = false
			}
		}
	}
	return f.err("store")
}

func (f *fakeMailClient) Move(_ context.Context, folder string, uid uint32, dest string) error {
	f.moved = append(f.moved, fmt.Sprintf("%s/%d->%s", folder, uid, dest))
	return f.err("move")
}

func (f *fakeMailClient) Delete(_ context.Context, folder string, uid uint32) error {
	f.deleted = append(f.deleted, fmt.Sprintf("%s/%d", folder, uid))
	return f.err("delete")
}

func (f *fakeMailClient) Append(_ context.Context, folder string, raw []byte, seen bool) error {
	f.appended = append(f.appended, fmt.Sprintf("%s/%d/%v", folder, len(raw), seen))
	return nil
}

func (f *fakeMailClient) Close() error { f.closed = true; return nil }

// installFakeDialer swaps the package-level dialer for the duration of a test.
func installFakeDialer(t *testing.T, c *fakeMailClient) {
	t.Helper()
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) { return c, nil }
	t.Cleanup(func() { mailDialer = prev })
}

func seedFolder(f *fakeMailClient, path string, special string) {
	f.folders = append(f.folders, mail.Folder{
		Path: path, Name: path, Delimiter: "/", SpecialUse: special, Selectable: true,
	})
}

func seedMessages(f *fakeMailClient, n int, base time.Time) {
	const folder = "INBOX"
	for i := 1; i <= n; i++ {
		uid := uint32(i)
		f.headers[folder] = append(f.headers[folder], mail.MessageHeader{
			UID: uid, Subject: fmt.Sprintf("邮件 %d", i),
			FromName: "Alice", FromAddr: "alice@example.com",
			Date: base.Add(time.Duration(i) * time.Minute),
			Size: 1024, Seen: i%2 == 0,
		})
		if f.raw[folder] == nil {
			f.raw[folder] = map[uint32][]byte{}
		}
		f.raw[folder][uid] = []byte(fmt.Sprintf(
			"From: Alice <alice@example.com>\r\nSubject: 邮件 %d\r\nDate: %s\r\n"+
				"Message-ID: <m%d@example.com>\r\nMIME-Version: 1.0\r\n"+
				"Content-Type: text/plain; charset=utf-8\r\n\r\nbody %d\r\n",
			i, base.Add(time.Duration(i)*time.Minute).Format(time.RFC1123Z), i, i,
		))
	}
	f.status[folder] = mail.Status{
		Messages: uint32(n), Unseen: uint32(n / 2), UIDNext: uint32(n + 1), UIDValidity: 7,
	}
}

// --- account tests ---

func TestMailAccountCRUDAndPasswordMasking(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	if acc.ID == 0 {
		t.Fatal("expected id")
	}
	if !acc.HasPassword {
		t.Fatal("HasPassword should be true")
	}

	list, err := ListMailAccounts()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if list[0].Password != "secret" {
		t.Fatalf("service must load the password for dialing")
	}

	// An empty password on update keeps the stored secret.
	acc.Password = ""
	acc.Name = "改名"
	updated, err := UpdateMailAccount(acc)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "改名" || updated.Password != "secret" {
		t.Fatalf("update kept wrong values: %+v", updated)
	}
}

func TestDeleteMailAccountRemovesCachedRows(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 3, time.Now())
	installFakeDialer(t, f)

	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folders, _ := ListMailFolders(acc.ID)
	if len(folders) != 1 {
		t.Fatalf("folders: %d", len(folders))
	}

	if err := DeleteMailAccount(acc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	left, _ := ListMailAccounts()
	if len(left) != 0 {
		t.Fatalf("account not deleted")
	}
	msgs, _ := ListMailMessages(MailMessageFilter{FolderID: folders[0].ID})
	if len(msgs) != 0 {
		t.Fatalf("messages not deleted: %d", len(msgs))
	}
}

// --- sync tests ---

func TestSyncMailAccountStoresHeadersAndCounters(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedFolder(f, "Sent", mail.SpecialUseSent)
	seedMessages(f, 4, time.Now())
	installFakeDialer(t, f)

	result, err := SyncMailAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Folders != 2 || result.NewMessages != 4 {
		t.Fatalf("result = %+v", result)
	}

	folders, _ := ListMailFolders(acc.ID)
	var inbox MailFolder
	for _, fo := range folders {
		if fo.Path == "INBOX" {
			inbox = fo
		}
	}
	if inbox.Unread != 2 || inbox.Total != 4 {
		t.Fatalf("counters = %+v", inbox)
	}
	if inbox.UIDValidity != 7 || inbox.LastSeenUID != 4 {
		t.Fatalf("uid window = %+v", inbox)
	}

	msgs, err := ListMailMessages(MailMessageFilter{FolderID: inbox.ID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("messages = %d", len(msgs))
	}
	// Newest first.
	if msgs[0].UID != 4 {
		t.Fatalf("order wrong: first uid %d", msgs[0].UID)
	}
	// Prefetch should have filled the snippet of the newest messages.
	if msgs[0].Snippet == "" {
		t.Fatalf("expected prefetched snippet, got empty")
	}

	// A second sync must not duplicate rows.
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	msgs, _ = ListMailMessages(MailMessageFilter{FolderID: inbox.ID})
	if len(msgs) != 4 {
		t.Fatalf("duplicated after resync: %d", len(msgs))
	}
}

func TestSyncClearsCacheWhenUIDValidityChanges(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 3, time.Now())
	installFakeDialer(t, f)

	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folders, _ := ListMailFolders(acc.ID)
	before, _ := ListMailMessages(MailMessageFilter{FolderID: folders[0].ID})
	if len(before) != 3 {
		t.Fatalf("setup: %d", len(before))
	}

	// Server rebuilt the mailbox: same UIDs, different messages.
	f.headers["INBOX"] = nil
	seedMessages(f, 2, time.Now().Add(time.Hour))
	f.status["INBOX"] = mail.Status{Messages: 2, Unseen: 1, UIDNext: 3, UIDValidity: 8}

	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("resync: %v", err)
	}
	after, _ := ListMailMessages(MailMessageFilter{FolderID: folders[0].ID})
	if len(after) != 2 {
		t.Fatalf("stale rows survived uidvalidity change: %d", len(after))
	}
}

func TestListMailMessagesKeysetPaging(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 5, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folders, _ := ListMailFolders(acc.ID)

	page1, err := ListMailMessages(MailMessageFilter{FolderID: folders[0].ID, Limit: 2})
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1: %v %d", err, len(page1))
	}
	last := page1[len(page1)-1]
	page2, err := ListMailMessages(MailMessageFilter{
		FolderID: folders[0].ID, Limit: 2, BeforeAt: last.SentAt, BeforeID: last.ID,
	})
	if err != nil || len(page2) != 2 {
		t.Fatalf("page2: %v %d", err, len(page2))
	}
	for _, m := range page2 {
		if m.ID >= last.ID {
			t.Fatalf("page2 repeated a row: %+v", m)
		}
	}
}

func TestSyncSurvivesOneFolderFailing(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	installFakeDialer(t, f)

	// Make STATUS fail only for the second folder.
	f.folders = append(f.folders, mail.Folder{Path: "Broken", Name: "Broken", Delimiter: "/"})
	f.failStatus = map[string]bool{"Broken": true}

	result, err := SyncMailAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("sync should not fail outright: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected a per-folder error, got %+v", result)
	}
}

// --- body / flags / actions ---

func TestGetMailMessageDetailFetchesBodyAndAttachments(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folders, _ := ListMailFolders(acc.ID)
	msgs, _ := ListMailMessages(MailMessageFilter{FolderID: folders[0].ID})

	detail, err := GetMailMessageDetail(context.Background(), msgs[0].ID, true)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !strings.Contains(detail.BodyText, "body 1") {
		t.Fatalf("body = %q", detail.BodyText)
	}
	if !detail.Seen {
		t.Fatalf("opening should mark the message seen")
	}
	if len(f.flagOps) == 0 {
		t.Fatalf("seen flag was not pushed to the server")
	}

	// Second open must not hit the network again.
	f.raw = map[string]map[uint32][]byte{}
	if _, err := GetMailMessageDetail(context.Background(), msgs[0].ID, true); err != nil {
		t.Fatalf("cached detail: %v", err)
	}
}

func TestSetMailMessageFlagsUpdatesLocalAndServer(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}
	folders, _ := ListMailFolders(acc.ID)
	msgs, _ := ListMailMessages(MailMessageFilter{FolderID: folders[0].ID})
	var unseen MailMessage
	for _, m := range msgs {
		if !m.Seen {
			unseen = m
		}
	}
	if unseen.ID == 0 {
		t.Fatal("expected an unseen message")
	}

	yes := true
	updated, err := SetMailMessageFlags(context.Background(), unseen.ID, MailFlagPatch{Seen: &yes})
	if err != nil {
		t.Fatalf("flags: %v", err)
	}
	if !updated.Seen {
		t.Fatalf("local row not updated")
	}
	// seedMessages marks every even message seen, so exactly one was unread;
	// marking it read takes the folder to zero.
	folder, _ := GetMailFolder(folders[0].ID)
	if folder.Unread != 0 {
		t.Fatalf("unread should have dropped to 0, got %d", folder.Unread)
	}
}

func TestDeleteMailMessageMovesToTrash(t *testing.T) {
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
	folders, _ := ListMailFolders(acc.ID)
	var inbox MailFolder
	for _, fo := range folders {
		if fo.SpecialUse == mail.SpecialUseInbox {
			inbox = fo
		}
	}
	msgs, _ := ListMailMessages(MailMessageFilter{FolderID: inbox.ID})

	if err := DeleteMailMessage(context.Background(), msgs[0].ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.moved) != 1 || !strings.Contains(f.moved[0], "->Trash") {
		t.Fatalf("expected move to Trash, got %+v", f.moved)
	}
}

func TestSendMailAppendsToSent(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "Sent", mail.SpecialUseSent)
	installFakeDialer(t, f)
	// The Sent folder has to be in the local cache before SendMail can find it.
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("sync: %v", err)
	}

	prevSender := mailSender
	mailSender = fakeSender{}
	t.Cleanup(func() { mailSender = prevSender })

	if err := SendMail(context.Background(), MailSendRequest{
		AccountID: acc.ID,
		To:        []string{"bob@example.com"},
		Subject:   "hi",
		Text:      "hello",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(f.appended) != 1 || !strings.HasPrefix(f.appended[0], "Sent/") {
		t.Fatalf("expected append to Sent, got %+v", f.appended)
	}
}

type fakeSender struct{}

func (fakeSender) Send(context.Context, mail.Account, mail.SendRequest) ([]byte, error) {
	return []byte("From: me@example.com\r\nSubject: hi\r\n\r\nhello"), nil
}

func TestSendMailWithoutRecipientsFails(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	err := SendMail(context.Background(), MailSendRequest{AccountID: acc.ID, Subject: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

// --- drafts ---

func TestMailDraftRoundTrip(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)

	saved, err := SaveMailDraft(MailDraft{
		AccountID: acc.ID, To: "bob@example.com", Subject: "草稿",
		BodyText: "内容", Attachments: []MailDraftFile{{Filename: "a.txt", Path: "/tmp/a.txt"}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.ID == 0 || len(saved.Attachments) != 1 {
		t.Fatalf("saved = %+v", saved)
	}

	saved.Subject = "改名"
	updated, err := SaveMailDraft(saved)
	if err != nil || updated.Subject != "改名" {
		t.Fatalf("update: %v %+v", err, updated)
	}

	list, _ := ListMailDrafts(acc.ID)
	if len(list) != 1 {
		t.Fatalf("list = %d", len(list))
	}
	if err := DeleteMailDraft(saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = ListMailDrafts(acc.ID)
	if len(list) != 0 {
		t.Fatalf("draft not deleted")
	}
}

// --- helpers ---

func TestSanitizeMailFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "passwd",
		`C:\tmp\a.txt`:     "C__tmp_a.txt",
		"a<b>c:d.txt":      "a_b_c_d.txt",
		"":                 "attachment",
		"   ":              "attachment",
	}
	for in, want := range cases {
		if got := sanitizeMailFilename(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteMailAttachmentFileAvoidsOverwrite(t *testing.T) {
	dir := t.TempDir()
	prev := model.DataDir
	model.DataDir = dir
	defer func() { model.DataDir = prev }()

	p1, err := writeMailAttachmentFile(42, "a.txt", []byte("one"))
	if err != nil {
		t.Fatalf("write 1: %v", err)
	}
	p2, err := writeMailAttachmentFile(42, "a.txt", []byte("two"))
	if err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("second write overwrote the first: %s", p1)
	}
	b, _ := os.ReadFile(p2)
	if string(b) != "two" {
		t.Fatalf("content = %q", b)
	}
	if filepath.Dir(p1) != filepath.Dir(p2) {
		t.Fatalf("files landed in different dirs")
	}
}
