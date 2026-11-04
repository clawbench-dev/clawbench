package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawbench/internal/mail"
)

// MailFlagPatch is a partial flag update from the reader's action bar.
type MailFlagPatch struct {
	Seen    *bool `json:"seen"`
	Flagged *bool `json:"flagged"`
}

// SetMailMessageFlags pushes a flag change to the server and mirrors it
// locally. The server is the source of truth: if it rejects the change the
// local row is left alone and the caller sees the error.
func SetMailMessageFlags(ctx context.Context, id int64, patch MailFlagPatch) (MailMessage, error) {
	msg, err := GetMailMessage(id)
	if err != nil {
		return MailMessage{}, err
	}
	seen, flagged := msg.Seen, msg.Flagged
	if patch.Seen != nil {
		seen = *patch.Seen
	}
	if patch.Flagged != nil {
		flagged = *patch.Flagged
	}
	if seen == msg.Seen && flagged == msg.Flagged {
		return msg, nil
	}
	if err := setMailMessageFlags(ctx, msg, seen, flagged); err != nil {
		return MailMessage{}, err
	}
	return GetMailMessage(id)
}

func setMailMessageFlags(ctx context.Context, msg MailMessage, seen, flagged bool) error {
	folder, err := GetMailFolder(msg.FolderID)
	if err != nil {
		return err
	}
	acc, err := GetMailAccount(msg.AccountID)
	if err != nil {
		return err
	}
	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	var add, remove []string
	if seen != msg.Seen {
		if seen {
			add = append(add, "\\Seen")
		} else {
			remove = append(remove, "\\Seen")
		}
	}
	if flagged != msg.Flagged {
		if flagged {
			add = append(add, "\\Flagged")
		} else {
			remove = append(remove, "\\Flagged")
		}
	}
	if err := client.StoreFlags(ctx, folder.Path, msg.UID, add, remove); err != nil {
		return err
	}
	if err := UpdateMailMessageFlags(msg.ID, seen, flagged, msg.Answered); err != nil {
		return err
	}
	if seen != msg.Seen {
		delta := 1
		if seen {
			delta = -1
		}
		if err := AdjustMailFolderUnread(msg.FolderID, delta); err != nil {
			return err
		}
	}
	return nil
}

// MarkMailFolderRead marks every message of a folder as read.
//
// The mailbox is the source of truth: the server call runs first and a failure
// leaves the local cache untouched, exactly like a single-message flag change.
// The cache update then mirrors the result so the list and the unread counter
// do not wait for the next sync.
func MarkMailFolderRead(ctx context.Context, folderID int64) error {
	folder, err := GetMailFolder(folderID)
	if err != nil {
		return err
	}
	acc, err := GetMailAccount(folder.AccountID)
	if err != nil {
		return err
	}
	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := client.MarkAllSeen(ctx, folder.Path); err != nil {
		return err
	}
	return MarkMailFolderReadLocally(folderID)
}

// MoveMailMessage moves one message to another folder. The local row is
// dropped rather than rewritten: the destination folder picks it up on its next
// sync with the destination's own UID.
func MoveMailMessage(ctx context.Context, id, destFolderID int64) error {
	msg, err := GetMailMessage(id)
	if err != nil {
		return err
	}
	src, err := GetMailFolder(msg.FolderID)
	if err != nil {
		return err
	}
	dest, err := GetMailFolder(destFolderID)
	if err != nil {
		return err
	}
	if src.ID == dest.ID {
		return nil
	}
	acc, err := GetMailAccount(msg.AccountID)
	if err != nil {
		return err
	}
	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := client.Move(ctx, src.Path, msg.UID, dest.Path); err != nil {
		return err
	}
	if err := DeleteMailMessageRow(id); err != nil {
		return err
	}
	DeleteMailAttachmentFiles(id)
	if !msg.Seen {
		_ = AdjustMailFolderUnread(src.ID, -1)
	}
	return nil
}

// DeleteMailMessage removes a message. Unless permanent is set and the account
// has a Trash folder, it is moved there instead of being expunged.
func DeleteMailMessage(ctx context.Context, id int64, permanent bool) error {
	msg, err := GetMailMessage(id)
	if err != nil {
		return err
	}
	folder, err := GetMailFolder(msg.FolderID)
	if err != nil {
		return err
	}

	if !permanent && folder.SpecialUse != mail.SpecialUseTrash {
		if trash, ok := findSpecialFolder(msg.AccountID, mail.SpecialUseTrash); ok {
			return MoveMailMessage(ctx, id, trash.ID)
		}
	}

	acc, err := GetMailAccount(msg.AccountID)
	if err != nil {
		return err
	}
	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if err := client.Delete(ctx, folder.Path, msg.UID); err != nil {
		return err
	}
	if err := DeleteMailMessageRow(id); err != nil {
		return err
	}
	DeleteMailAttachmentFiles(id)
	if !msg.Seen {
		_ = AdjustMailFolderUnread(folder.ID, -1)
	}
	return nil
}

func findSpecialFolder(accountID int64, special string) (MailFolder, bool) {
	folders, err := ListMailFolders(accountID)
	if err != nil {
		return MailFolder{}, false
	}
	for _, f := range folders {
		if f.SpecialUse == special {
			return f, true
		}
	}
	return MailFolder{}, false
}

// MailOutgoingAttachment is a file already on disk, referenced by the composer.
type MailOutgoingAttachment struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
}

// MailSendRequest is the composer payload.
type MailSendRequest struct {
	AccountID   int64                    `json:"accountId"`
	To          []string                 `json:"to"`
	Cc          []string                 `json:"cc"`
	Bcc         []string                 `json:"bcc"`
	Subject     string                   `json:"subject"`
	Text        string                   `json:"text"`
	HTML        string                   `json:"html"`
	InReplyTo   string                   `json:"inReplyTo"`
	References  string                   `json:"references"`
	Attachments []MailOutgoingAttachment `json:"attachments"`
	DraftID     int64                    `json:"draftId"`
}

// mailMaxAttachmentBytes caps one outgoing attachment.
const mailMaxAttachmentBytes = 25 * 1024 * 1024

// SendMail sends one message and appends a copy to the Sent folder.
func SendMail(ctx context.Context, req MailSendRequest) error {
	if len(req.To) == 0 && len(req.Cc) == 0 && len(req.Bcc) == 0 {
		return ErrMailNoRecipients
	}
	acc, err := GetMailAccount(req.AccountID)
	if err != nil {
		return err
	}

	out := mail.SendRequest{
		From:       acc.Email,
		To:         cleanAddresses(req.To),
		Cc:         cleanAddresses(req.Cc),
		Bcc:        cleanAddresses(req.Bcc),
		Subject:    strings.TrimSpace(req.Subject),
		Text:       req.Text,
		HTML:       req.HTML,
		InReplyTo:  req.InReplyTo,
		References: splitReferences(req.References),
	}
	for _, a := range req.Attachments {
		data, name, rerr := readOutgoingAttachment(a)
		if rerr != nil {
			return rerr
		}
		out.Attachments = append(out.Attachments, mail.SendAttachment{
			Filename: name,
			Data:     data,
		})
	}

	raw, err := mailSender.Send(ctx, acc.ProtocolAccount(), out)
	if err != nil {
		return err
	}

	// Best effort: a mailbox without a Sent folder (or a server that refuses
	// APPEND) must not turn a successfully delivered message into an error.
	if sent, ok := findSpecialFolder(acc.ID, mail.SpecialUseSent); ok {
		client, derr := mailDialer(ctx, acc.ProtocolAccount())
		if derr == nil {
			if aerr := client.Append(ctx, sent.Path, raw, true); aerr != nil {
				_ = aerr // logged by the caller's sync error surface
			}
			_ = client.Close()
		}
	}

	if req.DraftID > 0 {
		_ = DeleteMailDraft(req.DraftID)
	}
	return nil
}

// ErrMailNoRecipients is returned when the composer has no recipient at all.
var ErrMailNoRecipients = errors.New("mail: no recipients")

func readOutgoingAttachment(a MailOutgoingAttachment) ([]byte, string, error) {
	path := filepath.Clean(a.Path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", &mail.Error{
			Kind: mail.KindServer, Op: "read attachment",
			Err: fmt.Errorf("%s does not exist", a.Filename),
		}
	}
	if info.Size() > mailMaxAttachmentBytes {
		return nil, "", &mail.Error{
			Kind: mail.KindServer, Op: "read attachment",
			Err: fmt.Errorf("%s exceeds 25MB", a.Filename),
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	name := a.Filename
	if name == "" {
		name = filepath.Base(path)
	}
	return data, sanitizeMailFilename(name), nil
}

func cleanAddresses(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func splitReferences(refs string) []string {
	fields := strings.Fields(refs)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// MailReplyContext builds the quoting fields for a reply/forward draft.
type MailReplyContext struct {
	InReplyTo  string `json:"inReplyTo"`
	References string `json:"references"`
	To         string `json:"to"`
	Cc         string `json:"cc"`
	Subject    string `json:"subject"`
	Quote      string `json:"quote"`
}

// BuildMailReplyContext derives the composer defaults for reply/reply-all/forward.
func BuildMailReplyContext(id int64, mode string) (MailReplyContext, error) {
	msg, err := GetMailMessage(id)
	if err != nil {
		return MailReplyContext{}, err
	}
	text, _, _, err := GetMailMessageBody(id)
	if err != nil {
		return MailReplyContext{}, err
	}
	refs := strings.TrimSpace(msg.References)
	if refs != "" {
		refs += " "
	}
	if msg.MessageID != "" {
		refs += "<" + strings.Trim(msg.MessageID, "<>") + ">"
	}

	subject := msg.Subject
	switch mode {
	case "forward":
		if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
			subject = "Fwd: " + subject
		}
	case "reply", "replyAll":
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
	}

	quote := buildQuote(msg, text)
	ctx := MailReplyContext{
		InReplyTo:  "<" + strings.Trim(msg.MessageID, "<>") + ">",
		References: refs,
		Subject:    subject,
		Quote:      quote,
	}
	switch mode {
	case "reply", "replyAll":
		ctx.To = msg.FromAddr
		if mode == "replyAll" {
			ctx.Cc = FormatMailAddrs(msg.To)
		}
	case "forward":
		ctx.InReplyTo = ""
		ctx.References = ""
	}
	if msg.MessageID == "" {
		ctx.InReplyTo = ""
	}
	return ctx, nil
}

func buildQuote(msg MailMessage, text string) string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString(backgroundText("MailQuoteHeader") + "\n")
	from := backgroundText("MailQuoteFrom") + ": "
	if msg.FromName != "" {
		b.WriteString(from + msg.FromName + " <" + msg.FromAddr + ">\n")
	} else if msg.FromAddr != "" {
		b.WriteString(from + msg.FromAddr + "\n")
	}
	if msg.SentAt > 0 {
		b.WriteString(backgroundText("MailQuoteDate") + ": " + time.Unix(msg.SentAt, 0).Format("2006-01-02 15:04") + "\n")
	}
	b.WriteString(backgroundText("MailQuoteSubject") + ": " + msg.Subject + "\n\n")
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}
