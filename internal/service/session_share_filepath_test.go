package service_test

import (
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionSharePayload_SanitizesBlockLevelFilePath is the regression guard
// for an absolute-path leak in the public snapshot.
//
// A slim tool_use block carries a TOP-LEVEL file_path (model.SlimBlock.FilePath),
// which is a different slot from the copy inside `input`. inlineMessageContent
// sanitized input/output/summary/text but never named file_path, so the raw
// absolute path shipped verbatim to anonymous viewers — defeating the stated
// guarantee that paths are relativized.
//
// The pre-existing SanitizesPaths test could not catch this: its fixture puts
// file_path INSIDE input, which was already handled. This test exercises the
// block-level slot, which is the one the storage layer actually populates.
func TestSessionSharePayload_SanitizesBlockLevelFilePath(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	// A slim block: top-level file_path, no input object, no side-table row —
	// exactly what the migration writes when it strips a tool call.
	seedMessage(t, db, "s1", "assistant",
		`{"blocks":[{"type":"tool_use","id":"t1","name":"Read","file_path":"/home/u/proj/src/secret.ts","done":true}]}`,
		0, 0)

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.NotContains(t, raw, testProjectRoot,
		"the project root must not appear anywhere in the snapshot")
	assert.NotContains(t, raw, "/home/u",
		"the home directory must not appear anywhere in the snapshot")

	// The path is still present, just relativized — the viewer keeps the
	// information it needs to render the tool call.
	msg := payloadMessages(t, raw)[0]
	blocks := contentBlocks(t, msg)
	require.NotEmpty(t, blocks)
	assert.Equal(t, "src/secret.ts", blocks[0]["file_path"],
		"the block-level path must be relativized like every other path field")
}

// A file_path outside both the project and the home directory falls back to its
// basename rather than leaking the full absolute path.
func TestSessionSharePayload_BlockLevelFilePathOutsideRootsFallsBackToBasename(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "assistant",
		`{"blocks":[{"type":"tool_use","id":"t1","name":"Read","file_path":"/opt/other/place/thing.ts","done":true}]}`,
		0, 0)

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.NotContains(t, raw, "/opt/other/place",
		"a path outside the known roots must not leak its directories")
	msg := payloadMessages(t, raw)[0]
	assert.Equal(t, "thing.ts", contentBlocks(t, msg)[0]["file_path"])
}
