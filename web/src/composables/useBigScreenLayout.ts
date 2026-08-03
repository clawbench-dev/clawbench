import { ref } from 'vue'
import { normalizeRatio } from '@/utils/splitRatio'

export const BIG_SCREEN_MIN_WIDTH = 1024
// Physical (device-pixel) width threshold for the big-screen layout. CSS width
// alone can miss high-resolution tablets whose devicePixelRatio shrinks the CSS
// viewport below 1024 (e.g. 2400 physical px at DPR 2.5 → 960 CSS px).
export const BIG_SCREEN_MIN_PHYSICAL_WIDTH = 1280
export const LEFT_TAB_KEY = 'clawbench-bigscreen-left-tab'
export const SPLIT_RATIO_KEY = 'clawbench-bigscreen-split-ratio'
export const BIG_SCREEN_DOCK_TABS = ['browse', 'history', 'proxy', 'terminal', 'tasks', 'settings']

/**
 * Big-screen detection. Active when the CSS viewport is ≥1024px (desktop), or
 * when the device's physical width (CSS width × devicePixelRatio) is ≥1280px
 * AND the viewport is landscape. The landscape gate keeps high-DPR phones in
 * portrait (e.g. 430×3 = 1290) from accidentally splitting.
 */
export function computeIsBigScreen(cssWidth: number, cssHeight: number, devicePixelRatio: number): boolean {
  const physicalWidth = cssWidth * (devicePixelRatio || 1)
  return cssWidth >= BIG_SCREEN_MIN_WIDTH
    || (physicalWidth >= BIG_SCREEN_MIN_PHYSICAL_WIDTH && cssWidth > cssHeight)
}

const isBigScreen = ref(false)
const leftTab = ref<string>('browse')
const splitRatio = ref(0.5)
/** Big-screen focus tracking: which pane the user is currently working in. */
const activePane = ref<'left' | 'right'>('right')
let initialized = false
let sideEffects: ((tab: string) => void) | null = null
let setActiveTab: ((tab: string) => void) | null = null

function readPersistedLeftTab(): string {
  try {
    const v = localStorage.getItem(LEFT_TAB_KEY)
    if (v && BIG_SCREEN_DOCK_TABS.includes(v)) return v
  } catch {
    // localStorage may throw in restricted environments — fall through to default
  }
  return 'browse'
}

function initBigScreen() {
  if (initialized) return
  initialized = true
  leftTab.value = readPersistedLeftTab()
  try {
    const stored = localStorage.getItem(SPLIT_RATIO_KEY)
    if (stored !== null) {
      const raw = Number(stored)
      if (Number.isFinite(raw)) splitRatio.value = normalizeRatio(raw)
    }
  } catch {
    // ignore
  }
  if (typeof window !== 'undefined') {
    const recompute = () => {
      isBigScreen.value = computeIsBigScreen(
        window.innerWidth,
        window.innerHeight,
        window.devicePixelRatio || 1,
      )
    }
    recompute()
    // Viewport resize covers rotation, window resize and browser-zoom DPR changes.
    window.addEventListener('resize', recompute)
    if (typeof window.matchMedia === 'function') {
      const mql = window.matchMedia(`(min-width: ${BIG_SCREEN_MIN_WIDTH}px)`)
      const onChange = () => recompute()
      if (typeof mql.addEventListener === 'function') {
        mql.addEventListener('change', onChange)
      } else if (typeof (mql as { addListener?: unknown }).addListener === 'function') {
        ;(mql as { addListener: (cb: () => void) => void }).addListener(onChange)
      }
    }
  }
}

/** Returns the shared big-screen state refs (initializes once). */
export function useBigScreenLayout() {
  initBigScreen()
  return { isBigScreen, leftTab, splitRatio, activePane }
}

/** Ref access for useTabDrawer (init once, return only the refs it needs). */
export function getBigScreenState() {
  initBigScreen()
  return { isBigScreen, leftTab }
}

/** Record which pane the user is currently working in (drives focus-aware shortcuts). */
export function setActivePane(pane: 'left' | 'right') {
  activePane.value = pane
}

/**
 * Focus continuity on entering big-screen: if the user was on chat, the right
 * pane (chat) is focused; otherwise they were in a left-column tab.
 */
export function resolveActivePaneOnEnter(currentActiveTab: string): 'left' | 'right' {
  return currentActiveTab === 'chat' ? 'right' : 'left'
}

export function registerBigScreenCallbacks(opts: { sideEffects?: (tab: string) => void; setActiveTab?: (tab: string) => void }) {
  sideEffects = opts.sideEffects ?? null
  setActiveTab = opts.setActiveTab ?? null
}

/** Switch the big-screen left column tab. Writes activeTab + side-effects via callbacks; does NOT call onTabSwitch. */
export function switchLeftTab(tab: string) {
  if (!BIG_SCREEN_DOCK_TABS.includes(tab)) return
  if (leftTab.value === tab) return
  leftTab.value = tab
  try {
    localStorage.setItem(LEFT_TAB_KEY, tab)
  } catch {
    // ignore
  }
  setActiveTab?.(tab)
  sideEffects?.(tab)
}

/**
 * Q1A continuity rule: entering big-screen mode adopts the current narrow-mode
 * tab as the left column tab when it is a non-chat tab; otherwise keeps the
 * persisted/default leftTab.
 */
export function resolveLeftTabOnEnter(currentActiveTab: string, persistedLeftTab: string): string {
  if (currentActiveTab !== 'chat' && BIG_SCREEN_DOCK_TABS.includes(currentActiveTab)) return currentActiveTab
  return BIG_SCREEN_DOCK_TABS.includes(persistedLeftTab) ? persistedLeftTab : 'browse'
}

/** Normalize + persist the split ratio (persistence owned here, not in SplitView). */
export function setSplitRatio(ratio: number) {
  splitRatio.value = normalizeRatio(ratio)
  try {
    localStorage.setItem(SPLIT_RATIO_KEY, String(splitRatio.value))
  } catch {
    // ignore
  }
}

export function resetBigScreenState() {
  leftTab.value = 'browse'
  splitRatio.value = 0.5
  isBigScreen.value = false
  activePane.value = 'right'
  sideEffects = null
  setActiveTab = null
}

/** Test hooks — do not use in production code. */
export function _setBigScreenForTest(val: boolean) {
  isBigScreen.value = val
}
export function _resetForTest() {
  initialized = false
  resetBigScreenState()
}
