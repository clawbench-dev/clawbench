package cline

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// Captured from cline 3.0.75: `cline history --json` over a root holding both
// an ACP session (no title) and an older CLI run (titled). Extra fields the CLI
// writes (usage, checkpoint, metadata, prompt) must not disturb the decode.
func TestParseClineHistory_RealFrames(t *testing.T) {
	raw := []byte(`[
	  {"sessionId":"1795532036842_3QDdZ_cli","source":"cli","cwd":"/tmp/probe-cline2",
	   "provider":"cline","model":"cline-free/nemotron-3.5-super-vl","status":"idle",
	   "prompt":"hi","updatedAt":"2026-11-24T14:53:59.120Z",
	   "metadata":{"usage":{"inputTokens":0}},
	   "messagesPath":"/home/u/.cline/data/sessions/1795532036842_3QDdZ_cli/1795532036842_3QDdZ_cli.messages.json"},
	  {"sessionId":"1780478496362_6cegk","source":"cli","cwd":"/home/u/projects/clawbench",
	   "title":"say hello","status":"completed","exitCode":0,
	   "updatedAt":"2026-06-03T09:21:39.096Z"}
	]`)
	records, err := parseClineHistory(raw)
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "1795532036842_3QDdZ_cli", records[0].SessionID)
	assert.Equal(t, "/tmp/probe-cline2", records[0].Cwd)
	assert.Empty(t, records[0].Title, "an ACP session has no title")
	assert.Equal(t, "say hello", records[1].Title)
	assert.Equal(t, "2026-06-03T09:21:39.096Z", records[1].UpdatedAt)
}

// An empty root prints `[]` (measured); a decode failure must be reported, not
// swallowed into an empty list the drawer would show as "no sessions".
func TestParseClineHistory_EmptyAndMalformed(t *testing.T) {
	records, err := parseClineHistory([]byte("[]"))
	require.NoError(t, err)
	assert.Empty(t, records)

	_, err = parseClineHistory([]byte("not json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "history --json")
}

// cline keeps sessions globally, not per project, so the drawer's project scope
// has to be applied here. An empty cwd means "no scope".
func TestConvertClineSessions_ScopesToProject(t *testing.T) {
	records := []clineHistoryRecord{
		{SessionID: "a", Cwd: "/p/one", Title: "A", UpdatedAt: "2026-11-01T00:00:00Z"},
		{SessionID: "b", Cwd: "/p/two", Title: "B"},
		{SessionID: "c", Cwd: "/p/one/", Title: "C"}, // trailing slash is the same project
		{SessionID: "", Cwd: "/p/one"},               // no id: unusable
		{SessionID: "d", Cwd: "", Title: "D"},        // no recorded cwd: keep, fill from scope
	}

	scoped := convertClineSessions(records, "/p/one")
	require.Len(t, scoped, 3)
	assert.Equal(t, "a", string(scoped[0].SessionId))
	assert.Equal(t, "c", string(scoped[1].SessionId))
	assert.Equal(t, "d", string(scoped[2].SessionId))
	assert.Equal(t, "/p/one", scoped[2].Cwd, "a session without a cwd inherits the scope")

	all := convertClineSessions(records, "")
	require.Len(t, all, 4)
	require.NotNil(t, all[0].Title)
	assert.Equal(t, "A", *all[0].Title)
	require.NotNil(t, all[0].UpdatedAt)
	assert.Equal(t, "2026-11-01T00:00:00Z", *all[0].UpdatedAt)
}

// The scanner goes through the same CLI the agent runs (the first word of its
// ACP command) and its failure reaches the caller with the command named.
func TestListClineSessionsFromDisk_UsesAgentBinaryAndPropagatesFailure(t *testing.T) {
	orig := clineHistory
	t.Cleanup(func() { clineHistory = orig })

	var gotBin string
	clineHistory = func(agent *model.Agent) ([]byte, error) {
		gotBin = agent.AcpCommand
		return []byte(`[{"sessionId":"s1","cwd":"/p","title":"T"}]`), nil
	}
	sessions, err := listClineSessionsFromDisk(&model.Agent{AcpCommand: "/opt/cline/bin/cline --acp"}, "/p")
	require.NoError(t, err)
	assert.Equal(t, "/opt/cline/bin/cline --acp", gotBin)
	require.Len(t, sessions, 1)
	assert.Equal(t, "/p", sessions[0].Cwd)

	clineHistory = func(*model.Agent) ([]byte, error) { return nil, assert.AnError }
	_, err = listClineSessionsFromDisk(&model.Agent{}, "/p")
	require.Error(t, err)
}

// runClineHistory locates the binary from the agent's ACP command. A missing
// binary is reported with the command it tried, not silently as "no sessions".
func TestRunClineHistory_MissingBinaryIsReported(t *testing.T) {
	_, err := runClineHistory(&model.Agent{AcpCommand: "definitely-not-a-cline-binary-xyz --acp"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-a-cline-binary-xyz history --json")
}

// The production runner works against the real CLI: `cline history --json` needs
// no TTY and prints a JSON array, and the scanner reads it back through the
// same path the drawer uses. Skipped when cline is not installed.
func TestRunClineHistory_RealCLI(t *testing.T) {
	bin, err := exec.LookPath("cline")
	if err != nil {
		t.Skip("cline not installed")
	}
	raw, err := runClineHistory(&model.Agent{AcpCommand: bin + " --acp"})
	require.NoError(t, err)
	records, err := parseClineHistory(raw)
	require.NoError(t, err)
	// The count depends on the machine, so only the decode is asserted.
	_ = records
}
