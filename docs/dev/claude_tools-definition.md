# ClawBench 工具调用定义 — 完整 JSON Schema

本文件给出本会话中可用的全部工具的 **JSON Schema 结构**。
每个工具均使用与 Claude Code SDK / MCP 协议兼容的 JSON Schema 格式(Draft-07+)。

---

## 目录

- [核心 Agent / 任务管理工具](#核心-agent--任务管理工具)
- [文件与代码编辑工具](#文件与代码编辑工具)
- [搜索与信息检索工具](#搜索与信息检索工具)
- [Bash 与系统执行工具](#bash-与系统执行工具)
- [定时任务与监控工具](#定时任务与监控工具)
- [计划与流程管理工具](#计划与流程管理工具)
- [通知与输出工具](#通知与输出工具)
- [MCP: Chrome DevTools 工具](#mcp-chrome-devtools-工具)
- [MCP: Tavily 网络搜索工具](#mcp-tavily-网络搜索工具)
- [MCP 资源工具](#mcp-资源工具)
- [汇总: 全部工具名清单](#汇总-全部工具名清单)

---

## 核心 Agent / 任务管理工具

### Agent

```json
{
  "name": "Agent",
  "description": "Launch a new agent to handle complex, multi-step tasks. Each agent type has specific capabilities and tools available to it.",
  "input_schema": {
    "type": "object",
    "properties": {
      "description": {
        "type": "string",
        "description": "A short (3-5 word) description of the task"
      },
      "prompt": {
        "type": "string",
        "description": "The task for the agent to perform"
      },
      "subagent_type": {
        "type": "string",
        "enum": ["claude", "Explore", "general-purpose", "Plan", "statusline-setup"],
        "description": "The type of specialized agent to use for this task"
      },
      "model": {
        "type": "string",
        "enum": ["sonnet", "opus", "haiku"],
        "description": "Optional model override for this agent"
      },
      "run_in_background": {
        "type": "boolean",
        "description": "Set to true to run this agent asynchronously"
      },
      "isolation": {
        "type": "string",
        "enum": ["worktree"],
        "description": "Isolation mode. 'worktree' creates a temporary git worktree"
      }
    },
    "required": ["description", "prompt"]
  }
}
```

### TaskOutput

```json
{
  "name": "TaskOutput",
  "description": "DEPRECATED: Retrieves output from a running or completed task",
  "input_schema": {
    "type": "object",
    "properties": {
      "task_id": { "type": "string" },
      "block": { "type": "boolean" },
      "timeout": { "type": "number", "default": 30000, "minimum": 0, "maximum": 600000 }
    },
    "required": ["task_id", "block", "timeout"]
  }
}
```

### TaskStop

```json
{
  "name": "TaskStop",
  "description": "Stops a running background task by its ID",
  "input_schema": {
    "type": "object",
    "properties": {
      "task_id": { "type": "string" },
      "shell_id": { "type": "string", "deprecated": true }
    },
    "required": ["task_id"]
  }
}
```

### TaskCreate

```json
{
  "name": "TaskCreate",
  "description": "Create a structured task list for the current coding session",
  "input_schema": {
    "type": "object",
    "properties": {
      "subject": { "type": "string" },
      "description": { "type": "string" },
      "activeForm": { "type": "string" },
      "metadata": { "type": "object", "propertyNames": { "type": "array", "items": { "type": "string" } }, "additionalProperties": true }
    },
    "required": ["subject", "description"]
  }
}
```

### TaskList

```json
{
  "name": "TaskList",
  "description": "List all tasks in the task list",
  "input_schema": { "type": "object", "properties": {} }
}
```

### TaskGet

```json
{
  "name": "TaskGet",
  "description": "Retrieve a task by its ID",
  "input_schema": {
    "type": "object",
    "properties": { "taskId": { "type": "string" } },
    "required": ["taskId"]
  }
}
```

### TaskUpdate

```json
{
  "name": "TaskUpdate",
  "description": "Update task in the task list",
  "input_schema": {
    "type": "object",
    "properties": {
      "taskId": { "type": "string" },
      "subject": { "type": "string" },
      "description": { "type": "string" },
      "activeForm": { "type": "string" },
      "status": { "type": "string", "enum": ["pending", "in_progress", "completed", "deleted"] },
      "addBlocks": { "type": "array", "items": { "type": "string" } },
      "addBlockedBy": { "type": "array", "items": { "type": "string" } },
      "owner": { "type": "string" },
      "metadata": { "type": "object", "propertyNames": { "type": "array", "items": { "type": "string" } }, "additionalProperties": true }
    },
    "required": ["taskId"]
  }
}
```

### Workflow

```json
{
  "name": "Workflow",
  "description": "Execute a workflow script that orchestrates multiple subagents deterministically",
  "input_schema": {
    "type": "object",
    "properties": {
      "script": { "type": "string", "maxLength": 524288, "description": "Self-contained workflow script" },
      "name": { "type": "string", "description": "Name of a predefined workflow" },
      "description": { "type": "string", "description": "Ignored" },
      "title": { "type": "string", "description": "Ignored" },
      "args": { "description": "Optional input value exposed to the script as the global 'args'" },
      "scriptPath": { "type": "string", "description": "Path to a workflow script file on disk" },
      "resumeFromRunId": { "type": "string", "pattern": "^wf_[a-z0-9-]{6,}$" }
    }
  }
}
```

---

## 文件与代码编辑工具

### Read

```json
{
  "name": "Read",
  "description": "Reads a file from the local filesystem",
  "input_schema": {
    "type": "object",
    "properties": {
      "file_path": { "type": "string" },
      "offset": { "type": "number", "minimum": 0, "maximum": 9007199254740991 },
      "limit": { "type": "number", "exclusiveMinimum": 0, "maximum": 9007199254740991 },
      "pages": { "type": "string", "description": "Page range for PDF files" }
    },
    "required": ["file_path"]
  }
}
```

### Write

```json
{
  "name": "Write",
  "description": "Writes a file to the local filesystem, overwriting if one exists",
  "input_schema": {
    "type": "object",
    "properties": {
      "file_path": { "type": "string" },
      "content": { "type": "string" }
    },
    "required": ["file_path", "content"]
  }
}
```

### Edit

```json
{
  "name": "Edit",
  "description": "Performs exact string replacement in a file",
  "input_schema": {
    "type": "object",
    "properties": {
      "file_path": { "type": "string" },
      "old_string": { "type": "string" },
      "new_string": { "type": "string" },
      "replace_all": { "type": "boolean", "default": false }
    },
    "required": ["file_path", "old_string", "new_string"]
  }
}
```

### NotebookEdit

```json
{
  "name": "NotebookEdit",
  "description": "Replaces, inserts, or deletes a single cell in a Jupyter notebook",
  "input_schema": {
    "type": "object",
    "properties": {
      "notebook_path": { "type": "string" },
      "cell_id": { "type": "string" },
      "new_source": { "type": "string" },
      "cell_type": { "type": "string", "enum": ["code", "markdown"] },
      "edit_mode": { "type": "string", "enum": ["replace", "insert", "delete"], "default": "replace" }
    },
    "required": ["notebook_path", "new_source"]
  }
}
```

### LSP

```json
{
  "name": "LSP",
  "description": "Interact with Language Server Protocol (LSP) servers to get code intelligence features",
  "input_schema": {
    "type": "object",
    "properties": {
      "operation": {
        "type": "string",
        "enum": [
          "goToDefinition", "findReferences", "hover", "documentSymbol",
          "workspaceSymbol", "goToImplementation", "prepareCallHierarchy",
          "incomingCalls", "outgoingCalls"
        ]
      },
      "filePath": { "type": "string" },
      "line": { "type": "integer", "exclusiveMinimum": 0, "maximum": 9007199254740991 },
      "character": { "type": "integer", "exclusiveMinimum": 0, "maximum": 9007199254740991 },
      "query": { "type": "string", "description": "workspaceSymbol only" }
    },
    "required": ["operation", "filePath", "line", "character"]
  }
}
```

---

## 搜索与信息检索工具

### WebSearch

```json
{
  "name": "WebSearch",
  "description": "Search the web. Returns result blocks with titles and URLs. US-only.",
  "input_schema": {
    "type": "object",
    "properties": {
      "query": { "type": "string", "minLength": 2 },
      "allowed_domains": { "type": "array", "items": { "type": "string" } },
      "blocked_domains": { "type": "array", "items": { "type": "string" } }
    },
    "required": ["query"]
  }
}
```

### WebFetch

```json
{
  "name": "WebFetch",
  "description": "Fetches a URL, converts the page to markdown, and answers 'prompt' against it using a small fast model",
  "input_schema": {
    "type": "object",
    "properties": {
      "url": { "type": "string", "format": "uri" },
      "prompt": { "type": "string" }
    },
    "required": ["url", "prompt"]
  }
}
```

---

## Bash 与系统执行工具

### Bash

```json
{
  "name": "Bash",
  "description": "Executes a bash command and returns its output",
  "input_schema": {
    "type": "object",
    "properties": {
      "command": { "type": "string" },
      "timeout": { "type": "number", "maximum": 600000, "description": "Optional timeout in milliseconds" },
      "description": { "type": "string" },
      "run_in_background": { "type": "boolean" },
      "dangerouslyDisableSandbox": { "type": "boolean" }
    },
    "required": ["command"]
  }
}
```

### EnterWorktree

```json
{
  "name": "EnterWorktree",
  "description": "Create an isolated git worktree (only when explicitly instructed)",
  "input_schema": {
    "type": "object",
    "properties": {
      "name": { "type": "string" },
      "path": { "type": "string" }
    }
  }
}
```

### ExitWorktree

```json
{
  "name": "ExitWorktree",
  "description": "Exit a worktree session created by EnterWorktree",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": { "type": "string", "enum": ["keep", "remove"] },
      "discard_changes": { "type": "boolean", "default": false }
    },
    "required": ["action"]
  }
}
```

---

## 定时任务与监控工具

### CronCreate

```json
{
  "name": "CronCreate",
  "description": "Schedule a prompt to be enqueued at a future time",
  "input_schema": {
    "type": "object",
    "properties": {
      "cron": { "type": "string", "description": "Standard 5-field cron expression" },
      "prompt": { "type": "string" },
      "recurring": { "type": "boolean", "default": true },
      "durable": { "type": "boolean", "default": false }
    },
    "required": ["cron", "prompt"]
  }
}
```

### CronDelete

```json
{
  "name": "CronDelete",
  "description": "Cancel a cron job previously scheduled with CronCreate",
  "input_schema": {
    "type": "object",
    "properties": { "id": { "type": "string" } },
    "required": ["id"]
  }
}
```

### CronList

```json
{
  "name": "CronList",
  "description": "List all cron jobs scheduled via CronCreate in this session",
  "input_schema": { "type": "object", "properties": {} }
}
```

### ScheduleWakeup

```json
{
  "name": "ScheduleWakeup",
  "description": "Schedule when to resume work in /loop dynamic mode",
  "input_schema": {
    "type": "object",
    "properties": {
      "delaySeconds": { "type": "number", "description": "Seconds from now to wake up. Clamped to [60, 3600]" },
      "reason": { "type": "string" },
      "prompt": { "type": "string" }
    },
    "required": ["delaySeconds", "reason", "prompt"]
  }
}
```

### Monitor

```json
{
  "name": "Monitor",
  "description": "Start a background monitor that streams events from a long-running script",
  "input_schema": {
    "type": "object",
    "properties": {
      "description": { "type": "string" },
      "timeout_ms": { "type": "number", "default": 300000, "minimum": 1000, "maximum": 3600000 },
      "persistent": { "type": "boolean", "default": false },
      "command": { "type": "string" }
    },
    "required": ["description", "timeout_ms", "persistent", "command"]
  }
}
```

---

## 计划与流程管理工具

### EnterPlanMode

```json
{
  "name": "EnterPlanMode",
  "description": "Use this tool proactively when you're about to start a non-trivial implementation task",
  "input_schema": { "type": "object", "properties": {} }
}
```

### ExitPlanMode

```json
{
  "name": "ExitPlanMode",
  "description": "Use this tool when you are in plan mode and have finished writing your plan",
  "input_schema": {
    "type": "object",
    "properties": {
      "allowedPrompts": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "tool": { "type": "string", "enum": ["Bash"] },
            "prompt": { "type": "string" }
          },
          "required": ["tool", "prompt"]
        }
      }
    }
  }
}
```

---

## 通知与输出工具

### PushNotification

```json
{
  "name": "PushNotification",
  "description": "Sends a desktop notification in the user's terminal",
  "input_schema": {
    "type": "object",
    "properties": {
      "message": { "type": "string", "minLength": 1, "maxLength": 200 },
      "status": { "type": "string", "const": "proactive" }
    },
    "required": ["message", "status"]
  }
}
```

### Skill

```json
{
  "name": "Skill",
  "description": "Execute a skill within the main conversation",
  "input_schema": {
    "type": "object",
    "properties": {
      "skill": { "type": "string" },
      "args": { "type": "string" }
    },
    "required": ["skill"]
  }
}
```

---

## MCP: Chrome DevTools 工具

### mcp__chrome-devtools__list_pages

```json
{
  "name": "mcp__chrome-devtools__list_pages",
  "description": "Get a list of pages open in the browser",
  "input_schema": { "type": "object", "properties": {} }
}
```

### mcp__chrome-devtools__new_page

```json
{
  "name": "mcp__chrome-devtools__new_page",
  "description": "Open a new tab and load a URL",
  "input_schema": {
    "type": "object",
    "properties": {
      "url": { "type": "string" },
      "background": { "type": "boolean", "default": false },
      "isolatedContext": { "type": "string" },
      "timeout": { "type": "integer" }
    },
    "required": ["url"]
  }
}
```

### mcp__chrome-devtools__close_page

```json
{
  "name": "mcp__chrome-devtools__close_page",
  "description": "Closes the page by its index. The last open page cannot be closed",
  "input_schema": {
    "type": "object",
    "properties": { "pageId": { "type": "number" } },
    "required": ["pageId"]
  }
}
```

### mcp__chrome-devtools__select_page

```json
{
  "name": "mcp__chrome-devtools__select_page",
  "description": "Select a page as a context for future tool calls",
  "input_schema": {
    "type": "object",
    "properties": {
      "pageId": { "type": "number" },
      "bringToFront": { "type": "boolean" }
    },
    "required": ["pageId"]
  }
}
```

### mcp__chrome-devtools__navigate_page

```json
{
  "name": "mcp__chrome-devtools__navigate_page",
  "description": "Go to a URL, or back, forward, or reload",
  "input_schema": {
    "type": "object",
    "properties": {
      "type": { "type": "string", "enum": ["url", "back", "forward", "reload"] },
      "url": { "type": "string" },
      "ignoreCache": { "type": "boolean" },
      "handleBeforeUnload": { "type": "string", "enum": ["accept", "decline"] },
      "initScript": { "type": "string" },
      "timeout": { "type": "integer" }
    },
    "required": ["type"]
  }
}
```

### mcp__chrome-devtools__take_snapshot

```json
{
  "name": "mcp__chrome-devtools__take_snapshot",
  "description": "Take a text snapshot of the currently selected page based on the a11y tree",
  "input_schema": {
    "type": "object",
    "properties": {
      "verbose": { "type": "boolean", "default": false },
      "filePath": { "type": "string" }
    }
  }
}
```

### mcp__chrome-devtools__take_screenshot

```json
{
  "name": "mcp__chrome-devtools__take_screenshot",
  "description": "Take a screenshot of the page or element",
  "input_schema": {
    "type": "object",
    "properties": {
      "format": { "type": "string", "enum": ["png", "jpeg", "webp"], "default": "png" },
      "quality": { "type": "number", "minimum": 0, "maximum": 100 },
      "uid": { "type": "string" },
      "fullPage": { "type": "boolean" },
      "filePath": { "type": "string" }
    }
  }
}
```

### mcp__chrome-devtools__click

```json
{
  "name": "mcp__chrome-devtools__click",
  "description": "Clicks on the provided element",
  "input_schema": {
    "type": "object",
    "properties": {
      "uid": { "type": "string" },
      "dblClick": { "type": "boolean", "default": false },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["uid"]
  }
}
```

### mcp__chrome-devtools__hover

```json
{
  "name": "mcp__chrome-devtools__hover",
  "description": "Hover over the provided element",
  "input_schema": {
    "type": "object",
    "properties": {
      "uid": { "type": "string" },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["uid"]
  }
}
```

### mcp__chrome-devtools__drag

```json
{
  "name": "mcp__chrome-devtools__drag",
  "description": "Drag an element onto another element",
  "input_schema": {
    "type": "object",
    "properties": {
      "from_uid": { "type": "string" },
      "to_uid": { "type": "string" },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["from_uid", "to_uid"]
  }
}
```

### mcp__chrome-devtools__fill

```json
{
  "name": "mcp__chrome-devtools__fill",
  "description": "Type text into an input, text area or select an option from a <select> element",
  "input_schema": {
    "type": "object",
    "properties": {
      "uid": { "type": "string" },
      "value": { "type": "string" },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["uid", "value"]
  }
}
```

### mcp__chrome-devtools__fill_form

```json
{
  "name": "mcp__chrome-devtools__fill_form",
  "description": "Fill out multiple form elements at once",
  "input_schema": {
    "type": "object",
    "properties": {
      "elements": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "uid": { "type": "string" },
            "value": { "type": "string" }
          },
          "required": ["uid", "value"]
        }
      },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["elements"]
  }
}
```

### mcp__chrome-devtools__type_text

```json
{
  "name": "mcp__chrome-devtools__type_text",
  "description": "Type text using keyboard into a previously focused input",
  "input_schema": {
    "type": "object",
    "properties": {
      "text": { "type": "string" },
      "submitKey": { "type": "string" }
    },
    "required": ["text"]
  }
}
```

### mcp__chrome-devtools__press_key

```json
{
  "name": "mcp__chrome-devtools__press_key",
  "description": "Press a key or key combination",
  "input_schema": {
    "type": "object",
    "properties": {
      "key": { "type": "string", "description": "e.g. 'Enter', 'Control+A'" },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["key"]
  }
}
```

### mcp__chrome-devtools__upload_file

```json
{
  "name": "mcp__chrome-devtools__upload_file",
  "description": "Upload a file through a provided element",
  "input_schema": {
    "type": "object",
    "properties": {
      "uid": { "type": "string" },
      "filePath": { "type": "string" },
      "includeSnapshot": { "type": "boolean", "default": false }
    },
    "required": ["uid", "filePath"]
  }
}
```

### mcp__chrome-devtools__handle_dialog

```json
{
  "name": "mcp__chrome-devtools__handle_dialog",
  "description": "If a browser dialog was opened, use this command to handle it",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": { "type": "string", "enum": ["accept", "dismiss"] },
      "promptText": { "type": "string" }
    },
    "required": ["action"]
  }
}
```

### mcp__chrome-devtools__wait_for

```json
{
  "name": "mcp__chrome-devtools__wait_for",
  "description": "Wait for the specified text to appear on the selected page",
  "input_schema": {
    "type": "object",
    "properties": {
      "text": { "type": "array", "items": { "type": "string" }, "minItems": 1 },
      "timeout": { "type": "integer" }
    },
    "required": ["text"]
  }
}
```

### mcp__chrome-devtools__evaluate_script

```json
{
  "name": "mcp__chrome-devtools__evaluate_script",
  "description": "Evaluate a JavaScript function inside the currently selected page",
  "input_schema": {
    "type": "object",
    "properties": {
      "function": { "type": "string", "description": "A JavaScript function declaration" },
      "args": { "type": "array", "items": { "type": "string" }, "description": "uid list" }
    },
    "required": ["function"]
  }
}
```

### mcp__chrome-devtools__emulate

```json
{
  "name": "mcp__chrome-devtools__emulate",
  "description": "Emulates various features on the selected page",
  "input_schema": {
    "type": "object",
    "properties": {
      "networkConditions": { "type": "string", "enum": ["Offline", "Slow 3G", "Fast 3G", "Slow 4G", "Fast 4G"] },
      "cpuThrottlingRate": { "type": "number", "minimum": 1, "maximum": 20 },
      "geolocation": { "type": "string", "description": "lat x lon" },
      "userAgent": { "type": "string" },
      "colorScheme": { "type": "string", "enum": ["dark", "light", "auto"] },
      "viewport": { "type": "string", "description": "WxHxDPR[,mobile][,touch][,landscape]" }
    }
  }
}
```

### mcp__chrome-devtools__resize_page

```json
{
  "name": "mcp__chrome-devtools__resize_page",
  "description": "Resizes the selected page's window",
  "input_schema": {
    "type": "object",
    "properties": {
      "width": { "type": "number" },
      "height": { "type": "number" }
    },
    "required": ["width", "height"]
  }
}
```

### mcp__chrome-devtools__list_console_messages

```json
{
  "name": "mcp__chrome-devtools__list_console_messages",
  "description": "List all console messages for the currently selected page",
  "input_schema": {
    "type": "object",
    "properties": {
      "pageSize": { "type": "integer", "exclusiveMinimum": 0 },
      "pageIdx": { "type": "integer", "minimum": 0 },
      "types": { "type": "array", "items": { "type": "string", "enum": ["log","debug","info","error","warn","dir","dirxml","table","trace","clear","startGroup","startGroupCollapsed","endGroup","assert","profile","profileEnd","count","timeEnd","verbose","issue"] } },
      "includePreservedMessages": { "type": "boolean", "default": false }
    }
  }
}
```

### mcp__chrome-devtools__get_console_message

```json
{
  "name": "mcp__chrome-devtools__get_console_message",
  "description": "Gets a console message by its ID",
  "input_schema": {
    "type": "object",
    "properties": { "msgid": { "type": "number" } },
    "required": ["msgid"]
  }
}
```

### mcp__chrome-devtools__list_network_requests

```json
{
  "name": "mcp__chrome-devtools__list_network_requests",
  "description": "List all requests for the currently selected page",
  "input_schema": {
    "type": "object",
    "properties": {
      "pageSize": { "type": "integer", "exclusiveMinimum": 0 },
      "pageIdx": { "type": "integer", "minimum": 0 },
      "resourceTypes": { "type": "array", "items": { "type": "string", "enum": ["document","stylesheet","image","media","font","script","texttrack","xhr","fetch","prefetch","eventsource","websocket","manifest","signedexchange","ping","cspviolationreport","preflight","fedcm","other"] } },
      "includePreservedRequests": { "type": "boolean", "default": false }
    }
  }
}
```

### mcp__chrome-devtools__get_network_request

```json
{
  "name": "mcp__chrome-devtools__get_network_request",
  "description": "Gets a network request by an optional reqid",
  "input_schema": {
    "type": "object",
    "properties": {
      "reqid": { "type": "number" },
      "requestFilePath": { "type": "string" },
      "responseFilePath": { "type": "string" }
    }
  }
}
```

### mcp__chrome-devtools__performance_start_trace

```json
{
  "name": "mcp__chrome-devtools__performance_start_trace",
  "description": "Start a performance trace on the selected webpage",
  "input_schema": {
    "type": "object",
    "properties": {
      "reload": { "type": "boolean", "default": true },
      "autoStop": { "type": "boolean", "default": true },
      "filePath": { "type": "string" }
    }
  }
}
```

### mcp__chrome-devtools__performance_stop_trace

```json
{
  "name": "mcp__chrome-devtools__performance_stop_trace",
  "description": "Stop the active performance trace recording",
  "input_schema": {
    "type": "object",
    "properties": { "filePath": { "type": "string" } }
  }
}
```

### mcp__chrome-devtools__performance_analyze_insight

```json
{
  "name": "mcp__chrome-devtools__performance_analyze_insight",
  "description": "Provides more detailed information on a specific Performance Insight",
  "input_schema": {
    "type": "object",
    "properties": {
      "insightSetId": { "type": "string" },
      "insightName": { "type": "string" }
    },
    "required": ["insightSetId", "insightName"]
  }
}
```

### mcp__chrome-devtools__take_memory_snapshot

```json
{
  "name": "mcp__chrome-devtools__take_memory_snapshot",
  "description": "Capture a heap snapshot of the currently selected page",
  "input_schema": {
    "type": "object",
    "properties": { "filePath": { "type": "string" } },
    "required": ["filePath"]
  }
}
```

### mcp__chrome-devtools__lighthouse_audit

```json
{
  "name": "mcp__chrome-devtools__lighthouse_audit",
  "description": "Get Lighthouse score and reports for accessibility, SEO and best practices",
  "input_schema": {
    "type": "object",
    "properties": {
      "mode": { "type": "string", "enum": ["navigation", "snapshot"], "default": "navigation" },
      "device": { "type": "string", "enum": ["desktop", "mobile"], "default": "desktop" },
      "outputDirPath": { "type": "string" }
    }
  }
}
```

---

## MCP: Tavily 网络搜索工具

### mcp__tavily__tavily-search

```json
{
  "name": "mcp__tavily__tavily-search",
  "description": "A powerful web search tool that provides comprehensive, real-time results using Tavily's AI search engine",
  "input_schema": {
    "type": "object",
    "properties": {
      "query": { "type": "string" },
      "search_depth": { "type": "string", "enum": ["basic", "advanced"], "default": "basic" },
      "topic": { "type": "string", "enum": ["general", "news"], "default": "general" },
      "days": { "type": "number", "description": "Number of days back (news topic only)" },
      "time_range": { "type": "string", "enum": ["day", "week", "month", "year", "d", "w", "m", "y"] },
      "max_results": { "type": "number", "default": 10, "minimum": 5, "maximum": 20 },
      "include_images": { "type": "boolean", "default": false },
      "include_image_descriptions": { "type": "boolean", "default": false },
      "include_raw_content": { "type": "boolean", "default": false },
      "include_domains": { "type": "array", "items": { "type": "string" }, "default": [] },
      "exclude_domains": { "type": "array", "items": { "type": "string" } }
    },
    "required": ["query"]
  }
}
```

### mcp__tavily__tavily-extract

```json
{
  "name": "mcp__tavily__tavily-extract",
  "description": "A powerful web content extraction tool that retrieves and processes raw content from specified URLs",
  "input_schema": {
    "type": "object",
    "properties": {
      "urls": { "type": "array", "items": { "type": "string" } },
      "extract_depth": { "type": "string", "enum": ["basic", "advanced"], "default": "basic" },
      "include_images": { "type": "boolean", "default": false }
    },
    "required": ["urls"]
  }
}
```

---

## MCP 资源工具

### ListMcpResourcesTool

```json
{
  "name": "ListMcpResourcesTool",
  "description": "List available resources from configured MCP servers",
  "input_schema": {
    "type": "object",
    "properties": { "server": { "type": "string" } }
  }
}
```

### ReadMcpResourceTool

```json
{
  "name": "ReadMcpResourceTool",
  "description": "Reads a specific resource from an MCP server, identified by server name and resource URI",
  "input_schema": {
    "type": "object",
    "properties": {
      "server": { "type": "string" },
      "uri": { "type": "string" }
    },
    "required": ["server", "uri"]
  }
}
```

---

## 汇总: 全部工具名清单

### Claude Code SDK 内置工具 (29 个)

| 工具名 | 类别 |
|---|---|
| `Agent` | 任务管理 |
| `TaskCreate` | 任务管理 |
| `TaskGet` | 任务管理 |
| `TaskList` | 任务管理 |
| `TaskOutput` | 任务管理 |
| `TaskStop` | 任务管理 |
| `TaskUpdate` | 任务管理 |
| `Workflow` | 任务管理 |
| `Read` | 文件编辑 |
| `Write` | 文件编辑 |
| `Edit` | 文件编辑 |
| `NotebookEdit` | 文件编辑 |
| `LSP` | 文件编辑 |
| `WebSearch` | 检索 |
| `WebFetch` | 检索 |
| `Bash` | 系统执行 |
| `EnterWorktree` | 系统执行 |
| `ExitWorktree` | 系统执行 |
| `CronCreate` | 定时/监控 |
| `CronDelete` | 定时/监控 |
| `CronList` | 定时/监控 |
| `ScheduleWakeup` | 定时/监控 |
| `Monitor` | 定时/监控 |
| `EnterPlanMode` | 计划 |
| `ExitPlanMode` | 计划 |
| `PushNotification` | 输出 |
| `Skill` | 输出 |

### MCP: Chrome DevTools (29 个)

| 工具名 | 类别 |
|---|---|
| `mcp__chrome-devtools__list_pages` | 页面管理 |
| `mcp__chrome-devtools__new_page` | 页面管理 |
| `mcp__chrome-devtools__close_page` | 页面管理 |
| `mcp__chrome-devtools__select_page` | 页面管理 |
| `mcp__chrome-devtools__navigate_page` | 页面管理 |
| `mcp__chrome-devtools__take_snapshot` | 观察 |
| `mcp__chrome-devtools__take_screenshot` | 观察 |
| `mcp__chrome-devtools__list_console_messages` | 观察 |
| `mcp__chrome-devtools__get_console_message` | 观察 |
| `mcp__chrome-devtools__list_network_requests` | 观察 |
| `mcp__chrome-devtools__get_network_request` | 观察 |
| `mcp__chrome-devtools__click` | 交互 |
| `mcp__chrome-devtools__dblClick` (via `click.dblClick`) | 交互 |
| `mcp__chrome-devtools__hover` | 交互 |
| `mcp__chrome-devtools__drag` | 交互 |
| `mcp__chrome-devtools__fill` | 交互 |
| `mcp__chrome-devtools__fill_form` | 交互 |
| `mcp__chrome-devtools__type_text` | 交互 |
| `mcp__chrome-devtools__press_key` | 交互 |
| `mcp__chrome-devtools__upload_file` | 交互 |
| `mcp__chrome-devtools__handle_dialog` | 交互 |
| `mcp__chrome-devtools__wait_for` | 同步 |
| `mcp__chrome-devtools__evaluate_script` | 脚本 |
| `mcp__chrome-devtools__emulate` | 环境 |
| `mcp__chrome-devtools__resize_page` | 环境 |
| `mcp__chrome-devtools__performance_start_trace` | 性能 |
| `mcp__chrome-devtools__performance_stop_trace` | 性能 |
| `mcp__chrome-devtools__performance_analyze_insight` | 性能 |
| `mcp__chrome-devtools__take_memory_snapshot` | 性能 |
| `mcp__chrome-devtools__lighthouse_audit` | 性能 |

### MCP: Tavily (2 个)

| 工具名 | 类别 |
|---|---|
| `mcp__tavily__tavily-search` | 搜索 |
| `mcp__tavily__tavily-extract` | 提取 |

### MCP 资源工具 (2 个)

| 工具名 | 类别 |
|---|---|
| `ListMcpResourcesTool` | 资源 |
| `ReadMcpResourceTool` | 资源 |

**总计: 60 个工具调用定义**

---

*生成于 ClawBench 项目工作目录;基于 Claude Code SDK 工具清单与 MCP 协议 schema。*
