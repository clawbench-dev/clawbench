package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/api"
	"clawbench/internal/model"
	"clawbench/internal/rag"
)

// ClawBench built-in slash commands. They share the "/" prefix with ACP agent
// commands but are namespaced under "cb-" so they can never collide with an
// agent-provided command: any "/cb-*" is routed to ClawBench's own injection,
// every other "/xxx" is forwarded verbatim to the agent.
const (
	ClawbenchCmdChatSearch = "/cb-chatsearch"
	ClawbenchCmdTask       = "/cb-task"
	ClawbenchCmdUsage      = "/cb-usage"
)

// The endpoint reference embedded in each template is rendered from the
// embedded OpenAPI spec (internal/api), so the AI is always told the same
// contract the server implements. Only behavior rules — things that are not
// part of the HTTP contract — are written by hand here.
//
// Placeholders: {{BASE_URL}}, {{AI_TOKEN_HEADER}}, {{AI_TOKEN}}, {{PROJECT_PATH}}, {{SESSION_ID}}
const chatSearchInjectTemplate = `[You have access to historical conversation search for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}}
Auth: send the header "{{AI_TOKEN_HEADER}}: {{AI_TOKEN}}" on every request. If a request returns 401 the token has expired — tell the user to re-run the command rather than retrying.

Endpoints:
{{ENDPOINTS}}

Choosing the project — pick exactly one per request:
- Current project (default): send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}". Use this when the user asks about "this project" or names no project.
- A specific other project: call "GET /api/conversation-projects" to find its exact path, then send the cookie "{{PROJECT_COOKIE}}=<that path>". Match the project the user names by its directory name (the last path segment) or by its full path; if several projects share a name, ask which one rather than guessing. The list includes projects whose directory was deleted, so history stays reachable after the folder is gone.
- All projects: send NO project cookie at all. The search then covers every project on this instance and is what the user means by "across projects", "all projects" or "everywhere".

Required parameters:
- The search query (body field "q") is required.
- Set exclude_session_id to {{SESSION_ID}} to keep the current conversation out of the results.

When the search spans more than one project, say which project each result belongs to.
After searching, present the results in a natural, readable format (e.g. a summary paragraph or bullet list). Mention the session titles and key findings.
If no results found, answer based on your own knowledge — do NOT mention the search process.
`

// taskInjectTemplate is the on-demand instruction template injected when
// the user sends a message starting with "/cb-task ".
// Placeholders: {{BASE_URL}}, {{AI_TOKEN_HEADER}}, {{AI_TOKEN}}, {{PROJECT_COOKIE}}, {{PROJECT_PATH}}
const taskInjectTemplate = `[You have access to task management for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}}
Auth: send the header "{{AI_TOKEN_HEADER}}: {{AI_TOKEN}}" on every request. If a request returns 401 the token has expired — tell the user to re-run the command rather than retrying.

Endpoints:
{{ENDPOINTS}}

Project scope:
- Send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}" on every request; task endpoints take the project from that cookie and reject requests without it.

Discovering agent IDs:
- Call "GET /api/agents" and use the "id" field of the returned agents. You may reuse the current session's agent when appropriate.

Event-triggered tasks (trigger_mode=event):
- Before creating one, call "GET /api/forge/binding" and confirm "binding" is non-null — an event task only fires for a repository its project is bound to. If it is null, tell the user to bind a repository first rather than creating a task that will never run.
- Note that repeat_mode / max_runs do not constrain an event task, and that the event context is prepended to the prompt automatically; write the prompt as the instruction to act on that context.

After creating a task, you MUST include in your response: <scheduled-task id="task-id" />

Rules:
- Cron expressions are 5-field and interpreted in the server's local timezone. There is no validation endpoint: an invalid expression is rejected by the create call itself (HTTP 400). Treat that response as the validation result rather than claiming the expression was checked beforehand.
- Never create extremely high frequency tasks (e.g. * * * * *) without user confirmation
- Use the user's language for task names and prompts
`

// usageInjectTemplate is the on-demand instruction template injected when the
// user sends a message starting with "/cb-usage ".
// Placeholders: {{BASE_URL}}, {{AI_TOKEN_HEADER}}, {{AI_TOKEN}}, {{PROJECT_COOKIE}}, {{PROJECT_PATH}}, {{NOW}}
const usageInjectTemplate = `[You have access to token usage statistics for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}}
Auth: send the header "{{AI_TOKEN_HEADER}}: {{AI_TOKEN}}" on every request. If a request returns 401 the token has expired — tell the user to re-run the command rather than retrying.
Current time (UTC): {{NOW}}

Endpoints:
{{ENDPOINTS}}

Choosing the project — pick exactly one per request:
- Current project (default): send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}" and omit scope (it defaults to project). Use this when the user asks about "this project" or names no project.
- A specific other project: call "GET /api/conversation-projects" to find its exact path, then send the cookie "{{PROJECT_COOKIE}}=<that path>" and omit scope. Match the project the user names by its directory name (the last path segment) or by its full path; if several projects share a name, ask which one rather than guessing. Projects whose directory was deleted are still listed and their recorded usage remains queryable.
- All projects: send "scope=all" and do NOT send the project cookie. This aggregates every project on this instance and is what the user means by "across projects", "all projects" or "everything". Sending the cookie together with scope=all is rejected as contradictory.
  When the user wants to know which project consumes the most, also pass "dims=project" so rows are grouped per project instead of merged into one total.

Working with time:
- start / end are RFC3339 UTC timestamps. Compute the range from the current time above.
- Use the UTC form (append "Z") to match how the backend buckets days.
- The date range is capped by the server; keep it to a few months at most.

Present the numbers in a readable form: a short summary plus a table when several rows come back. Scale large token counts (e.g. 1.2M) and state the currency for cost.
State which project (or that all projects were used) the numbers cover.
If no data is returned, say so plainly — do NOT invent figures.
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
		matchClawbenchCommand(rawMsg, ClawbenchCmdTask) ||
		matchClawbenchCommand(rawMsg, ClawbenchCmdUsage)
}

// clawbenchNow is the current time in the form the AI needs for RFC3339 query
// parameters. The AI runs as a subprocess and has no reliable notion of "now",
// so the prompt carries it rather than leaving the model to guess a date.
func clawbenchNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// clawbenchBaseURL is the absolute base URL an AI subprocess must use to reach
// this server. The AI runs as a child process, so it has no notion of
// "same origin" — the scheme and port have to be spelled out.
//
// localhost is always correct: the child shares this machine, and the address
// is one of the two conditions IsAITokenRequest checks. The token itself (see
// applyAuthPlaceholders) is what authorizes the call.
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

// clawbenchCommandPrecheck runs the command-specific precondition for a
// built-in command, writing the error response and returning false when the
// request must not proceed. Commands with no precondition fall through true.
//
// This is the only place a per-command check belongs: the primary HTTP path
// calls it, so a new command gets its validation wired in by adding a case
// rather than another if-branch at the call site.
func clawbenchCommandPrecheck(w http.ResponseWriter, r *http.Request, rawMsg string) bool {
	if !matchClawbenchCommand(rawMsg, ClawbenchCmdChatSearch) {
		return true
	}
	// RAG availability check — GlobalStore is nil when the index is not ready.
	if rag.GlobalStore == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGNotReady")
		return false
	}
	// Empty query rejection. A bare "/cb-chatsearch" carries nothing to search.
	if strings.TrimSpace(strings.TrimPrefix(rawMsg, ClawbenchCmdChatSearch)) == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return false
	}
	return true
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
	case matchClawbenchCommand(rawMsg, ClawbenchCmdUsage):
		return renderUsageTemplate(projectPath)
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

// applyAuthPlaceholders substitutes the AI-token header name and a freshly
// signed token into a rendered template.
//
// A new token is signed on every injection rather than cached, so the validity
// window starts when the prompt is built and the 30-minute TTL covers the AI's
// first call. Signing returns "" when no cookie token is configured, which is
// exactly the no-password case where auth is open anyway.
//
// Shared by all three commands so a new command cannot ship without auth.
func applyAuthPlaceholders(tmpl string) string {
	tmpl = strings.ReplaceAll(tmpl, "{{AI_TOKEN_HEADER}}", model.AITokenHeader)
	return strings.ReplaceAll(tmpl, "{{AI_TOKEN}}", model.SignAIToken(time.Now()))
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
	return applyAuthPlaceholders(tmpl), nil
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
	return applyAuthPlaceholders(tmpl), nil
}

func renderUsageTemplate(projectPath string) (string, error) {
	endpoints, err := api.RenderCommand(api.CommandUsage)
	if err != nil {
		return "", fmt.Errorf("render usage endpoints: %w", err)
	}
	tmpl := strings.ReplaceAll(usageInjectTemplate, "{{ENDPOINTS}}", endpoints)
	tmpl = strings.ReplaceAll(tmpl, "{{BASE_URL}}", clawbenchBaseURL())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_COOKIE}}", clawbenchProjectCookie())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_PATH}}", projectPath)
	tmpl = strings.ReplaceAll(tmpl, "{{NOW}}", clawbenchNow())
	return applyAuthPlaceholders(tmpl), nil
}
