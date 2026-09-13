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
			},
			// Agent mutation must never be exposed to a task command.
			notWant: []string{
				"DELETE /api/agents",
				"PATCH /api/agents",
				"POST /api/agents",
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
	for _, cmd := range []Command{CommandChatSearch, CommandTask} {
		first, err := RenderCommand(cmd)
		require.NoError(t, err)
		for i := 0; i < 5; i++ {
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
		CommandChatSearch: 2000,
		CommandTask:       4000,
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

// TestOperationIDs_Resolvable asserts every operationId a command declares
// actually exists in the spec. This is the drift guard's first half: renaming
// an operation without updating commandOperations fails here.
func TestOperationIDs_Resolvable(t *testing.T) {
	for _, cmd := range []Command{CommandChatSearch, CommandTask} {
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
