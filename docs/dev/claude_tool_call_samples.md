# Claude Code Tool Call JSON Samples

This document contains actual JSON output samples for Claude Code tool calls, captured via CLI mode:

```bash
echo "<prompt>" | claude --print --output-format stream-json --verbose --add-dir <dir> --dangerously-skip-permissions --max-turns <N>
```

## Key Differences from CodeBuddy

| Aspect | Claude Code | CodeBuddy |
|--------|------------|-----------|
| CLI flag | `claude` | `codebuddy` |
| Stream JSON requires | `--verbose` flag | No extra flag |
| Tool call location | `assistant.message.content[].type="tool_use"` | Top-level `type="function_call"` |
| Tool result location | `user.message.content[].type="tool_result"` | Top-level `type="function_call_result"` |
| Tool input field | `input` (JSON object) | `arguments` (JSON string) |
| Call ID field | `id` | `callId` |
| Tool result ID | `tool_use_id` | `callId` |
| Tool result content | `content` (string or array) | `output.type="text"` + `output.text` |
| Unique tools | `Workflow`, `PushNotification`, `ScheduleWakeup`, `Monitor`, `ExitWorktree`, `mcp__chrome-devtools__*` | `ToolSearch`, `DeferExecuteTool`, `SendMessage`, `TeamCreate`, `TeamDelete`, `ImageGen` |
| JSON output mode | `--output-format json` = final result only; `stream-json` = full stream | `--output-format json` = full session array; `stream-json` = streaming NDJSON |

---

## JSON Structure Overview

Claude `--output-format stream-json` produces newline-delimited JSON objects. Each line has a `type` field:

| Type | Description |
|------|-------------|
| `system` | System events (init, hook_started, hook_response, thinking_tokens) |
| `assistant` | AI response — contains `message.content[]` with `thinking`, `tool_use`, or `text` blocks |
| `user` | Tool results — contains `message.content[]` with `tool_result` blocks |
| `result` | Final session result (duration, cost, usage) |

---

## 1. Read

### tool_use (in assistant message)

```json
{
  "type": "tool_use",
  "id": "call_function_rd08smnvmoui_1",
  "name": "Read",
  "input": {
    "file_path": "/home/xulongzhe/projects/clawbench/go.mod",
    "limit": 3
  }
}
```

### tool_result (in user message)

```json
{
  "tool_use_id": "call_function_rd08smnvmoui_1",
  "type": "tool_result",
  "content": "1\tmodule clawbench\n2\t\n3\tgo 1.25.0"
}
```

---

## 2. Write

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_buwsh9nw9icd_1",
  "name": "Bash",
  "input": {
    "command": "mkdir -p /tmp/claude_tool_samples && echo 'hello claude' > /tmp/claude_tool_samples/test_write.txt",
    "description": "Create directory and write test file"
  }
}
```

> Note: Claude often substitutes `Write` with `Bash` for simple file writes. The `Write` tool format is:

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Write",
  "input": {
    "file_path": "/tmp/claude_tool_samples/test_write.txt",
    "content": "hello claude"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "Successfully wrote to /tmp/claude_tool_samples/test_write.txt"
}
```

---

## 3. Edit

### tool_use

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Edit",
  "input": {
    "file_path": "/tmp/claude_tool_samples/test_write.txt",
    "old_string": "hello",
    "new_string": "goodbye"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "Successfully replaced 1 occurrence of \"hello\" with \"goodbye\" in /tmp/claude_tool_samples/test_write.txt"
}
```

---

## 4. Bash

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_dkkwar7jz65c_1",
  "name": "Bash",
  "input": {
    "command": "ls /home/xulongzhe/projects/clawbench/",
    "description": "List root directory contents"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_dkkwar7jz65c_1",
  "type": "tool_result",
  "content": "acp-mock\nAGENTS.md\nandroid\nassets\nbuild.sh\nCLAUDE.md\n..."
}
```

---

## 5. Glob

> Note: Claude often uses `Bash` with `find` instead of `Glob`. When it does use `Glob`:

### tool_use

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Glob",
  "input": {
    "pattern": "**/*.go",
    "path": "/home/xulongzhe/projects/clawbench/internal/ai",
    "limit": 5
  }
}
```

### tool_result

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "[\"/home/xulongzhe/projects/clawbench/internal/ai/codex.go\",\"...\"]"
}
```

---

## 6. Grep

> Note: Claude often uses `Bash` with `grep -rn` instead of the `Grep` tool.

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Grep",
  "input": {
    "pattern": "func main",
    "path": "/home/xulongzhe/projects/clawbench/cmd/",
    "output_mode": "content",
    "-n": true,
    "head_limit": 3
  }
}
```

---

## 7. WebSearch

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_yvlsjm41f112_1",
  "name": "WebSearch",
  "input": {
    "query": "Go programming language"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_yvlsjm41f112_1",
  "type": "tool_result",
  "content": "Web search results for query: \"Go programming language\"\n\n..."
}
```

---

## 8. WebFetch

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_xntds84k233s_1",
  "name": "WebFetch",
  "input": {
    "url": "https://httpbin.org/get",
    "prompt": "Extract the \"origin\" field from the response"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_xntds84k233s_1",
  "type": "tool_result",
  "content": "Unable to verify if domain httpbin.org is safe to fetch. This may be due to network restrictions..."
}
```

---

## 9. TaskCreate

### tool_use

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "TaskCreate",
  "input": {
    "subject": "Test Task",
    "description": "Claude test task for sampling"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "Task #1 created successfully: Test Task"
}
```

---

## 10. TaskGet

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_rthjkicjkej5_1",
  "name": "TaskGet",
  "input": {
    "taskId": "1"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_rthjkicjkej5_1",
  "type": "tool_result",
  "content": "Task not found"
}
```

---

## 11. TaskUpdate

### tool_use

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "TaskUpdate",
  "input": {
    "taskId": "999",
    "status": "completed"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "Task with ID \"999\" not found"
}
```

---

## 12. TaskList

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_ijcaf3mcsju8_1",
  "name": "TaskList",
  "input": {}
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_ijcaf3mcsju8_1",
  "type": "tool_result",
  "content": "No tasks found"
}
```

---

## 13. TaskStop

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_abkuolr815ru_1",
  "name": "TaskStop",
  "input": {
    "task_id": "fake-task-123"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_abkuolr815ru_1",
  "type": "tool_result",
  "content": "<tool_use_error>No task found with ID: fake-task-123</tool_use_error>"
}
```

---

## 14. TaskOutput

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "TaskOutput",
  "input": {
    "task_id": "fake-task-456",
    "block": true,
    "timeout": 60000
  }
}
```

---

## 15. EnterPlanMode

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_b0xw05d5qr2y_1",
  "name": "EnterPlanMode",
  "input": {}
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_b0xw05d5qr2y_1",
  "type": "tool_result",
  "content": "Entered plan mode. You should now focus on exploring the codebase and designing an implementation approach.\n\nIn plan mode, you should:\n1. Thoroughly explore the codebase to understand existing patterns\n2. Identify similar features and architectural approaches\n3. Consider multiple approaches and their trade-offs\n4. Use AskUserQuestion if you need to clarify the approach\n5. Design a concrete implementation strategy\n6. When ready, use ExitPlanMode to present your plan for approval\n\n..."
}
```

---

## 16. ExitPlanMode

### tool_use

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "ExitPlanMode",
  "input": {
    "allowedPrompts": [
      {
        "tool": "Bash",
        "prompt": "run tests"
      }
    ]
  }
}
```

---

## 17. Agent

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_rvazw1x9vf0x_1",
  "name": "Agent",
  "input": {
    "description": "Explore codebase for implementation task",
    "prompt": "I'm working on the ClawBench project. I need to understand the codebase structure...",
    "subagent_type": "Explore"
  }
}
```

### tool_result (agent returning structured content)

```json
{
  "tool_use_id": "call_function_rvazw1x9vf0x_1",
  "type": "tool_result",
  "content": [
    {
      "type": "text",
      "text": "Here is the high-level map of the ClawBench project:\n\n## Root Directory\n..."
    }
  ]
}
```

> Note: Agent tool_result `content` can be an **array** of content blocks (unlike most tools which return a plain string).

---

## 18. Skill

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Skill",
  "input": {
    "skill": "help"
  }
}
```

### tool_result (pattern)

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "Built-in command executed: /help"
}
```

---

## 19. PushNotification (Claude-unique)

### tool_use

```json
{
  "type": "tool_use",
  "id": "call_function_bsl87tqldr23_1",
  "name": "PushNotification",
  "input": {
    "message": "test notification",
    "status": "proactive"
  }
}
```

### tool_result

```json
{
  "tool_use_id": "call_function_bsl87tqldr23_1",
  "type": "tool_result",
  "content": "Not sent — user active (last keystroke 10s ago, threshold 60s). Terminal + mobile suppressed."
}
```

---

## 20. CronList

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "CronList",
  "input": {}
}
```

### tool_result (pattern)

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "No scheduled jobs."
}
```

---

## 21. CronCreate

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "CronCreate",
  "input": {
    "cron": "0 9 * * *",
    "prompt": "say hello",
    "recurring": true
  }
}
```

---

## 22. CronDelete

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "CronDelete",
  "input": {
    "id": "fake-job-123"
  }
}
```

---

## 23. EnterWorktree

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "EnterWorktree",
  "input": {
    "name": "claude-test-sample"
  }
}
```

### tool_result (pattern)

```json
{
  "tool_use_id": "<id>",
  "type": "tool_result",
  "content": "Created worktree at /home/xulongzhe/projects/clawbench/.claude/worktrees/claude-test-sample on branch worktree-claude-test-sample"
}
```

---

## 24. ExitWorktree (Claude-unique)

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "ExitWorktree",
  "input": {
    "action": "keep",
    "discard_changes": false
  }
}
```

---

## 25. NotebookEdit

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "NotebookEdit",
  "input": {
    "notebook_path": "/tmp/test.ipynb",
    "new_source": "print(1+1)",
    "cell_id": "0",
    "edit_mode": "replace"
  }
}
```

---

## 26. LSP

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "LSP",
  "input": {
    "operation": "hover",
    "filePath": "/home/xulongzhe/projects/clawbench/main.go",
    "line": 1,
    "character": 1
  }
}
```

---

## 27. Workflow (Claude-unique)

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Workflow",
  "input": {
    "name": "predefined-workflow-name",
    "args": {}
  }
}
```

Or with inline script:

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Workflow",
  "input": {
    "script": "// JavaScript workflow script\nexport default async function(args) { ... }",
    "description": "My custom workflow"
  }
}
```

---

## 28. ScheduleWakeup (Claude-unique)

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "ScheduleWakeup",
  "input": {
    "delaySeconds": 300,
    "reason": "Waiting for build to complete",
    "prompt": "Check if the build is done"
  }
}
```

---

## 29. Monitor (Claude-unique)

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "Monitor",
  "input": {
    "description": "Watch build logs for errors",
    "timeout_ms": 300000,
    "persistent": false,
    "command": "tail -f /var/log/build.log"
  }
}
```

---

## MCP Tools

### mcp__chrome-devtools__* (29 tools)

All Chrome DevTools tools follow the same pattern. Example:

#### mcp__chrome-devtools__navigate_page

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "mcp__chrome-devtools__navigate_page",
  "input": {
    "type": "url",
    "url": "https://example.com"
  }
}
```

#### mcp__chrome-devtools__click

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "mcp__chrome-devtools__click",
  "input": {
    "uid": "element-uid-from-snapshot",
    "includeSnapshot": false
  }
}
```

#### mcp__chrome-devtools__take_screenshot

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "mcp__chrome-devtools__take_screenshot",
  "input": {
    "format": "png",
    "filePath": "/tmp/screenshot.png"
  }
}
```

### mcp__tavily__* (2 tools)

#### mcp__tavily__tavily-search

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "mcp__tavily__tavily-search",
  "input": {
    "query": "Go programming language",
    "search_depth": "basic",
    "max_results": 10
  }
}
```

#### mcp__tavily__tavily-extract

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "mcp__tavily__tavily-extract",
  "input": {
    "urls": ["https://example.com/page1", "https://example.com/page2"],
    "extract_depth": "basic"
  }
}
```

### ListMcpResourcesTool / ReadMcpResourceTool

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "ListMcpResourcesTool",
  "input": {
    "server": "chrome-devtools"
  }
}
```

```json
{
  "type": "tool_use",
  "id": "<id>",
  "name": "ReadMcpResourceTool",
  "input": {
    "server": "chrome-devtools",
    "uri": "resource://path"
  }
}
```

---

## Full Stream JSON Structure

A complete Claude `--output-format stream-json --verbose` session produces these line types in order:

```
1. {"type":"system","subtype":"hook_started",...}        — Hook execution start
2. {"type":"system","subtype":"hook_response",...}       — Hook result
3. {"type":"system","subtype":"init",...}                — Session init (session_id, model, tools list)
4. {"type":"system","subtype":"thinking_tokens",...}     — Thinking token budget
5. {"type":"assistant","message":{"content":[{"type":"thinking",...}],...}} — Thinking block
6. {"type":"assistant","message":{"content":[{"type":"tool_use",...}],...}} — Tool call
7. {"type":"user","message":{"content":[{"type":"tool_result",...}],...}}   — Tool result
8. {"type":"assistant","message":{"content":[{"type":"text",...}],...}}     — Final text response
9. {"type":"result","subtype":"success",...}             — Session result
```

### Key fields in `assistant` message:

```json
{
  "type": "assistant",
  "message": {
    "id": "msg_<id>",
    "type": "message",
    "role": "assistant",
    "model": "claude-sonnet-4-20250514",
    "content": [
      {
        "type": "thinking",
        "thinking": "Let me read the file...",
        "signature": "<base64>"
      },
      {
        "type": "tool_use",
        "id": "call_function_<id>_<n>",
        "name": "Read",
        "input": { "file_path": "..." }
      }
    ],
    "stop_reason": "tool_use",
    "stop_sequence": null,
    "usage": {
      "input_tokens": 34730,
      "output_tokens": 26,
      "cache_creation_input_tokens": 0,
      "cache_read_input_tokens": 34688
    }
  },
  "session_id": "<uuid>"
}
```

### Key fields in `user` message (tool result):

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "tool_use_id": "call_function_<id>_<n>",
        "type": "tool_result",
        "content": "<result text or array>"
      }
    ]
  },
  "session_id": "<uuid>"
}
```

### Key fields in `result` message:

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "duration_ms": 11274,
  "duration_api_ms": 11100,
  "num_turns": 2,
  "result": "<final text>",
  "session_id": "<uuid>",
  "total_cost_usd": 0.012,
  "usage": {
    "input_tokens": 69527,
    "output_tokens": 43,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0
  }
}
```

---

## Claude vs CodeBuddy Format Comparison

### Tool Call: CodeBuddy

```json
{
  "type": "function_call",
  "callId": "call_27c70f0a117b4ec1888e761b",
  "name": "Read",
  "arguments": "{\"file_path\": \"/path/to/file\", \"limit\": 3}"
}
```

### Tool Call: Claude

```json
{
  "type": "tool_use",
  "id": "call_function_rd08smnvmoui_1",
  "name": "Read",
  "input": {
    "file_path": "/path/to/file",
    "limit": 3
  }
}
```

| Field | CodeBuddy | Claude |
|-------|-----------|--------|
| Call type | `"function_call"` (top-level) | `"tool_use"` (in `message.content[]`) |
| Call ID | `callId` | `id` |
| Parameters | `arguments` (JSON **string**) | `input` (JSON **object**) |
| Result type | `"function_call_result"` (top-level) | `"tool_result"` (in `message.content[]`) |
| Result ID | `callId` | `tool_use_id` |
| Result content | `output: { type: "text", text: "..." }` | `content: "..."` (string or array) |
| Result status | `status: "completed"` | No explicit status (error = `<tool_use_error>`) |
| Nesting | Flat top-level entries | Nested in `assistant`/`user` message objects |

---

## Key Observations

1. **Claude `input` is a JSON object**, not a string — no secondary parsing needed (unlike CodeBuddy's `arguments` which is a JSON string).
2. **Claude tool IDs** follow the pattern `call_function_<random>_<sequence_number>`.
3. **Claude tool_result content** can be a **string** (most tools) or an **array** of content blocks (Agent tool).
4. **Claude errors** are embedded in the content as `<tool_use_error>...</tool_use_error>` XML tags, not as a separate `status` field.
5. **Claude does NOT have `ToolSearch`/`DeferExecuteTool`** — all tools (including Cron, Worktree, LSP, NotebookEdit) are directly callable.
6. **Claude has `Workflow`, `PushNotification`, `ScheduleWakeup`, `Monitor`, `ExitWorktree`** — which CodeBuddy lacks.
7. **Claude `--output-format json`** returns only the final `result` object — you must use `stream-json` to capture intermediate tool calls.
8. **Claude `stream-json` requires `--verbose`** flag, otherwise it errors.
9. **MCP tools** (`mcp__chrome-devtools__*`, `mcp__tavily__*`) are direct tool calls in Claude, same format as built-in tools.
