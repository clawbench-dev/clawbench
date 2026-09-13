/**
 * Directory listing for the file manager's docked preview pane.
 *
 * When preview mode is on and the user clicks a directory, the pane shows that
 * directory's contents instead of a file. The listing is fetched on demand from
 * the existing `/api/dir` endpoint (the same one the main list uses), so the
 * pane never depends on the parent's `entries` prop — it can show a directory
 * that is not the one currently open in the list.
 */

import { ref, watch, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { apiGet } from '@/utils/api'

/** A single entry as returned by GET /api/dir. */
export interface DirPreviewEntry {
  name: string
  type: 'dir' | 'file' | 'image'
  size?: number
  modified?: string
  symlink?: boolean
  broken?: boolean
}

export interface UseDirPreviewOptions {
  /** Fetch only while this is true (preview mode on + pane visible). */
  active: Ref<boolean>
  /** The project-relative directory to list. */
  dirPath: Ref<string>
  /** Honour the file manager's "show hidden files" toggle. */
  showHidden: Ref<boolean>
}

export function useDirPreview(options: UseDirPreviewOptions) {
  const { active, dirPath, showHidden } = options

  const entries = ref<DirPreviewEntry[]>([])
  const loading = ref(false)
  const error = ref(false)
  const loadedPath = ref('')

  /** Bumped per request so a slow response for a previous directory can't
   *  overwrite the current one. */
  let seq = 0

  async function load(path: string) {
    const mySeq = ++seq
    loading.value = true
    error.value = false
    try {
      const url = `/api/dir?path=${encodeURIComponent(path)}`
      const data = await apiGet<{ items: DirPreviewEntry[] }>(url, { timeoutMs: 10_000 })
      if (mySeq !== seq) return
      entries.value = data.items || []
      loadedPath.value = path
    } catch (err) {
      if (mySeq !== seq) return
      appLog.w('DirPreview', 'Failed to list directory for preview', { path, error: err })
      entries.value = []
      error.value = true
      loadedPath.value = path
    } finally {
      if (mySeq === seq) loading.value = false
    }
  }

  /** Re-list the current directory (used by the pane's refresh affordance). */
  function refresh() {
    if (!active.value || !dirPath.value) return
    void load(dirPath.value)
  }

  watch(
    [active, dirPath],
    ([isActive, path]) => {
      if (!isActive || !path) {
        // Dropping out of preview mode (or clearing the target) releases the
        // listing so a stale directory can't flash when the pane reopens.
        if (!isActive) {
          entries.value = []
          loadedPath.value = ''
          loading.value = false
        }
        return
      }
      void load(path)
    },
    { immediate: true },
  )

  /** Hidden entries are filtered at render time so toggling "show hidden" in
   *  the toolbar updates the pane without a refetch. */
  function visible(entry: DirPreviewEntry): boolean {
    return showHidden.value || !entry.name.startsWith('.')
  }

  return { entries, loading, error, loadedPath, visible, refresh }
}
