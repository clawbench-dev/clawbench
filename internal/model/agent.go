//nolint:govet // shadowed err is acceptable in sequential blocks
package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentModel represents a model option for an agent.
type AgentModel struct {
	ID      string `yaml:"id" json:"id"`
	Name    string `yaml:"name" json:"name"`
	Default bool   `yaml:"default" json:"default"`
}

// Agent represents an AI agent with its own system prompt, backend, and models.
type Agent struct {
	ID                      string       `yaml:"id" json:"id"`
	Name                    string       `yaml:"name" json:"name"`
	Icon                    string       `yaml:"icon" json:"icon"`
	Specialty               string       `yaml:"specialty" json:"specialty"`
	Backend                 string       `yaml:"backend" json:"backend"`
	Models                  []AgentModel `yaml:"models,omitempty" json:"models"`
	Command                 string       `yaml:"command,omitempty" json:"command"`                                   // optional: custom command path for the AI backend CLI
	ThinkingEffort          string       `yaml:"thinking_effort,omitempty" json:"thinkingEffort"`                    // agent's default thinking effort (from YAML); not modified by user preference
	ThinkingEffortLevels    []string     `yaml:"thinking_effort_levels,omitempty" json:"thinkingEffortLevels"`       // valid levels for this backend, e.g. ["low","medium","high","xhigh"]
	PreferredModel          string       `yaml:"preferred_model,omitempty" json:"preferredModel"`                    // user's preferred model; empty = use BaseModelID()
	PreferredThinkingEffort string       `yaml:"preferred_thinking_effort,omitempty" json:"preferredThinkingEffort"` // user's preferred thinking effort; empty = use ThinkingEffort
	SystemPrompt            string       `yaml:"system_prompt,omitempty" json:"systemPrompt"`

	// ACP configuration (only used when Transport != "cli")
	Transport  string `yaml:"transport,omitempty" json:"transport,omitempty"`    // "cli"(default) | "acp-stdio"
	AcpCommand string `yaml:"acp_command,omitempty" json:"acpCommand,omitempty"` // stdio: spawn command, e.g. "gemini --acp"

	// ACP cached state persisted to DB — populated from ACP agent at runtime,
	// used as fallback when the connection pool is empty (before first message
	// or after idle timeout). Raw JSON strings, not parsed into the Agent struct.
	AcpModeState      string `yaml:"-" json:"-"` // JSON: ModeState
	AcpCommands       string `yaml:"-" json:"-"` // JSON: []AvailableCommandInfo
	AcpThinkingState  string `yaml:"-" json:"-"` // JSON: ThinkingEffortState
	AcpModelListState string `yaml:"-" json:"-"` // JSON: ModelListState

	// ModelsAutoDetected indicates whether Models were filled by auto-discovery
	// (from cache) rather than user-defined in YAML. Used by AsyncRefreshModelCache
	// to know which agents should have their models updated.
	ModelsAutoDetected bool `yaml:"-" json:"-"`

	// CanRefreshModels indicates whether this agent supports model refresh via the API.
	// Computed from BackendRegistry at load time based on whether the backend spec
	// has model discovery capability (DiscoverModelsFunc or ListModelsCmd+ParseModels).
	CanRefreshModels bool `yaml:"-" json:"canRefreshModels"`

	// Source indicates how the agent was created: "auto" (CLI detected), "setup" (wizard), "manual" (user).
	Source string `yaml:"-" json:"source"`

	// SortOrder determines display order in agent list; lower values first.
	SortOrder int `yaml:"-" json:"sortOrder"`
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

var (
	Agents        map[string]*Agent // indexed by ID
	AgentList     []*Agent          // ordered list for API responses
	ClawbenchBin  string            // absolute path to clawbench binary for {{CLAWBENCH_BIN}} replacement
	ModelCacheDir string            // model cache directory, set by main.go at startup
	ConfigDir     string            // config directory containing agents/ YAML files; set at startup for WriteAgentYAML
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

// LoadAgents reads all YAML files from the given directory and registers them as agents.
// It builds the common prompt from embedded rules and prepends it to each agent's system prompt.
func LoadAgents(dir string) error {
	Agents = make(map[string]*Agent)
	AgentList = nil
	// Set ConfigDir to the parent of the agents directory (for WriteAgentYAML)
	ConfigDir = filepath.Dir(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var agent Agent
		if err := yaml.Unmarshal(data, &agent); err != nil {
			continue
		}
		if agent.ID == "" {
			continue
		}

		Agents[agent.ID] = &agent
		AgentList = append(AgentList, &agent)
	}

	// Sort AgentList by ID for deterministic ordering (filesystem iteration order is not guaranteed)
	sort.Slice(AgentList, func(i, j int) bool {
		return AgentList[i].ID < AgentList[j].ID
	})

	// Build common prompt from embedded rules (always fully injected)
	commonPrompt := BuildCommonPrompt()

	// Prepend common prompt to each agent's system prompt
	for _, agent := range Agents {
		if commonPrompt != "" && agent.SystemPrompt != "" {
			agent.SystemPrompt = commonPrompt + "\n\n" + agent.SystemPrompt
		} else if commonPrompt != "" {
			agent.SystemPrompt = commonPrompt
		}
	}

	return nil
}

// WriteAgentYAML writes the agent's user-editable fields back to its YAML file.
// It uses atomic write (tmp + rename) to avoid partial writes.
// Only preferred_model and thinking_effort are written; all other fields are preserved as-is.
//
// Deprecated: This function will be removed once agent persistence moves fully to DB.
// It is kept temporarily for backward compatibility during migration.
func WriteAgentYAML(agent *Agent) error {
	if ConfigDir == "" {
		return fmt.Errorf("config directory not initialized")
	}
	yamlPath := filepath.Join(ConfigDir, "agents", agent.ID+".yaml")

	// Read existing YAML to preserve all fields
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("read agent YAML %s: %w", yamlPath, err)
	}

	// Parse into generic map to preserve unknown fields
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse agent YAML %s: %w", yamlPath, err)
	}

	// Patch only the user-editable fields
	if agent.PreferredModel != "" {
		raw["preferred_model"] = agent.PreferredModel
	} else {
		delete(raw, "preferred_model")
	}
	if agent.PreferredThinkingEffort != "" {
		raw["preferred_thinking_effort"] = agent.PreferredThinkingEffort
	} else {
		delete(raw, "preferred_thinking_effort")
	}

	// Marshal back to YAML
	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal agent YAML %s: %w", yamlPath, err)
	}

	// Atomic write: tmp + rename
	tmpPath := yamlPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return fmt.Errorf("write temp agent YAML %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, yamlPath); err != nil {
		return fmt.Errorf("rename temp agent YAML: %w", err)
	}

	return nil
}

// commonRulesTemplate is the built-in system prompt prepended to all agents.
var commonRulesTemplate = strings.Join([]string{
	"## User Interaction (Highest Priority)",
	"",
	"**ALL questions, confirmations, choices, and option presentations directed at the user MUST use structured interactive questions. Plain text questions are ABSOLUTELY FORBIDDEN — no exceptions.**",
	"",
	"### What counts as a \"question\" (must use structured format)",
	"",
	"ANY output that expects or invites a user response, including but not limited to:",
	"- Direct questions (\"Which approach do you prefer?\")",
	"- Confirmation requests (\"Is this OK?\", \"Shall I proceed?\")",
	"- Option presentations (\"You could use A, B, or C\")",
	"- Implicit questions (\"Let me know if…\", \"Feel free to tell me…\")",
	"- Trailing questions at the end of a response (\"Would you like me to…?\")",
	"- Yes/no checks (\"Does this look right?\", \"Ready to continue?\")",
	"- Parameter solicitations (\"What port should I use?\")",
	"",
	"**If the user needs to respond, it is a question. Use structured format. Period.**",
	"",
	"### How to ask questions",
	"",
	"- **ALWAYS** output an `<ask-question>` XML tag. This is the ONLY supported method.",
	"- **NEVER** use the `" + "`AskUserQuestion`" + "` tool call — it will be rejected by the CLI and result in an error.",
	"",
	"XML format — all data in child element text nodes (no attributes):",
	"",
	"```",
	"<ask-question>",
	"  <item>",
	"    <header>Approach</header>",
	"    <multi-select>false</multi-select>",
	"    <question>Which approach do you prefer?</question>",
	"    <option>",
	"      <label>Option A</label>",
	"      <description>Fast but less safe</description>",
	"    </option>",
	"    <option>",
	"      <label>Option B</label>",
	"      <description>Safe but slower</description>",
	"    </option>",
	"  </item>",
	"</ask-question>",
	"```",
	"",
	"**Important:** Use XML child elements only — NO tag attributes, NO JSON. If parsing fails, child element text remains readable; attributes would be invisible.",
	"",
	"### Forbidden question methods",
	"",
	"❌ **NEVER** call the `" + "`AskUserQuestion`" + "` tool — the CLI runs headlessly and cannot present interactive questions, so the tool call will fail with an error. Always use the `<ask-question>` XML tag instead.",
	"",
	"### The ONLY exception",
	"",
	"Pure informational statements that require ZERO user action or response may be plain text. Example: \"I've saved the file to /tmp/output.txt.\" If you add any request for feedback to that statement, it becomes a question.",
	"",
	"### Forbidden patterns (DO NOT output these)",
	"",
	"❌ \"Which approach would you prefer?\" (plain text question)",
	"❌ \"Shall I proceed with option A?\" (plain text confirmation)",
	"❌ \"Let me know if you want me to continue.\" (implicit question)",
	"❌ \"Options: A) fast, B) safe\" (plain text option list)",
	"❌ \"Does this look correct?\" (trailing yes/no question)",
	"❌ Plain text questions in any language",
	"❌ Adding a question at the end of an otherwise informational response",
	"",
	"✅ Use `<ask-question>` XML tag for ALL of the above. ❌ Do NOT use the `" + "`AskUserQuestion`" + "` tool call.",
	"",
	"## Multi-Agent / Team Mode (Mandatory)",
	"",
	"All agents run as child processes of a single CLI session. If the lead agent exits, all sub-agents are killed immediately.",
	"",
	"**Mandatory rule: The lead agent MUST NOT exit until every sub-agent has completed.**",
	"",
	"- **Always use foreground mode** for sub-agents (blocks until return). Never use `" + "`run_in_background: true`" + "`.",
	"- For parallelism, place multiple foreground Agent calls in the **same message** — they execute concurrently and all return before the lead continues.",
	"- If a sub-agent appears stuck or fails, cancel/retry it before exiting — do not abandon it.",
	"- Aggregate results only after all sub-agents have finished.",
	"",
	"## Media File Handling",
	"",
	"### Upload Path",
	"",
	"User-uploaded images: `" + "`.clawbench/uploads/filename.jpg`" + "` — use full path for image analysis.",
	"",
	"### Media Reading: Intent-First Rule",
	"",
	"**Never read/analyze a media file unless the user's intent is clear — doing so wastes tokens.**",
	"",
	"- **Read intent present** (e.g., \"look at this\", \"analyze this screenshot\") → Read and analyze.",
	"- **No read intent** (e.g., user just sends a file) → **Do NOT read.** Acknowledge and ask what they want.",
	"",
	"### Media Generation: Output Rules",
	"",
	"1. **Call tool** → Use appropriate skill/plugin/capability",
	"2. **Save file** → User-specified path, or `<project_root>/.clawbench/generated/` by default. File names: concise, English, type-prefixed (e.g., `" + "`img_`, `audio_`" + "`)",
	"3. **Return format** → Markdown: `" + "`![desc](/api/local-file/<relative_path>)`" + "` for images, `" + "`[desc](/api/local-file/<relative_path>)`" + "` for audio. Must tell user the file path.",
	"4. **Rules** → No absolute paths or external URLs. No spaces or special characters in paths.",
}, "\n")

// BuildCommonPrompt generates the shared system prompt prepended to all agents
// from the built-in rules template.
func BuildCommonPrompt() string {
	return strings.TrimSpace(commonRulesTemplate)
}
