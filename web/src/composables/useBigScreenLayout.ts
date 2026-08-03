import { ref } from 'vue'
import { normalizeRatio } from '@/utils/splitRatio'

export const BIG_SCREEN_MIN_WIDTH = 1024
export const LEFT_TAB_KEY = 'clawbench-bigscreen-left-tab'
export const SPLIT_RATIO_KEY = 'clawbench-bigscreen-split-ratio'
export const BIG_SCREEN_DOCK_TABS = ['browse', 'history', 'proxy', 'terminal', 'tasks', 'settings']

const isBigScreen = ref(false)
const leftTab = ref<string>('browse')
const splitRatio = ref(0.5)
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
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    const mql = window.matchMedia(`(min-width: ${BIG_SCREEN_MIN_WIDTH}px)`)
    isBigScreen.value = mql.matches
    const onChange = (e: MediaQueryListEvent) => { isBigScreen.value = e.matches }
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', onChange)
    } else if (typeof (mql as { addListener?: unknown }).addListener === 'function') {
      ;(mql as { addListener: (cb: (e: MediaQueryListEvent) => void) => void }).addListener(onChange)
    }
  }
}

/** Returns the shared big-screen state refs (initializes once). */
export function useBigScreenLayout() {
  initBigScreen()
  return { isBigScreen, leftTab, splitRatio }
}

/** Ref access for useTabDrawer (init once, return only the refs it needs). */
export function getBigScreenState() {
  initBigScreen()
  return { isBigScreen, leftTab }
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
