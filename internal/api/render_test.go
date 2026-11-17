package api

import (
	"fmt"
	"sort"
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
		{
			cmd: CommandAgent,
			want: []string{
				// The list is both the search/view surface (no dedicated
				// search endpoint exists) and how the AI finds a source to
				// clone — creation is by cloning, not from scratch.
				"GET /api/agents",
				"POST /api/agents",
				"PATCH /api/agents",
				"DELETE /api/agents",
			},
			// Agent management must not drag in unrelated operations that
			// share the Agents tag (model refresh, rescan, backend discovery)
			// or any task endpoint. The self-test routes ARE wanted, so this
			// list names the excluded sub-resources concretely rather than
			// matching the "/api/agents/{id}" prefix.
			notWant: []string{
				"GET /api/agents/{id}",
				"refresh-models",
				"POST /api/agents/rescan",
				"GET /api/backends",
				"POST /api/tasks",
				"DELETE /api/tasks",
			},
		},
		{
			cmd: CommandMCP,
			want: []string{
				// Both resources: the global list and the per-agent policy.
				"GET /api/config",
				"PATCH /api/config",
				"GET /api/agents",
				"PATCH /api/agents",
			},
			// The Config tag also carries passwords, TTS/RAG keys, port
			// forwarding and the desktop-upgrade settings. An MCP command must
			// never be able to reach any of them, and it must not be able to
			// create or delete agents either.
			notWant: []string{
				"POST /api/config",
				"POST /api/config/password",
				"POST /api/config/test",
				"POST /api/agents",
				"DELETE /api/agents",
				"POST /api/agents/rescan",
				"refresh-models",
				"GET /api/backends",
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
	for _, cmd := range []Command{CommandChatSearch, CommandTask, CommandUsage, CommandAgent} {
		first, err := RenderCommand(cmd)
		require.NoError(t, err)
		for range 5 {
			again, err := RenderCommand(cmd)
			require.NoError(t, err)
			assert.Equalf(t, first, again, "command %q rendered differently on repeat", cmd)
		}
	}
}

// TestRenderCommand_SizeBudget caps the injected fragments so a command's
// prompt cannot creep back toward the size of the full spec.
//
// This guards unbounded prose growth, and — as a side effect — also rejects a
// wholesale switch to tag-based selection for most commands, since a whole
// tag's endpoints are far larger than the hand-picked subset (measured at the
// time of writing: Agents tag 6409 > agent budget 4800; RAG 4943 > chatsearch
// 2600; System 3516 > usage 2000). It is NOT the primary defense, though: the
// real guard is the operationId selection itself — endpointsForOperations
// binds each id to its expected tag and fails loudly on a retag — plus each
// command's notWant assertions in CoversExpectedOperations, which pin the
// operations that must stay out regardless of size.
func TestRenderCommand_SizeBudget(t *testing.T) {
	budgets := map[Command]int{
		// chatsearch grew when the project-list endpoint joined it, so the AI
		// can resolve a project the user names to its exact path.
		CommandChatSearch: 2600,
		// The task body grew when the gating-script fields (script,
		// script_timeout) joined it, again when GET /api/agents gained the
		// project-filtering description, and again when it gained the
		// "ACP unconfirmed → empty models" contract note; the headroom is
		// deliberate, not drift.
		//
		// 9700 → 11500 when the mail trigger landed (measured 11298). The
		// operation selection is unchanged — this is prose, not a leaked
		// endpoint. The added bytes are the `trigger_mode=email` contract:
		// which messages count as new, that the three gates are AND-ed
		// substring matches, the per-message dedup, and the 5-per-sync cap.
		// None of that is inferable from the field names, and getting it wrong
		// makes the AI configure a task that either never fires or fires on the
		// whole inbox, so it stays in the spec rather than moving to the prompt
		// template (same bytes, worse locality).
		CommandTask:  11500,
		CommandUsage: 2000,
		// The agent command renders six operations: the CRUD four carry long
		// field notes (name uniqueness, avatar, disabled, project_scope, ACP
		// command ownership) plus the GET /api/agents ACP-model-list note, and
		// the two self-test routes were added so "create an agent" can be
		// verified rather than merely asserted. Descriptions are kept terse;
		// the behavioral rules live in the prompt template, not the spec.
		// Measured at 5708 when the self-test routes joined; grew to 6733 when
		// the delete description gained the takeover contract (#606), and to
		// 6749 when provider_id joined the PATCH field list, then 6986 after
		// #611 (queued-message edit) and the catalog endpoints landed,
		// and to 7075 when GET /api/agents gained the membershipCount note.
		// The headroom is deliberate, not drift.
		CommandAgent: 7100,
		// The MCP command renders four operations: GET/PATCH /api/config (the
		// global server list) and GET/PATCH /api/agents (the per-agent policy).
		//
		// The budget was ratcheted UP six times (9314 → 10301 → 10700 → 11600 →
		// 12200) each time another config block — `forge`, `meeting`, `mail`,
		// `tasks`, `share` and their sub-keys — joined the two shared config
		// descriptions. That was the wrong direction: those paragraphs document
		// the SETTINGS contract for whoever reads the API, and none of them is
		// actionable by a command whose only job is to edit `mcp.servers`. Every
		// ratchet simply paid for prose the AI never uses.
		//
		// The two config operations therefore carry `x-prompt-note` (see
		// operation.PromptNote): the spec keeps the full description, the prompt
		// renders the MCP-relevant summary. Measured 3373 after the split,
		// down from 12736 with the same four endpoints. The budget is now below
		// the old 12200 so the split cannot silently regress to whole paragraphs:
		// if the shared descriptions start leaking back in, the failure message
		// names the operation that grew.
		CommandMCP: 4000,
		// Three read-only operations (collections, documents, search). The
		// writes are deliberately absent: a question must not mutate the
		// library. Measured at 1070; headroom covers the search contract.
		CommandKBSearch: 1600,
	}
	for cmd, budget := range budgets {
		out, err := RenderCommand(cmd)
		require.NoError(t, err)
		if len(out) < budget {
			continue
		}
		// The failure has to say WHERE the bytes went, not just the total. The
		// total alone made every previous overrun a manual bisect through the
		// spec's descriptions; the per-operation sizes are computed from the
		// same rendered fragment, so the biggest contributor is the first line
		// of the message.
		t.Errorf("command %q renders %d bytes, over the %d budget (over by %d) — the operations below are ordered by bytes contributed, largest first:\n%s",
			cmd, len(out), budget, len(out)-budget, operationSizeBreakdown(t, cmd))
	}
}

// operationSizeBreakdown renders each operation of a command on its own and
// returns a size-ordered report: one line per operation with its byte count and
// its share of the whole fragment. It reuses writeEndpoint, so the numbers are
// exactly the bytes that end up in the prompt.
func operationSizeBreakdown(t *testing.T, cmd Command) string {
	t.Helper()

	specs := commandOperations[cmd]
	ids := make([]string, 0, len(specs))
	required := make(map[string]string, len(specs))
	for _, s := range specs {
		ids = append(ids, s.ID)
		required[s.ID] = s.Tag
	}
	eps, err := endpointsForOperations(ids, required)
	require.NoError(t, err)

	type row struct {
		label string
		size  int
		note  bool // the description was overridden by x-prompt-note
	}
	rows := make([]row, 0, len(eps))
	total := 0
	for _, ep := range eps {
		var b strings.Builder
		writeEndpoint(&b, ep)
		size := len(strings.TrimRight(b.String(), "\n"))
		total += size
		rows = append(rows, row{label: ep.Method + " " + ep.Path, size: size, note: ep.PromptNote})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })

	var b strings.Builder
	for _, r := range rows {
		src := "description"
		if r.note {
			src = "x-prompt-note"
		}
		fmt.Fprintf(&b, "  %6d bytes (%4.1f%%)  %-28s note from %s\n",
			r.size, 100*float64(r.size)/float64(max(total, 1)), r.label, src)
	}
	return strings.TrimRight(b.String(), "\n")
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

// TestRenderCommand_TaskDocumentsGatingScript pins the gating-script contract
// that is not derivable from the field names alone: the gate condition (exit 0
// runs the agent, anything else skips the run) and the four prompt template
// variables the script's result is substituted into. A regression here would
// leave the AI creating scripts whose output never reaches the prompt.
func TestRenderCommand_TaskDocumentsGatingScript(t *testing.T) {
	out, err := RenderCommand(CommandTask)
	require.NoError(t, err)

	assert.Contains(t, out, "退出码 0",
		"the gate condition must be stated")
	assert.Contains(t, out, "跳过本次 AI",
		"the closed-gate outcome must be stated")
	assert.Contains(t, out, "不建会话",
		"the no-session consequence must be stated")

	// The variables must be spelled literally so the model copies the exact
	// syntax into the prompt instead of paraphrasing it into dead text.
	for _, tok := range []string{"{{code}}", "{{stdout}}", "{{stderr}}", "{{output}}"} {
		assert.Containsf(t, out, tok, "the %s prompt variable must be documented", tok)
	}
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

// TestRenderCommand_PromptNoteOverridesDescription pins the split between the
// two audiences of an endpoint's prose.
//
// `GET /api/agents` documents the model-list merge rules for the UI's model
// picker — several kilobytes the AI never acts on when it just needs an agent
// id. Rendering all of it into every command that touches the endpoint is what
// pushed the task / agent / mcp budgets over their limits. The spec therefore
// carries `x-prompt-note` for the prompt fragment.
//
// This test asserts the behavior from BOTH sides, because either half alone
// would pass with the feature broken: the short note must appear, and the long
// description it replaces must not.
func TestRenderCommand_PromptNoteOverridesDescription(t *testing.T) {
	out, err := RenderCommand(CommandTask)
	require.NoError(t, err)

	assert.Contains(t, out, "列出当前项目可见的 agent",
		"the prompt note must reach the rendered command")
	// A distinctive sentence from the response-contract paragraph that the note
	// replaces. If the override silently stopped applying, this is what would
	// come back — and the size budget would fail, but only for commands large
	// enough to notice.
	assert.NotContains(t, out, "档位别名",
		"the model-picker contract must not be rendered into the prompt")
}

// TestRenderCommand_PromptNoteKeepsFullDescriptionInSpec is the other half: the
// override must be a RENDERING concern, not a deletion. The spec's own
// description has to stay intact for anyone reading the API docs.
func TestRenderCommand_PromptNoteKeepsFullDescriptionInSpec(t *testing.T) {
	s, err := load()
	require.NoError(t, err)

	var found bool
	for _, item := range s.Paths {
		op := item.method("get")
		if op == nil || op.OperationID != "agentsList" {
			continue
		}
		found = true
		assert.NotEmpty(t, op.PromptNote, "agentsList must carry x-prompt-note")
		assert.Contains(t, op.Description, "档位别名",
			"the full response contract must stay in the spec's description")
	}
	assert.True(t, found, "agentsList must exist in the spec")
}

// TestRenderCommand_MCPConfigUsesPromptNote pins the same split for the two
// /api/config operations the MCP command renders.
//
// Their shared descriptions document every settings block (wallpaper, port
// forwarding, mail, tasks, share, TTS, meetings, …). None of it is actionable
// for a command whose whole job is to rewrite `mcp.servers`, yet rendering it
// is what pushed /cb-mcp over its budget six times, each time "fixed" by
// raising the number. The MCP contract now lives in x-prompt-note; the full
// description stays for the API reader. Asserted from both sides: the MCP note
// must reach the prompt, the settings prose must not, and the settings prose
// must still exist in the spec.
func TestRenderCommand_MCPConfigUsesPromptNote(t *testing.T) {
	out, err := RenderCommand(CommandMCP)
	require.NoError(t, err)

	// The whole-replacement semantics are the one thing getting this wrong
	// silently deletes servers, so the note must state it.
	assert.Contains(t, out, "整体替换",
		"the MCP note must state that mcp.servers is replaced wholesale")
	assert.Contains(t, out, "明文返回",
		"the MCP note must state that credentials come back in plaintext")

	// Distinctive sentences from the settings paragraphs the note replaces.
	// If the override stopped applying, these are what would come back.
	for _, leaked := range []string{
		"tasks.retention_enabled",    // task retention sub-keys
		"share.link_ttl_days",        // share sub-keys
		"meeting.minutes_max_tokens", // meeting validation
		"tts.openai.stream_mode",     // TTS stream mode
		"port_forward.transport",     // tunnel transport switch
		"recent_project_cards",       // recent-project cards
	} {
		assert.NotContainsf(t, out, leaked,
			"the settings contract (%q) must not be rendered into /cb-mcp", leaked)
	}

	s, err := load()
	require.NoError(t, err)
	for _, id := range []string{"getConfig", "patchConfig"} {
		var found bool
		for _, item := range s.Paths {
			for _, method := range methodOrder {
				op := item.method(method)
				if op == nil || op.OperationID != id {
					continue
				}
				found = true
				assert.NotEmptyf(t, op.PromptNote, "%s must carry x-prompt-note", id)
				assert.Containsf(t, op.Description, "mcp.servers",
					"%s must keep the full settings description in the spec", id)
			}
		}
		assert.Truef(t, found, "%s must exist in the spec", id)
	}
}

// TestOperationIDs_Resolvable asserts every operationId a command declares
// actually exists in the spec. This is the drift guard's first half: renaming
// an operation without updating commandOperations fails here.
func TestOperationIDs_Resolvable(t *testing.T) {
	for _, cmd := range []Command{CommandChatSearch, CommandTask, CommandUsage, CommandAgent, CommandMCP} {
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
