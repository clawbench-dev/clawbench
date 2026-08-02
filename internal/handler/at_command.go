package handler

import (
	"fmt"
	"strings"

	"clawbench/internal/model"
)

// chatSearchInjectTemplate is the on-demand instruction template injected when
// the user sends a message starting with "@chatsearch ". It provides the AI
// with RAG search command usage and output format requirements.
// Placeholders: {{CLAWBENCH_BIN}}, {{PROJECT_PATH}}, {{SESSION_ID}}, {{PORT}}, {{DATA_DIR}}
const chatSearchInjectTemplate = `[You have access to historical conversation search for this request. Use the Bash tool to execute commands.]

Search historical conversations: {{CLAWBENCH_BIN}} rag search -q "search terms" --project {{PROJECT_PATH}} --exclude-session-id {{SESSION_ID}} --port {{PORT}} --data-dir {{DATA_DIR}}

Command flags:
- -q: Search query (required)
- --limit: Number of results (default 5)
- --project: Project path (required)
- --exclude-session-id: Exclude current session (required)
- --backend: Filter by backend
- --role: Filter by role (user/assistant)
- --from / --to: Time range

After searching, present the results in a natural, readable format (e.g. a summary paragraph or bullet list). Mention the session titles and key findings.
If no results found, answer based on your own knowledge — do NOT mention the search process.
`

// taskInjectTemplate is the on-demand instruction template injected when
// the user sends a message starting with "@task ". It provides the AI
// with scheduled task management command usage.
// Placeholders: {{CLAWBENCH_BIN}}, {{PROJECT_PATH}}, {{PORT}}, {{DATA_DIR}}
const taskInjectTemplate = `[You have access to scheduled task management for this request. Use the Bash tool to execute commands.]

Task management: {{CLAWBENCH_BIN}} task --project {{PROJECT_PATH}} --port {{PORT}} --data-dir {{DATA_DIR}}

Available subcommands: create / list / get / list-exec / update / delete / pause / resume / trigger / list-agents

When creating a task, use the --agent-id flag. Run "{{CLAWBENCH_BIN}} task list-agents --project {{PROJECT_PATH}} --port {{PORT}} --data-dir {{DATA_DIR}}" to discover available agent IDs. You may use the current session's agent if appropriate.

After creating a task, you MUST include in your response: <scheduled-task id="task-id" />

Rules:
- Always validate cron expression before creating a task
- Never create extremely high frequency tasks (e.g. * * * * *) without user confirmation
- Use the user's language for task names and prompts
`

// processAtCommand checks if the raw user message starts with an @ command
// and returns the injected template (without the original message) to be
// prepended to the prompt. The caller constructs the final prompt as:
//
//	prompt = atInjected + "\n\n" + prompt
//
// Since `prompt` already contains the original user message (with file prefixes),
// processAtCommand returns only the template to avoid duplication.
// For @chatsearch with empty query, returns the raw message unchanged (caller
// should handle the error response).
func processAtCommand(rawMsg, projectPath, sessionID string) string {
	if strings.HasPrefix(rawMsg, "@chatsearch ") {
		query := strings.TrimPrefix(rawMsg, "@chatsearch ")
		if strings.TrimSpace(query) == "" {
			return rawMsg
		}
		tmpl := strings.ReplaceAll(chatSearchInjectTemplate, "{{CLAWBENCH_BIN}}", model.ClawbenchBin)
		tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_PATH}}", projectPath)
		tmpl = strings.ReplaceAll(tmpl, "{{SESSION_ID}}", sessionID)
		tmpl = strings.ReplaceAll(tmpl, "{{PORT}}", fmt.Sprintf("%d", model.ServerPort))
		tmpl = strings.ReplaceAll(tmpl, "{{DATA_DIR}}", model.DataDir)
		// Return only the template; the caller appends the original prompt separately
		return tmpl
	}
	if strings.HasPrefix(rawMsg, "@task ") {
		tmpl := strings.ReplaceAll(taskInjectTemplate, "{{CLAWBENCH_BIN}}", model.ClawbenchBin)
		tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_PATH}}", projectPath)
		tmpl = strings.ReplaceAll(tmpl, "{{PORT}}", fmt.Sprintf("%d", model.ServerPort))
		tmpl = strings.ReplaceAll(tmpl, "{{DATA_DIR}}", model.DataDir)
		return tmpl
	}
	return rawMsg
}
