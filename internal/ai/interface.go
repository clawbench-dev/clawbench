package ai

import (
	"context"
	"fmt"

	"clawbench/internal/model"
)

// ChatRequest represents a request to the AI backend
type ChatRequest struct {
	Prompt                 string
	SessionID              string
	WorkDir                string
	SystemPrompt           string
	Model                  string                  // per-request model override (empty = use global default)
	Command                string                  // optional: custom command path for the AI backend CLI
	AgentID                string                  // agent ID for logging and persistence
	ThinkingEffort         string                  // thinking effort level, e.g., "high"; empty = auto (don't pass flag)
	Mode                   string                  // ACP session mode, e.g., "code", "ask", "architect"; empty = use current
	Resume                 bool                    // If true, resume an existing session instead of creating new
	ScheduledExecution     bool                    // If true, this is a scheduled task execution — skill-level anti-recursion block
	HasAttachments         bool                    // If true, the user message carries file attachments (triggers media rules injection)
	AssistantMessageCount  int                     // Number of finalized assistant messages in the session (0 for new sessions). Used for logging and periodic summary logic.
	HasConversationHistory bool                    // True if the session has any messages in DB (user + assistant, finalized + streaming). Drives shouldNewSessionFallback: true blocks silent fallback to NewSession on recovery failure (amnesia prevention). Differs from AssistantMessageCount: a session with only an in-flight user prompt has AssistantMessageCount=0 but HasConversationHistory=true.
	ForkContext            string                  // Formatted history from parent session, injected on fork's first message so the AI has context
	Images                 []model.ImageAttachment // Inline images for multimodal ACP prompts. Only ACP backends that advertise the image prompt capability consume these.
}

// ImageAttachment carries an inline image for a multimodal ACP prompt.
// Alias of model.ImageAttachment to avoid a circular dependency between
// the ai and model packages (ai imports model).
type ImageAttachment = model.ImageAttachment

// ShouldInjectSystemPrompt determines whether the system prompt should be injected
// into the user prompt for CLI backends that lack a --system-prompt flag.
// On the first message (!Resume): always inject.
// On resume: inject every N assistant turns (configured via chat.system_prompt_interval).
func (r ChatRequest) ShouldInjectSystemPrompt() bool {
	if r.SystemPrompt == "" {
		return false
	}
	if !r.Resume {
		return true
	}
	interval := model.ChatSystemPromptInterval
	if interval <= 0 {
		return false
	}
	return r.AssistantMessageCount > 0 && r.AssistantMessageCount%interval == 0
}

// Metadata contains additional information about the AI response
type Metadata struct {
	Mode           string  `json:"mode,omitempty"`           // ACP mode (e.g., "code", "ask", "architect")
	ThinkingEffort string  `json:"thinkingEffort,omitempty"` // Thinking effort level (e.g., "low", "medium", "high")
	Transport      string  `json:"transport,omitempty"`      // Backend transport type: "acp-stdio" or "cli"
	Model          string  `json:"model,omitempty"`
	InputTokens    int     `json:"inputTokens,omitempty"`
	OutputTokens   int     `json:"outputTokens,omitempty"`
	DurationMs     int     `json:"durationMs,omitempty"` // CLI self-reported duration
	WallMs         int     `json:"wallMs,omitempty"`     // Backend wall-clock duration (time from ExecuteStream start to finalization)
	CostUSD        float64 `json:"costUsd,omitempty"`
	SessionID      string  `json:"sessionId,omitempty"`
	StopReason     string  `json:"stopReason,omitempty"`
	IsError        bool    `json:"isError,omitempty"`
	ErrorMessage   string  `json:"errorMessage,omitempty"`
}

// Warning reason codes — used by frontend for i18n lookup and visual severity
const (
	ReasonDisconnect    = "disconnect"     // WS client disconnected
	ReasonTimeout       = "timeout"        // AI response timeout
	ReasonUserCancel    = "user_cancel"    // User explicitly cancelled
	ReasonContextCancel = "context_cancel" // Context cancelled (generic interruption)
	ReasonEmpty         = "empty"          // AI returned no content
	ReasonParseError    = "parse_error"    // CLI output parsing error
	ReasonBackendExit   = "backend_exit"   // CLI process exited abnormally
	ReasonRequestFailed = "request_failed" // Codex turn.failed
	ReasonPanic         = "panic"          // AI goroutine panicked
)

// SelectOptionDef describes a single selectable option (e.g., mode, thinking effort level).
// It is structurally identical to ModeDef, ThinkingEffortDef, and ConfigOptionValue —
// all share {ID, Name} with the same JSON tags. SelectOptionDef serves as the unified
// internal type; the domain-specific aliases are kept for JSON serialization compatibility.
type SelectOptionDef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// SelectState carries a unified select-one-from-many state, used internally
// to generalize mode and thinking effort handling. Domain-specific types
// (ModeState, ThinkingEffortState) are kept for JSON API compatibility.
type SelectState struct {
	CurrentID string
	Available []SelectOptionDef
	Category  string // "mode", "thought_level", etc.
}

// IsEmpty returns true if the SelectState has no data at all.
func (s SelectState) IsEmpty() bool {
	return s.CurrentID == "" && len(s.Available) == 0 && s.Category == ""
}

// IsValidOption returns true if the given ID exists in Available.
func (s SelectState) IsValidOption(id string) bool {
	for _, opt := range s.Available {
		if opt.ID == id {
			return true
		}
	}
	return false
}

// ToModeState converts SelectState to a ModeState for JSON serialization.
// Returns nil if the state is empty.
func (s SelectState) ToModeState() *ModeState {
	if s.IsEmpty() {
		return nil
	}
	ms := &ModeState{
		CurrentModeID:  s.CurrentID,
		AvailableModes: make([]ModeDef, len(s.Available)),
	}
	for i, opt := range s.Available {
		ms.AvailableModes[i] = ModeDef(opt)
	}
	return ms
}

// ToThinkingEffortState converts SelectState to a ThinkingEffortState for JSON serialization.
// Returns nil if the state is empty.
func (s SelectState) ToThinkingEffortState() *ThinkingEffortState {
	if s.IsEmpty() {
		return nil
	}
	tes := &ThinkingEffortState{
		CurrentID:       s.CurrentID,
		AvailableLevels: make([]ThinkingEffortDef, len(s.Available)),
	}
	for i, opt := range s.Available {
		tes.AvailableLevels[i] = ThinkingEffortDef(opt)
	}
	return tes
}

// NewSelectStateFromMode creates a SelectState from a ModeState.
// Returns an empty SelectState if ms is nil.
func NewSelectStateFromMode(ms *ModeState) SelectState {
	if ms == nil {
		return SelectState{}
	}
	sel := SelectState{
		CurrentID: ms.CurrentModeID,
		Category:  "mode",
	}
	for _, m := range ms.AvailableModes {
		sel.Available = append(sel.Available, SelectOptionDef(m))
	}
	return sel
}

// NewSelectStateFromThinkingEffort creates a SelectState from a ThinkingEffortState.
// Returns an empty SelectState if tes is nil.
func NewSelectStateFromThinkingEffort(tes *ThinkingEffortState) SelectState {
	if tes == nil {
		return SelectState{}
	}
	sel := SelectState{
		CurrentID: tes.CurrentID,
		Category:  "thought_level",
	}
	for _, l := range tes.AvailableLevels {
		sel.Available = append(sel.Available, SelectOptionDef(l))
	}
	return sel
}

// ModeDef describes a single available session mode (e.g., "ask", "architect", "code").
type ModeDef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ModeState carries the current and available session modes for an ACP session.
type ModeState struct {
	CurrentModeID  string    `json:"currentModeId"`
	AvailableModes []ModeDef `json:"availableModes"`
}

// ThinkingEffortDef describes a single available thinking effort level (e.g., "low", "medium", "high").
type ThinkingEffortDef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ModelListState carries the current and available models from an ACP agent.
// Populated from ACP config options with Category "model".
type ModelListState struct {
	CurrentModelID string             `json:"currentModelId"`
	Models         []model.AgentModel `json:"models"`
}

// ThinkingEffortState carries the current and available thinking effort levels for an ACP session.
// Populated from ACP config options with Category "thought_level".
type ThinkingEffortState struct {
	CurrentID       string              `json:"currentId"`
	AvailableLevels []ThinkingEffortDef `json:"availableLevels"`
}

// ConfigOptionValue represents a selectable value within a config option.
type ConfigOptionValue struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ConfigOptionDef describes a session config option (v2 style).
// Only options with Category "mode" are relevant for mode switching.
type ConfigOptionDef struct {
	ID       string              `json:"id"`
	Name     string              `json:"name,omitempty"`
	Category string              `json:"category,omitempty"` // "mode", etc.
	Values   []ConfigOptionValue `json:"values,omitempty"`
}

// ConfigOptionState carries the current value and available options for a config option.
type ConfigOptionState struct {
	ConfigID  string            `json:"configId"`
	CurrentID string            `json:"currentValueId"`
	Options   []ConfigOptionDef `json:"options,omitempty"`
}

// AvailableCommandInfo represents a slash command discovered from an ACP agent
// via the available_commands_update session notification.
type AvailableCommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputHint   string `json:"inputHint,omitempty"`
}

// PlanEntry represents a single entry in an agent's execution plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

// PlanState carries the full plan with entries from an ACP plan update.
type PlanState struct {
	Entries []PlanEntry `json:"entries"`
}

// UsageState carries context window usage information from an ACP UsageUpdate.
type UsageState struct {
	Used              int     `json:"used"`                        // Tokens currently in context
	Size              int     `json:"size"`                        // Total context window size in tokens
	InputTokens       int     `json:"inputTokens,omitempty"`       // Total input tokens across all turns (from PromptResponse.Usage)
	OutputTokens      int     `json:"outputTokens,omitempty"`      // Total output tokens across all turns (from PromptResponse.Usage)
	TotalTokens       int     `json:"totalTokens,omitempty"`       // Sum of all token types (from PromptResponse.Usage)
	CachedReadTokens  int     `json:"cachedReadTokens,omitempty"`  // Cache read tokens (from PromptResponse.Usage)
	CachedWriteTokens int     `json:"cachedWriteTokens,omitempty"` // Cache write tokens (from PromptResponse.Usage)
	ThoughtTokens     int     `json:"thoughtTokens,omitempty"`     // Reasoning/thinking tokens (from PromptResponse.Usage)
	Cost              float64 `json:"cost,omitempty"`              // Cumulative session cost (0 = not set)
	Currency          string  `json:"currency,omitempty"`          // ISO 4217 currency code (e.g., "USD")
}

// StreamEvent represents a single event in the streaming output
type StreamEvent struct {
	Type           string                 // "content", "thinking", "metadata", "done", "error", "tool_use", "tool_result", "raw_output", "queue_drain", "queue_cancel", "session_capture", "mode_update", "config_update", "commands_update", "thinking_effort_update", "plan_update", "model_list_update", "usage_update", "user_message", "stream_start", "replay_done", "content_reset"
	Content        string                 // Incremental text (Type=content, Type=thinking) or captured session ID (Type=session_capture)
	Reason         string                 // Structured reason code for i18n (e.g. "disconnect", "timeout", "parse_error")
	Meta           *Metadata              // Metadata (Type=metadata)
	Error          string                 // Error message (Type=error)
	Tool           *ToolCall              // Tool call info (Type=tool_use, Type=tool_result)
	RawOutput      string                 // Raw stdout lines from AI backend (Type=raw_output)
	QueueEvent     *QueueEventData        // Queue data (Type=queue_drain)
	Mode           *ModeState             // Mode state (Type=mode_update)
	Config         *ConfigOptionState     // Config option state (Type=config_update)
	Commands       []AvailableCommandInfo // Slash commands (Type=commands_update)
	ThinkingEffort *ThinkingEffortState   // Thinking effort state (Type=thinking_effort_update)
	Plan           *PlanState             // Plan state (Type=plan_update)
	ModelList      *ModelListState        // Model list state (Type=model_list_update)
	Usage          *UsageState            // Usage state (Type=usage_update)
	ToolMeta       *ToolCallMeta          // Extracted tool metadata for WS forwarding (Type=tool_use, Type=tool_result)
	UserMessage    *UserMessageData       // User message for cross-device sync (Type=user_message)
	StreamStart    *StreamStartData       // Stream start (Type=stream_start) — carries streaming message DB id
}

// StreamStartData carries the streaming message DB id for the stream_start event.
// Emitted by the service layer once per prompt so any client (including ones
// that opened the session mid-stream) can create a streaming placeholder
// anchored to the authoritative DB row id.
type StreamStartData struct {
	MessageID int64 `json:"message_id"`
}

// ToolCall represents a tool invocation by the AI.
// Each backend parser must normalize tool names and input field names
// to the canonical conventions before emitting ToolCall events:
//
//	Canonical tool names: Read, Write, Edit, Bash, Glob, Grep, LS, ...
//	Canonical input fields: file_path (not filePath), command, old_string, new_string, ...
type ToolCall struct {
	Name   string // Canonical tool name (e.g., "Read", "Bash", "Edit")
	ID     string // Tool call ID
	Input  string // Tool input (JSON string with canonical field names, accumulated incrementally)
	Output string // Tool execution output text (populated when available)
	Status string // Tool execution status: "success", "error", "" (unknown)
	Done   bool   // Whether the tool call input is complete
	// DurationMs is the wall-clock execution time in milliseconds.
	// Injected by SessionExecutor when the tool completes; backend parsers
	// leave it 0 (unknown).
	DurationMs int `json:"duration_ms,omitempty"`
}

// maxToolOutputBytes limits tool output stored per tool call to prevent
// unbounded DB growth from tools like Bash or Read with large output.
const maxToolOutputBytes = 51200 // 50KB

// truncateToolOutput truncates tool output exceeding maxToolOutputBytes
// and appends a truncation marker.
func truncateToolOutput(output string) string {
	if len(output) <= maxToolOutputBytes {
		return output
	}
	return output[:maxToolOutputBytes] + fmt.Sprintf("\n[truncated: original %d bytes]", len(output))
}

// QueueEventData carries data for queue_drain and queue_cancel WS events.
// queue_drain: atomically finalizes current streaming, starts next queued message.
// queue_cancel: emitted when user cancels while messages are queued.
type QueueEventData struct {
	SessionID string                `json:"sessionId,omitempty"` // Session this event belongs to (for frontend routing)
	QueueID   string                `json:"queueId,omitempty"`   // Frontend-generated ID for matching pending messages (queue_drain)
	QueueIDs  []string              `json:"queueIds"`            // IDs of cancelled queued messages (queue_cancel) — may be empty
	Text      string                `json:"text,omitempty"`
	MessageID int64                 `json:"messageId,omitempty"` // DB ID of the drained user message (queue_drain only)
	FilePaths []string              `json:"filePaths,omitempty"`
	Files     []model.FileEntry     `json:"files,omitempty"`
	Queue     []model.QueuedMessage `json:"queue,omitempty"`
}

// UserMessageData carries a user message for cross-device synchronization.
// Emitted via StreamHub.EmitToSession after AddChatMessage succeeds,
// so other devices subscribed to the same session see the message in real-time.
type UserMessageData struct {
	MessageID      int64             `json:"messageId"`                // DB row ID (0 if not yet persisted, e.g. enqueued messages)
	Content        string            `json:"content"`                  // Raw user message text
	Files          []model.FileEntry `json:"files,omitempty"`          // File attachments
	SenderClientID string            `json:"senderClientId,omitempty"` // WS client ID of the sender (to skip self-echo)
	QueueID        string            `json:"queueId,omitempty"`        // Frontend queue ID (for enqueued messages, enables precise drain matching)
}

// AIBackend defines the interface for AI backend implementations
type AIBackend interface {
	// Name returns the backend identifier (e.g., "claude", "codebuddy")
	Name() string

	// ExecuteStream runs the AI backend and returns a channel of streaming events.
	//
	// Producer contract: the returned channel MUST always be closed, even on
	// error, cancellation, or abnormal process exit. Consumers drain the channel
	// until it closes (see SessionExecutor.Finalize) to unblock producer sends,
	// so a producer that never closes its channel can hang the consumer.
	ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
