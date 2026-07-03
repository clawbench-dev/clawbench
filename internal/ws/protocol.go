package ws

// MessageTypeEvent is the type field for event messages sent from server to client.
const MessageTypeEvent = "event"

// StatusPermissionPending indicates a session is waiting for user approval.
const StatusPermissionPending = "permission_pending"

// ServerMessage is a message sent from server to client.
type ServerMessage struct {
	Type  string `json:"type"`            // "event", "ping"
	ID    string `json:"id,omitempty"`    // event ID for ack (e.g., "evt_1706000000_1")
	Event string `json:"event,omitempty"` // "session_update", "task_update", "queue_update", "summary_update"
	Data  any    `json:"data,omitempty"`
}

// ClientMessage is a message sent from client to server.
type ClientMessage struct {
	Type string `json:"type"`         // "ack", "pong"
	ID   string `json:"id,omitempty"` // ack target event ID
}

// SessionUpdateData is the data payload for "session_update" events.
type SessionUpdateData struct {
	SessionID       string `json:"session_id"`
	Status          string `json:"status"` // "running", "completed", "cancelled", "permission_pending", "permission_resolved"
	HasNewMessages  bool   `json:"has_new_messages"`
	ResponsePreview string `json:"response_preview,omitempty"` // preview of AI's final reply (completed only)
	SessionTitle    string `json:"session_title,omitempty"`    // session title for push notification
	ProjectPath     string `json:"project_path,omitempty"`
	ToolName        string `json:"tool_name,omitempty"` // tool name requesting approval (permission_pending only)
}

// TaskUpdateData is the data payload for "task_update" events.
type TaskUpdateData struct {
	TaskID          string `json:"task_id"`
	Status          string `json:"status"` // "running", "completed", "failed"
	ExecutionID     string `json:"execution_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	ProjectPath     string `json:"project_path,omitempty"`
	SessionTitle    string `json:"session_title,omitempty"`    // task name for push notification
	ResponsePreview string `json:"response_preview,omitempty"` // preview of AI's final reply (completed only)
}

// QueueUpdateData is the data payload for "queue_update" events.
type QueueUpdateData struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}

// SummaryUpdateData is the data payload for "summary_update" events.
type SummaryUpdateData struct {
	TargetType  string `json:"targetType"` // "chat_message" (legacy: "task_execution" may exist from older versions)
	TargetID    int64  `json:"targetID"`   // chat_history.id
	Summary     string `json:"summary"`    // empty = too short, non-empty = summary content
	ProjectPath string `json:"projectPath,omitempty"`
	SessionID   string `json:"sessionID,omitempty"`
}
