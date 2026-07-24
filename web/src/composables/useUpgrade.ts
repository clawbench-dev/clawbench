import { computed, reactive, ref, watch } from 'vue'
import { apiGet, apiPost } from '@/utils/api'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { appLog } from '@/utils/appLog'
import { compareVersions } from '@/utils/version'

const TAG = 'Upgrade'

export interface UpgradeState {
  phase: string
  current_version: string
  latest_version: string
  progress: number
  message: string
  backup_path: string
  error: string
}

const SKIP_KEY = 'clawbench-upgrade-skip'

// Module-level singleton state (shared across all component instances)
const state = reactive<UpgradeState>({
  phase: '',
  current_version: '',
  latest_version: '',
  progress: 0,
  message: '',
  backup_path: '',
  error: '',
})

const checking = ref(false)
const hasUpgrade = ref(false)
const showProgressDialog = ref(false)

let wsUnsubscribe: (() => void) | null = null

function ensureWsListener() {
  if (wsUnsubscribe) return
  const { onEvent } = useGlobalEvents()
  wsUnsubscribe = onEvent((event: string, data: unknown) => {
    if (event !== 'upgrade_update') return
    const d = data as UpgradeState
    Object.assign(state, d)
    appLog.d(TAG, 'WS update', { phase: d.phase, progress: d.progress })
  })
}

/** Called on WS reconnect to check if upgrade completed while disconnected */
async function onWsReconnect() {
  if (state.phase !== 'restarting') return
  appLog.d(TAG, 'WS reconnect while restarting, fetching status...')
  try {
    const data = await apiGet<UpgradeState>('/api/upgrade/status')
    Object.assign(state, data)
    // New server returns empty phase — upgrade succeeded
    if (!data.phase) {
      appLog.d(TAG, 'Upgrade verified: new server running')
      state.phase = 'completed'
    }
  } catch {
    // Status fetch failed, keep current state
  }
}

export function useUpgrade() {
  const { serverConfig } = useSettingsConfig()
  const { connected } = useGlobalEvents()

  ensureWsListener()

  // On WS reconnect after server restart during upgrade, verify upgrade result
  watch(connected, async (now, was) => {
    if (was === false && now === true && state.phase === 'restarting') {
      await onWsReconnect()
    }
  })

  /** Check for available upgrade */
  async function checkUpgrade(): Promise<void> {
    checking.value = true
    try {
      const data = await apiGet<{
        current_version: string
        latest_version: string
        has_upgrade: boolean
      }>('/api/upgrade/check')
      state.current_version = data.current_version
      state.latest_version = data.latest_version
      hasUpgrade.value = data.has_upgrade
    } catch (e) {
      appLog.w(TAG, 'Check failed', e)
      hasUpgrade.value = false
    } finally {
      checking.value = false
    }
  }

  /** Start the upgrade process and show progress dialog */
  async function startUpgrade(): Promise<void> {
    showProgressDialog.value = true
    try {
      await apiPost('/api/upgrade/start', {})
    } catch (e) {
      appLog.e(TAG, 'Start failed', e)
    }
  }

  /** Clear show progress flag (called after dialog opens) */
  function clearShowProgressDialog(): void {
    showProgressDialog.value = false
  }

  /** Fetch current upgrade status from server */
  async function fetchStatus(): Promise<void> {
    try {
      const data = await apiGet<UpgradeState>('/api/upgrade/status')
      Object.assign(state, data)
    } catch {
      // Server may be restarting
    }
  }

  /** Check if an upgrade prompt should be shown (startup check) */
  async function checkForUpgradePrompt(): Promise<string | null> {
    try {
      await checkUpgrade()
      if (!hasUpgrade.value) return null

      // Check skip preference
      const skipped = localStorage.getItem(SKIP_KEY)
      if (skipped === state.latest_version) return null

      return state.latest_version
    } catch {
      return null
    }
  }

  /** Skip this version (don't prompt again for this version) */
  function skipVersion(version: string): void {
    localStorage.setItem(SKIP_KEY, version)
  }

  /** Whether upgrade is in progress */
  const isInProgress = computed(() => {
    const p = state.phase
    return !!p && p !== 'completed' && p !== 'failed'
  })

  /** Whether server is restarting */
  const isRestarting = computed(() => state.phase === 'restarting')

  /** Whether upgrade completed successfully */
  const isCompleted = computed(() => state.phase === 'completed')

  /** Whether upgrade failed */
  const isFailed = computed(() => state.phase === 'failed')

  /** Verify upgrade succeeded by comparing server version after reconnect */
  async function verifyUpgrade(): Promise<boolean> {
    try {
      await fetchStatus()
      // Also check the server config version
      const { loadConfig } = useSettingsConfig()
      await loadConfig()
      const currentVer = (serverConfig.value?.version as string) ?? ''
      const latestVer = state.latest_version
      if (!currentVer || !latestVer) return false
      return compareVersions(currentVer, latestVer) >= 0
    } catch {
      return false
    }
  }

  return {
    state,
    checking,
    hasUpgrade,
    showProgressDialog,
    isInProgress,
    isRestarting,
    isCompleted,
    isFailed,
    checkUpgrade,
    startUpgrade,
    clearShowProgressDialog,
    fetchStatus,
    checkForUpgradePrompt,
    skipVersion,
    verifyUpgrade,
  }
}
