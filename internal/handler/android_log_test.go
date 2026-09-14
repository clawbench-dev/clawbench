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

// --- Hardening: field sanitization ---

// Every field lands in the log line, so a newline in ANY of them forges a
// record. Escaping only Msg (the previous behavior) left Tag/Level/Source free
// to inject whole lines.
func TestSanitizeLogField_EscapesLineBreaksInEveryField(t *testing.T) {
	forged := "Legit\n2026-01-01T00:00:00.000 [js] I/Real: FORGED"

	for _, field := range []string{"Msg", "Tag", "Level", "Source"} {
		t.Run(field, func(t *testing.T) {
			got := sanitizeLogField(forged, 4096)
			assert.NotContains(t, got, "\n", "%s must not carry a raw newline", field)
			assert.NotContains(t, got, "\r")
			// Escaped rather than dropped, so the log still shows a break existed.
			assert.Contains(t, got, "\\n")
			assert.Contains(t, got, "FORGED")
		})
	}
}

func TestSanitizeLogField_EscapesNul(t *testing.T) {
	// A NUL makes many tools treat the log as binary and truncate display.
	got := sanitizeLogField("a\x00b", 100)
	assert.NotContains(t, got, "\x00")
	assert.Contains(t, got, "\\0")
}

func TestSanitizeLogField_Truncates(t *testing.T) {
	got := sanitizeLogField(strings.Repeat("x", 100), 10)
	assert.Contains(t, got, "[truncated]")
	assert.Less(t, len(got), 100)
}

func TestSanitizeLogField_TruncatesOnRuneBoundary(t *testing.T) {
	// Cutting mid-rune would emit invalid UTF-8; the result must stay valid.
	got := sanitizeLogField(strings.Repeat("中", 20), 7)
	assert.True(t, utf8.ValidString(got), "truncation must not split a rune")
}

// The end-to-end guarantee: a hostile entry cannot produce more than one line.
func TestClientLogEntryLine_HostileFieldsProduceOneLine(t *testing.T) {
	hostile := "x\n2026-01-01T00:00:00.000 [js] I/Fake: INJECTED"
	e := ClientLogEntry{
		Level:  sanitizeLogField(hostile, clientLogMaxLevelLen),
		Tag:    sanitizeLogField(hostile, clientLogMaxTagLen),
		Msg:    sanitizeLogField(hostile, clientLogMaxMsgLen),
		Source: sanitizeLogField(hostile, clientLogMaxLevelLen),
		Ts:     1700000000000,
	}
	line := clientLogEntryLine(e)
	assert.Equal(t, 1, strings.Count(line, "\n"),
		"exactly one terminator: the entry must not split into extra records")
	assert.True(t, strings.HasSuffix(line, "\n"))
}

// --- Hardening: request body bound ---

// A single request must not be able to append more than clientLogMaxBodyBytes,
// regardless of how the entries are distributed.
func TestClientLogBodyCap_BoundsOneRequest(t *testing.T) {
	entries := make([]ClientLogEntry, 0, 200)
	for range 200 {
		entries = append(entries, ClientLogEntry{
			Level: "I", Tag: "T", Msg: strings.Repeat("x", clientLogMaxMsgLen), Ts: 1700000000000,
		})
	}

	var total int
	for _, e := range entries {
		e.Msg = sanitizeLogField(e.Msg, clientLogMaxMsgLen)
		e.Tag = sanitizeLogField(e.Tag, clientLogMaxTagLen)
		e.Level = sanitizeLogField(e.Level, clientLogMaxLevelLen)
		e.Source = sanitizeLogField(e.Source, clientLogMaxLevelLen)
		line := clientLogEntryLine(e)
		if total+len(line) > clientLogMaxBodyBytes {
			break
		}
		total += len(line)
	}

	assert.LessOrEqual(t, total, clientLogMaxBodyBytes)
	assert.Greater(t, total, 0, "the cap must still admit some entries")
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
