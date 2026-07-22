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
      _entries.value = JSON.parse(raw)
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

// Module-level watcher — runs exactly once
watch(() => store.state.projectRoot, () => {
  loadFromStorage()
})

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
  _entries.value = []
  _loaded = false
}

/**
 * Record a file as recently opened.
 * Deduplicates (moves to front if already present), caps at MAX_RECENT.
 */
export function recordRecentFile(path: string) {
  if (!path) return
  const now = Date.now()
  const filtered = _entries.value.filter(e => e.path !== path)
  _entries.value = [{ path, accessedAt: now }, ...filtered].slice(0, getMaxRecent())
  saveToStorage()
}

/**
 * Remove a file from recent history (e.g. after deletion).
 */
export function removeRecentFile(path: string) {
  _entries.value = _entries.value.filter(e => e.path !== path)
  saveToStorage()
}

export function useRecentFiles() {
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
  }
}
