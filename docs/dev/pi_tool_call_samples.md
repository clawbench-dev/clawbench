# Pi CLI Tool Call JSON Samples

This document contains actual JSON output samples for Pi CLI tool calls, captured via:

```bash
echo "<prompt>" | pi -p --mode json --model minimax-cn/MiniMax-M2.7 --no-context-files
```

## JSON Structure Overview

Pi `--mode json` produces newline-delimited JSON objects. Each line has a `type` field:

| Type | Description |
|------|-------------|
| `session` | Session init (version, id, cwd) |
| `agent_start` | Agent begins |
| `turn_start` | Turn begins |
| `message_start` | Message begins (role, content initial) |
| `message_update` | Streaming message update (thinking, toolCall, text) |
| `message_end` | Message finalized (role, content complete, stopReason) |
| `tool_execution_start` | Tool execution begins (toolCallId, toolName, args) |
| `tool_execution_end` | Tool execution completes (toolCallId, result, isError) |
| `turn_end` | Turn ends (message with usage, toolResults) |
| `agent_end` | Agent finishes |
| `auto_retry_start/end` | Automatic retry on error |

---

## 1. read

### Tool Call (via message_update with toolCall)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "thinking", "text": "..." },
      {
        "type": "toolCall",
        "id": "call_bbd96f43dd2140138ca453fb",
        "name": "read",
        "arguments": {
          "path": "/home/xulongzhe/projects/clawbench/go.mod",
          "limit": 3
        }
      }
    ],
    "stopReason": "toolUse"
  }
}
```

### Tool Execution

```json
{
  "type": "tool_execution_start",
  "toolCallId": "call_bbd96f43dd2140138ca453fb",
  "toolName": "read",
  "args": {
    "path": "/home/xulongzhe/projects/clawbench/go.mod",
    "limit": 3
  }
}
```

```json
{
  "type": "tool_execution_end",
  "toolCallId": "call_bbd96f43dd2140138ca453fb",
  "toolName": "read",
  "result": {
    "content": [
      {
        "type": "text",
        "text": "module clawbench\n\ngo 1.25.0\n\n[35 more lines in file. Use offset=4 to continue.]"
      }
    ]
  },
  "isError": false
}
```

### Tool Result (in message_end)

```json
{
  "type": "message_end",
  "message": {
    "role": "toolResult",
    "toolCallId": "call_bbd96f43dd2140138ca453fb",
    "toolName": "read",
    "content": [
      {
        "type": "text",
        "text": "module clawbench\n\ngo 1.25.0\n\n[35 more lines in file. Use offset=4 to continue.]"
      }
    ],
    "isError": false,
    "timestamp": 1780834359126
  }
}
```

---

## 2. write

### Tool Call (pattern)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "toolCall", "id": "call_<id>", "name": "write", "arguments": { "path": "/tmp/pi_samples/test_write.txt", "content": "hello pi" } }
    ],
    "stopReason": "toolUse"
  }
}
```

### Tool Execution

```json
{
  "type": "tool_execution_start",
  "toolCallId": "call_<id>",
  "toolName": "write",
  "args": { "path": "/tmp/pi_samples/test_write.txt", "content": "hello pi" }
}
```

```json
{
  "type": "tool_execution_end",
  "toolCallId": "call_<id>",
  "toolName": "write",
  "result": { "content": [{ "type": "text", "text": "Successfully wrote to /tmp/pi_samples/test_write.txt" }] },
  "isError": false
}
```

---

## 3. edit

### Tool Call (pattern)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "toolCall", "id": "call_<id>", "name": "edit", "arguments": { "path": "/tmp/pi_samples/test_write.txt", "edits": [{ "oldText": "hello", "newText": "goodbye" }] } }
    ],
    "stopReason": "toolUse"
  }
}
```

### Tool Execution

```json
{
  "type": "tool_execution_start",
  "toolCallId": "call_<id>",
  "toolName": "edit",
  "args": { "path": "/tmp/pi_samples/test_write.txt", "edits": [{ "oldText": "hello", "newText": "goodbye" }] }
}
```

---

## 4. bash

### Tool Call (pattern)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "toolCall", "id": "call_<id>", "name": "bash", "arguments": { "command": "echo hello from pi" } }
    ],
    "stopReason": "toolUse"
  }
}
```

### Tool Execution

```json
{
  "type": "tool_execution_start",
  "toolCallId": "call_<id>",
  "toolName": "bash",
  "args": { "command": "echo hello from pi" }
}
```

```json
{
  "type": "tool_execution_end",
  "toolCallId": "call_<id>",
  "toolName": "bash",
  "result": { "content": [{ "type": "text", "text": "hello from pi" }] },
  "isError": false
}
```

---

## 5. grep

### Tool Call (pattern)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "toolCall", "id": "call_<id>", "name": "grep", "arguments": { "pattern": "func main", "path": "/home/xulongzhe/projects/clawbench/cmd/" } }
    ],
    "stopReason": "toolUse"
  }
}
```

---

## 6. find

### Tool Call (pattern)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "toolCall", "id": "call_<id>", "name": "find", "arguments": { "pattern": "**/*.go", "path": "/home/xulongzhe/projects/clawbench/internal/ai/" } }
    ],
    "stopReason": "toolUse"
  }
}
```

---

## 7. ls

### Tool Call (pattern)

```json
{
  "type": "message_update",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "toolCall", "id": "call_<id>", "name": "ls", "arguments": { "path": "/home/xulongzhe/projects/clawbench/internal/ai/" } }
    ],
    "stopReason": "toolUse"
  }
}
```

---

## Full Stream JSON Structure

```
1.  {"type":"session","version":"...","id":"...","cwd":"..."}       — Session init
2.  {"type":"agent_start"}                                          — Agent begins
3.  {"type":"turn_start"}                                           — Turn begins
4.  {"type":"message_start","message":{"role":"user",...}}          — User prompt
5.  {"type":"message_end","message":{"role":"user",...}}            — User prompt finalized
6.  {"type":"message_start","message":{"role":"assistant",...}}     — Assistant starts
7.  {"type":"message_update","message":{"role":"assistant",         — Thinking
      "content":[{"type":"thinking","text":"..."}]}}
8.  {"type":"message_update","message":{"role":"assistant",         — Tool call (streaming)
      "content":[{"type":"toolCall","name":"read",
        "arguments":{"path":""},"partialJson":"..."}]}}
9.  {"type":"message_update","message":{...}}                       — Tool call arguments accumulate
10. {"type":"message_end","message":{"role":"assistant",            — Assistant message finalized
      "content":[{"type":"toolCall","name":"read",
        "arguments":{"path":"...","limit":3}}],
      "stopReason":"toolUse"}}
11. {"type":"tool_execution_start","toolCallId":"...","toolName":"read","args":{...}} — Execution starts
12. {"type":"tool_execution_end","toolCallId":"...","toolName":"read","result":{...},"isError":false} — Execution ends
13. {"type":"message_start","message":{"role":"toolResult",...}}    — Tool result message starts
14. {"type":"message_end","message":{"role":"toolResult",...}}      — Tool result message ends
15. {"type":"turn_end","message":{"role":"assistant",...},"toolResults":[...]} — Turn ends
16. {"type":"turn_start"}                                           — Next turn (if needed)
17. {"type":"message_update","message":{"role":"assistant",         — Final text response
      "content":[{"type":"thinking","text":"..."},{"type":"text","text":"..."}]}}
18. {"type":"turn_end",...}                                         — Final turn ends
19. {"type":"agent_end","messages":[...],"willRetry":false}         — Agent finishes
```

### Key fields in `turn_end`:

```json
{
  "type": "turn_end",
  "message": {
    "role": "assistant",
    "content": [],
    "api": "anthropic-messages",
    "provider": "minimax",
    "model": "MiniMax-M2.7",
    "usage": {
      "input": 12345,
      "output": 234,
      "cacheRead": 0,
      "cacheWrite": 0,
      "totalTokens": 12579,
      "cost": { "input": 0.001, "output": 0.002, "total": 0.003 }
    },
    "stopReason": "toolUse"
  },
  "toolResults": [...]
}
```

### Key fields in `agent_end`:

```json
{
  "type": "agent_end",
  "messages": [ ... all messages ... ],
  "willRetry": false
}
```

---

## Streaming Tool Call Accumulation

Pi streams tool call arguments incrementally via `message_update` events. The `toolCall` block accumulates:

```json
// Step 1: Tool call starts, arguments empty
{ "type": "toolCall", "id": "call_bbd96f43dd2140138ca453fb", "name": "read", "arguments": {}, "partialJson": "", "index": 1 }

// Step 2: Partial arguments
{ "type": "toolCall", "id": "call_bbd96f43dd2140138ca453fb", "name": "read", "arguments": { "path": "" }, "partialJson": "{\"path\": \"", "index": 1 }

// Step 3: Arguments filling in
{ "type": "toolCall", "id": "call_bbd96f43dd2140138ca453fb", "name": "read", "arguments": { "path": "/home/xulongzhe" }, "partialJson": "{\"path\": \"/home/xulongzhe", "index": 1 }

// Step N: Final complete arguments (no partialJson)
{ "type": "toolCall", "id": "call_bbd96f43dd2140138ca453fb", "name": "read", "arguments": { "path": "/home/xulongzhe/projects/clawbench/go.mod", "limit": 3 } }
```

---

## Key Observations

1. **Pi tool calls are streamed incrementally** — `message_update` events contain partial `toolCall` blocks that accumulate arguments over time via `partialJson`.
2. **Three-phase tool execution**: `message_update` (tool call) → `tool_execution_start` (execution begins) → `tool_execution_end` (result available).
3. **Tool names are lowercase**: `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls`.
4. **Tool call ID**: `call_<hex>` format (e.g., `call_bbd96f43dd2140138ca453fb`).
5. **`edit` tool uses `edits` array** — supports multiple edit operations in a single call: `{ "path": "...", "edits": [{ "oldText": "...", "newText": "..." }] }`.
6. **`tool_execution_end.result`** contains the tool output in `{ "content": [{ "type": "text", "text": "..." }] }` format.
7. **`isError` boolean** in `tool_execution_end` and tool result messages.
8. **Pi has no built-in plan mode, web search, web fetch, or agent sub-tools** — only 7 core tools.
9. **`agent_end.messages`** contains the complete conversation history for the session.
10. **`stopReason: "toolUse"`** indicates the assistant stopped to execute a tool; `"stop"` means the assistant finished responding.
11. **Thinking blocks** are streamed via `message_update` with `{ "type": "thinking", "text": "..." }` content blocks.
12. **Auto-retry**: On API errors, Pi automatically retries with `auto_retry_start`/`auto_retry_end` events.
