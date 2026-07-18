package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
)

// clientLogMu protects concurrent writes to client log files.
// Each source (android, js) gets its own mutex and log file.
var (
	androidClientLogMu sync.Mutex
	jsClientLogMu      sync.Mutex
)

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

// clientLogFilePath returns the log file path for the given source.
func clientLogFilePath(source string) string {
	name := "android.log"
	if source == "js" {
		name = "js.log"
	}
	return filepath.Join(model.ConfigInstance.LogDir, name)
}

// clientLogMu returns the mutex for the given source.
func clientLogMu(source string) *sync.Mutex {
	if source == "js" {
		return &jsClientLogMu
	}
	return &androidClientLogMu
}

// effectiveSource returns the effective source, defaulting to "android" when empty.
func effectiveSource(s string) string {
	if s == "" {
		return "android"
	}
	return s
}

// ServeClientLog handles POST /api/client-log (and legacy POST /api/android-log).
// It receives batched log entries from clients and appends them to per-source
// log files (.clawbench/logs/android.log, .clawbench/logs/js.log) in a
// human-readable format.
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

	// Group entries by effective source
	groups := make(map[string][]ClientLogEntry)
	for _, e := range req.Entries {
		src := effectiveSource(e.Source)
		groups[src] = append(groups[src], e)
	}

	totalWritten := 0

	for src, entries := range groups {
		// Format entries (one line per entry; escape newlines in messages)
		lines := make([]byte, 0, len(entries)*128)
		for _, e := range entries {
			t := time.UnixMilli(e.Ts)
			msg := strings.ReplaceAll(e.Msg, "\n", "\\n")
			line := fmt.Sprintf(
				"%s %s/%s: %s\n",
				t.Format("2006-01-02T15:04:05.000"),
				e.Level,
				e.Tag,
				msg,
			)
			lines = append(lines, line...)
		}

		// Append to file (source-specific mutex)
		mu := clientLogMu(src)
		mu.Lock()
		err := appendClientLog(src, lines)
		mu.Unlock()

		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("write %s client log: %w", src, err)))
			return
		}
		totalWritten += len(entries)
	}

	writeJSON(w, http.StatusOK, map[string]any{"written": totalWritten})
}

// appendClientLog appends formatted log lines to the source-specific log file.
// Caller must hold the appropriate mutex.
func appendClientLog(source string, lines []byte) error {
	path := clientLogFilePath(source)
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
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
