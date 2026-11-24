package cline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// On-disk ListSessions for cline.
//
// cline's Initialize advertises no sessionCapabilities, so session/list is
// treated as absent (the method table contains it; the advertised surface does
// not). Without a scanner the @resume drawer would be empty.
//
// Unlike codebuddy (one JSONL per session under a project slug), cline keeps an
// INDEX plus per-session files:
//
//	<CLINE_DIR>/data/sessions/<sessionId>/<sessionId>.json           index record
//	<CLINE_DIR>/data/sessions/<sessionId>/<sessionId>.messages.json  transcript
//
// and `cline history --json` prints the same records as a JSON array. The CLI
// is used as the source because it owns the layout: it already knows where its
// data root is (CLINE_DIR, --data-dir, --config), and it stays correct if the
// layout moves. Verified non-interactive: `cline history --json` needs no TTY
// and prints `[]` for an empty root.
//
// The ACP session id IS the directory/record id (verified: an ACP session/new
// returned 1795532036842_3QDdZ_cli and that id appeared both in history and as
// its own directory).
// ---------------------------------------------------------------------------

// clineHistoryRecord is one entry of `cline history --json`. Only the fields the
// drawer shows are decoded; the rest (usage, checkpoint, metadata) is ignored.
type clineHistoryRecord struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt"`
}

// clineHistoryTimeout bounds the index read: the CLI loads its whole session
// index, which measured ~2.6s on a dev machine, so a hung process must not hold
// the drawer open forever.
const clineHistoryTimeout = 30 * time.Second

// clineHistory runs the real CLI; tests swap it so no binary is needed.
var clineHistory = runClineHistory

func runClineHistory(agent *model.Agent) ([]byte, error) {
	bin := "cline"
	if agent != nil {
		if cmd := strings.Fields(agent.AcpCommand); len(cmd) > 0 && cmd[0] != "" {
			bin = cmd[0]
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), clineHistoryTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "history", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("cline: `%s history --json`: %w", bin, err)
	}
	return out, nil
}

// listClineSessionsFromDisk is the ai.ListSessionsFromDiskFn implementation.
//
// cwd scopes the result to one project: cline sessions are global (not filed per
// project), so an unscoped list would show every project's conversations in
// this project's drawer.
func listClineSessionsFromDisk(agent *model.Agent, cwd string) ([]acp.SessionInfo, error) {
	raw, err := clineHistory(agent)
	if err != nil {
		return nil, err
	}
	records, err := parseClineHistory(raw)
	if err != nil {
		return nil, err
	}
	sessions := convertClineSessions(records, cwd)
	slog.Info("cline on-disk ListSessions", "found", len(sessions), "cwd", cwd)
	return sessions, nil
}

func parseClineHistory(raw []byte) ([]clineHistoryRecord, error) {
	var records []clineHistoryRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("cline: parse `history --json`: %w", err)
	}
	return records, nil
}

// convertClineSessions maps history records to acp.SessionInfo.
//
// Empty titles are dropped by the drawer anyway, but the index writes titles for
// CLI runs only; an ACP session has none until the agent names it.
func convertClineSessions(records []clineHistoryRecord, cwd string) []acp.SessionInfo {
	out := make([]acp.SessionInfo, 0, len(records))
	for _, r := range records {
		if r.SessionID == "" {
			continue
		}
		sessionCwd := r.Cwd
		if sessionCwd == "" {
			sessionCwd = cwd
		}
		if cwd != "" && !samePath(sessionCwd, cwd) {
			continue
		}
		info := acp.SessionInfo{
			SessionId: acp.SessionId(r.SessionID),
			Cwd:       sessionCwd,
		}
		if r.Title != "" {
			title := r.Title
			info.Title = &title
		}
		if r.UpdatedAt != "" {
			updated := r.UpdatedAt
			info.UpdatedAt = &updated
		}
		out = append(out, info)
	}
	return out
}

// samePath compares two absolute paths without touching the filesystem.
func samePath(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}
