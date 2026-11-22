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

// redactAIArgs mirrors the production logging shape for a whole argument list:
// every element passes through model.RedactAIToken, and flags are preserved.
func redactAIArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = model.RedactAIToken(a)
	}
	return out
}

// The injected AI token must never reach the server log. Every log line that
// carries a prompt (or an argument embedding one) must run it through
// model.RedactAIToken first, or the token lands in {LogDir}/logs/ in plaintext,
// valid from loopback for its whole TTL. This test mirrors that call shape; it
// pins the redaction itself, not the call site.
func TestExecLogDoesNotContainAIToken(t *testing.T) {
	token := model.SignAIToken(time.Now())
	require.NotEmpty(t, token, "need a real token to make this test meaningful")

	// The exact shape injected into the prompt by the slash-command templates.
	argWithToken := "Auth: send the header \"" + model.AITokenHeader + ": " + token + "\" on every request."
	args := []string{"--some-flag", argWithToken}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	// Mirror the production log call, including the redaction.
	slog.Info("executing ai stream command",
		slog.String("prompt", model.RedactAIToken(argWithToken)),
		slog.Any("args", redactAIArgs(args)),
	)

	out := buf.String()
	assert.NotContains(t, out, token,
		"the raw token must never appear in log output")
	assert.Contains(t, out, "[redacted]")
	assert.Contains(t, out, "some-flag",
		"non-secret args must still be logged for debugging")
}
