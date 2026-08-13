package codebuddy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCodebuddySessionJSONL writes a synthetic CodeBuddy session jsonl file
// (matching the real ~/.codebuddy/projects/<slug>/<uuid>.jsonl format) and
// returns the session UUID.
func writeCodebuddySessionJSONL(t *testing.T, dir, cwd, title string, msgs int) string {
	t.Helper()
	uuid := "00000000-0000-4000-8000-00000000000" + fmt.Sprintf("%d", msgs%10)
	path := filepath.Join(dir, uuid+".jsonl")

	var content string
	// First line carries cwd.
	content += fmt.Sprintf(`{"id":"u1","timestamp":1780000000000,"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}],"sessionId":"%s","cwd":"%s"}`+"\n", uuid, cwd)
	// ai-title carries the title and a newer timestamp.
	content += fmt.Sprintf(`{"timestamp":1780000200000,"type":"ai-title","aiTitle":"%s","sessionId":"%s","cwd":"%s"}`+"\n", title, uuid, cwd)

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return uuid
}

func TestScanCodebuddySessions_ReturnsParsedSessions(t *testing.T) {
	dir := t.TempDir()
	writeCodebuddySessionJSONL(t, dir, "/home/user/proj", "标题A", 1)
	writeCodebuddySessionJSONL(t, dir, "/home/user/proj2", "标题B", 2)
	// A non-jsonl file should be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644))
	// A corrupt jsonl should be skipped gracefully.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "corrupt.jsonl"), []byte("{not json\n"), 0o644))

	sessions := scanCodebuddySessionsDir(dir)
	require.Len(t, sessions, 2, "should return exactly 2 valid sessions")

	byID := map[string]codebuddyDiskSession{}
	for _, s := range sessions {
		byID[s.SessionID] = s
	}
	s1, ok := byID["00000000-0000-4000-8000-000000000001"]
	require.True(t, ok, "session 1 should be present")
	assert.Equal(t, "/home/user/proj", s1.Cwd)
	assert.Equal(t, "标题A", s1.Title)
	assert.Equal(t, int64(1780000200000), s1.UpdatedAtMs)

	s2, ok := byID["00000000-0000-4000-8000-000000000002"]
	require.True(t, ok, "session 2 should be present")
	assert.Equal(t, "/home/user/proj2", s2.Cwd)
	assert.Equal(t, "标题B", s2.Title)
}

// TestCodebuddyListSessionsFromDisk_ISOUpdatedAt verifies that converting a
// scanned disk session to acp.SessionInfo produces an ISO-8601 UpdatedAt that
// the frontend's new Date() can parse, with a correct Title/SessionId/Cwd.
func TestCodebuddyListSessionsFromDisk_ISOUpdatedAt(t *testing.T) {
	// Build the acp.SessionInfo from a scanned disk session (temp dir), not the
	// real ~/.codebuddy. Use a temp HOME to avoid scanning the user's files.
	dir := t.TempDir()
	writeCodebuddySessionJSONL(t, dir, "/home/user/proj", "标题", 4)
	disks := scanCodebuddySessionsDir(dir)
	require.Len(t, disks, 1)

	// Use the scanCodebuddySessionsFromHome path with a temp HOME layout to go
	// through listCodebuddySessionsFromDisk conversion without touching ~/.
	home := t.TempDir()
	projectDir := filepath.Join(home, "projects", "home-user-proj")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	writeCodebuddySessionJSONL(t, projectDir, "/home/user/proj", "标题", 5)

	// Refactor-friendly: call the conversion via a temp-home scan helper.
	sessions := convertDiskSessions(scanCodebuddySessionsFromHome(home))
	require.Len(t, sessions, 1)
	require.NotNil(t, sessions[0].UpdatedAt, "UpdatedAt should be set")
	// Frontend does new Date(iso) — verify it parses to a valid time.
	parsed, err := time.Parse(time.RFC3339, *sessions[0].UpdatedAt)
	require.NoError(t, err, "UpdatedAt should be RFC3339: %q", *sessions[0].UpdatedAt)
	assert.Equal(t, int64(1780000200), parsed.Unix(), "updatedAt should reflect the ai-title timestamp")
	require.NotNil(t, sessions[0].Title, "Title should be set")
	assert.Equal(t, "标题", *sessions[0].Title)
	assert.Equal(t, "00000000-0000-4000-8000-000000000005", string(sessions[0].SessionId))
	assert.Equal(t, "/home/user/proj", sessions[0].Cwd)
}

func TestScanCodebuddySessions_EmptyDir(t *testing.T) {
	sessions := scanCodebuddySessionsDir(t.TempDir())
	assert.Empty(t, sessions)
}

func TestScanCodebuddySessions_MissingDir(t *testing.T) {
	sessions := scanCodebuddySessionsDir(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Empty(t, sessions)
}

// TestScanCodebuddySessions_SkipsSubAgents verifies that agent-* sessions under
// a <session-uuid>/subagents/ directory are NOT treated as top-level sessions.
func TestScanCodebuddySessions_SkipsSubAgents(t *testing.T) {
	dir := t.TempDir()
	// A real top-level session jsonl.
	writeCodebuddySessionJSONL(t, dir, "/home/user/proj", "顶层会话", 1)
	// A sub-agent session under <uuid>/subagents/ — must be ignored.
	subDir := filepath.Join(dir, "00000000-0000-4000-8000-000000000001", "subagents")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	subContent := `{"id":"s1","timestamp":1780000300000,"type":"message","role":"user","content":[{"type":"input_text","text":"sub"}],"sessionId":"parent-1","cwd":"/home/user/proj"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "agent-abc123.jsonl"), []byte(subContent), 0o644))
	// Also a non-subagents nested dir should still be scanned (defensive).
	nested := filepath.Join(dir, "tool-results")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	writeCodebuddySessionJSONL(t, nested, "/home/user/proj", "嵌套会话", 2)

	sessions := scanCodebuddySessionsDir(dir)
	// Only the top-level and nested sessions (NOT the sub-agent) should appear.
	ids := map[string]bool{}
	for _, s := range sessions {
		ids[s.SessionID] = true
	}
	assert.Len(t, sessions, 2, "sub-agent session must be excluded")
	assert.False(t, ids["agent-abc123"], "agent-* sub-agent session should be skipped")
	assert.True(t, ids["00000000-0000-4000-8000-000000000001"], "top-level session should be present")
}

func TestScanCodebuddySessions_UpdatesAtIsMaxTimestamp(t *testing.T) {
	dir := t.TempDir()
	// File where the last line has the newest timestamp but is a message.
	path := filepath.Join(dir, "aaaa.jsonl")
	content := `{"id":"a","timestamp":100,"type":"message","role":"user","content":[{"type":"input_text","text":"x"}],"sessionId":"aaaa","cwd":"/cwd"}` + "\n" +
		`{"id":"b","timestamp":500,"type":"message","role":"assistant","content":[{"type":"output_text","text":"y"}],"sessionId":"aaaa","cwd":"/cwd"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	sessions := scanCodebuddySessionsDir(dir)
	require.Len(t, sessions, 1)
	assert.Equal(t, int64(500), sessions[0].UpdatedAtMs)
	assert.Equal(t, "/cwd", sessions[0].Cwd)
}

// TestCodebuddyListSessionsFromDisk_Integration verifies the exported scanner
// using a temporary data dir via the injectable path function.
func TestCodebuddyListSessionsFromDisk_UsesHomeDir(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "projects", "home-user-proj")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	uuid := writeCodebuddySessionJSONL(t, projectDir, "/home/user/proj", "标题", 3)

	sessions := scanCodebuddySessionsFromHome(home)
	require.Len(t, sessions, 1)
	assert.Equal(t, uuid, sessions[0].SessionID)
	assert.Equal(t, "标题", sessions[0].Title)
}

// TestCwdToCodebuddySlug verifies the project-directory slug derivation from a
// cwd (path with leading slash stripped, "/" → "-", dots preserved).
func TestCwdToCodebuddySlug(t *testing.T) {
	cases := []struct {
		cwd, want string
	}{
		{"/home/user/proj", "home-user-proj"},
		{"/home/xulongzhe/projects/clawbench", "home-xulongzhe-projects-clawbench"},
		{"/home/xulongzhe/projects/clawbench/internal/ai", "home-xulongzhe-projects-clawbench-internal-ai"},
		{"/home/xulongzhe/projects/clawbench/.codebuddy/worktrees/fix", "home-xulongzhe-projects-clawbench-.codebuddy-worktrees-fix"},
		{"/home/user/.codebuddy/skills", "home-user-.codebuddy-skills"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, cwdToCodebuddySlug(c.cwd), "cwdToCodebuddySlug(%q)", c.cwd)
	}
}

// TestListCodebuddySessionsFromDisk_ScopedToCwd verifies that when a cwd is
// provided, only that project's slug directory is scanned (not the whole tree).
func TestListCodebuddySessionsFromDisk_ScopedToCwd(t *testing.T) {
	home := t.TempDir()
	// Project A sessions under slug home-user-projA.
	dirA := filepath.Join(home, "projects", "home-user-projA")
	require.NoError(t, os.MkdirAll(dirA, 0o755))
	writeCodebuddySessionJSONL(t, dirA, "/home/user/projA", "A会话", 1)
	// Project B sessions under slug home-user-projB.
	dirB := filepath.Join(home, "projects", "home-user-projB")
	require.NoError(t, os.MkdirAll(dirB, 0o755))
	writeCodebuddySessionJSONL(t, dirB, "/home/user/projB", "B会话", 2)

	// Scoped to project A → only A's sessions.
	got := scanCodebuddyProjectDir(home, "/home/user/projA")
	require.Len(t, got, 1)
	assert.Equal(t, "/home/user/projA", got[0].Cwd)
	assert.Equal(t, "A会话", got[0].Title)

	// Scoped to project B → only B's sessions.
	got = scanCodebuddyProjectDir(home, "/home/user/projB")
	require.Len(t, got, 1)
	assert.Equal(t, "B会话", got[0].Title)
}
