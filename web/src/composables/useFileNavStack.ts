import { ref, computed, type Ref, type ComputedRef } from 'vue'

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
    _pathStack.value = [..._pathStack.value, path]
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
