package wechat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/model"
)

// syncBufDir is the per-bot state directory.
func syncBufDir() string {
	return filepath.Join(model.DataDir, "wechat")
}

// syncBufPath is the long-poll cursor file for one bot.
//
// It lives under DataDir rather than in SQLite: it is a runtime cursor, not user
// data, and a table would drag in a docs/schema update for something the server
// can safely regenerate.
//
// The path is keyed by bot id because a cursor is only meaningful to the account
// that produced it. With a single shared file, logging out of bot A and scanning
// into bot B would start B's monitor from A's cursor: either the server rejects
// the foreign value and the SDK retries the same bad buf forever (OnBufUpdate
// only fires on success, so it is never replaced), leaving the bot permanently
// deaf, or the value is accepted and B's pending backlog is silently skipped.
func syncBufPath(botID string) string {
	name := "sync_buf"
	if safe := sanitizeBotID(botID); safe != "" {
		name += "." + safe
	}
	return filepath.Join(syncBufDir(), name)
}

// sanitizeBotID reduces a bot id to characters that are safe in a filename.
//
// The id comes from the login response, i.e. from the network, so it must not be
// able to escape the state directory (a value like "../../etc/passwd").
func sanitizeBotID(botID string) string {
	var b strings.Builder
	for _, r := range botID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// loadSyncBuf reads the persisted cursor for a bot, returning "" when none was
// written.
//
// A missing file is the normal first-run state, not an error. Without a
// persisted cursor every server restart can redeliver the inbound backlog as
// new messages, which would re-run old questions as fresh AI turns.
func loadSyncBuf(botID string) string {
	data, err := os.ReadFile(syncBufPath(botID))
	if err != nil {
		return ""
	}
	return string(data)
}

// saveSyncBuf persists the cursor atomically so a crash mid-write cannot leave
// a truncated value that would replay from an arbitrary point.
func saveSyncBuf(botID, buf string) error {
	path := syncBufPath(botID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create wechat state dir: %w", err)
	}
	tmp := path + ".tmp"
	// 0600: the cursor is opaque but it is per-account state, matching the
	// permission on config.yaml.
	if err := os.WriteFile(tmp, []byte(buf), 0o600); err != nil {
		return fmt.Errorf("write sync buf: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename sync buf: %w", err)
	}
	return nil
}

// ClearSyncBuf drops a bot's cursor. Called on logout: the next account must not
// inherit it.
func ClearSyncBuf(botID string) error {
	if err := os.Remove(syncBufPath(botID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove sync buf: %w", err)
	}
	return nil
}
