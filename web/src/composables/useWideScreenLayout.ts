import { ref } from 'vue'
import { DEFAULT_RATIO, normalizeRatio } from '@/utils/splitRatio'
import { appLog } from '@/utils/appLog'
import { DOCK_TAB_IDS, WIDE_SCREEN_PRIMARY_TABS, isDockTabId, type DockTabId } from '@/composables/dockTabs'
import { DEFAULT_LEFT_COLLAPSED, readPanelState, writePanelState } from '@/utils/panelState'

const TAG = 'WideScreenLayout'

/**
 * Wide-screen (two-column) threshold, in CSS pixels.
 *
 * 960 rather than 1024: the original 1024 existed only so that a 2400-physical-px
 * tablet at DPR 2.5 (→ 960 CSS px) could be rescued by a separate physical-width
 * clause. Once the criterion is pure CSS width, that rescue is unnecessary — 960
 * CSS px is itself enough room for two columns — so folding the tablet case into
 * the threshold lets the physical clause go entirely.
 *
 * Why CSS px and nothing else: the layout can only ever use CSS pixels, so that
 * is the only unit in which "is there room for a second column?" has a meaning.
 * The removed physical-width clause measured device glass, which produced
 * self-contradictory results — a 698 CSS px landscape phone was "wide" while a
 * 960 CSS px desktop window was not — and it also could not be expressed in CSS
 * (media queries see CSS px only), so the JS signal and any CSS media query would
 * disagree at the boundary. UI scale (`html { zoom }`) does not affect this: it
 * changes neither `innerWidth` nor media-query evaluation (measured).
 */
export const WIDE_SCREEN_MIN_WIDTH = 960
// The active left tab used to be persisted here under a single global key
// ('clawbench-widescreen-left-tab'). That gave the tab no project dimension, so
// switching to project B overwrote project A's tab and switching back restored
// B's. Persistence now lives in utils/panelState.ts, keyed per project, and
// App.vue applies it after the workspace is restored. This module
// deliberately keeps no storage for `leftTab` — a second writer here would race
// the per-project one.
/**
 * Validation whitelist for the wide-screen dock. Derived from the DOCK_TABS
 * registry (composables/dockTabs.ts) so it can never drift from what the dock
 * renders — the dock renders these ids, and these ids are switchable.
 *
 * Historically this was a hand-written array living apart from the render list;
 * adding a tab without updating it produced a button that did nothing on click.
 */
export const WIDE_SCREEN_DOCK_TABS: readonly DockTabId[] = DOCK_TAB_IDS

export { WIDE_SCREEN_PRIMARY_TABS }

/**
 * Visible tab order of the wide-screen vertical dock: fixed primary tabs first,
 * then the (already-filtered) secondary tabs. Used for rendering and for the
 * active-indicator position. Deterministic — never depends on runtime geometry.
 */
export function wideDockTabOrder(overflowTabs: readonly string[]): string[] {
  return [...WIDE_SCREEN_PRIMARY_TABS, ...overflowTabs]
}

/**
 * Wide-screen detection: active when the CSS viewport is at least
 * WIDE_SCREEN_MIN_WIDTH (960) CSS px.
 *
 * Takes only the CSS width. It used to also take screen dimensions and
 * devicePixelRatio for a physical-width fallback; that fallback is gone because
 * it measured the wrong thing (device glass, not layout space) and produced
 * self-contradictory results — see WIDE_SCREEN_MIN_WIDTH above.
 */
export function computeIsWideScreen(cssWidth: number): boolean {
  return cssWidth >= WIDE_SCREEN_MIN_WIDTH
}

const isWideScreen = ref(false)
const leftTab = ref<DockTabId>('browse')
const splitRatio = ref(DEFAULT_RATIO)
export const PANE_LEFT = 'left' as const
export const PANE_RIGHT = 'right' as const
export type ActivePane = typeof PANE_LEFT | typeof PANE_RIGHT

/** Wide-screen focus tracking: which pane the user is currently working in. */
const activePane = ref<ActivePane>(PANE_RIGHT)
/**
 * Wide-screen left pane collapsed state. When true, the left column is hidden
 * and the chat (right) pane takes the full width. Toggled by clicking the
 * currently-active dock tab again (VS Code-style).
 *
 * Starts collapsed (see DEFAULT_LEFT_COLLAPSED) and persists globally via the
 * panel-state registry, so a hidden column survives a reload. It is NOT
 * expanded implicitly on a tab change: `switchLeftTab` leaves it alone, and the
 * callers that represent a user switching tabs expand it explicitly. That
 * distinction is what keeps a programmatic restore (cold start, project switch)
 * from un-hiding a column the user had hidden.
 */
const leftCollapsed = ref(DEFAULT_LEFT_COLLAPSED)
/**
 * Wide-screen right (chat) pane collapsed state. When true, the chat pane is
 * hidden and the left pane takes the full width. Toggled by the chat button at
 * the bottom of the wide-screen vertical dock.
 */
const chatCollapsed = ref(false)
let initialized = false
let sideEffects: ((tab: DockTabId) => void) | null = null
let setActiveTab: ((tab: DockTabId) => void) | null = null

// Listener handles kept so re-init (test reset) can remove old listeners
// instead of accumulating duplicates on window / matchMedia.
let resizeListener: (() => void) | null = null
let mql: MediaQueryList | null = null
let mqlChangeListener: (() => void) | null = null

function initWideScreen() {
  if (initialized) return
  initialized = true
  // All three are global panel states read through the registry, which owns the
  // key names, encodings and defaults. `splitRatio` is normalized because the
  // registry stores a plain number and an out-of-range value (e.g. hand-edited
  // storage) must not produce a degenerate split.
  splitRatio.value = normalizeRatio(readPanelState<number>('splitRatio'))
  chatCollapsed.value = readPanelState<boolean>('chatCollapsed')
  leftCollapsed.value = readPanelState<boolean>('leftCollapsed')
  if (typeof window !== 'undefined') {
    const recompute = () => {
      isWideScreen.value = computeIsWideScreen(window.innerWidth)
    }
    recompute()
    // Remove any previously registered listeners (a prior init in this
    // process, e.g. after a test reset, may have added them).
    if (resizeListener) window.removeEventListener('resize', resizeListener)
    if (mql && mqlChangeListener) {
      if (typeof mql.removeEventListener === 'function') mql.removeEventListener('change', mqlChangeListener)
      else if (typeof (mql as { removeListener?: unknown }).removeListener === 'function') (mql as { removeListener: (cb: () => void) => void }).removeListener(mqlChangeListener)
    }
    // Viewport resize covers rotation, window resize and browser-zoom DPR changes.
    resizeListener = recompute
    window.addEventListener('resize', recompute)
    if (typeof window.matchMedia === 'function') {
      mql = window.matchMedia(`(min-width: ${WIDE_SCREEN_MIN_WIDTH}px)`)
      mqlChangeListener = () => recompute()
      if (typeof mql.addEventListener === 'function') {
        mql.addEventListener('change', mqlChangeListener)
      } else if (typeof (mql as { addListener?: unknown }).addListener === 'function') {
        ;(mql as { addListener: (cb: () => void) => void }).addListener(mqlChangeListener)
      }
    }
  }
}

/** Returns the shared wide-screen state refs (initializes once). */
export function useWideScreenLayout() {
  initWideScreen()
  return { isWideScreen, leftTab, splitRatio, activePane, leftCollapsed, chatCollapsed }
}

/** Ref access for useTabDrawer (init once, return only the refs it needs). */
export function getWideScreenState() {
  initWideScreen()
  return { isWideScreen, leftTab }
}

/** Record which pane the user is currently working in (drives focus-aware shortcuts). */
export function setActivePane(pane: 'left' | 'right') {
  activePane.value = pane
}

/** Collapse (true) or expand (false) the wide-screen left pane. Persisted globally. */
export function setLeftCollapsed(collapsed: boolean) {
  // Mutual-exclusion guard: the left pane cannot be collapsed while the right
  // (chat) pane is hidden — otherwise both panes would be display:none and the
  // content area would go blank. When the chat pane is hidden the left pane
  // already takes the full width, so collapsing it is a no-op anyway.
  if (collapsed && chatCollapsed.value) return
  leftCollapsed.value = collapsed
  writePanelState('leftCollapsed', collapsed)
}

/**
 * Collapse (true, hide chat) or expand (false, show chat) the wide-screen right
 * pane. Persisted so the chat stays hidden across reloads.
 */
export function setChatCollapsed(collapsed: boolean) {
  chatCollapsed.value = collapsed
  // Mutual-exclusion guard: hiding the chat pane forces the left pane back open
  // so there is always at least one visible pane (see setLeftCollapsed). Goes
  // through the setter so the reopen is persisted too.
  if (collapsed) setLeftCollapsed(false)
  writePanelState('chatCollapsed', collapsed)
}

/**
 * Focus continuity on entering wide-screen: if the user was on chat, the right
 * pane (chat) is focused; otherwise they were in a left-column tab.
 */
export function resolveActivePaneOnEnter(currentActiveTab: string): 'left' | 'right' {
  return currentActiveTab === 'chat' ? 'right' : 'left'
}

/**
 * Whether the chat panel is actually on screen right now — i.e. the user can
 * see its content. Used to decide "the user is already looking at this session,
 * so don't notify".
 *
 * Takes plain values rather than reading the refs itself. That is deliberate:
 * the caller is `<script setup>`, where a ref is NOT auto-unwrapped, so
 * `isWideScreen || ...` is always truthy and silently makes every session look
 * "on screen". Requiring explicit fields makes that mistake a type error.
 *
 * Two independent ways the panel can be hidden:
 *  - narrow layout: only the active tab is rendered, so any non-chat tab hides it
 *  - wide layout: the chat column is docked, but collapsing it (SplitView
 *    `rightCollapsed`) gives it display:none — including while activeTab still
 *    reads "chat"
 */
export function isChatPanelVisible(state: {
  isWideScreen: boolean
  chatCollapsed: boolean
  activeTab: string
}): boolean {
  if (!state.isWideScreen) return state.activeTab === 'chat'
  return !state.chatCollapsed
}

export function registerWideScreenCallbacks(opts: { sideEffects?: (tab: DockTabId) => void; setActiveTab?: (tab: DockTabId) => void }) {
  sideEffects = opts.sideEffects ?? null
  setActiveTab = opts.setActiveTab ?? null
}

/**
 * Switch the wide-screen left column tab. Writes activeTab + side-effects via
 * callbacks; does NOT call onTabSwitch.
 *
 * Does NOT touch `leftCollapsed`. It used to expand the column unconditionally,
 * which broke "the user hid the column": restoring a tab programmatically (cold
 * start, project switch) ran through here and un-hid it. Callers that represent
 * a user switching tabs must expand explicitly — see `switchTab`'s wide branch
 * and `handleWideDockTabClick`.
 *
 * Does not persist: the tab is remembered per project via the panel-state
 * registry, driven from App.vue's watcher on the active panel tab.
 *
 * Rejects unknown tabs loudly: a tab that the dock renders but that is not in
 * DOCK_TABS would otherwise be a silently dead button (the original forge bug
 * — click did nothing, with no state change, error or log). The whitelist is
 * derived from the same registry the dock renders from, so this should be
 * unreachable in practice; the log exists to make any future divergence
 * visible on the first click instead of being reported as "nothing happens".
 */
export function switchLeftTab(tab: string) {
  if (!isDockTabId(tab)) {
    appLog.w(TAG, `switchLeftTab ignored unknown tab "${tab}" — is it missing from DOCK_TABS?`)
    return
  }
  if (leftTab.value === tab) return
  leftTab.value = tab
  setActiveTab?.(tab)
  sideEffects?.(tab)
}

/**
 * Q1A continuity rule: entering wide-screen mode adopts the current narrow-mode
 * tab as the left column tab when it is a non-chat tab; otherwise keeps the
 * current/default leftTab.
 */
export function resolveLeftTabOnEnter(currentActiveTab: string, currentLeftTab: string): DockTabId {
  if (currentActiveTab !== 'chat' && isDockTabId(currentActiveTab)) return currentActiveTab
  return isDockTabId(currentLeftTab) ? currentLeftTab : 'browse'
}

/** Normalize + persist the split ratio (persistence owned here, not in SplitView). */
export function setSplitRatio(ratio: number) {
  splitRatio.value = normalizeRatio(ratio)
  writePanelState('splitRatio', splitRatio.value)
}

export function resetWideScreenState() {
  leftTab.value = 'browse'
  splitRatio.value = DEFAULT_RATIO
  isWideScreen.value = false
  activePane.value = 'right'
  leftCollapsed.value = DEFAULT_LEFT_COLLAPSED
  chatCollapsed.value = false
  sideEffects = null
  setActiveTab = null
}

/** Test hooks — do not use in production code. */
export function _setWideScreenForTest(val: boolean) {
  isWideScreen.value = val
}
export function _resetForTest() {
  initialized = false
  resetWideScreenState()
}
