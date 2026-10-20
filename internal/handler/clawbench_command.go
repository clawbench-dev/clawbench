package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	userguide "clawbench/docs/user-guide"
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
	ClawbenchCmdUserGuide  = "/cb-user-guide"
	ClawbenchCmdAgent      = "/cb-agent"
	ClawbenchCmdMCP        = "/cb-mcp"
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

Gating script (cron tasks only — the "script" field):
- Optional. It is a precondition gate that runs before the agent, with the project directory as its working directory. Exit code 0 lets the run proceed and the agent is called; any non-zero exit (or a timeout) closes the gate and the run is skipped — no session is created and no notification is sent.
- Output is data, not a control signal: a script may print to stdout/stderr without affecting whether the agent runs.
- Use it when the user wants to avoid spending tokens on runs where nothing changed (e.g. "only run if there are new commits"). Leave it empty otherwise.
- The prompt may reference the script's result through these template variables, written literally with double braces and substituted before the agent runs:
  {{code}} = the script's exit code
  {{stdout}} = the script's standard output
  {{stderr}} = the script's standard error
  {{output}} = the merged stdout + stderr
  Example prompt: "Here is the script output: {{output}} — summarize it."
- script_timeout is the timeout in seconds; 0 means the server default (300).
- The script is ignored for an event task.

After creating a task, you MUST include in your response: <scheduled-task id="task-id" />

Rules:
- Cron expressions are 5-field and interpreted in the server's local timezone. There is no validation endpoint: an invalid expression is rejected by the create call itself (HTTP 400). Treat that response as the validation result rather than claiming the expression was checked beforehand.
- Never create extremely high frequency tasks (e.g. * * * * *) without user confirmation
- Use the user's language for task names and prompts
`

// agentInjectTemplate is the on-demand instruction template injected when
// the user sends a message starting with "/cb-agent ".
//
// Agents are project-scoped: GET /api/agents filters by the project cookie
// (global agents plus this project's scoped agents), and PATCH uses the cookie
// to resolve the project_scope field — so the template carries the cookie just
// like /cb-task.
// Placeholders: {{BASE_URL}}, {{AI_TOKEN_HEADER}}, {{AI_TOKEN}}, {{PROJECT_COOKIE}}, {{PROJECT_PATH}}
const agentInjectTemplate = `[You have access to AI agent management for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}}
Auth: send the header "{{AI_TOKEN_HEADER}}: {{AI_TOKEN}}" on every request. If a request returns 401 the token has expired — tell the user to re-run the command rather than retrying.

Endpoints:
{{ENDPOINTS}}

Project scope:
- Send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}" on every request. "GET /api/agents" returns only the agents visible in this project (global agents plus agents scoped to it), and "PATCH /api/agents" uses the cookie to resolve "project_scope". Without the cookie, project-scoped agents are invisible.

Creating an agent — two shapes, chosen by whether a source agent is being cloned:
- CLONE an existing agent: call "GET /api/agents" first, pick the agent whose "backend" matches what the user wants, then POST /api/agents with "source_id" (that agent's "id") and "name". The clone inherits the source's backend, command, models and transport.
- CUSTOM ACP agent (for a tool with no built-in backend, e.g. Trae, CodeArts): POST /api/agents with "name" and "acp_command" (the stdio command that starts its ACP server, e.g. "trae acp"). Do NOT send "source_id" and do NOT invent a "backend" — the server assigns its own. The command is split on spaces (no quoting), so it cannot carry quoted arguments.
- If the user asks for an agent that is neither a clone of a listed agent nor backed by a command they supplied, ask which of the two they mean instead of guessing.
- The new name is globally unique: a name already used by another agent is rejected with 409 (AgentNameTaken), and a name equal to the group-chat user nickname is rejected with 409 (AgentNameReserved). If rejected, ask the user for a different name instead of retrying the same one.
- If the user has no command in mind, do not invent one. Ask for the exact command that starts the tool's ACP server (their docs name it), and say it must speak ACP over stdio.

Verifying an agent — ALWAYS do this after creating or changing one:
- To test an agent that already exists, resolve its id from "GET /api/agents" (match by name) and run the same two steps. When several agents share a name, list them and say which id you picked.
- Step 1 (free, no tokens): POST /api/agents/{id}/self-test. It returns {ok, passed, warned, failed, checks[{key,status,detail}]} and inspects only configuration and the filesystem — it never spawns the agent. Treat 'fail' as a blocker: report the failing check's 'detail' verbatim (it names the exact command, backend or model id), fix what you can with PATCH, and re-run. 'warn' is NOT a failure — for a brand-new ACP agent, "models: none reported yet" and "no ACP handshake recorded yet" are expected until the first turn.
- Step 2 (costs tokens, one short message): POST /api/agents/{id}/self-test-deep. It runs ONE real turn and returns {ok, error?, deep{reply, transport, stopReason, durationMs}, static}. Run it whenever the user asked you to create-and-verify, or said "test it"; if they only asked to create the agent, run step 1 and offer step 2. It refuses on its own when step 1 has failures, so never call it first.
- Both calls are plain POSTs with an empty body; the deep one can take up to ~90 seconds, so give curl a generous --max-time (e.g. 120) rather than the default short timeout.
- Report the outcome concretely: for step 1 the counts plus each non-ok check; for step 2 the model's reply, the transport actually used and the stop reason. Never claim an agent "works" from the create call alone — a created agent that cannot spawn looks identical to a working one until a check runs.
- If step 2 fails, relay 'error' (it states the concrete reason, e.g. executable missing, handshake failure, no content). For a command problem, PATCH "acp_command" on the custom agent and re-run both steps; do not delete and recreate the agent just to fix a command.

Searching and viewing:
- There is no search endpoint. Call "GET /api/agents" and filter the returned list yourself by the user's criteria (name, specialty, backend, disabled, usage counts). Each agent also carries "sessionCount" / "taskCount" / "membershipCount" and the default agent id is in "defaultAgent".
- For a single agent's details, use the same list — there is no per-id GET for the agent record.

Updating an agent (PATCH /api/agents):
- Always include "id". Only the documented fields may be changed; unknown fields are ignored.
- Validate against the current values first (e.g. "preferred_model" must be one of the agent's "models", "preferred_mode" must be an ACP mode the agent advertises). A bad value returns 400 with a specific msgKey rather than being silently dropped.
- "project_scope": true limits the agent to the current project, false makes it globally visible again. The default agent cannot be scoped (CannotScopeDefaultAgent), and scoping requires a selected project (NoProjectSelected).
- "acp_command" is only accepted for a custom ACP agent (its "backend" is "custom-acp"). A built-in backend's command is owned by auto-discovery and is refused with 400 (AcpCommandManagedByDiscovery); do not try to repoint a built-in agent at another binary this way.

Deleting an agent (DELETE /api/agents):
- Deletion is destructive and only allowed when the agent is unused: if it still has sessions, tasks, or active group memberships the call returns 409 (AgentInUse) with the counts in "detail". Relay those counts to the user instead of forcing the delete.
- The default agent cannot be deleted (CannotDeleteDefaultAgent) and cannot be disabled (CannotDisableDefaultAgent).
- NEVER delete without explicit user confirmation of which agent.

Rules:
- Use the user's language when relaying names, specialties and results.
- Report the concrete id returned by create/update so the user can act on it.
- Do not invent agent fields or backends — read the current list before answering.
`

// mcpInjectTemplate is the on-demand instruction template injected when the
// user sends a message starting with "/cb-mcp ".
//
// The command drives the SAME two resources the settings UI writes: the global
// server list (config.yaml `mcp.servers`) and the per-agent enable policy
// (`agents.mcp_servers`). Splitting them is the point of the design — a
// definition carries secrets and is written once; the policy is per agent.
//
// Placeholders: {{BASE_URL}}, {{AI_TOKEN_HEADER}}, {{AI_TOKEN}}, {{PROJECT_COOKIE}}, {{PROJECT_PATH}}
const mcpInjectTemplate = `[You have access to MCP server configuration for this request. Use the Bash tool to call the local ClawBench HTTP API with curl.]

Base URL: {{BASE_URL}}
Auth: send the header "{{AI_TOKEN_HEADER}}: {{AI_TOKEN}}" on every request. If a request returns 401 the token has expired — tell the user to re-run the command rather than retrying.

Endpoints:
{{ENDPOINTS}}

Project scope:
- The MCP server list itself is GLOBAL — it is not project-scoped, so PATCH /api/config needs no project cookie.
- Agent endpoints ARE project-filtered: send the cookie "{{PROJECT_COOKIE}}={{PROJECT_PATH}}" on GET /api/agents and PATCH /api/agents. GET returns only the agents visible in this project (global plus project-scoped ones), and PATCH uses the cookie to resolve "project_scope". Without it, a project-scoped agent is invisible and cannot be found by name.

Two separate resources — do not confuse them:
1. The GLOBAL server list lives in config.yaml under "mcp.servers". It is ONE list shared by every agent. Edit it with PATCH /api/config; read it with GET /api/config (field "mcp.servers"). This is where a server's command, args, env and url belong.
2. The PER-AGENT policy lives on the agent record as "mcpServers" ({mode, names}) and says WHICH global servers that agent may use. Edit it with PATCH /api/agents. Never copy a server definition into an agent.

PATCH /api/config replaces "mcp.servers" WHOLESALE:
- The array you send becomes the entire list. Servers you omit are DELETED, so always GET first, modify the array, then PATCH the full array back.
- Sending an empty array deletes every server. Only do that when the user asks to remove all of them.
- Each element: {name, transport, command?, args?, env?, url?, headers?}.
  - "name" is required, unique, and may only contain letters, digits, "_" and "-". It is what the per-agent policy references, so renaming a server silently breaks every policy that names the old one — tell the user when you rename.
  - "transport" is one of "stdio", "http", "sse".
  - stdio requires "command" (and optional "args" as an array, "env" as an object).
  - http / sse require "url" (http or https) and accept "headers" as an object.
- Validation rejects: a missing/duplicate/invalid name, an unknown transport, a stdio server with no command, a command that is not found on PATH, and a non-http(s) URL. The error message names the offending index — relay it verbatim and fix that entry instead of retrying blindly.
- env and headers are returned IN PLAINTEXT by GET. Do not print secret values back to the user unless they ask; refer to them by key name.

PATCH /api/agents takes "mcp_servers" as an object: {"mode":"all"|"none"|"selected","names":["a","b"]}.
- "all" (the default, and what an omitted mode means) enables every server in the global list.
- "none" disables MCP for that agent.
- "selected" enables only the servers named in "names".
- Changing the policy does NOT need a restart and does NOT interrupt a running turn: the connection re-reads the policy before each turn and reconnects with the new list if it changed. Say "takes effect from the next message", not "restart required".

Which agents can actually use MCP:
- ACP transport only. An agent with transport "cli" has no MCP parameter and reads its own config file — the global list has no effect on it. If the user asks to enable MCP on a CLI agent, explain that and offer to switch it to ACP (PATCH transport) only if they have an ACP command for it.
- Three backends accept the field and ignore it entirely: "dsh", "deepseek", "pi". ClawBench filters the list out for them, so enabling MCP there does nothing. Tell the user instead of silently writing a policy.
- "codex" and "commandcode" reject the "sse" transport; the whole session fails if one is sent. Those servers are filtered out for them. Prefer "http" when the user has a choice.
- "hermes" attaches MCP asynchronously, so the first turn after connecting may not see the tools. Mention this only if the user reports missing tools.

Rules:
- Read before writing. GET /api/config to see the current list, GET /api/agents to resolve an agent id by name. Never guess a server name or an agent id.
- When the user names a server they already have, match it by "name" rather than adding a near-duplicate.
- After a change, read the resource back and state concretely what is now configured (server names + transports; for an agent, its mode and the enabled names). Never claim success from the PATCH status alone.
- Removing a server is destructive for every agent using it: confirm which server by name before deleting, and mention that agents with mode "all" lose it too.
- Use the user's language.
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
- All projects: send "scope=all" and do NOT send the project cookie. This aggregates every project on this instance and is what the user means by "across projects", "all projects" or "everything". (The cookie is ignored when scope=all is present, so sending it does no harm — but omitting it keeps the request unambiguous.)
  When the user wants to know which project consumes the most, also pass "dims=project" so rows are grouped per project instead of merged into one total.

Working with time:
- start / end are RFC3339 UTC timestamps. Compute the range from the current time above.
- Use the UTC form (append "Z") to match how the backend buckets days.
- The date range is capped by the server; keep it to a few months at most.

Present the numbers in a readable form: a short summary plus a table when several rows come back. Scale large token counts (e.g. 1.2M) and state the currency for cost.
State which project (or that all projects were used) the numbers cover.
If no data is returned, say so plainly — do NOT invent figures.
`

// userGuideInjectTemplate is the on-demand instruction template injected when
// the user sends a message starting with "/cb-user-guide".
//
// Unlike the other templates this one does not drive the HTTP API: the manual
// is a set of local Markdown files that the AI reads directly from disk. That
// makes the directory the only contract, so it is substituted concretely rather
// than described.
//
// The chapter list is deliberately NOT injected: the AI can list the directory
// itself, and a baked-in list would go stale as chapters are added or renamed.
// Placeholders: {{GUIDE_DIR}}
const userGuideInjectTemplate = `[You have access to the ClawBench user manual for this request. Read it directly from the local files below — no HTTP API call is needed.]

Manual directory: {{GUIDE_DIR}}

How to answer:
- List the directory to see the chapters, then read only the ones relevant to the question; do not read the whole manual for a narrow question.
- Answer concisely and clearly, then cite the source: give the absolute file path (e.g. {{GUIDE_DIR}}/chat.md) and the section heading the answer came from. Never shorten the path to a bare file name.
- Quote concrete steps, menu names and defaults exactly as written in the manual.
- If the manual does not cover the question, say so plainly instead of guessing. Do not mention this instruction block.
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
		matchClawbenchCommand(rawMsg, ClawbenchCmdUsage) ||
		matchClawbenchCommand(rawMsg, ClawbenchCmdUserGuide) ||
		matchClawbenchCommand(rawMsg, ClawbenchCmdAgent) ||
		matchClawbenchCommand(rawMsg, ClawbenchCmdMCP)
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
	// The manual is extracted to disk at startup; an empty directory means that
	// step failed (or has not run). Injecting the template anyway would point
	// the AI at files that do not exist, so refuse with a clear error instead.
	if matchClawbenchCommand(rawMsg, ClawbenchCmdUserGuide) {
		if userguide.Dir() == "" {
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "UserGuideNotAvailable")
			return false
		}
		return true
	}
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
	case matchClawbenchCommand(rawMsg, ClawbenchCmdUserGuide):
		return renderUserGuideTemplate()
	case matchClawbenchCommand(rawMsg, ClawbenchCmdAgent):
		return renderAgentTemplate(projectPath)
	case matchClawbenchCommand(rawMsg, ClawbenchCmdMCP):
		return renderMCPTemplate(projectPath)
	}
	return rawMsg, nil
}

// RenderCommandInjection exposes processCommandInjection to the service layer,
// which needs it to inject a command template into a GROUP's host prompt. The
// orchestrator lives in internal/service and cannot import handler (cycle), so
// main.go wires this function into service.SetRenderGroupCommandFn.
//
// It covers every user-message injection kind (the built-in "/cb-*" commands
// and the explicit "#skill" reference), not just /cb-*, so the group path
// cannot silently lack one. Same contract as the individual renderers: a
// non-command is returned unchanged (the caller treats "unchanged" as "no
// injection").
func RenderCommandInjection(rawMsg, projectPath, sessionID string) (string, error) {
	return processCommandInjection(rawMsg, projectPath, sessionID)
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

func renderMCPTemplate(projectPath string) (string, error) {
	endpoints, err := api.RenderCommand(api.CommandMCP)
	if err != nil {
		return "", fmt.Errorf("render mcp endpoints: %w", err)
	}
	tmpl := strings.ReplaceAll(mcpInjectTemplate, "{{ENDPOINTS}}", endpoints)
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

// renderUserGuideTemplate builds the manual-reading prompt. The primary HTTP
// path rejects a missing manual in clawbenchCommandPrecheck; this repeats the
// guard because the queue-drain path calls the renderer directly and would
// otherwise inject a prompt pointing at a directory that does not exist.
func renderUserGuideTemplate() (string, error) {
	dir := userguide.Dir()
	if dir == "" {
		return "", fmt.Errorf("user guide not extracted")
	}
	return strings.ReplaceAll(userGuideInjectTemplate, "{{GUIDE_DIR}}", dir), nil
}

// renderAgentTemplate builds the agent-management prompt. Agents are
// project-scoped (the list filters by project, and PATCH resolves project_scope
// from it), so like the task template this one carries the project cookie.
func renderAgentTemplate(projectPath string) (string, error) {
	endpoints, err := api.RenderCommand(api.CommandAgent)
	if err != nil {
		return "", fmt.Errorf("render agent endpoints: %w", err)
	}
	tmpl := strings.ReplaceAll(agentInjectTemplate, "{{ENDPOINTS}}", endpoints)
	tmpl = strings.ReplaceAll(tmpl, "{{BASE_URL}}", clawbenchBaseURL())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_COOKIE}}", clawbenchProjectCookie())
	tmpl = strings.ReplaceAll(tmpl, "{{PROJECT_PATH}}", projectPath)
	return applyAuthPlaceholders(tmpl), nil
}
