[中文](RAG.md) | [English](RAG.en.md)

# RAG 历史记忆部署指南

ClawBench 内置 RAG（Retrieval-Augmented Generation）历史记忆系统，自动将聊天消息索引到向量数据库，AI 智能体可语义搜索历史对话。

## 系统架构

```
聊天消息 → Indexer → 文本提取 → 分块 → BGE-M3 Embedding → DuckDB 向量库
                                                              ↓
AI 智能体 → RAG Search API → 向量相似度搜索 → 返回相关历史片段
```

**核心组件：**

| 组件 | 说明 | 运行条件 |
|------|------|---------|
| **Indexer** | 后台轮询未索引消息，分块+嵌入+写入 DuckDB | `rag.enabled: true` |
| **Search** | 语义向量搜索，支持多维度过滤 | `rag.enabled: true` |
| **Cleanup Worker** | 定期清理超过保留期的软删除数据 | 始终运行 |

## 前置条件

### 1. 安装 Ollama

```bash
# Linux
curl -fsSL https://ollama.com/install.sh | sh

# macOS
brew install ollama

# 启动服务
ollama serve
```

### 2. 拉取 BGE-M3 模型

```bash
ollama pull bge-m3
```

> BGE-M3 是多语言嵌入模型，输出 1024 维向量，支持中英文语义检索。模型约 2GB。

### 3. 验证 Ollama 状态

```bash
curl http://localhost:11434/api/tags | python3 -m json.tool
```

确认 `bge-m3` 出现在模型列表中即可。

## 配置

在 `config/config.yaml` 中添加 RAG 配置段：

### 最小配置

只需一行即可启用，其余使用默认值：

```yaml
rag:
  enabled: true
```

### 完整配置

```yaml
rag:
  enabled: true                      # 启用 RAG（默认: false）
  ollama_base_url: "http://localhost:11434"  # Ollama API 地址（默认: http://localhost:11434）
  ollama_model: "bge-m3"             # 嵌入模型（默认: bge-m3）
  chunk_size: 512                     # 分块大小，token 数（默认: 512）
  chunk_overlap: 64                   # 分块重叠，token 数（默认: 64）
  poll_interval: "10s"                # 索引器轮询间隔（默认: 10s）
  batch_size: 10                      # 每轮索引消息数（默认: 10）
  search_limit: 5                     # 默认搜索结果数（默认: 5）
  retention_days: 90                  # 软删除数据保留天数，0=永久保留（默认: 90）
```

### 配置项说明

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `enabled` | `false` | RAG 总开关 |
| `ollama_base_url` | `http://localhost:11434` | Ollama 服务地址，远程部署时可指向其他机器 |
| `ollama_model` | `bge-m3` | 嵌入模型名称，需与 `ollama list` 中的一致 |
| `chunk_size` | `512` | 文本分块大小（token），值越大每块包含上下文越多，但检索精度降低 |
| `chunk_overlap` | `64` | 相邻分块重叠（token），防止关键信息被截断 |
| `poll_interval` | `10s` | 索引器检查新消息的频率 |
| `batch_size` | `10` | 每轮处理的未索引消息数 |
| `search_limit` | `5` | 搜索 API 默认返回条数 |
| `retention_days` | `90` | 软删除数据保留天数；设为 `0` 永不自动清理 |

## 数据存储

| 数据 | 位置 | 说明 |
|------|------|------|
| 向量索引 | `<二进制目录>/.clawbench/rag.duckdb` | DuckDB 数据库，含嵌入向量和分块文本 |
| 向量索引（开发模式） | `<二进制目录>/.clawbench/rag-dev.duckdb` | 开发模式独立 DuckDB，避免与正式版冲突 |
| 聊天记录 | `<二进制目录>/.clawbench/ClawBench.db` | SQLite 数据库，`indexed` 列标记索引状态 |

> **卸载清理**：删除 `.clawbench/` 目录即可完全移除所有 RAG 数据。

## 启动流程

启用 RAG 后，服务启动时的初始化顺序：

1. **初始化 DuckDB** — 创建/打开 `rag.duckdb`，检查嵌入维度（若维度不匹配会自动重建表）
2. **初始化嵌入客户端** — 连接 Ollama，验证 `bge-m3` 模型可用
3. **注入 RAG 规则** — AI 智能体的系统提示词（内嵌 `commonRulesTemplate`）中包含 RAG 搜索规则，`@chatsearch` 命令按需注入搜索模板
4. **启动索引器** — 后台轮询，将未索引的历史消息逐步入库
5. **启动清理器** — 始终运行，定期清理超期软删除数据（无论 RAG 是否启用）

> 首次启用时，Indexer 会自动回填所有历史消息（从最新开始），无需手动操作。

## 索引器工作原理

```
每 10s 轮询 → 取 batch_size 条未索引消息 → 逐条处理：
  ├─ 提取纯文本（排除 thinking/tool_use 块）
  ├─ 滑动窗口分块（512 token，64 token 重叠）
  ├─ 逐块调用 Ollama 生成 BGE-M3 嵌入
  ├─ 批量写入 DuckDB
  └─ 标记 SQLite 消息为已索引
```

**文本提取规则：**
- **用户消息**：直接使用原始文本
- **助手消息**：仅提取 `text` 类型的内容块，跳过 `thinking`、`tool_use`、`warning`、`error`

**分块策略：**
- 以段落/句子为边界切分，避免截断语义
- 单条消息最多 50 个分块（超出截断并记录警告）

## 搜索 API

AI 智能体通过 `clawbench rag` CLI 子命令搜索历史对话（推荐），也可直接调用 HTTP API：

### CLI 子命令（推荐）

```bash
# 向量搜索
clawbench rag search --project /path/to/project --query "SSH隧道保活" --limit 5 --exclude-session-id abc-123

# 消息详情
clawbench rag message --project /path/to/project --id 42

# 会话全量
clawbench rag session --project /path/to/project --session-id abc-123

# 查看帮助
clawbench rag --help
clawbench rag search --help
```

**CLI 优势**：自动处理认证（localhost 旁路）、TLS 自签名证书、项目路径注入，无需手动传递 cookie 或 token。

### HTTP API

也可直接调用 HTTP 端点（所有端点需要认证，localhost 自动旁路）：

### 向量搜索

```bash
curl "http://localhost:20000/api/rag/search?q=SSH+隧道保活&limit=5&exclude_session_id=abc-123"
```

**参数：**

| 参数 | 必填 | 说明 |
|------|------|------|
| `q` | ✅ | 搜索文本 |
| `limit` | ❌ | 返回条数（默认: 5） |
| `project` | ❌ | 按项目路径过滤 |
| `backend` | ❌ | 按 AI 后端过滤 |
| `role` | ❌ | 按角色过滤：`user` 或 `assistant` |
| `session_id` | ❌ | 限定到指定会话 |
| `exclude_session_id` | ❌ | 排除指定会话（推荐传入当前会话 ID） |
| `from` / `to` | ❌ | 时间范围过滤 |

### 消息详情

```bash
curl "http://localhost:20000/api/rag/message?id=42"
```

返回完整消息（含 thinking、tool_use 等所有内容块），适合在搜索片段不完整时获取完整上下文。

### 会话全量

```bash
curl "http://localhost:20000/api/rag/session?session_id=abc-123"
```

返回指定会话的所有消息，适合需要完整对话流程的场景。

## 软删除与清理

### 软删除机制

删除会话时不会物理删除数据，而是标记 `deleted=1`：

- **用户界面**：已删除的会话和消息完全不可见
- **RAG 搜索**：仍可检索到已删除的内容，确保历史知识不丢失
- **防写入守卫**：已删除的会话拒绝新消息插入

### 自动清理

Cleanup Worker **始终运行**（无论 RAG 是否启用），定期清理超过 `retention_days` 的软删除数据：

- **清理周期**：首次启动延迟 5 分钟，之后每 24 小时执行一次
- **级联顺序**：DuckDB `chat_chunks` → SQLite `ai_raw_responses` → `chat_history` → `chat_sessions`
- **配置**：`retention_days` 默认 90 天，设为 `0` 永不自动清理

## 远程部署

Ollama 和 ClawBench 可以部署在不同机器上：

```yaml
# ClawBench 服务器上
rag:
  enabled: true
  ollama_base_url: "http://192.168.1.100:11434"  # 指向远程 Ollama
```

**注意事项：**
- 确保网络可达（防火墙放行 11434 端口）
- 嵌入请求延迟会增加，可通过增大 `poll_interval` 适应
- 不建议通过公网暴露 Ollama 端口（无认证机制）

## 故障排查

### Indexer 未工作

1. 检查 Ollama 是否运行：`curl http://localhost:11434/api/tags`
2. 检查 bge-m3 模型是否已拉取：`ollama list | grep bge-m3`
3. 查看日志：`grep "rag" .clawbench/logs/*.log`

### 搜索无结果

1. 确认消息已被索引：检查 SQLite `chat_history.indexed` 列
2. 索引需要时间——历史消息回填是逐批进行的
3. 查看索引进度：`grep "indexed" .clawbench/logs/*.log`

### DuckDB 维度不匹配

如果更换了嵌入模型导致维度不同，启动时会自动检测并重建 DuckDB 表：

```
rag.duckdb: embedding dimension mismatch (expected 1024, got 768), resetting table
```

所有向量数据会丢失，Indexer 会重新回填。

### 磁盘空间

- 每条消息约产生 1-5 个分块（取决于长度）
- 每个分块的嵌入向量约 4KB（1024 × float32）
- 1000 条消息约占 5-25MB DuckDB 空间
