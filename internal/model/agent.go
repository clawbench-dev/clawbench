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
	Icon                    string       `json:"icon"`
	Specialty               string       `json:"specialty"`
	Backend                 string       `json:"backend"`
	Models                  []AgentModel `json:"models"`
	Command                 string       `json:"command"`                 // optional: custom command path for the AI backend CLI
	ThinkingEffort          string       `json:"thinkingEffort"`          // agent's default thinking effort; not modified by user preference
	ThinkingEffortLevels    []string     `json:"thinkingEffortLevels"`    // valid levels for this backend, e.g. ["low","medium","high","xhigh"]
	PreferredModel          string       `json:"preferredModel"`          // user's preferred model; empty = use BaseModelID()
	PreferredThinkingEffort string       `json:"preferredThinkingEffort"` // user's preferred thinking effort; empty = use ThinkingEffort
	SystemPrompt            string       `json:"systemPrompt"`

	// ACP configuration (only used when Transport != "cli")
	Transport  string `json:"transport"`            // "cli"(default) | "acp-stdio"
	AcpCommand string `json:"acpCommand,omitempty"` // acp-stdio: spawn command, e.g. "kimi --acp"

	// CustomSystemPrompt is the user-editable portion of the system prompt.
	// At runtime, LoadAgentsIntoMemory composes: SystemPrompt = commonPrompt + customSystemPrompt.
	// This separation ensures upgrades that change commonPrompt don't corrupt the stored prompt.
	CustomSystemPrompt string `json:"customSystemPrompt"`

	// ModelsAutoDetected indicates whether Models were filled by auto-discovery
	// (from cache) rather than user-defined. Used by AsyncRefreshModelCache
	// to know which agents should have their models updated.
	ModelsAutoDetected bool `json:"-"`

	// CanRefreshModels indicates whether this agent supports model refresh via the API.
	// Computed from BackendRegistry at load time based on whether the backend spec
	// has model discovery capability (registered via RegisterDiscoverModelsFunc).
	CanRefreshModels bool `json:"canRefreshModels"`

	// Source indicates how the agent was created: "auto" (CLI detected), "setup" (wizard), "manual" (user).
	Source string `json:"source"`

	// SortOrder determines display order in agent list; lower values first.
	SortOrder int `json:"sortOrder"`
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
// Used by scheduled tasks which should always use the agent's original default model.
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

// SupportsACP returns true if the agent has ACP capability (has an acp_command configured),
// regardless of its current transport setting.
func (a *Agent) SupportsACP() bool {
	return a.AcpCommand != ""
}

var (
	Agents       map[string]*Agent // indexed by ID
	AgentList    []*Agent          // ordered list for API responses
	ClawbenchBin string            // absolute path to clawbench binary for {{CLAWBENCH_BIN}} replacement
)

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
var commonRulesTemplate = `## User Interaction (Highest Priority)

ALL questions, confirmations, choices, and option presentations MUST use «ask-question» XML tags. Plain text questions are FORBIDDEN.

What counts as a question: anything that expects a user response — direct questions, confirmations ("Is this OK?"), option presentations, implicit questions ("Let me know if…"), trailing yes/no checks, parameter solicitations. If the user needs to respond, use structured format.

Format (XML child elements only, no attributes, no JSON):
«ask-question»
  <item>
    <header>Approach</header>
    <multi-select>false</multi-select>
    <question>Which approach do you prefer?</question>
    <option>
      <label>Option A</label>
      <description>Fast but less safe</description>
    </option>
    <option>
      <label>Option B</label>
      <description>Safe but slower</description>
    </option>
  </item>
«/ask-question»

NEVER call the AskUserQuestion tool — it fails in headless CLI. Always use «ask-question» XML tags.

Exception: pure informational statements needing zero user response may be plain text.

## Media Generation

1. Save to user-specified path or «project_root»/.clawbench/generated/. File names: concise, English, type-prefixed (img_, audio_).
2. Return as Markdown: ![desc](/api/local-file/«relative_path») for images, [desc](/api/local-file/«relative_path») for audio.
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
