package ws

import "clawbench/internal/model"

// MessageTypeEvent is the type field for event messages sent from server to client.
const MessageTypeEvent = "event"

// StatusPermissionPending indicates a session is waiting for user approval.
const StatusPermissionPending = "permission_pending"

// ServerMessage is a message sent from server to client.
type ServerMessage struct {
	Type  string `json:"type"`            // "event", "ping"
	ID    string `json:"id,omitempty"`    // event ID for ack (e.g., "evt_1706000000_1")
	Event string `json:"event,omitempty"` // "session_update", "task_update", "summary_update"
	Data  any    `json:"data,omitempty"`
}

// ClientMessage is a message sent from client to server.
type ClientMessage struct {
	Type       string `json:"type"`                   // "ack", "pong", "subscribe", "unsubscribe", "cancel", "permission_respond"
	ID         string `json:"id,omitempty"`           // ack target event ID
	SessionID  string `json:"session_id,omitempty"`   // for subscribe/unsubscribe/cancel
	ToolCallID string `json:"tool_call_id,omitempty"` // for permission_respond
	OptionID   string `json:"option_id,omitempty"`    // for permission_respond
	Cancelled  bool   `json:"cancelled,omitempty"`    // for permission_respond
}

// SessionUpdateData is the data payload for "session_update" events.
type SessionUpdateData struct {
	SessionID            string `json:"session_id"`
	Status               string `json:"status"` // "running", "completed", "cancelled", "permission_pending", "permission_resolved"
	HasNewMessages       bool   `json:"has_new_messages"`
	ResponsePreview      string `json:"response_preview,omitempty"`       // preview of AI's final reply with Markdown (for DingTalk)
	ResponsePreviewPlain string `json:"response_preview_plain,omitempty"` // Markdown-stripped preview (for Android/browser notifications)
	SessionTitle         string `json:"session_title,omitempty"`
	ProjectPath          string `json:"project_path,omitempty"`
	ToolName             string `json:"tool_name,omitempty"`  // tool name requesting approval (permission_pending only)
	ToolInput            string `json:"tool_input,omitempty"` // tool input JSON for approval details (permission_pending only)
}

// TaskUpdateData is the data payload for "task_update" events.
type TaskUpdateData struct {
	TaskID               string `json:"task_id"`
	Status               string `json:"status"` // "running", "completed", "failed"
	ExecutionID          string `json:"execution_id,omitempty"`
	SessionID            string `json:"session_id,omitempty"`
	ProjectPath          string `json:"project_path,omitempty"`
	SessionTitle         string `json:"session_title,omitempty"`          // task name for push notification
	ResponsePreview      string `json:"response_preview,omitempty"`       // preview with Markdown (for DingTalk)
	ResponsePreviewPlain string `json:"response_preview_plain,omitempty"` // Markdown-stripped preview (for Android/browser notifications)
}

// ChatStreamData wraps a chat streaming event for WS delivery.
type ChatStreamData struct {
	SessionID string `json:"session_id"`
	EventType string `json:"event_type"` // "content", "thinking", "tool_use", etc.
	Payload   any    `json:"payload"`
}

// SummaryUpdateData is the data payload for "summary_update" events.
type SummaryUpdateData struct {
	TargetType   string              `json:"targetType"` // "chat_message" (legacy: "task_execution" may exist from older versions)
	TargetID     int64               `json:"targetID"`   // chat_history.id
	Summary      string              `json:"summary"`    // empty = too short, non-empty = summary content
	SummaryCards *model.SummaryCards `json:"summaryCards,omitempty"`
	ProjectPath  string              `json:"projectPath,omitempty"`
	SessionID    string              `json:"sessionID,omitempty"`
}

// ChatRecommendationData is the data payload for "chat_recommendation" events.
type ChatRecommendationData struct {
	SessionID      string `json:"session_id"`
	ProjectPath    string `json:"project_path,omitempty"`
	Recommendation string `json:"recommendation"` // concise next-step suggestion to auto-fill / show
}
