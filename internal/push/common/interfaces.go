package common

import "clawbench/internal/model"

// SubscriberInfo is the subscriber data transferred across the interface boundary.
// Shared by all push backends (DingTalk, Feishu, WeChat).
type SubscriberInfo struct {
	UserID         string
	ConversationID string // DingTalk: conversation_id; Feishu: chat_id
	UserName       string
	Source         string // "stream" (auto) or "manual" (config/panel)
	// ContextToken is WeChat's per-user reply token. Always empty for
	// DingTalk/Feishu. WeChat cannot send proactively without one — the
	// platform mints it on an inbound message — so a subscriber with an empty
	// token is unreachable and the WeChat backend skips it.
	ContextToken string
}

// PushDB is the DB operations interface shared by all push backends.
// Injected from cmd/server to avoid import cycles.
type PushDB interface {
	MergeConfigSubscribers(users []string)
	GetSubscribers() ([]SubscriberInfo, error)
	UpsertSubscriber(userID, conversationID, userName, source string) error
	DeleteSubscriber(userID string) error

	// GetLastSessionID / SetLastSessionID implement the per-user sticky target:
	// a message carrying no "@{shortID}" prefix goes to the session this user
	// last addressed. GetLastSessionID returns "" when nothing was recorded
	// (never addressed a session, or a pre-migration row) — the caller must
	// then ask the user to pick via "/ls" rather than choose for them.
	GetLastSessionID(userID string) (string, error)
	SetLastSessionID(userID, sessionID string) error
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

// ProjectRef is one row of the /new project list. Index is 1-based and is what
// the user types back; Path is the canonical registered path.
//
// The list order is stable across a create (see service.ListProjectsForPush), so
// an index a user read stays valid for their next command.
type ProjectRef struct {
	Index  int
	Path   string
	Exists bool // directory still present on disk
}

// SessionMessenger abstracts session operations needed by push backends.
// Implemented in main.go to avoid import cycles (service → push → service).
type SessionMessenger interface {
	FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error)
	ListRecentSessions(limit int) ([]SessionInfo, error)
	IsSessionRunning(sessionID string) bool
	// GetSessionInfo returns metadata for one session, or an error when it does
	// not exist. Push backends need ProjectPath to place a downloaded IM
	// attachment in the session's own .clawbench/uploads/ directory.
	GetSessionInfo(sessionID string) (SessionInfo, error)
	// SendMessageToSession sends a message to a session. It routes through the
	// unified enqueue path: running sessions get the message queued for the
	// drain loop, non-running sessions start an execution. The B2 self-heal
	// inside handles the drain-loop exit race.
	//
	// files are the message's attachments (already written to disk by the
	// caller) and are injected into the prompt by the execution engine. An
	// empty message with files is valid — a bare file sent from IM carries no
	// text.
	SendMessageToSession(sessionID, message string, files []model.FileEntry) error
	// LastAssistantReply returns the target session's most recent FINALIZED
	// assistant reply as plain text (group-chat protocol tags already
	// stripped). ok is false when the session has no finished reply yet — the
	// caller must say so rather than send an empty message. A reply that is
	// still streaming is not returned.
	LastAssistantReply(sessionID string) (text string, ok bool, err error)
	// StopAndClearSession stops the session's in-flight turn (idempotent) and
	// drops its queued messages. wasRunning reports whether a turn was actually
	// cancelled; cleared is the number of queued messages removed.
	StopAndClearSession(sessionID string) (wasRunning bool, cleared int, err error)
	// ListProjects returns every registered project, index starting at 1, in
	// the order /new renders them.
	ListProjects() ([]ProjectRef, error)
	// CreateSessionInProject creates an empty chat session in projectPath using
	// the default agent visible in that project. The path must already be a
	// registered project; an unknown path is an error rather than an implicit
	// registration.
	CreateSessionInProject(projectPath string) (sessionID, title string, err error)
}

// ConnectedClientChecker checks whether any client is currently connected.
// Injected from cmd/server to avoid import cycles with the ws package.
type ConnectedClientChecker interface {
	HasConnectedClients() bool
}
