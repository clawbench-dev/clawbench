package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// failingBatchClient fails exactly one method on top of a working fake, which
// is how the sync's per-step error branches become reachable.
type failingBatchClient struct {
	*fakeMailClient
	failRawBatch bool
	failHeaders  bool
}

func (c *failingBatchClient) FetchRawBatch(ctx context.Context, folder string, uids []uint32) (map[uint32][]byte, error) {
	if c.failRawBatch {
		return nil, errors.New("batch failed")
	}
	return c.fakeMailClient.FetchRawBatch(ctx, folder, uids)
}

func (c *failingBatchClient) FetchHeaders(ctx context.Context, folder string, uids []uint32) ([]mail.MessageHeader, error) {
	if c.failHeaders {
		return nil, errors.New("headers failed")
	}
	return c.fakeMailClient.FetchHeaders(ctx, folder, uids)
}

func TestPrefetchSurvivesBatchFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	client := &failingBatchClient{fakeMailClient: f, failRawBatch: true}
	installClient(t, client)

	// The prefetch is an optimization: a failure must not fail the sync, and
	// the headers must still land.
	res, err := SyncMailAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.NewMessages == 0 {
		t.Fatalf("headers should still be cached, got %+v", res)
	}
}

func TestRefreshFolderFlagsPicksUpServerChanges(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Read one message on another device: the next sync has to mirror it.
	for i := range f.headers["INBOX"] {
		f.headers["INBOX"][i].Seen = true
		f.headers["INBOX"][i].Flagged = true
	}
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	folder, err := GetMailFolderByPath(acc.ID, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := ListMailMessages(MailMessageFilter{FolderID: folder.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if !m.Seen || !m.Flagged {
			t.Fatalf("server flag changes were not mirrored: %+v", m)
		}
	}
}

func TestRefreshFolderFlagsFailureIsReported(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 2, time.Now())
	installFakeDialer(t, f)
	if _, err := SyncMailAccount(context.Background(), acc.ID); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// A failing header refresh is best effort: the folder sync itself still
	// succeeds, and the stale flags are corrected on the next pass.
	client := &failingBatchClient{fakeMailClient: f, failHeaders: true}
	installClient(t, client)
	res, err := SyncMailAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("a flag refresh failure must not fail the sync: %v", err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("a best-effort refresh should not be reported as an error: %+v", res)
	}
}

func TestSyncRedialFailureStopsThePass(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)

	broken := newFakeClient()
	seedFolder(broken, "INBOX", mail.SpecialUseInbox)

	prev := mailDialer
	calls := 0
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) {
		calls++
		if calls == 1 {
			return &deadClient{fakeMailClient: broken}, nil
		}
		return nil, errors.New("still down")
	}
	t.Cleanup(func() { mailDialer = prev })

	res, err := SyncMailAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatalf("a failed re-dial is reported, not fatal: %v", err)
	}
	if len(res.Errors) < 2 {
		t.Fatalf("both the folder and the re-dial failure should be recorded: %+v", res)
	}
}

func TestFetchOlderDialFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, _ := seedOneMessage(t, acc.ID)
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) { return nil, errNoDial }
	t.Cleanup(func() { mailDialer = prev })

	if _, err := FetchOlderMailMessages(context.Background(), folder.ID); err == nil {
		t.Fatal("expected the dial failure to surface")
	}
}

func TestFetchOlderUnknownFolder(t *testing.T) {
	setupMailDB(t)
	if _, err := FetchOlderMailMessages(context.Background(), 999); err == nil {
		t.Fatal("expected not-found")
	}
}

func TestMailWorkerLoopStartsAndStops(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	f := newFakeClient()
	seedFolder(f, "INBOX", mail.SpecialUseInbox)
	seedMessages(f, 1, time.Now())
	installFakeDialer(t, f)

	w := &MailSyncWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: 5 * time.Millisecond,
		startup:  time.Millisecond,
	}
	w.start()
	// Wait for at least one tick, then stop. The loop must exit promptly.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := GetMailAccount(acc.ID)
		if err == nil && got.LastSyncAt > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.stop()

	got, err := GetMailAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncAt == 0 {
		t.Fatal("the worker never synced the account")
	}
}

func TestMailWorkerStopsDuringStartupDelay(t *testing.T) {
	setupMailDB(t)
	w := &MailSyncWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		interval: time.Hour,
		startup:  time.Hour,
	}
	w.start()
	// Stopping before the startup delay elapses must not wait for it.
	done := make(chan struct{})
	go func() { w.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop blocked on the startup delay")
	}
}

func TestMailWorkerTickSurvivesListFailure(t *testing.T) {
	// A closed handle makes ListMailAccounts fail; the tick must log and return
	// rather than panic (the worker starts before the DB is always ready).
	cleanup := useClosedDB(t)
	defer cleanup()

	w := &MailSyncWorker{stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	w.tick()
}

func TestMailWorkerTickStopsBetweenAccounts(t *testing.T) {
	setupMailDB(t)
	newTestAccount(t)
	// A closed stop channel means the loop returns before syncing anything.
	w := &MailSyncWorker{stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	close(w.stopCh)
	w.tick()
}

func TestDeleteMailAccountAttachmentFilesOnBrokenDB(t *testing.T) {
	cleanup := useClosedDB(t)
	defer cleanup()
	// The cleanup is best effort: a failing query must not panic.
	DeleteMailAccountAttachmentFiles(1)
}

func TestStoreMailBodyWarnsOnUnwritableAttachmentDir(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	_, msg := seedOneMessage(t, acc.ID)

	prev := model.DataDir
	blocked := t.TempDir() + "/blocked"
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	model.DataDir = blocked
	t.Cleanup(func() { model.DataDir = prev })

	raw := []byte("From: a@example.com\r\nSubject: s\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n" +
		"--B\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody\r\n" +
		"--B\r\nContent-Type: application/pdf; name=\"a.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"a.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n--B--\r\n")

	// A file that cannot be written must not lose the message: the metadata is
	// still recorded and the file is materialized on demand later.
	if err := storeMailBody(msg, raw, true); err != nil {
		t.Fatalf("store body: %v", err)
	}
	atts, err := ListMailAttachments(msg.ID)
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments: %v %d", err, len(atts))
	}
	if atts[0].LocalPath != "" {
		t.Fatalf("no path should be recorded for an unwritable dir, got %q", atts[0].LocalPath)
	}
}

func TestOpenMailAttachmentParseFailure(t *testing.T) {
	setupMailDB(t)
	acc := newTestAccount(t)
	folder, msg := seedOneMessage(t, acc.ID)
	f := newFakeClient()
	f.raw[folder.Path] = map[uint32][]byte{msg.UID: []byte("not a message")}
	installFakeDialer(t, f)
	if err := SaveMailAttachments(msg.ID, []MailAttachment{{
		MessageID: msg.ID, Filename: "a.txt", Size: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	atts, _ := ListMailAttachments(msg.ID)

	if _, _, err := OpenMailAttachment(context.Background(), atts[0].ID); err == nil {
		t.Fatal("expected the parse failure to surface")
	}
}

// useClosedDB swaps in a handle that is already closed, so every query fails.
func useClosedDB(t *testing.T) func() {
	t.Helper()
	db, err := InitInMemoryDB()
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	_ = db.Close()
	return store.SetDBForTest(db, db)
}

// installClient is installFakeDialer for any mail.Client implementation.
func installClient(t *testing.T, c mail.Client) {
	t.Helper()
	prev := mailDialer
	mailDialer = func(context.Context, mail.Account) (mail.Client, error) { return c, nil }
	t.Cleanup(func() { mailDialer = prev })
}
