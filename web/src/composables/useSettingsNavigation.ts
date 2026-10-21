import { ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

const MAX_POLL_ATTEMPTS = 60 // 2 minutes at 2s interval
const POLL_INTERVAL_MS = 2000

/**
 * How many consecutive polls may keep seeing the SAME instance id before we
 * conclude the restart never happened.
 *
 * A real restart always produces a gap: shutdown step 4 (`srv.Shutdown`) closes
 * the listener before the replacement can bind, so the old process cannot keep
 * answering — in the worst case it serves for the stream-finalize grace (~5s)
 * and then the listener is gone. 15 polls (30s) is comfortably past that, and
 * still well under the overall timeout so the user gets the actionable message
 * ("the server did not restart") rather than a generic "did not come back".
 */
const STALE_INSTANCE_LIMIT = 15

/**
 * Upper bound on the pre-restart identity probe. Short: it runs with the
 * dialog already closed, and its only job is to give the poller a baseline.
 */
const INSTANCE_PROBE_TIMEOUT_MS = 3000

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

  /**
   * Return from a detail page (project:<id> / agents:<id>) to its list.
   *
   * The detail is a stack entry pushed ON TOP of its list, so returning is a
   * POP, not a push. Using pushNav here was a real bug: after deleting a
   * project the stack went ['project','project:42'] → push 'project' →
   * ['project','project:42','project'], so the breadcrumb read
   * 设置 › 项目 › 项目详情 › 项目.
   *
   * A deep-linked detail (stack = ['project:42'], no list beneath) has nothing
   * to pop, so the parent is pushed instead. Both paths land the parent
   * category on top.
   */
  function returnToCategory(categoryId: string) {
    const stack = navStack.value
    const top = stack[stack.length - 1]
    // Drop the trailing detail entry for this category, if present.
    if (top && top.startsWith(categoryId + ':')) stack.pop()
    // Deep-linked detail (no list beneath) → put the list on top.
    if (stack[stack.length - 1] !== categoryId) stack.push(categoryId)
    currentCategory.value = stack[stack.length - 1] ?? null
  }

  /**
   * Replace the current top stack entry in place (no depth change).
   *
   * Used when a detail page hands off to a sibling detail page of the same
   * kind — e.g. duplicating an agent navigates from agents:<source> to
   * agents:<new>. Pushing would grow the breadcrumb into
   * 设置 › 源智能体 › 副本, so the entry is swapped instead.
   */
  function replaceTopNav(categoryId: string) {
    const stack = navStack.value
    if (stack.length === 0) {
      stack.push(categoryId)
    } else {
      stack[stack.length - 1] = categoryId
    }
    currentCategory.value = categoryId
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

  /**
   * Poll until the server answers from a DIFFERENT process, then reload.
   *
   * `beforeId` is the instance id observed before the restart was requested.
   * Requiring it to change is what makes this reliable:
   *
   *  - The outgoing process keeps serving normally until `srv.Shutdown` (after
   *    the stream-finalize grace), so a poll firing 2s after the request hits
   *    the OLD process. Accepting any 200 therefore reloaded the page onto the
   *    same build and looked like "the restart did nothing".
   *  - The version string cannot be the identity: restarting without upgrading
   *    legitimately keeps the version.
   *
   * If the id never changes we report a failure instead of reloading.
   */
  function pollUntilServerUp(beforeId: string | null) {
    restartingOverlay.value = true
    let attempts = 0
    let staleInstancePolls = 0

    pollTimer = setInterval(async () => {
      attempts++
      if (attempts >= MAX_POLL_ATTEMPTS) {
        finishPolling()
        toast.show(t('settings.restartTimeout'), { icon: 'warning', type: 'error', duration: 5000 })
        return
      }

      try {
        const resp = await fetch('/api/server/instance', { method: 'GET' })
        if (!resp.ok) {
          // The endpoint itself is missing (404 from an older server that the
          // new frontend is talking to, e.g. disk-mode frontend + stale embed).
          // There is no identity to compare, so we cannot prove the process
          // changed — reloading here is exactly the original bug. Report it
          // instead of pretending the restart worked.
          if (!beforeId) {
            finishPolling()
            toast.show(t('settings.restartNotApplied'), { icon: 'warning', type: 'error', duration: 8000 })
          }
          return
        }
        const data = await resp.json().catch(() => null) as { instance_id?: string } | null
        const id = data?.instance_id ?? ''

        // No baseline (the pre-restart probe failed or timed out): fall back to
        // accepting the first successful answer, as before. Weaker, but the
        // probe only fails when the server was already unreachable — in which
        // case reaching it again means it did come back.
        if (!beforeId) {
          finishPolling()
          window.location.reload()
          return
        }

        if (id && id !== beforeId) {
          finishPolling()
          window.location.reload()
          return
        }

        // Same process still answering.
        staleInstancePolls++
        if (staleInstancePolls >= STALE_INSTANCE_LIMIT) {
          finishPolling()
          toast.show(t('settings.restartNotApplied'), { icon: 'warning', type: 'error', duration: 8000 })
        }
      } catch {
        // Server not up yet, keep polling
      }
    }, POLL_INTERVAL_MS)
  }

  function finishPolling() {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    restartingOverlay.value = false
    restarting.value = false
  }

  /**
   * Read the current process identity, or null when it cannot be read.
   *
   * Bounded: the caller awaits this with the dialog already closed and the
   * restart button disabled, so a server that accepts the connection but never
   * answers would leave the UI stuck with no way out. A timeout is treated as
   * "no baseline", which the poller already handles.
   */
  async function fetchInstanceId(): Promise<string | null> {
    try {
      const resp = await fetch('/api/server/instance', {
        method: 'GET',
        signal: AbortSignal.timeout(INSTANCE_PROBE_TIMEOUT_MS),
      })
      if (!resp.ok) return null
      const data = await resp.json() as { instance_id?: string }
      return data.instance_id ?? null
    } catch {
      return null
    }
  }

  async function handleRestart() {
    restartDialogVisible.value = false
    restarting.value = true

    // Snapshot the identity BEFORE asking, so a later poll can prove the
    // process actually changed. Taken here (not at poll time) because the
    // request itself may be served by the process that is about to die.
    const beforeId = await fetchInstanceId()

    try {
      await restartServer()
      needsRestart.value = false
      // Server is shutting down — start polling until a NEW process answers
      pollUntilServerUp(beforeId)
    } catch (err) {
      // The server refused (or could not start) the restart: it is still
      // running the old build. Say so instead of silently leaving the user to
      // discover it on the next reload.
      finishPolling()
      // The message from apiPost is ALREADY localized and already carries the
      // "restart failed" prefix (the handler returns the localized text in
      // `error`, e.g. "重启失败：running binary is not accessible …"). Wrapping
      // it in restartFailedDetail again produced
      // "重启失败：重启失败：…" — so use it verbatim, and only synthesize a
      // message when there is none.
      const detail = err instanceof Error ? err.message.trim() : ''
      toast.show(
        detail || t('settings.restartFailed'),
        { icon: 'warning', type: 'error', duration: 8000 },
      )
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
    returnToCategory,
    replaceTopNav,
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
