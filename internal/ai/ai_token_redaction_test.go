package ai

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The injected AI token must never reach the server log. Some backends pass the
// prompt as a command-line argument, and both cli_backend and codex_stream log
// the full args slice — so without redaction the token lands in
// {LogDir}/logs/ in plaintext, valid from loopback for its whole TTL.
func TestExecLogDoesNotContainAIToken(t *testing.T) {
	token := model.SignAIToken(time.Now())
	require.NotEmpty(t, token, "need a real token to make this test meaningful")

	// The exact shape injected into the prompt by the slash-command templates.
	argWithToken := "Auth: send the header \"" + model.AITokenHeader + ": " + token + "\" on every request."
	args := []string{"--some-flag", argWithToken, "positional prompt tail"}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	// Mirror the production log call, including the redaction.
	slog.Info("executing ai stream command",
		slog.String("prompt", model.RedactAIToken(argWithToken)),
		slog.Any("args", redactArgs(args)),
	)

	out := buf.String()
	assert.NotContains(t, out, token,
		"the raw token must never appear in log output")
	assert.Contains(t, out, "[redacted]")
	assert.Contains(t, out, "some-flag",
		"non-secret args must still be logged for debugging")
}

// redactArgs must not drop or reorder arguments, so debugging output stays
// faithful apart from the secret.
func TestRedactArgs_PreservesEverythingElse(t *testing.T) {
	token := model.SignAIToken(time.Now())
	args := []string{"a", model.AITokenHeader + ": " + token, "c"}

	got := redactArgs(args)

	require.Len(t, got, len(args), "arg count must be preserved")
	assert.Equal(t, "a", got[0])
	assert.Equal(t, "c", got[2])
	assert.NotContains(t, got[1], token)
	assert.Contains(t, got[1], "[redacted]")
}

func TestRedactArgs_HandlesNilAndEmpty(t *testing.T) {
	assert.Empty(t, redactArgs(nil))
	assert.Empty(t, redactArgs([]string{}))
}

// A prompt containing no token must pass through byte-for-byte, so redaction
// cannot corrupt ordinary log lines.
func TestRedactArgs_LeavesCleanArgsUntouched(t *testing.T) {
	args := []string{"--model", "mock-pro", "plain prompt with no header"}
	assert.Equal(t, args, redactArgs(args))
}
