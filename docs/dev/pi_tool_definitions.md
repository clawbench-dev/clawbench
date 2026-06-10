# Pi 工具调用 JSON Schema 定义

本文档给出 Pi (Coding Agent Harness) 当前会话中可用的全部工具的 **JSON Schema 结构**。
每个工具均使用与 OpenAI/Tool Use协议兼容的 JSON Schema 格式。

---

## 目录

- [Pi 内置工具](#pi-内置工具)
  - [Read - 读取文件](#read---读取文件)
  - [Write - 写入文件](#write---写入文件)
  - [Edit - 编辑文件](#edit---编辑文件)
  - [Bash - 执行命令](#bash---执行命令)
  - [Grep - 搜索内容](#grep---搜索内容)
  - [Find - 查找文件](#find---查找文件)
  - [Ls - 列出目录](#ls---列出目录)
- [工具配置选项](#工具配置选项)
- [汇总清单](#汇总清单)

---

## Pi 内置工具

### Read - 读取文件

读取本地文件系统中的一个文件。支持文本、图像、音频和 PDF 文件。

```json
{
  "name": "read",
  "description": "Reads a file from the local filesystem. Supports text, images (PNG, JPG, GIF, WEBP), audio files (MP3, WAV, AIFF), and PDF files.",
  "input_schema": {
    "type": "object",
    "properties": {
      "path": {
        "type": "string",
        "description": "The path to the file to read (can be absolute or relative)."
      },
      "offset": {
        "type": "number",
        "description": "The line number to start reading from (1-indexed)."
      },
      "limit": {
        "type": "number",
        "description": "The maximum number of lines to read (defaults to 2000)."
      }
    },
    "required": ["path"]
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `path` | string | 是 | 文件路径（绝对或相对路径） |
| `offset` | number | 否 | 起始行号（从1 开始） |
| `limit` | number | 否 | 最大读取行数（默认 2000） |

---

### Write - 写入文件

将内容写入本地文件系统中的文件，若已存在则覆盖。

```json
{
  "name": "write",
  "description": "Writes content to a specified file in the local filesystem. Overwrites existing file if one exists.",
  "input_schema": {
    "type": "object",
    "properties": {
      "path": {
        "type": "string",
        "description": "The path to the file to write (can be absolute or relative)."
      },
      "content": {
        "type": "string",
        "description": "The content to write to the file."
      }
    },
    "required": ["path", "content"]
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `path` | string | 是 | 文件路径（绝对或相对路径） |
| `content` | string | 是 | 要写入文件的内容 |

---

### Edit - 编辑文件

对文件执行精确字符串替换。可同时执行多个替换。

```json
{
  "name": "edit",
  "description": "Performs exact string replacement in a file. Can perform multiple edits in a single call.",
  "input_schema": {
    "type": "object",
    "properties": {
      "path": {
        "type": "string",
        "description": "The path to the file to modify (can be absolute or relative)."
      },
      "edits": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "oldText": {
              "type": "string",
              "description": "The exact text to replace. Must match exactly including whitespace and indentation."
            },
            "newText": {
              "type": "string",
              "description": "The new text to replace the old text with."
            }
          },
          "required": ["oldText", "newText"]
        },
        "description": "Array of edit operations to perform."
      }
    },
    "required": ["path", "edits"]
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `path` | string | 是 | 文件路径（绝对或相对路径） |
| `edits` | array | 是 | 编辑操作数组 |
| `edits[].oldText` | string | 是 | 要替换的精确文本 |
| `edits[].newText` | string | 是 |替换后的新文本 |

---

### Bash - 执行命令

在持久化 shell 会话中执行 bash 命令并返回其输出。

```json
{
  "name": "bash",
  "description": "Executes a given bash command in a persistent shell session with optional timeout, ensuring proper handling and security measures.",
  "input_schema": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "The command to execute."
      },
      "timeout": {
        "type": "number",
        "description": "Optional timeout in milliseconds. If not specified, commands will time out after the default timeout."
      }
    },
    "required": ["command"]
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `command` | string | 是 | 要执行的命令 |
| `timeout` | number | 否 | 超时时间（毫秒） |

---

### Grep - 搜索内容

基于正则表达式的文件内容搜索工具。

```json
{
  "name": "grep",
  "description": "Fast content search tool that works with any codebase size. Searches file contents using regular expressions.",
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
      "glob": {
        "type": "string",
        "description": "Glob pattern to filter files (e.g., '*.js', '*.{ts,tsx}')."
      },
      "ignoreCase": {
        "type": "boolean",
        "description": "If true, search is case-insensitive. Defaults to false."
      },
      "literal": {
        "type": "boolean",
        "description": "If true, treats pattern as a literal string instead of regex. Defaults to false."
      },
      "context": {
        "type": "number",
        "description": "Number of lines to show before and after each match."
      },
      "limit": {
        "type": "number",
        "description": "Maximum number of results to return."
      }
    },
    "required": ["pattern"]
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `pattern` | string | 是 | 正则表达式搜索模式 |
| `path` | string | 否 | 搜索目录（默认当前目录） |
| `glob` | string | 否 | 文件过滤模式（如 `*.js`） |
| `ignoreCase` | boolean | 否 | 是否忽略大小写（默认 false） |
| `literal` | boolean | 否 | 是否按字面量搜索（默认 false） |
| `context` | number | 否 | 匹配周围显示的行数 |
| `limit` | number | 否 | 返回结果上限 |

---

### Find - 查找文件

使用 glob 模式快速查找文件。

```json
{
  "name": "find",
  "description": "Fast file pattern matching tool that works with any codebase size. Supports glob patterns like '**/*.js' or 'src/**/*.ts'.",
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
      },
      "limit": {
        "type": "number",
        "description": "Maximum number of results to return."
      }
    },
    "required": ["pattern"]
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `pattern` | string | 是 | Glob 模式（如 `**/*.js`） |
| `path` | string | 否 | 搜索目录（默认当前目录） |
| `limit` | number | 否 | 返回结果上限 |

---

### Ls - 列出目录

列出目录中的文件和子目录。

```json
{
  "name": "ls",
  "description": "Lists the names of files and subdirectories directly within a specified directory path.",
  "input_schema": {
    "type": "object",
    "properties": {
      "path": {
        "type": "string",
        "description": "The path to the directory to list."
      },
      "limit": {
        "type": "number",
        "description": "Maximum number of entries to return."
      }
    }
  }
}
```

**参数说明：**

| 参数 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `path` | string | 否 | 要列出的目录路径（默认当前目录） |
| `limit` | number | 否 | 返回条目上限 |

---

## 工具配置选项

### 可插拔操作接口

每个工具都支持通过自定义 `operations` 接口来覆盖默认行为，实现对远程系统（如 SSH）的委托：

```typescript
// Read 工具的可插拔操作
interface ReadOperations {
  readFile: (absolutePath: string) => Promise<Buffer>;
  access: (absolutePath: string) => Promise<void>;
  detectImageMimeType?: (absolutePath: string) => Promise<string | null | undefined>;
}

// Write 工具的可插拔操作
interface WriteOperations {
  writeFile: (absolutePath: string, content: string) => Promise<void>;
  mkdir: (dir: string) => Promise<void>;
}

// Edit 工具的可插拔操作
interface EditOperations {
  readFile: (absolutePath: string) => Promise<Buffer>;
  writeFile: (absolutePath: string, content: string) => Promise<void>;
  access: (absolutePath: string) => Promise<void>;
}

// Bash 工具的可插拔操作
interface BashOperations {
  exec: (command: string, cwd: string, options: {
    onData: (data: Buffer) => void;
    signal?: AbortSignal;
    timeout?: number;
    env?: NodeJS.ProcessEnv;
  }) => Promise<{ exitCode: number | null }>;
}

// Grep 工具的可插拔操作
interface GrepOperations {
  isDirectory: (absolutePath: string) => Promise<boolean> | boolean;
  readFile: (absolutePath: string) => Promise<string> | string;
}

// Find 工具的可插拔操作
interface FindOperations {
  exists: (absolutePath: string) => Promise<boolean> | boolean;
  glob: (pattern: string, cwd: string, options: {
    ignore: string[];
    limit: number;
  }) => Promise<string[]> | string[];
}

// Ls 工具的可插拔操作
interface LsOperations {
  exists: (absolutePath: string) => Promise<boolean> | boolean;
  stat: (absolutePath: string) => Promise<{ isDirectory: () => boolean }>;
  readdir: (absolutePath: string) => Promise<string[]> | string[];
}
```

### 工具工厂函数

使用工具工厂函数创建工具实例：

```typescript
import {
  createReadTool,
  createWriteTool,
  createEditTool,
  createBashTool,
  createGrepTool,
  createFindTool,
  createLsTool,
  createCodingTools,
  createReadOnlyTools,
} from "@earendil-works/pi-coding-agent";

// 创建单个工具
const readTool = createReadTool(cwd, { autoResizeImages: true });
const bashTool = createBashTool(cwd, { timeout: 30000 });

// 创建工具组合
const codingTools = createCodingTools(cwd); // read, bash, edit, write
const readOnlyTools = createReadOnlyTools(cwd);   // read, grep, find, ls
```

---

## 汇总清单

### 工具名总表

| 工具名 | 类别 | 描述 | 必填参数 |
|--------|------|------|----------|
| `read` | 文件读取 | 读取本地文件或目录 | `path` |
| `write` | 文件写入 | 写入文件到本地文件系统 | `path`, `content` |
| `edit` | 文件编辑 | 对文件执行精确字符串替换 | `path`, `edits` |
| `bash` | 系统执行 | 执行 bash 命令并返回输出 | `command` |
| `grep` | 搜索检索 | 基于正则表达式的文件内容搜索 | `pattern` |
| `find` | 搜索检索 | 使用 glob 模式查找文件 | `pattern` |
| `ls` | 目录列表 | 列出目录中的文件和子目录 | - |

### 工具使用场景

| 场景 | 推荐工具 |
|------|----------|
| 查看文件内容 | `read` |
| 创建新文件 | `write` |
| 修改现有文件 | `edit` |
| 运行命令/脚本 | `bash` |
| 搜索代码内容 | `grep` |
| 查找特定文件 | `find` |
| 浏览目录结构 | `ls` |

### 默认工具组合

- **codingTools**: `read`, `bash`, `edit`, `write`
- **readOnlyTools**: `read`, `grep`, `find`, `ls`

---

**总计: 7 个内置工具调用定义**

---

*生成于 ClawBench 项目工作目录;基于 Pi Coding Agent SDK 工具清单。*