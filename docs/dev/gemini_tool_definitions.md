# Gemini CLI 工具调用定义

本文档记录了 Gemini CLI Agent 支持的所有工具及其对应的 JSON 调用结构。

## 1. list_directory
列出指定目录下的文件和子目录。

```json
{
  "name": "list_directory",
  "description": "Lists the names of files and subdirectories directly within a specified directory path. Can optionally ignore entries matching provided glob patterns.",
  "parameters": {
    "type": "object",
    "properties": {
      "dir_path": {
        "type": "string",
        "description": "The path to the directory to list"
      },
      "file_filtering_options": {
        "type": "object",
        "properties": {
          "respect_gemini_ignore": {
            "type": "boolean",
            "description": "Optional: Whether to respect .geminiignore patterns when listing files. Defaults to true."
          },
          "respect_git_ignore": {
            "type": "boolean",
            "description": "Optional: Whether to respect .gitignore patterns when listing files. Only available in git repositories. Defaults to true."
          }
        },
        "description": "Optional: Whether to respect ignore patterns from .gitignore or .geminiignore"
      },
      "ignore": {
        "type": "array",
        "items": {
          "type": "string"
        },
        "description": "List of glob patterns to ignore"
      }
    },
    "required": [
      "dir_path"
    ]
  }
}
```

## 2. read_file
读取并返回指定文件的内容。支持文本、图像、音频和 PDF 文件。

```json
{
  "name": "read_file",
  "description": "Reads and returns the content of a specified file. If the file is large, the content will be truncated. Handles text, images (PNG, JPG, GIF, WEBP, SVG, BMP), audio files (MP3, WAV, AIFF, AAC, OGG, FLAC), and PDF files.",
  "parameters": {
    "type": "object",
    "properties": {
      "end_line": {
        "type": "number",
        "description": "Optional: The 1-based line number to end reading at (inclusive)."
      },
      "file_path": {
        "type": "string",
        "description": "The path to the file to read."
      },
      "start_line": {
        "type": "number",
        "description": "Optional: The 1-based line number to start reading from."
      }
    },
    "required": [
      "file_path"
    ]
  }
}
```

## 3. grep_search
在文件内容中搜索正则表达式模式。

```json
{
  "name": "grep_search",
  "description": "Searches for a regular expression pattern within file contents.",
  "parameters": {
    "type": "object",
    "properties": {
      "after": {
        "type": "integer",
        "minimum": 0,
        "description": "Show this many lines after each match (equivalent to grep -A). Defaults to 0 if omitted."
      },
      "before": {
        "type": "integer",
        "minimum": 0,
        "description": "Show this many lines before each match (equivalent to grep -B). Defaults to 0 if omitted."
      },
      "case_sensitive": {
        "type": "boolean",
        "description": "If true, search is case-sensitive. Defaults to false (ignore case) if omitted."
      },
      "context": {
        "type": "integer",
        "minimum": 0,
        "description": "Show this many lines of context around each match (equivalent to grep -C). Defaults to 0 if omitted."
      },
      "dir_path": {
        "type": "string",
        "description": "Directory or file to search. Directories are searched recursively. Relative paths are resolved against current working directory. Defaults to current working directory ('.') if omitted."
      },
      "exclude_pattern": {
        "type": "string",
        "description": "Optional: A regular expression pattern to exclude from the search results. If a line matches both the pattern and the exclude_pattern, it will be omitted."
      },
      "fixed_strings": {
        "type": "boolean",
        "description": "If true, treats the 'pattern' as a literal string instead of a regular expression. Defaults to false (basic regex) if omitted."
      },
      "include_pattern": {
        "type": "string",
        "description": "Glob pattern to filter files (e.g., '*.ts', 'src/**'). Recommended for large repositories to reduce noise. Defaults to all files if omitted."
      },
      "max_matches_per_file": {
        "type": "integer",
        "minimum": 1,
        "description": "Optional: Maximum number of matches to return per file. Use this to prevent being overwhelmed by repetitive matches in large files."
      },
      "names_only": {
        "type": "boolean",
        "description": "Optional: If true, only the file paths of the matches will be returned, without the line content or line numbers. This is useful for gathering a list of files."
      },
      "no_ignore": {
        "type": "boolean",
        "description": "If true, searches all files including those usually ignored (like in .gitignore, build/, dist/, etc). Defaults to false if omitted."
      },
      "pattern": {
        "type": "string",
        "description": "The pattern to search for. By default, treated as a Rust-flavored regular expression. Use '\\b' for precise symbol matching (e.g., '\\bMatchMe\\b')."
      },
      "total_max_matches": {
        "type": "integer",
        "minimum": 1,
        "description": "Optional: Maximum number of total matches to return. Use this to limit the overall size of the response. Defaults to 100 if omitted."
      }
    },
    "required": [
      "pattern"
    ]
  }
}
```

## 4. glob
使用 glob 模式高效查找文件。

```json
{
  "name": "glob",
  "description": "Efficiently finds files matching specific glob patterns (e.g., 'src/**/*.ts', '**/*.md'), returning absolute paths sorted by modification time (newest first). Ideal for quickly locating files based on their name or path structure, especially in large codebases.",
  "parameters": {
    "type": "object",
    "properties": {
      "case_sensitive": {
        "type": "boolean",
        "description": "Optional: Whether the search should be case-sensitive. Defaults to false."
      },
      "dir_path": {
        "type": "string",
        "description": "Optional: The absolute path to the directory to search within. If omitted, searches the root directory."
      },
      "pattern": {
        "type": "string",
        "description": "The glob pattern to match against (e.g., '**/*.py', 'docs/*.md')."
      },
      "respect_gemini_ignore": {
        "type": "boolean",
        "description": "Optional: Whether to respect .geminiignore patterns when finding files. Defaults to true."
      },
      "respect_git_ignore": {
        "type": "boolean",
        "description": "Optional: Whether to respect .gitignore patterns when finding files. Only available in git repositories. Defaults to true."
      }
    },
    "required": [
      "pattern"
    ]
  }
}
```

## 5. replace
替换文件内的文本。需要提供精确的匹配上下文。

```json
{
  "name": "replace",
  "description": "Replaces text within a file. By default, the tool expects to find and replace exactly ONE occurrence of 'old_string'. If you want to replace multiple occurrences of the exact same string, set 'allow_multiple' to true. This tool requires providing significant context around the change to ensure precise targeting.",
  "parameters": {
    "type": "object",
    "properties": {
      "allow_multiple": {
        "type": "boolean",
        "description": "If true, the tool will replace all occurrences of 'old_string'. If false (default), it will only succeed if exactly one occurrence is found."
      },
      "file_path": {
        "type": "string",
        "description": "The path to the file to modify."
      },
      "instruction": {
        "type": "string",
        "description": "A clear, semantic instruction for the code change, acting as a high-quality prompt for an expert LLM assistant."
      },
      "new_string": {
        "type": "string",
        "description": "The exact literal text to replace 'old_string' with, preferably unescaped. Provide the EXACT text. Ensure the resulting code is correct and idiomatic."
      },
      "old_string": {
        "type": "string",
        "description": "The exact literal text to replace, preferably unescaped. For single replacements (default), include at least 3 lines of context BEFORE and AFTER the target text, matching whitespace and indentation precisely."
      }
    },
    "required": [
      "file_path",
      "instruction",
      "old_string",
      "new_string"
    ]
  }
}
```

## 6. write_file
向文件写入内容。

```json
{
  "name": "write_file",
  "description": "Writes content to a specified file in the local filesystem.",
  "parameters": {
    "type": "object",
    "properties": {
      "content": {
        "type": "string",
        "description": "The content to write to the file. Do not use omission placeholders; provide complete literal content."
      },
      "file_path": {
        "type": "string",
        "description": "The path to the file to write to."
      }
    },
    "required": [
      "file_path",
      "content"
    ]
  }
}
```

## 7. web_fetch
从 URL 处理内容，包括本地和私有网络地址。

```json
{
  "name": "web_fetch",
  "description": "Processes content from URL(s), including local and private network addresses (e.g., localhost), embedded in a prompt.",
  "parameters": {
    "type": "object",
    "properties": {
      "prompt": {
        "type": "string",
        "description": "A comprehensive prompt that includes the URL(s) (up to 20) to fetch and specific instructions on how to process their content."
      }
    },
    "required": [
      "prompt"
    ]
  }
}
```

## 8. run_shell_command
执行 shell 命令。

```json
{
  "name": "run_shell_command",
  "description": "This tool executes a given shell command as 'bash -c <command>'.",
  "parameters": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "Exact bash command to execute as 'bash -c <command>'"
      },
      "delay_ms": {
        "type": "integer",
        "description": "Optional. Delay in milliseconds to wait after starting the process in the background. Useful to allow the process to start and generate initial output before returning."
      },
      "description": {
        "type": "string",
        "description": "Brief description of the command for the user. Be specific and concise."
      },
      "dir_path": {
        "type": "string",
        "description": "(OPTIONAL) The path of the directory to run the command in. If not provided, the project root directory is used."
      },
      "is_background": {
        "type": "boolean",
        "description": "Set to true if this command should be run in the background (e.g. for long-running servers or watchers)."
      }
    },
    "required": [
      "command"
    ]
  }
}
```

## 9. list_background_processes
列出当前活动的后台进程。

```json
{
  "name": "list_background_processes",
  "description": "Lists all active and recently completed background shell processes orchestrating by the agent.",
  "parameters": {
    "type": "object",
    "properties": {
      "wait_for_previous": {
        "type": "boolean",
        "description": "Set to true to wait for all previously requested tools in this turn to complete before starting."
      }
    }
  }
}
```

## 10. read_background_output
读取后台进程的输出。

```json
{
  "name": "read_background_output",
  "description": "Reads the output log of a background shell process. Support reading tail snapshot.",
  "parameters": {
    "type": "object",
    "properties": {
      "delay_ms": {
        "type": "integer",
        "description": "Optional. Delay in milliseconds to wait before reading the output."
      },
      "lines": {
        "type": "integer",
        "minimum": 1,
        "description": "Optional. Number of lines to read from the end of the log. Defaults to 100."
      },
      "pid": {
        "type": "integer",
        "description": "The process ID (PID) of the background process to inspect."
      },
      "wait_for_previous": {
        "type": "boolean",
        "description": "Set to true to wait for all previously requested tools in this turn to complete before starting."
      }
    },
    "required": [
      "pid"
    ]
  }
}
```

## 11. save_memory
保存持久化记忆（全局或项目级）。

```json
{
  "name": "save_memory",
  "description": "Saves concise user context (preferences, facts) for use across future sessions.",
  "parameters": {
    "type": "object",
    "properties": {
      "fact": {
        "type": "string",
        "description": "The specific fact or piece of information to remember."
      },
      "scope": {
        "type": "string",
        "enum": [
          "global",
          "project"
        ],
        "description": "Where to save the memory. 'global' (default) saves to a file loaded in every workspace. 'project' saves to a project-specific file private to the user."
      }
    },
    "required": [
      "fact"
    ]
  }
}
```

## 12. google_web_search
执行 Google 搜索。

```json
{
  "name": "google_web_search",
  "description": "Performs a web search using Google Search (via the Gemini API) and returns the results.",
  "parameters": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "The search query to find information on the web."
      }
    },
    "required": [
      "query"
    ]
  }
}
```

## 13. enter_plan_mode
进入计划模式进行复杂任务的研究与设计。

```json
{
  "name": "enter_plan_mode",
  "description": "Switch to Plan Mode to safely research, design, and plan complex changes using read-only tools.",
  "parameters": {
    "type": "object",
    "properties": {
      "reason": {
        "type": "string",
        "description": "Short reason explaining why you are entering plan mode."
      }
    }
  }
}
```

## 14. invoke_agent
调用子 Agent 执行特定任务。

```json
{
  "name": "invoke_agent",
  "description": "Invoke a subagent to perform a specific task or investigation.",
  "parameters": {
    "type": "object",
    "properties": {
      "agent_name": {
        "type": "string",
        "description": "Name of the subagent to invoke (e.g., codebase_investigator, cli_help, generalist)"
      },
      "prompt": {
        "type": "string",
        "description": "The COMPLETE query to send the subagent. MUST be comprehensive and detailed."
      },
      "wait_for_previous": {
        "type": "boolean",
        "description": "Set to true to wait for all previously requested tools in this turn to complete before starting."
      }
    },
    "required": [
      "agent_name",
      "prompt"
    ]
  }
}
```

## 15. activate_skill
激活专用 Agent 技能。

```json
{
  "name": "activate_skill",
  "description": "Activates a specialized agent skill by name (Available: 'skill-creator', 'mmx-cli', 'grill-with-docs', 'grill-me', 'find-skills').",
  "parameters": {
    "type": "object",
    "properties": {
      "name": {
        "type": "string",
        "enum": [
          "skill-creator",
          "mmx-cli",
          "grill-with-docs",
          "grill-me",
          "find-skills"
        ],
        "description": "The name of the skill to activate."
      }
    },
    "required": [
      "name"
    ]
  }
}
```
