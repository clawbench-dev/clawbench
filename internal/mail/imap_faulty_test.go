package mail

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

// faultyIMAP is a scripted IMAP server. The in-memory server used by the other
// tests always answers correctly, so it cannot exercise the failures this
// package has to survive: a malformed BODYSTRUCTURE (go-imap kills the
// connection), a STATUS reply without UIDVALIDITY, and a server that does not
// implement UIDPLUS or MOVE.
type faultyIMAP struct {
	t  *testing.T
	ln net.Listener

	caps []string
	uids []uint32

	uidValidity           uint32
	uidNext               uint32
	omitStatusUIDValidity bool
	// malformedBodyStructure makes every BODYSTRUCTURE item unparsable, which
	// is what a non-compliant provider does to go-imap.
	malformedBodyStructure bool
	// dropOnFetch closes the socket when the first FETCH arrives, i.e. a plain
	// transport failure with no malformed response involved.
	dropOnFetch bool
	// deletedUIDs is what `UID SEARCH DELETED` answers.
	deletedUIDs []uint32
	// failCommands makes the server answer NO to the listed commands. The
	// names are the normalized ones the switch below uses ("UID STORE",
	// "STORE", "UID COPY", "UID EXPUNGE", "EXPUNGE", "APPEND"). It is how the
	// client's error paths are reached: a server that refuses the command must
	// surface as an error, not as a silently ignored flag change.
	failCommands map[string]bool
	// appended collects the mailboxes an APPEND targeted, in order.
	appended []string

	mu            sync.Mutex
	fetches       []string // fetch option lists, in order
	uidExpunges   []string
	blankExpunges int
	copies        []string
	connections   int
}

func startFaultyIMAP(t *testing.T, srv *faultyIMAP) (*faultyIMAP, Account) {
	t.Helper()
	srv.t = t
	if len(srv.caps) == 0 {
		srv.caps = []string{"IMAP4rev1"}
	}
	if len(srv.uids) == 0 {
		srv.uids = []uint32{101, 102, 103}
	}
	if srv.uidValidity == 0 {
		srv.uidValidity = 42
	}
	if srv.uidNext == 0 {
		srv.uidNext = srv.uids[len(srv.uids)-1] + 1
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.ln = ln
	go srv.serve()
	t.Cleanup(func() { _ = ln.Close() })

	addr := ln.Addr().(*net.TCPAddr)
	return srv, Account{
		Username:     "tester",
		Password:     "secret",
		IMAPHost:     "127.0.0.1",
		IMAPPort:     addr.Port,
		IMAPSecurity: SecurityNone,
	}
}

func (s *faultyIMAP) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.connections++
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *faultyIMAP) handle(conn net.Conn) {
	defer conn.Close()
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	fmt.Fprintf(w, "* OK [CAPABILITY %s] scripted server ready\r\n", strings.Join(s.caps, " "))
	_ = w.Flush()

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag, cmd := fields[0], strings.ToUpper(fields[1])
		if cmd == "UID" && len(fields) >= 3 {
			cmd = "UID " + strings.ToUpper(fields[2])
			fields = append(fields[:2], fields[2:]...)
		}
		if cmd == "APPEND" {
			// The literal has to be consumed before anything else: leaving it
			// in the socket would make the next command line unreadable.
			s.handleAppend(w, r, tag, fields)
			continue
		}
		if s.failCommands[cmd] {
			fmt.Fprintf(w, "%s NO %s refused\r\n", tag, cmd)
			_ = w.Flush()
			continue
		}
		switch cmd {
		case "CAPABILITY":
			fmt.Fprintf(w, "* CAPABILITY %s\r\n%s OK CAPABILITY completed\r\n", strings.Join(s.caps, " "), tag)
		case "LOGIN":
			fmt.Fprintf(w, "%s OK LOGIN completed\r\n", tag)
		case "NOOP":
			fmt.Fprintf(w, "%s OK NOOP completed\r\n", tag)
		case "LOGOUT":
			fmt.Fprintf(w, "* BYE logging out\r\n%s OK LOGOUT completed\r\n", tag)
			_ = w.Flush()
			return
		case "LIST":
			fmt.Fprintf(w, "* LIST (\\HasNoChildren) \"/\" \"INBOX\"\r\n%s OK LIST completed\r\n", tag)
		case "STATUS":
			s.writeStatus(w, tag, fields)
		case "SELECT", "EXAMINE":
			s.writeSelect(w, tag)
		case "FETCH", "UID FETCH":
			s.mu.Lock()
			drop := s.dropOnFetch && len(s.fetches) == 0
			s.mu.Unlock()
			if drop {
				return
			}
			s.writeFetch(w, tag, fields)
		case "UID SEARCH":
			s.writeSearch(w, tag)
		case "UID STORE":
			fmt.Fprintf(w, "%s OK STORE completed\r\n", tag)
		case "UID COPY":
			s.mu.Lock()
			s.copies = append(s.copies, line)
			s.mu.Unlock()
			fmt.Fprintf(w, "%s OK COPY completed\r\n", tag)
		case "UID EXPUNGE":
			s.mu.Lock()
			s.uidExpunges = append(s.uidExpunges, line)
			s.mu.Unlock()
			fmt.Fprintf(w, "%s OK EXPUNGE completed\r\n", tag)
		case "EXPUNGE":
			s.mu.Lock()
			s.blankExpunges++
			s.mu.Unlock()
			fmt.Fprintf(w, "%s OK EXPUNGE completed\r\n", tag)
		default:
			fmt.Fprintf(w, "%s OK %s completed\r\n", tag, cmd)
		}
		_ = w.Flush()
	}
}

// handleAppend answers an APPEND command: it consumes the literal and either
// stores the message or refuses it, depending on failCommands.
func (s *faultyIMAP) handleAppend(w *bufio.Writer, r *bufio.Reader, tag string, fields []string) {
	mailbox := "INBOX"
	if len(fields) > 2 {
		mailbox = strings.Trim(fields[2], "\"")
	}
	// The literal size is the last "{n}" on the command line.
	size := 0
	for _, f := range fields {
		if !strings.HasPrefix(f, "{") {
			continue
		}
		n, err := strconv.Atoi(strings.Trim(strings.TrimSuffix(strings.TrimPrefix(f, "{"), "}"), "+"))
		if err == nil {
			size = n
		}
	}
	if size > 0 {
		// IMAP asks the server for permission before the literal: the client
		// sends the command line and waits for a continuation response.
		fmt.Fprintf(w, "+ Ready for literal data\r\n")
		_ = w.Flush()
		buf := make([]byte, size)
		if _, err := io.ReadFull(r, buf); err != nil {
			fmt.Fprintf(w, "%s NO APPEND truncated\r\n", tag)
			_ = w.Flush()
			return
		}
		// The literal is followed by CRLF.
		_, _ = r.Discard(2)
	}
	if s.failCommands["APPEND"] {
		fmt.Fprintf(w, "%s NO APPEND refused\r\n", tag)
		_ = w.Flush()
		return
	}
	s.mu.Lock()
	s.appended = append(s.appended, mailbox)
	s.mu.Unlock()
	fmt.Fprintf(w, "%s OK APPEND completed\r\n", tag)
	_ = w.Flush()
}

// appendedMailboxes returns the mailboxes an APPEND targeted, in order.
func (s *faultyIMAP) appendedMailboxes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.appended...)
}

func (s *faultyIMAP) writeStatus(w *bufio.Writer, tag string, fields []string) {
	// fields: TAG STATUS mailbox (MESSAGES UNSEEN UIDNEXT UIDVALIDITY)
	mailbox := "INBOX"
	if len(fields) > 2 {
		mailbox = strings.Trim(fields[2], "\"")
	}
	items := []string{"MESSAGES " + strconv.Itoa(len(s.uids)), "UNSEEN 1", "UIDNEXT " + strconv.FormatUint(uint64(s.uidNext), 10)}
	if !s.omitStatusUIDValidity {
		items = append(items, "UIDVALIDITY "+strconv.FormatUint(uint64(s.uidValidity), 10))
	}
	fmt.Fprintf(w, "* STATUS \"%s\" (%s)\r\n%s OK STATUS completed\r\n", mailbox, strings.Join(items, " "), tag)
}

func (s *faultyIMAP) writeSelect(w *bufio.Writer, tag string) {
	fmt.Fprintf(w, "* %d EXISTS\r\n* 0 RECENT\r\n", len(s.uids))
	fmt.Fprintf(w, "* OK [UIDVALIDITY %d] UIDs valid\r\n", s.uidValidity)
	fmt.Fprintf(w, "* OK [UIDNEXT %d] predicted next UID\r\n", s.uidNext)
	fmt.Fprintf(w, "%s OK [READ-WRITE] SELECT completed\r\n", tag)
}

func (s *faultyIMAP) writeFetch(w *bufio.Writer, tag string, fields []string) {
	// fields: TAG UID FETCH <set> (<items>)
	opts := ""
	if len(fields) >= 3 {
		opts = strings.Join(fields[2:], " ")
	}
	s.mu.Lock()
	s.fetches = append(s.fetches, opts)
	s.mu.Unlock()

	var set string
	if len(fields) >= 4 {
		set = fields[3]
	}
	wantStructure := strings.Contains(strings.ToUpper(opts), "BODYSTRUCTURE")
	seq := 0
	for _, uid := range s.uids {
		seq++
		if !uidSetMatches(set, uid) {
			continue
		}
		header := fmt.Sprintf("From: sender%d@example.com\r\nSubject: subject %d\r\nMessage-ID: <m%d@example.com>\r\n\r\n", uid, uid, uid)
		var b strings.Builder
		fmt.Fprintf(&b, "* %d FETCH (UID %d FLAGS () RFC822.SIZE %d INTERNALDATE \"01-Jan-2024 00:00:00 +0000\"",
			seq, uid, len(header)+10)
		if wantStructure {
			if s.malformedBodyStructure {
				// Not a valid body structure: go-imap's strict decoder aborts
				// here and closes the connection.
				b.WriteString(" BODYSTRUCTURE (garbage)")
			} else {
				b.WriteString(` BODYSTRUCTURE ("text" "plain" ("charset" "utf-8") NIL NIL "7bit" 10 1 NIL NIL NIL NIL)`)
			}
		}
		fmt.Fprintf(&b, " BODY[HEADER] {%d}\r\n%s)\r\n", len(header), header)
		_, _ = w.WriteString(b.String())
	}
	fmt.Fprintf(w, "%s OK FETCH completed\r\n", tag)
}

// uidSetMatches understands the "a:b", "a" and comma-separated forms the client
// can send, which is all the tests need.
func uidSetMatches(set string, uid uint32) bool {
	if set == "" {
		return true
	}
	for _, part := range strings.Split(set, ",") {
		lo, hi, isRange := strings.Cut(part, ":")
		start, err := strconv.ParseUint(strings.TrimSpace(lo), 10, 32)
		if err != nil {
			continue
		}
		stop := start
		if isRange {
			v, rerr := strconv.ParseUint(strings.TrimSpace(hi), 10, 32)
			if rerr != nil {
				continue
			}
			stop = v
		}
		if uint64(uid) >= start && uint64(uid) <= stop {
			return true
		}
	}
	return false
}

func (s *faultyIMAP) writeSearch(w *bufio.Writer, tag string) {
	parts := make([]string, 0, len(s.deletedUIDs))
	for _, uid := range s.deletedUIDs {
		parts = append(parts, strconv.FormatUint(uint64(uid), 10))
	}
	fmt.Fprintf(w, "* SEARCH %s\r\n%s OK SEARCH completed\r\n", strings.Join(parts, " "), tag)
}

func (s *faultyIMAP) counts() (blankExpunges int, uidExpunges []string, copies []string, connections int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blankExpunges, append([]string(nil), s.uidExpunges...), append([]string(nil), s.copies...), s.connections
}

func (s *faultyIMAP) fetchOptions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.fetches...)
}

// The decoder's error is the only thing that separates "the server sent a reply
// we could not parse" from "the socket just died". The command's own error is a
// scheduling race — go-imap closes the socket from its read goroutine, so the
// same malformed reply reaches the caller as a parse error on one run and as
// "use of closed network connection" on the next. takeDecoderError must still
// tell the two apart, otherwise the retry that recovers the folder fires
// randomly (and so does the guard that refuses to degrade on a real drop).
func TestTakeDecoderError_DistinguishesMalformedReplyFromDroppedSocket(t *testing.T) {
	ctx := context.Background()
	var set imap.UIDSet
	set.AddRange(imap.UID(1), imap.UID(1000))
	opts := &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		RFC822Size:    true,
		InternalDate:  true,
		BodySection:   []*imap.FetchItemBodySection{headerSectionItem()},
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}

	t.Run("malformed reply", func(t *testing.T) {
		_, acc := startFaultyIMAP(t, &faultyIMAP{malformedBodyStructure: true})
		c, err := DialIMAP(ctx, acc)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		cl := c.(*imapClient)
		if _, err := cl.selectFolder(ctx, "INBOX"); err != nil {
			t.Fatalf("select: %v", err)
		}
		// The command fails either way; which error it reports is the race.
		_, _ = cl.c.Fetch(set, opts).Collect()

		derr := cl.takeDecoderError()
		if derr == nil || isTransportFailure(derr) {
			t.Fatalf("a malformed reply must leave a parse error behind, got %v", derr)
		}
	})

	t.Run("no client yet", func(t *testing.T) {
		// The guard exists because classifyFetchError runs on a client whose
		// connection may already have been dropped by a failed dial.
		if derr := (&imapClient{}).takeDecoderError(); derr != nil {
			t.Fatalf("a client without a connection has no decoder error, got %v", derr)
		}
	})

	t.Run("dropped socket", func(t *testing.T) {
		_, acc := startFaultyIMAP(t, &faultyIMAP{dropOnFetch: true})
		c, err := DialIMAP(ctx, acc)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer c.Close()
		cl := c.(*imapClient)
		if _, err := cl.selectFolder(ctx, "INBOX"); err != nil {
			t.Fatalf("select: %v", err)
		}
		_, _ = cl.c.Fetch(set, opts).Collect()

		// A dropped socket must not leave a DECODER error behind. Which shape
		// that takes depends on how the kernel delivers the peer's close: a FIN
		// leaves go-imap's decoder error nil (the read loop breaks on EOF
		// without recording it), while a RST (the server closed with the request
		// still unread, which is what returning from the handler does here)
		// surfaces as *net.OpError{ECONNRESET}. Both mean "nothing was parsed
		// wrong", which is exactly the distinction classifyFetchError draws, so
		// asserting nil-only made this case depend on a scheduling detail
		// instead of on behavior.
		if derr := cl.takeDecoderError(); derr != nil && !isTransportFailure(derr) {
			t.Fatalf("a clean drop must leave no decoder error, got %v", derr)
		}
	})
}

// The CI variant of the same event: the command reports the dead socket
// ("use of closed network connection") even though it was the decoder that
// killed it. Classifying on the command's error alone would call this a plain
// transport failure, and the folder would never be retried.
func TestClassifyFetchError_DeadSocketAfterMalformedReplyIsADecodeError(t *testing.T) {
	ctx := context.Background()
	_, acc := startFaultyIMAP(t, &faultyIMAP{malformedBodyStructure: true})
	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	cl := c.(*imapClient)
	if _, err := cl.selectFolder(ctx, "INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}

	var set imap.UIDSet
	set.AddRange(imap.UID(1), imap.UID(1000))
	opts := &imap.FetchOptions{
		UID:           true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}
	_, _ = cl.c.Fetch(set, opts).Collect() // the decoder dies here

	// Exactly what the racing write reports in CI.
	writeErr := &net.OpError{Op: "write", Net: "tcp", Err: net.ErrClosed}
	var decode *decodeError
	if got := cl.classifyFetchError(writeErr); !errors.As(got, &decode) {
		t.Fatalf("a dead socket after a malformed reply must still be a decode error, got %v", got)
	}
}

// A malformed BODYSTRUCTURE used to cost the whole folder: go-imap closes the
// connection, so the batch (and every folder after it) failed. The client must
// notice, reconnect, and ask again without the item that cannot be parsed.
func TestFetchSurvivesMalformedBodyStructure(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{malformedBodyStructure: true})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	headers, err := c.FetchNewest(ctx, "INBOX", 100)
	if err != nil {
		t.Fatalf("a broken BODYSTRUCTURE must not fail the folder: %v", err)
	}
	if len(headers) != 3 {
		t.Fatalf("expected all 3 messages, got %d", len(headers))
	}
	if headers[0].Subject != "subject 101" {
		t.Fatalf("headers not recovered: %+v", headers[0])
	}
	if _, _, _, conns := srv.counts(); conns < 2 {
		t.Fatalf("expected a reconnect, got %d connection(s)", conns)
	}
	opts := srv.fetchOptions()
	if len(opts) < 2 {
		t.Fatalf("expected a retry fetch, got %d", len(opts))
	}
	if !strings.Contains(strings.ToUpper(opts[0]), "BODYSTRUCTURE") {
		t.Fatalf("first fetch should ask for BODYSTRUCTURE: %q", opts[0])
	}
	if strings.Contains(strings.ToUpper(opts[len(opts)-1]), "BODYSTRUCTURE") {
		t.Fatalf("the retry must drop BODYSTRUCTURE: %q", opts[len(opts)-1])
	}

	// The degradation is per folder and must stick for the rest of the pass:
	// re-probing would cost a dead connection on every fetch.
	before := len(srv.fetchOptions())
	if _, err := c.FetchNewest(ctx, "INBOX", 100); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	for _, opt := range srv.fetchOptions()[before:] {
		if strings.Contains(strings.ToUpper(opt), "BODYSTRUCTURE") {
			t.Fatalf("a degraded folder must not ask for BODYSTRUCTURE again: %q", opt)
		}
	}
}

// STATUS is allowed to omit UIDVALIDITY even when it is requested. Zero silently
// disables the "mailbox was renumbered, drop the cache" check, so the value has
// to come from somewhere else.
func TestStatusFallsBackToSelectForUIDValidity(t *testing.T) {
	_, acc := startFaultyIMAP(t, &faultyIMAP{omitStatusUIDValidity: true, uidValidity: 4242})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	st, err := c.Status(ctx, "INBOX")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.UIDValidity != 4242 {
		t.Fatalf("UIDValidity = %d, want 4242 (STATUS omitted it, EXAMINE has it)", st.UIDValidity)
	}
}

// Deleting one message must never expunge unrelated \Deleted mail. Without
// UIDPLUS a bare EXPUNGE does exactly that, so it may only run when the search
// proves nothing else is flagged.
func TestDeleteWithoutUIDPlusDoesNotExpungeOtherMessages(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{
		uids:        []uint32{101, 102, 103},
		deletedUIDs: []uint32{101, 103}, // 101 belongs to somebody else
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
		t.Fatalf("a bare EXPUNGE would have removed UID 101 as well, got %d", blank)
	}
	if len(uidExpunges) != 0 {
		t.Fatalf("UIDPLUS is not advertised, no UID EXPUNGE expected: %v", uidExpunges)
	}
}

func TestDeleteWithoutUIDPlusExpungesWhenItIsTheOnlyFlagged(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{
		uids:        []uint32{101, 102, 103},
		deletedUIDs: []uint32{103},
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
	blank, _, _, _ := srv.counts()
	if blank != 1 {
		t.Fatalf("the only flagged message can be expunged, got %d bare EXPUNGE(s)", blank)
	}
}

// Move without the MOVE extension used to go through go-imap's fallback, whose
// last step is a bare EXPUNGE — the same data loss as above, on a different
// action.
func TestMoveWithoutMoveCapabilityDoesNotExpungeOtherMessages(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{
		uids:        []uint32{101, 102, 103},
		deletedUIDs: []uint32{101, 103},
	})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.Move(ctx, "INBOX", 103, "Trash"); err != nil {
		t.Fatalf("move: %v", err)
	}
	blank, _, copies, _ := srv.counts()
	if len(copies) != 1 || !strings.Contains(copies[0], "Trash") {
		t.Fatalf("expected a COPY to Trash, got %v", copies)
	}
	if blank != 0 {
		t.Fatalf("a bare EXPUNGE would have removed UID 101 as well, got %d", blank)
	}
}

// A connection that simply drops is not a malformed response. Treating it as
// one would reconnect and re-fetch without BODYSTRUCTURE, permanently losing
// the attachment flags of the folder for the rest of the pass.
func TestTransportFailureIsNotTreatedAsMalformedResponse(t *testing.T) {
	srv, acc := startFaultyIMAP(t, &faultyIMAP{dropOnFetch: true})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if _, err := c.FetchNewest(ctx, "INBOX", 10); err == nil {
		t.Fatal("a dropped connection should surface as an error")
	}
	if opts := srv.fetchOptions(); len(opts) != 0 {
		t.Fatalf("the fetch was dropped, nothing should have been recorded: %v", opts)
	}
	if _, _, _, conns := srv.counts(); conns != 1 {
		t.Fatalf("a dropped connection must not be retried as a malformed response, got %d connection(s)", conns)
	}
}

// Sparse UIDs are why a fixed window fails. With UIDs millions apart, a window
// of n*3 matches nothing, and "nothing" reads as "there is no older mail".
func TestFetchNewestFindsMessagesWithSparseUIDs(t *testing.T) {
	uids := []uint32{100, 2000000, 4000000}
	srv, acc := startFaultyIMAP(t, &faultyIMAP{uids: uids, uidNext: 4000001})
	ctx := context.Background()

	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	headers, err := c.FetchNewest(ctx, "INBOX", 2)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(headers) != 2 {
		t.Fatalf("expected the 2 newest messages, got %d (%+v)", len(headers), headers)
	}
	if headers[0].UID != 2000000 || headers[1].UID != 4000000 {
		t.Fatalf("wrong messages: %d, %d", headers[0].UID, headers[1].UID)
	}
	// One fetch is enough: the window is sized from the UID density, not by
	// widening until it happens to hit something.
	if opts := srv.fetchOptions(); len(opts) != 1 {
		t.Fatalf("expected a single fetch, got %d: %v", len(opts), opts)
	}
}

// uidWindow is what makes pagination work on mailboxes whose UIDs are sparse.
func TestUIDWindowScalesWithUIDDensity(t *testing.T) {
	dense := &imapClient{uidNext: 101, numMessages: 100}
	if got := dense.uidWindow(50, 1000); got != 150 {
		t.Fatalf("dense mailbox window = %d, want 150", got)
	}

	// UIDNEXT ≈ 2.05e9 with 500 messages: one message every ~4M UIDs. A window
	// of n*3 would match nothing at all.
	sparse := &imapClient{uidNext: 2054349183, numMessages: 500}
	if got := sparse.uidWindow(50, 2054349183); got < 50*4000000 {
		t.Fatalf("sparse mailbox window = %d, want at least %d", got, 50*4000000)
	}

	// The window can never reach past the first message.
	if got := dense.uidWindow(50, 20); got != 20 {
		t.Fatalf("window must clamp to beforeUID, got %d", got)
	}
}

func TestUIDRangeWithin(t *testing.T) {
	set := imap.UIDSetNum(103)
	if !uidRangeWithin(set, imap.UIDRange{Start: 103, Stop: 103}) {
		t.Fatal("a single UID covers itself")
	}
	if uidRangeWithin(set, imap.UIDRange{Start: 101, Stop: 101}) {
		t.Fatal("a different UID is not covered")
	}
}

func TestFaultyServerHelperUsesQuotedMailbox(t *testing.T) {
	// Guard the scripted server itself: if STATUS parsing broke, the UIDVALIDITY
	// fallback test would pass for the wrong reason (the mailbox name is quoted).
	_, acc := startFaultyIMAP(t, &faultyIMAP{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialIMAP(ctx, acc)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Status(ctx, "INBOX"); err != nil {
		t.Fatalf("status: %v", err)
	}
}
