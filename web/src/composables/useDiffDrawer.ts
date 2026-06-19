/**
 * useDiffDrawer — shared composable for DiffDrawer state.
 *
 * Used by both CodePreview and MarkdownPreview to avoid duplicating
 * the drawerVisible / drawerMarkerType / drawerCharDiff / drawerDiffLines
 * computed properties and closeDrawer handler.
 */
import { computed } from 'vue'
import {
  diffDrawerVisible,
  diffDrawerMarker,
  closeDiffDrawer,
} from '@/composables/useMarkdownDiff.ts'

export function useDiffDrawer() {
  const drawerVisible = computed(() => diffDrawerVisible.value)
  const drawerMarkerType = computed(() => diffDrawerMarker.value?.type || 'modified')
  const drawerCharDiff = computed(() => diffDrawerMarker.value?.charDiff || null)
  const drawerDiffLines = computed(() => diffDrawerMarker.value?.diffLines)

  function closeDrawer() {
    closeDiffDrawer()
  }

  return {
    drawerVisible,
    drawerMarkerType,
    drawerCharDiff,
    drawerDiffLines,
    closeDrawer,
  }
}
