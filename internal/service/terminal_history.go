package service

import (
	"log/slog"
	"strconv"
	"strings"

	"clawbench/internal/store"
	"clawbench/internal/terminal"
)

// TerminalHistoryDDL creates the command-history table.
//
// One row per distinct command, not an append-only log: the panel exists to
// recall a command, so `ls` run two hundred times is one entry with a count,
// not two hundred entries that push everything else off the page. That is also
// why the UNIQUE index is on the command itself (scoped by project) and why the
// writer is an UPSERT.
//
// project_id is NULL for "global" (the same convention as terminal_quick_commands):
// commands imported from a shell's history file cannot be attributed to a
// project, while commands captured from a live session can.
const TerminalHistoryDDL = `
CREATE TABLE IF NOT EXISTS terminal_command_history (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	command      TEXT NOT NULL,
	project_id   INTEGER DEFAULT NULL,
	use_count    INTEGER NOT NULL DEFAULT 1,
	last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_terminal_history_command
	ON terminal_command_history(COALESCE(project_id, 0), command);
CREATE INDEX IF NOT EXISTS idx_terminal_history_recent
	ON terminal_command_history(last_used_at DESC);
`

const (
	// terminalHistoryMaxRows / MaxAgeDays bound the table. Recall value drops off
	// sharply with age — nobody scrolls back a year — and an unbounded history is
	// one more thing that grows without the user ever noticing.
	terminalHistoryMaxRows    = 2000
	terminalHistoryMaxAgeDays = 90

	// terminalHistoryImportLimit caps how much of a shell history file is pulled
	// in on one sync. Users with a decade of history have tens of thousands of
	// entries; the tail is the part that still has recall value.
	terminalHistoryImportLimit = 1000

	defaultTerminalHistoryLimit = 100
	maxTerminalHistoryLimit     = 500
)

// TerminalCommand is one remembered command.
type TerminalCommand struct {
	ID          int64  `json:"id"`
	Command     string `json:"command"`
	UseCount    int    `json:"use_count"`
	LastUsedAt  string `json:"last_used_at"`
	ProjectPath string `json:"project_path"`
	ProjectOnly bool   `json:"project_only"`
}

// RecordTerminalCommand remembers one command the user executed.
//
// Returns false (and no error) when the command is deliberately not stored —
// credentials, or input that is not a command at all. Callers treat that as a
// normal outcome, not a failure.
func RecordTerminalCommand(command, projectPath string) (bool, error) {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 4096 {
		return false, nil
	}
	if terminal.IsSensitiveCommand(command) {
		return false, nil
	}
	if !terminal.LooksLikeCommand(command) {
		return false, nil
	}
	if !store.DBReady() {
		return false, nil
	}

	projectID, err := store.ProjectIDForPath(projectPath)
	if err != nil {
		return false, err
	}
	// The UNIQUE index uses COALESCE(project_id, 0), so "global" must be written
	// as NULL to match rows imported from history files.
	var pid any
	if projectID != store.GlobalScopeProjectID {
		pid = projectID
	}
	if _, err := store.WriteExec(
		`INSERT INTO terminal_command_history (command, project_id) VALUES (?, ?)
		 ON CONFLICT (COALESCE(project_id, 0), command)
		 DO UPDATE SET use_count = use_count + 1, last_used_at = CURRENT_TIMESTAMP`,
		command, pid,
	); err != nil {
		return false, err
	}

	pruneTerminalHistory()
	return true, nil
}

// pruneTerminalHistory enforces the row and age caps. Failures are logged and
// ignored: pruning is housekeeping, and a full table is not a reason to fail
// the write that triggered it.
func pruneTerminalHistory() {
	if _, err := store.WriteExec(
		`DELETE FROM terminal_command_history WHERE id NOT IN (
			SELECT id FROM terminal_command_history ORDER BY last_used_at DESC, id DESC LIMIT ?
		)`, terminalHistoryMaxRows,
	); err != nil {
		slog.Warn("terminal history: prune by row cap failed", slog.String("error", err.Error()))
	}
	if _, err := store.WriteExec(
		`DELETE FROM terminal_command_history WHERE last_used_at < datetime('now', ?)`,
		"-"+strconv.Itoa(terminalHistoryMaxAgeDays)+" days",
	); err != nil {
		slog.Warn("terminal history: prune by age failed", slog.String("error", err.Error()))
	}
}

// ListTerminalCommands returns the commands visible in projectPath: those
// recorded there plus the global ones (imported from history files). Newest
// use first.
func ListTerminalCommands(projectPath, search string, limit int) ([]TerminalCommand, error) {
	if !store.DBReady() || !store.ReadDBReady() {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultTerminalHistoryLimit
	}
	if limit > maxTerminalHistoryLimit {
		limit = maxTerminalHistoryLimit
	}

	projectID, err := store.ProjectIDForPath(projectPath)
	if err != nil {
		return nil, err
	}

	query := `SELECT h.id, h.command, h.use_count, h.last_used_at, COALESCE(p.path, '')
		FROM terminal_command_history h
		LEFT JOIN projects p ON p.id = h.project_id
		WHERE (h.project_id IS NULL`
	args := []any{}
	if projectID != store.GlobalScopeProjectID {
		query += " OR h.project_id = ?"
		args = append(args, projectID)
	}
	// Closing the OR group is mandatory: without it the search below binds only
	// to the second branch and global rows escape the filter.
	query += ")"
	if s := strings.TrimSpace(search); s != "" {
		query += ` AND h.command LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(s)+"%")
	}
	query += " ORDER BY h.last_used_at DESC, h.id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []TerminalCommand{}
	for rows.Next() {
		var c TerminalCommand
		if err := rows.Scan(&c.ID, &c.Command, &c.UseCount, &c.LastUsedAt, &c.ProjectPath); err != nil {
			return nil, err
		}
		c.ProjectOnly = c.ProjectPath != ""
		out = append(out, c)
	}
	return out, rows.Err()
}

// escapeLike escapes the LIKE wildcards so a search for "50%" does not match
// everything.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// DeleteTerminalCommand removes one entry. Used to prune the noise that
// inevitably slips past the heuristics (a line typed inside a program on a
// platform without the foreground-process probe).
func DeleteTerminalCommand(id int64) error {
	if !store.DBReady() {
		return nil
	}
	_, err := store.WriteExec("DELETE FROM terminal_command_history WHERE id = ?", id)
	return err
}

// ClearTerminalHistory removes every remembered command.
func ClearTerminalHistory() error {
	if !store.DBReady() {
		return nil
	}
	_, err := store.WriteExec("DELETE FROM terminal_command_history")
	return err
}

// SyncTerminalHistory imports commands read from the local shells' history
// files. The caller does the file reading (internal/terminal.ReadShellHistory)
// so this function stays a pure DB operation and can be tested without touching
// the real home directory.
//
// Existing rows are left untouched (ON CONFLICT DO NOTHING, not the count-
// incrementing UPSERT used for live capture): importing the same file twice
// must not inflate use_count or reorder the list. Returns how many new rows
// were added.
func SyncTerminalHistory(commands []string) (int, error) {
	if !store.DBReady() || len(commands) == 0 {
		return 0, nil
	}
	// Only the tail has recall value; files are oldest-first.
	if len(commands) > terminalHistoryImportLimit {
		commands = commands[len(commands)-terminalHistoryImportLimit:]
	}

	seen := make(map[string]bool, len(commands))
	added := 0
	for _, cmd := range commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" || len(cmd) > 4096 || seen[cmd] {
			continue
		}
		if terminal.IsSensitiveCommand(cmd) || !terminal.LooksLikeCommand(cmd) {
			continue
		}
		seen[cmd] = true

		res, err := store.WriteExec(
			`INSERT INTO terminal_command_history (command, project_id) VALUES (?, NULL)
			 ON CONFLICT (COALESCE(project_id, 0), command) DO NOTHING`, cmd,
		)
		if err != nil {
			slog.Warn("terminal history: import row failed", slog.String("error", err.Error()))
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	if added > 0 {
		pruneTerminalHistory()
	}
	return added, nil
}
