import { ref, computed, watch, type ComputedRef } from 'vue'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'
import { localConfig } from '@/composables/useSettingsConfig'

const STORAGE_KEY_PREFIX = 'clawbench-recent-files:'
const DEFAULT_MAX_RECENT = 10
const TAG = 'RecentFiles'

function getMaxRecent(): number {
  return (localConfig.recentFilesCount as number) || DEFAULT_MAX_RECENT
}

function trimEntries() {
  const max = getMaxRecent()
  if (_entries.value.length > max) {
    _entries.value = _entries.value.slice(0, max)
    saveToStorage()
  }
}

export interface RecentFileEntry {
  path: string
  accessedAt: number
}

const _entries = ref<RecentFileEntry[]>([])
let _loaded = false

function storageKey(): string {
  const root = store.state.projectRoot || ''
  return STORAGE_KEY_PREFIX + root
}

function loadFromStorage() {
  try {
    const raw = localStorage.getItem(storageKey())
    if (raw) {
      const parsed = JSON.parse(raw)
      _entries.value = parsed.slice(0, getMaxRecent())
    } else {
      // No stored data for this project — clear entries so switching to a
      // project without recent files doesn't leak the previous project's list.
      _entries.value = []
    }
  } catch (e) {
    appLog.w(TAG, 'loadFromStorage failed:', e)
    _entries.value = []
  }
  _loaded = true
}

function saveToStorage() {
  try {
    localStorage.setItem(storageKey(), JSON.stringify(_entries.value))
  } catch (e) {
    appLog.w(TAG, 'saveToStorage failed:', e)
  }
}

let _watchersInitialized = false

function ensureWatchers() {
  if (_watchersInitialized) return
  _watchersInitialized = true
  // Deferred to first use to avoid accessing store during module initialization
  // (circular dep: app.ts → useFileNavStack → useRecentFiles → app.ts)
  watch(() => store?.state?.projectRoot, () => {
    loadFromStorage()
  })
  watch(() => localConfig.recentFilesCount, () => {
    trimEntries()
  })
}

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
  _entries.value = []
  _loaded = false
  _watchersInitialized = false
}

/**
 * Record a file as recently opened.
 * Deduplicates (moves to front if already present), caps at current max.
 */
export function recordRecentFile(path: string) {
  ensureWatchers()
  if (!path || !store.state.projectRoot) return
  const now = Date.now()
  const filtered = _entries.value.filter(e => e.path !== path)
  _entries.value = [{ path, accessedAt: now }, ...filtered].slice(0, getMaxRecent())
  saveToStorage()
}

/**
 * Remove a file from recent history (e.g. after deletion).
 */
export function removeRecentFile(path: string) {
  ensureWatchers()
  _entries.value = _entries.value.filter(e => e.path !== path)
  saveToStorage()
}

/**
 * Open a recent file via the given loader. If the file can no longer be
 * opened (e.g. it was deleted or moved externally), the stale entry is
 * removed from the recent list so it doesn't linger as a dead shortcut.
 * Returns whether the file opened successfully.
 */
export async function openRecentFile(path: string, load: (path: string) => Promise<boolean>): Promise<boolean> {
  ensureWatchers()
  const ok = await load(path)
  if (!ok) {
    removeRecentFile(path)
  }
  return ok
}

export function useRecentFiles() {
  ensureWatchers()
  // Lazy-load on first use
  if (!_loaded) {
    loadFromStorage()
  }

  /**
   * Recent files excluding the given current file path.
   * Respects the dynamic recentFilesCount setting.
   */
  function recentFilesExcluding(currentPath: ComputedRef<string | null>): ComputedRef<RecentFileEntry[]> {
    return computed(() => {
      const max = getMaxRecent()
      const all = currentPath.value
        ? _entries.value.filter(e => e.path !== currentPath.value)
        : _entries.value
      return all.slice(0, max)
    })
  }

  return {
    entries: _entries,
    recentFilesExcluding,
    recordRecentFile,
    removeRecentFile,
    openRecentFile,
  }
}
