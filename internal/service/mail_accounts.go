package service

import (
	"context"
	"errors"
	"log/slog"

	"clawbench/internal/store"
)

// ErrMailAccountInvalid is returned when the account form is missing required
// fields. The handler maps it to a 400 with a localized message.
var ErrMailAccountInvalid = errors.New("invalid mail account")

// TestMailAccount opens a connection and lists folders, which is the cheapest
// way to prove the address, password and TLS settings all work.
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
	_, err = client.ListFolders(ctx)
	return err
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
