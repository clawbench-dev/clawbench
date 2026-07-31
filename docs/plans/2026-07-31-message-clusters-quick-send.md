# User Message Clustering & Quick Send Recommendation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract all user messages from chat history (including archived sessions), cluster them by semantic similarity, count frequencies, and present top clusters as one-click quick-send recommendations.

**Architecture:** User-triggered on-demand computation with progress reporting. When user opens the recommendations drawer, cached results (if any) are shown immediately. A "Re-analyze" button lets the user trigger re-computation, with real-time progress displayed (phase, counts, elapsed). Results are cached in a SQLite table for instant loading next time. No nightly cron — purely manual trigger. Three-tier clustering: exact dedup → FTS token Sørensen-Dice similarity → vector cosine similarity (when embedding API available).

**Tech Stack:** Go backend (SQLite cache + meta progress table + RAG vec0/FTS5 + gse segmentation + goroutine computation + WebSocket progress via StreamHub), Vue 3 frontend (BottomSheet drawer + useCrudList pattern + progress bar).

---

## Core Design Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| Computation model | **User-triggered on-demand** with progress reporting via WebSocket | No nightly cron. User clicks "Re-analyze" → backend runs in goroutine → pushes progress events via StreamHub → frontend shows progress bar. Cached results shown on subsequent visits. |
| Cache table | `message_clusters_cache` + `message_clusters_meta` in ClawBench.db | Same SQLite file, WAL mode. Frontend reads cache — instant. Meta table tracks progress status. |
| Clustering algorithm | Two-tier: exact dedup → similarity grouping (Union-Find) | Exact dedup via SQL GROUP BY handles most clustering. Similarity handles "你好"/"你好啊" type near-matches. |
| **Similarity metric (C1 fix)** | **Sørensen-Dice** `2|intersection| / (|A| + |B|)` + **length-ratio penalty** | Overlap ratio was fundamentally flawed. Sørensen-Dice gives 2×1/(1+2)=0.67 for "你好"/"你好啊". Length penalty (minRatio=0.5) prevents "好的" clustering with long messages. |
| **Vector similarity threshold** | cosine ≥ 0.85 | Reasonable for short messages. |
| **FTS similarity threshold** | 0.65 | Sørensen-Dice at 0.65 captures "你好"/"你好啊" (0.67) but rejects "你好"/"你好再见" (0.4). |
| Message filtering (I2 fix) | Exclude: long (>200 chars), slash commands (starts with `/` or `@`), file-attached | Quick-send is for short reusable messages. |
| API design | Three endpoints: `GET` cache, `POST /compute` trigger, `GET /compute/status` progress | GET cache = instant. POST compute = starts goroutine, returns 202. GET status = polling progress. When done, frontend re-fetches cache. |
| Progress reporting | WebSocket `cluster_progress` events via StreamHub + meta table polling | Push events for real-time progress. Meta table for initial GET and fallback. |
| Mode reporting (I5 fix) | ComputeOnce writes actual mode into meta row | Returns real mode, not guessed. |
| Duplicate quick-send (I6 fix) | **Backend filters out clusters matching existing quick-send commands** when returning cached results | Handler reads `chat_quick_send` table commands, removes any cluster whose representative OR any variant matches. User never sees items they already have in quick-send. |
| SegmentTokens (I3 fix) | New `SegmentTokens() []string` returns raw token slice | Avoids rune-iteration bug from `SegmentText`. |

---

## Task 1: Backend — SQL query to extract distinct user messages with counts

**Files:**
- Modify: `internal/service/database.go`
- Test: `internal/service/database_test.go`

**Step 1-5: TDD cycle** (as before — GetUserMessageStats with filters: >200 chars, slash/@ commands, file-attached, streaming, empty)

```go
type UserMessageStat struct {
    Text  string `json:"text"`
    Count int    `json:"count"`
}

func GetUserMessageStats(limit int) ([]UserMessageStat, error) {
    if limit <= 0 { limit = 5000 }
    rows, err := dbRead.Query(
        `SELECT content, COUNT(*) as cnt
         FROM chat_history
         WHERE role = 'user' AND streaming = 0 AND content != ''
           AND LENGTH(content) <= 200
           AND (files IS NULL OR files = '')
           AND NOT (content LIKE '/%' OR content LIKE '@%')
         GROUP BY content ORDER BY cnt DESC LIMIT ?`, limit)
    // ... scan and return
}
```

---

## Task 2: Backend — Cluster cache + meta tables in SQLite

**Files:**
- Modify: `internal/service/database.go` (schema + CRUD)

```sql
CREATE TABLE IF NOT EXISTS message_clusters_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    representative TEXT NOT NULL,
    variants TEXT NOT NULL,
    total_count INTEGER NOT NULL,
    representative_count INTEGER NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS message_clusters_meta (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    mode TEXT NOT NULL DEFAULT '',
    progress TEXT NOT NULL DEFAULT 'idle',
    phase TEXT NOT NULL DEFAULT '',
    msg_count INTEGER NOT NULL DEFAULT 0,
    cluster_count INTEGER NOT NULL DEFAULT 0,
    elapsed_ms INTEGER NOT NULL DEFAULT 0,
    error_msg TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Functions: `SaveClusterCache`, `GetClusterCache`, `SaveClusterMeta`, `SaveClusterMetaError`, `GetClusterMeta`, `GetQuickSendCommands`

`GetQuickSendCommands()` returns all existing quick-send command strings for filtering:

```go
// GetQuickSendCommands returns all command values from chat_quick_send.
// Used by the message-clusters handler to filter out clusters that
// already exist in quick-send.
func GetQuickSendCommands() []string {
    rows, err := dbRead.Query("SELECT command FROM chat_quick_send")
    if err != nil { return nil }
    defer rows.Close()
    var commands []string
    for rows.Next() {
        var cmd string
        if rows.Scan(&cmd) == nil {
            commands = append(commands, cmd)
        }
    }
    return commands
}
```

---

## Task 3: Backend — Semantic clustering algorithm (Union-Find + Sørensen-Dice)

**Files:**
- Create: `internal/rag/cluster.go`
- Test: `internal/rag/cluster_test.go`

`MessageCluster` struct, `ClusterMessages(stats, simFn, threshold)` with Union-Find + union-by-rank, `sorensenDiceWithLengthPenalty(minLengthRatio=0.5)` returning Sørensen-Dice with length penalty. `MessageStat` defined in `rag` package (not `service`) to avoid cross-package coupling (I7 fix).

---

## Task 4: Backend — SegmentTokens function (I3 fix)

**Files:**
- Modify: `internal/rag/segment.go`
- Test: `internal/rag/segment_test.go`

`SegmentTokens(text string) []string` — returns raw token slice from gse, not joined string.

---

## Task 5: Backend — VectorSimilarityMatrix with nil-slice fix (C3 fix)

**Files:**
- Modify: `internal/rag/cluster.go`
- Test: `internal/rag/cluster_test.go`

`VectorSimilarityMatrix(ctx, embedder, texts)` — batch embed in sub-batches of 5, allocate inner slices with `make([]float64, len(emb))`, return cosine similarity lookup function.

---

## Task 6: Backend — ClusterMessagesWithEmbeddings returning mode (I5 fix)

**Files:**
- Modify: `internal/rag/cluster.go`
- Test: `internal/rag/cluster_test.go`

Returns `([]MessageCluster, string)` — mode is "vector" | "fts" | "exact", reflecting what was actually used.

---

## Task 7: Backend — ClusterWorker (user-triggered goroutine with progress)

**Files:**
- Create: `internal/rag/cluster_worker.go`
- Test: `internal/rag/cluster_worker_test.go`
- Modify: `internal/rag/rag.go` (StartClusterWorker takes StreamHub)
- Modify: `cmd/server/main.go` (wire up)

No cron. `ClusterWorker` runs computation in a goroutine when `ComputeOnce()` is called. Updates `message_clusters_meta` progress at each phase. Broadcasts `cluster_progress` WebSocket events via StreamHub.

```go
type ClusterProgress struct {
    Status       string `json:"status"`       // "idle" | "computing" | "done" | "error"
    Phase        string `json:"phase"`        // "extracting" | "clustering" | "saving"
    MsgCount     int    `json:"msg_count"`
    ClusterCount int    `json:"cluster_count"`
    ElapsedMs    int64  `json:"elapsed_ms"`
    Mode         string `json:"mode"`         // available only when done
    Error        string `json:"error,omitempty"`
}

type ClusterWorker struct {
    mu       sync.Mutex
    running  bool
    cancelFn context.CancelFunc
    hub      *ws.StreamHub
}

func (cw *ClusterWorker) ComputeOnce()       // starts goroutine, returns immediately
func (cw *ClusterWorker) IsRunning() bool    // prevents duplicate triggers
func (cw *ClusterWorker) GetProgress() ClusterProgress  // reads from meta table
```

Three phases:
1. **extracting**: `GetUserMessageStats(5000)` → update meta + broadcast
2. **clustering**: `ClusterMessagesWithEmbeddings()` → update meta + broadcast
3. **saving**: `SaveClusterCache()` → update meta + broadcast → final "done" broadcast

Wire up: `rag.StartClusterWorker(ws.GlobalHub)` in main.go.

---

## Task 8: Backend — API handlers (cache + compute + progress)

**Files:**
- Create: `internal/handler/message_clusters.go`
- Test: `internal/handler/message_clusters_test.go`
- Modify: `internal/handler/handler.go`

Three endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/chat/message-clusters` | GET | Read cached results + current progress status (instant) |
| `/api/chat/message-clusters/compute` | POST | Trigger re-computation (202 Accepted, goroutine) |
| `/api/chat/message-clusters/compute/status` | GET | Read current progress from meta table (for polling) |

```go
// GET /api/chat/message-clusters
func ServeMessageClusters(w, r) {
    entries, mode, updatedAt := service.GetClusterCache()
    progress := rag.GlobalClusterWorker.GetProgress()

    // Filter out clusters that match existing quick-send commands
    quickSendCommands := service.GetQuickSendCommands() // returns []string of all command values
    quickSendSet := make(map[string]bool, len(quickSendCommands))
    for _, cmd := range quickSendCommands {
        quickSendSet[cmd] = true
    }

    filteredEntries := make([]ClusterItem, 0)
    for _, e := range entries {
        variants := strings.Split(e.Variants, ",")
        // Skip this cluster if ANY variant matches an existing quick-send command
        allMatched := true // only skip if ALL variants are in quick-send
        hasUnmatched := false
        for _, v := range variants {
            if !quickSendSet[v] {
                hasUnmatched = true
                break
            }
        }
        if !hasUnmatched {
            continue // all variants already in quick-send, skip entire cluster
        }
        // Filter variants: remove ones already in quick-send from the variants display
        filteredVariants := make([]string, 0)
        for _, v := range variants {
            if !quickSendSet[v] {
                filteredVariants = append(filteredVariants, v)
            }
        }
        filteredEntries = append(filteredEntries, ClusterItem{
            ID:                  e.ID,
            Representative:      e.Representative,
            Variants:            filteredVariants,
            TotalCount:          e.TotalCount,
            RepresentativeCount: e.RepresentativeCount,
        })
    }

    // Response includes: filtered clusters, total, mode, progress status, updated_at
}

// POST /api/chat/message-clusters/compute
func ServeMessageClustersCompute(w, r) {
    if rag.GlobalClusterWorker.IsRunning() {
        return 409 Conflict  // already computing
    }
    rag.GlobalClusterWorker.ComputeOnce()  // starts goroutine
    return 202 Accepted
}

// GET /api/chat/message-clusters/compute/status
func ServeMessageClustersComputeStatus(w, r) {
    progress := rag.GlobalClusterWorker.GetProgress()
    return progress
}
```

Routes:
```go
register("/api/chat/message-clusters", middleware.Auth(ServeMessageClusters))
register("/api/chat/message-clusters/compute", middleware.Auth(ServeMessageClustersCompute))
register("/api/chat/message-clusters/compute/status", middleware.Auth(ServeMessageClustersComputeStatus))
```

---

## Task 9: Frontend — useMessageClusters composable

**Files:**
- Create: `web/src/composables/useMessageClusters.ts`
- Test: `web/src/composables/__tests__/useMessageClusters.test.ts`

```typescript
import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

export interface MessageCluster {
  id: number
  representative: string
  variants: string[]
  total_count: number
  representative_count: number
}

export interface ClusterProgress {
  status: string       // "idle" | "computing" | "done" | "error"
  phase: string        // "extracting" | "clustering" | "saving"
  msg_count: number
  cluster_count: number
  elapsed_ms: number
  mode: string
  error?: string
}

interface MessageClustersResponse {
  clusters: MessageCluster[]
  total: number
  mode: string
  progress: string      // progress status from GET cache endpoint
  updated_at: string
}

export function useMessageClusters() {
  const clusters = ref<MessageCluster[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const computing = ref(false)
  const progress = ref<ClusterProgress>({ status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '' })
  const mode = ref<string>('')
  const updatedAt = ref<string>('')

  // Read cached results (instant). Response includes progress status.
  async function fetchClusters() {
    loading.value = true
    try {
      const resp = await fetch('/api/chat/message-clusters')
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
      const data: MessageClustersResponse = await resp.json()
      clusters.value = data.clusters
      mode.value = data.mode
      updatedAt.value = data.updated_at
      progress.value.status = data.progress  // may be "idle", "computing", "done"
      loaded.value = true
    } catch (e) {
      appLog.e('MsgCluster', `Failed to fetch clusters: ${e}`)
    } finally {
      loading.value = false
    }
  }

  // Trigger on-demand computation. Returns 202, computation runs in goroutine.
  async function startCompute() {
    try {
      const resp = await fetch('/api/chat/message-clusters/compute', { method: 'POST' })
      if (resp.status === 409) {
        appLog.i('MsgCluster', 'Computation already running')
        return  // already computing
      }
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
      computing.value = true
      progress.value.status = 'computing'
      // Start polling progress
      pollProgress()
    } catch (e) {
      appLog.e('MsgCluster', `Failed to start computation: ${e}`)
    }
  }

  // Poll progress until done, then re-fetch cached results.
  let pollTimer: number | null = null
  function pollProgress() {
    pollTimer = window.setInterval(async () => {
      try {
        const resp = await fetch('/api/chat/message-clusters/compute/status')
        if (!resp.ok) return
        const data: ClusterProgress = await resp.json()
        progress.value = data

        if (data.status === 'done' || data.status === 'error') {
          stopPolling()
          computing.value = false
          if (data.status === 'done') {
            // Re-fetch cached results
            await fetchClusters()
          }
        }
      } catch (e) {
        appLog.e('MsgCluster', `Progress poll error: ${e}`)
      }
    }, 2000)  // poll every 2 seconds
  }

  function stopPolling() {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  // Also listen for WebSocket cluster_progress events for real-time updates
  // (supplements polling — gives instant phase transitions)

  return { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute, stopPolling }
}
```

---

## Task 10: Frontend — MessageClustersDrawer component

**Files:**
- Create: `web/src/components/chat/MessageClustersDrawer.vue`
- Test: `web/src/components/chat/__tests__/MessageClustersDrawer.test.ts`

```vue
<template>
  <BottomSheet v-model="visible" :title="t('chat.messageClusters.title')">
    <!-- Computation in progress — show progress bar -->
    <div v-if="computing || progress.status === 'computing'" class="clusters-progress">
      <div class="progress-bar">
        <div class="progress-fill" :style="{ width: progressPercent + '%' }"></div>
      </div>
      <p class="progress-text">
        {{ t('chat.messageClusters.phase_' + progress.phase, {
            msgCount: progress.msg_count,
            elapsed: formatElapsed(progress.elapsed_ms)
          }) }}
      </p>
    </div>
    <!-- Error state -->
    <div v-else-if="progress.status === 'error'" class="clusters-error">
      <p>{{ t('chat.messageClusters.error') }}</p>
      <p class="error-detail">{{ progress.error }}</p>
      <button @click="startCompute">{{ t('chat.messageClusters.retry') }}</button>
    </div>
    <!-- Loading cached data -->
    <div v-else-if="loading" class="clusters-loading">{{ t('chat.messageClusters.loading') }}</div>
    <!-- No cache yet (idle, never computed) -->
    <div v-else-if="clusters.length === 0 && progress.status === 'idle'" class="clusters-empty">
      <p>{{ t('chat.messageClusters.noCache') }}</p>
      <button @click="startCompute">{{ t('chat.messageClusters.firstAnalyze') }}</button>
    </div>
    <!-- Has cached results -->
    <div v-else-if="clusters.length > 0" class="clusters-list">
      <div class="clusters-header">
        <span class="cache-status">{{ t('chat.messageClusters.cacheStatus', { mode, updatedAt }) }}</span>
        <button class="reanalyze-btn" @click="startCompute" :disabled="computing">
          {{ t('chat.messageClusters.reanalyze') }}
        </button>
      </div>
      <div
        v-for="cluster in clusters"
        :key="cluster.id"
        class="cluster-item"
      >
        <div class="cluster-main">
          <span class="cluster-text">{{ cluster.representative }}</span>
          <span class="cluster-count">{{ cluster.total_count }}</span>
        </div>
        <div v-if="cluster.variants.length > 1" class="cluster-variants">
          <span v-for="v in cluster.variants.filter(v => v !== cluster.representative)" :key="v" class="variant-tag">
            {{ v }}
          </span>
        </div>
        <button
          class="add-quick-send-btn"
          @click="addToQuickSend(cluster)"
        >
          {{ t('chat.messageClusters.addQuickSend') }}
        </button>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { useMessageClusters, type MessageCluster } from '@/composables/useMessageClusters'
import { useQuickSend } from '@/composables/useQuickSend'
import { appLog } from '@/utils/appLog'

const { t } = useI18n()
const visible = ref(false)
const { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute, stopPolling } = useMessageClusters()
const { addItem } = useQuickSend()

// Note: No isAlreadyQuickSend check needed here — backend already filters
// out clusters whose variants all match existing quick-send commands.

// Progress percent: estimating based on phase
const progressPercent = computed(() => {
  if (progress.value.phase === 'extracting') return 20
  if (progress.value.phase === 'clustering') return 50
  if (progress.value.phase === 'saving') return 90
  if (progress.value.status === 'done') return 100
  return 10
})

function formatElapsed(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function isAlreadyQuickSend(text: string): boolean {
  return items.value.some(item => item.command === text)
}

async function addToQuickSend(cluster: MessageCluster) {
  try {
    await addItem({ label: cluster.representative, command: cluster.representative })
    appLog.i('MsgCluster', `Added "${cluster.representative}" to quick send`)
    // After adding, re-fetch clusters — backend will now filter this one out
    await fetchClusters()
  } catch (e) {
    appLog.e('MsgCluster', `Failed to add: ${e}`)
  }
}

async function open() {
  visible.value = true
  await fetchClusters()  // always read cache first — instant
}

function onClose() {
  stopPolling()  // stop polling when drawer closes
}

defineExpose({ open })
</script>
```

---

## Task 11: Frontend — WebSocket listener for cluster_progress events

**Files:**
- Modify: `web/src/composables/useMessageClusters.ts`

In addition to HTTP polling, listen for `cluster_progress` WebSocket events via the existing StreamHub connection. This gives real-time phase transitions (instant, no 2s polling delay).

```typescript
// In useMessageClusters, add WebSocket listener:
import { useWebSocket } from '@/composables/useWebSocket' // or however WS is accessed

// Listen for cluster_progress events
watch(wsEvents, (event) => {
  if (event.type === 'cluster_progress') {
    progress.value = event.data
    if (event.data.status === 'done') {
      computing.value = false
      fetchClusters()
    }
    if (event.data.status === 'error') {
      computing.value = false
    }
  }
})
```

---

## Task 12: Frontend — Integrate into QuickSendDrawer

**Files:**
- Modify: `web/src/components/chat/QuickSendDrawer.vue`

Add Lucide `Sparkles` icon button in drawer header → opens MessageClustersDrawer.

---

## Task 13: Frontend — i18n strings

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

```typescript
// en.ts
messageClusters: {
  title: 'Message Recommendations',
  loading: 'Loading...',
  computing: 'Analyzing messages...',
  noCache: 'No analysis results yet.',
  firstAnalyze: 'Start Analysis',
  reanalyze: 'Re-analyze',
  addQuickSend: 'Add to Quick Send',
  cacheStatus: 'Mode: {mode} | Updated: {updatedAt}',
  error: 'Analysis failed',
  retry: 'Retry',
  phase_extracting: 'Extracting messages ({msgCount} found, {elapsed})',
  phase_clustering: 'Clustering messages ({elapsed})',
  phase_saving: 'Saving results ({elapsed})',
  recommendations: 'Recommendations',
}

// zh.ts
messageClusters: {
  title: '消息推荐',
  loading: '正在加载...',
  computing: '正在分析消息...',
  noCache: '尚未分析过消息。',
  firstAnalyze: '开始分析',
  reanalyze: '重新分析',
  addQuickSend: '添加到快捷发送',
  cacheStatus: '模式: {mode} | 更新: {updatedAt}',
  error: '分析失败',
  retry: '重试',
  phase_extracting: '提取消息 (已找到 {msgCount} 条, {elapsed})',
  phase_clustering: '聚类分析中 ({elapsed})',
  phase_saving: '保存结果 ({elapsed})',
  recommendations: '推荐',
}
```

---

## Data Flow Summary

```
[User opens recommendations drawer]
MessageClustersDrawer.open()
  → fetchClusters() — GET /api/chat/message-clusters
    → If cache exists: instant response with clusters + mode + progress="done"
    → If no cache: response with empty clusters + progress="idle"

[User clicks "Start Analysis" or "Re-analyze"]
  → startCompute() — POST /api/chat/message-clusters/compute
    → Returns 202 Accepted
    → Backend: ClusterWorker.ComputeOnce() starts goroutine
    → Progress phases via WebSocket + polling:
      Phase "extracting": GetUserMessageStats(5000) → broadcast progress
      Phase "clustering": ClusterMessagesWithEmbeddings() → broadcast progress
      Phase "saving": SaveClusterCache() → broadcast progress
      Final "done": broadcast → frontend re-fetches cache → renders results

[Subsequent visits]
  → fetchClusters() — instant, reads SQLite cache table

[No nightly cron — purely manual trigger]
```

## Review Fixes Applied

| Review Issue | Fix | Task |
|-------------|-----|------|
| C1: overlap ratio flawed | Sørensen-Dice + length penalty (minRatio=0.5), threshold=0.65 | Task 3 |
| C2: HTTP blocking | Goroutine + 202 Accepted + progress polling | Task 7, 8 |
| C3: nil slice panic | `normalized[i] = make([]float64, len(emb))` | Task 5 |
| I1: SegmentText slow O(n²) | Pre-segment via SegmentTokens | Task 4 |
| I2: No message filtering | Exclude >200 chars, slash/@ commands, file-attached | Task 1 |
| I3: SegmentText returns string | New SegmentTokens() returns []string | Task 4 |
| I4: Cross-project scope | Intentional, documented | Task 1 |
| I5: Wrong mode reporting | ClusterMessagesWithEmbeddings returns mode | Task 6 |
| I6: Duplicate quick-send | Backend filters clusters matching existing quick-send commands; frontend re-fetches after adding | Task 8, 10 |
| I7: rag→service coupling | MessageStat defined in rag | Task 6 |
| M4: No appLog | Added appLog.e | Task 9 |
| M5: onMounted auto-fetch | Removed, only fetch on open() | Task 10 |
| M6: Union-Find rank | Added union-by-rank | Task 3 |
| M7: Emoji | Use Lucide Sparkles icon | Task 12 |
