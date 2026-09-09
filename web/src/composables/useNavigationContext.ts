import { ref, computed, type ComputedRef } from 'vue'
import type { FileScrollEntry } from '@/utils/fileScrollCache'

export type NavigationSurface = 'chat' | 'task' | 'file' | 'browse' | 'history'

export interface NavigationOrigin {
  surface: NavigationSurface
  tab: string
  label: string
  filePath?: string
  lineStart?: number
  lineEnd?: number
  viewMode?: string
  scrollTop?: number
  scrollEntry?: FileScrollEntry
  dirPath?: string
  sessionId?: string
}

export interface NavigationContextState {
  origin: NavigationOrigin | null
  active: boolean
  busy: boolean
}

export interface NavigationContextSnapshot {
  origin: NavigationOrigin | null
  active: boolean
}

const _origin = ref<NavigationOrigin | null>(null)
const _active = ref(false)
const _busy = ref(false)

const _hasOrigin = computed(() => _origin.value !== null)
const _originRef = computed(() => _origin.value)
const _originLabel = computed(() => _origin.value?.label ?? null)
const _busyRef = computed(() => _busy.value)

/**
 * File restoration data only has meaning for a file origin. Keeping it on
 * chat/task/browse/history origins lets returnToOrigin() reopen whatever file
 * happened to be cached globally, which is the source of the file-manager
 * back-navigation jumping to an unrelated file.
 */
function normalize(origin: NavigationOrigin): NavigationOrigin {
  if (origin.surface === 'file') return { ...origin }
  const next: NavigationOrigin = {
    surface: origin.surface,
    tab: origin.tab,
    label: origin.label,
  }
  if (origin.dirPath !== undefined) next.dirPath = origin.dirPath
  if (origin.sessionId !== undefined) next.sessionId = origin.sessionId
  return next
}

/**
 * The tab a jump origin should record as its return target.
 *
 * Normally that is the tab the user was on when the jump started. A chat jump
 * on a wide screen is the exception: the chat pane is the right column while
 * `activeTab` tracks the left column, so recording `activeTab` would tie the
 * origin to an unrelated left tab — the jump's own switchTab() would then
 * instantly settle the origin (no return banner) and a surviving origin would
 * return to the left tab instead of the conversation.
 */
export function resolveJumpOriginTab(
  surface: NavigationSurface,
  activeTab: string,
  wide: { isWideScreen: boolean; chatPaneActive: boolean },
): string {
  if (surface === 'chat' && wide.isWideScreen && wide.chatPaneActive) return 'chat'
  return activeTab
}

/**
 * Surfaces and tab ids overlap in spelling but never in meaning: a surface
 * describes where a jump came from (`task`, `file`), a tab id describes which
 * panel to activate (`tasks`, `view`). They are kept in two separate tables —
 * one table made it easy to add a surface and silently forget the tab it
 * activates.
 */
const SURFACE_TAB = new Map<NavigationSurface, string>([
  ['chat', 'chat'],
  ['task', 'tasks'],
  ['file', 'view'],
  ['browse', 'browse'],
  ['history', 'history'],
])

/** Tab ids that are already panel identifiers, so they map to themselves. */
const PANEL_TABS = new Set<string>([
  'chat',
  'tasks',
  'view',
  'browse',
  'history',
  'terminal',
  'proxy',
  'stats',
  'settings',
])

/**
 * Map a navigation surface or panel name to its corresponding tab identifier.
 *
 * Returns `null` for an unknown surface instead of silently falling back to
 * chat — a caller passing a surface we do not know about is a bug, and the
 * fallback used to hide it by dumping the user on an unrelated tab.
 */
export function surfaceToTab(surface: NavigationSurface | string): string | null {
  const tab = SURFACE_TAB.get(surface as NavigationSurface)
  if (tab !== undefined) return tab
  return PANEL_TABS.has(surface) ? surface : null
}

/** True when both origins describe the same return visit (same surface and tab). */
export function isSameVisit(a: NavigationOrigin | null, b: NavigationOrigin | null): boolean {
  if (!a || !b) return false
  return a.surface === b.surface && a.tab === b.tab
}

/**
 * Whether reaching `tab` by hand spends the pending origin.
 *
 * Normally yes — the user got back to where the jump started without Back, so
 * the return target is stale. A *suspended directory excursion* is the
 * exception: it parks a whole file visit, and its origin tab is `"view"`.
 * Switching to the file tab is not returning to the suspended file — the user
 * is usually just previewing another file from the directory they jumped to —
 * so the excursion must survive until the suspended file itself is reopened.
 */
export function shouldSettleOrigin(
  origin: NavigationOrigin | null,
  tab: string,
  state: { pending: boolean; currentFilePath?: string | null },
): boolean {
  if (!origin || origin.tab !== tab) return false
  if (state.pending && origin.filePath) {
    return state.currentFilePath === origin.filePath
  }
  return true
}

export function useNavigationContext() {
  function start(origin: NavigationOrigin): boolean {
    if (_active.value) {
      return false
    }
    _origin.value = normalize(origin)
    _active.value = true
    return true
  }

  /**
   * Force-replace the active origin. Needed when a new jump invalidates a
   * return target that was never consumed (the user left the jump flow by
   * other means) — otherwise Back drags them to an unrelated surface.
   */
  function replace(origin: NavigationOrigin): boolean {
    clear()
    return start(origin)
  }

  function setBusy(value: boolean): void {
    _busy.value = value
  }

  function consume(): NavigationOrigin | null {
    if (!_origin.value) {
      _active.value = false
      return null
    }
    const previous = _origin.value
    _origin.value = null
    _active.value = false
    return previous
  }

  function clear(): void {
    _origin.value = null
    _active.value = false
    _busy.value = false
  }

  function snapshot(): NavigationContextSnapshot {
    return {
      origin: _origin.value ? { ..._origin.value } : null,
      active: _active.value,
    }
  }

  function restore(state: NavigationContextSnapshot | null | undefined): void {
    if (!state) {
      clear()
      return
    }
    _origin.value = state.origin ? { ...state.origin } : null
    _active.value = state.active && state.origin !== null
    _busy.value = false
  }

  function resetForTesting(): void {
    clear()
  }

  return {
    start,
    replace,
    setBusy,
    hasOrigin: _hasOrigin as ComputedRef<boolean>,
    origin: _originRef as ComputedRef<NavigationOrigin | null>,
    originLabel: _originLabel as ComputedRef<string | null>,
    busy: _busyRef as ComputedRef<boolean>,
    consume,
    clear,
    snapshot,
    restore,
    resetForTesting,
  }
}
