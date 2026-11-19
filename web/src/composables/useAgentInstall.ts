import { ref } from 'vue'
import { apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const TAG = 'AgentInstall'

/**
 * Server-side install of a backend's built-in install command.
 *
 * The command is NOT sent from here: the server looks it up in its own backend
 * registry (`POST /api/backends/{id}/install`). That keeps an authenticated
 * endpoint from becoming "run this string on the server", and it also means the
 * base command cannot diverge from the one the dialog shows.
 *
 * The npm source is the one part that is not known up front: the server probes
 * the candidates when the install runs and appends `--registry=…` to the winner,
 * so the executed string can differ from the displayed one. The response's
 * `command` carries what actually ran, and the dialog shows it when it differs.
 *
 * Installs run on the machine serving ClawBench, which for a remote server or a
 * container is NOT the device looking at the UI.
 */

/**
 * The server's own budget is 10 minutes for the install command plus up to 5
 * minutes for downloading a private Node.js when the machine has none
 * (service.DefaultAgentInstallTimeout + platform's node install budget). The
 * client waits slightly longer so a slow-but-successful install is not reported
 * as a failure while the server is still finishing — the request would be
 * abandoned but the install would keep running, which reads as a phantom
 * failure and invites a second, conflicting attempt.
 */
const INSTALL_TIMEOUT_MS = 16 * 60 * 1000

/** The shape of a successful install response. */
export interface AgentInstallResponse {
  ok: boolean
  backend: string
  command: string
  /** Tail of the installer's output (may be empty). */
  output: string
  durationMs: number
  /** Whether the post-install rescan ran; false means the agent list is stale. */
  refreshed: boolean
  /** Fresh agent list, same shape as GET /api/agents. */
  agents?: unknown[]
  defaultAgent?: string
}

/**
 * Install one backend. Throws on failure with the server's localized message
 * already attached (that message carries the installer's output tail for a
 * command failure, and names the missing tool for a Node.js problem).
 */
export async function installBackend(backendId: string): Promise<AgentInstallResponse> {
  appLog.i(TAG, `installing backend ${backendId}`)
  const res = await apiPost<AgentInstallResponse>(
    `/api/backends/${encodeURIComponent(backendId)}/install`,
    {},
    { timeoutMs: INSTALL_TIMEOUT_MS },
  )
  appLog.i(TAG, `install finished for ${backendId} in ${res?.durationMs ?? 0}ms`)
  return res
}

/**
 * Install state for one dialog instance.
 *
 * Deliberately per-instance rather than a module singleton: two dialogs (the
 * welcome panel and an agent's settings page) must not share a spinner, and the
 * server already refuses concurrent installs globally.
 */
export function useAgentInstall() {
  const installing = ref(false)
  const error = ref('')
  const result = ref<AgentInstallResponse | null>(null)

  async function run(backendId: string): Promise<AgentInstallResponse | null> {
    if (installing.value) return null
    installing.value = true
    error.value = ''
    result.value = null
    try {
      const res = await installBackend(backendId)
      result.value = res
      return res
    } catch (err) {
      // The server's message is the actionable part (which executable is
      // missing, or the installer's own output). A network failure has no such
      // message, so fall back to the raw error text rather than an empty banner.
      error.value = err instanceof Error && err.message ? err.message : String(err)
      appLog.w(TAG, `install failed for ${backendId}`, err)
      return null
    } finally {
      installing.value = false
    }
  }

  function reset() {
    error.value = ''
    result.value = null
  }

  return { installing, error, result, run, reset }
}
