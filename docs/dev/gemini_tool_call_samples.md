# Gemini CLI Tool Call JSON Samples

This document contains actual JSON output samples for Gemini CLI tool calls, captured via:

```bash
echo "<prompt>" | gemini -p - --output-format stream-json --yolo
```

## JSON Structure Overview

Gemini `--output-format stream-json` produces newline-delimited JSON objects. Each line has a `type` field:

| Type | Description |
|------|-------------|
| `init` | Session init (session_id, model) |
| `message` | User/assistant messages (role, content, delta) |
| `tool_use` | Tool call request |
| `tool_result` | Tool execution result |
| `result` | Final session result |

---

## 1. read_file

### tool_use

```json
{
  "type": "tool_use",
  "timestamp": "2026-06-07T11:58:16.116Z",
  "tool_name": "read_file",
  "tool_id": "read_file_1780833496116_0",
  "parameters": {
    "file_path": "go.mod",
    "start_line": 1,
    "end_line": 3
  }
}
```

### tool_result

```json
{
  "type": "tool_result",
  "timestamp": "2026-06-07T11:58:16.291Z",
  "tool_id": "read_file_1780833496116_0",
  "status": "success",
  "output": "Read lines 1-3 of 38 from go.mod"
}
```

---

## 2. write_file

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "write_file",
  "tool_id": "write_file_<timestamp>_<n>",
  "parameters": {
    "file_path": "/tmp/gemini_samples/test_write.txt",
    "content": "hello gemini"
  }
}
```

### tool_result

```json
{
  "type": "tool_result",
  "tool_id": "write_file_<timestamp>_<n>",
  "status": "success",
  "output": "Successfully wrote to /tmp/gemini_samples/test_write.txt"
}
```

---

## 3. replace

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "replace",
  "tool_id": "replace_<timestamp>_<n>",
  "parameters": {
    "file_path": "/tmp/gemini_samples/test_write.txt",
    "old_string": "hello",
    "new_string": "goodbye",
    "instruction": "Replace hello with goodbye"
  }
}
```

### tool_result

```json
{
  "type": "tool_result",
  "tool_id": "replace_<timestamp>_<n>",
  "status": "success",
  "output": "Replaced 1 occurrence in /tmp/gemini_samples/test_write.txt"
}
```

---

## 4. list_directory

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "list_directory",
  "tool_id": "list_directory_<timestamp>_<n>",
  "parameters": {
    "dir_path": "/home/xulongzhe/projects/clawbench/internal/ai/"
  }
}
```

### tool_result

```json
{
  "type": "tool_result",
  "tool_id": "list_directory_<timestamp>_<n>",
  "status": "success",
  "output": "<directory listing>"
}
```

---

## 5. grep_search

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "grep_search",
  "tool_id": "grep_search_<timestamp>_<n>",
  "parameters": {
    "pattern": "func main",
    "dir_path": "/home/xulongzhe/projects/clawbench/cmd/"
  }
}
```

---

## 6. glob

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "glob",
  "tool_id": "glob_<timestamp>_<n>",
  "parameters": {
    "pattern": "**/*.go",
    "dir_path": "/home/xulongzhe/projects/clawbench/internal/ai/"
  }
}
```

---

## 7. run_shell_command

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "run_shell_command",
  "tool_id": "run_shell_command_<timestamp>_<n>",
  "parameters": {
    "command": "echo hello from gemini",
    "description": "Echo test message"
  }
}
```

---

## 8. google_web_search

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "google_web_search",
  "tool_id": "google_web_search_<timestamp>_<n>",
  "parameters": {
    "query": "Go programming language"
  }
}
```

---

## 9. web_fetch

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "web_fetch",
  "tool_id": "web_fetch_<timestamp>_<n>",
  "parameters": {
    "prompt": "Fetch https://example.com and summarize the page"
  }
}
```

---

## 10. save_memory

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "save_memory",
  "tool_id": "save_memory_<timestamp>_<n>",
  "parameters": {
    "fact": "This is a test",
    "scope": "project"
  }
}
```

---

## 11. enter_plan_mode

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "enter_plan_mode",
  "tool_id": "enter_plan_mode_<timestamp>_<n>",
  "parameters": {
    "reason": "testing plan mode"
  }
}
```

---

## 12. invoke_agent

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "invoke_agent",
  "tool_id": "invoke_agent_<timestamp>_<n>",
  "parameters": {
    "agent_name": "generalist",
    "prompt": "list files in the current directory"
  }
}
```

---

## 13. activate_skill

### tool_use

```json
{
  "type": "tool_use",
  "tool_name": "activate_skill",
  "tool_id": "activate_skill_<timestamp>_<n>",
  "parameters": {
    "name": "find-skills"
  }
}
```

---

## Full Stream JSON Structure

```
1. {"type":"init","session_id":"...","model":"gemini-3-flash-preview"}  — Session init
2. {"type":"message","role":"user","content":"..."}                     — User prompt
3. {"type":"message","role":"assistant","content":"...","delta":true}   — Streaming text
4. {"type":"tool_use","tool_name":"...","tool_id":"...","parameters":{}}— Tool call
5. {"type":"tool_result","tool_id":"...","status":"success","output":"."}— Tool result
6. {"type":"message","role":"assistant","content":"...","delta":true}   — Final response
7. {"type":"result","status":"success","stats":{...}}                   — Session result
```

### Key fields in `result`:

```json
{
  "type": "result",
  "status": "success",
  "stats": {
    "total_tokens": 26020,
    "input_tokens": 25805,
    "output_tokens": 119,
    "cached": 7615,
    "duration_ms": 48680,
    "tool_calls": 1,
    "models": {
      "gemini-3-flash-preview": {
        "total_tokens": 22696,
        "input_tokens": 22606,
        "output_tokens": 90
      }
    }
  }
}
```

---

## Key Observations

1. **Gemini tool IDs** follow the pattern `<tool_name>_<timestamp>_<n>` (e.g., `read_file_1780833496116_0`).
2. **Tool parameters use snake_case** — `file_path`, `start_line`, `dir_path`, etc. (different from CodeBuddy/Claude's camelCase).
3. **`tool_use` and `tool_result` are top-level entries** (not nested in messages, unlike Claude).
4. **`tool_result.output` is a plain string** — no nested `{ type: "text", text: "..." }` wrapper.
5. **Gemini tool names are snake_case** — `read_file`, `write_file`, `run_shell_command`, `google_web_search`, etc.
6. **`replace` tool requires `instruction` field** — a semantic description of the change (unique to Gemini).
7. **`web_fetch` uses a single `prompt` field** — the URL is embedded in the prompt text, not a separate parameter.
8. **Gemini has no `Grep`/`Glob`/`Edit`/`Bash` naming** — it uses `grep_search`, `glob`, `replace`, `run_shell_command` instead.
9. **Unique tools**: `list_directory`, `save_memory`, `invoke_agent`, `activate_skill`, `list_background_processes`, `read_background_output`.
