// Package mail implements the IMAP/SMTP side of the built-in mail client.
//
// It is a protocol-level package: it knows how to talk to a mail server and
// how to turn RFC 5322 bytes into a normalized structure, but it knows nothing
// about the database, the HTTP layer or the app's configuration. The service
// package owns persistence and calls into here through the Client / Sender
// interfaces so tests can substitute fakes.
package mail

import (
	"context"
	"time"
)

// Folder special-use values (RFC 6154) plus the conventional INBOX.
const (
	SpecialUseInbox   = "inbox"
	SpecialUseSent    = "sent"
	SpecialUseDrafts  = "drafts"
	SpecialUseTrash   = "trash"
	SpecialUseJunk    = "junk"
	SpecialUseArchive = "archive"
)

// Security is the transport encryption used to reach a server.
type Security string

const (
	// SecurityTLS is implicit TLS (IMAPS on 993, SMTPS on 465).
	SecurityTLS Security = "tls"
	// SecurityStartTLS upgrades a plaintext connection (143 / 587).
	SecurityStartTLS Security = "starttls"
	// SecurityNone is plaintext. Only sane for a server on localhost.
	SecurityNone Security = "none"
)

// Account is everything needed to reach one mailbox.
//
// Password is stored in the database in clear text (same trade-off as
// providers.api_key — this repo has no key-management story and the database
// file is 0600). It is never returned by the API unless the caller explicitly
// asks to reveal it.
type Account struct {
	ID           int64
	Name         string
	Email        string
	Username     string
	Password     string
	IMAPHost     string
	IMAPPort     int
	IMAPSecurity Security
	SMTPHost     string
	SMTPPort     int
	SMTPSecurity Security
	// InsecureTLS skips certificate verification. Self-hosted servers with a
	// self-signed certificate are common enough that refusing this outright
	// would make the feature unusable there; it is opt-in per account.
	InsecureTLS bool
	Enabled     bool
}

// Addr returns the IMAP host:port to dial.
func (a Account) IMAPAddr() string { return withPort(a.IMAPHost, a.IMAPPort, 993) }

// SMTPAddr returns the SMTP host:port to dial.
func (a Account) SMTPAddr() string { return withPort(a.SMTPHost, a.SMTPPort, 465) }

func withPort(host string, port, def int) string {
	if port <= 0 {
		port = def
	}
	return joinHostPort(host, port)
}

// Folder is one mailbox as advertised by LIST.
type Folder struct {
	// Path is the mailbox name exactly as the server reports it — modified
	// UTF-7 for non-ASCII names. Every IMAP command must use this value, not
	// the decoded one.
	Path string
	// Name is the leaf name for display ("收件箱" rather than "INBOX/收件箱").
	Name string
	// Delimiter is the hierarchy separator, '/' or '.' on most servers.
	Delimiter string
	// SpecialUse is one of inbox/sent/drafts/trash/junk/archive, or empty.
	SpecialUse string
	// Selectable is false for \Noselect containers (they cannot be opened).
	Selectable bool
}

// Status is the per-folder counters from STATUS.
type Status struct {
	Messages    uint32
	Unseen      uint32
	UIDNext     uint32
	UIDValidity uint32
}

// Address is one RFC 5322 address.
type Address struct {
	Name string
	Addr string
}

// MessageHeader is the metadata shown in the list. It is everything the
// envelope + bodystructure give us, with no body content.
type MessageHeader struct {
	UID            uint32
	MessageID      string
	Subject        string
	FromName       string
	FromAddr       string
	To             []Address
	Cc             []Address
	ReplyTo        []Address
	InReplyTo      string
	References     []string
	Date           time.Time
	Size           int64
	Seen           bool
	Flagged        bool
	Answered       bool
	HasAttachments bool
	// Preview is the server-side snippet from RFC 8970 PREVIEW, when the
	// server supports it. Empty otherwise.
	Preview string
}

// Attachment is one MIME part that is not the message body.
type Attachment struct {
	PartID      string
	Filename    string
	ContentType string
	Size        int64
	ContentID   string
	Inline      bool
	Content     []byte
}

// ParsedMessage is the result of decoding a full RFC 5322 message.
type ParsedMessage struct {
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
}

// SendAttachment is one file to attach when sending.
type SendAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// SendRequest is one outgoing message.
type SendRequest struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	Subject     string
	Text        string
	HTML        string
	InReplyTo   string
	References  []string
	Attachments []SendAttachment
}

// Client is the IMAP operations the service needs. It exists so the service
// can be tested against an in-memory implementation.
type Client interface {
	ListFolders(ctx context.Context) ([]Folder, error)
	Status(ctx context.Context, folder string) (Status, error)
	// FetchHeaders returns metadata for the given UIDs (missing UIDs are
	// silently skipped — the server is allowed to have expunged them).
	FetchHeaders(ctx context.Context, folder string, uids []uint32) ([]MessageHeader, error)
	// FetchNewest returns the newest n messages of a folder, oldest first.
	FetchNewest(ctx context.Context, folder string, n int) ([]MessageHeader, error)
	// FetchBefore returns up to n messages older than the given UID.
	FetchBefore(ctx context.Context, folder string, beforeUID uint32, n int) ([]MessageHeader, error)
	// FetchRaw returns the complete RFC 5322 message.
	FetchRaw(ctx context.Context, folder string, uid uint32) ([]byte, error)
	// FetchRawBatch returns several complete messages in one round trip.
	// UIDs the server no longer has are absent from the result.
	FetchRawBatch(ctx context.Context, folder string, uids []uint32) (map[uint32][]byte, error)
	// StoreFlags adds and/or removes flags on one message.
	StoreFlags(ctx context.Context, folder string, uid uint32, add, remove []string) error
	// Move moves one message to another folder.
	Move(ctx context.Context, folder string, uid uint32, dest string) error
	// Delete marks one message \Deleted and expunges it.
	Delete(ctx context.Context, folder string, uid uint32) error
	// Append appends a raw message to a folder (used for Sent).
	Append(ctx context.Context, folder string, raw []byte, seen bool) error
	Close() error
}

// Dialer opens a connection for an account. The returned Client owns the
// connection and must be closed by the caller.
type Dialer func(ctx context.Context, acc Account) (Client, error)

// Sender delivers one outgoing message and returns the exact RFC 5322 bytes
// that were sent, so the caller can APPEND the same message to the Sent folder.
type Sender interface {
	Send(ctx context.Context, acc Account, req SendRequest) ([]byte, error)
}
