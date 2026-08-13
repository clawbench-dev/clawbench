package codebuddy

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// On-disk ListSessions for CodeBuddy.
//
// CodeBuddy's ACP implementation does NOT support the session/list RPC
// (SessionCapabilities.List=false; calling session/list returns
// "-32601 Method not found"). However, every CodeBuddy session is persisted
// as a JSONL file on disk at ~/.codebuddy/projects/<project-slug>/<uuid>.jsonl.
// The UUID in the filename IS the ACP session ID (verified empirically), and
// each JSONL record carries the authoritative absolute cwd and ai-title.
//
// We scan these files to enumerate sessions as a fallback, mapping each to an
// acp.SessionInfo so the @resume drawer works for CodeBuddy too.
// ---------------------------------------------------------------------------

func init() {
	ai.ListSessionsFromDiskRegister("codebuddy", listCodebuddySessionsFromDisk)
}

// codebuddyDiskSession is a parsed session from a CodeBuddy JSONL file.
type codebuddyDiskSession struct {
	SessionID   string
	Cwd         string
	Title       string
	UpdatedAtMs int64
}

// codebuddyJSONLRecord is a single line in a CodeBuddy session JSONL file.
type codebuddyJSONLRecord struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Cwd       string          `json:"cwd"`
	AITitle   string          `json:"aiTitle"`
	Timestamp int64           `json:"timestamp"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
}

// listCodebuddySessionsFromDisk is the ai.ListSessionsFromDiskFn implementation
// for CodeBuddy. It scans ~/.codebuddy/projects for session JSONL files.
//
// When cwd (the current project root) is non-empty, the scan is scoped to the
// single project directory whose slug derives from cwd. This avoids walking the
// entire ~/.codebuddy/projects tree on every request — only the current
// project's sessions are returned (matching what the frontend displays, which
// filters by project root).
func listCodebuddySessionsFromDisk(agent *model.Agent, cwd string) ([]acp.SessionInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	projectsDir := filepath.Join(home, ".codebuddy", "projects")

	disks := scanCodebuddyProjectsScoped(projectsDir, cwd)
	sessions := convertDiskSessions(disks)
	slog.Info("codebuddy on-disk ListSessions", "found", len(sessions), "cwd", cwd, "scoped", cwd != "")
	return sessions, nil
}

// scanCodebuddyProjectsScoped scans the given CodeBuddy projects directory. If
// cwd is non-empty, only the project directory whose slug derives from cwd is
// scanned; otherwise the whole projects tree is walked.
func scanCodebuddyProjectsScoped(projectsDir, cwd string) []codebuddyDiskSession {
	if cwd != "" {
		projectDir := filepath.Join(projectsDir, cwdToCodebuddySlug(cwd))
		return scanCodebuddySessionsDir(projectDir)
	}
	return scanCodebuddySessionsDir(projectsDir)
}

// scanCodebuddyProjectDir scans the project directory for the given cwd under
// a CodeBuddy home directory. Used by tests to avoid touching the real home.
func scanCodebuddyProjectDir(home, cwd string) []codebuddyDiskSession {
	return scanCodebuddyProjectsScoped(filepath.Join(home, "projects"), cwd)
}

// cwdToCodebuddySlug derives CodeBuddy's project directory name from an
// absolute path. CodeBuddy stores sessions under
//
//	~/.codebuddy/projects/<slug>/<uuid>.jsonl
//
// where <slug> is the path with the leading slash stripped and each "/"
// replaced with "-" (dots are preserved). E.g.
//
//	/home/xulongzhe/projects/clawbench
//	→ home-xulongzhe-projects-clawbench
func cwdToCodebuddySlug(cwd string) string {
	trimmed := strings.TrimPrefix(cwd, "/")
	return strings.ReplaceAll(trimmed, "/", "-")
}

// convertDiskSessions maps parsed disk sessions to acp.SessionInfo values,
// producing an ISO-8601 UpdatedAt (RFC3339 UTC) that the frontend's
// new Date() can parse, and a Title pointer.
func convertDiskSessions(disks []codebuddyDiskSession) []acp.SessionInfo {
	sessions := make([]acp.SessionInfo, 0, len(disks))
	for _, d := range disks {
		title := d.Title
		var updatedAt *string
		if d.UpdatedAtMs > 0 {
			s := time.UnixMilli(d.UpdatedAtMs).UTC().Format(time.RFC3339)
			updatedAt = &s
		}
		sessions = append(sessions, acp.SessionInfo{
			SessionId: acp.SessionId(d.SessionID),
			Cwd:       d.Cwd,
			Title:     &title,
			UpdatedAt: updatedAt,
		})
	}
	return sessions
}

// scanCodebuddySessionsDir scans a CodeBuddy projects directory tree for
// session JSONL files and parses each into a codebuddyDiskSession.
//
// CodeBuddy's on-disk layout is two levels deep:
//
//	~/.codebuddy/projects/<project-slug>/<session-uuid>.jsonl
//
// so we walk recursively. Non-JSONL files, corrupt files, and files missing a
// cwd are skipped. Returns sessions sorted by most-recent-updated first.
func scanCodebuddySessionsDir(dir string) []codebuddyDiskSession {
	var sessions []codebuddyDiskSession
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		sessionID := strings.TrimSuffix(d.Name(), ".jsonl")
		s, ok := parseCodebuddyJSONL(path)
		if !ok {
			return nil
		}
		s.SessionID = sessionID
		sessions = append(sessions, s)
		return nil
	})

	sortSessionsByUpdatedAtDesc(sessions)
	return sessions
}

// parseCodebuddyJSONL reads one CodeBuddy session JSONL file and extracts the
// authoritative cwd, title (from ai-title), and latest timestamp. Returns ok=false
// if the file cannot be parsed or carries no cwd.
func parseCodebuddyJSONL(path string) (codebuddyDiskSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return codebuddyDiskSession{}, false
	}
	defer f.Close()

	var s codebuddyDiskSession
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec codebuddyJSONLRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // skip malformed records
		}
		if rec.Cwd != "" && s.Cwd == "" {
			s.Cwd = rec.Cwd
		}
		if rec.Type == "ai-title" && rec.AITitle != "" {
			s.Title = rec.AITitle
		}
		if rec.Timestamp > s.UpdatedAtMs {
			s.UpdatedAtMs = rec.Timestamp
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("codebuddy on-disk ListSessions: scan error", "path", path, "error", err)
	}
	if s.Cwd == "" {
		return codebuddyDiskSession{}, false
	}
	return s, true
}

// sortSessionsByUpdatedAtDesc sorts sessions most-recently-updated first.
// Sessions without a timestamp sort last.
func sortSessionsByUpdatedAtDesc(sessions []codebuddyDiskSession) {
	for i := 1; i < len(sessions); i++ {
		for j := i; j > 0; j-- {
			if sessions[j].UpdatedAtMs > sessions[j-1].UpdatedAtMs {
				sessions[j], sessions[j-1] = sessions[j-1], sessions[j]
			} else {
				break
			}
		}
	}
}

// scanCodebuddySessionsFromHome scans a given home directory's
// ~/codebuddy/projects layout. Used by tests to avoid touching the real home.
func scanCodebuddySessionsFromHome(home string) []codebuddyDiskSession {
	return scanCodebuddySessionsDir(filepath.Join(home, "projects"))
}
