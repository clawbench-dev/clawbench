import { ref } from 'vue'
import { apiGet } from '@/utils/api'

// Module-level singleton — shared across all callers
const terminalRuntimeEnabled = ref<boolean | null>(null)
const platformSupported = ref<boolean | null>(null)

/**
 * Lightweight composable for terminal runtime availability.
 *
 * Unlike `getServerValueWithDefault('terminal.enabled')` which reads the
 * *config* value (optimistically updated before restart), this queries the
 * actual server runtime: `/api/terminal/status` returns `enabled: false`
 * when the terminal manager is nil (e.g. config says true but server hasn't
 * restarted yet). Mirrors the SSH pattern where `sshInfo.enabled` comes from
 * the live `/api/ssh/info` endpoint.
 *
 * `platformSupported` indicates whether the OS supports PTY (false on Windows
 * where creack/pty lacks ConPTY). The frontend uses this to show a dedicated
 * "unsupported" empty state instead of hiding the terminal tab entirely.
 */
export function useTerminalStatus() {
  async function loadTerminalStatus() {
    try {
      const data = await apiGet<{ enabled: boolean; platform_supported: boolean }>('/api/terminal/status')
      terminalRuntimeEnabled.value = data.enabled ?? false
      platformSupported.value = data.platform_supported ?? true
    } catch {
      terminalRuntimeEnabled.value = false
      platformSupported.value = false
    }
  }

  return { terminalRuntimeEnabled, platformSupported, loadTerminalStatus }
}
