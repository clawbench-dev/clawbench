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

/** Map snake_case API response to camelCase FrpState */
function mapFrpInfo(data: Record<string, unknown>): Partial<FrpState> {
  return {
    enabled: data.enabled as boolean | undefined,
    running: data.running as boolean | undefined,
    state: data.state as string | undefined,
    serverAddr: data.server_addr as string | undefined,
    remotePort: data.remote_port as number | undefined,
    sshRemotePort: data.ssh_remote_port as number | undefined,
    remoteUrl: data.remote_url as string | undefined,
  }
}

let wsListenerRegistered = false

export function useFrp() {
  const frpConnected = computed(() => frpState.state === 'running')

  function fetchFrpInfo() {
    apiGet<Record<string, unknown>>('/api/frp/info').then(data => {
      Object.assign(frpState, mapFrpInfo(data))
    }).catch(() => {
      // FRP info not available — leave state as-is
    })
  }

  // Register WS listener once
  if (!wsListenerRegistered) {
    wsListenerRegistered = true
    const { onEvent } = useGlobalEvents()
    onEvent((event: string, data) => {
      if (event !== 'frp_status') return
      const d = data as Record<string, unknown>
      const status = d?.status as string | undefined
      if (!status) return

      frpState.state = status
      frpState.running = status === 'running'
      frpState.enabled = status !== 'disabled'

      // WS event uses snake_case keys
      if (data) {
        if (d.remote_url) frpState.remoteUrl = d.remote_url as string
        if (d.remote_port) frpState.remotePort = d.remote_port as number
        if (d.ssh_remote_port) frpState.sshRemotePort = d.ssh_remote_port as number
        if (d.server_addr) frpState.serverAddr = d.server_addr as string
      }
    })
  }

  return {
    frpState,
    frpConnected,
    fetchFrpInfo,
  }
}
