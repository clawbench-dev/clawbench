package model

import "time"

// ResponsePreviewMaxRunes is the maximum number of runes included in the
// response preview sent via WS session_update events and JPush notifications.
const ResponsePreviewMaxRunes = 512

// ChatMessage represents a single message in the chat history
type ChatMessage struct {
	ID          int64     `json:"id,omitempty"`
	Role        string    `json:"role"`
	Content     string    `json:"content"`
	Files       []string  `json:"files,omitempty"`
	SessionID   string    `json:"sessionId,omitempty"`
	Backend     string    `json:"backend,omitempty"`
	ProjectPath string    `json:"projectPath,omitempty"`
	Streaming   bool      `json:"streaming,omitempty"`
	Indexed     bool      `json:"indexed,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	Summary     *string   `json:"summary,omitempty"` // reading summary (nil=not summarized, ""=too short, non-empty=summary)
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
	Text      string   `json:"text"`
	FilePaths []string `json:"filePaths"`
	Files     []string `json:"files"`
	CreatedAt string   `json:"createdAt"`
}

// ContentBlock represents a typed block within an assistant message's content.
// Stored as JSON in the chat_history.content column.
type ContentBlock struct {
	Type   string         `json:"type"`             // "thinking", "tool_use", "text", "warning", "error"
	Text   string         `json:"text,omitempty"`   // thinking, text, or warning/error content
	Reason string         `json:"reason,omitempty"` // structured reason code for i18n (e.g. "disconnect", "timeout", "parse_error")
	Name   string         `json:"name,omitempty"`   // tool name (tool_use)
	ID     string         `json:"id,omitempty"`     // tool call ID (tool_use)
	Input  map[string]any `json:"input"`            // tool input (tool_use) — no omitempty: must serialize {} so frontend distinguishes "no data" from "empty input"
	Output string         `json:"output,omitempty"` // tool execution output text (tool_use)
	Status string         `json:"status,omitempty"` // tool execution status: "success", "error" (tool_use)
	Done   bool           `json:"done"`             // tool_use input complete (tool_use) — no omitempty: done=false must round-trip through DB
}
