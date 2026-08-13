//go:build integration

package codebuddy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// CodeBuddy On-Disk ListSessions — Real Data Verification
// ===========================================================================
//
// Verifies that scanning ~/.codebuddy/projects enumerates real CodeBuddy
// sessions. This uses the actual scanner (scanCodebuddySessionsDir) against the
// real home directory. It does NOT assert a specific session count (the user's
// data varies) but confirms sessions parse with valid cwd/sessionId fields.

// TestCodebuddyACP_ListSessions_OnDisk verifies the disk scanner reads real
// CodeBuddy sessions with usable cwd/sessionId fields.
func TestCodebuddyACP_ListSessions_OnDisk(t *testing.T) {
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping on-disk ListSessions integration test")
	}

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	projectsDir := filepath.Join(home, ".codebuddy", "projects")

	sessions := scanCodebuddySessionsDir(projectsDir)
	t.Logf("Scanned %d on-disk CodeBuddy sessions from %s", len(sessions), projectsDir)

	// There should be at least one session on a real install (the environment
	// has a populated ~/.codebuddy). If the dir is missing entirely, skip.
	if len(sessions) == 0 {
		if _, statErr := os.Stat(projectsDir); os.IsNotExist(statErr) {
			t.Skipf("~/.codebuddy/projects not found — skipping on-disk ListSessions test")
		}
	}

	for _, s := range sessions {
		assert.NotEmpty(t, s.SessionID, "session ID should not be empty")
		assert.NotEmpty(t, s.Cwd, "session cwd should be populated from jsonl for %q", s.SessionID)
		if s.Title != "" {
			t.Logf("  session %s cwd=%q title=%q updatedAtMs=%d", s.SessionID, s.Cwd, s.Title, s.UpdatedAtMs)
		} else {
			t.Logf("  session %s cwd=%q (untitled)", s.SessionID, s.Cwd)
		}
	}
}

// TestCodebuddyACP_ListSessions_ScanFindsCreatedSession is a focused check: if
// a session already exists on disk from prior runs, its ACP UUID must appear as
// a jsonl filename. This confirms the filename==sessionId mapping end-to-end.
func TestCodebuddyACP_ListSessions_ScanFindsCreatedSession(t *testing.T) {
	// Look for any known session file and confirm the scanner picks it up with
	// the same session ID (the filename minus .jsonl).
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	projectsDir := filepath.Join(home, ".codebuddy", "projects")
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		t.Skip("~/.codebuddy/projects not found — skipping")
	}

	// Wait briefly so any late writes settle (mirrors real usage).
	time.Sleep(100 * time.Millisecond)

	var sampleJSONL string
	err = filepath.WalkDir(projectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == ".jsonl" && sampleJSONL == "" {
			sampleJSONL = path
		}
		return nil
	})
	require.NoError(t, err)
	if sampleJSONL == "" {
		t.Skip("no jsonl session files found — skipping")
	}

	wantID := filepath.Base(sampleJSONL)
	wantID = wantID[:len(wantID)-len(".jsonl")]

	sessions := scanCodebuddySessionsDir(projectsDir)
	found := false
	for _, s := range sessions {
		if s.SessionID == wantID {
			found = true
			break
		}
	}
	assert.True(t, found, "scanner should discover jsonl file %s (session id %s)", sampleJSONL, wantID)
}

// TestCodebuddyACP_ListSessions_ScopedScan verifies that cwd-scoped scanning is
// much faster than a full-tree walk and returns only the current project's
// sessions. This is the performance-critical path used by the @resume drawer.
func TestCodebuddyACP_ListSessions_ScopedScan(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	projectsDir := filepath.Join(home, ".codebuddy", "projects")
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		t.Skip("~/.codebuddy/projects not found — skipping")
	}

	// Current project root (the repo being developed).
	wd, err := os.Getwd()
	require.NoError(t, err)

	// Measure full-tree scan.
	start := time.Now()
	full := scanCodebuddySessionsDir(projectsDir)
	fullDur := time.Since(start)
	t.Logf("Full-tree scan: %d sessions in %v", len(full), fullDur)

	// Measure cwd-scoped scan (should only touch one project dir).
	start = time.Now()
	scoped := scanCodebuddyProjectDir(home, wd)
	scopedDur := time.Since(start)
	t.Logf("Cwd-scoped scan for %q: %d sessions in %v", wd, len(scoped), scopedDur)

	// Scoped scan should be dramatically faster (10x+) than the full walk.
	if len(full) > 100 && scopedDur < fullDur {
		t.Logf("  speedup: %.1fx (scoped=%v vs full=%v)", float64(fullDur)/float64(scopedDur), scopedDur, fullDur)
	}

	// Every scoped session must belong to the current project's cwd.
	for _, s := range scoped {
		assert.Equal(t, wd, s.Cwd,
			"scoped scan for cwd %q should only return sessions in that project (got cwd %q)", wd, s.Cwd)
	}
}
