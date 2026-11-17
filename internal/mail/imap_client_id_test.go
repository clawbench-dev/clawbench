package mail

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 163/126 answer every SELECT with "NO SELECT Unsafe Login" until the client
// has declared itself (RFC 2971). The declaration therefore has to arrive
// after LOGIN and before the first SELECT, or no folder can be opened at all.
func TestDialIMAPDeclaresClientIDBeforeSelecting(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{caps: []string{"IMAP4rev1", "ID"}})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if _, err := c.FetchNewest(ctx, "INBOX", 1); err != nil {
		t.Fatalf("fetch newest: %v", err)
	}

	ids := srv.clientIDs()
	if len(ids) != 1 {
		t.Fatalf("expected exactly one ID command, got %d (commands: %v)", len(ids), srv.sentCommands())
	}
	if !strings.Contains(ids[0], "ClawBench") {
		t.Fatalf("the declaration must name the client, got %q", ids[0])
	}

	login, id, selectAt := -1, -1, -1
	for i, cmd := range srv.sentCommands() {
		switch cmd {
		case "LOGIN":
			login = i
		case "ID":
			id = i
		case "SELECT":
			if selectAt < 0 {
				selectAt = i
			}
		}
	}
	if login < 0 || id < 0 || selectAt < 0 {
		t.Fatalf("expected LOGIN, ID and SELECT, got %v", srv.sentCommands())
	}
	if id < login {
		t.Fatalf("ID cannot precede LOGIN: %v", srv.sentCommands())
	}
	if selectAt < id {
		t.Fatalf("ID must precede the first SELECT: %v", srv.sentCommands())
	}
}

// A server that does not advertise ID must not receive one: the command is an
// extension and sending it to everyone is noise at best.
func TestDialIMAPSkipsClientIDWhenNotAdvertised(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{})

	c, err := DialIMAP(context.Background(), acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if ids := srv.clientIDs(); len(ids) != 0 {
		t.Fatalf("no ID capability, but the client declared itself: %v", ids)
	}
}

// A refused ID must not cost the user the connection: the extension is
// optional, so a BAD reply is ignored and the sync proceeds without it.
func TestDialIMAPSurvivesClientIDRefusal(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{
		caps:         []string{"IMAP4rev1", "ID"},
		failCommands: map[string]bool{"ID": true},
	})

	c, err := DialIMAP(context.Background(), acc)
	if err != nil {
		t.Fatalf("a refused optional extension must not fail the connection: %v", err)
	}
	_ = c.Close()
}

// The 163 rejection is a client-identity requirement, not a server fault, and
// the raw reply has to stay readable: the message is what tells the user to
// upgrade.
func TestSelectUnsafeLoginIsReportedAsClientID(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{unsafeLoginOnSelect: true})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, err = c.FetchNewest(ctx, "INBOX", 1)
	if err == nil {
		t.Fatal("expected SELECT to fail")
	}
	if k := KindOf(err); k != KindClientID {
		t.Fatalf("expected the client-id kind, got %v (%v)", k, err)
	}
	if !strings.Contains(err.Error(), "Unsafe Login") {
		t.Fatalf("the server reply must survive classification, got %q", err.Error())
	}
}

// Servers are not required to wrap the rejection in an IMAP error value; the
// text-only path has to classify the same way.
func TestClassifyUnsafeLoginTextIsClientID(t *testing.T) {
	err := classify("select folder", errors.New("select folder: NO SELECT Unsafe Login"))
	if k := KindOf(err); k != KindClientID {
		t.Fatalf("expected the client-id kind, got %v (%v)", k, err)
	}
}
