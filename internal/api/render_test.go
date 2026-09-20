package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderCommand_CoversExpectedOperations pins the exact operation set each
// built-in command exposes. A change here means the AI's visible API surface
// changed, which must be a deliberate decision.
func TestRenderCommand_CoversExpectedOperations(t *testing.T) {
	tests := []struct {
		cmd     Command
		want    []string // substrings that must appear
		notWant []string // substrings that must NOT appear
	}{
		{
			cmd: CommandChatSearch,
			want: []string{
				// The project list is how the AI finds the exact path of a
				// project the user names (including deleted ones).
				"GET /api/conversation-projects",
				"POST /api/rag/search",
				"GET /api/rag/message",
				"GET /api/rag/session",
				"POST /api/rag/session-search",
			},
			// Index maintenance and summarization are destructive/expensive and
			// must never be triggered by a search command.
			notWant: []string{
				"reset-vector",
				"rebuild-fts",
				"message/summarize",
				"message-index-status",
				"rag/status",
			},
		},
		{
			cmd: CommandTask,
			want: []string{
				"GET /api/tasks",
				"POST /api/tasks",
				"PUT /api/tasks/{id}",
				"DELETE /api/tasks/{id}",
				"GET /api/agents",
				// An event task only fires for a bound repository, so the AI
				// needs to be able to check the binding before creating one.
				"GET /api/forge/binding",
			},
			// Agent mutation must never be exposed to a task command.
			notWant: []string{
				"DELETE /api/agents",
				"PATCH /api/agents",
				"POST /api/agents",
				// Binding mutation would let the task command rebind the
				// project; only the read side is in scope.
				"POST /api/forge/binding",
				"DELETE /api/forge/binding",
			},
		},
		{
			cmd: CommandUsage,
			want: []string{
				// The project list lets the AI resolve a project the user names
				// to the exact path the usage endpoint expects.
				"GET /api/conversation-projects",
				"GET /api/usage/stats",
			},
			// A read-only statistics command must never expose mutating or
			// unrelated endpoints that share the System tag.
			notWant: []string{
				"POST /api/config",
				"POST /api/upgrade",
				"DELETE /api/tasks",
				"POST /api/ai/chat",
			},
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.cmd), func(t *testing.T) {
			out, err := RenderCommand(tc.cmd)
			require.NoError(t, err)
			require.NotEmpty(t, out)

			for _, w := range tc.want {
				assert.Containsf(t, out, w, "command %q must expose %q", tc.cmd, w)
			}
			for _, nw := range tc.notWant {
				assert.NotContainsf(t, out, nw, "command %q must NOT expose %q", tc.cmd, nw)
			}
		})
	}
}

// TestRenderCommand_UnknownCommand guards the error path.
func TestRenderCommand_UnknownCommand(t *testing.T) {
	_, err := RenderCommand(Command("nope"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// TestRenderCommand_Deterministic asserts stable output: prompts are built on
// every request, so nondeterminism would defeat prompt caching and make tests
// flaky.
func TestRenderCommand_Deterministic(t *testing.T) {
	for _, cmd := range []Command{CommandChatSearch, CommandTask, CommandUsage} {
		first, err := RenderCommand(cmd)
		require.NoError(t, err)
		for range 5 {
			again, err := RenderCommand(cmd)
			require.NoError(t, err)
			assert.Equalf(t, first, again, "command %q rendered differently on repeat", cmd)
		}
	}
}

// TestRenderCommand_SizeBudget keeps the injected fragments from creeping back
// toward the size of the full spec. The budget is generous (roughly 2x current)
// so ordinary spec growth does not break the build, but a switch back to
// tag-based selection would blow past it immediately.
func TestRenderCommand_SizeBudget(t *testing.T) {
	budgets := map[Command]int{
		// chatsearch grew when the project-list endpoint joined it, so the AI
		// can resolve a project the user names to its exact path. The budget
		// still sits far below the full spec, so a switch back to tag-based
		// selection would blow past it.
		CommandChatSearch: 2600,
		CommandTask:       7000,
		CommandUsage:      2000,
	}
	for cmd, budget := range budgets {
		out, err := RenderCommand(cmd)
		require.NoError(t, err)
		assert.Lessf(t, len(out), budget,
			"command %q renders %d bytes, over the %d budget — check the operation selection",
			cmd, len(out), budget)
	}
}

// TestRenderCommand_FieldsMatchSpec checks that request-body fields are carried
// through verbatim, including the action enumeration note that tells the AI how
// to drive the multi-action task update endpoint.
func TestRenderCommand_FieldsMatchSpec(t *testing.T) {
	out, err := RenderCommand(CommandTask)
	require.NoError(t, err)

	assert.Contains(t, out, "action", "taskUpdate must advertise its action field")
	assert.Contains(t, out, "executionId", "taskUpdate must advertise executionId")
	assert.Contains(t, out, "deleteAllExecutions", "the action enumeration must reach the prompt")
	assert.Contains(t, out, "path: id", "path parameters must be rendered")
}

// TestRenderCommand_RendersEnumsInline asserts that permitted values reach the
// prompt. Without this the AI sees only "dims:array" and has to guess — the
// exact failure mode that had it sending dims=project, which the backend
// rejected with invalid_dim before project became a supported dimension.
func TestRenderCommand_RendersEnumsInline(t *testing.T) {
	out, err := RenderCommand(CommandUsage)
	require.NoError(t, err)

	// The full enum must be listed, project included: a prefix match would let
	// a dropped project dimension slip through.
	assert.Contains(t, out, "dims:model|backend|agent|project",
		"dims must list every permitted value, project included")
	assert.Contains(t, out, "metrics:input|output|total|cacheHit|credit|cost",
		"metrics must list its permitted values")
	assert.Contains(t, out, "order:asc|desc",
		"order must list its permitted values")
	// scope is how the AI asks for cross-project aggregation, so its permitted
	// values have to be visible too.
	assert.Contains(t, out, "scope:project|all",
		"scope must list its permitted values")
}

// TestRenderCommand_BodyFieldsCarryEnumsAndDescriptions guards the request-body
// side of the same contract. Query parameters were covered; body fields were
// rendered with a nil schema, so an enum like repeat_mode lost its permitted
// values and every field note was dropped — leaving the AI to guess at values
// the server would reject.
func TestRenderCommand_BodyFieldsCarryEnumsAndDescriptions(t *testing.T) {
	out, err := RenderCommand(CommandTask)
	require.NoError(t, err)

	// Enum values are inlined on the field list.
	assert.Contains(t, out, "repeat_mode:once|limited|unlimited",
		"body enums must list their permitted values")
	assert.Contains(t, out, "trigger_mode:cron|event",
		"body enums must list their permitted values")

	// Field notes are rendered, since a body field's constraints live only
	// there (there is no name to infer them from).
	assert.Contains(t, out, "field max_runs:",
		"body field descriptions must reach the prompt")
	assert.Contains(t, out, "repeat_mode=limited",
		"the conditional-required rule must survive into the prompt")

	// Non-string scalar types are still annotated.
	assert.Contains(t, out, "max_runs:integer")
}

// TestRenderCommand_TaskDocumentsEventSemantics pins the event-task caveats
// that are not derivable from the endpoint shapes: an event task ignores
// repeat_mode/max_runs, needs a bound repository, and is suppressed when the
// credential's own account authored the action. Each was a silent trap when
// only the request schema was visible.
func TestRenderCommand_TaskDocumentsEventSemantics(t *testing.T) {
	out, err := RenderCommand(CommandTask)
	require.NoError(t, err)

	assert.Contains(t, out, "`trigger_mode=event` 时忽略此字段",
		"repeat_mode must state it is ignored for event tasks")
	assert.Contains(t, out, "已绑定仓库",
		"the repository-binding precondition must be documented")
	assert.Contains(t, out, "凭据账号自身",
		"the anti-recursion suppression must be documented")
	assert.Contains(t, out, "本地时区",
		"the cron timezone must be documented")
}

// TestRenderCommand_MarksRepeatableArrayParams guards a real failure seen in
// end-to-end testing: dims/metrics are array-typed query params, which OpenAPI
// encodes as "repeat this parameter". Rendering the enum inline without saying
// so made the AI send "metrics=input,output,total" and get invalid_metric.
func TestRenderCommand_MarksRepeatableArrayParams(t *testing.T) {
	out, err := RenderCommand(CommandUsage)
	require.NoError(t, err)

	assert.Contains(t, out, "repeat:",
		"array-valued params must carry an explicit repeat hint")
	assert.Contains(t, out, "dims=model&dims=backend",
		"the hint must show the correct wire form")
	assert.Contains(t, out, "comma-separated values are rejected")
}

// TestRenderCommand_NoRepeatHintWithoutArrayParams asserts the hint is not
// emitted for endpoints that have no array parameter, keeping prompts compact.
func TestRenderCommand_NoRepeatHintWithoutArrayParams(t *testing.T) {
	// /cb-task's rendered endpoints (tasks, agents) use scalar params only.
	out, err := RenderCommand(CommandTask)
	require.NoError(t, err)
	assert.NotContains(t, out, "repeat:",
		"no array params means no repeat hint")
}

// TestOperationIDs_Resolvable asserts every operationId a command declares
// actually exists in the spec. This is the drift guard's first half: renaming
// an operation without updating commandOperations fails here.
func TestOperationIDs_Resolvable(t *testing.T) {
	for _, cmd := range []Command{CommandChatSearch, CommandTask, CommandUsage} {
		ids := OperationIDs(cmd)
		require.NotEmptyf(t, ids, "command %q declares no operations", cmd)
		for _, id := range ids {
			assert.Truef(t, operationExists(id), "command %q references unknown operationId %q", cmd, id)
		}
	}
}

// operationExists reports whether the embedded spec defines the operationId.
func operationExists(id string) bool {
	s, err := load()
	if err != nil {
		return false
	}
	for _, item := range s.Paths {
		for _, method := range methodOrder {
			if op := item.method(method); op != nil && op.OperationID == id {
				return true
			}
		}
	}
	return false
}

// TestSpecHasNoDuplicateOperationIDs guards the selector: duplicate ids would
// make operationId-based selection ambiguous.
func TestSpecHasNoDuplicateOperationIDs(t *testing.T) {
	s, err := load()
	require.NoError(t, err)

	seen := map[string]string{}
	for path, item := range s.Paths {
		for _, method := range methodOrder {
			op := item.method(method)
			if op == nil || op.OperationID == "" {
				continue
			}
			where := strings.ToUpper(method) + " " + path
			if prev, dup := seen[op.OperationID]; dup {
				t.Errorf("duplicate operationId %q: %s and %s", op.OperationID, prev, where)
			}
			seen[op.OperationID] = where
		}
	}
}
