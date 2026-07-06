import { reactive, computed } from 'vue'
import { apiGet } from '@/utils/api'
import { useGlobalEvents } from './useGlobalEvents'

export interface FrpState {
  enabled: boolean
  running: boolean
  state: string // 'disabled' | 'starting' | 'running' | 'failed' | 'stopped'
  serverAddr: string
  remotePort: number
  sshRemotePort: number
  remoteUrl: string
}

// Module-level singleton (shared across all consumers)
const frpState = reactive<FrpState>({
  enabled: false,
  running: false,
  state: 'disabled',
  serverAddr: '',
  remotePort: 0,
  sshRemotePort: 0,
  remoteUrl: '',
})

let wsListenerRegistered = false

export function useFrp() {
  const frpConnected = computed(() => frpState.state === 'running')

  function fetchFrpInfo() {
    apiGet<FrpState>('/api/frp/info').then(data => {
      Object.assign(frpState, data)
    }).catch(() => {
      // FRP info not available — leave state as-is
    })
  }

  // Register WS listener once
  if (!wsListenerRegistered) {
    wsListenerRegistered = true
    const { onEvent } = useGlobalEvents()
    onEvent((event: string, data: any) => {
      if (event !== 'frp_status') return
      const status = data?.status as string | undefined
      if (!status) return

      frpState.state = status
      frpState.running = status === 'running'
      frpState.enabled = status !== 'disabled'

      if (data) {
        if (data.remote_url) frpState.remoteUrl = data.remote_url as string
        if (data.remote_port) frpState.remotePort = data.remote_port as number
        if (data.ssh_remote_port) frpState.sshRemotePort = data.ssh_remote_port as number
        if (data.server_addr) frpState.serverAddr = data.server_addr as string
      }
    })
  }

  return {
    frpState,
    frpConnected,
    fetchFrpInfo,
  }
}
