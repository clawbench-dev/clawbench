# Settings Page Design / 配置页设计

**Date:** 2026-09-11
**Status:** Draft (Post-Review)

## Overview

将散落在 localStorage、齿轮菜单、后端 config.yaml 中的所有配置，统一收归到一个独立的配置页面。用户通过右上角齿轮按钮触发全屏右侧抽屉，进入两层导航的 iOS/Android 风格设置页。

## UI Structure

### Entry Point

- **触发方式：** 右上角 AppHeader 齿轮按钮
- **展现形式：** 从右侧滑出的全屏抽屉（Drawer）
- **齿轮按钮原功能迁移：** 语言切换、主题切换、Android 日志捕获、重配服务器 → 全部迁移到配置页内

### Two-Level Navigation

**第一层：分类列表**

```
┌────────────────────────┐
│  ← 设置               │
├────────────────────────┤
│                        │
│  外观              >   │
│  聊天              >   │
│  Agent 偏好        >   │
│  文件管理          >   │
│  文件查看器        >   │
│  终端              >   │
│  TTS 语音          >   │
│  RAG 记忆          >   │
│  端口转发          >   │
│  SSH 隧道          >   │
│  Android           >   │
│  服务器            >   │
│  关于              >   │
│                        │
└────────────────────────┘
```

**第二层：分类详情页**（点进去后）

```
┌────────────────────────┐
│  ← 聊天               │
├────────────────────────┤
│                        │
│  自动朗读      [开关]  │
│  折叠高度    150    >  │  需重启
│  初始消息数  20     >  │  需重启
│  分页大小    20     >  │  需重启
│  系统提示间隔 10    >  │  需重启
│  最大会话数  10     >  │  需重启
│                        │
└────────────────────────┘
```

- 每个分类的详情页以 iOS 风格分组列表呈现
- 需重启的配置项右侧标小标签 `需重启`
- 每行可展开或跳转到编辑控件（开关、下拉、数字输入、文本输入）

### Cold/Hot Save Flow

- **热配置：** 修改后立即调 API，内存值更新，即时生效
- **冷配置：** 修改后跟热配置一样保存，但保存时弹窗提示

**保存弹窗（仅当有冷配置变更时出现）：**

```
┌────────────────────────────┐
│  以下配置需重启后生效：     │
│  TTS 引擎、折叠高度        │
│                            │
│  [ 稍后 ]    [ 立即重启 ]  │
└────────────────────────────┘
```

- 选"稍后"：关闭弹窗，配置已保存但未生效
- 选"立即重启"：调重启 API → 服务断连几秒 → 前端自动重连

## Categories & Config Items

### 外观 (Appearance)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 语言 (中文/English) | localStorage `clawbench-locale` | 单选 | 热 |
| 主题 (亮/暗) | localStorage `theme` | 单选 | 热 |

### 聊天 (Chat)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 自动朗读 | localStorage `clawbench-auto-speech` | 开关 | 热 |
| 折叠高度 | 后端 config `chat.collapsed_height` | 数字输入 | 冷 |
| 初始消息数 | 后端 config `chat.initial_messages` | 数字输入 | 冷 |
| 分页大小 | 后端 config `chat.page_size` | 数字输入 | 冷 |
| 系统提示注入间隔 | 后端 config `chat.system_prompt_interval` | 数字输入 | 冷 |
| 最大会话数 | 后端 config `session.max_count` | 数字输入 | 冷 |

### Agent 偏好 (Agent Preferences)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 每个 Agent 首选模型 | localStorage `clawbench_model_<agentId>` | 下拉选择 | 热 |
| 每个 Agent 思考强度 | localStorage `clawbench_thinking_<agentId>` | 下拉选择 | 热 |

每个 Agent 一个分组，组内含模型和思考强度两项。Agent 列表动态从 `/api/agents` 获取，非硬编码。

### 文件管理 (File Management)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 显示隐藏文件 | localStorage `clawbenchShowHidden` | 开关 | 热 |
| 视图模式 (列表/网格) | localStorage `clawbench-file-view` | 单选 | 热 |
| 上传大小限制 (MB) | 后端 config `upload.max_size_mb` | 数字输入 | 冷 |
| 上传文件数限制 | 后端 config `upload.max_files` | 数字输入 | 冷 |

### 文件查看器 (File Viewer)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 自动换行 | localStorage `clawbench-word-wrap` | 开关 | 热 |
| 显示行号 | localStorage `clawbench-line-numbers` | 开关 | 热 |

### 终端 (Terminal)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 字体大小 | localStorage `clawbench-terminal-font-size` | 滑块 (8-28) | 热 |
| 启用终端 | 后端 config `terminal.enabled` | 开关 | 冷 |
| 空闲超时 | 后端 config `terminal.idle_timeout` | 文本 (如 "10m") | 冷 |
| 最大会话数 | 后端 config `terminal.max_sessions` | 数字输入 | 冷 |
| 回放缓冲行数 | 后端 config `terminal.buffer_lines` | 数字输入 | 冷 |

### TTS 语音 (TTS)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 引擎 | 后端 config `tts.engine` | 下拉 (edge/minimax/piper/kokoro/moss-nano) | 冷 |
| TTS 模型 | 后端 config `tts.tts_model` | 文本输入 | 冷 |
| 输出格式 | 后端 config `tts.format` | 下拉 (mp3/wav/pcm) | 冷 |
| 总结后端 | 后端 config `tts.summarize_backend` | 下拉 (simple/api/cli 列表) | 冷 |
| 总结模型 | 后端 config `tts.summarize_model` | 文本输入 | 冷 |
| 语速 | 后端 config `tts.speed` | 滑块 (0.5-3.0) | 冷 |
| 音色 | 后端 config `tts.voice` | 文本输入 | 冷 |
| 缓存文件数 | 后端 config `tts.max_cache_files` | 数字输入 (-1=无限) | 冷 |

### RAG 记忆 (RAG)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 启用 | 后端 config `rag.enabled` | 开关 | 冷 |
| Ollama 地址 | 后端 config `rag.ollama_base_url` | 文本输入 | 冷 |
| 模型 | 后端 config `rag.ollama_model` | 文本输入 | 冷 |
| 分块大小 | 后端 config `rag.chunk_size` | 数字输入 | 冷 |
| 搜索结果数 | 后端 config `rag.search_limit` | 数字输入 | 冷 |
| 保留天数 | 后端 config `rag.retention_days` | 数字输入 (0=永久) | 冷 |

### 端口转发 (Proxy)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 启用代理 | 后端 config `proxy.enabled` | 开关 | 冷 |
| 允许端口范围 | 后端 config `proxy.allowed_ports` | 文本输入 | 冷 |

### SSH 隧道 (SSH)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 启用 SSH | 后端 config `ssh.enabled` | 开关 | 冷 |
| 端口 | 后端 config `ssh.port` | 数字输入 (0=自动) | 冷 |

### Android

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 调试日志捕获 | localStorage `android_log_capture` | 开关 | 热 |

### 服务器 (Server)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 重配服务器 | Android JS bridge | 按钮 | — |

仅 App 模式下显示。点击触发 `AndroidNative.showServerDialog()`。

### 关于 (About)

| 配置项 | 来源 | 控件 | 冷/热 |
|--------|------|------|-------|
| 服务端版本 | 后端 | 文字展示 | — |
| 前端版本 | 构建 | 文字展示 | — |
| App 版本 | Android JS bridge | 文字展示 | — |

仅展示，不可编辑。

### 不放入配置页的 localStorage 项

| Key | 原因 |
|-----|------|
| `clawbench_client_id` | 系统自动生成的设备标识，用户无需关心 |
| `currentProjectPath` | 运行时状态，已有项目选择器管理 |
| `clawbenchLastFile_<root>` | 运行时状态，自动记忆上次打开的文件 |
| `clawbench-terminal-symbol-freq` | 自适应排序数据，非用户配置 |
| `git-branch-collapsed` / `git-worktree-collapsed` | 折叠状态，非用户配置 |

### 不放入配置页的后端配置（安全/敏感）

| 配置项 | 原因 |
|--------|------|
| `port` / `host` | 端口和绑定地址变更风险高 |
| `password` | 密码不适合明文展示 |
| `log_level` / `log_dir` / `log_max_days` | 运维级配置，普通用户无需改 |
| `tls.*` | 证书路径变更可导致服务不可用 |
| `ssh.host_key` | 密钥文件路径，敏感 |
| `default_agent` | 已有 Agent 选择器管理 |
| `dev_port` | 开发模式专用 |
| `watch_dir` | 已有项目选择器管理 |
| `tts.piper.*` / `tts.kokoro.*` / `tts.moss_nano.*` | 各引擎的高级参数，默认即可 |
| `tts.api.*` | API 密钥和端点，敏感且复杂 |
| `tasks.*` | 定时任务总结配置，太小众 |
| `rag.chunk_overlap` / `rag.poll_interval` / `rag.batch_size` | RAG 内部调优参数，高级 |
| `tts.inline_code_max_len` / `tts.max_summarize_runes` | TTS 文本处理参数，高级 |
| `terminal.max_line_bytes` / `terminal.max_buffer_mb` | 终端内存限制，高级 |

## Backend API Design

### 1. GET /api/config — 读取配置

返回当前运行时配置（脱敏）。合并 `/api/watch-dir` 的配置字段，统一为一个入口。

**Request:** `GET /api/config` (需认证)

**Response:**

```json
{
  "chat": {
    "initial_messages": 20,
    "page_size": 20,
    "collapsed_height": 150,
    "system_prompt_interval": 10
  },
  "session": {
    "max_count": 10
  },
  "upload": {
    "max_size_mb": 100,
    "max_files": 20
  },
  "terminal": {
    "enabled": true,
    "idle_timeout": "10m",
    "max_sessions": 10,
    "buffer_lines": 2000
  },
  "tts": {
    "engine": "edge",
    "tts_model": "",
    "format": "",
    "summarize_backend": "simple",
    "summarize_model": "",
    "speed": 1.0,
    "voice": "",
    "max_cache_files": 100
  },
  "rag": {
    "enabled": false,
    "ollama_base_url": "http://localhost:11434",
    "ollama_model": "bge-m3",
    "chunk_size": 512,
    "search_limit": 5,
    "retention_days": 90
  },
  "proxy": {
    "enabled": true,
    "allowed_ports": "1024-65535"
  },
  "ssh": {
    "enabled": true,
    "port": 0
  }
}
```

不返回敏感字段（password、tls 等）。

**与 `/api/watch-dir` 的关系：** `GET /api/config` 的响应包含原 `/api/watch-dir` 返回的所有配置字段。前端 `app.ts` `loadProject()` 迁移为使用 `GET /api/config`，`/api/watch-dir` 仅保留 `watchDir` 字段供项目路径选择使用。

### 2. PATCH /api/config — 更新配置

支持部分更新，只传要改的字段。

**Request:** `PATCH /api/config` (需认证)

```json
{
  "chat": {
    "collapsed_height": 200
  },
  "tts": {
    "engine": "minimax"
  }
}
```

**字段白名单（安全核心）：** PATCH handler 必须维护显式的字段白名单，拒绝任何白名单外的字段。白名单精确对应上方"Categories & Config Items"表中所有后端 config 字段。不在白名单中的字段（如 `password`、`tls.*`）直接返回 `400 Bad Request`，即使前端不发送这些字段，后端也必须做防御性校验。

**处理逻辑：**

1. 解析请求体，逐字段校验白名单
2. 校验字段值合法性（如 `tts.engine` 必须是合法引擎名、数字字段必须在合理范围内）
3. 获取 `configMutex` 写锁
4. 合并到内存配置（`ConfigInstance`）
5. 写入 `config/config.yaml`（原子写入：先写 `config.yaml.tmp`，再 `os.Rename`）
6. 释放锁
7. 返回：

```json
{
  "needs_restart": true,
  "changed_cold_fields": ["chat.collapsed_height", "tts.engine"]
}
```

**错误响应：**

```json
// 字段不在白名单
{ "error": "forbidden_field", "message": "field 'password' is not allowed", "status": 400 }

// 字段值非法
{ "error": "invalid_value", "message": "tts.engine must be one of: edge,minimax,piper,kokoro,moss-nano", "status": 400 }

// yaml 文件写入失败（权限等）
{ "error": "write_failed", "message": "failed to write config.yaml: permission denied", "status": 500 }
```

### 3. 热配置字段列表

以下字段 PATCH 后立即在内存中生效，无需重启：

| 字段 | 热更新方式 |
|------|-----------|
| `chat.collapsed_height` | 更新全局变量 `ChatCollapsedHeight` |
| `chat.initial_messages` | 更新全局变量 `ChatInitialMessages` |
| `chat.page_size` | 更新全局变量 `ChatPageSize` |
| `chat.system_prompt_interval` | 更新全局变量 `ChatSystemPromptInterval` |
| `session.max_count` | 更新全局变量 `SessionMaxCount` |
| `upload.max_size_mb` | 更新全局变量 `UploadMaxSizeMB` |
| `upload.max_files` | 更新全局变量 `UploadMaxFiles` |
| `tts.max_cache_files` | 更新全局变量 `TTSMaxCacheFiles` |

> 注：虽然这些字段可以热更新内存值，但为了一致性，config.yaml 也同步写入。下次重启时 yaml 值和内存值一致。

**决定：** 为简化实现，首版所有后端配置字段统一标为"需重启"。PATCH 写入内存 + config.yaml，始终返回 `needs_restart: true`。前端统一弹重启提示。后续可根据用户反馈逐个字段改为真热更新。

### 4. ConfigInstance 线程安全

当前 `ConfigInstance` 是启动时写入一次、之后只读的全局变量。PATCH 使其变为可变后，必须保护并发访问：

**方案：** `sync.RWMutex` 保护 `ConfigInstance`

- 读操作（各 handler 读取配置值）：`RLock` / `RUnlock`
- 写操作（PATCH 合并）：`Lock` / `Unlock`
- 全局变量（`ChatCollapsedHeight` 等）：使用 `atomic.Value` 或 `sync/atomic` 包的对应类型

### 5. POST /api/config/restart — 重启服务

**Request:** `POST /api/config/restart` (需认证)

**Response:**

```json
{
  "status": "restarting"
}
```

#### 自重启机制

**核心问题：** 进程不能杀死自己后再无人拉起，也不能先启动新进程再自杀（端口冲突）。必须确保新进程在旧进程完全退出后才启动。

**方案：哨兵进程（Sentinel Process）**

```go
func handleRestart(c echo.Context) error {
    // 1. 立即返回响应，前端开始等待重连
    c.JSON(http.StatusOK, map[string]string{"status": "restarting"})

    // 2. 在 goroutine 中启动哨兵进程
    go func() {
        exe, _ := os.Executable()
        args := os.Args[1:]
        pid := os.Getpid()

        var cmd *exec.Cmd
        if runtime.GOOS == "windows" {
            // Windows: 固定延迟 + 进程组隔离
            sentinelScript := fmt.Sprintf(
                "timeout /t 2 /nobreak >nul & %s %s",
                exe, strings.Join(args, " "),
            )
            cmd = exec.Command("cmd", "/c", sentinelScript)
            cmd.SysProcAttr = &syscall.SysProcAttr{
                CreationFlags: 0x00000200, // CREATE_NEW_PROCESS_GROUP
            }
        } else {
            // Unix: kill -0 轮询等待进程退出，带重试
            sentinelScript := fmt.Sprintf(
                "PID=%d; EXE='%s'; ARGS='%s'; "+
                    "while kill -0 $PID 2>/dev/null; do sleep 0.1; done; "+
                    "for i in 1 2 3 4 5; do sleep 0.2; exec $EXE $ARGS && exit 0; done; "+
                    "echo 'restart-failed' > '%s/.clawbench/restart-status'",
                pid, exe, strings.Join(args, " "), binDir,
            )
            cmd = exec.Command("/bin/sh", "-c", sentinelScript)
            // Linux: 解耦进程组，防止父进程退出时连带杀哨兵
            cmd.SysProcAttr = &syscall.SysProcAttr{
                Setpgid: true,
            }
        }

        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        cmd.Env = os.Environ()
        cmd.Start()  // 哨兵进程立即返回，不影响当前进程

        // 3. 写重启标记文件（供新进程启动时检测）
        os.WriteFile(filepath.Join(binDir, ".clawbench", "restart-sentinel"), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)

        // 4. 给前端一点时间接收响应（约 200ms），然后优雅关闭
        time.Sleep(200 * time.Millisecond)
        // 触发 graceful shutdown（与 SIGINT 相同路径）
        shutdownSignal <- os.Interrupt
    }()

    return nil
}
```

**执行时序：**

```
t=0ms     前端调用 POST /api/config/restart
t=0ms     后端返回 { status: "restarting" }
t=0ms     哨兵进程启动，开始轮询 kill -0 <pid>
t=200ms   后端触发 graceful shutdown
t=~300ms  HTTP server 停止监听，释放端口
t=~300ms  SSE 连接断开，前端检测到断连
t=~500ms  当前进程退出
t=~500ms  哨兵检测到进程已死，exec 启动新进程（带重试）
t=~1s     新进程启动，绑定端口
t=~1.5s   前端 useReconnect 自动重连成功
t=~2s     配置页关闭，回到主界面
```

#### 进程管理器兼容（server.sh / systemd / Docker）

**检测逻辑：** 重启前先检测是否运行在进程管理器下：

```go
func isRunningUnderSupervisor() bool {
    // 1. systemd: 检查 INVOCATION_ID 环境变量或 /run/systemd/system
    if os.Getenv("INVOCATION_ID") != "" { return true }
    // 2. Docker: 检查 /.dockerenv 或 container 环境变量
    if os.Getenv("container") != "" { return true }
    // 3. PPID=1: init 进程子进程（通常 systemd/launchd）
    if os.Getppid() == 1 { return true }
    return false
}
```

**行为分支：**

| 环境 | 重启策略 |
|------|---------|
| 有进程管理器 (systemd/Docker) | 只做 graceful shutdown，由管理器重启。不启动哨兵进程 |
| server.sh 管理 | graceful shutdown → server.sh 的 wait 退出 → server.sh 可选 `--watchdog` 循环重新拉起 |
| 独立运行（桌面） | 启动哨兵进程，自重启 |

#### 配置回退机制

如果新配置导致新进程启动失败（如无效的 TTS 引擎名），服务会中断且无法自恢复。

**防御措施：**

1. **写入前校验：** PATCH 写入 config.yaml 前，先将新配置解析回 `Config` struct 并运行 `ApplyDefaults()` + 基础校验，确认配置合法
2. **备份旧配置：** 写入新 yaml 前，`cp config.yaml config.yaml.bak`
3. **哨兵失败标记：** 哨兵启动新进程失败时，写 `.clawbench/restart-status` 记录错误
4. **前端提示：** 重连超时后显示"重启失败，新配置可能无效。请检查服务日志或手动重启"
5. **手动回退指引：** 提示用户可删除 `config.yaml` 或从 `config.yaml.bak` 恢复

#### Android 平台限制

Android 下 Go 二进制运行在 App 进程内，`/bin/sh` 和 `kill -0` 不可用。

**处理方式：**
- 配置页的"重启"按钮在 Android App 模式下**不显示**（或灰显）
- Android 下的后端配置变更保存到 config.yaml，但标记为"下次启动时生效"
- 通过 `AndroidNative` bridge 可选实现 App 级别的重启（杀进程 + 重启 Activity）

#### Windows 兼容

- 用 `timeout /t 2` 替代 `kill -0` 轮询（2 秒延迟，留足 graceful shutdown 时间）
- `cmd.SysProcAttr` 设置 `CREATE_NEW_PROCESS_GROUP` 防止哨兵被连带杀死

## Frontend Component Design

### New Components

```
web/src/components/settings/
├── SettingsDrawer.vue      — 全屏右侧抽屉容器
├── SettingsIndex.vue       — 第一层：分类列表
├── SettingsCategory.vue    — 第二层：分类详情页（通用）
├── SettingsItem.vue        — 通用配置行组件
└── SettingsRestartDialog.vue — 重启确认弹窗
```

### SettingsDrawer.vue

- 全屏右侧抽屉，`position: fixed`
- **z-index 管理：** 使用 CSS 变量 `--z-settings-drawer`，值高于 BottomSheet 和 PopupMenu（在 `App.vue` 中统一定义 z-index 层级）
- 内部管理导航栈：`ref<string[]>([])`
  - 空栈 → 显示 `SettingsIndex`
  - 栈顶有值 → 显示对应 `SettingsCategory`
- 返回按钮：pop 栈，栈空时关闭抽屉
- 齿轮按钮点击：打开抽屉 + 清空栈

### SettingsIndex.vue

- 渲染分类列表，每行：图标 + 分类名 + 箭头
- 点击 → push 分类 ID 到导航栈

### SettingsCategory.vue

- Props: `categoryId: string`
- 根据 categoryId 渲染对应的配置项列表
- 每个配置项使用 `SettingsItem` 组件

### SettingsItem.vue

通用配置行，支持多种控件类型：

| type | 渲染 |
|------|------|
| `switch` | 右侧开关 |
| `select` | 右侧当前值 + 箭头，点击弹出 BottomSheet 选择器（移动端友好） |
| `number` | 右侧数字 + 箭头，点击弹出数字输入 |
| `text` | 右侧文本 + 箭头，点击弹出文本输入 |
| `slider` | 右侧滑块 |
| `action` | 右侧箭头，点击执行回调 |

Props:
```typescript
interface SettingsItemProps {
  label: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action'
  modelValue: any
  options?: { label: string; value: any }[]  // for select
  min?: number   // for number/slider
  max?: number   // for number/slider
  step?: number  // for number/slider
  placeholder?: string  // for text
  needsRestart?: boolean  // 显示"需重启"标签
  disabled?: boolean
}
```

Emits: `update:modelValue`

**select 控件移动端适配：** 使用 BottomSheet 而非 PopupMenu 展示选项列表，更符合移动端操作习惯。BottomSheet 从底部弹出，每行一个选项，当前值打勾。

### SettingsRestartDialog.vue

- 接收 `changedFields: string[]` 列表
- 显示"以下配置需重启后生效" + 字段名列表
- 两个按钮："稍后"、"立即重启"
- "立即重启" → `POST /api/config/restart` → 等待重连 → 关闭配置页
- **重启失败处理：** 如果 30 秒内未重连，显示"重启失败"提示，并提供"重试"按钮

### AppHeader.vue Changes

- 齿轮按钮：移除当前 `PopupMenu`，改为 emit `openSettings` 事件
- App.vue 监听 `openSettings`，打开 `SettingsDrawer`
- 连接状态按钮保持不变（不走配置页）

## Data Flow

### 读取配置

```
SettingsDrawer 打开
  → GET /api/config（后端配置）
  → GET /api/agents（Agent 列表，用于 Agent 偏好分组）
  → 读取 localStorage（前端配置）
  → 合并为统一视图渲染
```

### 保存配置

```
用户修改配置项
  → 区分来源：
    - localStorage 项：直接写 localStorage，即时生效
    - 后端配置项：PATCH /api/config（带白名单校验 + 并发锁 + 原子写入）
  → 后端返回 { needs_restart, changed_cold_fields }
  → 如果 needs_restart && changed_cold_fields.length > 0：
    → 弹出 SettingsRestartDialog
```

### 重启服务

```
用户点击"立即重启"
  → POST /api/config/restart
  → 前端显示"服务重启中…"
  → SSE 断连
  → useReconnect 自动重连
  → 重连成功后关闭配置页
  → 30 秒超时：显示"重启失败"提示
```

## Implementation Notes

### config.yaml 写入策略

**原子写入：** 写入 `config.yaml.tmp`，然后 `os.Rename` 为 `config.yaml`。`os.Rename` 在大多数文件系统上是原子的，避免崩溃导致半写文件。

**备份：** 写入前 `cp config.yaml config.yaml.bak`，供手动回退。

**presence map 处理：** 简化方案（首版）：整体重写 config.yaml，所有字段显式写入。缺点：布尔型字段如 `proxy.enabled` 原本是靠 absent 走默认值 `true`，重写后变成 `proxy: {enabled: true}` 显式声明。这改变了与 `ApplyDefaults()` 的交互方式——如果未来默认值变更，显式的 `true` 会覆盖新默认值。

**缓解措施：** 写入前，解析原始 config.yaml 记录哪些字段是用户显式设置的（非默认值），重写时只写用户显式设置的字段 + 本次 PATCH 变更的字段，其余字段不写入（让默认值逻辑生效）。实现上可维护一个 `userExplicitFields map[string]bool`，在首次加载 config.yaml 时从 presence map 初始化。

**首版简化：** 整体重写，接受 presence 语义变化。后续优化按用户显式字段精确写入。

### 热更新（后续优化）

首版所有后端配置标为"需重启"。后续可逐个字段添加热更新逻辑：

1. PATCH handler 中判断字段是否支持热更新
2. 支持的：直接更新全局变量 + 返回 `needs_restart: false`
3. 不支持的：仅更新内存值 + 写 yaml + 返回 `needs_restart: true`

### 安全考虑

- `PATCH /api/config` 和 `POST /api/config/restart` 均需认证（`middleware.Auth`）
- `GET /api/config` 脱敏返回，不含 password / tls key
- **PATCH 字段白名单：** 后端必须维护显式白名单，拒绝白名单外字段（即使前端不发送，也要做防御性校验）
- App 模式下的"重配服务器"保留 Android JS bridge 方式，不走配置 API

### 移动端适配

- 抽屉宽度：移动端 100vw，桌面端 max-width 420px 居右
- 分类详情页使用滑动切换动画（push/pop）
- 触摸友好的行高（≥ 48px）
- 开关/选择器控件大小符合触摸目标
- select 控件使用 BottomSheet 而非 PopupMenu

### 向后兼容

- 移除齿轮菜单的语言/主题处理器不影响页面加载——主题在 `App.vue` 的 `onMounted` 中从 localStorage 读取并应用，不依赖齿轮菜单
- localStorage 键名不变，迁移前后数据格式一致
- `app.ts` `loadProject()` 迁移为使用 `GET /api/config`，但 `/api/watch-dir` 保持兼容（仅返回 `watchDir`），确保未升级的前端仍可工作
