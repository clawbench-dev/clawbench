package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestIMAPFolderAndStatusOps walks the folder/status half of the client
// against the in-memory server: what a sync pass does before it ever fetches a
// message.
func TestIMAPFolderAndStatusOps(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	folders, err := c.ListFolders(ctx)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if len(folders) == 0 {
		t.Fatal("expected the seeded folders")
	}
	// INBOX must sort first: it is what the panel opens by default.
	if folders[0].Path != "INBOX" {
		t.Fatalf("INBOX should rank first, got %+v", folders)
	}

	st, err := c.Status(ctx, "INBOX")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.UIDValidity == 0 {
		t.Fatal("UIDVALIDITY is what detects a reset; it must be reported")
	}

	// An unknown folder is a normal failure, not a panic.
	if _, err := c.Status(ctx, "NoSuchFolder"); err == nil {
		t.Fatal("expected an error for an unknown folder")
	}
}

func TestIMAPFetchAndMutateOps(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	for i, subj := range []string{"第一封", "第二封", "第三封"} {
		if err := c.Append(ctx, "INBOX", rawMail(subj, "alice@example.com", "body"), false); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	newest, err := c.FetchNewest(ctx, "INBOX", 2)
	if err != nil {
		t.Fatalf("fetch newest: %v", err)
	}
	if len(newest) != 2 {
		t.Fatalf("expected the newest two, got %d", len(newest))
	}
	if newest[0].Subject != "第二封" || newest[1].Subject != "第三封" {
		t.Fatalf("unexpected order/content: %+v", newest)
	}
	// Headers come from the header block, so the sender must be decoded.
	if newest[0].FromAddr != "alice@example.com" {
		t.Fatalf("from addr = %q", newest[0].FromAddr)
	}

	byUID, err := c.FetchHeaders(ctx, "INBOX", []uint32{newest[0].UID})
	if err != nil {
		t.Fatalf("fetch headers: %v", err)
	}
	if len(byUID) != 1 || byUID[0].UID != newest[0].UID {
		t.Fatalf("unexpected header result: %+v", byUID)
	}

	before, err := c.FetchBefore(ctx, "INBOX", newest[1].UID, 10)
	if err != nil {
		t.Fatalf("fetch before: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected the older messages")
	}
	for _, h := range before {
		if h.UID >= newest[1].UID {
			t.Fatalf("FetchBefore returned a newer message: %+v", h)
		}
	}

	raw, err := c.FetchRaw(ctx, "INBOX", newest[0].UID)
	if err != nil {
		t.Fatalf("fetch raw: %v", err)
	}
	if !strings.Contains(string(raw), "Subject:") {
		t.Fatalf("raw message looks wrong: %q", raw)
	}

	batch, err := c.FetchRawBatch(ctx, "INBOX", []uint32{newest[0].UID, newest[1].UID})
	if err != nil {
		t.Fatalf("fetch raw batch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("expected both messages, got %d", len(batch))
	}

	// Flags: add \Seen, then take it away again.
	if err := c.StoreFlags(ctx, "INBOX", newest[0].UID, []string{"\\Seen"}, nil); err != nil {
		t.Fatalf("store flags: %v", err)
	}
	after, err := c.FetchHeaders(ctx, "INBOX", []uint32{newest[0].UID})
	if err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if !after[0].Seen {
		t.Fatal("\\Seen was not applied")
	}
	if err := c.StoreFlags(ctx, "INBOX", newest[0].UID, nil, []string{"\\Seen"}); err != nil {
		t.Fatalf("clear flags: %v", err)
	}

	// Move into Trash, then expunge from there.
	if err := c.Move(ctx, "INBOX", newest[0].UID, "Trash"); err != nil {
		t.Fatalf("move: %v", err)
	}
	trashNewest, err := c.FetchNewest(ctx, "Trash", 10)
	if err != nil {
		t.Fatalf("fetch trash: %v", err)
	}
	if len(trashNewest) != 1 {
		t.Fatalf("the message did not arrive in Trash: %+v", trashNewest)
	}
	if err := c.Delete(ctx, "Trash", trashNewest[0].UID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if err := c.MarkAllSeen(ctx, "INBOX"); err != nil {
		t.Fatalf("mark all seen: %v", err)
	}
	remaining, err := c.FetchNewest(ctx, "INBOX", 10)
	if err != nil {
		t.Fatalf("refetch inbox: %v", err)
	}
	for _, h := range remaining {
		if !h.Seen {
			t.Fatalf("message still unseen after mark-all-seen: %+v", h)
		}
	}
}

func TestIMAPAppendHonoursSeenFlag(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// The Sent copy is appended already-read: the user wrote it.
	if err := c.Append(ctx, "Sent", rawMail("已发送", "me@example.com", "x"), true); err != nil {
		t.Fatalf("append: %v", err)
	}
	sent, err := c.FetchNewest(ctx, "Sent", 5)
	if err != nil {
		t.Fatalf("fetch sent: %v", err)
	}
	if len(sent) != 1 || !sent[0].Seen {
		t.Fatalf("appended message should be seen: %+v", sent)
	}
}

func TestIMAPCloseIsIdempotent(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()

	c, err := DialIMAP(context.Background(), acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// A deferred Close runs after the explicit one in real call sites; a
	// second close must not panic or report a spurious error.
	if err := c.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestDialIMAPClassifiesAuthFailure(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	acc.Password = "wrong"

	_, err := DialIMAP(context.Background(), acc)
	if err == nil {
		t.Fatal("expected the login to fail")
	}
	if KindOf(err) != KindAuth {
		t.Fatalf("expected auth kind, got %v (%v)", KindOf(err), err)
	}
}

func TestDialIMAPClassifiesConnectFailure(t *testing.T) {
	// Port 1 on loopback is closed in practice; the point is that the failure
	// is classified as a connection problem rather than a server error.
	_, err := DialIMAP(context.Background(), Account{
		Username: "u", Password: "p", IMAPHost: "127.0.0.1", IMAPPort: 1,
		IMAPSecurity: SecurityNone,
	})
	if err == nil {
		t.Fatal("expected the dial to fail")
	}
	if kind := KindOf(err); kind != KindConnect && kind != KindTimeout {
		t.Fatalf("expected a connect/timeout kind, got %v (%v)", kind, err)
	}
}

func TestDialIMAPTLSAgainstPlainServerIsClassified(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	// The in-memory server speaks plaintext; asking for TLS must fail at the
	// handshake rather than hang.
	acc.IMAPSecurity = SecurityTLS

	done := make(chan error, 1)
	go func() {
		_, err := DialIMAP(context.Background(), acc)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the handshake to fail")
		}
		if kind := KindOf(err); kind != KindTLS && kind != KindConnect && kind != KindServer {
			t.Fatalf("unexpected classification %v (%v)", kind, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DialIMAP did not return: the TLS handshake has no bound")
	}
}

func TestIMAPHelpers(t *testing.T) {
	if got := ParsePort("", 993); got != 993 {
		t.Fatalf("empty port should fall back to the default, got %d", got)
	}
	if got := ParsePort(" 143 ", 993); got != 143 {
		t.Fatalf("ParsePort = %d", got)
	}
	if got := ParsePort("nope", 993); got != 993 {
		t.Fatalf("a bad port should fall back, got %d", got)
	}
	if got := joinHostPort("imap.qq.com", 993); got != "imap.qq.com:993" {
		t.Fatalf("joinHostPort = %q", got)
	}

	if got := leafName("INBOX/Work/2025", "/"); got != "2025" {
		t.Fatalf("leafName = %q", got)
	}
	if got := leafName("INBOX", "/"); got != "INBOX" {
		t.Fatalf("leafName(no delimiter) = %q", got)
	}

	if got := firstOrEmpty([]string{"a", "b"}); got != "a" {
		t.Fatalf("firstOrEmpty = %q", got)
	}
	if got := firstOrEmpty(nil); got != "" {
		t.Fatalf("firstOrEmpty(nil) = %q", got)
	}

	set := uidSet([]uint32{1, 2, 3})
	if len(set) != 1 {
		t.Fatalf("uidSet should collapse a run into one range: %+v", set)
	}
	if set[0].Start != 1 || set[0].Stop != 3 {
		t.Fatalf("range = %d:%d, want 1:3", set[0].Start, set[0].Stop)
	}
	flags := toFlags([]string{"\\Seen", "\\Flagged"})
	if len(flags) != 2 || flags[0] != "\\Seen" {
		t.Fatalf("toFlags = %+v", flags)
	}
}
