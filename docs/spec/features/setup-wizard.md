# 首次访问欢迎面板（WelcomeOverlay）

> **重要说明**：本文档的历史版本（5 步设置向导）已废弃——引用的 `/api/setup/{status,models,verify,complete}` 端点、`PiConfig` 写入、`auth.json`/`models.json` 等机制在当前代码中**不存在**（`grep` 在 `internal/` 中零输出）。当前系统不再有独立的"设置向导"页面，Agent 创建直接通过 `AgentInstallDialog` 组件 + 数据库 `agents` 表完成。
> 
> 当前用户首次访问时看到的是 **`WelcomeOverlay`**——一个"已安装后端检测"面板，不是逐步向导。

## 概述

`WelcomeOverlay` 是首次访问 ClawBench 时显示的欢迎遮罩，提供后端安装状态总览和安装入口：

- **触发条件**：用户首次访问（`localStorage 'clawbench_welcome_dismissed'` 未设置）或通过 `clawbench-show-welcome` 自定义事件触发（`web/src/components/SettingsCategory.vue:270`）
- **关闭机制**：用户点击关闭后写入 `STORAGE_KEY`，之后不再显示
- **数据源**：实时拉取 `GET /api/backends`（12 个后端规格）和 `GET /api/agents`（已注册 Agent）

## 流程图

### WelcomeOverlay 数据流

```mermaid
sequenceDiagram
    participant F as Frontend (WelcomeOverlay.vue)
    participant H as handler
    participant DB

    F->>H: GET /api/backends
    H-->>F: 12 个后端规格 (含 install_cmd)
    F->>H: GET /api/agents
    H->>DB: SELECT * FROM agents
    DB-->>H: 已注册 Agent 列表
    H-->>F: agents 列表
    F->>F: 渲染后端列表<br/>已安装项高亮 + 安装入口
    Note over F: 用户点 rescan
    F->>H: POST /api/agents/rescan
    H->>H: SyncDiscoverAgentsDB
    H-->>F: 刷新结果
    Note over F: 用户关闭欢迎
    F->>F: localStorage.setItem<br/>(STORAGE_KEY, "1")
```

### AgentInstallDialog 安装流程

```mermaid
sequenceDiagram
    participant F as Frontend (AgentInstallDialog)
    participant H as handler
    participant S as service

    F->>H: GET /api/agents/:id/install-cmd
    H-->>F: 安装命令 (BackendSpec.InstallCmd)
    F->>F: 展示命令 + 复制按钮
    Note over F: 用户在终端执行
    F->>H: POST /api/agents/rescan
    H->>S: SyncDiscoverAgentsDB
    S->>S: 扫描 PATH + 验证 CLI
    S-->>H: 新检测到的 Agent
    H-->>F: agents 列表更新
```

## 功能与设计要点

### 功能清单

- **后端检测面板**：显示所有 12 个注册后端（`internal/model/BackendRegistry`），每项标注：
  - 后端名称 + 描述
  - 是否已检测到 CLI（来自 `agents` 表）
  - 安装命令（`BackendSpec.InstallCmd`，如 `"npm install -g @anthropic-ai/claude-code"`）
  - ACP 能力（是否支持 `Transport: "acp-stdio"`）
- **手动刷新**：`POST /api/agents/rescan` 触发 `SyncDiscoverAgentsDB`（`cmd/server/main.go:708`）重新扫描 PATH 中的 CLI
- **安装对话框**：`AgentInstallDialog` 组件打开后显示安装命令和复制按钮，引导用户在终端执行
- **持久化关闭状态**：用户关闭后写入 `localStorage['clawbench_welcome_dismissed']`，下次不再自动显示
- **事件触发重显**：`clawbench-show-welcome` 自定义事件（`SettingsCategory.vue:270`）允许设置页主动重新打开欢迎面板

### 设计要点

- **WelcomeOverlay 不是向导**：当前没有"分步创建 Agent"流程——Agent 创建走 `WelcomeOverlay`（检测/安装）→ `AgentInstallDialog`（执行 install_cmd）→ 自动发现的链路
- **不存在的端点澄清**：以下端点在当前代码中**不存在**，如在历史文档/对话中遇到应视为过期：
  - `GET /api/setup/status`
  - `POST /api/setup/models`
  - `POST /api/setup/verify`
  - `POST /api/setup/complete`
- **不存在的文件澄清**：以下概念在当前代码中**不存在**：
  - `PiConfig` 类型（Agent 配置走 SQL `agents` 表）
  - `auth.json` / `models.json` 文件
  - 内嵌 Pi 二进制检测
- **Agent 创建时机**：当前 Agent 创建有两种路径：
  1. 自动发现：`SyncDiscoverAgentsDB` 扫描 PATH 中的 CLI（启动时 + `rescan` 时）
  2. 手动安装：用户通过 `AgentInstallDialog` 在终端执行 `InstallCmd`，然后 `rescan` 触发发现

## 关键代码引用

| 文件 | 关键符号 |
|------|----------|
| `web/src/components/WelcomeOverlay.vue` | 首次访问欢迎遮罩组件 |
| `web/src/components/SettingsCategory.vue:270` | `clawbench-show-welcome` 事件触发 |
| `web/src/components/AgentInstallDialog.vue` | 安装命令对话框 |
| `internal/handler/handler.go` | `/api/backends`、`/api/agents`、`/api/agents/rescan` 路由 |
| `internal/model/discovery.go:239` | `SyncDiscoverAgentsDB` 函数 |
| `internal/model/agent.go` | `BackendSpec.InstallCmd` 字段 |
| `cmd/server/main.go:708` | 启动时调用 `SyncDiscoverAgentsDB` |
