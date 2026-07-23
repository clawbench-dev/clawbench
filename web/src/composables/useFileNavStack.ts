import { ref, computed, type Ref, type ComputedRef } from 'vue'
import { recordRecentFile } from '@/composables/useRecentFiles'

const MAX_STACK_DEPTH = 20

const _overlayOpen = ref(false)
const _pathStack: Ref<string[]> = ref([])

const _currentFilePath = computed(() => {
  const stack = _pathStack.value
  return stack.length > 0 ? stack[stack.length - 1] : null
})

const _canGoBack = computed(() => _pathStack.value.length > 1)

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
  _overlayOpen.value = false
  _pathStack.value = []
}

export function useFileNavStack() {
  function openFile(path: string) {
    _overlayOpen.value = true
    // Record to recent files — single entry point for all file opens
    recordRecentFile(path)
    // Deduplicate: if the same file is already at the top, don't push again
    const stack = _pathStack.value
    if (stack.length > 0 && stack[stack.length - 1] === path) return
    // Cap stack depth — trim oldest entries from the bottom
    const next = [...stack, path]
    if (next.length > MAX_STACK_DEPTH) {
      _pathStack.value = next.slice(next.length - MAX_STACK_DEPTH)
    } else {
      _pathStack.value = next
    }
  }

  function goBack(): string | null {
    if (_pathStack.value.length <= 1) return null
    _pathStack.value = _pathStack.value.slice(0, -1)
    return _pathStack.value[_pathStack.value.length - 1]
  }

  function closeOverlay() {
    _overlayOpen.value = false
    _pathStack.value = []
  }

  return {
    overlayOpen: _overlayOpen,
    currentFilePath: _currentFilePath as ComputedRef<string | null>,
    canGoBack: _canGoBack,
    openFile,
    goBack,
    closeOverlay,
  }
}
