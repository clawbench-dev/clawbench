# RAG 历史记忆部署指南

ClawBench 内置 RAG 历史记忆系统。系统持续把聊天消息分块写入主 SQLite 数据库，通过 FTS5 提供全文搜索，并在嵌入服务可用时通过 sqlite-vec 的 `vec0` 虚拟表提供向量搜索。嵌入服务不可用时会自动退化为 FTS-only，RAG 不需要单独启用。

## 在界面中使用

### 设置：会话搜索

配置 RAG 检索的分块、向量模型与索引重建。

![会话搜索设置](screenshots/set-09-rag.png)

- **状态信息**：当前模式、索引进度、FTS 索引大小、向量化进度、向量索引大小、嵌入服务健康状态
- **索引参数**：分块大小、分块重叠、批次大小、保留天数、搜索条数上限
- **向量区**：向量开关、Base URL、模型、API Key、搜索池大小
- **底部重建按钮**：重建 FTS 索引 / 重建向量索引 / 全量重建

> 全量重建耗时较长（需重新分词），会在后台异步执行。

### 搜索抽屉

建好索引后，用自然语言搜索历史会话和项目文档。在设置中启用「会话搜索」后，项目文档会被分块、向量化并建立索引，之后可用自然语言搜索历史会话与文档内容。

会话搜索抽屉支持关键词 / 语义 / 混合三种检索方式，以及归档、排序、时间范围等筛选：

![会话搜索（混合检索）](screenshots/sess-06-search-rag.png)

桌面端筛选项：

| 筛选项 | 可选值 |
|---|---|
| **搜索模式** | 混合 / 全文 |
| **归档状态** | 全部 / 未归档 / 已归档 |
| **排序** | 相关性 / 最新 / 最早 |
| **类型** | 全部 / 对话 / 任务 |
| **时间** | 全部 / 今天 / 近 7 天 / 近 30 天 / 自定义 |

结果项显示标题、摘要、类型标签、标签与时间。点击进入详情页，可继续对话或删除。

移动端：点输入栏的「搜索会话」按钮，从底部弹出搜索抽屉，支持**混合检索**（关键词 + 语义向量），可按会话范围（当前项目 / 外部 / 全部）、匹配方式、时间范围筛选。结果里会高亮命中的片段，点击直接跳到对应消息。

![会话搜索](screenshots-mobile/m-07-session-search.png)

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
