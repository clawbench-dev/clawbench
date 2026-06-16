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

/**
 * Apply a stack mutation with automatic rollback on failure.
 * Snapshots the stack before mutation, runs `fn`, and restores if `fn` throws.
 */
function withRollback(mutate: () => void, fn: () => Promise<void>): Promise<void> {
  const prev = [..._dirStack.value]
  mutate()
  return fn().catch(() => {
    _dirStack.value = prev
  })
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

  /**
   * Async-aware stack operations with automatic rollback on loadFiles failure.
   * These snapshot the stack, apply the mutation, call `loadFn`, and restore
   * the previous stack if `loadFn` throws.
   */
  async function pushDirAndLoad(path: string, loadFn: () => Promise<void>): Promise<void> {
    if (_dirStack.value.length > 0 && _dirStack.value[_dirStack.value.length - 1] === path) return
    await withRollback(() => { _dirStack.value = [..._dirStack.value, path] }, loadFn)
  }

  async function popDirAndLoad(loadFn: () => Promise<void>): Promise<string | null> {
    if (_dirStack.value.length <= 1) return null
    const newDir = _dirStack.value[_dirStack.value.length - 2]
    await withRollback(() => { _dirStack.value = _dirStack.value.slice(0, -1) }, loadFn)
    return newDir
  }

  async function truncateToDirAndLoad(path: string, loadFn: () => Promise<void>): Promise<void> {
    const idx = _dirStack.value.indexOf(path)
    await withRollback(() => {
      _dirStack.value = idx !== -1 ? _dirStack.value.slice(0, idx + 1) : [path]
    }, loadFn)
  }

  async function replaceTopAndLoad(path: string, loadFn: () => Promise<void>): Promise<void> {
    await withRollback(() => {
      _dirStack.value = _dirStack.value.length === 0
        ? [path]
        : [..._dirStack.value.slice(0, -1), path]
    }, loadFn)
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
    pushDirAndLoad,
    popDirAndLoad,
    truncateToDirAndLoad,
    replaceTopAndLoad,
  }
}
