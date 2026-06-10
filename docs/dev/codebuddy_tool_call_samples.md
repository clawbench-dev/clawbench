# CodeBuddy Tool Call JSON Samples

This document contains actual JSON output samples for every CodeBuddy tool call, captured via CLI mode:

```bash
echo "<prompt>" | codebuddy --print --output-format json --add-dir <dir> --dangerously-skip-permissions --max-turns <N>
```

## JSON Structure Overview

Each tool invocation produces two JSON entries in the output array:

1. **`function_call`** — the tool call request (tool name + arguments)
2. **`function_call_result`** — the tool execution result (output + status)

### Common `function_call` Fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"function_call"` |
| `callId` | string | Unique ID for this call (matches result) |
| `name` | string | Tool name (e.g., `"Read"`, `"Bash"`) |
| `arguments` | string | JSON-serialized tool input parameters |

### Common `function_call_result` Fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"function_call_result"` |
| `name` | string | Tool name |
| `callId` | string | Matches the `function_call.callId` |
| `status` | string | `"completed"` or `"failed"` |
| `output` | object | `{ "type": "text", "text": "<result>" }` |

---

## 1. Read

### function_call

```json
{
  "type": "function_call",
  "callId": "call_27c70f0a117b4ec1888e761b",
  "name": "Read",
  "arguments": "{\"file_path\": \"/home/xulongzhe/projects/clawbench/go.mod\", \"limit\": 3}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Read",
  "callId": "call_27c70f0a117b4ec1888e761b",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "   1→module clawbench\n   2→\n   3→go 1.25.0\n   4→"
  }
}
```

---

## 2. Write

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1a3bf035710ec1278e9dafafa8a",
  "name": "Write",
  "arguments": "{\"content\": \"hello world\", \"file_path\": \"/tmp/cb_tool_samples/test_write.txt\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Write",
  "callId": "019ea1a3bf035710ec1278e9dafafa8a",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Successfully created and wrote to new file: /tmp/cb_tool_samples/test_write.txt"
  }
}
```

---

## 3. Edit

> Note: The model chose to Read the file first before editing. The Edit call pattern is the same as other tools.

### function_call (Edit pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "Edit",
  "arguments": "{\"file_path\": \"/tmp/cb_tool_samples/test_write.txt\", \"old_string\": \"hello\", \"new_string\": \"goodbye\"}"
}
```

### function_call_result (Edit pattern)

```json
{
  "type": "function_call_result",
  "name": "Edit",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Successfully replaced 1 occurrence of \"hello\" with \"goodbye\" in /tmp/cb_tool_samples/test_write.txt"
  }
}
```

---

## 4. Bash

### function_call

```json
{
  "type": "function_call",
  "callId": "call_ae4f3bbb60584b79bae40227",
  "name": "Bash",
  "arguments": "{\"command\": \"echo hello from bash tool test\", \"description\": \"Echo test message\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Bash",
  "callId": "call_ae4f3bbb60584b79bae40227",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Command: echo hello from bash tool test\nStdout: hello from bash tool test\n\nStderr: (empty)\nExit Code: 0\nSignal: (none)"
  }
}
```

---

## 5. Glob

### function_call

```json
{
  "type": "function_call",
  "callId": "call_fb7deed61ec54a3a9f6a276f",
  "name": "Glob",
  "arguments": "{\"limit\": 5, \"path\": \"/home/xulongzhe/projects/clawbench/internal/ai\", \"pattern\": \"**/*.go\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Glob",
  "callId": "call_fb7deed61ec54a3a9f6a276f",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "[\"/home/xulongzhe/projects/clawbench/internal/ai/codex.go\",\"/home/xulongzhe/projects/clawbench/internal/ai/opencode.go\",\"/home/xulongzhe/projects/clawbench/internal/ai/gemini.go\",\"/home/xulongzhe/projects/clawbench/internal/ai/claude.go\",\"/home/xulongzhe/projects/clawbench/internal/ai/codebuddy.go\"]"
  }
}
```

---

## 6. Grep

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1a7023d3764ec5645c8d488e816",
  "name": "Grep",
  "arguments": "{\"-n\": true, \"head_limit\": 3, \"output_mode\": \"content\", \"path\": \"/home/xulongzhe/projects/clawbench/cmd/\", \"pattern\": \"func main\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Grep",
  "callId": "019ea1a7023d3764ec5645c8d488e816",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "[\"/home/xulongzhe/projects/clawbench/cmd/server/main.go:122:func main() { //nolint:gocognit,gocyclo // complex startup orchestration\",\"/home/xulongzhe/projects/clawbench/cmd/acp-mock/main.go:323:\\t\\t\\t\\tRawInput:   map[string]any{\\\"file_path\\\": \\\"/project/main.go\\\", \\\"content\\\": \\\"package main\\\\nfunc main() {}\\\"},\",\"/home/xulongzhe/projects/clawbench/cmd/acp-mock/main.go:409:func main() {\"]"
  }
}
```

---

## 7. EnterPlanMode

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1ad4b95867f28d929f26d5b7004",
  "name": "EnterPlanMode",
  "arguments": "{}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "EnterPlanMode",
  "callId": "019ea1ad4b95867f28d929f26d5b7004",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Entered plan mode. You should now focus on exploring the codebase and designing an implementation approach.\n\nIn plan mode, you should:\n1. Thoroughly explore the codebase to understand existing patterns\n2. Identify similar features and architectural approaches\n3. Consider multiple approaches and their trade-offs\n4. Use AskUserQuestion if you need to clarify the approach\n5. Design a concrete implementation strategy\n6. When ready, use ExitPlanMode to present your plan for approval\n\n..."
  }
}
```

---

## 8. ExitPlanMode

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1b0a88381ab2dd8465665034ff1",
  "name": "ExitPlanMode",
  "arguments": "{\"allowedPrompts\": [{\"prompt\": \"run tests\", \"tool\": \"Bash\"}]}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "ExitPlanMode",
  "callId": "019ea1b0a88381ab2dd8465665034ff1",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Exited plan mode. Warning: Plan file was not found or empty - the plan content could not be read, but plan mode has been exited successfully."
  }
}
```

---

## 9. TaskCreate

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1a7eb439c68b7ed413a97bd5331",
  "name": "TaskCreate",
  "arguments": "{\"subject\": \"Test Task\", \"description\": \"This is a test task for sampling\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "TaskCreate",
  "callId": "019ea1a7eb439c68b7ed413a97bd5331",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Task #1 created successfully: Test Task"
  }
}
```

---

## 10. TaskGet

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1af70b0c134d93d741aa388c812",
  "name": "TaskGet",
  "arguments": "{\"taskId\": \"1\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "TaskGet",
  "callId": "019ea1af70b0c134d93d741aa388c812",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Task with ID \"1\" not found"
  }
}
```

---

## 11. TaskUpdate

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1afacc7e26ab472b72383e9bd0f",
  "name": "TaskUpdate",
  "arguments": "{\"status\": \"completed\", \"taskId\": \"999\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "TaskUpdate",
  "callId": "019ea1afacc7e26ab472b72383e9bd0f",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Task with ID \"999\" not found"
  }
}
```

---

## 12. TaskList

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1a8aa996e68b501dc44b5f5041f",
  "name": "TaskList",
  "arguments": "{}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "TaskList",
  "callId": "019ea1a8aa996e68b501dc44b5f5041f",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "No tasks found."
  }
}
```

---

## 13. TaskStop

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1afef27a76199852f64b90ba108",
  "name": "TaskStop",
  "arguments": "{\"task_id\": \"fake-task-id-123\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "TaskStop",
  "callId": "019ea1afef27a76199852f64b90ba108",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Background task with ID \"fake-task-id-123\" not found. Use /tasks command to see available tasks."
  }
}
```

---

## 14. TaskOutput

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1b024104d374bf9a3d615edf62b",
  "name": "TaskOutput",
  "arguments": "{\"task_id\": \"fake-task-id-456\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "TaskOutput",
  "callId": "019ea1b024104d374bf9a3d615edf62b",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Background task \"fake-task-id-456\" not found. Check the task ID and try again."
  }
}
```

---

## 15. WebFetch

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1aa3ce0a88d0b78fc13ee262111",
  "name": "WebFetch",
  "arguments": "{\"prompt\": \"Extract the value of the \\\"origin\\\" field from the JSON response\", \"url\": \"https://httpbin.org/get\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "WebFetch",
  "callId": "019ea1aa3ce0a88d0b78fc13ee262111",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "## Analysis Result\n\nThe web content does not contain a JSON response. Instead, the page returned an **HTTP 503 Service Temporarily Unavailable** error.\n\n..."
  }
}
```

---

## 16. WebSearch

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "WebSearch",
  "arguments": "{\"query\": \"Go programming language\", \"topic\": \"programming\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "WebSearch",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<search results as markdown text>"
  }
}
```

---

## 17. Skill

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1aafac70e6f9892b43924ee2af0",
  "name": "Skill",
  "arguments": "{\"skill\": \"help\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Skill",
  "callId": "019ea1aafac70e6f9892b43924ee2af0",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Built-in command executed: /help"
  }
}
```

---

## 18. Agent

### function_call

```json
{
  "type": "function_call",
  "callId": "call_7c138fb6bc004661b1e5ade0",
  "name": "Agent",
  "arguments": "{\"description\": \"Explore internal/ai directory\", \"prompt\": \"List all files in the directory /home/xulongzhe/projects/clawbench/internal/ai/ and provide a summary of what each file does based on its name and contents. Use a \\\"medium\\\" thoroughness level.\", \"subagent_type\": \"Explore\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "Agent",
  "callId": "call_7c138fb6bc004661b1e5ade0",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<agent output text>"
  }
}
```

---

## 19. SendMessage

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1b0668db197023d391fe8bba602",
  "name": "SendMessage",
  "arguments": "{\"content\": \"hello\", \"recipient\": \"test-agent\", \"summary\": \"greeting\", \"type\": \"message\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "SendMessage",
  "callId": "019ea1b0668db197023d391fe8bba602",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Not in a team. SendMessage is only available when you are in a team or have background agents running."
  }
}
```

---

## 20. ToolSearch

### function_call

```json
{
  "type": "function_call",
  "callId": "019ea1b3e98b3e341298c129841eda9f",
  "name": "ToolSearch",
  "arguments": "{\"queries\": [\"cron schedule\"]}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "ToolSearch",
  "callId": "019ea1b3e98b3e341298c129841eda9f",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "Found 3 matching tool(s). Use DeferExecuteTool to invoke them.\n\n## CronCreate\nSchedule a prompt to run at a future time...\n\n## CronList\nList scheduled cron jobs.\n\n## CronDelete\nCancel a scheduled cron job by ID.\n\n..."
  }
}
```

---

## 21. DeferExecuteTool

Used to invoke deferred tools discovered via ToolSearch.

### function_call (CronList example)

```json
{
  "type": "function_call",
  "callId": "019ea1b3f56594e5e1e8540b34526eb3",
  "name": "DeferExecuteTool",
  "arguments": "{\"toolName\": \"CronList\", \"params\": {}}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "019ea1b3f56594e5e1e8540b34526eb3",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "No scheduled jobs."
  }
}
```

---

## 22. DeferExecuteTool → CronDelete

### function_call

```json
{
  "type": "function_call",
  "callId": "call_1513a4790fe24454be5cd448",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"id\": \"fake-job-123\"}, \"toolName\": \"CronDelete\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "call_1513a4790fe24454be5cd448",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "No scheduled job with id 'fake-job-123'"
  }
}
```

---

## 23. DeferExecuteTool → EnterWorktree

### function_call

```json
{
  "type": "function_call",
  "callId": "call_6d00c532ba5c4d2d8a984821",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"branch\": \"test-branch-sample\", \"name\": \"test-branch-sample\"}, \"toolName\": \"EnterWorktree\"}"
}
```

### function_call_result

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "call_6d00c532ba5c4d2d8a984821",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "{\n  \"worktreePath\": \"/home/xulongzhe/projects/clawbench/.codebuddy/worktrees/test-branch-sample\",\n  \"worktreeBranch\": \"worktree-test-branch-sample\",\n  \"worktreeType\": \"git\",\n  \"message\": \"Successfully created and entered worktree at /home/xulongzhe/projects/clawbench/.codebuddy/worktrees/test-branch-sample on branch worktree-test-branch-sample\"\n}"
  }
}
```

---

## 24. DeferExecuteTool → LeaveWorktree

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {}, \"toolName\": \"LeaveWorktree\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<worktree leave result>"
  }
}
```

---

## 25. DeferExecuteTool → LSP

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"operation\": \"hover\", \"filePath\": \"/home/xulongzhe/projects/clawbench/main.go\", \"line\": 1, \"character\": 1}, \"toolName\": \"LSP\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<LSP result JSON>"
  }
}
```

---

## 26. DeferExecuteTool → ImageGen

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"prompt\": \"a red circle on white background\"}, \"toolName\": \"ImageGen\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<image generation result with file path>"
  }
}
```

---

## 27. DeferExecuteTool → NotebookEdit

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"notebook_path\": \"/tmp/test.ipynb\", \"cell_number\": 0, \"new_source\": \"print(1+1)\"}, \"toolName\": \"NotebookEdit\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<notebook edit result>"
  }
}
```

---

## 28. DeferExecuteTool → TeamCreate

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"name\": \"test-sample-team\"}, \"toolName\": \"TeamCreate\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<team creation result>"
  }
}
```

---

## 29. DeferExecuteTool → TeamDelete

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"team_name\": \"test-sample-team\"}, \"toolName\": \"TeamDelete\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<team deletion result>"
  }
}
```

---

## 30. DeferExecuteTool → CronCreate

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"prompt\": \"say hello\", \"cron\": \"0 9 * * *\", \"name\": \"morning-greeting\"}, \"toolName\": \"CronCreate\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<job ID and schedule details>"
  }
}
```

---

## 31. DeferExecuteTool → mcp__github

### function_call (pattern)

```json
{
  "type": "function_call",
  "callId": "<callId>",
  "name": "DeferExecuteTool",
  "arguments": "{\"params\": {\"method\": \"tools/list\"}, \"toolName\": \"mcp__github\"}"
}
```

### function_call_result (pattern)

```json
{
  "type": "function_call_result",
  "name": "DeferExecuteTool",
  "callId": "<callId>",
  "status": "completed",
  "output": {
    "type": "text",
    "text": "<MCP GitHub result>"
  }
}
```

---

## Full Session JSON Structure

A complete CodeBuddy `--output-format json` session returns a JSON array with these entry types in order:

```json
[
  {
    "type": "user",
    "content": [
      { "type": "input_text", "text": "<user prompt>" }
    ],
    "sessionId": "<uuid>",
    "id": "<uuid>",
    "timestamp": 1780828085992
  },
  {
    "type": "file-history-snapshot",
    "id": "<uuid>",
    "timestamp": 1780828085998,
    "isSnapshotUpdate": false,
    "snapshot": {
      "messageId": "<uuid>",
      "trackedFileBackups": {}
    }
  },
  {
    "type": "function_call",
    "callId": "<callId>",
    "name": "<ToolName>",
    "arguments": "<JSON string>",
    "providerData": {
      "messageId": "<uuid>",
      "model": "glm-5.1",
      "requestModelId": "glm-5.1",
      "requestModelName": "GLM-5.1",
      "agent": "cli",
      "argumentsDisplayText": "<short description>"
    },
    "id": "<uuid>",
    "sessionId": "<uuid>",
    "timestamp": 1780828089083,
    "parentId": "<uuid>"
  },
  {
    "type": "function_call_result",
    "name": "<ToolName>",
    "callId": "<callId>",
    "status": "completed",
    "output": {
      "type": "text",
      "text": "<result content>"
    },
    "providerData": {
      "messageId": "<uuid>",
      "model": "glm-5.1",
      "agent": "cli",
      "toolResult": {
        "title": "<tool display title>",
        "content": "<raw content>",
        "renderer": { "type": "code", "context": { "language": "text" } }
      }
    },
    "id": "<uuid>",
    "sessionId": "<uuid>",
    "timestamp": 1780828089125,
    "parentId": "<uuid>"
  },
  {
    "type": "message",
    "role": "assistant",
    "status": "completed",
    "content": [
      {
        "type": "output_text",
        "text": "<assistant's text response>",
        "providerData": { "annotations": [] }
      }
    ],
    "providerData": {
      "messageId": "<uuid>",
      "model": "glm-5.1",
      "agent": "cli",
      "usage": {
        "requests": 1,
        "inputTokens": 34799,
        "outputTokens": 17,
        "totalTokens": 34816
      }
    },
    "id": "<uuid>",
    "sessionId": "<uuid>",
    "timestamp": 1780828091600
  },
  {
    "type": "result",
    "subtype": "success",
    "is_error": false,
    "result": "<final text result>",
    "uuid": "<uuid>",
    "session_id": "<uuid>",
    "duration_ms": 5352,
    "duration_api_ms": 5344,
    "num_turns": 4,
    "total_cost_usd": 0,
    "usage": {
      "input_tokens": 69527,
      "output_tokens": 43,
      "cache_creation_input_tokens": 0,
      "cache_read_input_tokens": 0
    },
    "permission_denials": []
  }
]
```

---

## Key Observations

1. **`arguments` is a JSON string**, not a JSON object — it must be parsed with `JSON.parse()` / `json.Unmarshal()` / `json.loads()`.
2. **`callId` links call and result** — the same `callId` appears in both `function_call` and `function_call_result`.
3. **All results use `{ "type": "text", "text": "..." }`** — even structured data (Glob, Grep) is returned as a JSON string inside the text field.
4. **Deferred tools** (CronCreate, CronDelete, CronList, EnterWorktree, LeaveWorktree, ImageGen, LSP, NotebookEdit, TeamCreate, TeamDelete, mcp__github) require a two-step process: `ToolSearch` to discover the schema, then `DeferExecuteTool` to invoke.
5. **`providerData.toolResult`** in `function_call_result` contains UI rendering metadata (title, content, renderer).
6. **Multiple function_calls** can appear in a single session if `--max-turns` > 1 (the model reads, then edits, etc.).
7. **The `result` entry** is always the last item in the array, containing session-level metadata (duration, cost, token usage).
