# OpenCode Tool Call JSON Samples

This document contains actual JSON output samples for OpenCode tool calls, captured via:

```bash
echo "<prompt>" | opencode run --format json
```

## JSON Structure Overview

OpenCode `run --format json` produces newline-delimited JSON objects. Each line has a `type` field:

| Type | Description |
|------|-------------|
| `step_start` | Beginning of a processing step |
| `step_finish` | End of a processing step |
| `text` | Text output from the assistant |
| `tool_use` | Tool call (with result embedded in `part.state`) |

### Key Difference

OpenCode's `tool_use` entry contains **both the call and the result** in a single object — the `part.state` field includes `input`, `output`, `status`, and `metadata` all together.

---

## 1. Read

### tool_use (complete with result)

```json
{
  "type": "tool_use",
  "timestamp": 1780833473432,
  "sessionID": "ses_15e0dbdf4ffeQTwYZY7ryU516Y",
  "part": {
    "type": "tool",
    "tool": "read",
    "callID": "call_81511c9a043c438ea91d9275",
    "state": {
      "status": "completed",
      "input": {
        "filePath": "/home/xulongzhe/projects/clawbench/go.mod",
        "limit": 3
      },
      "output": "<path>/home/xulongzhe/projects/clawbench/go.mod</path>\n<type>file</type>\n<content>\n1: module clawbench\n2: \n3: go 1.25.0\n\n(Showing lines 1-3 of 37. Use offset=4 to continue.)\n</content>",
      "metadata": {
        "preview": "module clawbench\n\ngo 1.25.0",
        "truncated": true,
        "loaded": []
      },
      "title": "go.mod",
      "time": {
        "start": 1780833473413,
        "end": 1780833473430
      }
    },
    "id": "prt_ea1f251c9001JDSz7k1QJ7D0b0",
    "sessionID": "ses_15e0dbdf4ffeQTwYZY7ryU516Y",
    "messageID": "msg_ea1f24330001vLAznZd4apPljt"
  }
}
```

---

## 2. Write

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "write",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "filePath": "/tmp/opencode_samples/test_write.txt",
        "content": "hello opencode"
      },
      "output": "<path>/tmp/opencode_samples/test_write.txt</path><type>file</type><content>Successfully wrote to /tmp/opencode_samples/test_write.txt</content>",
      "metadata": {
        "preview": "Successfully wrote to /tmp/opencode_samples/test_write.txt",
        "truncated": false,
        "loaded": []
      },
      "title": "test_write.txt",
      "time": { "start": ..., "end": ... }
    }
  }
}
```

---

## 3. Edit

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "edit",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "filePath": "/tmp/opencode_samples/test_write.txt",
        "oldString": "hello",
        "newString": "goodbye"
      },
      "output": "<path>...</path><type>file</type><content>...</content>",
      "metadata": { ... },
      "title": "test_write.txt"
    }
  }
}
```

---

## 4. Bash

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "bash",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "command": "echo hello from opencode",
        "description": "Echo test message"
      },
      "output": "hello from opencode",
      "metadata": { ... },
      "title": "echo hello from opencode"
    }
  }
}
```

---

## 5. Glob

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "glob",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "pattern": "**/*.go",
        "path": "/home/xulongzhe/projects/clawbench/internal/ai"
      },
      "output": "<list of matching files>",
      "metadata": { ... }
    }
  }
}
```

---

## 6. Grep

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "grep",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "pattern": "func main",
        "path": "/home/xulongzhe/projects/clawbench/cmd/"
      },
      "output": "<search results>",
      "metadata": { ... }
    }
  }
}
```

---

## 7. TodoWrite

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "todowrite",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "todos": [
          {
            "content": "Test task",
            "status": "pending",
            "priority": "medium"
          }
        ]
      },
      "output": "<todo list result>",
      "metadata": { ... }
    }
  }
}
```

---

## 8. EnterPlanMode

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "enterplanmode",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {},
      "output": "<plan mode entry message>",
      "metadata": { ... }
    }
  }
}
```

---

## 9. ExitPlanMode

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "exitplanmode",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "allowedPrompts": [
          { "tool": "Bash", "prompt": "run tests" }
        ]
      },
      "output": "<plan mode exit message>",
      "metadata": { ... }
    }
  }
}
```

---

## 10. Skill

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "skill",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "name": "help"
      },
      "output": "<skill output>",
      "metadata": { ... }
    }
  }
}
```

---

## 11. Task (SubAgent)

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "task",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "description": "Explore ai directory",
        "prompt": "List all files in /home/xulongzhe/projects/clawbench/internal/ai/",
        "subagent_type": "explore"
      },
      "output": "<agent output>",
      "metadata": { ... }
    }
  }
}
```

---

## 12. WebFetch

### tool_use (pattern)

```json
{
  "type": "tool_use",
  "part": {
    "type": "tool",
    "tool": "webfetch",
    "callID": "call_<id>",
    "state": {
      "status": "completed",
      "input": {
        "url": "https://example.com",
        "format": "markdown"
      },
      "output": "<fetched content>",
      "metadata": { ... }
    }
  }
}
```

---

## Full Stream JSON Structure

```
1. {"type":"step_start","sessionID":"...","part":{...}}    — Step begin
2. {"type":"tool_use","sessionID":"...","part":{...}}     — Tool call+result (complete)
3. {"type":"step_finish","sessionID":"...","part":{...}}  — Step end
4. {"type":"step_start",...}                               — Next step
5. {"type":"text","sessionID":"...","part":{...}}         — Text output
6. {"type":"step_finish",...}                              — Final step end
```

---

## Key Observations

1. **OpenCode combines call and result in a single `tool_use` entry** — unlike Claude/CodeBuddy/Gemini which separate them into distinct events.
2. **Tool name is in `part.tool`** (lowercase: `"read"`, `"write"`, `"bash"`, `"glob"`).
3. **Call ID is in `part.callID`** — format `call_<hex>`.
4. **Input is in `part.state.input`** — a JSON object.
5. **Output is in `part.state.output`** — typically XML-formatted string with `<path>`, `<type>`, `<content>` tags.
6. **Metadata includes** `preview`, `truncated`, `loaded`, `title`, and `time` (start/end timestamps).
7. **Tool names are lowercase** in output: `read`, `write`, `edit`, `bash`, `glob`, `grep`, `todowrite`, `enterplanmode`, `exitplanmode`, `skill`, `task`, `webfetch`.
8. **`step_start`/`step_finish`** wrap each tool call and text output, providing session structure.
9. **No separate tool_result event** — everything is self-contained in the `tool_use` entry.
10. **Unique tools**: `TodoWrite` (batch task management with priority), `Task` (subagent with `subagent_type`).
