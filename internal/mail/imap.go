package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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
)

// imapClient wraps imapclient.Client with a deadline-aware connection.
//
// go-imap has no context support: commands block on the socket. We therefore
// keep the net.Conn and push a deadline before every command, which turns a
// hung server into a normal error instead of a permanently stuck sync worker.
type imapClient struct {
	c        *imapclient.Client
	conn     net.Conn
	selected string
	readOnly bool
	closed   bool
}

// DialIMAP connects and logs in. The caller owns the returned client.
func DialIMAP(ctx context.Context, acc Account) (Client, error) {
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", acc.IMAPAddr())
	if err != nil {
		return nil, classify("dial", err)
	}

	tlsCfg := &tls.Config{
		ServerName:         acc.IMAPHost,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: acc.InsecureTLS, // certificate verification is opt-in per account
	}
	opts := &imapclient.Options{
		TLSConfig:   tlsCfg,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}

	var c *imapclient.Client
	switch acc.IMAPSecurity {
	case SecurityTLS:
		tlsConn := tls.Client(conn, tlsCfg)
		if herr := tlsConn.HandshakeContext(ctx); herr != nil {
			_ = conn.Close()
			return nil, classify("TLS 握手", herr)
		}
		conn = tlsConn
		c = imapclient.New(conn, opts)
	case SecurityStartTLS:
		c, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			_ = conn.Close()
			return nil, classify("starttls", err)
		}
	default:
		c = imapclient.New(conn, opts)
	}

	if err := c.WaitGreeting(); err != nil {
		_ = c.Close()
		return nil, classify("read greeting", err)
	}
	_ = conn.SetDeadline(time.Now().Add(opTimeout))
	if err := c.Login(acc.Username, acc.Password).Wait(); err != nil {
		_ = c.Close()
		return nil, classify("login", err)
	}
	return &imapClient{c: c, conn: conn}, nil
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
			return nil, classify("list folders", err)
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
		return Status{}, classify("status", err)
	}
	st := Status{UIDNext: uint32(data.UIDNext), UIDValidity: data.UIDValidity}
	if data.NumMessages != nil {
		st.Messages = *data.NumMessages
	}
	if data.NumUnseen != nil {
		st.Unseen = *data.NumUnseen
	}
	return st, nil
}

func (m *imapClient) selectFolder(ctx context.Context, folder string) (*imap.SelectData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.selected == folder {
		return nil, nil
	}
	m.deadline()
	data, err := m.c.Select(folder, nil).Wait()
	if err != nil {
		return nil, classify("select folder", err)
	}
	m.selected = folder
	m.readOnly = false
	return data, nil
}

func (m *imapClient) FetchHeaders(ctx context.Context, folder string, uids []uint32) ([]MessageHeader, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return nil, err
	}
	return m.fetchHeadersBySet(ctx, uidSet(uids))
}

func (m *imapClient) FetchNewest(ctx context.Context, folder string, n int) ([]MessageHeader, error) {
	if n <= 0 {
		return nil, nil
	}
	sel, err := m.selectFolder(ctx, folder)
	if err != nil {
		return nil, err
	}
	total := 0
	if sel != nil {
		total = int(sel.NumMessages)
	} else if mb := m.c.Mailbox(); mb != nil {
		total = int(mb.NumMessages)
	}
	if total == 0 {
		return nil, nil
	}
	start := total - n + 1
	if start < 1 {
		start = 1
	}
	var set imap.SeqSet
	set.AddRange(uint32(start), uint32(total))
	return m.fetchHeadersBySet(ctx, set)
}

func (m *imapClient) FetchBefore(ctx context.Context, folder string, beforeUID uint32, n int) ([]MessageHeader, error) {
	if n <= 0 || beforeUID <= 1 {
		return nil, nil
	}
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return nil, err
	}
	// UIDs are not contiguous, so fetch a wider window and keep the newest n.
	const maxWindow = 1000
	if n > maxWindow {
		n = maxWindow
	}
	window := uint32(n * 3)
	if window < 100 {
		window = 100
	}
	lo := uint32(1)
	if beforeUID > window {
		lo = beforeUID - window
	}
	var set imap.UIDSet
	set.AddRange(imap.UID(lo), imap.UID(beforeUID-1))
	headers, err := m.fetchHeadersBySet(ctx, set)
	if err != nil {
		return nil, err
	}
	if len(headers) > n {
		headers = headers[len(headers)-n:]
	}
	return headers, nil
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

func (m *imapClient) fetchHeadersBySet(ctx context.Context, set imap.NumSet) ([]MessageHeader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.deadline()
	section := headerSectionItem()
	msgs, err := m.c.Fetch(set, &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		RFC822Size:    true,
		InternalDate:  true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		BodySection:   []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, classify("fetch headers", err)
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
		return nil, classify("fetch body", err)
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
			return classify("store flags", err)
		}
	}
	if len(remove) > 0 {
		m.deadline()
		if err := m.c.Store(set, &imap.StoreFlags{
			Op: imap.StoreFlagsDel, Silent: true, Flags: toFlags(remove),
		}, nil).Close(); err != nil {
			return classify("store flags", err)
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
		return classify("mark all seen", err)
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
	m.deadline()
	if _, err := m.c.Move(imap.UIDSetNum(imap.UID(uid)), dest).Wait(); err != nil {
		return classify("move message", err)
	}
	return nil
}

func (m *imapClient) Delete(ctx context.Context, folder string, uid uint32) error {
	if _, err := m.selectFolder(ctx, folder); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.deadline()
	set := imap.UIDSetNum(imap.UID(uid))
	if err := m.c.Store(set, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Close(); err != nil {
		return classify("delete message", err)
	}
	m.deadline()
	if m.c.Caps().Has(imap.CapUIDPlus) {
		if err := m.c.UIDExpunge(set).Close(); err != nil {
			return classify("delete message", err)
		}
	} else if err := m.c.Expunge().Close(); err != nil {
		return classify("delete message", err)
	}
	return nil
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
		return classify("append to sent", err)
	}
	if err := cmd.Close(); err != nil {
		return classify("append to sent", err)
	}
	if _, err := cmd.Wait(); err != nil {
		return classify("append to sent", err)
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
