/**
 * Watches the knowledge base's indexing queue so the user learns when a batch
 * finishes even after leaving the panel.
 *
 * Deliberately a module-level watcher rather than a per-panel one: the panel is
 * closed while indexing continues, and that is exactly the window where the
 * user currently has no idea whether it finished. One toast per batch, not per
 * document.
 */
import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { useToast } from '@/composables/useToast'
import { appLog } from '@/utils/appLog'
import i18n from '@/i18n'

const TAG = 'KnowledgeBase'
const POLL_MS = 3000

const pending = ref(0)
const failed = ref(0)

let timer: ReturnType<typeof setInterval> | null = null
let watching = false
let sawPending = false
let failedAtStart = 0

export function useKBIndexStatus() {
  return { pending, failed }
}

/**
 * Begin (or continue) watching. Call it after adding, importing or reindexing;
 * calling it again while a watch is running just polls once more.
 */
export function startKBIndexWatch(): void {
  if (watching) {
    void pollKBIndex()
    return
  }
  watching = true
  sawPending = false
  failedAtStart = failed.value
  void pollKBIndex()
}

export function stopKBIndexWatch(): void {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
  watching = false
  sawPending = false
}

async function pollKBIndex(): Promise<void> {
  try {
    const res = await apiGet<{
      available: boolean
      stats?: { pending_docs: number; failed_docs: number }
    }>('/api/kb/status')
    if (!res.available || !res.stats) {
      stopKBIndexWatch()
      return
    }
    pending.value = res.stats.pending_docs
    failed.value = res.stats.failed_docs

    if (pending.value > 0) {
      sawPending = true
      ensureTimer()
      return
    }
    // The queue is empty. A watch that never saw work has nothing to report.
    const reported = sawPending
    stopKBIndexWatch()
    if (!reported) return

    const failedCount = Math.max(0, failed.value - failedAtStart)
    const toast = useToast()
    // Never stomp on an action toast the user is reading (e.g. "reindex
    // queued"). The completion is informational; it is most useful precisely
    // when no other toast is on screen.
    if (toast.visible.value) return
    toast.show(
      failedCount > 0
        ? i18n.global.t('kb.indexDoneFailed', { count: failedCount })
        : i18n.global.t('kb.indexDone'),
      { type: failedCount > 0 ? 'error' : 'success', duration: 3200 },
    )
  } catch (e) {
    // A transient status failure must not end the watch: the indexer may
    // still be running.
    appLog.w(TAG, `index status failed: ${e instanceof Error ? e.message : String(e)}`)
    ensureTimer()
  }
}

function ensureTimer(): void {
  if (timer) return
  timer = setInterval(() => {
    void pollKBIndex()
  }, POLL_MS)
}

/** Test hook: drop module state between cases. */
export function _resetKBIndexWatchForTest(): void {
  stopKBIndexWatch()
  pending.value = 0
  failed.value = 0
  failedAtStart = 0
}
