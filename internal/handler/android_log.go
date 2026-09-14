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
	"unicode"

	"clawbench/internal/model"
)

// clientLogMu protects concurrent writes to the unified client log file.
// All sources (android native, js frontend) share ONE file; each line carries
// an inline [source] marker so entries can be distinguished when reading.
var clientLogMu sync.Mutex

// Per-field caps. Every one of these fields is attacker-controlled, and a log
// line is only useful if its shape is predictable — an unbounded field both
// lets a client forge structure and lets a single request exhaust the file cap.
const (
	clientLogMaxMsgLen   = 4096
	clientLogMaxTagLen   = 128
	clientLogMaxLevelLen = 8
	// clientLogMaxBodyBytes bounds one request's total contribution to the file,
	// independent of entry count, so 200 entries cannot each carry 4 KiB.
	clientLogMaxBodyBytes = 256 << 10 // 256 KiB
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

// sanitizeLogField makes an attacker-controlled value safe to embed in one
// log line.
//
// Escaping only Msg is not enough: every field is placed into the line, so a
// line break in ANY of them splits the record and lets a caller forge
// additional entries (e.g. Tag = "x\n<timestamp> [js] I/Real: ..."). All fields
// are escaped for that reason.
//
// Nor is escaping only "\n"/"\r" enough. Readers disagree about what ends a
// line, and several of those characters survive a newline-only blacklist:
// Python's str.splitlines() also breaks on VT, FF, FS/GS/RS and NEL, and JS
// treats U+2028/U+2029 as line terminators in its grammar. So this escapes
// every control character (unicode.IsControl covers C0, DEL and C1 — including
// NEL) plus the two Unicode line separators that are not classified as
// controls. Anything printable passes through untouched.
//
// Escaping is used rather than stripping so the log still shows that the input
// contained something odd — this is a debug log, and silently losing the
// structure would hide the very thing an operator is looking for.
func sanitizeLogField(s string, maxLen int) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == 0:
			b.WriteString(`\0`)
		case unicode.IsControl(r) || r == '\u2028' || r == '\u2029':
			// Any other control char (VT, FF, FS/GS/RS, NEL, DEL, …) or a
			// Unicode line separator. Render as an escape so it stays visible
			// but cannot act as a record break.
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}

	out := b.String()
	if len(out) > maxLen {
		// Truncate on a rune boundary so the file stays valid UTF-8.
		out = strings.ToValidUTF8(out[:maxLen], "")
		out += "…[truncated]"
	}
	return out
}

// clientLogEntryLine renders one entry as a single log line. Caller must have
// sanitized the fields (see sanitizeLogField).
func clientLogEntryLine(e ClientLogEntry) string {
	return fmt.Sprintf(
		"%s [%s] %s/%s: %s\n",
		time.UnixMilli(e.Ts).Format("2006-01-02T15:04:05.000"),
		effectiveSource(e.Source),
		e.Level,
		e.Tag,
		e.Msg,
	)
}

// ServeClientLog handles POST /api/client-log. It receives batched log entries
// from clients and appends them to a single unified log file
// ({LogDir}/logs/client.log); each line carries an inline [js] / [android]
// marker for its origin.
//
// No rate limit, deliberately. Disk usage is already bounded by the per-request
// byte cap plus the 50 MiB file cap with rotation, and the endpoint is
// auth-protected, so a limiter would not change either bound — it would only
// throttle churn. Every other authenticated endpoint is unlimited, and a 429
// here is silently dropped by both clients (appLog.ts discards without retry),
// so a limiter would trade a real risk of losing logs for no security gain.
// Adding one would need to be a global write-throttling middleware, not a
// special case for this endpoint.
func ServeClientLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Bound the body BEFORE decoding. The byte cap further down applies to
	// rendered lines, which is too late: json.Decode builds the whole
	// []ClientLogEntry first, so without this a single request could make the
	// server allocate hundreds of MiB (200 entries × arbitrarily large Msg).
	//
	// The 2× headroom over clientLogMaxBodyBytes is deliberate: the two caps
	// measure different things (raw JSON vs. rendered lines) and escaping can
	// expand the body. A consequence is that a 200-entry batch of maximum-size
	// messages (~830 KiB) is rejected here rather than truncated by the append
	// cap — which is fine, and still leaves the append cap binding for any
	// batch that fits in the body limit.
	r.Body = http.MaxBytesReader(w, r.Body, clientLogMaxBodyBytes*2)

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

	// Format entries. EVERY field is attacker-controlled, so all of them are
	// sanitized — escaping only Msg would leave Tag/Level/Source free to forge
	// whole lines. The total is also bounded so one request cannot dominate the
	// file regardless of how the entries are distributed.
	lines := make([]byte, 0, len(req.Entries)*128)
	written := 0
	for _, e := range req.Entries {
		e.Msg = sanitizeLogField(e.Msg, clientLogMaxMsgLen)
		e.Tag = sanitizeLogField(e.Tag, clientLogMaxTagLen)
		e.Level = sanitizeLogField(e.Level, clientLogMaxLevelLen)
		e.Source = sanitizeLogField(e.Source, clientLogMaxLevelLen)

		line := clientLogEntryLine(e)
		if len(lines)+len(line) > clientLogMaxBodyBytes {
			break
		}
		lines = append(lines, line...)
		written++
	}

	clientLogMu.Lock()
	err := appendClientLog(lines)
	clientLogMu.Unlock()

	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("write client log: %w", err)))
		return
	}

	// Report what was actually appended, not what was received: a batch can be
	// cut short by the body cap, and a count that disagrees with the file makes
	// this diagnostic endpoint untrustworthy.
	writeJSON(w, http.StatusOK, map[string]any{"written": written})
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
