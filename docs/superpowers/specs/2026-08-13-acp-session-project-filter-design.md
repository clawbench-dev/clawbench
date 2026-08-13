# ACP 外部会话按当前项目过滤设计

日期：2026-08-13

## 背景与问题

「外部会话」名单（ACP 会话下载名单，`AcpSessionDrawer`）由 agent 端 `ListSessions` 返回，是**全局**的：它会列出该 agent 所有目录下的会话。用户在本项目里打开名单时，会看到其它项目、子目录的会话，导致：

- **3.1**：删除本地会话后，该会话在外部名单里出现异常/未命名。
- **3.2**：下载属于当前项目（但为子目录）的会话，不报错却建出空壳「未命名会话」。
- **3.3**：下载不属于当前项目的会话，报「该会话在智能体端不存在」，并把会话误归入当前目录。

根因：`ServeACPLoadSession` 用 cookie 的当前项目 cwd 去 spawn agent 查找会话，而会话实际运行在自己的 cwd 里，目录错位导致加载失败。ACP 的 `ListSessions` 返回的 `SessionInfo` 带有会话真实的 `cwd`，但前端在解析时丢弃了它。

## 目标

外部会话名单**只显示当前项目根下的会话**（绝对匹配，不含子目录、不含其它项目）。这样：

- 名单聚焦当前项目，其它项目的会话不出现；
- 下载失败路径（3.2/3.3）不可达；
- 3.1 的名字异常随之消失（名单里都是当前项目、有正常标题的会话）。

## 范围

**纯前端改动，后端零改动。**

后端 `ServeACPSessions`（`internal/handler/agent.go`）已通过 `writeJSON` 返回完整的 `[]acp.SessionInfo`，其中包含 `cwd` 字段（`types_gen.go` 中 `SessionInfo.Cwd string json:"cwd"`）。前端只是丢弃了该字段，需补回并用于过滤。

## 设计

### 1. `web/src/composables/useAcpSession.ts`

- 给 `AcpSessionInfo` 接口增加 `cwd: string`。
- 在 `loadAcpSessions` 的映射中补上 `cwd: s.cwd || ''`。

### 2. `web/src/components/chat/AcpSessionDrawer.vue`

- 从 app store 读取当前 `projectRoot`。
- 过滤逻辑：`filteredSessions` 只保留 `trimTrailingSlash(cwd) === trimTrailingSlash(projectRoot)` 的会话。子目录、其它项目、cwd 为空的会话一律不显示。
- 归一化：**仅去除尾斜杠**（不做符号链接解析）。
- 当有会话因属于其它项目/子目录被隐藏时，显示一行提示（如「有 N 个其它项目的会话未显示」），避免用户误以为名单异常。

### 3. 测试

- `useAcpSession.test.ts`：断言映射保留 `cwd`。
- `AcpSessionDrawer.test.ts`：断言只显示 `cwd === projectRoot` 的会话；子目录、其它项目被隐藏。

## 效果验证

- 名单只显示当前项目下的会话。
- 其它项目 / 子目录会话不显示，无法触发下载失败（3.2/3.3）。
- 3.1 名字异常消失。

## 非目标

- 不实现「自动切换目录再下载」的跨目录下载。
- 不做符号链接路径解析。
- 不改动后端 `ServeACPLoadSession` / `ServeACPSessions`。
