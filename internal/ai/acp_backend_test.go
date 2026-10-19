package ai

import (
	"errors"
	"fmt"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// TestShouldNewSessionFallback verifies the rule that a failed session recovery
// only falls back to a brand-new session when the session has NO conversation
// history at all (no user or assistant messages). If the session has any messages,
// rebuilding would cause amnesia, so we must NOT fall back — the error is
// surfaced so the user can retry, preserving the original session mapping.
func TestShouldNewSessionFallback(t *testing.T) {
	t.Run("no conversation yet falls back", func(t *testing.T) {
		assert.True(t, shouldNewSessionFallback(false), "session with no messages may fall back to a new session")
	})

	t.Run("existing conversation does not fall back", func(t *testing.T) {
		assert.False(t, shouldNewSessionFallback(true), "session with conversation history must not be rebuilt (amnesia)")
	})
}

// --- AUTH_REQUIRED translation (JSON-RPC -32000) ---

// Command Code answers session/new with -32000 AUTH_REQUIRED when no account
// credential is present. The raw SDK blob ("acp: session/new:
// {\"code\":-32000,...}") tells the user nothing actionable, so the connection
// error path must recognize the code and emit the login instruction instead.
func TestAsACPAuthRequired(t *testing.T) {
	t.Run("detects -32000 through wrapping", func(t *testing.T) {
		// The connection layer wraps the SDK error several times before it
		// reaches ExecuteStream ("acp: session/new: %v"), so detection must not
		// depend on the error being unwrapped.
		inner := &acp.RequestError{Code: acpAuthRequiredCode, Message: "Not authenticated."}
		wrapped := fmt.Errorf("acp: session/new: %w", fmt.Errorf("outer: %w", inner))

		got := asACPAuthRequired(wrapped)
		assert.NotNil(t, got, "must find -32000 through fmt.Errorf wrapping")
		assert.Equal(t, "Not authenticated.", got.Message)
	})

	t.Run("ignores other codes", func(t *testing.T) {
		for _, code := range []int{-32601, -32602, -32603, -32700, -32800} {
			err := fmt.Errorf("x: %w", &acp.RequestError{Code: code, Message: "m"})
			assert.Nil(t, asACPAuthRequired(err), "code %d is not AUTH_REQUIRED", code)
		}
	})

	t.Run("ignores non-JSONRPC errors", func(t *testing.T) {
		assert.Nil(t, asACPAuthRequired(errors.New("connection refused")))
		assert.Nil(t, asACPAuthRequired(nil))
	})
}

// The message must name the backend's real login command; the agent's own
// wording is only a fallback for backends that do not declare one.
func TestACPAuthRequiredMessage(t *testing.T) {
	t.Run("uses the spec LoginCmd", func(t *testing.T) {
		saved := model.GetBackendRegistry()
		t.Cleanup(func() { model.BackendRegistry = saved })
		model.BackendRegistry = []model.BackendSpec{
			{ID: "cc", Backend: "cc", LoginCmd: "cmdc login"},
		}

		msg := acpAuthRequiredMessage(&model.Agent{Backend: "cc"},
			&acp.RequestError{Code: acpAuthRequiredCode, Message: "raw"})
		assert.Contains(t, msg, "cmdc login",
			"the message must name the login command the user has to run")
		assert.NotContains(t, msg, "raw",
			"the agent's raw wording is replaced when a LoginCmd is declared")
	})

	t.Run("falls back to the agent message", func(t *testing.T) {
		saved := model.GetBackendRegistry()
		t.Cleanup(func() { model.BackendRegistry = saved })
		model.BackendRegistry = []model.BackendSpec{
			{ID: "nol", Backend: "nol"}, // no LoginCmd
		}

		msg := acpAuthRequiredMessage(&model.Agent{Backend: "nol"},
			&acp.RequestError{Code: acpAuthRequiredCode, Message: "Run `thing login` first."})
		assert.Contains(t, msg, "Run `thing login` first.",
			"without a declared LoginCmd the agent's own instruction must survive")
	})

	t.Run("unknown backend does not panic", func(t *testing.T) {
		msg := acpAuthRequiredMessage(&model.Agent{Backend: "no-such-backend"},
			&acp.RequestError{Code: acpAuthRequiredCode, Message: "msg"})
		assert.Contains(t, msg, "msg")
	})
}

// The command must ALSO travel as a structured field: the client renders a
// "copy the login command" action from it, and scraping it back out of the
// localized message would break on any wording change or non-English locale.
func TestACPLoginCommand(t *testing.T) {
	saved := model.GetBackendRegistry()
	t.Cleanup(func() { model.BackendRegistry = saved })
	model.BackendRegistry = []model.BackendSpec{
		{ID: "cc", Backend: "cc", LoginCmd: "cmdc login"},
		{ID: "nol", Backend: "nol"},
	}

	assert.Equal(t, "cmdc login", acpLoginCommand(&model.Agent{Backend: "cc"}))
	assert.Empty(t, acpLoginCommand(&model.Agent{Backend: "nol"}),
		"a backend without a declared command must not invent one")
	assert.Empty(t, acpLoginCommand(&model.Agent{Backend: "no-such-backend"}))
	assert.Empty(t, acpLoginCommand(nil))
}
