package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"clawbench/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupClientLogTest points the client log at a fresh temp dir and returns it.
// Auth is left unconfigured (setupTestEnv blanks both tokens), so the handler
// can be called directly without a session — these tests exercise the log
// handling, not the auth gate (see TestServeClientLog_RequiresAuth for that).
func setupClientLogTest(t *testing.T) string {
	t.Helper()
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	origLogDir := model.ConfigInstance.LogDir
	t.Cleanup(func() { model.ConfigInstance.LogDir = origLogDir })

	logDir := t.TempDir()
	model.ConfigInstance.LogDir = logDir
	return logDir
}

// readClientLog reads back the unified client log written during a test.
func readClientLog(t *testing.T, logDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(logDir, "client.log"))
	require.NoError(t, err, "handler should have written client.log")
	return string(data)
}

// appendClientLog rotation: once the log file would exceed clientLogMaxBytes,
// the current file is renamed to .1 (previous .1 dropped) and a fresh file is
// started, so client.log never grows without bound.
func TestAppendClientLog_RotatesPastCap(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir
	path := filepath.Join(tmpDir, "client.log")

	// Fill the file to exactly the cap (a single batch that lands at the cap
	// must NOT rotate; rotation happens only when the NEXT batch would exceed it).
	big := strings.Repeat("x", int(clientLogMaxBytes))
	require.NoError(t, appendClientLog([]byte(big)))

	// The next append — even a tiny one — would exceed the cap, so it must
	// rotate the current file first.
	require.NoError(t, appendClientLog([]byte("tail")))

	// Old content now lives in .1; the live file starts fresh with just "tail".
	rotated, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	assert.Equal(t, big, string(rotated))

	live, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "tail", string(live))
}

func TestAppendClientLog_SecondRotationDropsPreviousGen(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir
	path := filepath.Join(tmpDir, "client.log")

	require.NoError(t, appendClientLog([]byte(strings.Repeat("a", int(clientLogMaxBytes)))))
	// Rotate again with different content.
	require.NoError(t, appendClientLog([]byte(strings.Repeat("b", int(clientLogMaxBytes)))))
	require.NoError(t, appendClientLog([]byte("c")))

	// .1 holds the "b" generation only — the "a" generation was dropped.
	rotated, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("b", int(clientLogMaxBytes)), string(rotated))
	assert.False(t, strings.Contains(string(rotated), "a"))
}

// TestAppendClientLog_RenameFailureFallsThrough exercises the non-fatal rotate
// fall-through: when the rename to .1 fails (e.g. a non-empty directory is
// squatting on the .1 path), the append must still land in the live file
// instead of erroring out and losing the batch.
func TestAppendClientLog_RenameFailureFallsThrough(t *testing.T) {
	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()

	tmpDir := t.TempDir()
	model.ConfigInstance.LogDir = tmpDir
	path := filepath.Join(tmpDir, "client.log")
	rotated := path + ".1"

	// Fill the live file so the next append triggers the rotation branch.
	big := strings.Repeat("x", int(clientLogMaxBytes))
	require.NoError(t, appendClientLog([]byte(big)))

	// Occupy the .1 path with a non-empty directory: os.Remove(rotated) fails
	// (ENOTEMPTY) and os.Rename(client.log → client.log.1) fails (target not empty),
	// exercising the slog.Warn fall-through inside the rotation branch.
	require.NoError(t, os.MkdirAll(filepath.Join(rotated, "stub"), 0o755))
	require.NoError(t, appendClientLog([]byte("tail")))

	// Rotation failed, so the live file kept the full history…
	live, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, big+"tail", string(live))

	// …and the squatting directory was left untouched.
	fi, err := os.Stat(rotated)
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

// --- Hardening: field sanitization (end-to-end through the handler) ---
//
// These deliberately go through ServeClientLog and read back client.log rather
// than calling sanitizeLogField directly. An earlier version tested the helper
// in isolation and kept passing when the handler's calls to it were deleted —
// it proved nothing about the wiring.

// hostileSeparators are the characters different readers treat as a line
// break. Escaping only "\n"/"\r" is not enough: Python's str.splitlines()
// also breaks on VT/FF/FS/GS/RS/NEL, and JS treats U+2028/U+2029 as line
// terminators, so a forged record could still be produced with one of these.
var hostileSeparators = map[string]string{
	"LF":        "\n",
	"CR":        "\r",
	"VT":        "\v",
	"FF":        "\f",
	"FS":        "\x1c",
	"GS":        "\x1d",
	"RS":        "\x1e",
	"NEL":       "\u0085",
	"LS U+2028": "\u2028",
	"PS U+2029": "\u2029",
}

// Every field lands in the log line, so a break in ANY of them forges a record.
func TestServeClientLog_NoFieldCanForgeALine(t *testing.T) {
	for name, sep := range hostileSeparators {
		t.Run(name, func(t *testing.T) {
			logDir := setupClientLogTest(t)

			forged := "Legit" + sep + "2026-01-01T00:00:00.000 [js] I/Real: FORGED"
			body := map[string]any{"entries": []ClientLogEntry{{
				Level: forged, Tag: forged, Msg: forged, Source: forged,
				Ts: 1700000000000,
			}}}

			req := newRequest(t, http.MethodPost, "/api/client-log", body)
			w := callHandler(ServeClientLog, req)
			require.Equal(t, http.StatusOK, w.Code)

			content := readClientLog(t, logDir)

			// The security property: exactly ONE record. A forged entry would
			// add a second line terminator, because the attacker's payload
			// embeds a full "timestamp [src] level/tag: msg" line.
			assert.Equal(t, 1, strings.Count(content, "\n"),
				"entry must not split into extra records")

			// And the separator must not be followed by the attacker's forged
			// timestamp, which is what would make a second line look real.
			assert.NotContains(t, content, sep+"2026-01-01",
				"separator must not be followed by a forged timestamp")

			// No raw control byte survives into the file.
			if sep != "\n" {
				assert.NotContains(t, content, sep,
					"separator must be escaped, not persisted")
			}
		})
	}
}

// NUL would make many tools treat the log as binary and truncate display.
func TestServeClientLog_EscapesNul(t *testing.T) {
	logDir := setupClientLogTest(t)

	body := map[string]any{"entries": []ClientLogEntry{{
		Level: "I", Tag: "T", Msg: "a\x00b", Ts: 1700000000000,
	}}}
	req := newRequest(t, http.MethodPost, "/api/client-log", body)
	w := callHandler(ServeClientLog, req)
	require.Equal(t, http.StatusOK, w.Code)

	content := readClientLog(t, logDir)
	assert.NotContains(t, content, "\x00")
	assert.Contains(t, content, `\0`)
}

// The per-field cap must actually be applied by the handler, so one field
// cannot blow past the file cap.
func TestServeClientLog_TruncatesOversizedFields(t *testing.T) {
	logDir := setupClientLogTest(t)

	body := map[string]any{"entries": []ClientLogEntry{{
		Level: "I", Tag: "T", Msg: strings.Repeat("x", clientLogMaxMsgLen*4), Ts: 1700000000000,
	}}}
	req := newRequest(t, http.MethodPost, "/api/client-log", body)
	w := callHandler(ServeClientLog, req)
	require.Equal(t, http.StatusOK, w.Code)

	content := readClientLog(t, logDir)
	assert.Contains(t, content, "[truncated]")
	assert.Less(t, len(content), clientLogMaxMsgLen*2,
		"the handler must cap the field, not just the helper")
}

// Truncation must not emit invalid UTF-8.
func TestSanitizeLogField_TruncatesOnRuneBoundary(t *testing.T) {
	got := sanitizeLogField(strings.Repeat("中", 20), 7)
	assert.True(t, utf8.ValidString(got), "truncation must not split a rune")
}

// --- Hardening: request body bound (end-to-end) ---

// A single request must not append more than clientLogMaxBodyBytes. This goes
// through the handler: an earlier version re-implemented the cap inside the
// test, so it kept passing when the handler's cap was deleted.
func TestServeClientLog_BoundsOneRequest(t *testing.T) {
	logDir := setupClientLogTest(t)

	// Each entry is capped at clientLogMaxMsgLen, so a full 200-entry batch
	// would be ~800 KiB — larger than the MaxBytesReader limit, which would
	// reject the whole request before the append cap ever applies. Size the
	// batch to sit inside the body limit so this exercises the append cap.
	perEntry := clientLogMaxBodyBytes*2/200 - 64
	entries := make([]ClientLogEntry, 0, 200)
	for range 200 {
		entries = append(entries, ClientLogEntry{
			Level: "I", Tag: "T",
			Msg: strings.Repeat("x", perEntry),
			Ts:  1700000000000,
		})
	}

	req := newRequest(t, http.MethodPost, "/api/client-log", map[string]any{"entries": entries})
	w := callHandler(ServeClientLog, req)
	require.Equal(t, http.StatusOK, w.Code)

	content := readClientLog(t, logDir)
	assert.LessOrEqual(t, len(content), clientLogMaxBodyBytes,
		"one request must not exceed the body cap")
	assert.Greater(t, len(content), 0, "the cap must still admit some entries")
}

// The endpoint is an append-only write into a server file, so an anonymous
// caller must not be able to reach it. Without this, anyone who can connect
// could forge log lines or rotate the file to destroy the previous generation.
func TestServeClientLog_RequiresAuth(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	model.SessionToken = hashPassword("testpass")
	model.CookieToken = "instance-key"

	origLogDir := model.ConfigInstance.LogDir
	defer func() { model.ConfigInstance.LogDir = origLogDir }()
	model.ConfigInstance.LogDir = t.TempDir()

	body := map[string]any{"entries": []ClientLogEntry{
		{Level: "I", Tag: "T", Msg: "anonymous write", Ts: 1700000000000},
	}}

	t.Run("no credential is rejected", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/client-log", body)
		req.RemoteAddr = "203.0.113.9:12345" // remote, unauthenticated
		w := callHandlerWithAuth(ServeClientLog, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("valid session cookie is accepted", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/client-log", body)
		req.RemoteAddr = "203.0.113.9:12345"
		withAuthCookie(req, "instance-key")
		w := callHandlerWithAuth(ServeClientLog, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// The per-request byte cap must apply BEFORE json.Decode, not just to the
// rendered lines: otherwise one request can make the server allocate hundreds
// of MiB building the entry slice.
func TestServeClientLog_RejectsOversizedBodyBeforeDecode(t *testing.T) {
	logDir := setupClientLogTest(t)

	// Well past the MaxBytesReader limit (2 × clientLogMaxBodyBytes).
	huge := strings.Repeat("x", clientLogMaxBodyBytes*3)
	body := map[string]any{"entries": []ClientLogEntry{
		{Level: "I", Tag: "T", Msg: huge, Ts: 1700000000000},
	}}

	req := newRequest(t, http.MethodPost, "/api/client-log", body)
	w := callHandler(ServeClientLog, req)

	assert.NotEqual(t, http.StatusOK, w.Code,
		"an oversized body must not be accepted")

	// Nothing should have been appended.
	if _, err := os.Stat(filepath.Join(logDir, "client.log")); err == nil {
		content, readErr := os.ReadFile(filepath.Join(logDir, "client.log"))
		require.NoError(t, readErr)
		assert.Empty(t, string(content), "a rejected request must write nothing")
	}
}

// A body just under the limit must still be accepted (the bound must not
// reject legitimate traffic).
func TestServeClientLog_AcceptsBodyUnderLimit(t *testing.T) {
	logDir := setupClientLogTest(t)

	body := map[string]any{"entries": []ClientLogEntry{
		{Level: "I", Tag: "T", Msg: strings.Repeat("x", 1024), Ts: 1700000000000},
	}}
	req := newRequest(t, http.MethodPost, "/api/client-log", body)
	w := callHandler(ServeClientLog, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, readClientLog(t, logDir))
}
