package model

import (
	"encoding/json"
	"strings"
	"time"
)

// ResponsePreviewMaxRunes is the maximum number of runes included in the
// response_preview field of WS session_update/task_update events.
const ResponsePreviewMaxRunes = 512

// PushPreviewMaxRunes is the maximum number of runes for push notification
// previews (browser, Android, DingTalk). Shorter than WS preview for readability.
const PushPreviewMaxRunes = 200

// FileEntry represents a file or directory attachment with metadata.
type FileEntry struct {
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
}

// FileEntriesFromPaths creates []FileEntry from plain paths with isDir=false.
// Used for backward-compatible construction when isDir is unknown.
func FileEntriesFromPaths(paths []string) []FileEntry {
	if len(paths) == 0 {
		return nil
	}
	entries := make([]FileEntry, len(paths))
	for i, p := range paths {
		entries[i] = FileEntry{Path: p}
	}
	return entries
}

// PathsFromFileEntries extracts plain paths from []FileEntry.
// Deprecated: Use fileEntryLabel (in handler) instead to preserve line info.
// This function strips StartLine/EndLine, which causes prompt prefix regression.
func PathsFromFileEntries(entries []FileEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	return paths
}

// ChatMessage represents a single message in the chat history
type ChatMessage struct {
	ID          int64       `json:"id,omitempty"`
	Role        string      `json:"role"`
	Content     string      `json:"content"`
	Files       []FileEntry `json:"files,omitempty"`
	SessionID   string      `json:"sessionId,omitempty"`
	Backend     string      `json:"backend,omitempty"`
	ProjectPath string      `json:"projectPath,omitempty"`
	Streaming   bool        `json:"streaming,omitempty"`
	Indexed     bool        `json:"indexed,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	Summary     *string     `json:"summary,omitempty"` // reading summary (nil=not summarized, ""=too short, non-empty=summary)
}

// UnmarshalJSON implements custom deserialization for ChatMessage.
// Handles backward compatibility: old-format files column stored as
// ["path1","path2"] (array of strings) is automatically migrated to
// the new format [{"path":"path1","isDir":false}].
func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type Alias ChatMessage
	aux := &struct {
		Files json.RawMessage `json:"files,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(m),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.Files) == 0 {
		return nil
	}
	// Try new format: []FileEntry
	var entries []FileEntry
	if err := json.Unmarshal(aux.Files, &entries); err == nil {
		m.Files = entries
		return nil
	}
	// Fallback: old format []string
	var paths []string
	if err := json.Unmarshal(aux.Files, &paths); err != nil {
		return err
	}
	m.Files = FileEntriesFromPaths(paths)
	return nil
}

// ChatSession represents a chat session
type ChatSession struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Backend         string     `json:"backend"`
	AgentID         string     `json:"agentId,omitempty"`
	AgentSource     string     `json:"agentSource,omitempty"`
	Model           string     `json:"model,omitempty"`
	SessionType     string     `json:"sessionType,omitempty"`     // "chat" | "scheduled"
	SourceSessionID string     `json:"sourceSessionId,omitempty"` // non-empty = continued from scheduled task
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	Running         bool       `json:"running,omitempty"`
	UnreadCount     int        `json:"unreadCount,omitempty"`
	PendingApproval bool       `json:"pendingApproval,omitempty"` // ACP permission request awaiting user response
	LastReadAt      *time.Time `json:"-"`
}

// QueuedMessage represents a message waiting in the pending queue for a session.
// Stored in-memory only (not persisted to DB).
type QueuedMessage struct {
	QueueID   string      `json:"queueId"` // Frontend-generated unique ID for matching
	Text      string      `json:"text"`
	FilePaths []string    `json:"filePaths"`
	Files     []FileEntry `json:"files"`
	CreatedAt string      `json:"createdAt"`
}

// ContentBlock represents a typed block within an assistant message's content.
// Stored as JSON in the chat_history.content column.
type ContentBlock struct {
	Type        string         `json:"type"`                   // "thinking", "tool_use", "text", "warning", "error"
	Text        string         `json:"text,omitempty"`         // thinking, text, or warning/error content
	Reason      string         `json:"reason,omitempty"`       // structured reason code for i18n (e.g. "disconnect", "timeout", "parse_error")
	Name        string         `json:"name,omitempty"`         // tool name (tool_use)
	ID          string         `json:"id,omitempty"`           // tool call ID (tool_use)
	Input       map[string]any `json:"input"`                  // tool input (tool_use) — no omitempty: must serialize {} so frontend distinguishes "no data" from "empty input"
	Output      string         `json:"output,omitempty"`       // tool execution output text (tool_use)
	Status      string         `json:"status,omitempty"`       // tool execution status: "success", "error" (tool_use)
	Done        bool           `json:"done"`                   // tool_use input complete (tool_use) — no omitempty: done=false must round-trip through DB
	Summary     string         `json:"summary,omitempty"`      // extracted display summary (tool_use) — redundant, avoids loading input for toolbar
	DisplayName string         `json:"display_name,omitempty"` // subagent_type for Agent tools (tool_use) — redundant, replaces toolDisplayName() lookup
	FilePath    string         `json:"file_path,omitempty"`    // detected file path (tool_use) — redundant, for FILE_MODIFYING_TOOLS detection
}

// MarshalJSON implements custom serialization for ContentBlock.
// For tool_use blocks, only slim fields are serialized (no input/output),
// which are stored separately in the chat_tool_calls table.
// Exception: interactive tools (AskUserQuestion, PermissionApproval) include
// input inline because they need it for immediate card rendering and their
// input is not stored in chat_tool_calls when created by ConvertAskQuestionBlocks.
// For other block types, standard serialization is used.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if b.Type == "tool_use" {
		nameLower := strings.ToLower(b.Name)
		isInteractive := nameLower == "askuserquestion" || nameLower == "permissionapproval"
		if isInteractive {
			// Interactive tools: include input for immediate frontend rendering
			type InteractiveBlock struct {
				Type        string         `json:"type"`
				Name        string         `json:"name,omitempty"`
				ID          string         `json:"id,omitempty"`
				Input       map[string]any `json:"input"`
				Output      string         `json:"output,omitempty"`
				Status      string         `json:"status,omitempty"`
				Done        bool           `json:"done"`
				Summary     string         `json:"summary,omitempty"`
				DisplayName string         `json:"display_name,omitempty"`
				FilePath    string         `json:"file_path,omitempty"`
			}
			return json.Marshal(InteractiveBlock{
				Type:        b.Type,
				Name:        b.Name,
				ID:          b.ID,
				Input:       b.Input,
				Output:      b.Output,
				Status:      b.Status,
				Done:        b.Done,
				Summary:     b.Summary,
				DisplayName: b.DisplayName,
				FilePath:    b.FilePath,
			})
		}
		// Slim serialization: type+name+id+status+done+summary+display_name+file_path
		type SlimBlock struct {
			Type        string `json:"type"`
			Name        string `json:"name,omitempty"`
			ID          string `json:"id,omitempty"`
			Status      string `json:"status,omitempty"`
			Done        bool   `json:"done"`
			Summary     string `json:"summary,omitempty"`
			DisplayName string `json:"display_name,omitempty"`
			FilePath    string `json:"file_path,omitempty"`
		}
		return json.Marshal(SlimBlock{
			Type:        b.Type,
			Name:        b.Name,
			ID:          b.ID,
			Status:      b.Status,
			Done:        b.Done,
			Summary:     b.Summary,
			DisplayName: b.DisplayName,
			FilePath:    b.FilePath,
		})
	}
	// Standard serialization using Alias to avoid infinite recursion
	type Alias ContentBlock
	return json.Marshal(Alias(b))
}
