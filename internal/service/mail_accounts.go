package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"clawbench/internal/mail"
	"clawbench/internal/store"
)

// ErrMailAccountInvalid is returned when the account form is missing required
// fields. The handler maps it to a 400 with a localized message.
var ErrMailAccountInvalid = errors.New("invalid mail account")

// TestMailAccount opens a connection, lists folders and opens the inbox.
//
// Listing alone is not a real probe: 163/126 answer LIST happily and then
// refuse SELECT, which the user experiences as "connected" followed by a sync
// that never brings any mail. Opening a folder is the first thing a sync does,
// so it belongs in the test.
func TestMailAccount(ctx context.Context, id int64) error {
	acc, err := GetMailAccount(id)
	if err != nil {
		return err
	}
	client, err := mailDialer(ctx, acc.ProtocolAccount())
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	folders, err := client.ListFolders(ctx)
	if err != nil {
		return err
	}
	folder := probeFolder(folders)
	if folder == "" {
		return nil
	}
	return client.OpenFolder(ctx, folder)
}

// probeFolder picks the mailbox the connection probe opens: the inbox when the
// server labels it, then the conventional INBOX name (most servers do not send
// SPECIAL-USE), then whatever the server listed first.
func probeFolder(folders []mail.Folder) string {
	for _, f := range folders {
		if f.SpecialUse == mail.SpecialUseInbox {
			return f.Path
		}
	}
	for _, f := range folders {
		if strings.EqualFold(f.Path, "INBOX") && f.Selectable {
			return f.Path
		}
	}
	for _, f := range folders {
		if f.Selectable {
			return f.Path
		}
	}
	return ""
}

// DeleteMailAccountAttachmentFiles removes the on-disk attachment directories
// of every message that belongs to the account.
func DeleteMailAccountAttachmentFiles(accountID int64) {
	rows, err := store.ReadDB().Query(`SELECT id FROM mail_messages WHERE account_id = ?`, accountID)
	if err != nil {
		slog.Debug("mail: list messages for cleanup failed", "account", accountID, "error", err)
		return
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Debug("mail: iterate messages for cleanup failed", "account", accountID, "error", err)
		return
	}
	for _, id := range ids {
		DeleteMailAttachmentFiles(id)
	}
}
