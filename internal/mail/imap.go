package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cention-sany/utf7"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/jhillyerd/enmime"
)

const (
	dialTimeout = 15 * time.Second
	opTimeout   = 60 * time.Second
	// probeTimeout bounds the liveness probe run after a failed command. It is
	// deliberately much shorter than opTimeout: the probe only decides whether
	// to redial, and a server that has stopped answering should not cost a
	// second full timeout.
	probeTimeout = 5 * time.Second
)

// imapClient wraps imapclient.Client with a deadline-aware connection.
//
// go-imap has no context support: commands block on the socket. We therefore
// keep the net.Conn and push a deadline before every command, which turns a
// hung server into a normal error instead of a permanently stuck sync worker.
type imapClient struct {
	acc  Account
	c    *imapclient.Client
	conn net.Conn

	selected    string
	readOnly    bool
	closed      bool
	uidNext     uint32
	numMessages uint32

	// degraded holds the folders whose full FETCH died on a malformed
	// response. Later fetches for them skip BODYSTRUCTURE, the only item in
	// the request that go-imap parses strictly.
	degraded map[string]bool
}

// DialIMAP connects and logs in. The caller owns the returned client.
func DialIMAP(ctx context.Context, acc Account) (Client, error) {
	m := &imapClient{acc: acc, degraded: make(map[string]bool)}
	if err := m.connect(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *imapClient) connect(ctx context.Context) error {
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", m.acc.IMAPAddr())
	if err != nil {
		return classify("dial", err)
	}

	tlsCfg := &tls.Config{
		ServerName:         m.acc.IMAPHost,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: m.acc.InsecureTLS, // certificate verification is opt-in per account
	}
	opts := &imapclient.Options{
		TLSConfig:   tlsCfg,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}

	var c *imapclient.Client
	switch m.acc.IMAPSecurity {
	case SecurityTLS:
		tlsConn := tls.Client(conn, tlsCfg)
		if herr := tlsConn.HandshakeContext(ctx); herr != nil {
			_ = conn.Close()
			return classify("TLS 握手", herr)
		}
		conn = tlsConn
		c = imapclient.New(conn, opts)
	case SecurityStartTLS:
		c, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			_ = conn.Close()
			return classify("starttls", err)
		}
	default:
		c = imapclient.New(conn, opts)
	}

	if err := c.WaitGreeting(); err != nil {
		_ = c.Close()
		return classify("read greeting", err)
	}
	_ = conn.SetDeadline(time.Now().Add(opTimeout))
	if err := c.Login(m.acc.Username, m.acc.Password).Wait(); err != nil {
		_ = c.Close()
		return classify("login", err)
	}
	m.c = c
	m.conn = conn
	m.selected = ""
	m.readOnly = false
	m.closed = false
	m.uidNext = 0
	m.numMessages = 0
	return nil
}

// reconnect replaces a connection the server has already killed. The old
// socket is dropped without a LOGOUT round trip: it is dead, so the attempt
// would only block until the deadline.
func (m *imapClient) reconnect(ctx context.Context) error {
	if m.c != nil {
		_ = m.c.Close()
	}
	m.c = nil
	m.closed = false
	return m.connect(ctx)
}

func (m *imapClient) Close() error {
	if m.c == nil || m.closed {
		return nil
	}
	// Callers hold `defer client.Close()` and also close explicitly on the
	// failure paths; a second Logout on a dead connection reports "use of
	// closed network connection", which reads like a real problem.
	m.closed = true
	_ = m.conn.SetDeadline(time.Now().Add(2 * time.Second))
	_ = m.c.Logout().Wait()
	return m.c.Close()
}

func (m *imapClient) deadline() {
	_ = m.conn.SetDeadline(time.Now().Add(opTimeout))
}

// fail classifies a command error, promoting it to a connection failure when
// the socket is already gone.
//
// go-imap kills the connection on a malformed response instead of skipping the
// message, so the error that reaches us is the *parse* error while the real
// problem is that there is no connection left. Classifying that as a server
// error made the sync worker keep using the dead client for the rest of the
// pass, losing every remaining folder.
func (m *imapClient) fail(op string, err error) error {
	if err == nil {
		return nil
	}
	if m.connGone() {
		return &Error{Kind: KindConnect, Op: op, Err: err}
	}
	return classify(op, err)
}

// connGone reports whether the connection can no longer carry a command.
//
// The closed channel is checked first because it is free, but it is not enough
// on its own: go-imap completes the pending command before its read goroutine
// closes that channel, so a decode error can be observed a moment early. A
// NOOP settles it — if the connection is healthy it answers immediately, and
// if it is not the probe fails.
func (m *imapClient) connGone() bool {
	if m.c == nil || m.closed {
		return true
	}
	select {
	case <-m.c.Closed():
		return true
	default:
	}
	_ = m.conn.SetDeadline(time.Now().Add(probeTimeout))
	ok := m.c.Noop().Wait() == nil
	m.deadline()
	return !ok
}

// ListFolders returns every selectable mailbox.
func (m *imapClient) ListFolders(ctx context.Context) ([]Folder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.deadline()

	data, err := m.c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		// SPECIAL-USE is optional; retry without it rather than failing the
		// whole sync on a server that rejects the option.
		m.deadline()
		data, err = m.c.List("", "*", nil).Collect()
		if err != nil {
			return nil, m.fail("list folders", err)
		}
	}

	folders := make([]Folder, 0, len(data))
	for _, d := range data {
		if hasAttr(d.Attrs, imap.MailboxAttrNoSelect) || hasAttr(d.Attrs, imap.MailboxAttrNonExistent) {
			continue
		}
		delim := "/"
		if d.Delim != 0 {
			delim = string(d.Delim)
		}
		name, derr := utf7.UTF7Decode(d.Mailbox)
		if derr != nil {
			name = d.Mailbox
		}
		folders = append(folders, Folder{
			Path:       d.Mailbox, // raw wire name (modified UTF-7 for non-ASCII)
			Name:       leafName(name, delim),
			Delimiter:  delim,
			SpecialUse: specialUseOf(d.Attrs, name),
			Selectable: true,
		})
	}
	sort.SliceStable(folders, func(i, j int) bool {
		return folderRank(folders[i]) < folderRank(folders[j])
	})
	return folders, nil
}

func (m *imapClient) Status(ctx context.Context, folder string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	m.deadline()
	data, err := m.c.Status(folder, &imap.StatusOptions{
		NumMessages: true,
		NumUnseen:   true,
		UIDNext:     true,
		UIDValidity: true,
	}).Wait()
	if err != nil {
		return Status{}, m.fail("status", err)
	}
	st := Status{UIDNext: uint32(data.UIDNext), UIDValidity: data.UIDValidity}
	if data.NumMessages != nil {
		st.Messages = *data.NumMessages
	}
	if data.NumUnseen != nil {
		st.Unseen = *data.NumUnseen
	}
	if st.UIDValidity == 0 {
		st.UIDValidity = m.uidValidityViaExamine(ctx, folder)
	}
	return st, nil
}

// uidValidityViaExamine reads UIDVALIDITY out of a SELECT response.
//
// Some servers accept `STATUS … (UIDVALIDITY)` and answer without the item
// (measured on a Chinese provider), which leaves the field at zero. Zero is not
// a harmless value: the sync compares it against the cached one to notice that
// the mailbox was renumbered, so a permanently-zero field silently disables
// that reset and every cached UID keeps pointing at a different message.
// EXAMINE reports the value without disturbing the selected mailbox's flags.
func (m *imapClient) uidValidityViaExamine(ctx context.Context, folder string) uint32 {
	if err := ctx.Err(); err != nil {
		return 0
	}
	m.deadline()
	data, err := m.c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		// Best effort: a missing UIDVALIDITY is not worth failing the whole
		// folder over, and the caller's counters are still valid.
		m.deadline()
		return 0
	}
	m.selected = folder
	m.readOnly = true
	m.uidNext = uint32(data.UIDNext)
	m.numMessages = data.NumMessages
	return data.UIDValidity
}

func (m *imapClient) selectFolder(ctx context.Context, folder string) (*imap.SelectData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A read-only selection is not reusable for anything that writes, so it
	// only short-circuits when write access is what we already have.
	if m.selected == folder && !m.readOnly {
		return nil, nil
	}
	m.deadline()
	data, err := m.c.Select(folder, nil).Wait()
	if err != nil {
		return nil, m.fail("select folder", err)
	}
	m.selected = folder
	m.readOnly = false
	m.uidNext = uint32(data.UIDNext)
	m.numMessages = data.NumMessages
	return data, nil
}

func (m *imapClient) FetchHeaders(ctx context.Context, folder string, uids []uint32) ([]MessageHeader, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	return m.fetchHeadersBySet(ctx, folder, uidSet(uids))
}

func (m *imapClient) FetchNewest(ctx context.Context, folder string, n int) ([]MessageHeader, error) {
	if n <= 0 {
		return nil, nil
	}
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return nil, err
	}
	// Address the window by UID, not by sequence number: a UID is stable, so
	// a retry after a reconnect cannot land on a different message. UIDNEXT
	// comes from the SELECT response and is cached by selectFolder.
	if m.uidNext <= 1 {
		return nil, nil
	}
	return m.fetchBefore(ctx, folder, m.uidNext, n)
}

func (m *imapClient) FetchBefore(ctx context.Context, folder string, beforeUID uint32, n int) ([]MessageHeader, error) {
	if n <= 0 || beforeUID <= 1 {
		return nil, nil
	}
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return nil, err
	}
	return m.fetchBefore(ctx, folder, beforeUID, n)
}

// fetchBefore returns the newest n messages with a UID below beforeUID.
//
// The window has to be expressed in UIDs, but UIDs are not contiguous: a
// mailbox that has seen a lot of churn can have gaps of millions between two
// neighboring messages (measured: UIDNEXT ≈ 2.05e9). A fixed window of n*3
// then matches nothing at all, which reads as "there is no older mail" and
// stops the scroll-back dead. The window is therefore sized from the average
// UID span per message, and widened while it still comes up short.
func (m *imapClient) fetchBefore(ctx context.Context, folder string, beforeUID uint32, n int) ([]MessageHeader, error) {
	const maxWindow = 1000
	if n > maxWindow {
		n = maxWindow
	}
	window := m.uidWindow(n, beforeUID)
	var headers []MessageHeader
	for range 6 {
		lo := uint32(1)
		if beforeUID > window {
			lo = beforeUID - window
		}
		var set imap.UIDSet
		set.AddRange(imap.UID(lo), imap.UID(beforeUID-1))
		found, err := m.fetchHeadersBySet(ctx, folder, set)
		if err != nil {
			return nil, err
		}
		headers = found
		if len(headers) >= n || lo == 1 {
			break
		}
		// Widen, but never past the first message: the loop has to terminate
		// and uint32 has no room for a runaway multiplier.
		if window > beforeUID/4 {
			window = beforeUID
		} else {
			window *= 4
		}
	}
	if len(headers) > n {
		headers = headers[len(headers)-n:]
	}
	return headers, nil
}

// uidWindow sizes the UID range worth asking for when n messages are wanted.
func (m *imapClient) uidWindow(n int, beforeUID uint32) uint32 {
	if n <= 0 {
		return 0
	}
	size := uint64(n)
	window := size * 3
	// UIDNEXT divided by the message count is the average UID span one message
	// occupies. Doubling it leaves room for the messages that were deleted
	// without shrinking UIDNEXT.
	if m.uidNext > 1 && m.numMessages > 0 {
		if avg := uint64(m.uidNext) / uint64(m.numMessages); avg > 1 {
			window = size * avg * 2
		}
	}
	if window < 100 {
		window = 100
	}
	if window > uint64(beforeUID) {
		window = uint64(beforeUID)
	}
	return uint32(window)
}

// headerSectionItem asks for the message's header block, which is parsed with
// enmime instead of asking the server for an ENVELOPE:
//
// QQ Mail (and other Chinese providers) emit ENVELOPE structures that violate
// RFC 9051's grammar — one malformed message makes go-imap's strict parser
// abort with `imapwire: expected ')', got "z"`, which kills the connection and
// with it the whole account sync. A header block is opaque bytes: a broken
// header can only cost that one message's fields, never the connection.
//
// BODYSTRUCTURE is the one structured item left in the request, so it carries
// the same risk; fetchHeadersBySet retries without it when a batch dies. Do not
// add another structured item here without extending that fallback.
//
// Deliberately the WHOLE header, not `HEADER.FIELDS (From To …)`: QQ accepts
// that form and answers with an empty block (measured: 2 bytes, just CRLF), so
// every message arrived with no subject and no sender. The unfiltered form
// returns the real headers (~4 KB per message); the filter saves bandwidth on
// servers that implement it, but a silently empty mailbox costs far more.
func headerSectionItem() *imap.FetchItemBodySection {
	return &imap.FetchItemBodySection{
		Specifier: imap.PartSpecifierHeader,
		Peek:      true,
	}
}

func (m *imapClient) fetchHeadersBySet(ctx context.Context, folder string, set imap.UIDSet) ([]MessageHeader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	withStructure := !m.degraded[folder]
	headers, err := m.fetchHeadersOnce(ctx, folder, set, withStructure)
	if err == nil {
		return headers, nil
	}
	var decode *decodeError
	if !errors.As(err, &decode) {
		// A network failure is not a reason to stop asking for BODYSTRUCTURE;
		// doing so would cost the attachment flags of every message in the
		// folder for the rest of the pass.
		return nil, err
	}
	// The batch died on a response go-imap could not parse, and go-imap
	// answers that by closing the connection. Reconnecting and asking for the
	// same messages again without BODYSTRUCTURE recovers all of them: the
	// header block is an opaque literal that no parser can reject, and the
	// remaining items are plain numbers and flags. The only loss is the
	// attachment flag for messages whose body has not been decoded yet, which
	// is a far better trade than the whole folder never syncing.
	if m.degraded == nil {
		m.degraded = make(map[string]bool)
	}
	m.degraded[folder] = true
	if rerr := m.reconnect(ctx); rerr != nil {
		return nil, rerr
	}
	return m.fetchHeadersOnce(ctx, folder, set, false)
}

// decodeError marks a failure to parse a server response, as opposed to one
// that came from the transport.
//
// go-imap closes the connection on a malformed response, so by the time the
// error surfaces the socket is gone and every classifier calls it a connection
// failure. The original error is what tells the two apart: a transport failure
// is a net error (or EOF), a decoder failure is a plain error.
type decodeError struct{ error }

// isTransportFailure reports whether err is the socket going away rather than a
// response the decoder could not parse.
func isTransportFailure(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe)
}

// takeDecoderError closes the dead connection and returns the error that
// stopped its read loop, if there was one.
//
// This is the tie-breaker the error alone cannot give us. go-imap closes the
// socket from its read goroutine when it cannot parse a response, and the
// command that was in flight then reports the dead socket ("use of closed
// network connection") rather than the parse error — which goroutine notices
// first is a scheduling race, so the same malformed reply reaches us as a
// transport error on one run and as a parse error on the next.
//
// A clean peer close leaves go-imap's decoder error nil (the read loop breaks
// on EOF without recording it), so nil means "nothing was parsed wrong": a real
// drop. Any non-nil, non-transport error is a response the decoder rejected.
func (m *imapClient) takeDecoderError() error {
	if m.c == nil {
		return nil
	}
	return m.c.Close()
}

func (m *imapClient) classifyFetchError(err error) error {
	if isTransportFailure(err) {
		// Only consult the decoder when the socket is really gone: closing a
		// connection that a timeout left alive would turn a transient read
		// timeout into a permanent failure.
		if m.connGone() {
			if derr := m.takeDecoderError(); derr != nil && !isTransportFailure(derr) {
				return &decodeError{m.fail("fetch headers", derr)}
			}
		}
		return m.fail("fetch headers", err)
	}
	return &decodeError{m.fail("fetch headers", err)}
}

func (m *imapClient) fetchHeadersOnce(ctx context.Context, folder string, set imap.UIDSet, withStructure bool) ([]MessageHeader, error) {
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.deadline()
	section := headerSectionItem()
	opts := &imap.FetchOptions{
		UID:          true,
		Flags:        true,
		RFC822Size:   true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{section},
	}
	if withStructure {
		opts.BodyStructure = &imap.FetchItemBodyStructure{Extended: true}
	}
	msgs, err := m.c.Fetch(set, opts).Collect()
	if err != nil {
		return nil, m.classifyFetchError(err)
	}
	out := make([]MessageHeader, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, headerFromBuffer(msg, section))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out, nil
}

// applyHeaderSection fills the header fields from a raw header block.
//
// Every field is best-effort: a header the parser cannot read is dropped and
// the message still lands in the list (UID, size, flags and date come from the
// FETCH response, not from here). This is the whole point of not using
// ENVELOPE — one weird header must never take the mailbox down.
func applyHeaderSection(h *MessageHeader, raw []byte) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return
	}
	applyHeaderIdentifiers(h, env)
	applyHeaderAddresses(h, env)
	applyHeaderDate(h, env)
}

// applyHeaderIdentifiers fills the message identity fields: subject plus the
// Message-ID/In-Reply-To/References triple used for threading.
func applyHeaderIdentifiers(h *MessageHeader, env *enmime.Envelope) {
	if v := strings.TrimSpace(env.GetHeader("Subject")); v != "" {
		h.Subject = v
	}
	if v := env.GetHeader("Message-ID"); v != "" {
		h.MessageID = strings.Trim(strings.TrimSpace(v), "<>")
	}
	if v := env.GetHeader("In-Reply-To"); v != "" {
		h.InReplyTo = firstOrEmpty(strings.Fields(strings.Trim(v, "<>")))
	}
	if v := env.GetHeader("References"); v != "" {
		for _, ref := range strings.Fields(v) {
			if id := strings.Trim(ref, "<>"); id != "" {
				h.References = append(h.References, id)
			}
		}
	}
}

// applyHeaderAddresses fills the address fields. A field the parser rejects is
// left empty rather than failing the whole header.
func applyHeaderAddresses(h *MessageHeader, env *enmime.Envelope) {
	if list, err := env.AddressList("From"); err == nil && len(list) > 0 {
		h.FromName = list[0].Name
		h.FromAddr = list[0].Address
	}
	if list, err := env.AddressList("To"); err == nil {
		h.To = convertMailAddresses(list)
	}
	if list, err := env.AddressList("Cc"); err == nil {
		h.Cc = convertMailAddresses(list)
	}
	if list, err := env.AddressList("Reply-To"); err == nil {
		h.ReplyTo = convertMailAddresses(list)
	}
}

// applyHeaderDate only fills the date when FETCH did not already provide one.
func applyHeaderDate(h *MessageHeader, env *enmime.Envelope) {
	if !h.Date.IsZero() {
		return
	}
	v := env.GetHeader("Date")
	if v == "" {
		return
	}
	if d, err := mail.ParseDate(v); err == nil {
		h.Date = d
	}
}

func headerFromBuffer(msg *imapclient.FetchMessageBuffer, section *imap.FetchItemBodySection) MessageHeader {
	h := MessageHeader{
		UID:            uint32(msg.UID),
		Size:           msg.RFC822Size,
		HasAttachments: bodyHasAttachments(msg.BodyStructure),
	}
	if !msg.InternalDate.IsZero() {
		h.Date = msg.InternalDate
	}
	if env := msg.Envelope; env != nil {
		h.Subject = env.Subject
		h.MessageID = env.MessageID
		h.InReplyTo = firstOrEmpty(env.InReplyTo)
		h.References = env.InReplyTo
		if len(env.From) > 0 {
			h.FromName = env.From[0].Name
			h.FromAddr = env.From[0].Addr()
		}
		h.To = convertAddresses(env.To)
		h.Cc = convertAddresses(env.Cc)
		h.ReplyTo = convertAddresses(env.ReplyTo)
		if h.Date.IsZero() {
			h.Date = env.Date
		}
	}
	// No ENVELOPE is requested any more; the header block is the primary
	// source. Keeping the ENVELOPE branch costs nothing and lets an older
	// server (or a test double) that still sends one keep working.
	if msg.Envelope == nil && section != nil {
		if raw := msg.FindBodySection(section); len(raw) > 0 {
			applyHeaderSection(&h, raw)
		}
	}
	for _, f := range msg.Flags {
		switch f {
		case imap.FlagSeen:
			h.Seen = true
		case imap.FlagFlagged:
			h.Flagged = true
		case imap.FlagAnswered:
			h.Answered = true
		case imap.FlagDeleted:
			h.Deleted = true
		}
	}
	return h
}

func (m *imapClient) FetchRaw(ctx context.Context, folder string, uid uint32) ([]byte, error) {
	raw, err := m.FetchRawBatch(ctx, folder, []uint32{uid})
	if err != nil {
		return nil, err
	}
	b, ok := raw[uid]
	if !ok {
		return nil, &Error{Kind: KindServer, Op: "fetch body", Err: errors.New("server returned no body for this message")}
	}
	return b, nil
}

func (m *imapClient) FetchRawBatch(ctx context.Context, folder string, uids []uint32) (map[uint32][]byte, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.deadline()
	section := &imap.FetchItemBodySection{Peek: true}
	msgs, err := m.c.Fetch(uidSet(uids), &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, m.fail("fetch body", err)
	}
	out := make(map[uint32][]byte, len(msgs))
	for _, msg := range msgs {
		if b := msg.FindBodySection(section); b != nil {
			out[uint32(msg.UID)] = b
		}
	}
	return out, nil
}

func (m *imapClient) StoreFlags(ctx context.Context, folder string, uid uint32, add, remove []string) error {
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	set := imap.UIDSetNum(imap.UID(uid))
	if len(add) > 0 {
		m.deadline()
		if err := m.c.Store(set, &imap.StoreFlags{
			Op: imap.StoreFlagsAdd, Silent: true, Flags: toFlags(add),
		}, nil).Close(); err != nil {
			return m.fail("store flags", err)
		}
	}
	if len(remove) > 0 {
		m.deadline()
		if err := m.c.Store(set, &imap.StoreFlags{
			Op: imap.StoreFlagsDel, Silent: true, Flags: toFlags(remove),
		}, nil).Close(); err != nil {
			return m.fail("store flags", err)
		}
	}
	return nil
}

// MarkAllSeen adds \Seen to every message in the folder.
//
// It stores over the sequence range 1:* rather than over locally known UIDs:
// the unread badge comes from STATUS, so messages the cache never fetched would
// otherwise stay unread on the server and the count would bounce right back on
// the next sync.
func (m *imapClient) MarkAllSeen(ctx context.Context, folder string) error {
	sel, err := m.selectFolder(ctx, folder)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	total := 0
	if sel != nil {
		total = int(sel.NumMessages)
	} else if mb := m.c.Mailbox(); mb != nil {
		total = int(mb.NumMessages)
	}
	if total == 0 {
		return nil
	}
	var set imap.SeqSet
	set.AddRange(1, uint32(total))
	m.deadline()
	if err := m.c.Store(set, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen},
	}, nil).Close(); err != nil {
		return m.fail("mark all seen", err)
	}
	return nil
}

func (m *imapClient) Move(ctx context.Context, folder string, uid uint32, dest string) error {
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	set := imap.UIDSetNum(imap.UID(uid))
	if m.c.Caps().Has(imap.CapMove) {
		m.deadline()
		if _, err := m.c.Move(set, dest).Wait(); err != nil {
			return m.fail("move message", err)
		}
		return nil
	}
	// go-imap's own MOVE fallback ends with a bare EXPUNGE, which takes every
	// other \Deleted message in the mailbox with it. Do the three steps here
	// so the last one can be targeted.
	m.deadline()
	if _, err := m.c.Copy(set, dest).Wait(); err != nil {
		return m.fail("copy message", err)
	}
	m.deadline()
	if err := m.c.Store(set, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Close(); err != nil {
		return m.fail("move message", err)
	}
	return m.expungeOne(ctx, set)
}

func (m *imapClient) Delete(ctx context.Context, folder string, uid uint32) error {
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	set := imap.UIDSetNum(imap.UID(uid))
	m.deadline()
	if err := m.c.Store(set, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Close(); err != nil {
		return m.fail("delete message", err)
	}
	return m.expungeOne(ctx, set)
}

// expungeOne removes exactly the given UIDs.
//
// UID EXPUNGE (RFC 4315) is the only way to do that safely. Without UIDPLUS a
// bare EXPUNGE removes *every* \Deleted message in the mailbox, including ones
// another client has flagged and not committed yet — deleting one message in
// the Trash would silently wipe unrelated mail. So a bare EXPUNGE is only used
// after a search proves the message is the only one flagged.
func (m *imapClient) expungeOne(ctx context.Context, set imap.UIDSet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.deadline()
	if m.c.Caps().Has(imap.CapUIDPlus) {
		if err := m.c.UIDExpunge(set).Close(); err != nil {
			return m.fail("expunge message", err)
		}
		return nil
	}
	onlyOurs, err := m.onlyDeleted(ctx, set)
	if err != nil {
		return err
	}
	if !onlyOurs {
		// Leave the message flagged. Servers hide \Deleted messages and the
		// sync drops them from the cache, so it is gone as far as the user is
		// concerned; the next client that expunges takes it.
		return nil
	}
	m.deadline()
	if err := m.c.Expunge().Close(); err != nil {
		return m.fail("expunge message", err)
	}
	return nil
}

// onlyDeleted reports whether every \Deleted message in the selected mailbox is
// covered by set.
func (m *imapClient) onlyDeleted(ctx context.Context, set imap.UIDSet) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.deadline()
	data, err := m.c.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagDeleted}}, nil).Wait()
	if err != nil {
		return false, m.fail("search deleted", err)
	}
	deleted, ok := data.All.(imap.UIDSet)
	if !ok || len(deleted) == 0 {
		// An empty result cannot be right — we just flagged this message — so
		// treat it as "do not know" rather than as permission to expunge.
		return false, nil
	}
	for _, r := range deleted {
		if !uidRangeWithin(set, r) {
			return false, nil
		}
	}
	return true, nil
}

func uidRangeWithin(super imap.UIDSet, r imap.UIDRange) bool {
	for _, s := range super {
		if r.Start >= s.Start && r.Stop <= s.Stop {
			return true
		}
	}
	return false
}

func (m *imapClient) Append(ctx context.Context, folder string, raw []byte, seen bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	opts := &imap.AppendOptions{}
	if seen {
		opts.Flags = []imap.Flag{imap.FlagSeen}
	}
	m.deadline()
	cmd := m.c.Append(folder, int64(len(raw)), opts)
	if _, err := cmd.Write(raw); err != nil {
		_ = cmd.Close()
		return m.fail("append to sent", err)
	}
	if err := cmd.Close(); err != nil {
		return m.fail("append to sent", err)
	}
	if _, err := cmd.Wait(); err != nil {
		return m.fail("append to sent", err)
	}
	return nil
}

// --- helpers ---

func uidSet(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, u := range uids {
		set.AddNum(imap.UID(u))
	}
	return set
}

func toFlags(names []string) []imap.Flag {
	out := make([]imap.Flag, 0, len(names))
	for _, n := range names {
		out = append(out, imap.Flag(n))
	}
	return out
}

// convertMailAddresses maps stdlib addresses (what enmime returns) to the
// package's own shape.
func convertMailAddresses(addrs []*mail.Address) []Address {
	out := make([]Address, 0, len(addrs))
	for _, a := range addrs {
		if a == nil {
			continue
		}
		out = append(out, Address{Name: a.Name, Addr: a.Address})
	}
	return out
}

func convertAddresses(addrs []imap.Address) []Address {
	out := make([]Address, 0, len(addrs))
	for _, a := range addrs {
		if a.IsGroupStart() || a.IsGroupEnd() {
			continue
		}
		out = append(out, Address{Name: a.Name, Addr: a.Addr()})
	}
	return out
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func hasAttr(attrs []imap.MailboxAttr, want imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == want {
			return true
		}
	}
	return false
}

func specialUseOf(attrs []imap.MailboxAttr, name string) string {
	for _, a := range attrs {
		switch a {
		case imap.MailboxAttrSent:
			return SpecialUseSent
		case imap.MailboxAttrDrafts:
			return SpecialUseDrafts
		case imap.MailboxAttrTrash:
			return SpecialUseTrash
		case imap.MailboxAttrJunk:
			return SpecialUseJunk
		case imap.MailboxAttrArchive:
			return SpecialUseArchive
		case imap.MailboxAttrAll:
			return SpecialUseArchive
		}
	}
	// Servers without SPECIAL-USE (and many Chinese providers) only expose
	// localized names, so fall back to the conventional ones.
	lower := strings.ToLower(name)
	switch lower {
	case SpecialUseInbox:
		return "inbox"
	case SpecialUseSent, "sent items", "sent messages", "已发送", "已发送邮件", "已发送文件夹":
		return SpecialUseSent
	case SpecialUseDrafts, "草稿", "草稿箱":
		return SpecialUseDrafts
	case SpecialUseTrash, "deleted items", "deleted messages", "已删除", "垃圾箱", "废纸篓":
		return SpecialUseTrash
	case SpecialUseJunk, "spam", "垃圾邮件":
		return SpecialUseJunk
	case SpecialUseArchive, "归档":
		return SpecialUseArchive
	}
	return ""
}

// folderRank orders the folder bar: INBOX first, then the well-known boxes,
// then everything else alphabetically.
func folderRank(f Folder) int {
	switch f.SpecialUse {
	case SpecialUseInbox:
		return 0
	case SpecialUseSent:
		return 10
	case SpecialUseDrafts:
		return 11
	case SpecialUseArchive:
		return 12
	case SpecialUseJunk:
		return 13
	case SpecialUseTrash:
		return 14
	}
	return 20
}

func leafName(full, delim string) string {
	if full == "" {
		return ""
	}
	if delim == "" {
		return full
	}
	parts := strings.Split(full, delim)
	return parts[len(parts)-1]
}

// bodyHasAttachments walks the body structure looking for a part that is
// explicitly an attachment. A multipart/alternative with text+html is not an
// attachment; a multipart/mixed with a PDF is.
func bodyHasAttachments(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		if d := part.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			found = true
			return false
		}
		return true
	})
	return found
}

// ParsePort is a small helper used by the account form and tests.
func ParsePort(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 || n > 65535 {
		return def
	}
	return n
}

func joinHostPort(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
