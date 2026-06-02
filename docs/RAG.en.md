[中文](RAG.md) | [English](RAG.en.md)

# RAG History Memory Deployment Guide

ClawBench includes a built-in RAG (Retrieval-Augmented Generation) history memory system that automatically indexes chat messages into a vector database, allowing AI agents to semantically search historical conversations.

## System Architecture

```
Chat Messages → Indexer → Text Extraction → Chunking → BGE-M3 Embedding → DuckDB Vector Store
                                                                          ↓
AI Agent → RAG Search API → Vector Similarity Search → Return Relevant Historical Fragments
```

**Core Components:**

| Component | Description | Runs When |
|-----------|-------------|-----------|
| **Indexer** | Background polling for unindexed messages; chunks + embeds + writes to DuckDB | `rag.enabled: true` |
| **Search** | Semantic vector search with multi-dimensional filtering | `rag.enabled: true` |
| **Cleanup Worker** | Periodically purges soft-deleted data past retention period | Always runs |

## Prerequisites

### 1. Install Ollama

```bash
# Linux
curl -fsSL https://ollama.com/install.sh | sh

# macOS
brew install ollama

# Start the service
ollama serve
```

### 2. Pull the BGE-M3 Model

```bash
ollama pull bge-m3
```

> BGE-M3 is a multilingual embedding model that outputs 1024-dimensional vectors, supporting both Chinese and English semantic retrieval. The model is approximately 2GB.

### 3. Verify Ollama Status

```bash
curl http://localhost:11434/api/tags | python3 -m json.tool
```

Confirm that `bge-m3` appears in the model list.

## Configuration

Add the RAG section to `config/config.yaml`:

### Minimal Configuration

Enable with a single line; all other settings use defaults:

```yaml
rag:
  enabled: true
```

### Full Configuration

```yaml
rag:
  enabled: true                      # Enable RAG (default: false)
  ollama_base_url: "http://localhost:11434"  # Ollama API URL (default: http://localhost:11434)
  ollama_model: "bge-m3"             # Embedding model (default: bge-m3)
  chunk_size: 512                     # Chunk size in tokens (default: 512)
  chunk_overlap: 64                   # Overlap between chunks in tokens (default: 64)
  poll_interval: "10s"                # Indexer poll interval (default: 10s)
  batch_size: 10                      # Messages per indexer batch (default: 10)
  search_limit: 5                     # Default search result limit (default: 5)
  retention_days: 90                  # Soft-deleted data retention days; 0=keep forever (default: 90)
```

### Configuration Reference

| Parameter | Default | Description |
|-----------|---------|-------------|
| `enabled` | `false` | RAG master switch |
| `ollama_base_url` | `http://localhost:11434` | Ollama service URL; point to a remote machine for distributed setups |
| `ollama_model` | `bge-m3` | Embedding model name; must match `ollama list` output |
| `chunk_size` | `512` | Text chunk size in tokens; larger values capture more context but reduce retrieval precision |
| `chunk_overlap` | `64` | Overlap between adjacent chunks in tokens; prevents key information from being split |
| `poll_interval` | `10s` | How often the indexer checks for new messages |
| `batch_size` | `10` | Number of unindexed messages processed per poll cycle |
| `search_limit` | `5` | Default number of results returned by search API |
| `retention_days` | `90` | Days to retain soft-deleted data; set to `0` to disable automatic cleanup |

## Data Storage

| Data | Location | Description |
|------|----------|-------------|
| Vector index | `<binary_dir>/.clawbench/rag.duckdb` | DuckDB database with embeddings and chunked text |
| Vector index (dev mode) | `<binary_dir>/.clawbench/rag-dev.duckdb` | Separate DuckDB for dev mode, avoids conflict with production |
| Chat records | `<binary_dir>/.clawbench/ClawBench.db` | SQLite database; `indexed` column tracks indexing status |

> **Clean uninstall**: Delete the `.clawbench/` directory to remove all RAG data completely.

## Startup Sequence

When RAG is enabled, the initialization order at startup:

1. **Initialize DuckDB** — Create/open `rag.duckdb`; check embedding dimension (auto-rebuilds table if dimension mismatch)
2. **Initialize embedding client** — Connect to Ollama, verify `bge-m3` model availability
3. **Inject RAG rules** — RAG search rules injected via `@chatsearch` command on demand (no longer in static `rules.md`)
4. **Start indexer** — Background polling, gradually indexing historical messages
5. **Start cleanup worker** — Always runs, periodically purges expired soft-deleted data (regardless of RAG enablement)

> On first enable, the Indexer automatically backfills all historical messages (newest first) — no manual action required.

## Indexer Workflow

```
Poll every 10s → Fetch batch_size unindexed messages → Process each:
  ├─ Extract plain text (exclude thinking/tool_use blocks)
  ├─ Sliding window chunking (512 tokens, 64 token overlap)
  ├─ Call Ollama per chunk for BGE-M3 embedding
  ├─ Bulk insert into DuckDB
  └─ Mark SQLite message as indexed
```

**Text Extraction Rules:**
- **User messages**: Use raw text directly
- **Assistant messages**: Extract only `text` content blocks; skip `thinking`, `tool_use`, `warning`, `error`

**Chunking Strategy:**
- Split at paragraph/sentence boundaries to avoid truncating semantics
- Maximum 50 chunks per message (excess truncated with warning)

## Search API

AI agents search historical conversations via `clawbench rag` CLI subcommands (recommended), or by calling the HTTP API directly:

### CLI Subcommands (Recommended)

```bash
# Vector search
clawbench rag search --project /path/to/project --query "SSH tunnel keepalive" --limit 5 --exclude-session-id abc-123

# Message detail
clawbench rag message --project /path/to/project --id 42

# Full session
clawbench rag session --project /path/to/project --session-id abc-123

# View help
clawbench rag --help
clawbench rag search --help
```

**CLI advantages**: Automatically handles authentication (localhost bypass), TLS self-signed certificates, and project path injection — no need to manually pass cookies or tokens.

### HTTP API

You can also call the HTTP endpoints directly (all endpoints require authentication; localhost auto-bypasses):

### Vector Search

```bash
curl "http://localhost:20000/api/rag/search?q=SSH+tunnel+keepalive&limit=5&exclude_session_id=abc-123"
```

**Parameters:**

| Parameter | Required | Description |
|-----------|----------|-------------|
| `q` | ✅ | Search text |
| `limit` | ❌ | Number of results (default: 5) |
| `project` | ❌ | Filter by project path |
| `backend` | ❌ | Filter by AI backend |
| `role` | ❌ | Filter by role: `user` or `assistant` |
| `session_id` | ❌ | Limit to specific session |
| `exclude_session_id` | ❌ | Exclude specific session (recommended: pass current session ID) |
| `from` / `to` | ❌ | Time range filter |

### Message Detail

```bash
curl "http://localhost:20000/api/rag/message?id=42"
```

Returns the complete message (including thinking, tool_use, and all content blocks) — useful when a search fragment is incomplete.

### Full Session

```bash
curl "http://localhost:20000/api/rag/session?session_id=abc-123"
```

Returns all messages in a session — useful when you need the complete conversation flow.

## Soft-Delete & Cleanup

### Soft-Delete Mechanism

Deleting a session does not physically remove data; instead, it sets `deleted=1`:

- **User interface**: Deleted sessions and messages are completely invisible
- **RAG search**: Deleted content remains searchable, ensuring historical knowledge is preserved
- **Write guard**: Deleted sessions reject new message inserts

### Automatic Cleanup

The Cleanup Worker **always runs** (regardless of RAG enablement), periodically purging soft-deleted data older than `retention_days`:

- **Cleanup cycle**: 5-minute delay on first start, then every 24 hours
- **Cascade order**: DuckDB `chat_chunks` → SQLite `ai_raw_responses` → `chat_history` → `chat_sessions`
- **Configuration**: `retention_days` defaults to 90 days; set to `0` to disable automatic cleanup

## Remote Deployment

Ollama and ClawBench can run on different machines:

```yaml
# On ClawBench server
rag:
  enabled: true
  ollama_base_url: "http://192.168.1.100:11434"  # Point to remote Ollama
```

**Notes:**
- Ensure network connectivity (firewall allows port 11434)
- Embedding request latency will increase; consider increasing `poll_interval`
- Do not expose Ollama port on public networks (no authentication mechanism)

## Troubleshooting

### Indexer Not Working

1. Check if Ollama is running: `curl http://localhost:11434/api/tags`
2. Check if bge-m3 model is pulled: `ollama list | grep bge-m3`
3. Check logs: `grep "rag" .clawbench/logs/*.log`

### No Search Results

1. Confirm messages have been indexed: check SQLite `chat_history.indexed` column
2. Indexing takes time — historical message backfill is processed in batches
3. Check indexing progress: `grep "indexed" .clawbench/logs/*.log`

### DuckDB Dimension Mismatch

If you change the embedding model and the dimension differs, DuckDB will auto-detect and rebuild the table on startup:

```
rag.duckdb: embedding dimension mismatch (expected 1024, got 768), resetting table
```

All vector data will be lost; the Indexer will re-backfill from scratch.

### Disk Space

- Each message produces approximately 1–5 chunks (depending on length)
- Each chunk's embedding vector is approximately 4KB (1024 × float32)
- 1,000 messages consume approximately 5–25MB of DuckDB space
