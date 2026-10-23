package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A slim tool_use block carries a TOP-LEVEL file_path (model.SlimBlock.FilePath),
// a different slot from the copy inside `input`. It must survive the assembly
// intact — the viewer needs it to render the tool card, and the transcript is
// served verbatim.
//
// This test used to assert the opposite (that the slot was relativized), which
// pinned a sanitizer that has since been removed: rewriting only the paths that
// happened to start with the project root or home directory made the page look
// scrubbed while tool output, credentials and file contents sailed past it.
func TestSessionSharePayload_KeepsBlockLevelFilePath(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	// A slim block: top-level file_path, no input object, no side-table row —
	// exactly what the migration writes when it strips a tool call.
	seedMessage(t, db, "s1", "assistant",
		`{"blocks":[{"type":"tool_use","id":"t1","name":"Read","file_path":"/home/u/proj/src/secret.ts","done":true}]}`,
		0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msg := payloadMessages(t, raw)[0]
	blocks := contentBlocks(t, msg)
	require.NotEmpty(t, blocks)
	assert.Equal(t, "/home/u/proj/src/secret.ts", blocks[0]["file_path"],
		"the block-level path must reach the reader as stored")
}

// A file_path outside both the project and the home directory is likewise served
// whole.
func TestSessionSharePayload_KeepsBlockLevelFilePathOutsideRoots(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "assistant",
		`{"blocks":[{"type":"tool_use","id":"t1","name":"Read","file_path":"/opt/other/place/thing.ts","done":true}]}`,
		0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msg := payloadMessages(t, raw)[0]
	assert.Equal(t, "/opt/other/place/thing.ts", contentBlocks(t, msg)[0]["file_path"])
}
