import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  useBigScreenLayout,
  getBigScreenState,
  switchLeftTab,
  setSplitRatio,
  resetBigScreenState,
  registerBigScreenCallbacks,
  _setBigScreenForTest,
  _resetForTest,
  BIG_SCREEN_DOCK_TABS,
  LEFT_TAB_KEY,
  SPLIT_RATIO_KEY,
} from '@/composables/useBigScreenLayout'

beforeEach(() => {
  _resetForTest()
  resetBigScreenState()
  localStorage.clear()
})

describe('useBigScreenLayout', () => {
  it('matchMedia absent → isBigScreen stays false and does not throw', () => {
    const { isBigScreen } = useBigScreenLayout()
    expect(isBigScreen.value).toBe(false)
  })

  it('leftTab defaults to browse and is clamped to allowed tabs', () => {
    const { leftTab } = useBigScreenLayout()
    expect(leftTab.value).toBe('browse')
    expect(BIG_SCREEN_DOCK_TABS).toContain(leftTab.value)
  })

  it('switchLeftTab ignores invalid tabs and persists valid ones', () => {
    switchLeftTab('terminal')
    expect(localStorage.getItem(LEFT_TAB_KEY)).toBe('terminal')
    switchLeftTab('not-a-tab' as never)
    expect(localStorage.getItem(LEFT_TAB_KEY)).toBe('terminal')
  })

  it('switchLeftTab runs registered side-effects and activeTab setter, but only on change', () => {
    const sideEffects = vi.fn()
    const setActiveTab = vi.fn()
    registerBigScreenCallbacks({ sideEffects, setActiveTab })

    switchLeftTab('tasks')
    expect(setActiveTab).toHaveBeenCalledWith('tasks')
    expect(sideEffects).toHaveBeenCalledWith('tasks')

    switchLeftTab('tasks') // same tab → early return
    expect(sideEffects).toHaveBeenCalledTimes(1)
  })

  it('setSplitRatio normalizes and persists', () => {
    setSplitRatio(1.9)
    expect(Number(localStorage.getItem(SPLIT_RATIO_KEY))).toBe(1)
    setSplitRatio(0.35)
    expect(Number(localStorage.getItem(SPLIT_RATIO_KEY))).toBeCloseTo(0.35)
  })

  it('restores persisted leftTab on init', () => {
    localStorage.setItem(LEFT_TAB_KEY, 'settings')
    _resetForTest()
    const { leftTab } = useBigScreenLayout()
    expect(leftTab.value).toBe('settings')
  })

  it('big-screen mode makes getBigScreenState expose chat + leftTab as active tabs', () => {
    const { isBigScreen, leftTab } = getBigScreenState()
    _setBigScreenForTest(true)
    switchLeftTab('terminal')
    expect(isBigScreen.value).toBe(true)
    expect(leftTab.value).toBe('terminal')
  })
})

describe('useBigScreenLayout matchMedia wiring', () => {
  it('reflects matchMedia matches and change events', async () => {
    vi.resetModules()
    const listeners: Array<(e: { matches: boolean }) => void> = []
    const mql = {
      matches: true,
      addEventListener: (_t: string, cb: (e: { matches: boolean }) => void) => { listeners.push(cb) },
      removeEventListener: vi.fn(),
    }
    vi.stubGlobal('matchMedia', vi.fn(() => mql))
    const mod = await import('@/composables/useBigScreenLayout')
    expect(mod.getBigScreenState().isBigScreen.value).toBe(true)
    mql.matches = false
    listeners.forEach((cb) => cb({ matches: false }))
    expect(mod.getBigScreenState().isBigScreen.value).toBe(false)
    vi.unstubAllGlobals()
  })
})
