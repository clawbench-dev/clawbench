[中文](DEVELOPMENT.md) | [English](DEVELOPMENT.en.md)

# 编译与开发指南

## 快速开始

### 方式一：使用发布包（推荐）

从 [GitHub Releases](https://github.com/xulongzhe/clawbench/releases) 下载最新版 ZIP 包，解压即可部署。所有配置项均有默认值，无需配置文件即可启动。

```bash
# 1. 下载并解压
wget https://github.com/xulongzhe/clawbench/releases/latest/download/clawbench-linux-amd64.zip
unzip clawbench-linux-amd64.zip

# 2. 启动服务（无需配置文件）
cd clawbench
./clawbench
```

> 首次启动会自动生成8位随机密码并保存到 `.clawbench/auto-password`，启动时以字符框突出打印。如需自定义配置，可复制 `config/config.example.yaml` 为 `config/config.yaml` 并修改。

发布包内容（Linux）：

| 文件 | 说明 |
|------|------|
| `clawbench-linux-amd64` | 后端二进制 |
| `public/` | 前端静态资源（已构建） |
| `config/config.example.yaml` | 配置模板（可选） |
| `dev-server.sh` | 开发调试启动脚本 |

发布包内容（Windows）：

| 文件 | 说明 |
|------|------|
| `clawbench-windows-amd64.exe` | 后端二进制 |
| `public/` | 前端静态资源（已构建） |
| `config/config.example.yaml` | 配置模板（可选） |

### 方式二：从源码构建

**Linux/macOS：**

```bash
# 1. 克隆项目
git clone https://github.com/xulongzhe/clawbench.git
cd clawbench

# 2. 一键构建并启动（无需配置文件，所有项均有默认值）
./build.sh && ./clawbench
```

**开发调试模式**：

```bash
# 后台启动（Go dev 后端 + Vite 热更新）
./dev-server.sh

# 前台启动，查看实时日志
./dev-server.sh --fg

# 停止后台进程
./dev-server.sh --stop

# 重启
./dev-server.sh --restart
```

**Windows：**

```powershell
# 1. 克隆项目
git clone https://github.com/xulongzhe/clawbench.git
cd clawbench

# 2. 一键构建并启动（无需配置文件）
./build.sh; ./clawbench
```

**交叉编译**（在 Linux 上构建 Windows 二进制）：

```bash
./build.sh --windows
```

### 系统要求

| 平台 | 支持 |
|------|------|
| Linux x86_64 / ARM64 | ✅ |
| Windows x86_64 | ✅ |

| 依赖 | 说明 |
|------|------|
| **CodeBuddy CLI** 或 **Claude Code CLI** | AI 后端（需提前安装并完成认证，可选 OpenCode / Gemini / Codex / Qoder / VeCLI / MiMo） |

### 配置文件

`config/config.yaml` 完全可选，所有配置项均有默认值。如需自定义，复制 `config/config.example.yaml` 为 `config/config.yaml` 并修改。

**默认值**：

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `port` | 20000 | 服务端口 |
| `password` | 自动生成 8 位 hex | 首次生成后保存到 `.clawbench/auto-password`，重启复用 |
| `log_dir` | `<BinDir>/.clawbench/logs` | 二进制同级目录下 |
| `log_max_days` | 7 | 日志保留天数 |
| `upload.max_size_mb` | 100 | 上传大小上限 MB |
| `upload.max_files` | 20 | 单次上传文件数上限 |
| `chat.initial_messages` | 20 | 初始加载消息条数 |
| `chat.page_size` | 20 | 懒加载每页消息条数 |
| `chat.collapsed_height` | 150 | 历史消息折叠高度 px |
| `session.max_count` | 10 | 每项目会话上限 |
| `recent_projects.max_count` | 10 | 标题栏下拉显示的最近项目数量上限 |
| `terminal.enabled` | true | Web 终端默认启用 |
| `terminal.idle_timeout` | 10m | 终端空闲超时 |
| `terminal.max_sessions` | 10 | 每项目终端会话上限 |
| `port_forward.enabled` | true | 端口转发默认启用 |
| `port_forward.port` | 0 (auto) | SSH 端口（0 = 主端口+1） |
| `port_forward.host_key` | (auto) | Host key 文件路径 |
| `port_forward.allowed_ports` | (all) | 允许转发的端口范围 |
| `tts.engine` | edge | Edge TTS 免费无限制 |
| `tts.speed` | 1.0 | 正常语速 |
| `tts.max_cache_files` | 100 | TTS 语音缓存文件最大数量；超出时自动删除最旧的（-1=不限） |
| `tts.inline_code_max_len` | 100 | 行内代码保留最大字符数（rune），超出则删除 |
| `tts.max_summarize_runes` | 10000 | 总结输入最大字符数，超出截取尾部 |
| `summarize.backend` | simple | 统一总结后端（TTS 语音 + 定时任务共用），零延迟 |
| `summarize.model` | (空) | 总结模型，空则使用后端默认模型 |
| `summarize.api` | (空) | API 子配置（backend 为 api 时使用），含 base_url/key/format |
| `summarize.chat_summary` | true | 会话完成后自动生成最后助手消息摘要（`*bool`，nil=true） |

**自动密码机制**：未配置 `password` 时，系统自动生成 8 位十六进制随机密码，保存到 `.clawbench/auto-password`（权限 0600）。重启时复用已保存的密码，不会重新生成。配置 `password` 后自动删除该文件。启动时以字符框突出打印密码，方便查看。

**示例配置**：

```yaml
# 以下均为默认值，仅在需要覆盖时才需配置
# port: 20000
# password: "your_password"     # 不配置则自动生成8位hex密码
# default_agent: "assistant"   # 默认智能体，留空则使用第一个智能体
```

### 启动命令

#### 正式版

| 命令 | 说明 |
|------|------|
| `./clawbench` | 直接运行（前台，默认端口 20000） |
| `./clawbench --port 8080` | 指定端口 |
| `./clawbench --data-dir /data/.clawbench` | 指定数据目录 |

#### 开发调试模式

| 命令 | 说明 |
|------|------|
| `./dev-server.sh` | 后台启动（dev 后端 + Vite，端口 20002/20001） |
| `./dev-server.sh --fg` | 前台启动 |
| `./dev-server.sh --stop` | 停止进程 |
| `./dev-server.sh --restart` | 重启 |

> **注意**：开发调试与正式版使用独立端口和数据库，可同时运行，互不干扰。

---

## 高级配置

完整配置参考 `config/config.example.yaml`。所有项均可选，以下为覆盖默认值的示例：

```yaml
# port: 20000                        # 发布版服务端口（默认 20000）
# password: "your_password"          # 访问密码（不配置则自动生成 UUID 并保存）

# 默认智能体（可选）
default_agent: "assistant"      # 默认使用的智能体 ID，留空则使用第一个智能体
                                 # 可用智能体：assistant（全能助手）、coder（编码专家）、
                                 # gemini（Gemini ACP）、handyman（勤杂工）、codebuddy2（Gemini）、gpt54（GPT）

# 上传限制（默认 max_size_mb: 100, max_files: 20）
upload:
  max_size_mb: 10
  max_files: 20

# 日志配置（默认 .clawbench/logs, 7 天保留）
log_dir: ".clawbench/logs"
log_max_days: 7

# TLS (HTTPS) 配置（可选）
tls:
  enabled: false                # 启用 HTTPS
  cert_file: "/path/to/fullchain.pem"   # 证书文件
  key_file: "/path/to/privkey.pem"      # 私钥文件

# 端口转发 + SSH 隧道配置（默认启用，已合并为 port_forward）
port_forward:
  enabled: true                    # 启用 SSH 隧道服务（默认: true）
  port: 0                          # SSH 端口（0 = 自动 = 主端口+1）
  host_key: ""                     # Host key 文件路径（空 = 自动生成）
  allowed_ports: ""                # 允许转发的端口范围（空 = 允许所有）

# Chat UI 配置（默认 initial_messages: 20, page_size: 20, collapsed_height: 150）
chat:
  initial_messages: 20
  page_size: 20
  collapsed_height: 150
```

### AI 后端配置

ClawBench 通过调用本地 CLI 实现与 AI 编程工具的交互，无需额外 API Key 配置。

**CodeBuddy 后端**：安装 CodeBuddy CLI 并完成登录认证，确保 `codebuddy` 命令在 PATH 中可用。模型自动发现通过解析安装目录下的 `product.cloudhosted.json` 实现，支持 21+ 模型（GLM、DeepSeek、Kimi、MiniMax、Hunyuan 等）。

**Claude Code 后端**：安装 Claude Code CLI 并完成认证，确保 `claude` 命令在 PATH 中可用。

**OpenCode 后端**：安装 OpenCode CLI 并完成认证，确保 `opencode` 命令在 PATH 中可用。

**Gemini 后端**：仅支持 ACP 模式（通过 `gemini acp` 启动 stdio 子进程），无需单独安装 CLI。

**Codex 后端**：安装 OpenAI Codex CLI 并完成认证，确保 `codex` 命令在 PATH 中可用。模型自动发现通过二进制字符串/状态数据库扫描实现。

**Qoder 后端**：安装 Qoder CLI（阿里编码智能体）并完成认证，确保 `qoder` 命令在 PATH 中可用。Qoder 支持自动模型路由，无需指定具体模型。模型自动发现通过解析 `dynamic-texts.json` 实现。

**VeCLI 后端**：安装 VeCLI（火山引擎 Doubao）并完成认证，确保 `vecli` 命令在 PATH 中可用。VeCLI 输出纯文本（非 JSON Lines），不支持会话恢复，元数据通过 `--session-summary` 文件在进程退出后提取。模型自动发现通过解析 `MODEL_REGISTRY` 实现。

**CodeWhale 后端**：安装 CodeWhale（需 v0.8.33+）并完成认证，确保 `codewhale` 命令在 PATH 中可用。使用 `codewhale exec --auto --output-format stream-json` 模式，原生支持 `--system-prompt`、`--model`、`--resume` 参数。

**MiMo-Code 后端**：安装 MiMo-Code CLI 并完成认证，确保 `mimo` 命令在 PATH 中可用。MiMo-Code 是 OpenCode 的分支，复用 OpenCode 的 JSON 流格式和流解析器，支持 CLI + ACP 双模式。使用 `mimo run --format json` 模式，支持 `--session`、`--model`、`--variant`（思考档位）参数。ACP 模式通过 `mimo acp` 命令启用。

**Pi 后端**：安装 Pi CLI 并完成认证，确保 `pi` 命令在 PATH 中可用。Pi 是极简编程智能体，使用 `--mode json` 输出 NDJSON 事件流，支持会话恢复（`--session`/`--continue`）和模型指定（`--model`）。

十种后端可在 ClawBench Web UI 中实时切换，会话数据隔离。

### TTS 语音合成配置

ClawBench 支持 TTS 语音合成，自动将 AI 回复总结后朗读。支持 5 种 TTS 引擎和 12 种总结后端。

| TTS 引擎 | 说明 | 网络要求 |
|----------|------|---------|
| `edge` | 微软 Edge TTS，免费无限制（默认），原生 Go 实现（无 Python/CLI 依赖） | 需要网络 |
| `minimax` | 云端合成，音质最佳 | 需要 mmx CLI + API 配额 |
| `piper` | 本地离线，速度极快 | 无需网络 |
| `kokoro` | 本地离线，高质量中文 | 无需网络 |
| `moss-nano` | 本地离线，多语言，48kHz 音色克隆 | 首次需下载模型 |

各引擎的安装部署、配置示例、可用音色等详细说明请参阅 **[TTS 语音合成部署指南](TTS.md)**。

---

## 部署说明

### HTTPS 配置（公网部署）

生产环境建议启用 HTTPS：

1. **获取证书**：使用 Let's Encrypt 或其他 CA 签发证书
2. **配置 TLS**：在 `config/config.yaml` 中启用
   ```yaml
   tls:
     enabled: true
     cert_file: "/etc/letsencrypt/live/your-domain.com/fullchain.pem"
     key_file: "/etc/letsencrypt/live/your-domain.com/privkey.pem"
   ```
3. **重启服务**：重启 `./clawbench` 进程即可

### 多实例部署

ClawBench 支持在同一台服务器上运行多个实例（不同端口），各实例数据完全隔离：

```bash
# 实例 1：默认端口 20000
./clawbench

# 实例 2：指定不同端口
./clawbench --port 20300
```

浏览器不区分端口的 Cookie，因此多实例部署时 Cookie 会按端口自动添加前缀（如 `cb20300_`），防止同域名不同端口的实例间 Cookie 冲突。默认端口 20000 不添加前缀，保持向后兼容。

### Docker 部署

项目提供 Docker 部署支持，适用于容器化环境：

```bash
# 一键构建并启动（使用 scripts/docker-build.sh）
./scripts/docker-build.sh

# 停止容器
./scripts/docker-build.sh --stop

# 清理容器和镜像
./scripts/docker-build.sh --clean
```

或手动构建：

```bash
docker compose up -d
```

Docker 配置说明：
- **Dockerfile**：基于 Ubuntu 24.04，动态链接（glibc 2.39+），无 Python 运行时依赖（Edge TTS 为原生 Go 实现）
- **docker-compose.yml**：默认端口 20300，数据持久化到 Docker volume（`/data/.clawbench/`）
- **构建脚本**：`scripts/docker-build.sh` 构建镜像并启动容器

### Linux 动态链接

GitHub Release 中的 Linux 二进制使用动态链接（CGO_ENABLED=1），依赖 glibc 2.39+（Ubuntu 24.04+）。静态链接（`-extldflags '-static'`）已被移除，因为 DuckDB 的 CGO 代码与静态链接不兼容——编译通过但运行时 DuckDB 会触发 SIGSEGV。

### 数据存储

| 数据 | 路径 | 说明 |
|------|------|------|
| 数据库 | `二进制同级/.clawbench/ClawBench.db` | SQLite，会话/历史/项目/定时任务 |
| 日志 | `二进制同级/.clawbench/logs/` | 按天轮转，自动清理 |
| 自动密码 | `二进制同级/.clawbench/auto-password` | 未配置 password 时自动生成，重启复用 |
| 上传文件 | `项目目录/.clawbench/uploads/` | 用户上传的文件，属于具体项目 |

> 所有运行时数据存放在二进制同级目录下的 `.clawbench/`，实现绿色便携部署，删除程序目录即可彻底卸载。当项目目录与二进制目录相同时，上传文件也在同一个 `.clawbench/` 下。

### 开发调试模式

使用 `./dev-server.sh` 启动独立开发环境：

- 后端：`http://localhost:20002`
- 前端（Vite HMR）：`http://localhost:20001`
- 数据库：使用 `ClawBench-dev.db`，与正式版数据隔离
- RAG 向量库：使用 `rag-dev.duckdb`，与正式版向量数据隔离

```bash
./dev-server.sh              # 后台启动
./dev-server.sh --fg         # 前台启动
./dev-server.sh --stop       # 停止
./dev-server.sh --restart    # 重启
```

开发环境变量：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `VITE_BACKEND_PROTO` | `https` | Vite 开发代理的后端协议（开发环境设为 `http` 可跳过 HTTPS） |

---

## 架构设计

### 智能体架构

ClawBench 不只是一个"聊天壳"——它是一个完整的智能体运行平台：

- **Agent 数据库存储**：每个智能体存储在数据库中，包含专属 system prompt、模型、后端、思考档位，通过设置向导或自动发现创建
- **自动发现**：首次启动时若数据库中无智能体，自动扫描已安装的 AI CLI（claude、codebuddy、opencode、codex、qodercli、vecli、deepseek、mimo、pi），为每个检测到的后端在数据库中创建智能体记录。仅执行一次
- **共享规则**：规则模板内嵌于 Go 二进制（`commonRulesTemplate` in `agent.go`），定义所有智能体的公共行为和强制规则（RAG 搜索、媒体处理），避免重复配置。`@chatsearch`/`@task` 命令按需注入，取代旧的 `SCHEDULED_BEGIN/END` 标记
- **模板占位符**：`{{AVAILABLE_AGENTS}}` 自动替换为可用智能体列表，方便智能体间互相调度
- **多 Agent 调度**：不同任务匹配不同智能体，全能助手负责对话，专业 Agent 执行定时任务
- **工具调用透传**：AI 的工具调用（文件读写、Bash 命令、代码编辑）实时可视化展示
- **Cron 定时执行**：AI 通过 `clawbench task` CLI 子命令创建定时任务，确认后由 Cron 调度自动执行，聊天消息中内嵌任务卡片；`list` 和 `get` 子命令可查看已有任务，`--prompt` 支持 `@path` 语法从文件读取提示词
- **Cron 管控**：定时任务执行时不注入 `@task` 命令模板，防止 AI 递归创建任务；CLI 层通过 `CLAWBENCH_SCHEDULED=1` 环境变量提供双重保护
- **多后端可切换**：同一平台同时支持 CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi 后端（CLI 模式），以及 Gemini/Kimi（ACP 模式），会话数据隔离

### 项目结构

```
clawbench/
├── cmd/server/main.go           # 应用入口
├── internal/
│   ├── handler/                 # HTTP 处理器
│   │   ├── handler.go           # 路由注册
│   │   ├── auth.go              # 认证
│   │   ├── chat.go              # AI 聊天（SSE 流式推送 + @命令注入 + XML ask-question 解析）
│   │   ├── at_command.go        # @命令检测与模板注入（@chatsearch/@task）
│   │   ├── session_resume.go    # 恢复已删除会话（POST /api/ai/session/resume）
│   │   ├── chat_quick_send.go   # 快捷发送 CRUD
│   │   ├── agent.go             # Agent 管理
│   │   ├── scheduler.go         # 定时任务（CRUD + 执行列表 + 继续对话 GET/POST /api/tasks/{id}/executions/{execId}/continue）
│   │   ├── rag_api.go           # RAG 搜索 API
│   │   ├── file.go              # 文件读取
│   │   ├── file_ops.go          # 文件操作
│   │   ├── file_thumb.go        # 图片缩略图生成（方形画布 + 主色调填充）
│   │   ├── file_archive.go      # 文件打包下载（zip，防符号链接穿越）
│   │   ├── file_watch.go        # 文件变更 SSE 推送
│   │   ├── settings.go          # 设置（密码修改 + SHA-256 验证 + 加密密钥轮换）
│   │   ├── setup.go             # 设置向导（/api/setup/status, providers, models, verify, complete）
│   │   ├── upload.go            # 文件上传
│   │   ├── git.go               # Git 操作
│   │   ├── project.go           # 项目管理
│   │   ├── ssh_info.go          # SSH 隧道信息接口
│   │   ├── terminal.go          # 终端 + 快捷命令 CRUD + 多会话管理
│   │   └── static.go            # 静态文件
│   ├── middleware/              # 中间件（认证/日志/恢复/请求ID）
│   ├── platform/                # 平台适配（跨平台路径 + Windows CLI 工具）
│   │   ├── path.go              # ListRootPaths, IsPathUnderAnyRoot, ManglePath, ExpandTilde
│   │   ├── path_unix.go         # Unix: root = "/"
│   │   ├── path_windows.go      # Windows: 枚举可用驱动器根路径
│   │   ├── shell.go             # Shell 检测
│   │   ├── npm.go               # Windows .cmd 包装器解析（ResolveCLIPath，提取 JS 入口路径）
│   │   ├── strings.go           # 跨平台二进制字符串提取（ExtractStrings，替代 POSIX strings）
│   │   └── path_test.go / shell_test.go / npm_test.go / strings_test.go
│   ├── service/                 # 业务逻辑
│   │   ├── database.go          # SQLite 初始化
│   │   ├── chat.go              # 聊天历史管理
│   │   ├── continue_conversation.go # 继续对话（从任务执行继续 → 新会话）
│   │   ├── agent_store.go       # 智能体存储（DB-backed CRUD + agent_api_keys 表）
│   │   ├── agent_migration.go   # 智能体迁移（YAML → DB 一次性迁移，幂等）
│   │   ├── crypto.go            # API Key 加密（AES-256-GCM + HKDF-SHA256，密码修改时轮换密钥 + previousEncryptionKey 崩溃恢复）
│   │   ├── summary.go           # 聊天自动摘要（AsyncSummarize + summaries 表）
│   │   ├── scheduler.go         # 定时任务调度
│   │   ├── uuid.go              # UUID 工具
│   │   └── logger.go            # 文件日志（按天轮转）
│   ├── model/                   # 数据模型
│   │   ├── config.go / defaults.go / chat.go / file.go / agent.go / scheduler.go / path.go / ssh.go / discovery.go / provider_registry.go
│   │   └── errors.go
│   ├── ssh/                     # SSH 隧道服务器
│   │   ├── server.go            # SSH 服务器（direct-tcpip 端口转发）
│   │   └── server_test.go       # 测试
│   ├── proxy/                   # HTTP 反向代理 + 端口转发逻辑
│   │   ├── reverse_proxy.go     # HTTP 反向代理（解决 SSH 隧道 Host header 不匹配）
│   │   └── reverse_proxy_test.go # 测试
│   ├── cli/                     # CLI 子命令（AI 智能体自服务）
│   │   ├── task.go              # 定时任务子命令（create/update/delete/pause/resume/trigger/list/get/list-agents；--prompt 支持 @path 语法）
│   │   ├── migrate.go           # 一次性数据库迁移（task_executions 内容→chat_history）
│   │   ├── rag.go               # RAG 搜索子命令（search/message/session）
│   │   ├── help.go              # --help 自文档化基础设施
│   │   └── helpers.go           # 共享代码（loadConfig/apiURL/httpDo/TLS/cookie）
│   ├── ai/                      # AI 后端抽象
│       ├── interface.go         # AIBackend 接口
│       ├── factory.go           # 后端工厂
│       ├── cli_backend.go       # 共享 CLI 后端抽象
│       ├── common_stream.go     # 共享流参数/工具规范化/系统提示词
│       ├── stream_parser.go     # 共享流解析工具
│       ├── claude.go / claude_stream.go
│       ├── codebuddy.go / codebuddy_stream.go
│       ├── opencode.go / opencode_stream.go
│       ├── stream_json_parser.go # Gemini/Kimi 共享流解析器
│       ├── codex.go / codex_stream.go
│       ├── qoder.go / qoder_stream.go
│       ├── vecli.go / vecli_stream.go
│       ├── deepseek.go / deepseek_stream.go
│       ├── mimo.go              # MiMo-Code 后端（复用 OpenCode 流解析器）
│       └── pi.go / pi_stream.go
│   └── speech/                  # TTS 语音合成
│       ├── common_tts.go        # CLISpeechProvider 共享基类
│       ├── edge_tts.go          # Edge TTS（原生 Go gorilla/websocket 实现，DRM 令牌，无外部依赖）
│       ├── minimax.go / piper.go / kokoro.go / moss_tts_nano.go  # 其他 TTS 引擎实现
│   └── summarize/               # 文本总结（TTS + 任务执行摘要）
│       ├── summarizer.go        # Summarizer 接口 + 工厂方法
│       ├── simple.go            # 纯文本清洗
│       ├── ai_backend_summarizer.go # AIBackendSummarizer（CLI 后端总结）
│       ├── mmx.go               # MMXSummarizer（mmx-cli text chat）
│       ├── openai.go            # OpenAI Chat Completions API 总结
│       ├── anthropic.go         # Anthropic Messages API 总结
│       ├── strip_markdown.go    # Markdown 剥离
│       ├── task.go              # 任务执行摘要生成
├── web/src/components/common/  # 通用组件
│   ├── SummaryToggle.vue        # 摘要切换（按钮/标签模式）
├── config/                      # 配置目录
│   └── config.example.yaml      # 配置模板
├── web/                         # Vue 3 前端源码
│   └── src/
│       ├── components/          # Vue 组件
│       ├── composables/         # 组合式函数（useQuickSend、useQuickCommands、useChatStream 等）
│       ├── stores/              # 状态管理
│       └── utils/               # 工具函数
├── build.sh                     # 编译脚本
├── dev-server.sh                # 开发调试启动脚本
├── Dockerfile                   # Docker 镜像定义（Ubuntu 24.04 基础）
├── docker-compose.yml           # Docker Compose 配置
├── scripts/
│   ├── docker-build.sh          # Docker 一键构建脚本
│   └── fetch-provider-models.sh # 提供商模型数据生成脚本（models.dev API → provider_models.json，curl+jq）
└── vite.config.ts               # Vite 配置
```

---

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.25+ (net/http + SQLite) |
| 前端 | Vue 3 + Vite + TypeScript |
| 语法高亮 | highlight.js |
| Markdown | marked.js |
| 图表渲染 | Mermaid.js |
| 数学公式 | KaTeX |
| HTML 净化 | DOMPurify |
| AI 后端 | CodeBuddy CLI / Claude Code CLI / OpenCode CLI / Codex CLI / Qoder CLI / MiMo-Code CLI / VeCLI（流式输出 → SSE 推送）+ Gemini/Kimi（ACP stdio） |
| TTS 总结 | OpenAI/Anthropic 兼容 API（本地或云端，如 Ollama 使用 `format: "openai"`） |
| SSH 隧道 | golang.org/x/crypto/ssh（内嵌 SSH 服务器，direct-tcpip 端口转发） |
| 定时调度 | robfig/cron |
