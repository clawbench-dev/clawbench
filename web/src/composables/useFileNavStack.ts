import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { recordRecentFile } from '@/composables/useRecentFiles'

const MAX_STACK_DEPTH = 20

export interface FileNavLocation {
  path: string
  lineStart?: number
  lineEnd?: number
  viewMode?: string
}

const _overlayOpen = ref(false)
const _history: Ref<FileNavLocation[]> = ref([])
const _historyIndex = ref(-1)

const _currentLocation = computed(() => {
  const index = _historyIndex.value
  return index >= 0 ? _history.value[index] ?? null : null
})
const _currentFilePath = computed(() => _currentLocation.value?.path ?? null)

const _canGoBack = computed(() => _historyIndex.value > 0)
const _canGoForward = computed(() => _historyIndex.value >= 0 && _historyIndex.value < _history.value.length - 1)

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
  _overlayOpen.value = false
  _history.value = []
  _historyIndex.value = -1
}

export function useFileNavStack() {
  function openFile(path: string, location: Omit<FileNavLocation, 'path'> = {}) {
    _overlayOpen.value = true
    // Record to recent files — single entry point for all file opens
    recordRecentFile(path)
    const nextLocation = { path, ...location }
    const current = _currentLocation.value
    // Consecutive opens of the same destination update metadata without adding
    // a duplicate history item. Different line targets in one file are distinct.
    if (current?.path === path && current.lineStart === location.lineStart && current.lineEnd === location.lineEnd) {
      _history.value[_historyIndex.value] = { ...current, ...nextLocation }
      return
    }
    // A new navigation after going back starts a new branch, as browser history does.
    const next = [..._history.value.slice(0, _historyIndex.value + 1), nextLocation]
    if (next.length > MAX_STACK_DEPTH) {
      _history.value = next.slice(next.length - MAX_STACK_DEPTH)
    } else {
      _history.value = next
    }
    _historyIndex.value = _history.value.length - 1
  }

  function updateCurrent(location: Partial<Omit<FileNavLocation, 'path'>>) {
    const current = _currentLocation.value
    if (!current) return
    _history.value[_historyIndex.value] = { ...current, ...location }
  }

  function goBack(): string | null {
    if (!_canGoBack.value) return null
    _historyIndex.value--
    return _currentFilePath.value
  }

  function goForward(): string | null {
    if (!_canGoForward.value) return null
    _historyIndex.value++
    return _currentFilePath.value
  }

  function closeOverlay() {
    _overlayOpen.value = false
    _history.value = []
    _historyIndex.value = -1
  }

  function removePath(path: string) {
    const idx = _history.value.map((entry) => entry.path).lastIndexOf(path)
    if (idx === -1) return
    _history.value = [..._history.value.slice(0, idx), ..._history.value.slice(idx + 1)]
    if (idx < _historyIndex.value) _historyIndex.value--
    else if (_historyIndex.value >= _history.value.length) _historyIndex.value = _history.value.length - 1
    // If the stack is now empty, close overlay
    if (_history.value.length === 0) {
      _overlayOpen.value = false
    }
  }

  return {
    overlayOpen: _overlayOpen,
    currentFilePath: _currentFilePath as ComputedRef<string | null>,
    currentLocation: _currentLocation as ComputedRef<FileNavLocation | null>,
    canGoBack: _canGoBack,
    canGoForward: _canGoForward,
    openFile,
    updateCurrent,
    goBack,
    goForward,
    closeOverlay,
    removePath,
  }
}
