[中文](RAG.md) | [English](RAG.en.md)

# RAG History Memory Deployment Guide

ClawBench continuously chunks chat messages into the main SQLite database. FTS5 provides full-text search, while sqlite-vec's `vec0` virtual table provides vector search whenever an embedding service is available. If embeddings are unavailable, the system automatically falls back to FTS-only operation; RAG does not require a separate enable switch.

## Architecture

```text
Chat → Indexer → text extraction → chunks → SQLite chat_chunks + FTS5
                                          └→ OpenAI-compatible embeddings → vec0

AI agents → RAG API (HTTP) → RRF hybrid search → history snippets
```

All RAG data lives in `<data-dir>/ClawBench.db`. ClawBench does not create `rag.duckdb` or a separate vector database.

## Embedding Service

The defaults target Ollama:

```bash
ollama serve
ollama pull bge-m3
curl http://localhost:11434/api/tags
```

Any OpenAI-compatible service exposing `/v1/models` and `/v1/embeddings` can be used. Without one, chunks are still indexed in FTS5. Missing vectors are backfilled after the embedding service recovers.

## Configuration

`config/config.yaml` is optional. The effective defaults are:

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

| Key | Default | Purpose |
|-----|---------|---------|
| `base_url` | `http://localhost:11434` | OpenAI-compatible API base URL |
| `model` | `bge-m3` | Embedding model |
| `api_key` | empty | Optional cloud-service credential |
| `chunk_size` | `512` | Tokens per chunk |
| `chunk_overlap` | `64` | Overlap between adjacent chunks |
| `poll_interval` | `5s` | Indexer polling interval |
| `batch_size` | `50` | Messages processed per batch (hot-reloadable) |
| `search_limit` | `100` | Default result count |
| `search_pool_size` | `20` | Candidates per source before RRF fusion |
| `retention_days` | `90` | Soft-deleted data retention; `0` keeps data forever |

`ollama_base_url` and `ollama_model` remain compatibility aliases. New configurations should use `base_url` and `model`.

## Indexing and Search

The Indexer reads unindexed messages, extracts user text and assistant `text` blocks, skips thinking/tool_use/warning/error blocks, and chunks text with a sliding window. A message produces at most 50 chunks.

Search fuses FTS5 and vector candidates. When embeddings are unavailable, only full-text search runs. Changing to a model with a different vector dimension rebuilds the vector index but preserves SQLite text chunks for automatic backfill.

Call the HTTP API directly:

```bash
# Semantic / full-text hybrid search (POST; the query field is named q)
curl -X POST http://localhost:20000/api/rag/search \
  -H 'Content-Type: application/json' \
  -H 'X-ClawBench-AI-Token: <token>' \
  -b 'clawbench_project=/path/to/project' \
  -d '{"q":"SSH tunnel keepalive","limit":20,"exclude_session_id":"abc-123"}'

# Fetch a full message by ID (includes thinking / tool_use blocks)
curl -H 'X-ClawBench-AI-Token: <token>' 'http://localhost:20000/api/rag/message?id=42'

# Fetch every message in a session
curl -H 'X-ClawBench-AI-Token: <token>' 'http://localhost:20000/api/rag/session?id=abc-123'
```

`POST /api/rag/search` accepts `q`, `limit`, `backend`, `role`, `session_id`, `exclude_session_id`, `from`, and `to`. Project scope travels in the `clawbench_project` cookie. Authentication uses the session cookie or a short-lived `X-ClawBench-AI-Token` from a local AI subprocess; a loopback address alone no longer bypasses auth, so the examples above need a token (the built-in slash commands inject one for the AI — call manually after logging in and use the session cookie).

## Deletion and Maintenance

Session deletion is initially soft. The Cleanup Worker purges expired RAG chunks, raw responses, chat messages, and sessions according to `retention_days`. Removing `<data-dir>/ClawBench.db` removes both chat and RAG data, so back it up first.

For troubleshooting, verify the embedding service's `/v1/models` and `/v1/embeddings` endpoints, confirm the configured model returns non-empty vectors, inspect logs for `rag:`, FTS, or sqlite-vec errors, and test keyword search when embeddings are offline.
