package service

import (
	"context"
	"fmt"

	"clawbench/internal/store"
)

// ShareRevokeResult reports how many links of each kind the master switch
// revoked. It exists so the PATCH response can say what actually happened —
// "0 revoked" and "everything revoked" look identical to a user otherwise.
type ShareRevokeResult struct {
	FileShares    int64
	SessionShares int64
	MeetingShares int64
}

// RevokeAllShares revokes every issued share link, across all three kinds.
//
// This is what the `share.enabled = false` switch means: the links are
// capability URLs, so "turned off" can only be true if the capability itself is
// gone. Deleting the rows (and clearing the meeting publication flag) makes old
// links 404 immediately and is deliberately NOT reversible — turning the switch
// back on must not resurrect links the user was told had been revoked.
//
// Runs in one transaction so a failure cannot leave file links revoked while
// meeting records stay public (or the reverse).
func RevokeAllShares() (ShareRevokeResult, error) {
	var out ShareRevokeResult

	ctx := context.Background()
	tx, err := store.WriteBegin()
	if err != nil {
		return out, err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM file_shares`)
	if err != nil {
		return out, fmt.Errorf("revoke file shares: %w", err)
	}
	out.FileShares, _ = res.RowsAffected()

	res, err = tx.ExecContext(ctx, `DELETE FROM session_shares`)
	if err != nil {
		return out, fmt.Errorf("revoke session shares: %w", err)
	}
	out.SessionShares, _ = res.RowsAffected()

	// Meetings are not deleted: the owner still reads them in the meeting list.
	// Only the public flag is cleared, which is what every archived-record
	// endpoint gates on (PublicMeetingByTokenShared).
	res, err = tx.ExecContext(ctx, `UPDATE live_notes SET share_enabled = 0, share_expires_at = NULL WHERE share_enabled != 0`)
	if err != nil {
		return out, fmt.Errorf("revoke meeting shares: %w", err)
	}
	out.MeetingShares, _ = res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("commit share revocation: %w", err)
	}
	return out, nil
}
