package handler

import (
	"fmt"
	"strings"

	"clawbench/internal/api"
	"clawbench/internal/model"
)

// ClawBench built-in slash commands. They share the "/" prefix with ACP agent
// commands but are namespaced under "cb-" so they can never collide with an
// agent-provided command: any "/cb-*" is routed to ClawBench's own injection,
// every other "/xxx" is forwarded verbatim to the agent.
const (
	ClawbenchCmdChatSearch = "/cb-chatsearch"
	ClawbenchCmdTask       = "/cb-task"
)

// The endpoint reference embedded in each template is rendered from the
// embedded OpenAPI spec (internal/api), so the AI is always told the same
// contract the server implements. Only behaviour rules — things that are not
// part of the HTTP contract — are written by hand here.
//
// Placeholders: {{BASE_URL}}, {{PROJECT_PATH}}, {{SESSION_ID}}
const chatSearchInjectTemplate = `[You have access to historical conversation search for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}} (no authentication needed from localhost)

Endpoints:
{{ENDPOINTS}}

Required parameters:
- The search query (body field "q") is required.
- Send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}" so results stay inside this project.
- Set exclude_session_id to {{SESSION_ID}} to keep the current conversation out of the results.

After searching, present the results in a natural, readable format (e.g. a summary paragraph or bullet list). Mention the session titles and key findings.
If no results found, answer based on your own knowledge — do NOT mention the search process.
`

// taskInjectTemplate is the on-demand instruction template injected when
// the user sends a message starting with "/cb-task ".
// Placeholders: {{BASE_URL}}, {{PROJECT_COOKIE}}, {{PROJECT_PATH}}
const taskInjectTemplate = `[You have access to scheduled task management for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}} (no authentication needed from localhost)

Endpoints:
{{ENDPOINTS}}

Project scope:
- Send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}" on every request; task endpoints take the project from that cookie and reject requests without it.

Discovering agent IDs:
- Call "GET /api/agents" and use the "id" field of the returned agents. You may reuse the current session's agent when appropriate.

After creating a task, you MUST include in your response: <scheduled-task id="task-id" />

Rules:
- Always validate cron expression before creating a task
- Never create extremely high frequency tasks (e.g. * * * * *) without user confirmation
- Use the user's language for task names and prompts
`

// matchClawbenchCommand reports whether msg is exactly cmd, or cmd followed by
// a space (i.e. cmd with arguments). A bare command with no trailing space is
// still a ClawBench command: the frontend trims trailing whitespace before
// sending, so selecting "/cb-task" from the menu and pressing Enter sends
// "/cb-task" with no space. Treating only "/cb-task " as a match would let the
// bare form slip into the ACP slash-command path and be forwarded to the agent.
func matchClawbenchCommand(msg, cmd string) bool {
	return msg == cmd || strings.HasPrefix(msg, cmd+" ")
}

// IsClawbenchCommand reports whether the raw user message starts with one of
// ClawBench's built-in "/cb-" commands. Used to keep these commands out of the
// ACP slash-command path (they must not be forwarded to the agent).
func IsClawbenchCommand(rawMsg string) bool {
	return matchClawbenchCommand(rawMsg, ClawbenchCmdChatSearch) ||
		matchClawbenchCommand(rawMsg, ClawbenchCmdTask)
}

// clawbenchBaseURL is the absolute base URL an AI subprocess must use to reach
// this server. The AI runs as a child process, so it has no notion of
// "same origin" — the scheme and port have to be spelled out. localhost is
// always correct: the child shares this machine, and localhost requests bypass
// auth unconditionally.
func clawbenchBaseURL() string {
	scheme := "http"
	if model.ConfigInstance.ResolveTLSActive() {
		scheme = "https"
	}
	port := model.ServerPort
	if port == 0 {
		port = 20000
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, port)
}

// processClawbenchCommand checks if the raw user message starts with a
// ClawBench built-in command and returns the injected template (without the
// original message) to be prepended to the prompt. The caller constructs the
// final prompt as:
//
//	prompt = injected + "\n\n" + prompt
//
// Since `prompt` already contains the original user message (with file prefixes),
// processClawbenchCommand returns only the template to avoid duplication.
//
// The error is returned when the endpoint reference cannot be rendered from the
// embedded spec. That should be impossible (the spec is compiled in and covered
// by tests), so a failure means the binary is corrupt: refusing the command is
// safer than injecting a fragment that would send the AI to the wrong endpoint.
func processClawbenchCommand(rawMsg, projectPath, sessionID string) (string, error) {
	switch {
	case matchClawbenchCommand(rawMsg, ClawbenchCmdChatSearch):
		// A bare command has no query to search for. The primary HTTP path
		// rejects this earlier with SearchQueryRequired, but the queue-drain
		// path does not, so the guard belongs here: injecting a search
		// template without a query would leave the AI nothing to search.
		if strings.TrimSpace(strings.TrimPrefix(rawMsg, ClawbenchCmdChatSearch)) == "" {
			return rawMsg, nil
		}
		return renderChatSearchTemplate(projectPath, sessionID)
	case matchClawbenchCommand(rawMsg, ClawbenchCmdTask):
		return renderTaskTemplate(projectPath)
	}
	return rawMsg, nil
}

// clawbenchProjectCookie is the cookie name the AI must send for project
// scoping. It goes through ScopedCookieName because a non-default port prefixes
// the cookie (e.g. "cb21999_clawbench_project") to keep multiple instances on
// one host from clobbering each other — sending the bare name would 403.
func clawbenchProjectCookie() string {
	return model.ScopedCookieName("clawbench_project")
}

func renderChatSearchTemplate(projectPath, sessionID string) (string, error) {
	endpoints, err := api.RenderCommand(api.CommandChatSearch)
	if err != nil {
		return "", fmt.Errorf("render chat-search endpoints: %w", err)
	}
	tmpl := strings.ReplaceAll(chatSearchInjectTemplate, "{{ENDPOINTS}}", endpoints)
	tmpl = strings.ReplaceAll(tmpl, "{{BASE_URL}}", clawbenchBaseURL())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_COOKIE}}", clawbenchProjectCookie())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_PATH}}", projectPath)
	tmpl = strings.ReplaceAll(tmpl, "{{SESSION_ID}}", sessionID)
	return tmpl, nil
}

func renderTaskTemplate(projectPath string) (string, error) {
	endpoints, err := api.RenderCommand(api.CommandTask)
	if err != nil {
		return "", fmt.Errorf("render task endpoints: %w", err)
	}
	tmpl := strings.ReplaceAll(taskInjectTemplate, "{{ENDPOINTS}}", endpoints)
	tmpl = strings.ReplaceAll(tmpl, "{{BASE_URL}}", clawbenchBaseURL())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_COOKIE}}", clawbenchProjectCookie())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_PATH}}", projectPath)
	return tmpl, nil
}
