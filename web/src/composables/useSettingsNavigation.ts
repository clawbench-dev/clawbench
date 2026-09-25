import { ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

const MAX_POLL_ATTEMPTS = 60 // 2 minutes at 2s interval
const POLL_INTERVAL_MS = 2000

// ── Module-level guard registry ──
// Must be module-level (not per-instance) so that SettingsGroupPanel and
// SettingsPage share the same Map regardless of which component calls
// useSettingsNavigation() first.
const guards = new Map<string, () => boolean>()

function registerGuard(id: string, guard: () => boolean) {
  guards.set(id, guard)
}

function unregisterGuard(id: string) {
  guards.delete(id)
}

// Module-level shared overlay state — the global ConnectionOverlay reads the same
// ref that SettingsPage writes to, so both stay in sync.
export const restartingOverlay = ref(false)

// Module-level pending settings category deep-link — set by entry points that
// live OUTSIDE the settings tab (e.g. AppHeader's "more appearance options"
// button) and consumed by SettingsPage when the settings tab becomes active.
// Module-level (not per-instance) because the SettingsPage may not even be
// mounted yet when the request is made — the TabPanel mounts it lazily on the
// first visit — so the request must survive until the page mounts/activates.
const pendingSettingsCategory = ref<string | null>(null)

/** Request a deep-link into a settings category (from outside the settings tab). */
export function setPendingSettingsCategory(categoryId: string) {
  pendingSettingsCategory.value = categoryId
}

/**
 * Consume (and clear) the pending settings category request.
 * Returns the category id or null when nothing is pending.
 */
export function consumePendingSettingsCategory(): string | null {
  const id = pendingSettingsCategory.value
  pendingSettingsCategory.value = null
  return id
}

/**
 * Reactive pending category ref — SettingsPage watches it so a deep-link is
 * honored even when the settings tab is already active and switchTab() no-ops.
 */
export { pendingSettingsCategory }

/** Check all registered guards. Returns true if all guards allow reset. */
function checkAllGuards(): boolean {
  for (const guard of guards.values()) {
    if (!guard()) return false
  }
  return true
}

/**
 * Shared composable for settings page navigation, restart logic, and state.
 * Used by SettingsPage.vue to avoid code duplication.
 */
export function useSettingsNavigation() {
  const { t } = useI18n()
  const { loadConfig, restartServer } = useSettingsConfig()
  const toast = useToast()

  const navStack = ref<string[]>([])
  const restartDialogVisible = ref(false)
  const changedColdFields = ref<string[]>([])
  const needsRestart = ref(false)
  const restarting = ref(false)

  // Track the poll timer for cleanup
  let pollTimer: ReturnType<typeof setInterval> | null = null

  const currentCategory = ref<string | null>(null)

  // Update currentCategory whenever navStack changes
  function pushNav(categoryId: string) {
    navStack.value.push(categoryId)
    currentCategory.value = categoryId
  }

  function popNav() {
    if (navStack.value.length > 0) {
      navStack.value.pop()
      currentCategory.value = navStack.value.length > 0
        ? navStack.value[navStack.value.length - 1]
        : null
    }
  }

  /**
   * Truncate the stack to its first `depth` entries (0 = back to the index).
   *
   * This is what a breadcrumb crumb click does: jumping to an ancestor is a
   * multi-level pop, not a single one. Depth is clamped to the current stack
   * length so an out-of-range crumb can never *push* the user deeper.
   */
  function truncateNav(depth: number) {
    const target = Math.max(0, Math.min(depth, navStack.value.length))
    if (target === navStack.value.length) return
    // Mutate in place (like push/pop) so the array identity is preserved for
    // any holder that captured the ref.
    navStack.value.splice(target)
    currentCategory.value = target > 0 ? navStack.value[target - 1] : null
  }

  function resetState() {
    if (!checkAllGuards()) return  // at least one guard says don't reset
    navStack.value = []
    currentCategory.value = null
    needsRestart.value = false
    restarting.value = false
    restartingOverlay.value = false
    restartDialogVisible.value = false
    changedColdFields.value = []
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function handleRestartNeeded(fields: string[]) {
    changedColdFields.value = fields
    needsRestart.value = true
    restartDialogVisible.value = true
  }

  function pollUntilServerUp() {
    restartingOverlay.value = true
    let attempts = 0

    pollTimer = setInterval(async () => {
      attempts++
      if (attempts >= MAX_POLL_ATTEMPTS) {
        clearInterval(pollTimer!)
        pollTimer = null
        restartingOverlay.value = false
        restarting.value = false
        toast.show(t('settings.restartTimeout'), { icon: '⚠️', type: 'error', duration: 5000 })
        return
      }

      try {
        const resp = await fetch('/api/agents', { method: 'GET' })
        if (resp.ok) {
          clearInterval(pollTimer!)
          pollTimer = null
          restartingOverlay.value = false
          restarting.value = false
          window.location.reload()
        }
      } catch {
        // Server not up yet, keep polling
      }
    }, POLL_INTERVAL_MS)
  }

  async function handleRestart() {
    restartDialogVisible.value = false
    restarting.value = true
    try {
      await restartServer()
      needsRestart.value = false
      // Server is shutting down — start polling until it comes back
      pollUntilServerUp()
    } catch {
      restarting.value = false
    }
  }

  // Cleanup on unmount
  onUnmounted(() => {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  })

  return {
    t,
    loadConfig,
    restartServer,
    navStack,
    currentCategory,
    pushNav,
    popNav,
    truncateNav,
    resetState,
    restartDialogVisible,
    changedColdFields,
    needsRestart,
    restarting,
    restartingOverlay,
    handleRestartNeeded,
    handleRestart,
    registerGuard,
    unregisterGuard,
    checkAllGuards,
  }
}
