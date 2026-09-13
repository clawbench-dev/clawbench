package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withServerPort pins model.ServerPort for the duration of a test so the
// rendered base URL is deterministic.
func withServerPort(t *testing.T, port int) {
	t.Helper()
	orig := model.ServerPort
	model.ServerPort = port
	t.Cleanup(func() { model.ServerPort = orig })
}

// --- processClawbenchCommand tests ---

func TestProcessClawbenchCommand_ChatSearchInjects(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-chatsearch fix login bug", "/project", "session-123")
	require.NoError(t, err)

	// The endpoint reference is rendered from the embedded spec.
	assert.Contains(t, result, "historical conversation search")
	assert.Contains(t, result, "POST /api/rag/search")
	assert.Contains(t, result, "GET /api/rag/message")
	assert.Contains(t, result, "http://localhost:20000")
	// Project scoping and current-session exclusion are carried by the template.
	assert.Contains(t, result, "clawbench_project=/project")
	assert.Contains(t, result, "session-123")
	// Returns ONLY the template; the caller prepends it to a prompt that already
	// contains the user's original message.
	assert.NotContains(t, result, "/cb-chatsearch fix login bug")
}

func TestProcessClawbenchCommand_TaskInjects(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-task daily build", "/project", "session-456")
	require.NoError(t, err)

	assert.Contains(t, result, "task management")
	assert.Contains(t, result, "POST /api/tasks")
	assert.Contains(t, result, "GET /api/agents")
	assert.Contains(t, result, "clawbench_project=/project")
	assert.NotContains(t, result, "/cb-task daily build")
}

// TestProcessClawbenchCommand_NoCliReferences guards the core of the refactor:
// the injected instructions must never tell the AI to shell out to the
// clawbench binary again.
func TestProcessClawbenchCommand_NoCliReferences(t *testing.T) {
	withServerPort(t, 20000)

	for _, msg := range []string{"/cb-chatsearch auth bug", "/cb-task daily build", "/cb-usage last week"} {
		result, err := processClawbenchCommand(msg, "/project", "sess-1")
		require.NoError(t, err)
		assert.NotContainsf(t, result, "clawbench rag", "%s must not reference the rag CLI", msg)
		assert.NotContainsf(t, result, "clawbench task", "%s must not reference the task CLI", msg)
		assert.NotContainsf(t, result, "--data-dir", "%s must not reference CLI flags", msg)
		assert.NotContainsf(t, result, "--exclude-session-id", "%s must not reference CLI flags", msg)
	}
}

func TestProcessClawbenchCommand_NoPrefixPassesThrough(t *testing.T) {
	result, err := processClawbenchCommand("hello world", "/project", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestProcessClawbenchCommand_EmptyQueryReturnsRaw(t *testing.T) {
	// /cb-chatsearch with only whitespace after it is not a template case —
	// the caller emits SearchQueryRequired. (The handler rejects it before
	// reaching the renderer, so this only pins the pass-through.)
	result, err := processClawbenchCommand("/cb-chatsearch  ", "/project", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "/cb-chatsearch  ", result)
}

func TestProcessClawbenchCommand_TaskEmptyDescReturnsInjected(t *testing.T) {
	withServerPort(t, 20000)

	// /cb-task with just a space still injects — the task description can be
	// short or absent.
	result, err := processClawbenchCommand("/cb-task ", "/project", "session-123")
	require.NoError(t, err)
	assert.Contains(t, result, "task management")
}

func TestProcessClawbenchCommand_PartialPrefixNoMatch(t *testing.T) {
	result, err := processClawbenchCommand("/cb-chat something", "/project", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "/cb-chat something", result)
}

func TestProcessClawbenchCommand_BareChatSearchReturnsRaw(t *testing.T) {
	// A bare /cb-chatsearch (no trailing space) — exactly what the frontend
	// sends after trimming a menu selection — has an empty query and must
	// return the raw message so the caller can emit SearchQueryRequired.
	result, err := processClawbenchCommand("/cb-chatsearch", "/project", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "/cb-chatsearch", result)
}

func TestProcessClawbenchCommand_BareTaskInjects(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-task", "/project", "session-123")
	require.NoError(t, err)
	assert.Contains(t, result, "task management")
	assert.NotContains(t, result, "/cb-task")
}

func TestProcessClawbenchCommand_AgentSlashCommandPassesThrough(t *testing.T) {
	// A regular agent slash command (no cb- namespace) must pass through
	// untouched — it is forwarded to the agent, not injected locally.
	result, err := processClawbenchCommand("/compact", "/project", "session-123")
	require.NoError(t, err)
	assert.Equal(t, "/compact", result)
}

// --- IsClawbenchCommand tests ---

func TestIsClawbenchCommand(t *testing.T) {
	assert.True(t, IsClawbenchCommand("/cb-chatsearch query"))
	assert.True(t, IsClawbenchCommand("/cb-task do thing"))
	// Bare commands (no trailing space) are what the frontend sends after
	// trimming — they must still be recognized so they are not misrouted to
	// the ACP slash-command path (regression).
	assert.True(t, IsClawbenchCommand("/cb-chatsearch"))
	assert.True(t, IsClawbenchCommand("/cb-task"))
	assert.False(t, IsClawbenchCommand("/compact"))
	assert.False(t, IsClawbenchCommand("/cb-chatsearchx query")) // no space after command
	assert.False(t, IsClawbenchCommand("hello"))
	assert.False(t, IsClawbenchCommand(""))
}

// TestProcessClawbenchCommand_PlaceholderReplacement asserts every placeholder
// is substituted: a leftover "{{...}}" would reach the model verbatim.
func TestProcessClawbenchCommand_PlaceholderReplacement(t *testing.T) {
	withServerPort(t, 20000)

	tests := []struct {
		msg string
	}{
		{"/cb-chatsearch auth bug"},
		{"/cb-task daily report"},
	}
	for _, tc := range tests {
		result, err := processClawbenchCommand(tc.msg, "/my/project", "sess-abc")
		require.NoError(t, err)
		assert.NotContainsf(t, result, "{{", "%s left an unreplaced placeholder", tc.msg)
		assert.Containsf(t, result, "/my/project", "%s must carry the project path", tc.msg)
	}
}

// TestProcessClawbenchCommand_BaseURLTracksPort verifies the injected base URL
// follows the configured port: the AI runs as a child process and has no other
// way to discover where the server listens.
func TestProcessClawbenchCommand_BaseURLTracksPort(t *testing.T) {
	for _, port := range []int{20000, 8080, 3000} {
		withServerPort(t, port)
		result, err := processClawbenchCommand("/cb-task daily build", "/project", "sess-1")
		require.NoError(t, err)
		assert.Containsf(t, result, "http://localhost:"+itoa(port),
			"base URL must reflect port %d", port)
	}
}

// TestProcessClawbenchCommand_BaseURLDefaultPort covers the unset-port fallback.
func TestProcessClawbenchCommand_BaseURLDefaultPort(t *testing.T) {
	withServerPort(t, 0)
	result, err := processClawbenchCommand("/cb-task test", "/project", "sess-1")
	require.NoError(t, err)
	assert.Contains(t, result, "http://localhost:20000")
}

func TestProcessClawbenchCommand_ChatSearchContainsNaturalFormat(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-chatsearch test", "/project", "session-123")
	require.NoError(t, err)

	// Must instruct AI to present results naturally (no structured XML card format)
	assert.Contains(t, result, "natural, readable format")
	assert.NotContains(t, result, "<rag-results>")
	assert.NotContains(t, result, "<rag-item>")
}

// TestProcessClawbenchCommand_TaskKeepsBehaviourRules pins the hand-written
// rules that are not derivable from the spec.
func TestProcessClawbenchCommand_TaskKeepsBehaviourRules(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-task test task", "/project", "session-123")
	require.NoError(t, err)

	assert.Contains(t, result, "<scheduled-task", "the completion marker must survive")
	// The cron rule must not promise a validation step that does not exist:
	// there is no validation endpoint, so the create call is the only check.
	assert.Contains(t, result, "no validation endpoint")
	assert.Contains(t, result, "HTTP 400")
	assert.NotContains(t, result, "Always validate cron expression",
		"the rule must not instruct the AI to validate via a non-existent endpoint")
	assert.Contains(t, result, "high frequency")
	assert.Contains(t, result, "user's language")
}

// TestProcessClawbenchCommand_TaskWarnsEventPreconditions asserts the injected
// prompt tells the AI the two things that make an event task silently dead:
// an unbound repository, and repeat_mode not constraining an event task.
func TestProcessClawbenchCommand_TaskWarnsEventPreconditions(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-task review new PRs", "/project", "sess-1")
	require.NoError(t, err)

	assert.Contains(t, result, "GET /api/forge/binding",
		"the binding check endpoint must be advertised")
	assert.Contains(t, result, "never run",
		"the AI must be told an unbound project yields a dead task")
	assert.Contains(t, result, "repeat_mode",
		"the repeat_mode caveat must reach the prompt")
}

// TestProcessClawbenchCommand_TaskExcludesDestructiveAgentOps asserts the task
// command never advertises agent mutation. Tag-based selection would have
// leaked DELETE/PATCH/POST /api/agents into this prompt.
func TestProcessClawbenchCommand_TaskExcludesDestructiveAgentOps(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-task something", "/project", "sess-1")
	require.NoError(t, err)

	assert.Contains(t, result, "GET /api/agents")
	assert.NotContains(t, result, "DELETE /api/agents")
	assert.NotContains(t, result, "PATCH /api/agents")
	assert.NotContains(t, result, "POST /api/agents")
}

// TestProcessClawbenchCommand_ChatSearchExcludesIndexMaintenance asserts the
// search command never advertises index rebuild or summarization.
func TestProcessClawbenchCommand_ChatSearchExcludesIndexMaintenance(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-chatsearch auth bug", "/project", "sess-1")
	require.NoError(t, err)

	assert.NotContains(t, result, "reset-vector")
	assert.NotContains(t, result, "rebuild-fts")
	assert.NotContains(t, result, "message/summarize")
}

// TestProcessClawbenchCommand_NoMessageDuplication verifies the fix for ISS-287:
// processClawbenchCommand must return ONLY the template without appending the
// original message, because the caller already prepends the result to a
// prompt that contains the user's original message.
func TestProcessClawbenchCommand_NoMessageDuplication(t *testing.T) {
	withServerPort(t, 20000)

	userMsg := "/cb-chatsearch how to fix auth"
	prompt := userMsg
	injected, err := processClawbenchCommand(userMsg, "/project", "sess-1")
	require.NoError(t, err)
	prompt = injected + "\n\n" + prompt
	assert.Equal(t, 1, strings.Count(prompt, "/cb-chatsearch how to fix auth"),
		"user message should appear exactly once in the final prompt (ISS-287)")

	userMsgTask := "/cb-task daily build"
	prompt = userMsgTask
	injected, err = processClawbenchCommand(userMsgTask, "/project", "sess-1")
	require.NoError(t, err)
	prompt = injected + "\n\n" + prompt
	assert.Equal(t, 1, strings.Count(prompt, "/cb-task daily build"),
		"user message should appear exactly once in the final prompt for /cb-task (ISS-287)")
}

// --- /cb-usage ---

func TestProcessClawbenchCommand_UsageInjects(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-usage 最近一周哪个模型最贵", "/project", "sess-1")
	require.NoError(t, err)

	assert.Contains(t, result, "token usage statistics")
	assert.Contains(t, result, "GET /api/usage/stats")
	assert.Contains(t, result, "clawbench_project=/project")
	// Returns only the template; the caller prepends the original message.
	assert.NotContains(t, result, "/cb-usage 最近一周哪个模型最贵")
}

// TestProcessClawbenchCommand_UsageInjectsCurrentTime asserts the prompt
// carries "now". The AI runs as a subprocess with no reliable clock, so
// without this it would have to guess the date when building start/end.
func TestProcessClawbenchCommand_UsageInjectsCurrentTime(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-usage this month", "/project", "sess-1")
	require.NoError(t, err)

	now := clawbenchNow()
	assert.Contains(t, result, "Current time (UTC): "+now)
	// Must be an RFC3339 UTC instant the AI can reuse verbatim.
	_, parseErr := time.Parse(time.RFC3339, now)
	assert.NoError(t, parseErr, "injected time must be RFC3339")
	assert.True(t, strings.HasSuffix(now, "Z"), "injected time must be UTC")
}

// TestProcessClawbenchCommand_UsageRendersEnums asserts the permitted values
// reach the prompt. dims is the parameter the AI most easily gets wrong.
func TestProcessClawbenchCommand_UsageRendersEnums(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-usage usage", "/project", "sess-1")
	require.NoError(t, err)

	assert.Contains(t, result, "dims:model|backend|agent")
	assert.Contains(t, result, "metrics:input|output|total|cacheHit|credit|cost")
	assert.NotContains(t, result, "dims:project",
		"the unsupported project dimension must not be advertised")
}

// TestProcessClawbenchCommand_UsageExcludesUnrelatedSystemOps asserts the
// usage command exposes only the statistics endpoint. System is a broad tag,
// so a regression to tag-based selection would leak unrelated operations.
func TestProcessClawbenchCommand_UsageExcludesUnrelatedSystemOps(t *testing.T) {
	withServerPort(t, 20000)

	result, err := processClawbenchCommand("/cb-usage totals", "/project", "sess-1")
	require.NoError(t, err)

	assert.NotContains(t, result, "POST /api/config")
	assert.NotContains(t, result, "POST /api/upgrade")
	assert.NotContains(t, result, "GET /api/health")
}

// TestProcessClawbenchCommand_UsageIsRecognizedAsBuiltin guards the routing:
// IsClawbenchCommand must claim "/cb-usage" or it is forwarded to the agent as
// an ACP slash command and the injection never happens.
func TestProcessClawbenchCommand_UsageIsRecognizedAsBuiltin(t *testing.T) {
	assert.True(t, IsClawbenchCommand("/cb-usage"))
	assert.True(t, IsClawbenchCommand("/cb-usage last week"))
	assert.False(t, IsClawbenchCommand("/cb-usagex"))
}

// TestProcessClawbenchCommand_ProjectCookieIsPortScoped guards a real bug: on a
// non-default port the server only accepts the scoped cookie name
// ("cb21999_clawbench_project"), so telling the AI to send the bare name made
// every call 403. The default port keeps the bare name, which is why this only
// shows up on multi-instance / custom-port setups.
func TestProcessClawbenchCommand_ProjectCookieIsPortScoped(t *testing.T) {
	t.Run("default port uses bare name", func(t *testing.T) {
		withServerPort(t, 20000)
		for _, msg := range []string{"/cb-chatsearch auth bug", "/cb-task daily", "/cb-usage last week"} {
			result, err := processClawbenchCommand(msg, "/project", "sess-1")
			require.NoError(t, err)
			assert.Containsf(t, result, "clawbench_project=/project",
				"%s must name the bare cookie on the default port", msg)
		}
	})

	t.Run("custom port uses scoped name", func(t *testing.T) {
		withServerPort(t, 21999)
		for _, msg := range []string{"/cb-chatsearch auth bug", "/cb-task daily", "/cb-usage last week"} {
			result, err := processClawbenchCommand(msg, "/project", "sess-1")
			require.NoError(t, err)
			assert.Containsf(t, result, "cb21999_clawbench_project=/project",
				"%s must name the port-scoped cookie", msg)
			// The bare name must not appear: it would 403 on this port.
			assert.NotContainsf(t, result, `"clawbench_project=/project"`,
				"%s must not advertise the bare cookie name on a custom port", msg)
		}
	})
}

// --- clawbenchCommandPrecheck ---

// TestClawbenchCommandPrecheck covers the per-command precondition that the
// converged dispatch relies on. It returns false only when it has written an
// error response, so the caller must stop.
//
// RAG readiness is process-global and false in this test binary, so the
// chatsearch cases assert the *ordering* of the two checks rather than a fixed
// status: with the store nil, RAGNotReady (503) must win over the empty-query
// check (400), and both must reject.
func TestClawbenchCommandPrecheck(t *testing.T) {
	ragReady := rag.GlobalStore != nil

	t.Run("chatsearch rejects a bare command", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/ai/chat", nil)
		assert.False(t, clawbenchCommandPrecheck(w, r, "/cb-chatsearch"),
			"a bare /cb-chatsearch has nothing to search for")
		if ragReady {
			assert.Equal(t, http.StatusBadRequest, w.Code)
		} else {
			assert.Equal(t, http.StatusServiceUnavailable, w.Code,
				"RAG-not-ready must be reported before the empty-query check")
		}
	})

	t.Run("chatsearch accepts a query when RAG is ready", func(t *testing.T) {
		if !ragReady {
			t.Skip("RAG store not initialized in this test binary")
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/ai/chat", nil)
		assert.True(t, clawbenchCommandPrecheck(w, r, "/cb-chatsearch auth bug"))
		assert.Equal(t, http.StatusOK, w.Code, "no error response should be written")
	})

	t.Run("commands without a precondition pass through", func(t *testing.T) {
		for _, msg := range []string{"/cb-task daily", "/cb-usage totals", "/compact"} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/ai/chat", nil)
			assert.Truef(t, clawbenchCommandPrecheck(w, r, msg),
				"%s has no precondition and must pass", msg)
			assert.Equalf(t, http.StatusOK, w.Code, "%s must not write an error", msg)
		}
	})
}

// itoa avoids pulling strconv into the test's import list for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
