import { ref } from 'vue'
import { baseName } from '@/utils/path'
import { recentProjectDisplayPath } from '@/utils/recentProjects'
import { appLog } from '@/utils/appLog'

const TAG = 'RecentProjects'

export interface RecentProjectItem {
  name: string
  path: string
  displayPath: string
}

/**
 * Loads the recent-projects list from the backend (`GET /api/recent-projects`,
 * ordered by `accessed_at DESC`). Shared by the header dropdown and the
 * session-sidebar chips bar so both surfaces stay in sync.
 */
export function useRecentProjects() {
  const items = ref<RecentProjectItem[]>([])
  const loading = ref(false)

  async function load(homeDir = ''): Promise<void> {
    loading.value = true
    try {
      const resp = await fetch('/api/recent-projects')
      const paths: string[] = await resp.json()
      items.value = paths.map((p) => ({
        name: baseName(p),
        path: p,
        displayPath: recentProjectDisplayPath(p, homeDir),
      }))
    } catch (e) {
      appLog.w(TAG, 'Failed to load recent projects', e)
      items.value = []
    } finally {
      loading.value = false
    }
  }

  return { items, loading, load }
}
