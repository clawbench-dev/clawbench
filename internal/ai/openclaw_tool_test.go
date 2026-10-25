package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the OpenClaw ACP tool path against the rawInput shapes the
// `openclaw acp` bridge forwards (OpenClaw's native arguments, not canonical
// ACP names). `path` is remapped to file_path by the BASE table (so a tool this
// list has never seen still renders a path) and `ls` removes it again; edit's
// arguments live in a nested edits[] array.
//
// The mirror tests in backends/openclaw/register_test.go exercise the same
// behavior through the exported wrappers, but they live in another package,
// so they do not contribute coverage here.

func mustParseOpenClawInput(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m), "input is not valid JSON: %s", raw)
	return m
}

// --- openClawACPToolName ---

func TestOpenClawACPToolName(t *testing.T) {
	t.Run("empty title", func(t *testing.T) {
		assert.Equal(t, "", openClawACPToolName(""))
	})

	t.Run("tool name before the colon", func(t *testing.T) {
		assert.Equal(t, "read", openClawACPToolName("read: path: /a/b.go"))
		assert.Equal(t, "web_fetch", openClawACPToolName("web_fetch: url: https://example.com"))
	})

	t.Run("title without a colon is the name", func(t *testing.T) {
		assert.Equal(t, "read", openClawACPToolName("read"))
	})

	t.Run("a leading colon yields no name", func(t *testing.T) {
		// idx == 0 is not a separator: the whole title stays as the candidate
		// and then fails the phrase check below.
		assert.Equal(t, "", openClawACPToolName(": path: /a"))
	})

	t.Run("a phrase is not a tool name", func(t *testing.T) {
		// Titles are sometimes progress narration. Deriving a tool name from
		// them would rename the card after whatever the agent happened to say.
		assert.Equal(t, "", openClawACPToolName("Reading the file now: /a/b.go"))
		assert.Equal(t, "", openClawACPToolName("Searching/scanning: x"))
	})
}

// --- normalizeOpenClawACPInput ---

func TestNormalizeOpenClawACPInput(t *testing.T) {
	t.Run("empty input is an empty object", func(t *testing.T) {
		assert.Equal(t, "{}", normalizeOpenClawACPInput("read", nil))
		assert.Equal(t, "{}", normalizeOpenClawACPInput("read", []byte{}))
	})

	t.Run("read remaps path to file_path", func(t *testing.T) {
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("read", []byte(`{"path":"/a/b.go"}`)))
		assert.Equal(t, "/a/b.go", m["file_path"])
		assert.NotContains(t, m, "path", "the raw key must not survive")
	})

	t.Run("write remaps path and keeps content", func(t *testing.T) {
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("write", []byte(`{"path":"/x","content":"hi"}`)))
		assert.Equal(t, "/x", m["file_path"])
		assert.Equal(t, "hi", m["content"])
	})

	t.Run("ls keeps path", func(t *testing.T) {
		// The LS card reads `path`; remapping it would blank the card. This is
		// why the remap cannot be global.
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("ls", []byte(`{"path":"/tmp/x"}`)))
		assert.Equal(t, "/tmp/x", m["path"])
		assert.NotContains(t, m, "file_path")
	})

	t.Run("edit promotes the first nested edit", func(t *testing.T) {
		raw := `{"path":"/x","edits":[{"oldText":"aaa","newText":"bbb"},{"oldText":"ccc","newText":"ddd"}]}`
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("edit", []byte(raw)))
		assert.Equal(t, "/x", m["file_path"])
		// renderEditDiff reads only the flat fields, so without this promotion
		// the card draws a file header with an empty body.
		assert.Equal(t, "aaa", m["old_string"])
		assert.Equal(t, "bbb", m["new_string"])
		// The full list stays available for other consumers.
		edits, ok := m["edits"].([]any)
		require.True(t, ok, "edits[] must survive: %v", m)
		require.Len(t, edits, 2)
		second, ok := edits[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "ccc", second["old_string"])
		assert.Equal(t, "ddd", second["new_string"])
	})

	t.Run("edit without edits keeps the flat shape", func(t *testing.T) {
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("edit", []byte(`{"path":"/x"}`)))
		assert.Equal(t, "/x", m["file_path"])
	})

	t.Run("exec is untouched", func(t *testing.T) {
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("exec", []byte(`{"command":"echo hi","workdir":"/tmp"}`)))
		assert.Equal(t, "echo hi", m["command"])
		assert.Equal(t, "/tmp", m["workdir"])
	})

	t.Run("an unknown tool still remaps path", func(t *testing.T) {
		// The point of putting path→file_path in the BASE table: openclaw's tool
		// set grows with every release (the probe already saw apply_patch). A
		// tool the switch does not list must still render a file path, not a
		// blank Edit/Write header.
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("apply_patch", []byte(`{"path":"/x"}`)))
		assert.Equal(t, "/x", m["file_path"])
		assert.NotContains(t, m, "path")
	})

	t.Run("base remaps apply to every tool", func(t *testing.T) {
		// These four are canonical ACP spellings that the bridge forwards in
		// camelCase regardless of tool.
		raw := `{"filePath":"/f","oldString":"a","newString":"b","replaceAll":true}`
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("exec", []byte(raw)))
		assert.Equal(t, "/f", m["file_path"])
		assert.Equal(t, "a", m["old_string"])
		assert.Equal(t, "b", m["new_string"])
		assert.Equal(t, true, m["replace_all"])
	})

	t.Run("the shared table is never mutated", func(t *testing.T) {
		// normalizeOpenClawACPInput copies the table before ls deletes `path`
		// from its copy, so a later read call must still remap it.
		_ = normalizeOpenClawACPInput("ls", []byte(`{"path":"/x"}`))
		assert.Contains(t, openClawACPBaseRemaps, "path",
			"the package-level table must keep the shared path remap")
		m := mustParseOpenClawInput(t, normalizeOpenClawACPInput("read", []byte(`{"path":"/a"}`)))
		assert.Equal(t, "/a", m["file_path"], "a previous ls call must not disable the remap")
	})

	t.Run("invalid JSON is returned verbatim", func(t *testing.T) {
		// Not reachable from the wire (RawInput is already decoded), but the
		// function is exported for tests and must not panic on garbage.
		assert.Equal(t, "not-json", normalizeOpenClawACPInput("read", []byte("not-json")))
	})
}

// --- parseOpenClawACPToolCall ---

func TestParseOpenClawACPToolCall(t *testing.T) {
	t.Run("read start frame", func(t *testing.T) {
		tool := parseOpenClawACPToolCall(acp.SessionUpdateToolCall{
			ToolCallId: "call_abc",
			Title:      "read: path: /a/b.go",
			Kind:       acp.ToolKindRead,
			RawInput:   map[string]any{"path": "/a/b.go"},
		})
		require.NotNil(t, tool)
		assert.Equal(t, "Read", tool.Name)
		assert.Equal(t, "call_abc", tool.ID)
		assert.False(t, tool.Done)
		assert.JSONEq(t, `{"file_path":"/a/b.go"}`, tool.Input)
	})

	t.Run("ls start frame keeps path", func(t *testing.T) {
		tool := parseOpenClawACPToolCall(acp.SessionUpdateToolCall{
			ToolCallId: "call_ls",
			Title:      "ls: path: /tmp",
			Kind:       acp.ToolKindRead,
			RawInput:   map[string]any{"path": "/tmp"},
		})
		assert.JSONEq(t, `{"path":"/tmp"}`, tool.Input)
	})

	t.Run("no raw input leaves the input empty", func(t *testing.T) {
		tool := parseOpenClawACPToolCall(acp.SessionUpdateToolCall{
			ToolCallId: "call_bare",
			Title:      "read",
			Kind:       acp.ToolKindRead,
		})
		assert.Empty(t, tool.Input)
	})
}

// --- parseOpenClawACPToolCallUpdate ---

func TestParseOpenClawACPToolCallUpdate(t *testing.T) {
	t.Run("completed update carries status and output", func(t *testing.T) {
		status := acp.ToolCallStatusCompleted
		title := "exec: command: echo hi"
		tool := parseOpenClawACPToolCallUpdate(acp.SessionToolCallUpdate{
			ToolCallId: "call_abc",
			Status:     &status,
			Title:      &title,
			RawInput:   map[string]any{"command": "echo hi"},
			RawOutput:  map[string]any{"stdout": "hi\n"},
		})
		require.NotNil(t, tool)
		assert.True(t, tool.Done)
		assert.Equal(t, "success", tool.Status)
		assert.Equal(t, "call_abc", tool.ID)
		assert.JSONEq(t, `{"command":"echo hi"}`, tool.Input)
		assert.Contains(t, tool.Output, "hi")
	})

	t.Run("failed update marks an error", func(t *testing.T) {
		status := acp.ToolCallStatusFailed
		tool := parseOpenClawACPToolCallUpdate(acp.SessionToolCallUpdate{
			ToolCallId: "call_fail",
			Status:     &status,
		})
		assert.True(t, tool.Done)
		assert.Equal(t, "error", tool.Status)
	})

	t.Run("in-progress update keeps the name from the opening frame", func(t *testing.T) {
		// A done update must not derive a name from its title: a completed
		// frame's title can be progress narration, which would rename the card.
		status := acp.ToolCallStatusCompleted
		title := "Reading the file now"
		tool := parseOpenClawACPToolCallUpdate(acp.SessionToolCallUpdate{
			ToolCallId: "call_abc",
			Status:     &status,
			Title:      &title,
		})
		assert.Empty(t, tool.Name)
	})

	t.Run("nil title does not panic", func(t *testing.T) {
		status := acp.ToolCallStatusInProgress
		tool := parseOpenClawACPToolCallUpdate(acp.SessionToolCallUpdate{
			ToolCallId: "call_notitle",
			Status:     &status,
			RawInput:   map[string]any{"path": "/a"},
		})
		assert.False(t, tool.Done)
	})

	t.Run("falls back to content when raw input is absent", func(t *testing.T) {
		status := acp.ToolCallStatusInProgress
		tool := parseOpenClawACPToolCallUpdate(acp.SessionToolCallUpdate{
			ToolCallId: "call_content",
			Status:     &status,
			Content: []acp.ToolCallContent{
				{
					Content: &acp.ToolCallContentContent{
						Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello"}},
					},
				},
			},
		})
		assert.NotNil(t, tool)
	})
}

// --- Dispatch from the shared ACP routing ---

// TestParseACPToolCall_OpenClawDispatch pins that the backend id routes to the
// OpenClaw parsers rather than the generic flat remap. Without the case, a read
// would keep `path` and the card would show no file.
func TestParseACPToolCall_OpenClawDispatch(t *testing.T) {
	tool := parseACPToolCall("openclaw", acp.SessionUpdateToolCall{
		ToolCallId: "call_1",
		Title:      "read: path: /a/b.go",
		Kind:       acp.ToolKindRead,
		RawInput:   map[string]any{"path": "/a/b.go"},
	})
	assert.JSONEq(t, `{"file_path":"/a/b.go"}`, tool.Input,
		"openclaw must use its per-tool normalizer, not the generic remap")
}

func TestParseACPToolCallUpdate_OpenClawDispatch(t *testing.T) {
	status := acp.ToolCallStatusCompleted
	title := "read: path: /a/b.go"
	tool := parseACPToolCallUpdate("openclaw", acp.SessionToolCallUpdate{
		ToolCallId: "call_1",
		Status:     &status,
		Title:      &title,
		RawInput:   map[string]any{"path": "/a/b.go"},
	})
	assert.True(t, tool.Done)
	assert.JSONEq(t, `{"file_path":"/a/b.go"}`, tool.Input)

	// Without a title the tool name is unknown, so only the BASE remaps apply.
	// `path` is one of them, which is the intended trade-off: an update that
	// omits its title can only be a file tool in practice (exec/ls titles are
	// always present), and a blank Edit/Write header is worse than an LS card
	// reading file_path for the handful of frames that drop the title.
	tool = parseACPToolCallUpdate("openclaw", acp.SessionToolCallUpdate{
		ToolCallId: "call_2",
		Status:     &status,
		RawInput:   map[string]any{"path": "/a/b.go"},
	})
	assert.JSONEq(t, `{"file_path":"/a/b.go"}`, tool.Input,
		"an untitled update falls back to the base remaps, which include path")
}

// --- OpenClawACPBaseRemapsForTest ---

// TestOpenClawACPBaseRemapsForTest_ReturnsCopy pins the reason this export
// exists at all: the backend sub-package needs to compare the table against its
// own, and it must not be able to change production behavior while doing so.
// Returning the map itself would make every later frame normalize differently
// depending on test execution order.
func TestOpenClawACPBaseRemapsForTest_ReturnsCopy(t *testing.T) {
	first := OpenClawACPBaseRemapsForTest()
	require.NotEmpty(t, first, "the base table must not be empty")
	assert.Equal(t, "file_path", first["path"],
		"the exported copy must carry the same remaps as the package table")

	first["path"] = "tampered"
	delete(first, "oldString")

	assert.Equal(t, "file_path", openClawACPBaseRemaps["path"],
		"mutating the copy must not touch the package-level table")
	assert.Contains(t, openClawACPBaseRemaps, "oldString",
		"deleting a key from the copy must not remove it from the table")

	// A second call sees the untouched table, not the previous caller's edits.
	second := OpenClawACPBaseRemapsForTest()
	assert.Equal(t, "file_path", second["path"])
	assert.Contains(t, second, "oldString")
}
