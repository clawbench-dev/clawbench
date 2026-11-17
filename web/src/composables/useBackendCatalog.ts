import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

/**
 * One entry of the server's backend catalogue (`GET /api/backends`).
 *
 * Mirrors the JSON the handler emits for `model.BackendSpec`: the fields the
 * two install surfaces need are the id (AgentIcon lookup + install endpoint),
 * the display name/specialty, and the built-in install command. An empty
 * `install_cmd` means "not installable" (the built-in backend).
 */
export interface BackendInfo {
  id: string
  name: string
  specialty: string
  default_cmd: string
  thinking_effort_levels?: string[]
  install_cmd?: string
}

const TAG = 'BackendCatalog'

/**
 * The backend catalogue shared by the first-run welcome panel and
 * Settings › Agents.
 *
 * Both surfaces offer the same one-click install, so they must render the same
 * list and the same commands — the server owns the command (the install
 * endpoint looks it up itself); this composable only reads it for display.
 *
 * Per-instance state rather than a module singleton: the two surfaces are
 * never mounted together in practice, and a shared cache would need a reset
 * hook for tests. A second small request is cheaper than that coupling.
 */
export function useBackendCatalog() {
  const backends = ref<BackendInfo[]>([])
  const loading = ref(false)

  async function load(): Promise<BackendInfo[]> {
    loading.value = true
    try {
      const resp = await fetch('/api/backends')
      if (!resp.ok) return backends.value
      const data = await resp.json() as { backends?: BackendInfo[] }
      backends.value = data.backends || []
    } catch (err) {
      // An empty catalogue hides the install section; the caller's own
      // loading/empty state covers the visible half of the failure.
      appLog.w(TAG, 'failed to load backend catalogue', err)
    } finally {
      loading.value = false
    }
    return backends.value
  }

  return { backends, loading, load }
}
