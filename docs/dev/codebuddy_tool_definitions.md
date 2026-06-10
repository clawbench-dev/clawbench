# Tool Call Structure Definitions

## Agent

Launch a new agent to handle complex, multi-step tasks autonomously.

```json
{
  "name": "Agent",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["description", "prompt"],
    "properties": {
      "description": {
        "type": "string",
        "description": "A short (3-5 word) description of the task"
      },
      "max_turns": {
        "type": "integer",
        "exclusiveMinimum": 0,
        "description": "Maximum number of agentic turns (API round-trips) before stopping."
      },
      "mode": {
        "type": "string",
        "enum": ["acceptEdits", "bypassPermissions", "default", "plan"],
        "description": "Permission mode for spawned teammate (e.g., \"plan\" to require plan approval)."
      },
      "model": {
        "type": "string",
        "enum": ["default", "lite", "reasoning"],
        "description": "Model variant to use. \"default\": inherits from parent; \"lite\": fast and cost-effective; \"reasoning\": enhanced reasoning."
      },
      "name": {
        "type": "string",
        "description": "Name for the spawned agent. Makes it addressable via SendMessage({to: name}). The name \"team-lead\" is reserved."
      },
      "prompt": {
        "type": "string",
        "description": "The task for the agent to perform."
      },
      "resume": {
        "type": "string",
        "description": "Optional agent ID to resume from."
      },
      "run_in_background": {
        "type": "boolean",
        "description": "Set to true to run this agent in the background."
      },
      "subagent_type": {
        "type": "string",
        "description": "The type of specialized agent to use for this task."
      },
      "team_name": {
        "type": "string",
        "description": "Team name for spawning. Uses current team context if omitted."
      }
    }
  }
}
```

---

## Read

Reads a file from the local filesystem.

```json
{
  "name": "Read",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["file_path"],
    "properties": {
      "file_path": {
        "type": "string",
        "description": "The path to the file to read (can be absolute or relative)."
      },
      "limit": {
        "type": "number",
        "description": "The number of lines to read. Only provide if the file is too large to read at once."
      },
      "offset": {
        "type": "number",
        "description": "The line number to start reading from. Only provide if the file is too large to read at once."
      }
    }
  }
}
```

---

## Write

Writes a file to the local filesystem. Overwrites existing file if one exists.

```json
{
  "name": "Write",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["file_path", "content"],
    "properties": {
      "content": {
        "type": "string",
        "description": "The content to write to the file."
      },
      "file_path": {
        "type": "string",
        "description": "The path to the file to write (can be absolute or relative)."
      }
    }
  }
}
```

---

## Edit

Performs exact string replacements in files.

```json
{
  "name": "Edit",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["file_path", "old_string", "new_string"],
    "properties": {
      "file_path": {
        "type": "string",
        "description": "The path to the file to modify (can be absolute or relative)."
      },
      "new_string": {
        "type": "string",
        "description": "The new text to replace the old text with. Must be different from old_string."
      },
      "old_string": {
        "type": "string",
        "description": "The text to replace."
      },
      "replace_all": {
        "type": "boolean",
        "default": false,
        "description": "Replace all occurrences of old_string (default false)."
      }
    }
  }
}
```

---

## Bash

Executes a given bash command and returns its output.

```json
{
  "name": "Bash",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["command"],
    "properties": {
      "command": {
        "type": "string",
        "description": "The command to execute."
      },
      "dangerouslyDisableSandbox": {
        "type": "boolean",
        "description": "Set to true ONLY when you are confident the command requires operating outside the sandbox."
      },
      "description": {
        "type": "string",
        "description": "Clear, concise description of what this command does in active voice."
      },
      "run_in_background": {
        "type": "boolean",
        "description": "Set to true to run this command in the background."
      },
      "timeout": {
        "type": "number",
        "description": "Optional timeout in milliseconds. Omit to use the system default."
      }
    }
  }
}
```

---

## Glob

Fast file pattern matching tool that works with any codebase size.

```json
{
  "name": "Glob",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["pattern"],
    "properties": {
      "limit": {
        "type": "integer",
        "default": 100,
        "exclusiveMinimum": 0,
        "description": "Maximum number of results to return. Defaults to 100."
      },
      "offset": {
        "type": "integer",
        "default": 0,
        "minimum": 0,
        "description": "Number of results to skip from the beginning. Defaults to 0."
      },
      "path": {
        "type": "string",
        "description": "The directory to search in. If not specified, the current working directory will be used."
      },
      "pattern": {
        "type": "string",
        "description": "The glob pattern to match files against."
      }
    }
  }
}
```

---

## Grep

A powerful search tool built on ripgrep.

```json
{
  "name": "Grep",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["pattern"],
    "properties": {
      "-A": {
        "type": "number",
        "description": "Number of lines to show after each match. Requires output_mode: \"content\"."
      },
      "-B": {
        "type": "number",
        "description": "Number of lines to show before each match. Requires output_mode: \"content\"."
      },
      "-C": {
        "type": "number",
        "description": "Alias for context."
      },
      "-i": {
        "type": "boolean",
        "description": "Case insensitive search."
      },
      "-n": {
        "type": "boolean",
        "description": "Show line numbers in output. Requires output_mode: \"content\"."
      },
      "context": {
        "type": "number",
        "description": "Number of lines to show before and after each match. Requires output_mode: \"content\"."
      },
      "glob": {
        "type": "string",
        "description": "Glob pattern to filter files (e.g., \"*.js\", \"*.{ts,tsx}\")."
      },
      "head_limit": {
        "type": "number",
        "description": "Limit output to first N lines/entries."
      },
      "multiline": {
        "type": "boolean",
        "default": false,
        "description": "Enable multiline mode where . matches newlines."
      },
      "offset": {
        "type": "number",
        "description": "Skip first N lines/entries before applying head_limit."
      },
      "output_mode": {
        "type": "string",
        "enum": ["content", "files_with_matches", "count"],
        "description": "Output mode. Defaults to \"files_with_matches\"."
      },
      "path": {
        "type": "string",
        "description": "File or directory to search in. Defaults to current working directory."
      },
      "pattern": {
        "type": "string",
        "description": "The regular expression pattern to search for in file contents."
      },
      "type": {
        "type": "string",
        "description": "File type to search (e.g., js, py, rust, go, java)."
      }
    }
  }
}
```

---

## EnterPlanMode

Use this tool proactively when you're about to start a non-trivial implementation task.

```json
{
  "name": "EnterPlanMode",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "properties": {}
  }
}
```

---

## ExitPlanMode

Use this tool when you are in plan mode and have finished writing your plan.

```json
{
  "name": "ExitPlanMode",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "allowedPrompts": {
        "type": "array",
        "items": {
          "type": "object",
          "additionalProperties": false,
          "required": ["tool", "prompt"],
          "properties": {
            "prompt": {
              "type": "string",
              "description": "Semantic description of the action, e.g., \"run tests\", \"install dependencies\"."
            },
            "tool": {
              "type": "string",
              "enum": ["Bash"],
              "description": "The tool this prompt applies to."
            }
          }
        },
        "description": "Prompt-based permissions needed to implement the plan."
      }
    }
  }
}
```

---

## TaskCreate

Create a structured task list for your current coding session.

```json
{
  "name": "TaskCreate",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["subject", "description"],
    "properties": {
      "activeForm": {
        "type": "string",
        "description": "Present continuous form shown in spinner when in_progress (e.g., \"Running tests\")."
      },
      "description": {
        "type": "string",
        "description": "A detailed description of what needs to be done."
      },
      "metadata": {
        "type": "object",
        "additionalProperties": {},
        "description": "Arbitrary metadata to attach to the task."
      },
      "owner": {
        "type": "string",
        "description": "Task owner (agent name). Use to assign the task to a specific teammate."
      },
      "subject": {
        "type": "string",
        "description": "A brief title for the task."
      }
    }
  }
}
```

---

## TaskGet

Retrieve a task by its ID from the task list.

```json
{
  "name": "TaskGet",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["taskId"],
    "properties": {
      "taskId": {
        "type": "string",
        "description": "The ID of the task to retrieve."
      }
    }
  }
}
```

---

## TaskUpdate

Update a task in the task list.

```json
{
  "name": "TaskUpdate",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["taskId"],
    "properties": {
      "activeForm": {
        "type": "string",
        "description": "Present continuous form shown in spinner when in_progress."
      },
      "addBlockedBy": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Task IDs that block this task."
      },
      "addBlocks": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Task IDs that this task blocks."
      },
      "description": {
        "type": "string",
        "description": "New description for the task."
      },
      "metadata": {
        "type": "object",
        "additionalProperties": {},
        "description": "Metadata keys to merge into the task. Set a key to null to delete it."
      },
      "owner": {
        "type": "string",
        "description": "New owner for the task."
      },
      "status": {
        "type": "string",
        "enum": ["pending", "in_progress", "completed", "deleted"],
        "description": "New status for the task."
      },
      "subject": {
        "type": "string",
        "description": "New subject for the task."
      },
      "taskId": {
        "type": "string",
        "description": "The ID of the task to update."
      }
    }
  }
}
```

---

## TaskList

List all tasks in the task list.

```json
{
  "name": "TaskList",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "properties": {}
  }
}
```

---

## WebFetch

Fetches content from a specified URL and processes it using an AI model.

```json
{
  "name": "WebFetch",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["url", "prompt"],
    "properties": {
      "prompt": {
        "type": "string",
        "description": "The prompt to run on the fetched content."
      },
      "url": {
        "type": "string",
        "description": "The URL to fetch content from."
      }
    }
  }
}
```

---

## WebSearch

Searches the web and uses the results to inform responses.

```json
{
  "name": "WebSearch",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["query"],
    "properties": {
      "allowed_domains": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Only include search results from these domains."
      },
      "blocked_domains": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Never include search results from these domains."
      },
      "query": {
        "type": "string",
        "minLength": 2,
        "description": "The search query to use."
      },
      "query_keyword_groups": {
        "type": "array",
        "items": { "type": "string" },
        "maxItems": 5,
        "description": "MUST use when comparing multiple technologies/products/concepts. Provide one short keyword phrase per angle. Max 5 groups."
      },
      "topic": {
        "type": "string",
        "enum": ["general", "news", "programming", "documentation", "academic", "finance", "technology", "legal", "medical"],
        "description": "Optional topic type to optimize search relevance."
      }
    }
  }
}
```

---

## TaskStop

Stops a running background task by its ID.

```json
{
  "name": "TaskStop",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "shell_id": {
        "type": "string",
        "description": "Deprecated: use task_id instead."
      },
      "task_id": {
        "type": "string",
        "description": "The ID of the background task to stop."
      }
    }
  }
}
```

---

## TaskOutput

Retrieves output from a running or completed background task.

```json
{
  "name": "TaskOutput",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["task_id"],
    "properties": {
      "block": {
        "type": "boolean",
        "default": true,
        "description": "Whether to wait for task completion (default: true)."
      },
      "filter": {
        "type": "string",
        "description": "Optional regex pattern to filter output lines (only applies to background shell tasks)."
      },
      "task_id": {
        "type": "string",
        "description": "The ID of the background task to retrieve."
      },
      "timeout": {
        "type": "number",
        "default": 60000,
        "description": "Timeout in milliseconds (0-600000, default: 60000)."
      }
    }
  }
}
```

---

## Skill

Execute a skill within the main conversation.

```json
{
  "name": "Skill",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "args": {
        "type": "string",
        "description": "Optional arguments for the skill."
      },
      "command": {
        "type": "string",
        "description": "(Legacy) The skill name (no arguments). E.g., \"pdf\" or \"xlsx\"."
      },
      "skill": {
        "type": "string",
        "description": "The skill name. E.g., \"commit\", \"review-pr\", or \"pdf\"."
      }
    }
  }
}
```

---

## ToolSearch

Search for available tools using natural language queries.

```json
{
  "name": "ToolSearch",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "queries": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Keyword-based search query using MiniSearch full-text search engine. Supports prefix matching, fuzzy matching, and relevance ranking."
      },
      "tool_names": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Exact, fully-qualified tool name(s) to look up."
      },
      "top_k": {
        "type": "integer",
        "default": 3,
        "exclusiveMinimum": 0,
        "maximum": 20,
        "description": "Maximum number of tools to return with full details (default: 3, max: 20)."
      }
    }
  }
}
```

---

## DeferExecuteTool

Execute a deferred tool by name.

```json
{
  "name": "DeferExecuteTool",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["toolName", "params"],
    "properties": {
      "params": {
        "type": "object",
        "additionalProperties": {},
        "description": "The parameters to pass to the target tool. Must match the tool's schema."
      },
      "toolName": {
        "type": "string",
        "description": "The exact name of the deferred tool to execute."
      }
    }
  }
}
```

---

## SendMessage

Send messages to agent teammates and handle protocol requests/responses in a team.

```json
{
  "name": "SendMessage",
  "parameters": {
    "type": "object",
    "additionalProperties": false,
    "required": ["type"],
    "properties": {
      "approve": {
        "type": "boolean",
        "description": "Whether to approve the request (required for shutdown_response, plan_approval_response)."
      },
      "content": {
        "type": "string",
        "description": "Message text, reason, or feedback."
      },
      "recipient": {
        "type": "string",
        "description": "Agent name of the recipient (required for message, shutdown_request, plan_approval_response)."
      },
      "request_id": {
        "type": "string",
        "description": "Request ID to respond to (required for shutdown_response, plan_approval_response)."
      },
      "summary": {
        "type": "string",
        "description": "A 5-10 word summary of the message, shown as a preview in the UI (required for message, broadcast)."
      },
      "type": {
        "type": "string",
        "enum": ["message", "broadcast", "shutdown_request", "shutdown_response", "plan_approval_response"],
        "description": "Message type."
      }
    }
  }
}
```

---

## Deferred Tools (require ToolSearch first)

The following tools are NOT directly callable. Use ToolSearch to discover their schemas, then DeferExecuteTool to invoke them.

| Tool Name | Description |
|-----------|-------------|
| CronCreate | Schedule a prompt to run at a future time — either recurring on a cron schedule, or once at a specific time. Session-only. |
| CronDelete | Cancel a scheduled cron job by ID. Removes it from the in-memory session store. |
| CronList | List all scheduled cron jobs in this session. |
| EnterWorktree | Create an isolated git worktree and switch the current session into it. |
| ImageGen | Generate images from text descriptions using AI models. |
| LeaveWorktree | Leave the current worktree session and switch back to the original working directory. |
| LSP | Interact with Language Server Protocol (LSP) servers to get code intelligence features. |
| NotebookEdit | Completely replaces the contents of a specific cell in a Jupyter notebook (.ipynb file) with new source. |
| TeamCreate | Create a new team to coordinate multiple agents working on a project. |
| TeamDelete | Remove team and task directories when the swarm work is complete. |
| mcp__github | GitHub MCP server (may still be connecting). |
