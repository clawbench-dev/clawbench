# API 文档

规格源文件位于 **`internal/api/openapi.yaml`**（本目录不再存放 `openapi.yaml`）。

> 移动原因：规格需通过 `go:embed` 编入二进制，用于渲染内置斜杠命令注入给 AI 的接口说明；而 `go:embed` 不能跨模块目录向上引用。**编辑规格请改 `internal/api/openapi.yaml`。**

它是 ClawBench HTTP API 的完整 OpenAPI 3.0 单文件规格（自包含、无外链 `$ref`），覆盖全部 155 个路径 / 194 个操作。

## 使用方式

- **文件管理器内预览**：在 ClawBench 里打开 `internal/api/openapi.yaml`，文件查看器以 Swagger UI 渲染，可直接用 "Try it out" 测试（CORS 代理见 `/api/openapi-proxy`）。
- **外部工具**：任何 OpenAPI 3.0 工具（Swagger Editor、Postman、openapi-generator）都可直接读取本文件。

## 约定

- **鉴权**：除显式标注 `security: []` 的端点外，所有 `/api/` 路由都需会话 Cookie（`clawbench_session`）或本机 AI 的短时 `aiToken`。免鉴权端点的原因写在各 operation 的 `description` 里（如 `/api/ssh/info` 供 Android 原生发现端口、`/api/health` 供原生在登录前做身份探测）。
- **错误体**：统一为 `components/schemas/ErrorResponse`（`error` / `code` / `msgKey` / `detail`），`msgKey` 供前端本地化。
- **项目范围**：通过 `clawbench_project` Cookie 或 `project_path` 查询参数传递。

## 不建模的部分

以下端点无法用 OpenAPI 表达，规格里只做说明（`description` + 101/事件流响应）：

| 类型 | 端点 |
|------|------|
| WebSocket | `/api/ai/events/ws`、`/api/tts/audio/ws`、`/api/stt/transcribe/ws`、`/api/terminal/ws`、`/api/file/watch/ws` |
| SSE | `/api/dir/search`、`/api/file/content-search`、`/api/tts/stream/{jobId}` |

聊天流式内容统一经 `/api/ai/events/ws` 推送，不在本文件建模。

## Tag 分组

Auth、System、Config、Theme、Projects、Chat、Sessions、Queue、Events、Git、Files、Share、Forge、Agents、TTS、STT、Terminal、Tasks、RAG、Upgrade、Proxy、SSH/FRP、Fonts、Upload、ClientLogs、APK。

## 与实现的同步

除人工维护外，还有**双向漂移守卫**（`internal/handler/openapi_drift_test.go`）自动拦截脱节：

- 规格声明了但未注册的路径 → 测试失败（否则 AI 会被指引调用 404）
- 注册了但规格未记录的 `/api/` 路由 → 测试失败
- 鉴权标注与路由的 `middleware.Auth` 包裹不一致 → 测试失败

## 内置斜杠命令的接口注入

内置斜杠命令的提示词片段由 `internal/api` 从规格渲染，**按 operationId 精确选取**（而非按 tag —— tag 过粗，会把删 agent、重建索引等破坏性操作一并注入）：

| 命令 | 注入的 operationId |
|------|-------------------|
| `/cb-chatsearch` | `ragSearch`、`ragMessage`、`ragSession`、`ragSessionSearch` |
| `/cb-task` | `tasksList`、`tasksCreate`、`taskGet`、`taskUpdate`、`taskDelete`、`taskExecutions`、`agentsList` |
| `/cb-usage` | `usageStats` |

选取列表见 `internal/api/render.go` 的 `commandOperations`；`internal/api/render_test.go` 断言每个 operationId 存在且仍带预期 tag。

## 维护

本文件是**手工维护**的，不会自动生成，因此极易与实际实现脱节。任何新增 / 删除 / 修改 `/api/` 端点（路径、方法、鉴权、query 参数、请求体字段、响应字段、状态码）都必须同步更新 `openapi.yaml` —— 这是 `AGENTS.md` 开发规则中的硬性要求。

要点：

- 路由注册的唯一来源是 `internal/handler/handler.go` 的 `RegisterRoutes`。
- 字段名与参数名**必须从 handler 代码里抄**：对照 `decodeJSON` 结构体的 JSON tag、`r.URL.Query().Get(...)`、`requireMethod(...)` / `switch r.Method`。**禁止凭路由名望文生义**——这是过去 40+ 处不一致的根因。
- 留意通配路由的子路径分发：`/api/tasks/`（`{id}` 与 `executions` 子路径）、`/api/agents/`、`/api/fs/file/`、`/api/fs/raw/`、`/api/share/`、`/api/chat/quick-send/` 等。
- 鉴权变化须同步 `security` 标注（默认 `cookieAuth` 或本机 AI 的 `aiToken`，免鉴权端点显式写 `security: []`）。
- 已移除的端点（如 `/api/files`、`/api/git/status`、`/api/terminal/config`）在相关操作的 `description` 中标注了取代者。
- 改完自检：路由无遗漏无多余、YAML 合法、无重复 `operationId`、`$ref` 可解析。
