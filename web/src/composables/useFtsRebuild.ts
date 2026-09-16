import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

/** Progress snapshot returned by GET /api/rag/rebuild-fts/status. */
export interface FtsRebuildStatus {
  status: 'idle' | 'running' | 'done' | 'error' | 'cancelled'
  phase: string
  total: number
  processed: number
  progress_pct: number
  indexed: number
  resegmented: number
  elapsed_ms: number
  error?: string
}

const POLL_INTERVAL_MS = 1000
// The rebuild re-segments every chunk (~149s on a 44k-chunk store), so the
// poller must be willing to wait minutes. This is only a runaway guard.
const POLL_TIMEOUT_MS = 30 * 60 * 1000

/**
 * Poll the FTS rebuild status until it reaches a terminal state.
 *
 * The server runs the rebuild in the background and answers the trigger with
 * 202; polling is how the UI learns the outcome. Previously the request was
 * held open for the whole rebuild, which exceeded the 10s API timeout and made
 * a successful rebuild appear as a failure.
 *
 * Returns the terminal status, or null if polling timed out.
 */
export async function pollFtsRebuildStatus(
  isCancelled: () => boolean = () => false,
): Promise<FtsRebuildStatus | null> {
  const deadline = Date.now() + POLL_TIMEOUT_MS
  while (Date.now() < deadline) {
    if (isCancelled()) return null
    try {
      const status = await apiGet<FtsRebuildStatus>('/api/rag/rebuild-fts/status')
      if (status.status !== 'running') return status
    } catch (err) {
      // A transient poll failure must not abort the whole operation: the
      // rebuild keeps running server-side, so keep polling.
      appLog.w('RagRebuild', 'status poll failed, retrying', err)
    }
    await new Promise(resolve => setTimeout(resolve, POLL_INTERVAL_MS))
  }
  appLog.w('RagRebuild', 'status polling timed out')
  return null
}

/**
 * Trigger the FTS rebuild and wait for its terminal status.
 *
 * Throws on a rejected trigger (409 already running, 503 segmenter
 * unavailable) so callers can surface the server's localized explanation.
 */
export async function startFtsRebuild(
  isCancelled: () => boolean = () => false,
): Promise<FtsRebuildStatus | null> {
  await apiPost('/api/rag/rebuild-fts', {})
  return pollFtsRebuildStatus(isCancelled)
}
