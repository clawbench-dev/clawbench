package mail

import (
	"context"
	"testing"
)

// TestOpenFolder pins the connection probe that proves a provider really lets us
// in. LOGIN and LIST succeed on several providers that then refuse SELECT (a
// mailbox not enabled for IMAP, a folder the account cannot read), and without
// this check the sync worker would report a healthy account and then fetch
// nothing forever.
func TestOpenFolder(t *testing.T) {
	acc, stop := startTestIMAP(t)
	defer stop()
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// A mailbox that exists opens.
	if err := c.OpenFolder(ctx, "INBOX"); err != nil {
		t.Fatalf("OpenFolder(INBOX): %v", err)
	}

	// A mailbox the server does not have is reported, not silently accepted.
	if err := c.OpenFolder(ctx, "NoSuchFolder"); err == nil {
		t.Fatal("expected an error for a missing mailbox")
	} else if kind := KindOf(err); kind != KindServer {
		t.Fatalf("a refused SELECT is a server-side problem, got %v (%v)", kind, err)
	}

	// A cancelled context never reaches the server.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := c.OpenFolder(cancelled, "INBOX"); err == nil {
		t.Fatal("expected a cancelled context to be reported")
	}
}
