package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCodexRollout(t *testing.T, root, day, id, cwd string, updatedAt time.Time) string {
	t.Helper()
	dir := filepath.Join(root, "sessions", filepath.FromSlash(day))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, fmt.Sprintf("rollout-2026-08-27T10-00-00-%s.jsonl", id))
	content := fmt.Sprintf(
		`{"timestamp":"2026-08-27T10:00:00.000Z","type":"session_meta","payload":{"id":%q,"timestamp":"2026-08-27T10:00:00.000Z","cwd":%q,"originator":"codex_cli_rs","cli_version":"1.0.0"}}`+"\n"+
			`{"timestamp":"2026-08-27T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"sensitive text must not be parsed"}]}}`+"\n",
		id, cwd,
	)
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	require.NoError(t, os.Chtimes(file, updatedAt, updatedAt))
	return file
}

func TestScanCodexSessionsFiltersProjectAndSortsNewestFirst(t *testing.T) {
	codexHome := t.TempDir()
	project := filepath.Join(t.TempDir(), "Project With Spaces")
	older := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	writeCodexRollout(t, codexHome, "2026/08/26", "00000000-0000-4000-8000-000000000001", project, older)
	writeCodexRollout(t, codexHome, "2026/08/27", "00000000-0000-4000-8000-000000000002", project, newer)
	writeCodexRollout(t, codexHome, "2026/08/27", "00000000-0000-4000-8000-000000000003", filepath.Join(t.TempDir(), "other"), newer.Add(time.Hour))

	sessions, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), project, 100, 100)

	require.Len(t, sessions, 2)
	assert.Equal(t, "00000000-0000-4000-8000-000000000002", sessions[0].SessionID)
	assert.Equal(t, "00000000-0000-4000-8000-000000000001", sessions[1].SessionID)
	assert.Equal(t, 3, stats.scanned)
}

func TestScanCodexSessionsSkipsMalformedAndMissingFields(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions", "2026", "08", "27")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "rollout-broken.jsonl"), []byte("{broken\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "rollout-missing.jsonl"),
		[]byte(`{"type":"session_meta","payload":{"id":"id-without-cwd"}}`+"\n"), 0o644))

	sessionsRoot := filepath.Dir(filepath.Dir(filepath.Dir(root)))
	sessions, stats := scanCodexSessions(sessionsRoot, "C:/Work/Repo", 100, 100)

	assert.Empty(t, sessions)
	assert.Equal(t, 2, stats.skipped)
}

func TestCodexProjectPathsEqualWindowsVariants(t *testing.T) {
	assert.True(t, codexProjectPathsEqual(`C:\Work\Repo With Spaces`, `c:/work/repo with spaces/`))
	assert.True(t, codexProjectPathsEqual(`C:\Work\Repo\.\sub\..`, `c:/work/repo`))
	assert.True(t, codexProjectPathsEqual(`\\Server\Share\Repo`, `//server/share/repo/`))
	assert.False(t, codexProjectPathsEqual(`C:\Work\Repo`, `C:\Work\Other`))
}

func TestCodexProjectPathsEqualPreservesPOSIXCaseSensitivity(t *testing.T) {
	assert.True(t, codexProjectPathsEqual("/home/user/repo/", "/home/user/repo"))
	assert.False(t, codexProjectPathsEqual("/home/User/repo", "/home/user/repo"))
}

func TestScanCodexSessionsHonorsLimits(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()
	for i := range 5 {
		writeCodexRollout(t, codexHome, "2026/08/27",
			fmt.Sprintf("00000000-0000-4000-8000-00000000000%d", i), project,
			time.Date(2026, 8, 27, 10, i, 0, 0, time.UTC))
	}

	sessions, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), project, 3, 2)

	assert.Len(t, sessions, 2)
	assert.LessOrEqual(t, stats.scanned, 3)
}

func TestScanCodexSessionsAppliesResultLimitAfterUpdatedSort(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()
	base := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	for i := range 3 {
		file := writeCodexRollout(t, codexHome, fmt.Sprintf("2026/08/%02d", 25+i),
			fmt.Sprintf("00000000-0000-4000-8000-00000000001%d", i), project, base.Add(time.Duration(i)*time.Hour))
		if i == 0 {
			require.NoError(t, os.Chtimes(file, base.Add(10*time.Hour), base.Add(10*time.Hour)))
		}
	}

	sessions, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), project, 100, 2)

	require.Len(t, sessions, 2)
	assert.Equal(t, "00000000-0000-4000-8000-000000000010", sessions[0].SessionID)
	assert.Equal(t, 3, stats.scanned)
}

func TestParseCodexSessionHeaderUsesMetadataID(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()
	file := writeCodexRollout(t, codexHome, "2026/08/27",
		"00000000-0000-4000-8000-000000000009", project, time.Now())
	info, err := os.Stat(file)
	require.NoError(t, err)

	session, err := parseCodexSessionHeader(file, info)

	require.NoError(t, err)
	assert.Equal(t, "00000000-0000-4000-8000-000000000009", session.SessionID)
	assert.Equal(t, project, session.Cwd)
	assert.Equal(t, time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC), session.CreatedAt)
}

func TestListCodexSessionsFromDiskUsesCodexHomeAndReturnsACPInfo(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()
	updatedAt := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	writeCodexRollout(t, codexHome, "2026/08/27",
		"00000000-0000-4000-8000-000000000021", project, updatedAt)
	t.Setenv("CODEX_HOME", codexHome)

	sessions, err := listCodexSessionsFromDisk(&model.Agent{Backend: "codex"}, project)

	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "00000000-0000-4000-8000-000000000021", string(sessions[0].SessionId))
	assert.Equal(t, project, sessions[0].Cwd)
	require.NotNil(t, sessions[0].UpdatedAt)
	assert.Equal(t, updatedAt.Format(time.RFC3339), *sessions[0].UpdatedAt)
}

func TestListCodexSessionsFromDiskSkipsEmptyProject(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())

	sessions, err := listCodexSessionsFromDisk(&model.Agent{Backend: "codex"}, " ")

	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func TestResolveCodexHomeUsesEnvironment(t *testing.T) {
	expected := filepath.Join(t.TempDir(), "custom", "..", "codex")
	t.Setenv("CODEX_HOME", expected)

	actual, err := resolveCodexHome()

	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(expected), actual)
}

func TestScanCodexSessionsMissingRootAndDisabledLimits(t *testing.T) {
	sessions, stats := scanCodexSessions(filepath.Join(t.TempDir(), "missing"), t.TempDir(), 100, 100)
	assert.Empty(t, sessions)
	assert.Zero(t, stats.scanned)

	sessions, stats = scanCodexSessions(t.TempDir(), t.TempDir(), 0, 100)
	assert.Empty(t, sessions)
	assert.Zero(t, stats.scanned)
}

func TestScanCodexSessionsMarksScanLimitReached(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()
	for i := range 3 {
		writeCodexRollout(t, codexHome, "2026/08/27",
			fmt.Sprintf("00000000-0000-4000-8000-00000000003%d", i), project, time.Now())
	}

	sessions, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), project, 1, 10)

	require.Len(t, sessions, 1)
	assert.True(t, stats.limitReached)
	assert.Equal(t, 1, stats.scanned)
}

func TestParseCodexSessionHeaderFallbacksAndErrors(t *testing.T) {
	dir := t.TempDir()
	modified := time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC)

	sessionIDFallback := filepath.Join(dir, "session-id-fallback.jsonl")
	require.NoError(t, os.WriteFile(sessionIDFallback, []byte(
		"\n"+`{"timestamp":"2026-08-27T10-00-00","type":"session_meta","payload":{"session_id":"fallback-id","cwd":"/project"}}`+"\n",
	), 0o644))
	require.NoError(t, os.Chtimes(sessionIDFallback, modified, modified))
	info, err := os.Stat(sessionIDFallback)
	require.NoError(t, err)
	session, err := parseCodexSessionHeader(sessionIDFallback, info)
	require.NoError(t, err)
	assert.Equal(t, "fallback-id", session.SessionID)
	assert.Equal(t, time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC), session.CreatedAt)

	wrongType := filepath.Join(dir, "wrong-type.jsonl")
	require.NoError(t, os.WriteFile(wrongType, []byte(`{"type":"response_item","payload":{}}`+"\n"), 0o644))
	info, err = os.Stat(wrongType)
	require.NoError(t, err)
	_, err = parseCodexSessionHeader(wrongType, info)
	assert.ErrorContains(t, err, "first record type")

	badPayload := filepath.Join(dir, "bad-payload.jsonl")
	require.NoError(t, os.WriteFile(badPayload, []byte(`{"type":"session_meta","payload":"invalid"}`+"\n"), 0o644))
	info, err = os.Stat(badPayload)
	require.NoError(t, err)
	_, err = parseCodexSessionHeader(badPayload, info)
	assert.ErrorContains(t, err, "decode session metadata")

	empty := filepath.Join(dir, "empty.jsonl")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	info, err = os.Stat(empty)
	require.NoError(t, err)
	_, err = parseCodexSessionHeader(empty, info)
	assert.ErrorContains(t, err, "session metadata not found")
}

func TestParseAndFormatCodexSessionTime(t *testing.T) {
	assert.True(t, parseCodexSessionTime("").IsZero())
	assert.True(t, parseCodexSessionTime("invalid").IsZero())
	assert.Nil(t, formatCodexSessionTime(time.Time{}))

	parsed := parseCodexSessionTime("2026-08-27T10:00:00.123456Z")
	assert.Equal(t, 123456000, parsed.Nanosecond())
	formatted := formatCodexSessionTime(parsed)
	require.NotNil(t, formatted)
	assert.Equal(t, "2026-08-27T10:00:00Z", *formatted)
}

func TestResolveCodexHomeDefaultsToUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")

	actual, err := resolveCodexHome()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".codex"), actual)
}

func TestNormalizeCodexProjectPathEdgeCases(t *testing.T) {
	// Empty / whitespace-only → empty string.
	assert.Equal(t, "", normalizeCodexProjectPath(""))
	assert.Equal(t, "", normalizeCodexProjectPath("   "))

	// Windows drive path → normalized + lowercased.
	assert.Equal(t, "c:/users/test", normalizeCodexProjectPath(`C:\Users\Test`))

	// UNC path → double-slash preserved, no trailing slash.
	assert.Equal(t, "//server/share", normalizeCodexProjectPath(`\\server\share\`))

	// POSIX path unchanged (case-sensitive).
	assert.Equal(t, "/home/User/Project", normalizeCodexProjectPath("/home/User/Project"))

	// "." cleans to empty.
	assert.Equal(t, "", normalizeCodexProjectPath("."))
}

func TestScanCodexSessionsSortsEqualUpdatedAtByCreatedAt(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()
	same := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	// Same UpdatedAt, different CreatedAt (determined by file content timestamp).
	dir := filepath.Join(codexHome, "sessions", "2026", "08", "27")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	earlier := filepath.Join(dir, "rollout-2026-08-27T09-00-00-earlier.jsonl")
	require.NoError(t, os.WriteFile(earlier, []byte(
		`{"timestamp":"2026-08-27T09:00:00.000Z","type":"session_meta","payload":{"id":"sess-earlier","timestamp":"2026-08-27T09:00:00.000Z","cwd":"`+project+`","originator":"codex_cli_rs","cli_version":"1.0.0"}}`+"\n",
	), 0o644))
	require.NoError(t, os.Chtimes(earlier, same, same))
	later := filepath.Join(dir, "rollout-2026-08-27T11-00-00-later.jsonl")
	require.NoError(t, os.WriteFile(later, []byte(
		`{"timestamp":"2026-08-27T11:00:00.000Z","type":"session_meta","payload":{"id":"sess-later","timestamp":"2026-08-27T11:00:00.000Z","cwd":"`+project+`","originator":"codex_cli_rs","cli_version":"1.0.0"}}`+"\n",
	), 0o644))
	require.NoError(t, os.Chtimes(later, same, same))

	sessions, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), project, 100, 100)

	require.Len(t, sessions, 2)
	assert.Equal(t, "sess-later", sessions[0].SessionID, "later CreatedAt wins when UpdatedAt ties")
	assert.Equal(t, "sess-earlier", sessions[1].SessionID)
	assert.Equal(t, 2, stats.scanned)
}

func TestScanCodexSessionsSkipsEntryInfoErrors(t *testing.T) {
	codexHome := t.TempDir()
	project := t.TempDir()

	// A rollout file that disappears between ReadDir and entry.Info() is hard
	// to fabricate portably; instead verify the scan still tolerates a broken
	// rollout symlink that points nowhere (Info() on a dangling symlink fails).
	dir := filepath.Join(codexHome, "sessions", "2026", "08", "27")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	broken := filepath.Join(dir, "rollout-2026-08-27T10-00-00-broken.jsonl")
	require.NoError(t, os.Symlink(filepath.Join(dir, "does-not-exist"), broken))

	sessions, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), project, 100, 100)

	assert.Empty(t, sessions)
	assert.Equal(t, 1, stats.skipped, "dangling symlink should be counted as skipped")
}

func TestParseCodexSessionHeaderOpenError(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	require.NoError(t, err)
	_, err = parseCodexSessionHeader(filepath.Join(t.TempDir(), "missing.jsonl"), info)
	assert.Error(t, err)
}
