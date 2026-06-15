package terminal

// ClientMessage represents a message sent from the frontend to the server.
type ClientMessage struct {
	Type string `json:"type"`           // "input", "resize", "close"
	Data string `json:"data,omitempty"` // For "input": the keystroke data
	Cols uint16 `json:"cols,omitempty"` // For "resize": terminal columns
	Rows uint16 `json:"rows,omitempty"` // For "resize": terminal rows
}

// ServerMessage represents a message sent from the server to the frontend.
type ServerMessage struct {
	Type      string `json:"type"`                // "output", "replay", "status", "exit", "error"
	SessionID string `json:"sessionId,omitempty"` // For "status": session identifier (for reconnect)
	Data      string `json:"data,omitempty"`      // For "output"/"replay": the terminal data
	Cwd       string `json:"cwd,omitempty"`       // For "status": current working directory
	Running   bool   `json:"running,omitempty"`   // For "status": whether PTY is running
	Code      int    `json:"code,omitempty"`      // For "exit": exit code
	Message   string `json:"message,omitempty"`   // For "error": error description
	ErrCode   string `json:"errcode,omitempty"`   // For "error": machine-readable error code
}

// Error codes for WebSocket error messages
const (
	ErrCodeShellFailed         = "shell_start_failed"
	ErrCodeSessionLimit        = "session_limit"
	ErrCodePlatformUnsupported = "platform_unsupported"
)

// WebSocket close codes (custom range 4000-4999 per RFC 6455)
const (
	// StatusReplaced is sent when a new WebSocket client connects and the
	// existing client is kicked. The frontend should NOT auto-reconnect
	// when it receives this close code — the session is still alive, just
	// owned by a different client now.
	StatusReplaced = 4001
)
