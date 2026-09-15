[中文](RAG.md) | [English](RAG.en.md)

# RAG 历史记忆部署指南

ClawBench 内置 RAG 历史记忆系统。系统持续把聊天消息分块写入主 SQLite 数据库，通过 FTS5 提供全文搜索，并在嵌入服务可用时通过 sqlite-vec 的 `vec0` 虚拟表提供向量搜索。嵌入服务不可用时会自动退化为 FTS-only，RAG 不需要单独启用。

## 系统架构

```text
聊天消息 → Indexer → 文本提取 → 分块 → SQLite chat_chunks + FTS5
                                      └→ OpenAI 兼容 Embedding → vec0

AI 智能体 → RAG API（HTTP）→ RRF 混合检索 → 历史片段
```

所有 RAG 数据都保存在 `<data-dir>/ClawBench.db`。系统不会创建 `rag.duckdb` 或独立向量数据库。

## 嵌入服务

默认配置指向 Ollama：

```bash
ollama serve
ollama pull bge-m3
curl http://localhost:11434/api/tags
```

也可以使用任意提供 `/v1/models` 和 `/v1/embeddings` 的 OpenAI 兼容服务。没有可用的嵌入服务时，消息仍会被分块并写入 FTS5；服务恢复后 Indexer 会回填缺失向量。

## 配置

`config/config.yaml` 是可选的。默认配置已经启用索引和全文检索：

```yaml
rag:
  base_url: "http://localhost:11434"
  model: "bge-m3"
  api_key: ""
  chunk_size: 512
  chunk_overlap: 64
  poll_interval: "5s"
  batch_size: 50
  search_limit: 100
  search_pool_size: 20
  retention_days: 90
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `base_url` | `http://localhost:11434` | OpenAI 兼容 API 基址 |
| `model` | `bge-m3` | 嵌入模型名称 |
| `api_key` | 空 | 云端嵌入服务的可选密钥 |
| `chunk_size` | `512` | 分块 token 数 |
| `chunk_overlap` | `64` | 相邻分块重叠 token 数 |
| `poll_interval` | `5s` | Indexer 轮询间隔 |
| `batch_size` | `50` | 每轮处理的消息数（热生效） |
| `search_limit` | `100` | 默认结果数 |
| `search_pool_size` | `20` | 各检索源参与 RRF 融合的候选数 |
| `retention_days` | `90` | 软删除数据保留天数；`0` 表示永久保留 |

`ollama_base_url` 和 `ollama_model` 仅作为旧配置兼容项保留，新配置应使用 `base_url` 和 `model`。

## 索引与搜索

Indexer 每轮读取未索引消息，提取用户文本和助手的 `text` 内容块，跳过 thinking、tool_use、warning 和 error 内容，然后按滑动窗口分块。每条消息最多生成 50 个分块。

搜索优先融合 FTS5 与向量候选；嵌入不可用时只运行全文检索。切换到不同维度的嵌入模型时，系统检测维度差异并重建向量索引，SQLite 中的文本分块仍然保留，随后自动回填向量。

推荐直接调用 HTTP API：

```bash
# 语义 / 全文混合检索（POST，query 字段名为 q）
curl -X POST http://localhost:20000/api/rag/search \
  -H 'Content-Type: application/json' \
  -H 'X-ClawBench-AI-Token: <token>' \
  -b 'clawbench_project=/path/to/project' \
  -d '{"q":"SSH 隧道保活","limit":20,"exclude_session_id":"abc-123"}'

# 按 ID 取完整消息（含 thinking / tool_use 块）
curl -H 'X-ClawBench-AI-Token: <token>' 'http://localhost:20000/api/rag/message?id=42'

# 取会话全部消息
curl -H 'X-ClawBench-AI-Token: <token>' 'http://localhost:20000/api/rag/session?id=abc-123'
```

`POST /api/rag/search` 支持 `q`、`limit`、`backend`、`role`、`session_id`、`exclude_session_id`、`from` 和 `to` 过滤参数。项目范围通过 `clawbench_project` Cookie 传递。认证走会话 Cookie 或本机 AI 的短时 `X-ClawBench-AI-Token`；回环地址本身不再免密，因此上述示例需带令牌（令牌由内置斜杠命令注入 AI，人工调用请先登录并用会话 Cookie）。

## 删除与维护

删除会话时先做软删除。Cleanup Worker 按 `retention_days` 清理过期会话对应的 RAG 分块、原始响应、聊天消息和会话记录。删除 `<data-dir>/ClawBench.db` 会同时移除聊天与 RAG 数据，操作前应先备份。

排障时检查：

1. `base_url` 是否可访问 `/v1/models` 和 `/v1/embeddings`。
2. `model` 是否存在且返回非空向量。
3. 服务日志中是否出现 `rag:`、FTS 或 sqlite-vec 错误。
4. 嵌入不可用时先验证关键词搜索；这不影响 FTS-only 工作模式。
