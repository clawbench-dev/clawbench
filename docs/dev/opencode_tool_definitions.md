# OpenCode 工具调用 JSON Schema 定义

本文档给出 OpenCode 当前会话中可用的全部工具的 JSON Schema 结构。

---

## 目录

- [文件与代码编辑工具](#文件与代码编辑工具)
- [搜索与信息检索工具](#搜索与信息检索工具)
- [Bash 与系统执行工具](#bash-与系统执行工具)
- [任务管理工具](#任务管理工具)
- [计划与流程管理工具](#计划与流程管理工具)
- [技能加载工具](#技能加载工具)
- [子 Agent 调用工具](#子-agent-调用工具)
- [Web 内容获取工具](#web-内容获取工具)
- [汇总清单](#汇总清单)

---

## 文件与代码编辑工具

### Read

读取本地文件系统中的一个文件。

```json
{
  "name": "Read",
  "description": "Read a file or directory from the local filesystem. If the path does not exist, an error is returned.",
  "input_schema": {
    "type": "object",
    "properties": {
      "filePath": {
        "type": "string",
        "description": "The absolute path to the file or directory to read."
      },
      "offset": {
        "type": "integer",
        "minimum": 0,
        "maximum": 9007199254740991,
        "description": "The line number to start reading from (1-indexed)."
      },
      "limit": {
        "type": "integer",
        "minimum": 0,
        "maximum": 9007199254740991,
        "description": "The maximum number of lines to read (defaults to 2000)."
      }
    },
    "required": ["filePath"]
  }
}
```

### Write

写入文件到本地文件系统，若已存在则覆盖。

```json
{
  "name": "Write",
  "description": "Writes a file to the local filesystem. Overwrites existing file if one exists.",
  "input_schema": {
    "type": "object",
    "properties": {
      "filePath": {
        "type": "string",
        "description": "The absolute path to the file to write."
      },
      "content": {
        "type": "string",
        "description": "The content to write to the file."
      }
    },
    "required": ["filePath", "content"]
  }
}
```

### Edit

对文件执行精确字符串替换。

```json
{
  "name": "Edit",
  "description": "Performs exact string replacement in a file.",
  "input_schema": {
    "type": "object",
    "properties": {
      "filePath": {
        "type": "string",
        "description": "The absolute path to the file to modify."
      },
      "oldString": {
        "type": "string",
        "description": "The text to replace. Must match exactly including indentation."
      },
      "newString": {
        "type": "string",
        "description": "The new text to replace the old text with."
      },
      "replaceAll": {
        "type": "boolean",
        "default": false,
        "description": "Replace all occurrences of old_string (default false)."
      }
    },
    "required": ["filePath", "oldString", "newString"]
  }
}
```

---

## 搜索与信息检索工具

### Glob

快速文件模式匹配工具，适用于任何规模的代码库。

```json
{
  "name": "Glob",
  "description": "Fast file pattern matching tool that works with any codebase size. Supports glob patterns like '**/*.js' or 'src/**/*.ts'. Returns matching file paths sorted by modification time.",
  "input_schema": {
    "type": "object",
    "properties": {
      "pattern": {
        "type": "string",
        "description": "The glob pattern to match files against."
      },
      "path": {
        "type": "string",
        "description": "The directory to search in. If not specified, the current working directory will be used."
      }
    },
    "required": ["pattern"]
  }
}
```

### Grep

基于 ripgrep 的强大搜索工具。

```json
{
  "name": "Grep",
  "description": "Fast content search tool that works with any codebase size. Searches file contents using regular expressions. Returns file paths and line numbers with at least one match sorted by modification time.",
  "input_schema": {
    "type": "object",
    "properties": {
      "pattern": {
        "type": "string",
        "description": "The regex pattern to search for in file contents."
      },
      "path": {
        "type": "string",
        "description": "The directory to search in. Defaults to the current working directory."
      },
      "include": {
        "type": "string",
        "description": "File pattern to include in the search (e.g., '*.js', '*.{ts,tsx}')."
      }
    },
    "required": ["pattern"]
  }
}
```

---

## Bash 与系统执行工具

### Bash

执行 bash 命令并返回其输出。

```json
{
  "name": "Bash",
  "description": "Executes a given bash command in a persistent shell session with optional timeout, ensuring proper handling and security measures.",
  "input_schema": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "The command to execute."
      },
      "timeout": {
        "type": "integer",
        "minimum": -9007199254740991,
        "exclusiveMinimum": 0,
        "maximum": 9007199254740991,
        "description": "Optional timeout in milliseconds. If not specified, commands will time out after 120000ms."
      },
      "workdir": {
        "type": "string",
        "description": "The working directory to run the command in. Defaults to the current directory."
      },
      "description": {
        "type": "string",
        "description": "Clear, concise description of what this command does in 5-10 words."
      }
    },
    "required": ["command"]
  }
}
```

---

## 任务管理工具

### TodoWrite

创建和维护当前编码会话的、结构化任务列表。

```json
{
  "name": "TodoWrite",
  "description": "Create and maintain a structured task list for the current coding session. Tracks progress, organizes multi-step work, and surfaces status to the user.",
  "input_schema": {
    "type": "object",
    "properties": {
      "todos": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "content": {
              "type": "string",
              "description": "Brief description of the task."
            },
            "status": {
              "type": "string",
              "enum": ["pending", "in_progress", "completed", "cancelled"],
              "description": "Current status of the task."
            },
            "priority": {
              "type": "string",
              "enum": ["high", "medium", "low"],
              "description": "Priority level of the task."
            }
          },
          "required": ["content", "status", "priority"]
        }
      }
    },
    "required": ["todos"]
  }
}
```

---

## 计划与流程管理工具

### EnterPlanMode

当即将开始非平凡的实现任务时，主动使用此工具。

```json
{
  "name": "EnterPlanMode",
  "description": "Use this tool proactively when you're about to start a non-trivial implementation task.",
  "input_schema": {
    "type": "object",
    "properties": {}
  }
}
```

### ExitPlanMode

当处于计划模式且已完成计划编写时使用。

```json
{
  "name": "ExitPlanMode",
  "description": "Use this tool when you are in plan mode and have finished writing your plan.",
  "input_schema": {
    "type": "object",
    "properties": {
      "allowedPrompts": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "tool": {
              "type": "string",
              "enum": ["Bash"],
              "description": "The tool this prompt applies to."
            },
            "prompt": {
              "type": "string",
              "description": "Semantic description of the action, e.g., 'run tests', 'install dependencies'."
            }
          },
          "required": ["tool", "prompt"]
        }
      }
    }
  }
}
```

---

## 技能加载工具

### Skill

加载专业技能以处理匹配其描述的任务。

```json
{
  "name": "Skill",
  "description": "Load a specialized skill when the task at hand matches one of the available skills.",
  "input_schema": {
    "type": "object",
    "properties": {
      "name": {
        "type": "string",
        "description": "The name of the skill from available_skills."
      }
    },
    "required": ["name"]
  }
}
```

---

## 子 Agent 调用工具

### Task

启动新的 agent 来处理复杂的、多步骤任务。

```json
{
  "name": "Task",
  "description": "Launch a new agent to handle complex, multistep tasks autonomously. When using this tool, you must specify a subagent_type parameter to select which agent type to use.",
  "input_schema": {
    "type": "object",
    "properties": {
      "description": {
        "type": "string",
        "description": "A short (3-5 word) description of the task."
      },
      "prompt": {
        "type": "string",
        "description": "The task for the agent to perform. Be very detailed in your description."
      },
      "subagent_type": {
        "type": "string",
        "enum": ["explore", "general"],
        "description": "The type of specialized agent to use for this task."
      },
      "task_id": {
        "type": "string",
        "description": "This should only be set if you mean to resume a previous task (you can pass a prior task_id and the task will continue the same subagent session as before instead of creating a fresh one)."
      }
    },
    "required": ["description", "prompt", "subagent_type"]
  }
}
```

---

## Web 内容获取工具

### WebFetch

从指定 URL 获取内容并转换为 markdown 格式。

```json
{
  "name": "WebFetch",
  "description": "Fetches content from a specified URL. Takes a URL and optional format as input. Fetches the URL content, converts to requested format (markdown by default).",
  "input_schema": {
    "type": "object",
    "properties": {
      "url": {
        "type": "string",
        "description": "The URL to fetch content from."
      },
      "format": {
        "type": "string",
        "enum": ["text", "markdown", "html"],
        "default": "markdown",
        "description": "The format to return the content in."
      },
      "timeout": {
        "type": "number",
        "minimum": 0,
        "maximum": 120,
        "description": "Optional timeout in seconds (max 120)."
      }
    },
    "required": ["url"]
  }
}
```

---

## 汇总清单

### 工具名总表

| 工具名 | 类别 |描述 |
|---|---|---|
| `Read` | 文件编辑 | 读取本地文件或目录 |
| `Write` | 文件编辑 | 写入文件到本地文件系统 |
| `Edit` | 文件编辑 | 对文件执行精确字符串替换 |
| `Glob` | 搜索检索 | 快速文件模式匹配 |
| `Grep` | 搜索检索 | 基于正则表达式的文件内容搜索 |
| `Bash` | 系统执行 | 执行 bash 命令并返回输出 |
| `TodoWrite` | 任务管理 | 创建和维护结构化任务列表 |
| `EnterPlanMode` | 计划流程 | 进入计划模式 |
| `ExitPlanMode` | 计划流程 | 退出计划模式 |
| `Skill` | 技能加载 | 加载专业技能 |
| `Task` | 子 Agent | 启动子 agent 处理复杂任务 |
| `WebFetch` | Web 获取 | 从 URL 获取内容 |

**总计: 12 个工具调用定义**

---

*生成于 ClawBench 项目工作目录*