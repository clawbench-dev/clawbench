import { ref } from 'vue'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

/**
 * Which layer a rebuild discards. The three kinds differ in how much work the
 * server has to redo:
 *
 * - fts:    re-segments existing chunks (chunking and embeddings untouched)
 * - vector: re-embeds existing chunks (chunking and segmentation untouched)
 * - full:   deletes all chunks and rebuilds from the source messages, so it is
 *           the only kind that re-chunks (required after changing chunk_size)
 */
export type RebuildKind = 'fts' | 'vector' | 'full'

/** Progress snapshot returned by GET /api/rag/rebuild/status. */
export interface RebuildStatus {
  kind: '' | RebuildKind
  status: 'idle' | 'running' | 'done' | 'error' | 'cancelled' | 'blocked'
  phase: '' | 'resegmenting' | 'embedding' | 'indexing'
  total: number
  processed: number
  progress_pct: number
  elapsed_ms: number
  error?: string
}

const POLL_INTERVAL_MS = 1000
// A full re-segmentation measured ~149s on a 44k-chunk store, and a full rebuild
// additionally re-chunks and re-embeds every message, so the poller must be
// willing to wait many minutes. This is only a runaway guard.
const POLL_TIMEOUT_MS = 60 * 60 * 1000

/** Latest observed status, shared so the UI can render live progress. */
export const rebuildStatus = ref<RebuildStatus>({
  kind: '', status: 'idle', phase: '', total: 0, processed: 0,
  progress_pct: 0, elapsed_ms: 0,
})

/** Human-readable progress label for the running rebuild, or '' when idle. */
export function rebuildProgressLabel(): string {
  const s = rebuildStatus.value
  if (s.status !== 'running') return ''
  if (s.total <= 0) return ''
  return `${s.processed}/${s.total}`
}

/**
 * Poll the rebuild status until it reaches a terminal state.
 *
 * The server runs the rebuild in the background and answers the trigger with 202;
 * polling is how the UI learns the outcome and shows progress. Previously the
 * request was held open for the whole rebuild, which exceeded the 10s API timeout
 * and made a successful rebuild appear as a failure.
 *
 * Returns the terminal status, or null if polling stopped early (unmounted or the
 * runaway guard fired).
 */
export async function pollRebuildStatus(
  isCancelled: () => boolean = () => false,
): Promise<RebuildStatus | null> {
  const deadline = Date.now() + POLL_TIMEOUT_MS
  while (Date.now() < deadline) {
    if (isCancelled()) return null
    try {
      const status = await apiGet<RebuildStatus>('/api/rag/rebuild/status')
      rebuildStatus.value = status
      if (status.status !== 'running') return status
    } catch (err) {
      // A transient poll failure must not abort the whole operation: the rebuild
      // keeps running server-side, so keep polling.
      appLog.w('RagRebuild', 'status poll failed, retrying', err)
    }
    await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS))
  }
  appLog.w('RagRebuild', 'status polling timed out')
  return null
}

/**
 * Trigger a rebuild of the given kind and wait for its terminal status.
 *
 * Throws on a rejected trigger (409 already running, 503 segmenter unavailable)
 * so callers can surface the server's localized explanation.
 */
export async function startRebuild(
  kind: RebuildKind,
  isCancelled: () => boolean = () => false,
): Promise<RebuildStatus | null> {
  await apiPost('/api/rag/rebuild', { kind })
  return pollRebuildStatus(isCancelled)
}

/** Reset shared progress state — for tests only. */
export function _resetRebuildStatusForTesting() {
  rebuildStatus.value = {
    kind: '', status: 'idle', phase: '', total: 0, processed: 0,
    progress_pct: 0, elapsed_ms: 0,
  }
}
