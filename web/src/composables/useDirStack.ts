import { ref, computed, readonly, type Ref, type ComputedRef } from 'vue'

const _dirStack: Ref<string[]> = ref([])

const _currentDir = computed(() => {
  const stack = _dirStack.value
  return stack.length > 0 ? stack[stack.length - 1] : ''
})

const _canGoBack = computed(() => _dirStack.value.length > 1)

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
  _dirStack.value = []
}

/** @internal Restore stack to a previous snapshot — for error rollback only */
export function _restoreStack(prev: string[]) {
  _dirStack.value = prev
}

export function useDirStack() {
  function pushDir(path: string) {
    // Dedup: no-op if pushing the same path that's already on top
    if (_dirStack.value.length > 0 && _dirStack.value[_dirStack.value.length - 1] === path) return
    _dirStack.value = [..._dirStack.value, path]
  }

  function popDir(): string | null {
    if (_dirStack.value.length <= 1) return null
    _dirStack.value = _dirStack.value.slice(0, -1)
    return _dirStack.value[_dirStack.value.length - 1]
  }

  function truncateToDir(path: string) {
    const idx = _dirStack.value.indexOf(path)
    if (idx !== -1) {
      // Truncate to the target (inclusive)
      _dirStack.value = _dirStack.value.slice(0, idx + 1)
    } else {
      // Path not in stack — reset to just this path
      _dirStack.value = [path]
    }
  }

  function resetStack(path?: string) {
    _dirStack.value = path ? [path] : []
  }

  function replaceTop(path: string) {
    if (_dirStack.value.length === 0) {
      _dirStack.value = [path]
    } else {
      _dirStack.value = [..._dirStack.value.slice(0, -1), path]
    }
  }

  return {
    dirStack: readonly(_dirStack),
    currentDir: _currentDir as ComputedRef<string>,
    canGoBack: _canGoBack,
    pushDir,
    popDir,
    truncateToDir,
    resetStack,
    replaceTop,
  }
}
