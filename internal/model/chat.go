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
//
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
	ID           int64         `json:"id,omitempty"`
	Role         string        `json:"role"`
	Content      string        `json:"content"`
	Files        []FileEntry   `json:"files,omitempty"`
	SessionID    string        `json:"sessionId,omitempty"`
	Backend      string        `json:"backend,omitempty"`
	ProjectPath  string        `json:"projectPath,omitempty"`
	Streaming    bool          `json:"streaming,omitempty"`
	Indexed      bool          `json:"indexed,omitempty"`
	QueueID      string        `json:"queueId,omitempty"` // frontend-generated queueId for optimistic bubble matching
	Queued       bool          `json:"queued,omitempty"`  // true while the message waits for the drain loop
	CreatedAt    time.Time     `json:"createdAt"`
	Summary      *string       `json:"summary,omitempty"`      // reading summary (nil=not summarized, ""=too short, non-empty=summary)
	SummaryCards *SummaryCards `json:"summaryCards,omitempty"` // structured card metadata for summary view
}

// SummaryTool is a compact record of a tool_use block present in a reading
// summary view. Only answerable interactive tools (AskUserQuestion) are
// captured: input is needed for card rendering, and done/status/output carry
// the final state. Actionable-only tools whose buttons need live session
// context (PermissionApproval) are intentionally excluded from summary view.
type SummaryTool struct {
	Name   string         `json:"name"`
	ID     string         `json:"id,omitempty"`
	Input  map[string]any `json:"input,omitempty"`
	Done   bool           `json:"done,omitempty"`
	Status string         `json:"status,omitempty"`
	Output string         `json:"output,omitempty"`
}

// AskQuestionOption is a single option in an ask-question card.
type AskQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskQuestionCard is a single question item in an <ask-question> block,
// shaped to match the frontend AskItem used by renderAskUserQuestion
// (web/src/utils/xmlParser.ts). A frontend caller renders it via
// formatToolInput({ questions: summaryCards.askQuestions }, 'AskUserQuestion').
type AskQuestionCard struct {
	Header      string              `json:"header"`
	MultiSelect bool                `json:"multiSelect"`
	Question    string              `json:"question"`
	Options     []AskQuestionOption `json:"options"`
}

// SummaryFileChange ties a created/modified file path to the Write/Edit tool
// call IDs that produced it. ToolIDs let the frontend fetch the diff content
// on demand (GET /api/ai/chat/tool-call) in summary-only view, where content
// blocks are slim and carry no input/output.
type SummaryFileChange struct {
	Path    string   `json:"path"`
	ToolIDs []string `json:"toolIDs,omitempty"`
}

// SummaryFileChanges is a []SummaryFileChange that also decodes the legacy
// []string format (plain paths) stored by older versions.
type SummaryFileChanges []SummaryFileChange

// SummaryWarning is a compact record of a warning/error content block present
// in a message. Carried in summaryCards so the summary view can still surface
// error banners (e.g. "Server restarted, AI response interrupted") when the
// heavy content was stripped by summarizeContentForView. Mirrors the block
// fields the frontend banner renderer reads.
type SummaryWarning struct {
	Type        string `json:"type"`                   // "warning" | "error"
	Text        string `json:"text,omitempty"`         // display text / fallback
	Reason      string `json:"reason,omitempty"`       // structured reason for i18n (e.g. "restart")
	ErrorCode   int    `json:"error_code,omitempty"`   // structured error code (e.g. ACP JSON-RPC -32603)
	HTTPStatus  int    `json:"http_status,omitempty"`  // upstream HTTP status when available (e.g. 500)
	ErrorSource string `json:"error_source,omitempty"` // "agent" | "clawbench" | "network"
}

// UnmarshalJSON accepts both the current object format
// ([{"path":...,"toolIDs":[...]}]) and the legacy plain-path format
// (["path1","path2"]).
func (s *SummaryFileChanges) UnmarshalJSON(data []byte) error {
	var objs []SummaryFileChange
	if err := json.Unmarshal(data, &objs); err == nil {
		*s = objs
		return nil
	}
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return err
	}
	out := make(SummaryFileChanges, 0, len(paths))
	for _, p := range paths {
		out = append(out, SummaryFileChange{Path: p})
	}
	*s = out
	return nil
}

// SummaryCards holds the structured card metadata persisted alongside the
// reading summary text. Tools are auto-expand tool_use blocks; TaskIDs are the
// scheduled-task IDs referenced by <scheduled-task> tags; AskQuestions are
// <ask-question> XML cards. Populated at summarization time and stored in the
// summaries.summary_cards column.
type SummaryCards struct {
	Tools        []SummaryTool     `json:"tools,omitempty"`
	TaskIDs      []int64           `json:"taskIDs,omitempty"`
	AskQuestions []AskQuestionCard `json:"askQuestions,omitempty"`
	// CreatedFiles / ModifiedFiles hold the file paths written (Write) or edited
	// (Edit) by the message, plus the tool call IDs. They restore the
	// file-changes banner in summary-only view (where full content blocks are
	// omitted) and enable on-demand diff fetching via the tool call IDs.
	CreatedFiles  SummaryFileChanges `json:"createdFiles,omitempty"`
	ModifiedFiles SummaryFileChanges `json:"modifiedFiles,omitempty"`
	// Warnings carry the warning/error blocks of a message so the summary view
	// (where content blocks are stripped) still renders error banners — the
	// same role CreatedFiles/ModifiedFiles play for the file-changes banner.
	Warnings []SummaryWarning `json:"warnings,omitempty"`
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
	Pinned          bool       `json:"pinned,omitempty"`           // pinned to top of session list
	PendingApproval bool       `json:"pendingApproval,omitempty"` // ACP permission request awaiting user response
	LastReadAt      *time.Time `json:"-"`
	ProjectPath     string     `json:"projectPath,omitempty"` // project this session belongs to (overview grouping)
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

// ImageAttachment carries an inline image for a multimodal ACP prompt.
// Either Path (local file path, read at prompt-build time) or Data
// (base64-encoded image bytes) may be set. MimeType is required and
// describes the image type (e.g. "image/png").
type ImageAttachment struct {
	Path     string `json:"path,omitempty"` // local file path; backend reads it into Data
	Data     string `json:"data,omitempty"` // base64-encoded image bytes
	MimeType string `json:"mimeType"`
	URI      string `json:"uri,omitempty"` // optional source reference
}

// ContentBlock represents a typed block within an assistant message's content.
// Stored as JSON in the chat_history.content column.
type ContentBlock struct {
	Type        string         `json:"type"`                   // "thinking", "tool_use", "text", "warning", "error"
	Text        string         `json:"text,omitempty"`         // thinking, text, or warning/error content
	ThinkID     string         `json:"think_id,omitempty"`     // thinking: stable ID for chat_thinking upsert (full text lives there; content keeps this slim marker)
	Reason      string         `json:"reason,omitempty"`       // structured reason code for i18n (e.g. "disconnect", "timeout", "parse_error")
	ErrorCode   int            `json:"error_code,omitempty"`   // structured error code (e.g. ACP JSON-RPC code -32603)
	HTTPStatus  int            `json:"http_status,omitempty"`  // upstream HTTP status when available (e.g. 500)
	ErrorSource string         `json:"error_source,omitempty"` // "agent" | "clawbench" | "network"
	Name        string         `json:"name,omitempty"`         // tool name (tool_use)
	ID          string         `json:"id,omitempty"`           // tool call ID (tool_use)
	Input       map[string]any `json:"input"`                  // tool input (tool_use) — no omitempty: must serialize {} so frontend distinguishes "no data" from "empty input"
	Output      string         `json:"output,omitempty"`       // tool execution output text (tool_use)
	Status      string         `json:"status,omitempty"`       // tool execution status: "success", "error" (tool_use)
	Done        bool           `json:"done"`                   // tool_use input complete (tool_use) — no omitempty: done=false must round-trip through DB
	Summary     string         `json:"summary,omitempty"`      // extracted display summary (tool_use) — redundant, avoids loading input for toolbar
	DisplayName string         `json:"display_name,omitempty"` // subagent_type for Agent tools (tool_use) — redundant, replaces toolDisplayName() lookup
	FilePath    string         `json:"file_path,omitempty"`    // detected file path (tool_use) — redundant, for FILE_MODIFYING_TOOLS detection
	DurationMs  int            `json:"duration_ms,omitempty"`  // tool execution wall-clock duration in ms (tool_use)
	// ParentToolCallID links sub-agent content to the Agent tool call that
	// spawned it: set on thinking/text/tool_use blocks produced by a sub-agent,
	// empty for top-level content. Extracted from the backend's ACP _meta
	// parent-link key (see internal/ai/acp_parent_link.go). Used by the frontend
	// to group a sub-agent's output under its parent Agent card.
	ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
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
				DurationMs  int            `json:"duration_ms,omitempty"`
				// ParentToolCallID must round-trip for sub-agent grouping on reload.
				ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
			}
			return json.Marshal(InteractiveBlock{
				Type:             b.Type,
				Name:             b.Name,
				ID:               b.ID,
				Input:            b.Input,
				Output:           b.Output,
				Status:           b.Status,
				Done:             b.Done,
				Summary:          b.Summary,
				DisplayName:      b.DisplayName,
				FilePath:         b.FilePath,
				DurationMs:       b.DurationMs,
				ParentToolCallID: b.ParentToolCallID,
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
			DurationMs  int    `json:"duration_ms,omitempty"`
			// ParentToolCallID must round-trip for sub-agent grouping on reload.
			ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
		}
		return json.Marshal(SlimBlock{
			Type:             b.Type,
			Name:             b.Name,
			ID:               b.ID,
			Status:           b.Status,
			Done:             b.Done,
			Summary:          b.Summary,
			DisplayName:      b.DisplayName,
			FilePath:         b.FilePath,
			DurationMs:       b.DurationMs,
			ParentToolCallID: b.ParentToolCallID,
		})
	}
	// Standard serialization using Alias to avoid infinite recursion
	type Alias ContentBlock
	return json.Marshal(Alias(b))
}
