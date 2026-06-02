package ai

import (
	"encoding/json"
	"fmt"
)

// buildBaseStreamArgs builds the shared base arguments for Claude-family CLI backends.
// It constructs the common argument list for --print / --output-format stream-json.
//
// The extraFlags callback receives the ChatRequest and returns additional backend-specific
// flags (e.g., disallowed-tools list, verbose flag). If nil, no extra flags are appended.
func buildBaseStreamArgs(req ChatRequest, extraFlags func(ChatRequest) []string) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--include-partial-messages",
	}

	if req.Resume {
		args = append(args, "--resume", req.SessionID)
	} else if req.SessionID != "" {
		args = append(args, "--session-id", req.SessionID)
	}

	if req.WorkDir != "" {
		args = append(args, "--add-dir", req.WorkDir)
	}
	args = append(args, "--dangerously-skip-permissions")

	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	// Pass model name if per-request override is set
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Pass thinking effort level (e.g., --effort high) for Claude/Codebuddy
	if req.ThinkingEffort != "" {
		args = append(args, "--effort", req.ThinkingEffort)
	}

	if extraFlags != nil {
		args = append(args, extraFlags(req)...)
	}

	return args
}

// injectSystemPrompt prepends the system prompt to req.Prompt when
// ShouldInjectSystemPrompt returns true. Used by CLI backends that lack
// a --system-prompt flag (gemini, opencode, codex, vecli).
func injectSystemPrompt(req ChatRequest) string {
	if !req.ShouldInjectSystemPrompt() {
		return req.Prompt
	}
	return fmt.Sprintf("[System Instructions: %s]\n\n%s", req.SystemPrompt, req.Prompt)
}

// normalizeToolName maps backend-specific tool names to the canonical names
// used by ToolCall throughout the codebase.
//
// Canonical names: Read, Write, Edit, Bash, Glob, Grep, LS, WebFetch, WebSearch,
// Agent, EnterPlanMode, Skill, TodoWrite.
//
// The same mapping is used by gemini_stream.go and opencode_stream.go.
//
//nolint:gocyclo // complex stream parsing logic
func normalizeToolName(toolName string) string {
	switch toolName {
	case "read_file", "read", "look_at":
		return "Read"
	case "write_file", "write":
		return "Write"
	case "edit_file", "replace", "edit":
		return "Edit"
	case "shell", "run_command", "bash", "exec_shell":
		return "Bash"
	case "list_files", "list_directory", "ls", "list_dir":
		return "LS"
	case "search_files", "grep", "grep_files":
		return "Grep"
	case "file_search", "glob":
		return "Glob"
	case "web_fetch", "webfetch", "fetch_url":
		return "WebFetch"
	case "google_web_search", "websearch", "web_search":
		return "WebSearch"
	case "invoke_agent", "task", "agent_spawn", "spawn_agent", "delegate_to_agent":
		return "Agent"
	case "enter_plan_mode":
		return "EnterPlanMode"
	case "activate_skill", "skill", "load_skill":
		return "Skill"
	case "todowrite", "todo_write", "checklist_write":
		return "TodoWrite"
	case "apply_patch":
		return "Edit" // patch-based editing -> Edit
	case "git_status", "git_diff", "git_log", "git_show", "git_blame":
		return "Git" // git operations -> Git
	case "save_memory":
		return "save_memory" // no canonical PascalCase equivalent
	default:
		return toolName
	}
}

// normalizeToolInput remaps camelCase input field names to canonical snake_case.
// It accepts an optional pathMappings map to rename additional fields (e.g., dirPath->path).
// If rawInput is not valid JSON, it returns the input unchanged.
//
// The defaultMappings provide standard camelCase → snake_case remappings shared by all
// backends (e.g., filePath → file_path). These are applied first; caller-provided
// pathMappings can override them if needed (e.g., mapping filePath to a different target).
func normalizeToolInput(rawInput []byte, pathMappings map[string]string) ([]byte, error) {
	if len(rawInput) == 0 {
		return rawInput, nil
	}

	var input map[string]any
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return rawInput, err
	}

	// Default camelCase → snake_case remappings shared by all backends.
	// Callers can override these via pathMappings by providing a mapping
	// for the same source key (e.g., filePath → custom_path).
	defaultMappings := map[string]string{
		"filePath": "file_path",
	}

	// Merge: caller pathMappings take precedence over defaults.
	// If a caller maps the same source key to a different target,
	// the caller's mapping wins.
	merged := make(map[string]string, len(defaultMappings)+len(pathMappings))
	for k, v := range defaultMappings {
		merged[k] = v
	}
	for k, v := range pathMappings {
		merged[k] = v
	}

	// Apply all remappings in a single pass (no double-remap risk)
	for from, to := range merged {
		if v, ok := input[from]; ok {
			delete(input, from)
			input[to] = v
		}
	}

	normalized, err := json.Marshal(input)
	if err != nil {
		return rawInput, err
	}
	return normalized, nil
}

// execCommandJSON is a shared helper that returns canonical {"command":"..."} JSON
// for Bash tool call input normalization. Used by codex_stream.go for its resume
// output parser.
func execCommandJSON(command string) string {
	m := map[string]string{"command": command}
	b, _ := json.Marshal(m)
	return string(b)
}
