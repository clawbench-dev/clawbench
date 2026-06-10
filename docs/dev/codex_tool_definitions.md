# Codex 工具调用 JSON Schema 定义

本文档给出 OpenAI Codex CLI (`codex exec --json`) 当前会话中可用的全部工具的 **JSON Schema 结构**。
每个工具均使用与 OpenAI/Tool Use 协议兼容的 JSON Schema 格式。

> **捕获方式：** 从 Codex CLI v0.137.0 二进制中提取工具定义，结合系统提示和 handler 源码路径交叉验证。

---

## 目录

- [命令执行工具](#命令执行工具)
  - [exec_command - 统一执行命令](#exec_command---统一执行命令)
  - [shell_command - Shell 命令](#shell_command---shell-命令)
  - [write_stdin - 写入标准输入](#write_stdin---写入标准输入)
- [文件编辑工具](#文件编辑工具)
  - [apply_patch - 应用补丁](#apply_patch---应用补丁)
- [目标与计划工具](#目标与计划工具)
  - [create_goal - 创建目标](#create_goal---创建目标)
  - [get_goal - 获取目标状态](#get_goal---获取目标状态)
  - [update_goal - 更新目标](#update_goal---更新目标)
  - [update_plan - 更新计划](#update_plan---更新计划)
- [多媒体工具](#多媒体工具)
  - [view_image - 查看图片](#view_image---查看图片)
- [交互与权限工具](#交互与权限工具)
  - [request_user_input - 请求用户输入](#request_user_input---请求用户输入)
  - [request_permissions - 请求权限](#request_permissions---请求权限)
- [多代理工具](#多代理工具)
  - [spawn_agent - 生成子代理](#spawn_agent---生成子代理)
  - [close_agent - 关闭子代理](#close_agent---关闭子代理)
  - [send_input - 向子代理发送消息](#send_input---向子代理发送消息)
  - [resume_agent - 恢复子代理](#resume_agent---恢复子代理)
  - [list_agents - 列出子代理](#list_agents---列出子代理)
  - [wait - 等待子代理](#wait---等待子代理)
  - [followup_task - 后续任务](#followup_task---后续任务)
  - [message_tool - 消息工具](#message_tool---消息工具)
- [批量作业工具](#批量作业工具)
  - [spawn_agents_on_csv - CSV 批量子代理](#spawn_agents_on_csv---csv-批量子代理)
  - [report_agent_job_result - 报告作业结果](#report_agent_job_result---报告作业结果)
- [搜索与发现工具](#搜索与发现工具)
  - [web_search - 网络搜索](#web_search---网络搜索)
  - [tool_search - 工具搜索](#tool_search---工具搜索)
- [MCP 工具](#mcp-工具)
  - [mcp_tool_call - MCP 工具调用](#mcp_tool_call---mcp-工具调用)
  - [read_mcp_resource - 读取 MCP 资源](#read_mcp_resource---读取-mcp-资源)
  - [list_mcp_resources - 列出 MCP 资源](#list_mcp_resources---列出-mcp-资源)
  - [list_mcp_resource_templates - 列出 MCP 资源模板](#list_mcp_resource_templates---列出-mcp-资源模板)
- [动态与扩展工具](#动态与扩展工具)
  - [dynamic_tool_call - 动态工具调用](#dynamic_tool_call---动态工具调用)
  - [extension_tools - 扩展工具](#extension_tools---扩展工具)
  - [tool_suggest / request_plugin_install - 工具建议与插件安装](#tool_suggest--request_plugin_install---工具建议与插件安装)
- [内部测试工具](#内部测试工具)
  - [barrier - 同步屏障](#barrier---同步屏障)
- [汇总清单](#汇总清单)
- [JSONL 流 item 类型](#jsonl-流-item-类型)
- [v0.57.0 → v0.137.0 变更差异](#v0570--v01370-变更差异)

---

## 命令执行工具

### exec_command - 统一执行命令

在 PTY 中运行 shell 命令，返回输出或会话 ID 用于持续交互。这是 Codex 的**核心工具**（unified_exec 路径），所有文件读写、代码编辑、系统操作都可通过此工具执行。

```json
{
  "name": "exec_command",
  "description": "Runs a command in a PTY, returning output or a session ID for ongoing interaction.",
  "input_schema": {
    "type": "object",
    "properties": {
      "cmd": {
        "type": "string",
        "description": "Shell command to execute."
      },
      "justification": {
        "type": "string",
        "description": "User-facing approval question for `require_escalated`; omit otherwise."
      },
      "login": {
        "type": "boolean",
        "description": "True runs the shell with -l/-i semantics; false disables them. Defaults to true."
      },
      "max_output_tokens": {
        "type": "number",
        "description": "Output token budget. Defaults to 10000 tokens; larger requests may be capped by policy."
      },
      "prefix_rule": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Reusable approval prefix for `cmd`, only with `sandbox_permissions: \"require_escalated\"`; for example [\"git\", \"pull\"]."
      },
      "sandbox_permissions": {
        "type": "string",
        "enum": ["use_default", "with_additional_permissions", "require_escalated"],
        "description": "Per-command sandbox override. Defaults to `use_default`; use `with_additional_permissions` with `additional_permissions`, or `require_escalated` for unsandboxed execution."
      },
      "additional_permissions": {
        "type": "object",
        "description": "Sandboxed filesystem or network access for this command; only with `sandbox_permissions: \"with_additional_permissions\"`.",
        "properties": {
          "network": {
            "type": "object",
            "properties": {
              "enabled": { "type": "boolean", "description": "True requests network access; false or omitted requests none." }
            }
          },
          "file_system": {
            "type": "object",
            "properties": {
              "read": {
                "type": "array",
                "items": { "type": "string" },
                "description": "Absolute paths to grant read access; omit when none are needed."
              },
              "write": {
                "type": "array",
                "items": { "type": "string" },
                "description": "Absolute paths to grant write access; omit when none are needed."
              }
            }
          }
        }
      },
      "shell": {
        "type": "string",
        "description": "Shell binary to launch. Defaults to the user's default shell."
      },
      "tty": {
        "type": "boolean",
        "description": "True allocates a PTY for the command; false or omitted uses plain pipes."
      },
      "workdir": {
        "type": "string",
        "description": "Working directory for the command. Defaults to the turn cwd."
      },
      "yield_time_ms": {
        "type": "number",
        "description": "Wait before yielding output. Defaults to 10000 ms; effective range is 250-30000 ms."
      },
      "environment_id": {
        "type": "string",
        "description": "Environment id from <environment_context>. Omit to use the primary environment."
      }
    },
    "required": ["cmd"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `cmd` | string | 是 | 要执行的 shell 命令 |
| `justification` | string | 否 | `require_escalated` 模式下的用户审批提示 |
| `login` | boolean | 否 | 是否使用 login shell（默认 true） |
| `max_output_tokens` | number | 否 | 输出 token 预算（默认 10000） |
| `prefix_rule` | string[] | 否 | `require_escalated` 下的命令前缀白名单 |
| `sandbox_permissions` | string | 否 | 沙箱权限覆盖：`use_default`、`with_additional_permissions` 或 `require_escalated` |
| `additional_permissions` | object | 否 | `with_additional_permissions` 下的额外文件系统/网络权限 |
| `additional_permissions.network.enabled` | boolean | 否 | 是否请求网络访问 |
| `additional_permissions.file_system.read` | string[] | 否 | 授予读访问的绝对路径列表 |
| `additional_permissions.file_system.write` | string[] | 否 | 授予写访问的绝对路径列表 |
| `shell` | string | 否 | Shell 二进制路径（默认用户 shell） |
| `tty` | boolean | 否 | 是否分配 PTY（默认 false） |
| `workdir` | string | 否 | 工作目录（默认当前 turn 目录） |
| `yield_time_ms` | number | 否 | 输出等待时间（默认 10000ms，范围 250-30000ms） |
| `environment_id` | string | 否 | 环境上下文 ID（省略则使用主环境） |

> **v0.137.0 变更：** `sandbox_permissions` 新增 `with_additional_permissions` 选项，配合 `additional_permissions` 使用；新增 `environment_id` 参数。

---

### shell_command - Shell 命令

通过 shell 执行命令的独立工具路径（与 unified_exec 的 `exec_command` 并存）。系统提示中要求"始终设置 `workdir` 参数"。

```json
{
  "name": "shell_command",
  "description": "Execute a shell command. Always set the workdir param when using this function. Do not use cd unless absolutely necessary.",
  "input_schema": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "Shell command to execute."
      },
      "workdir": {
        "type": "string",
        "description": "Working directory for the command."
      },
      "environment_id": {
        "type": "string",
        "description": "Environment id from <environment_context>. Omit to use the primary environment."
      }
    },
    "required": ["command"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `command` | string | 是 | 要执行的 shell 命令 |
| `workdir` | string | 否 | 工作目录（系统提示要求始终设置） |
| `environment_id` | string | 否 | 环境上下文 ID |

> **v0.137.0 新增。** Handler 路径: `core/src/tools/handlers/shell/shell_command.rs`

---

### write_stdin - 写入标准输入

向已有的统一执行会话写入字符并返回近期输出。用于与长时间运行的交互式命令进行后续交互。

```json
{
  "name": "write_stdin",
  "description": "Writes characters to an existing unified exec session and returns recent output.",
  "input_schema": {
    "type": "object",
    "properties": {
      "chars": {
        "type": "string",
        "description": "Bytes to write to stdin. Defaults to empty, which polls without writing."
      },
      "max_output_tokens": {
        "type": "number",
        "description": "Output token budget. Defaults to 10000 tokens; larger requests may be capped by policy."
      },
      "session_id": {
        "type": "number",
        "description": "Identifier of the running unified exec session."
      },
      "yield_time_ms": {
        "type": "number",
        "description": "Wait before yielding output. Non-empty writes default to 250 ms and cap at 30000 ms; empty polls wait 5000-300000 ms by default."
      }
    },
    "required": ["session_id"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `session_id` | number | 是 | 运行中执行会话的标识符 |
| `chars` | string | 否 | 写入 stdin 的内容（空则仅轮询） |
| `max_output_tokens` | number | 否 | 输出 token 预算（默认 10000） |
| `yield_time_ms` | number | 否 | 输出等待时间（非空写入默认 250ms，空轮询 5000-300000ms） |

---

## 文件编辑工具

### apply_patch - 应用补丁

使用自定义补丁格式编辑文件。这是 Codex v0.137.0 中**首选的文件编辑方式**，系统提示明确指示"Use the `apply_patch` tool to edit files"。此工具为 FREEFORM 工具，补丁内容不需要包裹在 JSON 中。

补丁格式：

```
*** Begin Patch
*** Add File: <path>
+<initial content line>
*** Update File: <path>
*** Move to: <new_path>          ← 可选重命名
@@ <context line>
-<removed line>
+<added line>
*** Delete File: <path>
*** End Patch
```

```json
{
  "name": "apply_patch",
  "description": "Use the apply_patch tool to edit files. This is a FREEFORM tool, so do not wrap the patch in JSON.",
  "input_schema": {
    "type": "object",
    "properties": {
      "patch": {
        "type": "string",
        "description": "The patch content in Codex patch format. Starts with '*** Begin Patch' and ends with '*** End Patch'."
      },
      "environment_id": {
        "type": "string",
        "description": "Environment id from <environment_context>. Omit to use the primary environment."
      }
    },
    "required": ["patch"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `patch` | string | 是 | Codex 补丁格式的内容 |
| `environment_id` | string | 否 | 环境上下文 ID |

**补丁格式要点：**

1. 必须包含操作头：`*** Add File:`、`*** Delete File:` 或 `*** Update File:`
2. 新增行必须以 `+` 前缀标记（即使是新文件也是如此）
3. 删除行以 `-` 前缀标记
4. `*** Move to:` 可在 `*** Update File:` 后使用，用于重命名文件
5. 禁止使用 `applypatch` 或 `apply-patch`，只允许 `apply_patch`

**JSONL 流中的表现：** `apply_patch` 调用产生 `file_change` 类型的 ThreadItem，包含 `add`/`delete`/`update` 操作。

> **v0.137.0 新增。** Handler 路径: `core/src/tools/handlers/apply_patch.rs`、`core/src/apply_patch.rs`、`core/src/tools/runtimes/apply_patch.rs`

---

## 目标与计划工具

### create_goal - 创建目标

仅在用户明确请求或系统/开发者指令要求时创建目标；不要从普通任务推断目标。仅在无现有目标时生效。

```json
{
  "name": "create_goal",
  "description": "Create a goal only when explicitly requested by the user or system/developer instructions; do not infer goals from ordinary tasks. Set token_budget only when an explicit token budget is requested. Fails if a goal exists; use update_goal only for status.",
  "input_schema": {
    "type": "object",
    "properties": {
      "objective": {
        "type": "string",
        "description": "Required. The concrete objective to start pursuing. This starts a new active goal only when no goal is currently defined; if a goal already exists, this tool fails."
      },
      "token_budget": {
        "type": "integer",
        "description": "Positive token budget for the new goal. Omit unless explicitly requested."
      }
    },
    "required": ["objective"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `objective` | string | 是 | 要达成的具体目标 |
| `token_budget` | integer | 否 | Token 预算（仅在明确请求时设置） |

---

### get_goal - 获取目标状态

获取当前线程的目标，包括状态、预算、token 和耗时使用量、剩余 token 预算。

```json
{
  "name": "get_goal",
  "description": "Get the current goal for this thread, including status, budgets, token and elapsed-time usage, and remaining token budget.",
  "input_schema": {
    "type": "object",
    "properties": {},
    "required": [],
    "additionalProperties": false
  }
}
```

**参数说明：** 无参数。

---

### update_goal - 更新目标

更新现有目标状态。仅用于标记目标为 `complete`（达成）或 `blocked`（阻塞）。同一阻塞条件需连续出现至少 3 次 goal turn 才能标记为 `blocked`。

```json
{
  "name": "update_goal",
  "description": "Update the existing goal. Use this tool only to mark the goal achieved or genuinely blocked. Set status to `complete` only when the objective has actually been achieved and no required work remains. Set status to `blocked` only after the same blocking condition has recurred for at least three consecutive goal turns and the agent is at an impasse.",
  "input_schema": {
    "type": "object",
    "properties": {
      "status": {
        "type": "string",
        "enum": ["complete", "blocked"],
        "description": "Required. Set to `complete` only when the objective is achieved and no required work remains. Set to `blocked` only after the same blocking condition has recurred for at least three consecutive goal turns and the agent is at an impasse."
      }
    },
    "required": ["status"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `status` | string | 是 | 目标状态：`complete` 或 `blocked` |

---

### update_plan - 更新计划

更新任务计划。提供可选说明和计划步骤列表，每个步骤包含步骤文本和状态。同一时间最多只能有一个步骤处于 `in_progress`。

```json
{
  "name": "update_plan",
  "description": "Updates the task plan. Provide an optional explanation and a list of plan items, each with a step and status. At most one step can be in_progress at a time.",
  "input_schema": {
    "type": "object",
    "properties": {
      "explanation": {
        "type": "string",
        "description": "Optional explanation for this plan update."
      },
      "plan": {
        "type": "array",
        "description": "The list of steps",
        "items": {
          "type": "object",
          "properties": {
            "status": {
              "type": "string",
              "enum": ["pending", "in_progress", "completed"],
              "description": "Step status."
            },
            "step": {
              "type": "string",
              "description": "Task step text."
            }
          },
          "required": ["step", "status"],
          "additionalProperties": false
        }
      }
    },
    "required": ["plan"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `explanation` | string | 否 | 计划更新说明 |
| `plan` | array | 是 | 步骤列表 |
| `plan[].step` | string | 是 | 步骤文本 |
| `plan[].status` | string | 是 | 步骤状态：`pending`、`in_progress`、`completed` |

---

## 多媒体工具

### view_image - 查看图片

查看本地文件系统中的图片文件。用于需要对磁盘上的图片进行视觉检查的场景。

```json
{
  "name": "view_image",
  "description": "View a local image file from the filesystem when visual inspection is needed. Use this for images already available on disk.",
  "input_schema": {
    "type": "object",
    "properties": {
      "path": {
        "type": "string",
        "description": "Local filesystem path to an image file."
      },
      "detail": {
        "type": "string",
        "enum": ["high", "original"],
        "description": "Image detail level. Defaults to `high`; use `original` to preserve exact resolution."
      },
      "environment_id": {
        "type": "string",
        "description": "Environment id from <environment_context>. Omit to use the primary environment."
      }
    },
    "required": ["path"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `path` | string | 是 | 图片文件的本地路径 |
| `detail` | string | 否 | 图片细节级别：`high`（默认，缩放）或 `original`（原始分辨率） |
| `environment_id` | string | 否 | 环境上下文 ID |

> **v0.137.0 变更：** 新增 `detail` 参数和 `environment_id` 参数。

---

## 交互与权限工具

### request_user_input - 请求用户输入

向用户请求 1-3 个简短问题的输入并等待回复。此工具仅在 Plan 模式下可用。

```json
{
  "name": "request_user_input",
  "description": "Request user input for one to three short questions and wait for the response. This tool is only available in Plan mode.",
  "input_schema": {
    "type": "object",
    "properties": {
      "questions": {
        "type": "array",
        "description": "Questions to show the user. Prefer 1 and do not exceed 3",
        "items": {
          "type": "object",
          "properties": {
            "header": {
              "type": "string",
              "description": "Short header label shown in the UI (12 or fewer chars)."
            },
            "id": {
              "type": "string",
              "description": "Stable identifier for mapping answers (snake_case)."
            },
            "is_secret": {
              "type": "boolean",
              "description": "Whether the answer should be hidden in the UI (for passwords, tokens, etc.)."
            },
            "options": {
              "type": "array",
              "description": "Provide 2-3 mutually exclusive choices. Put the recommended option first and suffix its label with \"(Recommended)\". Do not include an \"Other\" option in this list; the client will add a free-form \"Other\" option automatically.",
              "items": {
                "type": "object",
                "properties": {
                  "description": {
                    "type": "string",
                    "description": "One short sentence explaining impact/tradeoff if selected."
                  },
                  "label": {
                    "type": "string",
                    "description": "User-facing label (1-5 words)."
                  }
                },
                "required": ["label", "description"],
                "additionalProperties": false
              }
            },
            "question": {
              "type": "string",
              "description": "Single-sentence prompt shown to the user."
            }
          },
          "required": ["id", "header", "question", "options"],
          "additionalProperties": false
        }
      }
    },
    "required": ["questions"],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `questions` | array | 是 | 问题列表（1-3 个） |
| `questions[].id` | string | 是 | 问题标识符（snake_case） |
| `questions[].header` | string | 是 | UI 标题标签（12 字符以内） |
| `questions[].question` | string | 是 | 单句提示文本 |
| `questions[].options` | array | 是 | 2-3 个互斥选项 |
| `questions[].options[].label` | string | 是 | 选项标签（1-5 词） |
| `questions[].options[].description` | string | 是 | 选项说明（一句话） |
| `questions[].is_secret` | boolean | 否 | 答案是否在 UI 中隐藏（用于密码、令牌等） |

> **v0.137.0 变更：** 新增 `is_secret` 参数。

---

### request_permissions - 请求权限

向用户请求额外的文件系统或网络权限，并等待客户端授予所请求权限配置的子集。被授予的权限自动应用于当前 turn 中后续的 shell 类命令，或在整个 session 中生效（如果客户端在 session 范围内批准）。

```json
{
  "name": "request_permissions",
  "description": "Request additional filesystem or network permissions from the user and wait for the client to grant a subset of the requested permission profile. Use environment_id to target a specific attached environment; omit it to use the primary environment. Relative filesystem paths resolve against the selected environment cwd. Granted permissions apply automatically to later shell-like commands in the current turn, or for the rest of the session if the client approves them at session scope.",
  "input_schema": {
    "type": "object",
    "properties": {
      "file_system": {
        "type": "object",
        "description": "Filesystem access request.",
        "properties": {
          "read": {
            "type": "array",
            "items": { "type": "string" },
            "description": "Absolute paths to grant read access; omit when none are needed."
          },
          "write": {
            "type": "array",
            "items": { "type": "string" },
            "description": "Absolute paths to grant write access; omit when none are needed."
          }
        }
      },
      "network": {
        "type": "object",
        "description": "Network access request.",
        "properties": {
          "enabled": {
            "type": "boolean",
            "description": "True requests network access; false or omitted requests none."
          }
        }
      },
      "reason": {
        "type": "string",
        "description": "Optional short explanation for why additional permissions are needed."
      },
      "environment_id": {
        "type": "string",
        "description": "Environment id from <environment_context>. Omit to use the primary environment."
      }
    },
    "required": [],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `file_system` | object | 否 | 文件系统访问请求 |
| `file_system.read` | string[] | 否 | 授予读访问的绝对路径列表 |
| `file_system.write` | string[] | 否 | 授予写访问的绝对路径列表 |
| `network` | object | 否 | 网络访问请求 |
| `network.enabled` | boolean | 否 | 是否请求网络访问 |
| `reason` | string | 否 | 需要额外权限的简短说明 |
| `environment_id` | string | 否 | 环境上下文 ID |

> **v0.137.0 新增。** Handler 路径: `core/src/tools/handlers/request_permissions.rs`

---

## 多代理工具

Codex v0.137.0 支持多代理（Multi-Agent）功能，提供 v1 和 v2 两套 handler。以下按 v2（主要版本）描述。

### spawn_agent - 生成子代理

生成一个新的子代理来并行执行任务。系统提示中要求"Only use `spawn_agent` if and only if the user explicitly asks for sub-agents, delegation, or parallel agent work."

```json
{
  "name": "spawn_agent",
  "description": "Spawn and manage sub-agents. Only use spawn_agent if and only if the user explicitly asks for sub-agents, delegation, or parallel agent work.",
  "input_schema": {
    "type": "object",
    "properties": {
      "message": {
        "type": "string",
        "description": "Initial plain-text task for the new agent. Use either message or items."
      },
      "items": {
        "type": "array",
        "description": "Structured input items. Use this to pass explicit mentions (for example app:// connector paths).",
        "items": {
          "type": "object",
          "properties": {
            "type": {
              "type": "string",
              "enum": ["text", "image", "local_image", "skill", "mention"],
              "description": "Input item type."
            },
            "text": {
              "type": "string",
              "description": "Text content when type is text."
            },
            "image_url": {
              "type": "string",
              "description": "Image URL when type is image."
            },
            "path": {
              "type": "string",
              "description": "Path when type is local_image/skill, or structured mention target (e.g. app://<connector-id>) when type is mention."
            },
            "display_name": {
              "type": "string",
              "description": "Display name when type is skill or mention."
            }
          }
        }
      },
      "fork_turns": {
        "type": "string",
        "description": "Optional number of turns to fork. Defaults to `all`. Use `none`, `all`, or a positive integer string such as `3` to fork only the most recent turns."
      },
      "model": {
        "type": "string",
        "description": "Model override for the new agent. Omit unless an explicit override is needed."
      },
      "reasoning_effort": {
        "type": "string",
        "description": "Reasoning effort override for the new agent. Omit to inherit the parent effort."
      },
      "service_tier": {
        "type": "string",
        "description": "Service tier override for the new agent. Omit unless explicitly requested."
      },
      "agent_type": {
        "type": "string",
        "description": "Agent type. Full-history forked agents inherit the parent agent type, model, and reasoning effort."
      },
      "fork_context": {
        "type": "string",
        "description": "Fork context for the spawned agent."
      },
      "task_name": {
        "type": "string",
        "description": "Canonical task name for the spawned agent."
      }
    },
    "required": [],
    "additionalProperties": false
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `message` | string | 否* | 初始任务文本（与 `items` 二选一） |
| `items` | array | 否* | 结构化输入项（与 `message` 二选一） |
| `fork_turns` | string | 否 | 分叉的 turn 数（默认 `all`） |
| `model` | string | 否 | 模型覆盖 |
| `reasoning_effort` | string | 否 | 推理力度覆盖 |
| `service_tier` | string | 否 | 服务层级覆盖 |
| `agent_type` | string | 否 | 代理类型 |
| `fork_context` | string | 否 | 分叉上下文 |
| `task_name` | string | 否 | 规范任务名 |

> **v0.137.0 新增。** Handler 路径: `multi_agents/spawn.rs`、`multi_agents_v2/spawn.rs`

---

### close_agent - 关闭子代理

关闭一个已生成的子代理。代理不能关闭自身，必须由父代理关闭。

```json
{
  "name": "close_agent",
  "description": "Close, shutdown, or stop a sub-agent thread.",
  "input_schema": {
    "type": "object",
    "properties": {
      "target": { "type": "string", "description": "Agent identifier to close." }
    },
    "required": ["target"],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `multi_agents/close_agent.rs`、`multi_agents_v2/close_agent.rs`

---

### send_input - 向子代理发送消息

向一个已存在的子代理发送后续消息。

```json
{
  "name": "send_input",
  "description": "Send message to an existing agent for follow-up, interruption, or redirection.",
  "input_schema": {
    "type": "object",
    "properties": {
      "target": { "type": "string", "description": "Agent identifier." },
      "message": { "type": "string", "description": "Message to send." }
    },
    "required": ["target", "message"],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `multi_agents/send_input.rs`

---

### resume_agent - 恢复子代理

恢复一个已关闭的子代理。

```json
{
  "name": "resume_agent",
  "description": "Resume or reopen a closed agent sub-agent thread.",
  "input_schema": {
    "type": "object",
    "properties": {
      "target": { "type": "string", "description": "Agent identifier to resume." }
    },
    "required": ["target"],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `multi_agents/resume_agent.rs`

---

### list_agents - 列出子代理

列出当前会话中的所有子代理。

```json
{
  "name": "list_agents",
  "description": "List all sub-agents in the current session.",
  "input_schema": {
    "type": "object",
    "properties": {},
    "required": [],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `multi_agents_v2/list_agents.rs`

---

### wait - 等待子代理

等待一个或多个子代理完成。

```json
{
  "name": "wait",
  "description": "Wait for sub-agent(s) to complete.",
  "input_schema": {
    "type": "object",
    "properties": {
      "targets": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Agent identifiers to wait for."
      },
      "timeout_ms": {
        "type": "number",
        "description": "Maximum wait time in milliseconds. Must be greater than zero."
      }
    },
    "required": [],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `multi_agents_v2/wait.rs`

---

### followup_task - 后续任务

对子代理执行后续任务操作。

> **v0.137.0 新增。** Handler 路径: `multi_agents_v2/followup_task.rs`

---

### message_tool - 消息工具

代理间消息传递工具。

> **v0.137.0 新增。** Handler 路径: `multi_agents_v2/message_tool.rs`、`multi_agents_v2/send_message.rs`

---

## 批量作业工具

### spawn_agents_on_csv - CSV 批量子代理

处理 CSV 文件，为每一行生成一个 worker 子代理。指令字符串是模板，其中 `{column}` 占位符被行值替换。此调用阻塞直到所有行完成，并自动导出结果。

```json
{
  "name": "spawn_agents_on_csv",
  "description": "Process a CSV by spawning one worker sub-agent per row. The instruction string is a template where `{column}` placeholders are replaced with row values. Each worker must call report_agent_job_result with a JSON object (matching output_schema when provided); missing reports are treated as failures. This call blocks until all rows finish and automatically exports results to output_csv_path (or a default path).",
  "input_schema": {
    "type": "object",
    "properties": {
      "input_csv_path": {
        "type": "string",
        "description": "Path to the CSV file containing input rows."
      },
      "instruction": {
        "type": "string",
        "description": "Instruction template to apply to each CSV row. Use {column_name} placeholders to inject values from the row."
      },
      "id_column": {
        "type": "string",
        "description": "CSV column to use as stable item id. Omit to use row numbers."
      },
      "output_csv_path": {
        "type": "string",
        "description": "Output CSV path for exported results. Omit to create one next to the input CSV."
      },
      "max_concurrency": {
        "type": "number",
        "description": "Maximum concurrent workers for this job. Defaults to 16 and is capped by config. Alias: max_workers."
      },
      "max_runtime_per_worker_secs": {
        "type": "number",
        "description": "Maximum runtime per worker before failure. Defaults to 1800 seconds; config may set a different default."
      },
      "output_schema": {
        "type": "object",
        "description": "JSON Schema for each worker result. Omit to accept any result object."
      }
    },
    "required": ["input_csv_path", "instruction"],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `agent_jobs/spawn_agents_on_csv.rs`

---

### report_agent_job_result - 报告作业结果

Worker 专用工具，用于报告代理作业项的结果。主代理不应调用此工具。

```json
{
  "name": "report_agent_job_result",
  "description": "Worker-only tool to report a result for an agent job item. Main agents should not call this.",
  "input_schema": {
    "type": "object",
    "properties": {
      "job_id": {
        "type": "string",
        "description": "Identifier of the job."
      },
      "item_id": {
        "type": "string",
        "description": "Identifier of the job item."
      },
      "result": {
        "type": "object",
        "description": "The result object."
      },
      "cancel_remaining": {
        "type": "boolean",
        "description": "True cancels remaining job items after this result is recorded; false or omitted continues the job."
      }
    },
    "required": ["job_id", "item_id", "result"],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `agent_jobs/report_agent_job_result.rs`

---

## 搜索与发现工具

### web_search - 网络搜索

内置网络搜索工具。Codex v0.137.0 支持三种 WebSearchAction 子操作：

- `search` — 执行搜索查询
- `open_page` — 打开网页
- `find_in_page` — 在页面中查找

配置项 `web_search` 可在 `config.toml` 中设置为 `"live"`、`"cached"` 或 `"disabled"`。

JSONL 流中以 `web_search` 类型的 ThreadItem 呈现（包含 `web_search_call` ResponseItem 类型）。

> **v0.137.0 新增。** 实验性功能，需启用 `features.web_search` / `features.web_search_request` / `features.web_search_cached`。

---

### tool_search - 工具搜索

工具发现/搜索机制。允许代理在运行时发现和调用新工具。

```json
{
  "name": "tool_search",
  "description": "Search for and discover available tools at runtime.",
  "input_schema": {
    "type": "object",
    "properties": {},
    "required": [],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `core/src/tools/handlers/tool_search.rs`。实验性功能，相关配置：`tool_search`、`tool_search_always_defer_mcp_tools`、`tool_suggest`。

---

## MCP 工具

### mcp_tool_call - MCP 工具调用

调用已连接的 MCP (Model Context Protocol) 服务器上的工具。

Handler 路径: `core/src/tools/handlers/mcp.rs`

JSONL 流中以 `mcp_tool_call` 类型的 ThreadItem 呈现，包含：
- `server_name` — MCP 服务器名称
- `tool_name` — 工具名称
- `connector_id` / `connector_name` — 连接器标识
- `tool_title` — 工具标题

MCP 支持 `stdio`、`in_process`、`streamable_http`、`jsonrpc` 等传输协议。

---

### read_mcp_resource - 读取 MCP 资源

从 MCP 服务器读取资源。

Handler 路径: `core/src/tools/handlers/mcp_resource/read_mcp_resource.rs`

---

### list_mcp_resources - 列出 MCP 资源

列出 MCP 服务器上的可用资源。

Handler 路径: `core/src/tools/handlers/mcp_resource/list_mcp_resources.rs`

---

### list_mcp_resource_templates - 列出 MCP 资源模板

列出 MCP 服务器上的资源模板。

Handler 路径: `core/src/tools/handlers/mcp_resource/list_mcp_resource_templates.rs`

---

## 动态与扩展工具

### dynamic_tool_call - 动态工具调用

运行时动态工具调用机制。允许在会话期间注册和调用新工具。

Handler 路径: `core/src/tools/handlers/dynamic.rs`

JSONL 流中以 `dynamic_tool_call` 类型的 ThreadItem 呈现，包含 `DynamicToolCallParams`（namespace、arguments、turn_id 等）。

> **v0.137.0 新增。**

---

### extension_tools - 扩展工具

插件/连接器提供的扩展工具。

Handler 路径: `core/src/tools/handlers/extension_tools.rs`

---

### tool_suggest / request_plugin_install - 工具建议与插件安装

- **tool_suggest** — 建议安装可帮助当前任务的插件或连接器
- **request_plugin_install** — 请求安装插件

Handler 路径: `core/src/tools/handlers/request_plugin_install.rs`、`core/src/tools/handlers/list_available_plugins_to_install.rs`

参数包含 `tool_type`（`connector` 或 `plugin`）、`action_type`（`install`）、`plugin_id`/`connector_id`、`suggest_reason`。

---

## 内部测试工具

### barrier - 同步屏障

内部同步辅助工具，用于 Codex 集成测试。允许多个并发工具调用在屏障处汇合。

```json
{
  "name": "barrier",
  "description": "Internal synchronization helper used by Codex integration tests.",
  "input_schema": {
    "type": "object",
    "properties": {
      "id": {
        "type": "string",
        "description": "Identifier shared by concurrent calls that should rendezvous."
      },
      "participants": {
        "type": "number",
        "description": "Number of tool calls that must arrive before the barrier opens."
      },
      "timeout_ms": {
        "type": "number",
        "description": "Maximum barrier wait in milliseconds. Defaults to 1000."
      },
      "sleep_before_ms": {
        "type": "number",
        "description": "Delay before any other action. Defaults to no delay."
      },
      "sleep_after_ms": {
        "type": "number",
        "description": "Delay after completing the barrier. Defaults to no delay."
      }
    },
    "required": [],
    "additionalProperties": false
  }
}
```

> **v0.137.0 新增。** Handler 路径: `core/src/tools/handlers/test_sync.rs`。仅用于集成测试。

---

## 汇总清单

### 核心工具名总表

| 工具名 | 类别 | 描述 | 必填参数 | 版本 |
|--------|------|------|----------|------|
| `exec_command` | 命令执行 | 统一执行命令（PTY） | `cmd` | v0.57+ |
| `shell_command` | 命令执行 | Shell 命令 | `command` | v0.137 |
| `write_stdin` | 命令执行 | 向运行中的会话写入标准输入 | `session_id` | v0.57+ |
| `apply_patch` | 文件编辑 | 应用补丁编辑文件 | `patch` | v0.137 |
| `create_goal` | 目标管理 | 创建目标 | `objective` | v0.57+ |
| `get_goal` | 目标管理 | 获取当前目标及状态 | - | v0.57+ |
| `update_goal` | 目标管理 | 标记目标为达成或阻塞 | `status` | v0.57+ |
| `update_plan` | 计划管理 | 更新任务计划步骤 | `plan` | v0.57+ |
| `view_image` | 多媒体 | 查看本地图片文件 | `path` | v0.57+ |
| `request_user_input` | 交互 | 请求用户输入 | `questions` | v0.57+ |
| `request_permissions` | 权限 | 请求额外文件系统/网络权限 | - | v0.137 |
| `spawn_agent` | 多代理 | 生成子代理 | - | v0.137 |
| `close_agent` | 多代理 | 关闭子代理 | `target` | v0.137 |
| `send_input` | 多代理 | 向子代理发送消息 | `target`, `message` | v0.137 |
| `resume_agent` | 多代理 | 恢复子代理 | `target` | v0.137 |
| `list_agents` | 多代理 | 列出子代理 | - | v0.137 |
| `wait` | 多代理 | 等待子代理完成 | - | v0.137 |
| `followup_task` | 多代理 | 后续任务 | - | v0.137 |
| `message_tool` | 多代理 | 代理间消息传递 | - | v0.137 |
| `spawn_agents_on_csv` | 批量作业 | CSV 批量子代理 | `input_csv_path`, `instruction` | v0.137 |
| `report_agent_job_result` | 批量作业 | 报告作业结果 | `job_id`, `item_id`, `result` | v0.137 |
| `web_search` | 搜索 | 网络搜索 | - | v0.137 |
| `tool_search` | 搜索 | 工具发现/搜索 | - | v0.137 |
| `mcp_tool_call` | MCP | MCP 工具调用 | - | v0.137 |
| `read_mcp_resource` | MCP | 读取 MCP 资源 | - | v0.137 |
| `list_mcp_resources` | MCP | 列出 MCP 资源 | - | v0.137 |
| `list_mcp_resource_templates` | MCP | 列出 MCP 资源模板 | - | v0.137 |
| `dynamic_tool_call` | 动态工具 | 动态工具调用 | - | v0.137 |
| `extension_tools` | 扩展 | 扩展工具 | - | v0.137 |
| `tool_suggest` | 扩展 | 工具建议 | - | v0.137 |
| `request_plugin_install` | 扩展 | 请求插件安装 | - | v0.137 |
| `barrier` | 内部测试 | 同步屏障 | - | v0.137 |

### 工具分类

| 类别 | 工具数 | 工具列表 |
|------|--------|----------|
| 命令执行 | 3 | `exec_command`, `shell_command`, `write_stdin` |
| 文件编辑 | 1 | `apply_patch` |
| 目标与计划 | 4 | `create_goal`, `get_goal`, `update_goal`, `update_plan` |
| 多媒体 | 1 | `view_image` |
| 交互与权限 | 2 | `request_user_input`, `request_permissions` |
| 多代理 | 7 | `spawn_agent`, `close_agent`, `send_input`, `resume_agent`, `list_agents`, `wait`, `followup_task`, `message_tool` |
| 批量作业 | 2 | `spawn_agents_on_csv`, `report_agent_job_result` |
| 搜索与发现 | 2 | `web_search`, `tool_search` |
| MCP | 4 | `mcp_tool_call`, `read_mcp_resource`, `list_mcp_resources`, `list_mcp_resource_templates` |
| 动态与扩展 | 4 | `dynamic_tool_call`, `extension_tools`, `tool_suggest`, `request_plugin_install` |
| 内部测试 | 1 | `barrier` |

### 关键特征

1. **`apply_patch` 是首选的文件编辑方式** — v0.137.0 新增，系统提示明确指示优先使用 `apply_patch` 而非通过 `exec_command` 间接编辑文件。
2. **JSONL 流不再只有 `command_execution`** — v0.137.0 新增 `file_change`、`web_search`、`mcp_tool_call`、`dynamic_tool_call`、`collab_agent_tool_call`、`image_generation`、`reasoning` 等 item 类型。
3. **内部工具在 `--json` 输出中仍不可见** — `create_goal`/`get_goal`/`update_goal`/`update_plan` 等工具由 Codex 运行时内部处理，不作为 item 输出到 JSONL 流中。
4. **所有 Schema 均设置 `"additionalProperties": false`** — 严格禁止额外参数。
5. **`request_user_input` 仅在 Plan 模式下可用**。
6. **多代理功能需用户显式请求** — 系统提示要求仅在用户明确要求子代理、委派或并行工作时才使用 `spawn_agent`。
7. **`exec_command` 新增 `with_additional_permissions` 沙箱选项** — 配合 `additional_permissions` 子对象使用，可在命令级别请求额外文件系统/网络权限。
8. **环境 ID（`environment_id`）** — 多个工具新增此参数，用于多环境场景。

---

## JSONL 流 item 类型

v0.137.0 中 `--json` 输出的 ThreadItem 类型完整列表：

| Item 类型 | 对应工具 | 说明 |
|-----------|----------|------|
| `agent_message` | (内部) | 代理文本输出 |
| `command_execution` | `exec_command` / `shell_command` | 命令执行 |
| `file_change` | `apply_patch` | 文件变更（Add/Delete/Update） |
| `web_search` | `web_search` | 网络搜索 |
| `mcp_tool_call` | `mcp_tool_call` | MCP 工具调用 |
| `dynamic_tool_call` | `dynamic_tool_call` | 动态工具调用 |
| `collab_agent_tool_call` | `spawn_agent` 等 | 协作代理工具调用 |
| `image_view` | `view_image` | 图片查看 |
| `image_generation` | (内置) | 图片生成 |
| `plan` | `update_plan` | 计划更新 |
| `reasoning` | (内部) | 推理过程 |
| `user_message` | (内部) | 用户消息 |
| `entered_review_mode` | (内部) | 进入审查模式 |
| `exited_review_mode` | (内部) | 退出审查模式 |
| `context_compaction` | (内部) | 上下文压缩 |
| `hook_prompt` | (内部) | Hook 提示 |

---

## v0.57.0 → v0.137.0 变更差异

### 新增工具（20+）

| 工具名 | 说明 |
|--------|------|
| `shell_command` | 独立的 shell 命令执行路径 |
| `apply_patch` | 文件补丁编辑（核心新增） |
| `request_permissions` | 请求额外文件系统/网络权限 |
| `spawn_agent` | 生成子代理 |
| `close_agent` | 关闭子代理 |
| `send_input` | 向子代理发送消息 |
| `resume_agent` | 恢复子代理 |
| `list_agents` | 列出子代理 |
| `wait` | 等待子代理 |
| `followup_task` | 后续任务 |
| `message_tool` | 代理间消息 |
| `spawn_agents_on_csv` | CSV 批量子代理 |
| `report_agent_job_result` | 报告作业结果 |
| `web_search` | 网络搜索 |
| `tool_search` | 工具搜索 |
| `mcp_tool_call` | MCP 工具调用 |
| `read_mcp_resource` | 读取 MCP 资源 |
| `list_mcp_resources` | 列出 MCP 资源 |
| `list_mcp_resource_templates` | 列出 MCP 资源模板 |
| `dynamic_tool_call` | 动态工具调用 |
| `extension_tools` | 扩展工具 |
| `tool_suggest` | 工具建议 |
| `request_plugin_install` | 请求插件安装 |
| `barrier` | 同步屏障（测试用） |

### 现有工具变更

| 工具名 | 变更 |
|--------|------|
| `exec_command` | `sandbox_permissions` 新增 `with_additional_permissions` 枚举值；新增 `additional_permissions` 对象（`file_system.read`/`file_system.write`/`network.enabled`）；新增 `environment_id` 参数 |
| `view_image` | 新增 `detail` 参数（`high`/`original`）；新增 `environment_id` 参数 |
| `request_user_input` | 新增 `is_secret` 参数 |

---

**总计: 31 个工具调用定义**（核心内置 8 + v0.137.0 新增 23+）

---

*更新于 ClawBench 项目工作目录；基于 Codex CLI v0.137.0 二进制逆向分析 + handler 源码路径交叉验证。*
