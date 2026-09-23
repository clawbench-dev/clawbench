import { ref } from 'vue'
import { apiGet } from '@/utils/api'

// Module-level singleton — shared across all callers
const terminalRuntimeEnabled = ref<boolean | null>(null)
const platformSupported = ref<boolean | null>(null)
const cwdProbeSupported = ref<boolean | null>(null)

/**
 * Lightweight composable for terminal runtime availability.
 *
 * Unlike `getServerValueWithDefault('terminal.enabled')` which reads the
 * *config* value (optimistically updated before restart), this queries the
 * actual server runtime: `/api/terminal/status` returns `enabled: false`
 * when the terminal manager is nil (e.g. config says true but server hasn't
 * restarted yet). Mirrors the SSH pattern where `sshInfo.enabled` comes from
 * the live `/api/ssh/info/full` endpoint.
 *
 * `platformSupported` indicates whether the OS supports PTY (false on Windows
 * where creack/pty lacks ConPTY). The frontend uses this to show a dedicated
 * "unsupported" empty state instead of hiding the terminal tab entirely.
 *
 * `cwdProbeSupported` indicates whether the SERVER can resolve the shell's live
 * working directory (Linux/Android only — see internal/terminal/cwd_linux.go).
 * It gates dropping files onto the terminal, which uploads into that directory;
 * without a live cwd the drop would silently target the wrong folder. Note this
 * is a server capability and cannot be inferred from `navigator.userAgent`: the
 * browser may run on macOS while the server runs on Linux.
 */
export function useTerminalStatus() {
  async function loadTerminalStatus() {
    try {
      const data = await apiGet<{
        enabled: boolean
        platform_supported: boolean
        cwd_probe_supported: boolean
      }>('/api/terminal/status')
      terminalRuntimeEnabled.value = data.enabled ?? false
      platformSupported.value = data.platform_supported ?? true
      cwdProbeSupported.value = data.cwd_probe_supported ?? false
    } catch {
      terminalRuntimeEnabled.value = false
      platformSupported.value = false
      cwdProbeSupported.value = false
    }
  }

  return { terminalRuntimeEnabled, platformSupported, cwdProbeSupported, loadTerminalStatus }
}
