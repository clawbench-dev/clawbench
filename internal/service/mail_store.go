package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"clawbench/internal/mail"
	"clawbench/internal/store"
)

// MailDDL creates the four tables behind the mail client.
//
// These tables are GLOBAL, not project-scoped: a mailbox belongs to the person,
// not to a repository, and the same inbox has to be visible from every project.
// That is why there is no project_id column here (unlike notifications).
//
// Bodies are stored per message but fetched lazily: sync only fills the header
// columns, and body_text/body_html stay NULL until the user opens the message.
// Attachments are materialized to disk at that same moment; the row only holds
// the metadata and the local path.
const MailDDL = `
CREATE TABLE IF NOT EXISTS mail_accounts (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	name           TEXT NOT NULL DEFAULT '',
	email          TEXT NOT NULL DEFAULT '',
	username       TEXT NOT NULL DEFAULT '',
	password       TEXT NOT NULL DEFAULT '',
	imap_host      TEXT NOT NULL DEFAULT '',
	imap_port      INTEGER NOT NULL DEFAULT 993,
	imap_security  TEXT NOT NULL DEFAULT 'tls',
	smtp_host      TEXT NOT NULL DEFAULT '',
	smtp_port      INTEGER NOT NULL DEFAULT 465,
	smtp_security  TEXT NOT NULL DEFAULT 'tls',
	insecure_tls   INTEGER NOT NULL DEFAULT 0,
	enabled        INTEGER NOT NULL DEFAULT 1,
	sort_order     INTEGER NOT NULL DEFAULT 0,
	last_sync_at   INTEGER NOT NULL DEFAULT 0,
	last_error     TEXT NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS mail_folders (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id     INTEGER NOT NULL,
	path           TEXT NOT NULL,
	name           TEXT NOT NULL DEFAULT '',
	delimiter      TEXT NOT NULL DEFAULT '/',
	special_use    TEXT NOT NULL DEFAULT '',
	uid_validity   INTEGER NOT NULL DEFAULT 0,
	uid_next       INTEGER NOT NULL DEFAULT 0,
	last_seen_uid  INTEGER NOT NULL DEFAULT 0,
	unread         INTEGER NOT NULL DEFAULT 0,
	total          INTEGER NOT NULL DEFAULT 0,
	sort_order     INTEGER NOT NULL DEFAULT 0,
	UNIQUE(account_id, path)
);
CREATE INDEX IF NOT EXISTS idx_mail_folders_account ON mail_folders(account_id);

CREATE TABLE IF NOT EXISTS mail_messages (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id      INTEGER NOT NULL,
	folder_id       INTEGER NOT NULL,
	uid             INTEGER NOT NULL,
	message_id      TEXT NOT NULL DEFAULT '',
	subject         TEXT NOT NULL DEFAULT '',
	from_name       TEXT NOT NULL DEFAULT '',
	from_addr       TEXT NOT NULL DEFAULT '',
	to_addrs        TEXT NOT NULL DEFAULT '[]',
	cc_addrs        TEXT NOT NULL DEFAULT '[]',
	reply_to        TEXT NOT NULL DEFAULT '',
	in_reply_to     TEXT NOT NULL DEFAULT '',
	refs            TEXT NOT NULL DEFAULT '',
	sent_at         INTEGER NOT NULL DEFAULT 0,
	size            INTEGER NOT NULL DEFAULT 0,
	seen            INTEGER NOT NULL DEFAULT 0,
	flagged         INTEGER NOT NULL DEFAULT 0,
	answered        INTEGER NOT NULL DEFAULT 0,
	has_attachments INTEGER NOT NULL DEFAULT 0,
	snippet         TEXT NOT NULL DEFAULT '',
	body_text       TEXT,
	body_html       TEXT,
	body_fetched    INTEGER NOT NULL DEFAULT 0,
	UNIQUE(account_id, folder_id, uid)
);
CREATE INDEX IF NOT EXISTS idx_mail_messages_list ON mail_messages(folder_id, sent_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_mail_messages_account ON mail_messages(account_id);

CREATE TABLE IF NOT EXISTS mail_attachments (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	message_id   INTEGER NOT NULL,
	part_id      TEXT NOT NULL DEFAULT '',
	filename     TEXT NOT NULL DEFAULT '',
	content_type TEXT NOT NULL DEFAULT '',
	size         INTEGER NOT NULL DEFAULT 0,
	content_id   TEXT NOT NULL DEFAULT '',
	inline       INTEGER NOT NULL DEFAULT 0,
	local_path   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_mail_attachments_msg ON mail_attachments(message_id);

CREATE TABLE IF NOT EXISTS mail_drafts (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id  INTEGER NOT NULL,
	to_addrs    TEXT NOT NULL DEFAULT '',
	cc_addrs    TEXT NOT NULL DEFAULT '',
	bcc_addrs   TEXT NOT NULL DEFAULT '',
	subject     TEXT NOT NULL DEFAULT '',
	body_text   TEXT NOT NULL DEFAULT '',
	body_html   TEXT NOT NULL DEFAULT '',
	in_reply_to TEXT NOT NULL DEFAULT '',
	refs        TEXT NOT NULL DEFAULT '',
	attachments TEXT NOT NULL DEFAULT '[]',
	updated_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_mail_drafts_account ON mail_drafts(account_id);
`

// MailAccount is one configured mailbox. Password is never serialized: the API
// exposes HasPassword and only reveals the secret on an explicit request.
type MailAccount struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	Password     string `json:"-"`
	IMAPHost     string `json:"imapHost"`
	IMAPPort     int    `json:"imapPort"`
	IMAPSecurity string `json:"imapSecurity"`
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPSecurity string `json:"smtpSecurity"`
	InsecureTLS  bool   `json:"insecureTls"`
	Enabled      bool   `json:"enabled"`
	SortOrder    int    `json:"sortOrder"`
	LastSyncAt   int64  `json:"lastSyncAt"`
	LastError    string `json:"lastError"`
	HasPassword  bool   `json:"hasPassword"`
	CreatedAt    int64  `json:"createdAt"`
}

// ProtocolAccount converts the stored row into the protocol-layer account.
func (a MailAccount) ProtocolAccount() mail.Account {
	return mail.Account{
		ID:           a.ID,
		Name:         a.Name,
		Email:        a.Email,
		Username:     a.Username,
		Password:     a.Password,
		IMAPHost:     a.IMAPHost,
		IMAPPort:     a.IMAPPort,
		IMAPSecurity: mail.Security(a.IMAPSecurity),
		SMTPHost:     a.SMTPHost,
		SMTPPort:     a.SMTPPort,
		SMTPSecurity: mail.Security(a.SMTPSecurity),
		InsecureTLS:  a.InsecureTLS,
		Enabled:      a.Enabled,
	}
}

// MailFolder is one cached mailbox.
type MailFolder struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"accountId"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	Delimiter   string `json:"delimiter"`
	SpecialUse  string `json:"specialUse"`
	UIDValidity uint32 `json:"-"`
	UIDNext     uint32 `json:"-"`
	LastSeenUID uint32 `json:"-"`
	Unread      int    `json:"unread"`
	Total       int    `json:"total"`
	SortOrder   int    `json:"-"`
}

// MailMessage is one cached message header.
type MailMessage struct {
	ID             int64      `json:"id"`
	AccountID      int64      `json:"accountId"`
	FolderID       int64      `json:"folderId"`
	FolderPath     string     `json:"folderPath,omitempty"`
	UID            uint32     `json:"uid"`
	MessageID      string     `json:"messageId"`
	Subject        string     `json:"subject"`
	FromName       string     `json:"fromName"`
	FromAddr       string     `json:"fromAddr"`
	To             []MailAddr `json:"to"`
	Cc             []MailAddr `json:"cc"`
	ReplyTo        string     `json:"replyTo"`
	InReplyTo      string     `json:"inReplyTo"`
	References     string     `json:"references"`
	SentAt         int64      `json:"sentAt"`
	Size           int64      `json:"size"`
	Seen           bool       `json:"seen"`
	Flagged        bool       `json:"flagged"`
	Answered       bool       `json:"answered"`
	HasAttachments bool       `json:"hasAttachments"`
	Snippet        string     `json:"snippet"`
	BodyFetched    bool       `json:"bodyFetched"`
}

// MailAddr is one address in a stored recipient list.
type MailAddr struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// MailMessageDetail is the reader view: header + decoded body + attachments.
type MailMessageDetail struct {
	MailMessage
	BodyText    string           `json:"bodyText"`
	BodyHTML    string           `json:"bodyHtml"`
	Attachments []MailAttachment `json:"attachments"`
}

// MailAttachment is one file attached to a cached message.
type MailAttachment struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"messageId"`
	PartID      string `json:"-"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	ContentID   string `json:"contentId"`
	Inline      bool   `json:"inline"`
	LocalPath   string `json:"-"`
}

// MailDraft is a locally stored compose draft.
//
// Drafts live only here, not on the server: syncing IMAP drafts would require
// another APPEND/EXPUNGE state machine for a feature whose whole value is "do
// not lose what I typed".
type MailDraft struct {
	ID          int64           `json:"id"`
	AccountID   int64           `json:"accountId"`
	To          string          `json:"to"`
	Cc          string          `json:"cc"`
	Bcc         string          `json:"bcc"`
	Subject     string          `json:"subject"`
	BodyText    string          `json:"bodyText"`
	BodyHTML    string          `json:"bodyHtml"`
	InReplyTo   string          `json:"inReplyTo"`
	References  string          `json:"references"`
	Attachments []MailDraftFile `json:"attachments"`
	UpdatedAt   int64           `json:"updatedAt"`
}

// MailDraftFile points at a file already written by the upload endpoint.
type MailDraftFile struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
}

// --- accounts ---

const mailAccountColumns = `id, name, email, username, password, imap_host, imap_port,
	imap_security, smtp_host, smtp_port, smtp_security, insecure_tls, enabled,
	sort_order, last_sync_at, last_error, created_at`

func scanMailAccount(sc interface{ Scan(...any) error }) (MailAccount, error) {
	var a MailAccount
	var insecure, enabled int
	err := sc.Scan(&a.ID, &a.Name, &a.Email, &a.Username, &a.Password, &a.IMAPHost,
		&a.IMAPPort, &a.IMAPSecurity, &a.SMTPHost, &a.SMTPPort, &a.SMTPSecurity,
		&insecure, &enabled, &a.SortOrder, &a.LastSyncAt, &a.LastError, &a.CreatedAt)
	a.InsecureTLS = insecure != 0
	a.Enabled = enabled != 0
	a.HasPassword = a.Password != ""
	return a, err
}

// ListMailAccounts returns every account in display order.
func ListMailAccounts() ([]MailAccount, error) {
	rows, err := store.ReadDB().Query(`SELECT ` + mailAccountColumns +
		` FROM mail_accounts ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []MailAccount
	for rows.Next() {
		a, err := scanMailAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetMailAccount loads one account, including its password (needed to dial).
func GetMailAccount(id int64) (MailAccount, error) {
	row := store.ReadDB().QueryRow(`SELECT `+mailAccountColumns+` FROM mail_accounts WHERE id = ?`, id)
	a, err := scanMailAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MailAccount{}, ErrMailAccountNotFound
	}
	return a, err
}

// ErrMailAccountNotFound is returned for an unknown account id.
var ErrMailAccountNotFound = errors.New("mail account not found")

// CreateMailAccount inserts a new account and returns it with its id.
func CreateMailAccount(a MailAccount) (MailAccount, error) {
	now := time.Now().Unix()
	res, err := store.WriteExec(`INSERT INTO mail_accounts
		(name, email, username, password, imap_host, imap_port, imap_security,
		 smtp_host, smtp_port, smtp_security, insecure_tls, enabled, sort_order,
		 last_sync_at, last_error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.Name, a.Email, a.Username, a.Password, a.IMAPHost, a.IMAPPort, a.IMAPSecurity,
		a.SMTPHost, a.SMTPPort, a.SMTPSecurity, boolInt(a.InsecureTLS), boolInt(a.Enabled),
		a.SortOrder, 0, "", now)
	if err != nil {
		return MailAccount{}, err
	}
	id, _ := res.LastInsertId()
	return GetMailAccount(id)
}

// UpdateMailAccount writes the editable fields. An empty Password keeps the
// stored one, which is how the UI can edit everything else without ever
// receiving the secret.
func UpdateMailAccount(a MailAccount) (MailAccount, error) {
	cur, err := GetMailAccount(a.ID)
	if err != nil {
		return MailAccount{}, err
	}
	password := a.Password
	if password == "" {
		password = cur.Password
	}
	_, err = store.WriteExec(`UPDATE mail_accounts SET
		name = ?, email = ?, username = ?, password = ?, imap_host = ?, imap_port = ?,
		imap_security = ?, smtp_host = ?, smtp_port = ?, smtp_security = ?,
		insecure_tls = ?, enabled = ?, sort_order = ?
		WHERE id = ?`,
		a.Name, a.Email, a.Username, password, a.IMAPHost, a.IMAPPort, a.IMAPSecurity,
		a.SMTPHost, a.SMTPPort, a.SMTPSecurity, boolInt(a.InsecureTLS), boolInt(a.Enabled),
		a.SortOrder, a.ID)
	if err != nil {
		return MailAccount{}, err
	}
	return GetMailAccount(a.ID)
}

// DeleteMailAccount removes the account and every cached row that belongs to
// it. Attachment files on disk are removed by the caller (it knows the data dir).
func DeleteMailAccount(id int64) error {
	ctx := context.Background()
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM mail_attachments WHERE message_id IN
		(SELECT id FROM mail_messages WHERE account_id = ?)`, id); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM mail_messages WHERE account_id = ?`,
		`DELETE FROM mail_folders WHERE account_id = ?`,
		`DELETE FROM mail_drafts WHERE account_id = ?`,
		`DELETE FROM mail_accounts WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetMailAccountSyncResult records the outcome of one sync pass.
func SetMailAccountSyncResult(id int64, lastError string) error {
	_, err := store.WriteExec(`UPDATE mail_accounts SET last_sync_at = ?, last_error = ? WHERE id = ?`,
		time.Now().Unix(), lastError, id)
	return err
}

// --- folders ---

// UpsertMailFolder inserts or updates one folder and returns its row id.
func UpsertMailFolder(f MailFolder) (int64, error) {
	_, err := store.WriteExec(`INSERT INTO mail_folders
		(account_id, path, name, delimiter, special_use, sort_order)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(account_id, path) DO UPDATE SET
			name = excluded.name,
			delimiter = excluded.delimiter,
			special_use = excluded.special_use,
			sort_order = excluded.sort_order`,
		f.AccountID, f.Path, f.Name, f.Delimiter, f.SpecialUse, f.SortOrder)
	if err != nil {
		return 0, err
	}
	var id int64
	err = store.ReadDB().QueryRow(`SELECT id FROM mail_folders WHERE account_id = ? AND path = ?`,
		f.AccountID, f.Path).Scan(&id)
	return id, err
}

// UpdateMailFolderState stores the UID window and counters after a sync.
func UpdateMailFolderState(id int64, uidValidity, uidNext, lastSeen uint32, unread, total int) error {
	_, err := store.WriteExec(`UPDATE mail_folders SET
		uid_validity = ?, uid_next = ?, last_seen_uid = ?, unread = ?, total = ?
		WHERE id = ?`, uidValidity, uidNext, lastSeen, unread, total, id)
	return err
}

// ListMailFolders returns the cached folders of one account.
func ListMailFolders(accountID int64) ([]MailFolder, error) {
	rows, err := store.ReadDB().Query(`SELECT id, account_id, path, name, delimiter,
		special_use, uid_validity, uid_next, last_seen_uid, unread, total, sort_order
		FROM mail_folders WHERE account_id = ? ORDER BY sort_order, id`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []MailFolder
	for rows.Next() {
		var f MailFolder
		if err := rows.Scan(&f.ID, &f.AccountID, &f.Path, &f.Name, &f.Delimiter,
			&f.SpecialUse, &f.UIDValidity, &f.UIDNext, &f.LastSeenUID,
			&f.Unread, &f.Total, &f.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetMailFolder loads one folder row.
func GetMailFolder(id int64) (MailFolder, error) {
	var f MailFolder
	err := store.ReadDB().QueryRow(`SELECT id, account_id, path, name, delimiter,
		special_use, uid_validity, uid_next, last_seen_uid, unread, total, sort_order
		FROM mail_folders WHERE id = ?`, id).Scan(&f.ID, &f.AccountID, &f.Path, &f.Name,
		&f.Delimiter, &f.SpecialUse, &f.UIDValidity, &f.UIDNext, &f.LastSeenUID,
		&f.Unread, &f.Total, &f.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return MailFolder{}, ErrMailFolderNotFound
	}
	return f, err
}

// GetMailFolderByPath loads one folder by its IMAP path.
func GetMailFolderByPath(accountID int64, path string) (MailFolder, error) {
	var f MailFolder
	err := store.ReadDB().QueryRow(`SELECT id, account_id, path, name, delimiter,
		special_use, uid_validity, uid_next, last_seen_uid, unread, total, sort_order
		FROM mail_folders WHERE account_id = ? AND path = ?`, accountID, path).
		Scan(&f.ID, &f.AccountID, &f.Path, &f.Name, &f.Delimiter, &f.SpecialUse,
			&f.UIDValidity, &f.UIDNext, &f.LastSeenUID, &f.Unread, &f.Total, &f.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return MailFolder{}, ErrMailFolderNotFound
	}
	return f, err
}

// ErrMailFolderNotFound is returned for an unknown folder id.
var ErrMailFolderNotFound = errors.New("mail folder not found")

// ClearMailFolderMessages drops every cached message of a folder. Used when the
// server's UIDVALIDITY changes: old UIDs no longer identify the same messages.
func ClearMailFolderMessages(folderID int64) error {
	if _, err := store.WriteExec(`DELETE FROM mail_attachments WHERE message_id IN
		(SELECT id FROM mail_messages WHERE folder_id = ?)`, folderID); err != nil {
		return err
	}
	_, err := store.WriteExec(`DELETE FROM mail_messages WHERE folder_id = ?`, folderID)
	return err
}

// --- messages ---

// UpsertMailMessages inserts headers that are not cached yet and returns the
// rows it actually created (with their ids). Existing rows are left untouched:
// their body and local flags are newer than what a re-sync of the same UID
// would tell us.
func UpsertMailMessages(msgs []MailMessage) ([]MailMessage, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	ctx := context.Background()
	tx, err := store.WriteBegin()
	if err != nil {
		return nil, err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO mail_messages
		(account_id, folder_id, uid, message_id, subject, from_name, from_addr,
		 to_addrs, cc_addrs, reply_to, in_reply_to, refs, sent_at, size,
		 seen, flagged, answered, has_attachments, snippet, body_fetched)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)
		ON CONFLICT(account_id, folder_id, uid) DO UPDATE SET
			subject = excluded.subject,
			from_name = excluded.from_name,
			from_addr = excluded.from_addr,
			to_addrs = excluded.to_addrs,
			cc_addrs = excluded.cc_addrs,
			reply_to = excluded.reply_to,
			in_reply_to = excluded.in_reply_to,
			refs = excluded.refs,
			sent_at = excluded.sent_at,
			size = excluded.size,
			has_attachments = excluded.has_attachments,
			message_id = excluded.message_id
		WHERE mail_messages.subject <> excluded.subject
		   OR mail_messages.from_addr <> excluded.from_addr
		   OR mail_messages.from_name <> excluded.from_name
		   OR mail_messages.to_addrs <> excluded.to_addrs`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stmt.Close() }()

	var inserted []MailMessage
	for _, m := range msgs {
		res, err := stmt.ExecContext(ctx, m.AccountID, m.FolderID, m.UID, m.MessageID, m.Subject,
			m.FromName, m.FromAddr, marshalAddrs(m.To), marshalAddrs(m.Cc), m.ReplyTo,
			m.InReplyTo, m.References, m.SentAt, m.Size, boolInt(m.Seen),
			boolInt(m.Flagged), boolInt(m.Answered), boolInt(m.HasAttachments), m.Snippet)
		if err != nil {
			return inserted, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if id, idErr := res.LastInsertId(); idErr == nil {
				m.ID = id
			}
			inserted = append(inserted, m)
		}
	}
	return inserted, tx.Commit()
}

// UpdateMailMessageFlags rewrites the local flag copy of one message.
func UpdateMailMessageFlags(id int64, seen, flagged, answered bool) error {
	_, err := store.WriteExec(`UPDATE mail_messages SET seen = ?, flagged = ?, answered = ? WHERE id = ?`,
		boolInt(seen), boolInt(flagged), boolInt(answered), id)
	return err
}

// UpdateMailMessageFlagsByUID refreshes flags discovered during sync.
func UpdateMailMessageFlagsByUID(folderID int64, uid uint32, seen, flagged, answered bool) error {
	_, err := store.WriteExec(`UPDATE mail_messages SET seen = ?, flagged = ?, answered = ?
		WHERE folder_id = ? AND uid = ?`, boolInt(seen), boolInt(flagged), boolInt(answered), folderID, uid)
	return err
}

// MailMessageFilter is the list query.
type MailMessageFilter struct {
	AccountID int64
	FolderID  int64
	Limit     int
	BeforeAt  int64
	BeforeID  int64
	Query     string
}

// ListMailMessages returns a page of headers, newest first.
func ListMailMessages(f MailMessageFilter) ([]MailMessage, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT m.id, m.account_id, m.folder_id, m.uid, m.message_id, m.subject,
		m.from_name, m.from_addr, m.to_addrs, m.cc_addrs, m.reply_to, m.in_reply_to,
		m.refs, m.sent_at, m.size, m.seen, m.flagged, m.answered, m.has_attachments,
		m.snippet, m.body_fetched
		FROM mail_messages m WHERE m.folder_id = ?`
	args := []any{f.FolderID}
	if f.BeforeAt > 0 {
		q += ` AND (m.sent_at < ? OR (m.sent_at = ? AND m.id < ?))`
		args = append(args, f.BeforeAt, f.BeforeAt, f.BeforeID)
	}
	if s := strings.TrimSpace(f.Query); s != "" {
		like := "%" + s + "%"
		q += ` AND (m.subject LIKE ? OR m.from_addr LIKE ? OR m.from_name LIKE ? OR m.snippet LIKE ?)`
		args = append(args, like, like, like, like)
	}
	q += ` ORDER BY m.sent_at DESC, m.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := store.ReadDB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []MailMessage
	for rows.Next() {
		m, err := scanMailMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const mailMessageColumns = `id, account_id, folder_id, uid, message_id, subject,
	from_name, from_addr, to_addrs, cc_addrs, reply_to, in_reply_to, refs, sent_at,
	size, seen, flagged, answered, has_attachments, snippet, body_fetched`

func scanMailMessage(sc interface{ Scan(...any) error }) (MailMessage, error) {
	var m MailMessage
	var toJSON, ccJSON string
	var seen, flagged, answered, hasAtt, fetched int
	err := sc.Scan(&m.ID, &m.AccountID, &m.FolderID, &m.UID, &m.MessageID, &m.Subject,
		&m.FromName, &m.FromAddr, &toJSON, &ccJSON, &m.ReplyTo, &m.InReplyTo,
		&m.References, &m.SentAt, &m.Size, &seen, &flagged, &answered, &hasAtt,
		&m.Snippet, &fetched)
	m.To = unmarshalAddrs(toJSON)
	m.Cc = unmarshalAddrs(ccJSON)
	m.Seen = seen != 0
	m.Flagged = flagged != 0
	m.Answered = answered != 0
	m.HasAttachments = hasAtt != 0
	m.BodyFetched = fetched != 0
	return m, err
}

// GetMailMessage loads one header.
func GetMailMessage(id int64) (MailMessage, error) {
	row := store.ReadDB().QueryRow(`SELECT `+mailMessageColumns+` FROM mail_messages WHERE id = ?`, id)
	m, err := scanMailMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MailMessage{}, ErrMailMessageNotFound
	}
	return m, err
}

// GetMailMessageBody returns the stored body of a message.
func GetMailMessageBody(id int64) (text, htmlBody string, fetched bool, err error) {
	var t, h sql.NullString
	var f int
	err = store.ReadDB().QueryRow(`SELECT body_text, body_html, body_fetched FROM mail_messages WHERE id = ?`, id).
		Scan(&t, &h, &f)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, ErrMailMessageNotFound
	}
	return t.String, h.String, f != 0, err
}

// ErrMailMessageNotFound is returned for an unknown message id.
var ErrMailMessageNotFound = errors.New("mail message not found")

// SaveMailMessageBody stores the decoded body and marks the message fetched.
func SaveMailMessageBody(id int64, text, htmlBody, snippet string) error {
	_, err := store.WriteExec(`UPDATE mail_messages SET body_text = ?, body_html = ?,
		snippet = ?, body_fetched = 1 WHERE id = ?`, text, htmlBody, snippet, id)
	return err
}

// DeleteMailMessageRow removes one cached message (and its attachment rows).
func DeleteMailMessageRow(id int64) error {
	if _, err := store.WriteExec(`DELETE FROM mail_attachments WHERE message_id = ?`, id); err != nil {
		return err
	}
	_, err := store.WriteExec(`DELETE FROM mail_messages WHERE id = ?`, id)
	return err
}

// MailFolderCachedCount reports how many messages of a folder are cached
// locally. Compared with MailFolder.Total (the server's count) it tells the UI
// whether scrolling back can still load more.
func MailFolderCachedCount(folderID int64) (int, error) {
	var n int
	err := store.ReadDB().QueryRow(`SELECT COUNT(*) FROM mail_messages WHERE folder_id = ?`, folderID).Scan(&n)
	return n, err
}

// AdjustMailFolderUnread moves the cached unread counter by delta.
//
// The authoritative number comes from STATUS on every sync; this delta keeps
// the badge honest between syncs without recounting rows (a folder can hold
// thousands of messages the cache never saw).
func AdjustMailFolderUnread(folderID int64, delta int) error {
	_, err := store.WriteExec(`UPDATE mail_folders SET unread = MAX(0, unread + ?) WHERE id = ?`,
		delta, folderID)
	return err
}

// MarkMailFolderReadLocally mirrors a folder-wide "mark all read" into the
// cache: every cached row becomes seen and the unread counter drops to zero.
// The mailbox is what actually marks the messages; this only keeps the list
// from still showing unread rows until the next sync.
func MarkMailFolderReadLocally(folderID int64) error {
	if _, err := store.WriteExec(`UPDATE mail_messages SET seen = 1 WHERE folder_id = ? AND seen = 0`,
		folderID); err != nil {
		return err
	}
	_, err := store.WriteExec(`UPDATE mail_folders SET unread = 0 WHERE id = ?`, folderID)
	return err
}

// --- attachments ---

// SaveMailAttachments replaces the attachment rows of a message.
func SaveMailAttachments(messageID int64, atts []MailAttachment) error {
	if _, err := store.WriteExec(`DELETE FROM mail_attachments WHERE message_id = ?`, messageID); err != nil {
		return err
	}
	for _, a := range atts {
		_, err := store.WriteExec(`INSERT INTO mail_attachments
			(message_id, part_id, filename, content_type, size, content_id, inline, local_path)
			VALUES (?,?,?,?,?,?,?,?)`,
			messageID, a.PartID, a.Filename, a.ContentType, a.Size, a.ContentID,
			boolInt(a.Inline), a.LocalPath)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListMailAttachments returns the attachments of one message.
func ListMailAttachments(messageID int64) ([]MailAttachment, error) {
	rows, err := store.ReadDB().Query(`SELECT id, message_id, part_id, filename, content_type,
		size, content_id, inline, local_path FROM mail_attachments
		WHERE message_id = ? ORDER BY id`, messageID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []MailAttachment
	for rows.Next() {
		var a MailAttachment
		var inline int
		if err := rows.Scan(&a.ID, &a.MessageID, &a.PartID, &a.Filename, &a.ContentType,
			&a.Size, &a.ContentID, &inline, &a.LocalPath); err != nil {
			return nil, err
		}
		a.Inline = inline != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetMailAttachment loads one attachment row.
func GetMailAttachment(id int64) (MailAttachment, error) {
	var a MailAttachment
	var inline int
	err := store.ReadDB().QueryRow(`SELECT id, message_id, part_id, filename, content_type,
		size, content_id, inline, local_path FROM mail_attachments WHERE id = ?`, id).
		Scan(&a.ID, &a.MessageID, &a.PartID, &a.Filename, &a.ContentType, &a.Size,
			&a.ContentID, &inline, &a.LocalPath)
	a.Inline = inline != 0
	if errors.Is(err, sql.ErrNoRows) {
		return MailAttachment{}, ErrMailAttachmentNotFound
	}
	return a, err
}

// ErrMailAttachmentNotFound is returned for an unknown attachment id.
var ErrMailAttachmentNotFound = errors.New("mail attachment not found")

// --- unread summary ---

// MailUnreadSummary is one account's unread total, for the dock badge.
type MailUnreadSummary struct {
	AccountID int64  `json:"accountId"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Unread    int    `json:"unread"`
}

// MailUnreadTotals returns unread counts per account plus the grand total.
func MailUnreadTotals() ([]MailUnreadSummary, int, error) {
	rows, err := store.ReadDB().Query(`SELECT a.id, a.name, a.email,
		COALESCE(SUM(f.unread), 0)
		FROM mail_accounts a LEFT JOIN mail_folders f ON f.account_id = a.id
		GROUP BY a.id ORDER BY a.sort_order, a.id`)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var out []MailUnreadSummary
	total := 0
	for rows.Next() {
		var s MailUnreadSummary
		if err := rows.Scan(&s.AccountID, &s.Name, &s.Email, &s.Unread); err != nil {
			return nil, 0, err
		}
		total += s.Unread
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// --- drafts ---

// ListMailDrafts returns the drafts of one account, newest first.
func ListMailDrafts(accountID int64) ([]MailDraft, error) {
	rows, err := store.ReadDB().Query(`SELECT id, account_id, to_addrs, cc_addrs,
		bcc_addrs, subject, body_text, body_html, in_reply_to, refs, attachments, updated_at
		FROM mail_drafts WHERE account_id = ? ORDER BY updated_at DESC, id DESC`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []MailDraft
	for rows.Next() {
		d, err := scanMailDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanMailDraft(sc interface{ Scan(...any) error }) (MailDraft, error) {
	var d MailDraft
	var attJSON string
	err := sc.Scan(&d.ID, &d.AccountID, &d.To, &d.Cc, &d.Bcc, &d.Subject,
		&d.BodyText, &d.BodyHTML, &d.InReplyTo, &d.References, &attJSON, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	d.Attachments = []MailDraftFile{}
	if attJSON != "" {
		_ = json.Unmarshal([]byte(attJSON), &d.Attachments)
	}
	return d, nil
}

// GetMailDraft loads one draft.
func GetMailDraft(id int64) (MailDraft, error) {
	row := store.ReadDB().QueryRow(`SELECT id, account_id, to_addrs, cc_addrs,
		bcc_addrs, subject, body_text, body_html, in_reply_to, refs, attachments, updated_at
		FROM mail_drafts WHERE id = ?`, id)
	d, err := scanMailDraft(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MailDraft{}, ErrMailDraftNotFound
	}
	return d, err
}

// ErrMailDraftNotFound is returned for an unknown draft id.
var ErrMailDraftNotFound = errors.New("mail draft not found")

// SaveMailDraft inserts (id==0) or updates a draft and returns it.
func SaveMailDraft(d MailDraft) (MailDraft, error) {
	att, _ := json.Marshal(d.Attachments)
	now := time.Now().Unix()
	if d.ID == 0 {
		res, err := store.WriteExec(`INSERT INTO mail_drafts
			(account_id, to_addrs, cc_addrs, bcc_addrs, subject, body_text, body_html,
			 in_reply_to, refs, attachments, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			d.AccountID, d.To, d.Cc, d.Bcc, d.Subject, d.BodyText, d.BodyHTML,
			d.InReplyTo, d.References, string(att), now)
		if err != nil {
			return MailDraft{}, err
		}
		id, _ := res.LastInsertId()
		return GetMailDraft(id)
	}
	_, err := store.WriteExec(`UPDATE mail_drafts SET to_addrs = ?, cc_addrs = ?,
		bcc_addrs = ?, subject = ?, body_text = ?, body_html = ?, in_reply_to = ?,
		refs = ?, attachments = ?, updated_at = ? WHERE id = ?`,
		d.To, d.Cc, d.Bcc, d.Subject, d.BodyText, d.BodyHTML, d.InReplyTo,
		d.References, string(att), now, d.ID)
	if err != nil {
		return MailDraft{}, err
	}
	return GetMailDraft(d.ID)
}

// DeleteMailDraft removes one draft.
func DeleteMailDraft(id int64) error {
	_, err := store.WriteExec(`DELETE FROM mail_drafts WHERE id = ?`, id)
	return err
}

// --- helpers ---

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func marshalAddrs(addrs []MailAddr) string {
	if len(addrs) == 0 {
		return "[]"
	}
	b, err := json.Marshal(addrs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func unmarshalAddrs(s string) []MailAddr {
	out := []MailAddr{}
	if s == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []MailAddr{}
	}
	return out
}

// FormatMailAddrs renders a recipient list for the composer's "To" field.
func FormatMailAddrs(addrs []MailAddr) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, a.Addr)
	}
	return strings.Join(parts, ", ")
}
