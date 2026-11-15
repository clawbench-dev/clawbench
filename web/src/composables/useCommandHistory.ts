import { ref, computed } from 'vue'
import { apiGet, apiPost, apiDelete } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const TAG = 'CommandHistory'

/** One remembered command. Mirrors service.TerminalCommand. */
export interface TerminalCommand {
  id: number
  command: string
  use_count: number
  last_used_at: string
  project_path: string
  project_only: boolean
}

/** How long a sync stays fresh. Opening the panel repeatedly must not re-read
 *  the shell history files every time. */
const SYNC_TTL_MS = 10 * 60 * 1000

// Module-level singletons: the drawer, the input drawer and the terminal panel
// all read the same list, and a second instance of the composable would show a
// stale copy after one of them records a command.
const items = ref<TerminalCommand[]>([])
const loading = ref(false)
const loaded = ref(false)
let lastSyncAt = 0

/** @internal Reset module state — for tests only. */
export function _resetCommandHistoryForTesting() {
  items.value = []
  loading.value = false
  loaded.value = false
  lastSyncAt = 0
}

export function useCommandHistory() {
  /** Most recent commands, newest first. */
  const recent = computed(() => items.value)

  async function fetchHistory(search = '', limit = 100) {
    loading.value = true
    try {
      const params = new URLSearchParams()
      if (search) params.set('q', search)
      params.set('limit', String(limit))
      const res = await apiGet<TerminalCommand[]>(`/api/terminal/history?${params.toString()}`)
      items.value = res || []
      loaded.value = true
    } catch (err) {
      appLog.w(TAG, 'failed to load history', String(err))
    } finally {
      loading.value = false
    }
  }

  /**
   * Import commands from the local shells' history files.
   *
   * Idempotent on the server (existing rows are left alone), so calling it when
   * the panel opens is safe; the TTL only avoids re-reading the files on every
   * open. Returns how many rows were added.
   */
  async function syncFromShells(force = false): Promise<number> {
    if (!force && Date.now() - lastSyncAt < SYNC_TTL_MS) return 0
    try {
      const res = await apiPost<{ added: number }>('/api/terminal/history/sync', {})
      lastSyncAt = Date.now()
      return res?.added ?? 0
    } catch (err) {
      appLog.w(TAG, 'failed to sync shell history', String(err))
      return 0
    }
  }

  /** Load the list, importing from the shell history files first if stale. */
  async function refresh(search = '') {
    const added = await syncFromShells()
    if (added > 0 || !loaded.value) {
      await fetchHistory(search)
    }
  }

  async function remove(id: number): Promise<boolean> {
    try {
      await apiDelete(`/api/terminal/history/${id}`)
      items.value = items.value.filter(i => i.id !== id)
      return true
    } catch (err) {
      appLog.w(TAG, 'failed to delete entry', String(err))
      return false
    }
  }

  async function clear(): Promise<boolean> {
    try {
      await apiDelete('/api/terminal/history')
      items.value = []
      return true
    } catch (err) {
      appLog.w(TAG, 'failed to clear history', String(err))
      return false
    }
  }

  return {
    items,
    recent,
    loading,
    loaded,
    fetchHistory,
    syncFromShells,
    refresh,
    remove,
    clear,
  }
}
