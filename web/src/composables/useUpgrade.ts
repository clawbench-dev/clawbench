import { computed, reactive, ref, watch } from 'vue'
import { apiGet, apiPost } from '@/utils/api'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { getNative } from '@/utils/clawbenchNative'
import { appLog } from '@/utils/appLog'
import { compareVersions } from '@/utils/version'

const TAG = 'Upgrade'
const MAX_POLL_DURATION = 5 * 60 * 1000 // 5 minutes

/** How long the "upgrade complete" message is shown before auto-refreshing. */
const RELOAD_DELAY_MS = 1500
/** sessionStorage flag preventing duplicate refreshes after the first upgrade. */
const RELOAD_SESSION_KEY = 'clawbench-upgrade-reloaded'

const RELEASES_BASE_URL = 'https://github.com/xulongzhe/clawbench/releases/tag/'

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
let reconnectPollTimer: ReturnType<typeof setInterval> | null = null
let pollStartTime: number | null = null
let wsWatchRegistered = false
let completionWatchRegistered = false

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

/**
 * Register the completion→reload trigger exactly once. Both the WS event path
 * and the reconnect-polling path converge on `state.phase`, so a single watch
 * covers them (no double-firing — hardReloadAfterUpgrade is idempotent).
 */
function ensureCompletionWatch() {
  if (completionWatchRegistered) return
  completionWatchRegistered = true
  watch(() => state.phase, (phase, prev) => {
    if (prev !== 'completed' && phase === 'completed') {
      hardReloadAfterUpgrade()
    }
  })
}

/** Register WS connected/disconnected watch exactly once */
function ensureWsWatch() {
  if (wsWatchRegistered) return
  wsWatchRegistered = true
  const { connected } = useGlobalEvents()
  watch(connected, (now) => {
    if (!now && isInProgressInternal()) {
      // WS disconnected while upgrade is active — start polling
      startReconnectPolling()
    }
    if (now && reconnectPollTimer) {
      // WS reconnected — do one immediate check
      pollUpgradeStatus()
    }
  })
}

function isInProgressInternal(): boolean {
  const p = state.phase
  return !!p && p !== 'completed' && p !== 'failed'
}

/** Shared logic: fetch upgrade status and detect completion */
async function fetchStatus(): Promise<void> {
  try {
    const data = await apiGet<UpgradeState>('/api/upgrade/status')
    // New server returns empty phase — upgrade succeeded (the server never
    // sends "completed": the old process is killed while "restarting" and the
    // new process starts with an empty phase).
    const effectivePhase = !data.phase && isInProgressInternal() ? 'completed' : data.phase
    Object.assign(state, data, { phase: effectivePhase })
  } catch {
    // Server may be restarting — keep existing state
  }
}

/** Poll during reconnect: fetch status then stop when the upgrade settles. */
async function pollUpgradeStatus() {
  await fetchStatus()
  if (state.phase === 'completed' || state.phase === 'failed') {
    stopReconnectPolling()
  }
}

/** Start polling upgrade status after WS disconnect during upgrade.
 *  Stops automatically when upgrade completes, fails, or times out. */
function startReconnectPolling() {
  if (reconnectPollTimer) return
  appLog.d(TAG, 'Starting reconnect polling...')
  pollStartTime = Date.now()
  reconnectPollTimer = setInterval(async () => {
    // Timeout guard — prevent infinite polling
    if (pollStartTime && Date.now() - pollStartTime > MAX_POLL_DURATION) {
      appLog.w(TAG, 'Polling timeout — upgrade may have failed')
      state.phase = 'failed'
      state.error = 'Upgrade verification timed out'
      stopReconnectPolling()
      return
    }
    await pollUpgradeStatus()
  }, 2000)
}

function stopReconnectPolling() {
  if (reconnectPollTimer) {
    clearInterval(reconnectPollTimer)
    reconnectPollTimer = null
  }
  pollStartTime = null
}

/**
 * Hard-refresh the page after an upgrade completes so the browser / WebView
 * drops any cached old-version assets (index.html is served with no-cache but
 * WebViews and Service Workers can still serve stale chunks).
 *
 * Only fires once per session — a sessionStorage flag (survives reloads but
 * resets when the tab closes) prevents duplicate refreshes. Native hosts
 * (Electron / Android) get a bridge call that clears their HTTP cache before
 * reloading; plain web falls back to clearing the Cache Storage API +
 * location.reload().
 */
function hardReloadAfterUpgrade(): void {
  // sessionStorage may throw in private/incognito browsing. When unavailable,
  // fall through — the completion watch fires only once per phase transition
  // (phase stays "completed"), so no duplicate reloads are scheduled.
  try {
    if (sessionStorage.getItem(RELOAD_SESSION_KEY)) return
    sessionStorage.setItem(RELOAD_SESSION_KEY, '1')
  } catch { /* flag unavailable — still proceed */ }
  appLog.i(TAG, 'Upgrade completed — scheduling hard reload')
  setTimeout(() => {
    if (getNative()?.reloadApp) {
      const res = getNative()?.reloadApp?.()
      if (res instanceof Promise) {
        res.catch((e) => appLog.w(TAG, 'reloadApp failed', e))
      }
      return
    }
    // Web fallback: drop Cache Storage entries (Service Worker leftovers) then reload.
    if (window.caches && window.caches.keys) {
      window.caches.keys()
        .then((keys) => Promise.all(keys.map((k) => window.caches.delete(k))))
        .catch(() => {})
        .finally(() => window.location.reload())
    } else {
      window.location.reload()
    }
  }, RELOAD_DELAY_MS)
}

export function useUpgrade() {
  const { serverConfig } = useSettingsConfig()

  ensureWsListener()
  ensureWsWatch()
  ensureCompletionWatch()

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
    // Allow the auto-reload to fire again for this tab's next upgrade.
    sessionStorage.removeItem(RELOAD_SESSION_KEY)
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

  /** GitHub release notes URL for the target (latest) version */
  const releaseNotesUrl = computed(() => {
    const latest = state.latest_version
    if (!latest) return ''
    const tag = `v${latest.replace(/^v/, '')}`
    return `${RELEASES_BASE_URL}${tag}`
  })

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
    releaseNotesUrl,
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
    hardReloadAfterUpgrade,
  }
}
