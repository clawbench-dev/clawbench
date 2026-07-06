import { computed } from 'vue'
import type { Ref } from 'vue'
import { useTerminalStatus } from '@/composables/useTerminalStatus'
import { usePortForward } from '@/composables/usePortForward'
import { useFrp } from '@/composables/useFrp'

export function useDrillDownSideEffects(categoryId: Ref<string> | string) {
  const { loadTerminalStatus } = useTerminalStatus()
  const { loadSSHInfo } = usePortForward()
  const { frpState, fetchFrpInfo } = useFrp()

  // Resolve categoryId (supports both ref and plain string)
  const catId = typeof categoryId === 'string' ? categoryId : categoryId

  /** Run after successful save. changedKeys = dot-paths that were PATCHed. */
  function afterSave(changedKeys: string[]) {
    if (catId === 'terminal' && changedKeys.includes('terminal.enabled')) {
      loadTerminalStatus()
    }
    if (catId === 'portForward' && changedKeys.includes('port_forward.enabled')) {
      loadSSHInfo()
    }
    if (catId === 'frp') {
      if (changedKeys.includes('frp.enabled')) fetchFrpInfo()
    }
  }

  /** Whether to show FRP status dot (only for frp category) */
  const showFrpStatusDot = computed(() => catId === 'frp')

  /** Get FRP status dot color */
  const frpStatusDot = computed(() => {
    if (catId !== 'frp' || !frpState.enabled) return undefined
    if (frpState.state === 'running') return 'green' as const
    if (frpState.state === 'starting') return 'yellow' as const
    if (frpState.state === 'failed') return 'red' as const
    return undefined
  })

  /** Whether to auto-reset TTS voice on engine change */
  const needsVoiceReset = computed(() => catId === 'tts')

  /** FRP auto_port info items (injected when auto_port=true && enabled=true) */
  const frpAutoPortInfo = computed(() => {
    if (catId !== 'frp') return null
    return { state: frpState.state, remotePort: frpState.remotePort, sshRemotePort: frpState.sshRemotePort }
  })

  /** Fetch initial state on mount */
  function init() {
    if (catId === 'frp') fetchFrpInfo()
  }

  return { afterSave, showFrpStatusDot, frpStatusDot, needsVoiceReset, frpAutoPortInfo, init }
}
