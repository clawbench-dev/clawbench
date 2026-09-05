package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
)

// clientLogMu protects concurrent writes to the unified client log file.
// All sources (android native, js frontend) share ONE file; each line carries
// an inline [source] marker so entries can be distinguished when reading.
var clientLogMu sync.Mutex

// ClientLogEntry represents a single log entry from a client (Android app or JS frontend).
type ClientLogEntry struct {
	Level  string `json:"level"` // D, I, W, E
	Tag    string `json:"tag"`
	Msg    string `json:"msg"`
	Ts     int64  `json:"ts"`               // epoch millis
	Source string `json:"source,omitempty"` // "android" or "js"; defaults to "android" when empty
}

// clientLogRequest is the request body for POST /api/client-log.
type clientLogRequest struct {
	Entries []ClientLogEntry `json:"entries"`
}

// clientLogFilePath returns the unified client log file path.
func clientLogFilePath() string {
	return filepath.Join(model.ConfigInstance.LogDir, "client.log")
}

// effectiveSource returns the effective source, defaulting to "android" when empty.
func effectiveSource(s string) string {
	if s == "" {
		return "android"
	}
	return s
}

// ServeClientLog handles POST /api/client-log (and legacy POST /api/android-log).
// It receives batched log entries from clients and appends them to a single
// unified log file ({LogDir}/logs/client.log); each line carries an inline
// [js] / [android] marker for its origin.
func ServeClientLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	var req clientLogRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(req.Entries) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	// Cap at 200 entries per request
	if len(req.Entries) > 200 {
		req.Entries = req.Entries[:200]
	}

	// Format entries (one line per entry; escape newlines in messages).
	// Unified file — each line carries the effective source as [js]/[android].
	lines := make([]byte, 0, len(req.Entries)*128)
	for _, e := range req.Entries {
		t := time.UnixMilli(e.Ts)
		msg := strings.ReplaceAll(e.Msg, "\n", "\\n")
		src := effectiveSource(e.Source)
		line := fmt.Sprintf(
			"%s [%s] %s/%s: %s\n",
			t.Format("2006-01-02T15:04:05.000"),
			src,
			e.Level,
			e.Tag,
			msg,
		)
		lines = append(lines, line...)
	}

	clientLogMu.Lock()
	err := appendClientLog(lines)
	clientLogMu.Unlock()

	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("write client log: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"written": len(req.Entries)})
}

// clientLogMaxBytes is the client-log file cap. When an append would push the
// file past this size the current file is rotated to .1 (replacing any older
// .1) and a fresh file is started. client.log grows unboundedly otherwise
// (it is append-only with no rotation), eventually filling the disk.
const clientLogMaxBytes = 50 << 20 // 50 MiB

// appendClientLog appends formatted log lines to the unified client log file.
// Caller must hold clientLogMu.
func appendClientLog(lines []byte) error {
	path := clientLogFilePath()
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Rotate before appending when the file is already at/over the cap, so a
	// single huge batch cannot push a small file far past the limit either.
	if fi, err := os.Stat(path); err == nil && fi.Size()+int64(len(lines)) > clientLogMaxBytes {
		rotated := path + ".1"
		_ = os.Remove(rotated) // drop the previous generation
		if err := os.Rename(path, rotated); err != nil {
			// Not fatal: fall through and keep appending to the oversized file.
			slog.Warn("client log rotate failed", slog.String("path", path), slog.String("err", err.Error()))
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // log file, not security-sensitive
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(lines); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	return nil
}
