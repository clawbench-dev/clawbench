package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

const (
	codexSessionScanLimit   = 10000
	codexSessionResultLimit = 200
	codexSessionHeaderLimit = 256 * 1024
	codexSessionWarnLimit   = 5
)

var windowsDrivePath = regexp.MustCompile(`^[A-Za-z]:/`)

type codexDiskSession struct {
	SessionID string
	Cwd       string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type codexRolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
}

func init() {
	ai.ListSessionsFromDiskRegister("codex", listCodexSessionsFromDisk)
}

func listCodexSessionsFromDisk(_ *model.Agent, cwd string) ([]acp.SessionInfo, error) {
	if strings.TrimSpace(cwd) == "" {
		slog.Warn("codex on-disk ListSessions: project cwd is empty; skipping scan")
		return nil, nil
	}
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil, err
	}

	disks, stats := scanCodexSessions(filepath.Join(codexHome, "sessions"), cwd, codexSessionScanLimit, codexSessionResultLimit)
	sessions := make([]acp.SessionInfo, 0, len(disks))
	for _, disk := range disks {
		updatedAt := formatCodexSessionTime(disk.UpdatedAt)
		sessions = append(sessions, acp.SessionInfo{
			SessionId: acp.SessionId(disk.SessionID),
			Cwd:       disk.Cwd,
			UpdatedAt: updatedAt,
		})
	}

	slog.Info("codex on-disk ListSessions",
		"found", len(sessions),
		"scanned", stats.scanned,
		"skipped", stats.skipped,
		"scan_limit_reached", stats.limitReached,
		"project_scoped", true)
	return sessions, nil
}

func resolveCodexHome() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); configured != "" {
		return filepath.Clean(configured), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve Codex home: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

type codexScanStats struct {
	scanned      int
	skipped      int
	limitReached bool
}

func scanCodexSessions(root, cwd string, scanLimit, resultLimit int) ([]codexDiskSession, codexScanStats) {
	if scanLimit <= 0 || resultLimit <= 0 {
		return nil, codexScanStats{}
	}

	var sessions []codexDiskSession
	stats := codexScanStats{}
	walkCodexSessionFilesNewestFirst(root, &stats, scanLimit, func(filePath string, info os.FileInfo) bool {
		session, err := parseCodexSessionHeader(filePath, info)
		if err != nil {
			stats.skipped++
			if stats.skipped <= codexSessionWarnLimit {
				slog.Warn("codex on-disk ListSessions: skipping invalid rollout",
					"file", filepath.Base(filePath),
					"error", err)
			}
			return true
		}
		if cwd != "" && !codexProjectPathsEqual(session.Cwd, cwd) {
			return true
		}
		sessions = append(sessions, session)
		return true
	})

	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
		}
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	if len(sessions) > resultLimit {
		sessions = sessions[:resultLimit]
	}
	return sessions, stats
}

func walkCodexSessionFilesNewestFirst(root string, stats *codexScanStats, scanLimit int, visit func(string, os.FileInfo) bool) {
	var walk func(string) bool
	walk = func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return true
		}
		for i := len(entries) - 1; i >= 0; i-- {
			entry := entries[i]
			entryPath := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if !walk(entryPath) {
					return false
				}
				continue
			}
			if !strings.HasPrefix(entry.Name(), "rollout-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			if stats.scanned >= scanLimit {
				stats.limitReached = true
				return false
			}
			stats.scanned++
			info, err := entry.Info()
			if err != nil {
				stats.skipped++
				continue
			}
			if !visit(entryPath, info) {
				return false
			}
		}
		return true
	}
	_ = walk(root)
}

func parseCodexSessionHeader(filePath string, info os.FileInfo) (codexDiskSession, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return codexDiskSession{}, err
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(io.LimitReader(f, codexSessionHeaderLimit))
	for {
		line, readErr := reader.ReadBytes('\n')
		if strings.TrimSpace(string(line)) != "" {
			var rollout codexRolloutLine
			if err := json.Unmarshal(line, &rollout); err != nil {
				return codexDiskSession{}, fmt.Errorf("decode rollout header: %w", err)
			}
			if rollout.Type != "session_meta" {
				return codexDiskSession{}, fmt.Errorf("first record type is %q", rollout.Type)
			}
			var meta codexSessionMeta
			if err := json.Unmarshal(rollout.Payload, &meta); err != nil {
				return codexDiskSession{}, fmt.Errorf("decode session metadata: %w", err)
			}
			sessionID := meta.ID
			if sessionID == "" {
				sessionID = meta.SessionID
			}
			if sessionID == "" || meta.Cwd == "" {
				return codexDiskSession{}, errors.New("session metadata missing id or cwd")
			}
			createdAt := parseCodexSessionTime(meta.Timestamp)
			if createdAt.IsZero() {
				createdAt = parseCodexSessionTime(rollout.Timestamp)
			}
			return codexDiskSession{
				SessionID: sessionID,
				Cwd:       meta.Cwd,
				CreatedAt: createdAt,
				UpdatedAt: info.ModTime(),
			}, nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return codexDiskSession{}, errors.New("session metadata not found in bounded header")
			}
			return codexDiskSession{}, readErr
		}
	}
}

func parseCodexSessionTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse("2006-01-02T15-04-05", value); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func formatCodexSessionTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func codexProjectPathsEqual(left, right string) bool {
	left = normalizeCodexProjectPath(left)
	right = normalizeCodexProjectPath(right)
	return left == right
}

func normalizeCodexProjectPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" {
		return ""
	}
	isUNC := strings.HasPrefix(value, "//")
	isWindows := isUNC || windowsDrivePath.MatchString(value)
	cleaned := path.Clean(value)
	if cleaned == "." {
		return ""
	}
	if isUNC {
		cleaned = "//" + strings.TrimLeft(cleaned, "/")
	}
	cleaned = strings.TrimSuffix(cleaned, "/")
	if isWindows {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned
}
