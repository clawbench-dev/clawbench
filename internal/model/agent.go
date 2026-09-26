package model

import (
	"strings"
)

// AgentModel represents a model option for an agent.
type AgentModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// Agent represents an AI agent with its own system prompt, backend, and models.
type Agent struct {
	ID                      string       `json:"id"`
	Name                    string       `json:"name"`
	Specialty               string       `json:"specialty"`
	Backend                 string       `json:"backend"`
	Models                  []AgentModel `json:"models"`
	Command                 string       `json:"command"`                 // optional: custom command path for the AI backend CLI
	ThinkingEffort          string       `json:"thinkingEffort"`          // agent's default thinking effort; not modified by user preference
	ThinkingEffortLevels    []string     `json:"thinkingEffortLevels"`    // valid levels for this backend, e.g. ["low","medium","high","xhigh"]
	PreferredMode           string       `json:"preferredMode"`           // user's preferred ACP mode; empty = use agent's default
	PreferredModel          string       `json:"preferredModel"`          // user's preferred model; empty = use BaseModelID()
	PreferredThinkingEffort string       `json:"preferredThinkingEffort"` // user's preferred thinking effort; empty = use ThinkingEffort
	// RuntimeSystemPrompt is the fully composed prompt (shared prefix + the
	// user's own text). It is a RUNTIME-ONLY value: composed on every load by
	// LoadAgentsIntoMemoryFromDB and never persisted.
	//
	// It is deliberately not a stored column. Persisting a composed prompt
	// freezes whatever the shared prefix looked like at the time, so a later
	// change to the built-in prompt is appended after that frozen copy and has
	// no effect. Only CustomSystemPrompt is durable; this field is derived.
	RuntimeSystemPrompt string `json:"-"`

	// ACP configuration (only used when Transport != "cli")
	Transport  string `json:"transport"`            // "cli" | "acp-stdio"; default depends on AcpCommand
	AcpCommand string `json:"acpCommand,omitempty"` // acp-stdio: spawn command, e.g. "kimi --acp"

	// CustomSystemPrompt is the user's own prompt text — the only prompt value
	// that is persisted. At runtime LoadAgentsIntoMemoryFromDB composes
	// RuntimeSystemPrompt = shared prompt + CustomSystemPrompt, so changing the
	// built-in prompt always takes effect and can never corrupt stored text.
	CustomSystemPrompt string `json:"customSystemPrompt"`

	// ModelsAutoDetected indicates whether Models were filled by auto-discovery
	// rather than user-defined. RefreshAgents uses it to decide which agents
	// should have their models updated by discovery.
	ModelsAutoDetected bool `json:"-"`

	// CanRefreshModels indicates whether this agent supports model refresh via the API.
	// Computed from BackendRegistry at load time based on whether the backend spec
	// has model discovery capability (registered via RegisterModelSource).
	CanRefreshModels bool `json:"canRefreshModels"`

	// CLIModels is the pure CLI-discovered list, without any ACP merge. It is
	// populated only in API responses, so a client can show the CLI view when the
	// agent is switched to CLI transport without re-deriving it.
	//
	// Models, by contrast, carries the resolved list (ACP membership applied).
	// Keeping both on the wire is what lets the transport toggle be a plain field
	// read on the client instead of a merge.
	CLIModels []AgentModel `json:"cliModels,omitempty"`

	// SupportsCLI indicates whether this agent's backend has a CLI implementation
	// (a registered CLI backend factory). Backends like grok are ACP-only and
	// return false. Computed at load time from the ai backend factory registry.
	SupportsCLI bool `json:"supportsCLI"`

	// SupportsMidTurn indicates whether this agent's backend can inject a
	// message into a turn that is ALREADY RUNNING instead of queueing it for the
	// next one. Computed at load time from the backend's mid-turn policy
	// (model.BackendSupportsMidTurn). Drives the queued-message action label:
	// true → "insert into the current reply", false → "interrupt and send".
	SupportsMidTurn bool `json:"supportsMidTurn"`

	// SortOrder determines display order in agent list; lower values first.
	SortOrder int `json:"sortOrder"`

	// AutoApprove defaults new sessions of this agent to auto-approve ON.
	// On session creation, service.CreateSession initializes the new row's
	// chat_sessions.auto_approve from this value, so the choice is persisted
	// rather than living only in frontend state. It is a creation-time
	// snapshot: changing this default later does not rewrite existing sessions,
	// and the user can still toggle it per session in the session drawer.
	AutoApprove bool `json:"autoApprove"`
}

// DefaultModelID returns the default model ID for this agent.
// Priority: PreferredModel (user preference) > first model with Default:true > first model in list > empty string.
func (a *Agent) DefaultModelID() string {
	if a.PreferredModel != "" {
		return a.PreferredModel
	}
	return a.BaseModelID()
}

// BaseModelID returns the base default model ID without considering user preference.
// Used by tasks which should always use the agent's original default model.
// Priority: first model with Default:true > first model in list > empty string.
func (a *Agent) BaseModelID() string {
	for _, m := range a.Models {
		if m.Default {
			return m.ID
		}
	}
	if len(a.Models) > 0 {
		return a.Models[0].ID
	}
	return ""
}

// EffectiveThinkingEffort returns the thinking effort for interactive sessions.
// Priority: PreferredThinkingEffort (user preference) > ThinkingEffort (agent default).
func (a *Agent) EffectiveThinkingEffort() string {
	if a.PreferredThinkingEffort != "" {
		return a.PreferredThinkingEffort
	}
	return a.ThinkingEffort
}

// EffectiveModeID returns the mode ID for new ACP sessions.
// Priority: PreferredMode (user preference) > empty (agent's default from session/new).
func (a *Agent) EffectiveModeID() string {
	return a.PreferredMode
}

// SupportsACP returns true if the agent has ACP capability (has an acp_command configured),
// regardless of its current transport setting.
func (a *Agent) SupportsACP() bool {
	return a.AcpCommand != ""
}

var (
	Agents    map[string]*Agent // indexed by ID
	AgentList []*Agent          // ordered list for API responses
)

// GetAgent returns the current in-memory agent for an ID, or nil.
//
// RefreshAgents replaces the whole Agents map with freshly loaded pointers, so a
// long-lived holder (an ACP connection, for example) that captured an *Agent at
// creation time would keep reading a stale model list after a refresh. Resolve
// through this accessor at use time instead of caching the pointer.
func GetAgent(id string) *Agent {
	if id == "" {
		return nil
	}
	return Agents[id]
}

// GetDefaultAgentID returns the default agent ID for new sessions.
// Priority: configured DefaultAgentID > first agent in AgentList > empty string.
func GetDefaultAgentID() string {
	if DefaultAgentID != "" {
		if _, ok := Agents[DefaultAgentID]; ok {
			return DefaultAgentID
		}
	}
	if len(AgentList) > 0 {
		return AgentList[0].ID
	}
	return ""
}

// commonRulesTemplate is the built-in system prompt prepended to all agents.
// Backticks are represented as «» placeholders and replaced in BuildCommonPrompt.
// Tag names are written as literal angle brackets — they are markup, not code
// spans, and wrapping them in backticks made models emit `clawbench-ask-question`,
// which no parser accepts.
var commonRulesTemplate = `## User Interaction (Highest Priority)

ALL questions, confirmations, choices, and option presentations MUST use <clawbench-ask-question> tags. Plain text questions are FORBIDDEN.

What counts as a question: anything that expects a user response — direct questions, confirmations ("Is this OK?"), option presentations, implicit questions ("Let me know if…"), trailing yes/no checks, parameter solicitations. If the user needs to respond, use structured format.

Format: ONE question per tag, with native Markdown inside. No attributes, no JSON.

Single choice — a plain list:
<clawbench-ask-question>
**Approach**
Which approach do you prefer?
- Option A — Fast but less safe
- Option B — Safe but slower
</clawbench-ask-question>

Multiple choice — a checkbox list («[ ]»):
<clawbench-ask-question>
**Which features should I enable?**
- [ ] Syntax highlighting — color-code tokens for readability
- [ ] Word wrap — break long lines at viewport edge
</clawbench-ask-question>

Rules:
- One tag = one question. For several questions, emit several tags.
- «**bold**» on its own line is the card title (optional).
- Any other non-list line is the question text.
- Each «- item» is an option with a title and, after « — », a brief description explaining what it means or when it applies. A title alone is rarely enough for an informed choice.
- If an option title is bold, put the description's « — » after the closing «**», never inside it, so the whole bold phrase stays the title. A « — » inside the bold run is part of the title, not the separator.
- A checkbox list («- [ ]») means multiple choice; a plain list means single choice.

If the payload is malformed the tag is stripped and its text is rendered as
Markdown, so ALWAYS keep the question readable as plain Markdown.

NEVER call the AskUserQuestion tool — it fails in headless CLI. Always use <clawbench-ask-question> tags.

Exception: pure informational statements needing zero user response may be plain text.

## Media Generation

1. Save to user-specified path or «project_root»/.clawbench/generated/. File names: concise, English, type-prefixed (img_, audio_, video_).
2. Return as Markdown with project-relative paths (no URL prefix): ![desc](«relative_path») for images, [desc](«relative_path») for audio, [desc](«relative_path») for video.
3. No absolute paths or external URLs. No spaces or special characters in paths.`

// mediaRulesTemplate is injected into the system prompt only when the user
// message carries file attachments (uploaded files or attached project files).
// Uses same «» placeholder convention as commonRulesTemplate.
var mediaRulesTemplate = `## Media File Handling

Upload path: .clawbench/uploads/filename.jpg — use full path for image analysis.

Reading: Never read/analyze a media file unless the user's intent is clear (e.g., "look at this"). No intent → acknowledge and ask what they want.`

// bt replaces «» placeholder pairs with backticks for readable template strings.
func bt(s string) string {
	s = strings.ReplaceAll(s, "«", "`")
	s = strings.ReplaceAll(s, "»", "`")
	return s
}

// BuildCommonPrompt generates the shared system prompt prepended to all agents
// from the built-in rules template.
func BuildCommonPrompt() string {
	return strings.TrimSpace(bt(commonRulesTemplate))
}

// BuildMediaPrompt generates the media handling rules injected only when
// the user message carries file attachments.
func BuildMediaPrompt() string {
	return strings.TrimSpace(bt(mediaRulesTemplate))
}
