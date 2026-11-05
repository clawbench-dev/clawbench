package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/mail"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// mailAttachmentsDir is where decoded attachments are written. Files are only
// materialized when the user opens or downloads a message, so the directory
// grows with actual usage instead of with mailbox size.
func mailAttachmentsDir(messageID int64) string {
	return filepath.Join(model.DataDir, "mail", "attachments", fmt.Sprintf("%d", messageID))
}

// GetMailMessageDetail returns the reader view, fetching and decoding the body
// on first open.
func GetMailMessageDetail(ctx context.Context, id int64, markSeen bool) (MailMessageDetail, error) {
	msg, err := GetMailMessage(id)
	if err != nil {
		return MailMessageDetail{}, err
	}
	if !msg.BodyFetched {
		if ferr := fetchAndStoreMailBody(ctx, msg, true); ferr != nil {
			return MailMessageDetail{}, ferr
		}
		msg, err = GetMailMessage(id)
		if err != nil {
			return MailMessageDetail{}, err
		}
	} else if markSeen && !msg.Seen {
		if ferr := setMailMessageFlags(ctx, msg, true, msg.Flagged); ferr != nil {
			slog.Debug("mail: mark seen failed", "id", id, "error", ferr)
		} else {
			msg.Seen = true
		}
	}

	text, htmlBody, _, err := GetMailMessageBody(id)
	if err != nil {
		return MailMessageDetail{}, err
	}
	atts, err := ListMailAttachments(id)
	if err != nil {
		return MailMessageDetail{}, err
	}
	return MailMessageDetail{
		MailMessage: msg,
		BodyText:    text,
		BodyHTML:    htmlBody,
		Attachments: atts,
	}, nil
}

func fetchAndStoreMailBody(ctx context.Context, msg MailMessage, markSeen bool) error {
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

	raw, err := client.FetchRaw(ctx, folder.Path, msg.UID)
	if err != nil {
		return err
	}
	if err := storeMailBody(msg, raw, true); err != nil {
		return err
	}
	if markSeen && !msg.Seen {
		if err := client.StoreFlags(ctx, folder.Path, msg.UID, []string{"\\Seen"}, nil); err != nil {
			return err
		}
		if err := UpdateMailMessageFlags(msg.ID, true, msg.Flagged, msg.Answered); err != nil {
			return err
		}
		if err := AdjustMailFolderUnread(msg.FolderID, -1); err != nil {
			slog.Debug("mail: adjust unread failed", "folder", msg.FolderID, "error", err)
		}
	}
	return nil
}

// storeMailBody decodes a raw message and persists its body, snippet and
// attachment metadata. writeFiles=false is used by the sync prefetch, which
// only needs the preview text.
func storeMailBody(msg MailMessage, raw []byte, writeFiles bool) error {
	pm, err := mail.Parse(raw)
	if err != nil {
		return err
	}
	snippet := mail.Snippet(pm.Text, pm.HTML, 200)
	if err := SaveMailMessageBody(msg.ID, pm.Text, pm.HTML, snippet); err != nil {
		return err
	}

	atts := make([]MailAttachment, 0, len(pm.Attachments))
	for _, a := range pm.Attachments {
		row := MailAttachment{
			MessageID:   msg.ID,
			PartID:      a.PartID,
			Filename:    sanitizeMailFilename(a.Filename),
			ContentType: a.ContentType,
			Size:        a.Size,
			ContentID:   a.ContentID,
			Inline:      a.Inline,
		}
		if writeFiles && !a.Inline {
			if path, err := writeMailAttachmentFile(msg.ID, row.Filename, a.Content); err == nil {
				row.LocalPath = path
			} else {
				slog.Warn("mail: write attachment failed", "message", msg.ID, "file", row.Filename, "error", err)
			}
		}
		atts = append(atts, row)
	}
	if err := SaveMailAttachments(msg.ID, atts); err != nil {
		return err
	}
	if len(atts) > 0 && !msg.HasAttachments {
		if err := SetMailMessageHasAttachments(msg.ID); err != nil {
			slog.Debug("mail: mark attachments failed", "message", msg.ID, "error", err)
		}
	}
	return nil
}

// prefetchMailBodies decodes the newest messages so the list has preview lines
// and the first open is instant. Bounded on purpose: only messages that are
// already in the cache, still without a body, and small enough to be cheap.
func prefetchMailBodies(ctx context.Context, client mail.Client, folderID int64, path string, limit int) {
	rows, err := ListMailMessages(MailMessageFilter{FolderID: folderID, Limit: 50})
	if err != nil {
		return
	}
	var todo []MailMessage
	for _, m := range rows {
		if m.BodyFetched || m.Size > mailPrefetchMaxSize {
			continue
		}
		todo = append(todo, m)
		if len(todo) >= limit {
			break
		}
	}
	if len(todo) == 0 {
		return
	}

	uids := make([]uint32, 0, len(todo))
	for _, m := range todo {
		uids = append(uids, m.UID)
	}
	raws, err := client.FetchRawBatch(ctx, path, uids)
	if err != nil {
		slog.Debug("mail: prefetch bodies failed", "folder", path, "error", err)
		return
	}
	for _, m := range todo {
		raw, ok := raws[m.UID]
		if !ok {
			continue
		}
		if err := storeMailBody(m, raw, false); err != nil {
			slog.Debug("mail: prefetch parse failed", "message", m.ID, "error", err)
		}
	}
}

// OpenMailAttachment returns a path to the attachment's bytes, materializing
// the file if it was not written when the message was first decoded.
func OpenMailAttachment(ctx context.Context, attachmentID int64) (MailAttachment, string, error) {
	att, err := GetMailAttachment(attachmentID)
	if err != nil {
		return MailAttachment{}, "", err
	}
	if att.LocalPath != "" {
		if _, serr := os.Stat(att.LocalPath); serr == nil {
			return att, att.LocalPath, nil
		}
	}

	msg, err := GetMailMessage(att.MessageID)
	if err != nil {
		return MailAttachment{}, "", err
	}
	folder, err := GetMailFolder(msg.FolderID)
	if err != nil {
		return MailAttachment{}, "", err
	}
	acc, err := GetMailAccount(msg.AccountID)
	if err != nil {
		return MailAttachment{}, "", err
	}
	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return MailAttachment{}, "", err
	}
	defer func() { _ = client.Close() }()

	raw, err := client.FetchRaw(ctx, folder.Path, msg.UID)
	if err != nil {
		return MailAttachment{}, "", err
	}
	pm, err := mail.Parse(raw)
	if err != nil {
		return MailAttachment{}, "", err
	}
	for _, a := range pm.Attachments {
		if !sameAttachment(att, a) {
			continue
		}
		path, err := writeMailAttachmentFile(msg.ID, att.Filename, a.Content)
		if err != nil {
			return MailAttachment{}, "", err
		}
		_, _ = store.WriteExec(`UPDATE mail_attachments SET local_path = ? WHERE id = ?`, path, att.ID)
		return att, path, nil
	}
	return MailAttachment{}, "", ErrMailAttachmentNotFound
}

func sameAttachment(row MailAttachment, part mail.Attachment) bool {
	if row.PartID != "" && part.PartID != "" {
		return row.PartID == part.PartID
	}
	if row.ContentID != "" && part.ContentID != "" {
		return row.ContentID == part.ContentID
	}
	return row.Filename == sanitizeMailFilename(part.Filename)
}

func writeMailAttachmentFile(messageID int64, name string, content []byte) (string, error) {
	dir := mailAttachmentsDir(messageID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := uniqueMailPath(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func uniqueMailPath(dir, name string) string {
	base := sanitizeMailFilename(name)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	path := filepath.Join(dir, base)
	for i := 1; i < 100; i++ {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, len(dir), ext))
}

// sanitizeMailFilename strips directories, control characters and characters
// Windows rejects, and caps the length. An empty result becomes "attachment".
func sanitizeMailFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	if i := strings.LastIndexAny(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '<', '>', ':', '"', '|', '?', '*', 0:
			b.WriteRune('_')
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		out = "attachment"
	}
	if len([]rune(out)) > 120 {
		r := []rune(out)
		ext := filepath.Ext(out)
		keep := 120 - len([]rune(ext))
		if keep < 10 {
			keep = 120
			ext = ""
		}
		out = string(r[:keep]) + ext
	}
	return out
}

// DeleteMailAttachmentFiles removes the on-disk directory of one message.
func DeleteMailAttachmentFiles(messageID int64) {
	_ = os.RemoveAll(mailAttachmentsDir(messageID))
}
