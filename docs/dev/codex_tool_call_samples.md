# Codex CLI Tool Call JSON Samples

This document contains actual JSON output samples for Codex CLI tool calls, captured via:

```bash
echo "<prompt>" | codex exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -c model="MiniMax-M2.7"
```

## JSON Structure Overview

Codex `exec --json` produces newline-delimited JSON objects (JSONL). Each line has a `type` field:

| Type | Description |
|------|-------------|
| `thread.started` | Thread starts, contains `thread_id` |
| `turn.started` | Turn begins |
| `item.started` | Item begins (command_execution in_progress, or todo_list init) |
| `item.completed` | Item completes (agent_message text, command_execution result, or todo_list final) |
| `item.updated` | Item updated (todo_list step completion) |
| `turn.completed` | Turn ends with `usage` stats |
| `turn.failed` | Turn fails with `error` |
| `error` | Warning/error message (e.g., reconnection) |

### Key Difference from Other Backends

Codex's `exec --json` output **does not expose internal tool calls** (like `create_goal`, `update_plan`, `view_image`) as separate items. Only `command_execution` items appear in the JSONL stream. Internal tools are handled by the Codex runtime and their results are reflected in `agent_message` text only.

---

## 1. exec_command (success)

### item.started — command starts

```json
{
  "type": "item.started",
  "item": {
    "id": "item_0",
    "type": "command_execution",
    "command": "/bin/bash -lc 'echo \"hello from codex exec_command tool\"'",
    "aggregated_output": "",
    "exit_code": null,
    "status": "in_progress"
  }
}
```

### item.completed — command succeeds

```json
{
  "type": "item.completed",
  "item": {
    "id": "item_0",
    "type": "command_execution",
    "command": "/bin/bash -lc 'echo \"hello from codex exec_command tool\"'",
    "aggregated_output": "hello from codex exec_command tool\n",
    "exit_code": 0,
    "status": "completed"
  }
}
```

---

## 2. exec_command (failure)

### item.started — command starts

```json
{
  "type": "item.started",
  "item": {
    "id": "item_0",
    "type": "command_execution",
    "command": "/bin/bash -lc 'ls /nonexistent_directory_xyz'",
    "aggregated_output": "",
    "exit_code": null,
    "status": "in_progress"
  }
}
```

### item.completed — command fails

```json
{
  "type": "item.completed",
  "item": {
    "id": "item_0",
    "type": "command_execution",
    "command": "/bin/bash -lc 'ls /nonexistent_directory_xyz'",
    "aggregated_output": "ls: cannot access '/nonexistent_directory_xyz': No such file or directory\n",
    "exit_code": 2,
    "status": "failed"
  }
}
```

**Note:** Failed commands have `status: "failed"` and non-zero `exit_code`.

---

## 3. agent_message

### item.completed — text response

```json
{
  "type": "item.completed",
  "item": {
    "id": "item_1",
    "type": "agent_message",
    "text": "hello from codex exec_command tool"
  }
}
```

**Note:** `agent_message` items only appear as `item.completed`, never `item.started` in the JSONL stream. They represent the model's text response after executing commands.

---

## 4. todo_list (plan)

### item.started — plan created

```json
{
  "type": "item.started",
  "item": {
    "id": "item_0",
    "type": "todo_list",
    "items": [
      { "text": "Step 1: Create a simple test file to demonstrate tool usage", "completed": false },
      { "text": "Step 2: Read the file back to confirm it exists", "completed": false },
      { "text": "Step 3: Clean up the test file", "completed": false }
    ]
  }
}
```

### item.updated — step completed

```json
{
  "type": "item.updated",
  "item": {
    "id": "item_0",
    "type": "todo_list",
    "items": [
      { "text": "Step 1: Create a simple test file to demonstrate tool usage", "completed": true },
      { "text": "Step 2: Read the file back to confirm it exists", "completed": false },
      { "text": "Step 3: Clean up the test file", "completed": false }
    ]
  }
}
```

### item.completed — all steps done

```json
{
  "type": "item.completed",
  "item": {
    "id": "item_0",
    "type": "todo_list",
    "items": [
      { "text": "Step 1: Create a simple test file to demonstrate tool usage", "completed": true },
      { "text": "Step 2: Read the file back to confirm it exists", "completed": true },
      { "text": "Step 3: Clean up the test file", "completed": true }
    ]
  }
}
```

---

## 5. thread.started

```json
{
  "type": "thread.started",
  "thread_id": "019ea214-58dd-7251-ad7a-42eab4759833"
}
```

---

## 6. turn.started / turn.completed

### turn.started

```json
{
  "type": "turn.started"
}
```

### turn.completed

```json
{
  "type": "turn.completed",
  "usage": {
    "input_tokens": 32877,
    "cached_input_tokens": 19254,
    "output_tokens": 323,
    "reasoning_output_tokens": 164
  }
}
```

**Usage fields:**

| Field | Description |
|-------|-------------|
| `input_tokens` | Total input tokens consumed |
| `cached_input_tokens` | Tokens served from cache |
| `output_tokens` | Output tokens generated |
| `reasoning_output_tokens` | Reasoning/thinking tokens generated |

---

## 7. error / turn.failed

### error event (warning)

```json
{
  "type": "error",
  "message": "Reconnecting... 2/5 (request timed out)"
}
```

### turn.failed event

```json
{
  "type": "turn.failed",
  "error": {
    "message": "{\"error\":{\"message\":\"invalid params, code: 2013, msg: invalid params, unknown model 'nonexistent-model-xyz-123' (2013)\",\"code\":\"invalid_prompt\"}}"
  }
}
```

---

## 8. apply_patch (via exec_command)

Codex does not have a separate `apply_patch` tool in the JSON output — it invokes the built-in `apply_patch` binary via `exec_command`:

### item.started

```json
{
  "type": "item.started",
  "item": {
    "id": "item_4",
    "type": "command_execution",
    "command": "/bin/bash -lc \"apply_patch '*** Begin Patch\n*** Update File: /tmp/codex_test_file.txt\n@@\n-hello world\n+goodbye world\n*** End Patch'\"",
    "aggregated_output": "",
    "exit_code": null,
    "status": "in_progress"
  }
}
```

### item.completed (success)

```json
{
  "type": "item.completed",
  "item": {
    "id": "item_4",
    "type": "command_execution",
    "command": "/bin/bash -lc \"apply_patch '*** Begin Patch\n*** Update File: /tmp/codex_test_file.txt\n@@\n-hello world\n+goodbye world\n*** End Patch'\"",
    "aggregated_output": "Success. Updated the following files:\nM /tmp/codex_test_file.txt\n",
    "exit_code": 0,
    "status": "completed"
  }
}
```

---

## Full Stream JSON Structure

```
1.  {"type":"thread.started","thread_id":"..."}                      — Thread starts
2.  {"type":"turn.started"}                                           — Turn begins
3.  {"type":"item.completed","item":{"type":"agent_message",...}}     — Agent text (plan intro)
4.  {"type":"item.started","item":{"type":"todo_list",...}}           — Plan created (optional)
5.  {"type":"item.started","item":{"type":"command_execution",        — Command starts
      "status":"in_progress","exit_code":null,...}}
6.  {"type":"item.completed","item":{"type":"command_execution",      — Command completes
      "status":"completed","exit_code":0,...}}
7.  {"type":"item.updated","item":{"type":"todo_list",...}}           — Plan step updated (optional)
8.  {"type":"item.started","item":{"type":"command_execution",...}}   — Next command starts
9.  {"type":"item.completed","item":{"type":"command_execution",...}} — Next command completes
10. {"type":"item.updated","item":{"type":"todo_list",...}}           — Plan step updated (optional)
11. {"type":"item.completed","item":{"type":"agent_message",...}}     — Agent final text
12. {"type":"item.completed","item":{"type":"todo_list",...}}         — Plan finalized (optional)
13. {"type":"turn.completed","usage":{...}}                           — Turn ends with usage
```

### Error flow:

```
1.  {"type":"thread.started","thread_id":"..."}
2.  {"type":"turn.started"}
3.  {"type":"error","message":"..."}                                  — Warning/error message
4.  {"type":"turn.failed","error":{"message":"..."}}                  — Turn failed
```

---

## Command Execution Item Fields

### `item.started` with `command_execution`:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Item identifier (e.g., `"item_0"`) |
| `type` | string | Always `"command_execution"` |
| `command` | string | Full command string (wrapped in `/bin/bash -lc '...'`) |
| `aggregated_output` | string | Always `""` at start |
| `exit_code` | null | Always `null` at start |
| `status` | string | Always `"in_progress"` |

### `item.completed` with `command_execution`:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Same item identifier |
| `type` | string | Always `"command_execution"` |
| `command` | string | Same command string |
| `aggregated_output` | string | Full stdout/stderr output |
| `exit_code` | number/null | Process exit code (0 = success) |
| `status` | string | `"completed"` or `"failed"` |

### `item.completed` with `agent_message`:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Item identifier |
| `type` | string | Always `"agent_message"` |
| `text` | string | Agent's text response |

### `item.started` / `item.updated` / `item.completed` with `todo_list`:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Item identifier |
| `type` | string | Always `"todo_list"` |
| `items` | array | List of `{text, completed}` objects |

---

## ClawBench Normalization

In the ClawBench `CodexStreamParser`, `command_execution` items are normalized to the canonical `Bash` tool call:

| Codex JSON Field | ClawBench StreamEvent |
|-------------------|-----------------------|
| `item.started` + `command_execution` | `tool_use` event (done=false) with name=`"Bash"`, input=`{"command":"..."}` |
| `item.completed` + `command_execution` | `tool_use` event (done=true) with output and status |
| `item.completed` + `agent_message` | `thinking` + `content` events (split via `codexSplitThinking`) |
| `thread.started` | `session_capture` event (captures `thread_id` as external session ID) |
| `turn.completed` | `metadata` event (with `SessionID`, `InputTokens`, `OutputTokens`) + `done` |
| `turn.failed` | `error` + `done` events |
| `error` | `warning` event |

---

## Key Observations

1. **Codex has a single visible tool: `command_execution`** — all file operations, code editing, and system interactions are funneled through shell commands.
2. **Internal tools (`create_goal`, `get_goal`, `update_goal`, `update_plan`, `view_image`) are not emitted as items** in the JSONL stream — their effects are reflected in `agent_message` text and `todo_list` items only.
3. **`agent_message` items only appear as `item.completed`** — there is no `item.started` for agent messages in the JSONL output.
4. **Commands are always wrapped in `/bin/bash -lc '...'`** — the login shell wrapping is controlled by the `login` parameter of `exec_command`.
5. **`apply_patch` is invoked as a shell command** — not as a separate tool type. The patch format uses `*** Begin Patch` / `*** End Patch` markers.
6. **`todo_list` items represent the plan** — created via `item.started`, updated via `item.updated` (step completion), and finalized via `item.completed`.
7. **Exit codes**: `0` = success (`status: "completed"`), non-zero = failure (`status: "failed"`).
8. **Error recovery**: Codex emits `error` events for transient issues (reconnection), and `turn.failed` for unrecoverable failures.
9. **Usage tracking**: `turn.completed` includes `input_tokens`, `cached_input_tokens`, `output_tokens`, and `reasoning_output_tokens`.
10. **Thread ID**: Captured from `thread.started` and used for session resume via `codex exec resume`.
