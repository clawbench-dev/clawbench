package common

// SubscriberInfo is the subscriber data transferred across the interface boundary.
// Shared by all push backends (DingTalk, Feishu, etc.).
type SubscriberInfo struct {
	UserID         string
	ConversationID string // DingTalk: conversation_id; Feishu: chat_id
	UserName       string
	Source         string // "stream" (auto) or "manual" (config/panel)
}

// PushDB is the DB operations interface shared by all push backends.
// Injected from cmd/server to avoid import cycles.
type PushDB interface {
	MergeConfigSubscribers(users []string)
	GetSubscribers() ([]SubscriberInfo, error)
	UpsertSubscriber(userID, conversationID, userName, source string) error
	DeleteSubscriber(userID string) error
}

// SessionInfo carries session metadata across the interface boundary.
type SessionInfo struct {
	ID          string
	Title       string
	ProjectPath string
	Backend     string
	AgentID     string
	Model       string
}

// SessionMessenger abstracts session operations needed by push backends.
// Implemented in main.go to avoid import cycles (service → push → service).
type SessionMessenger interface {
	FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error)
	ListRecentSessions(limit int) ([]SessionInfo, error)
	IsSessionRunning(sessionID string) bool
	// SendMessageToSession sends a message to a session. It routes through the
	// unified enqueue path: running sessions get the message queued for the
	// drain loop, non-running sessions start an execution. The B2 self-heal
	// inside handles the drain-loop exit race.
	SendMessageToSession(sessionID, message string) error
}

// ConnectedClientChecker checks whether any client is currently connected.
// Injected from cmd/server to avoid import cycles with the ws package.
type ConnectedClientChecker interface {
	HasConnectedClients() bool
}
