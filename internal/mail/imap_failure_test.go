package mail

import (
	"context"
	"strings"
	"testing"
)

// The client's error paths matter as much as its happy path: a server that
// refuses a command must surface as an error, because the caller decides
// whether to retry, and a silently dropped flag change would leave the local
// cache disagreeing with the server forever.

// A refused STORE must be reported. Otherwise the UI would mark a message read
// (or unread) while the server still shows the old flag, and the next sync
// would silently flip it back.
func TestStoreFlags_ReportsServerRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{failCommands: map[string]bool{"UID STORE": true}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.StoreFlags(ctx, "INBOX", 101, []string{"\\Seen"}, nil); err == nil {
		t.Fatal("a refused STORE must surface as an error")
	}
	if err := c.StoreFlags(ctx, "INBOX", 101, nil, []string{"\\Seen"}); err == nil {
		t.Fatal("a refused STORE (removing flags) must surface as an error")
	}
}

// MarkAllSeen is a bulk STORE, and a refusal there must not be reported as
// "everything is read".
func TestMarkAllSeen_ReportsServerRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{failCommands: map[string]bool{"STORE": true}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.MarkAllSeen(ctx, "INBOX"); err == nil {
		t.Fatal("a refused bulk STORE must surface as an error")
	}
}

// The MOVE fallback copies first and only then flags the original. A refused
// COPY means the message was NOT moved, and the caller has to hear about it.
func TestMove_ReportsCopyRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{failCommands: map[string]bool{"UID COPY": true}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Move(ctx, "INBOX", 101, "Trash"); err == nil {
		t.Fatal("a refused COPY must surface as an error")
	}
}

// A refused STORE in the MOVE fallback means the original was copied but not
// flagged \Deleted, so it was not moved. Reporting success would show the
// message in the trash while it still sits in the inbox.
func TestMove_ReportsStoreRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{failCommands: map[string]bool{"UID STORE": true}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Move(ctx, "INBOX", 101, "Trash"); err == nil {
		t.Fatal("a refused STORE must surface as an error")
	}
}

// A refused STORE in the delete path means the message was never flagged, so
// it was never deleted.
func TestDelete_ReportsStoreRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{failCommands: map[string]bool{"UID STORE": true}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Delete(ctx, "INBOX", 101); err == nil {
		t.Fatal("a refused STORE must surface as an error")
	}
}

// With UIDPLUS the expunge is targeted at one UID. A refusal there must be
// reported — the message is flagged but still on the server.
func TestExpunge_ReportsUIDExpungeRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{
		caps:         []string{"IMAP4rev1", "UIDPLUS"},
		failCommands: map[string]bool{"UID EXPUNGE": true},
	})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Delete(ctx, "INBOX", 101); err == nil {
		t.Fatal("a refused UID EXPUNGE must surface as an error")
	}
}

// Without UIDPLUS the expunge is a bare EXPUNGE, guarded by a search that
// proves only our own message is flagged. A refused EXPUNGE must be reported.
func TestExpunge_ReportsBareExpungeRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{
		uids:         []uint32{101, 102, 103},
		deletedUIDs:  []uint32{103},
		failCommands: map[string]bool{"EXPUNGE": true},
	})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Delete(ctx, "INBOX", 103); err == nil {
		t.Fatal("a refused EXPUNGE must surface as an error")
	}
}

// The sent copy is written with APPEND. A refusal must be reported, and a
// successful APPEND must land in the folder it named.
func TestAppend_ReportsServerRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{failCommands: map[string]bool{"APPEND": true}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Append(ctx, "Sent", []byte("Subject: hi\r\n\r\nbody\r\n"), true); err == nil {
		t.Fatal("a refused APPEND must surface as an error")
	}
}

func TestAppend_StoresTheMessageInTheNamedFolder(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Append(ctx, "Sent", []byte("Subject: hi\r\n\r\nbody\r\n"), true); err != nil {
		t.Fatalf("append: %v", err)
	}
	got := srv.appendedMailboxes()
	if len(got) != 1 || got[0] != "Sent" {
		t.Fatalf("expected the message to be appended to Sent, got %v", got)
	}
}

// With UIDPLUS the expunge is targeted at exactly one UID, and no bare
// EXPUNGE may be issued — a bare one would take every other \Deleted message
// in the mailbox with it.
func TestDeleteWithUIDPlusExpungesOnlyTheTargetedUID(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{
		caps:        []string{"IMAP4rev1", "UIDPLUS"},
		uids:        []uint32{101, 102, 103},
		deletedUIDs: []uint32{101, 103},
	})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Delete(ctx, "INBOX", 103); err != nil {
		t.Fatalf("delete: %v", err)
	}
	blank, uidExpunges, _, _ := srv.counts()
	if blank != 0 {
		t.Fatalf("UIDPLUS is advertised, no bare EXPUNGE may be issued, got %d", blank)
	}
	if len(uidExpunges) != 1 || !strings.Contains(uidExpunges[0], "103") {
		t.Fatalf("expected a UID EXPUNGE of 103, got %v", uidExpunges)
	}
}
