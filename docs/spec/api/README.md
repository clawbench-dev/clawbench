# API 文档

`openapi.yaml` 是 ClawBench HTTP API 的完整 OpenAPI 3.0 单文件规格（自包含、无外链 `$ref`），覆盖全部 143 个路径 / 181 个操作。

## 使用方式

- **文件管理器内预览**：在 ClawBench 里打开 `docs/spec/api/openapi.yaml`，文件查看器以 Swagger UI 渲染，可直接用 "Try it out" 测试（CORS 代理见 `/api/openapi-proxy`）。
- **外部工具**：任何 OpenAPI 3.0 工具（Swagger Editor、Postman、openapi-generator）都可直接读取本文件。

## 约定

- **鉴权**：除显式标注 `security: []` 的端点外，所有 `/api/` 路由都需会话 Cookie（`clawbench_session`）。免鉴权端点的原因写在各 operation 的 `description` 里（如 `/api/ssh/info` 供 Android 原生发现端口、`/api/client-log` 供原生上报日志）。
- **错误体**：统一为 `components/schemas/ErrorResponse`（`error` / `code` / `msgKey` / `detail`），`msgKey` 供前端本地化。
- **项目范围**：通过 `clawbench_project` Cookie 或 `project_path` 查询参数传递。

## 不建模的部分

以下端点无法用 OpenAPI 表达，规格里只做说明（`description` + 101/事件流响应）：

| 类型 | 端点 |
|------|------|
| WebSocket | `/api/ai/events/ws`、`/api/tts/audio/ws`、`/api/stt/transcribe/ws`、`/api/terminal/ws` |
| SSE | `/api/file/watch`、`/api/dir/search`、`/api/tts/stream/{jobId}` |

聊天流式内容统一经 `/api/ai/events/ws` 推送，不在本文件建模。

## Tag 分组

Auth、System、Config、Theme、Projects、Chat、Sessions、Queue、Events、Git、Files、Share、Forge、Agents、TTS、STT、Terminal、Tasks、RAG、Upgrade、Proxy、SSH/FRP、Fonts、Upload、ClientLogs、APK。

## 维护

路由注册的唯一来源是 `internal/handler/handler.go` 的 `RegisterRoutes`。新增或删除端点时同步更新本文件；已移除的端点（如 `/api/files`、`/api/git/status`、`/api/terminal/config`）在相关操作的 `description` 中标注了取代者。
