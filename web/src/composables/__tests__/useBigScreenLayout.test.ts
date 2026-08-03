import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  useBigScreenLayout,
  getBigScreenState,
  switchLeftTab,
  setSplitRatio,
  resetBigScreenState,
  registerBigScreenCallbacks,
  _setBigScreenForTest,
  _resetForTest,
  resolveLeftTabOnEnter,
  resolveActivePaneOnEnter,
  setActivePane,
  computeIsBigScreen,
  BIG_SCREEN_DOCK_TABS,
  LEFT_TAB_KEY,
  SPLIT_RATIO_KEY,
} from '@/composables/useBigScreenLayout'

beforeEach(() => {
  _resetForTest()
  resetBigScreenState()
  localStorage.clear()
})

describe('computeIsBigScreen', () => {
  it('desktop: CSS width ≥1024 → big screen', () => {
    expect(computeIsBigScreen(1280, 800, 1)).toBe(true)
    expect(computeIsBigScreen(1024, 768, 1)).toBe(true)
  })

  it('high-DPR tablet landscape (CSS <1024) → big screen via physical width', () => {
    // 2400 physical px at DPR 2.5 → CSS 960 (the user's tablet case)
    expect(computeIsBigScreen(960, 600, 2.5)).toBe(true)
  })

  it('high-DPR phone portrait → NOT big screen (landscape gate)', () => {
    // 430×3 = 1290 physical ≥1280, but portrait
    expect(computeIsBigScreen(430, 900, 3)).toBe(false)
  })

  it('high-DPR phone landscape → big screen (wide physical viewport)', () => {
    expect(computeIsBigScreen(844, 390, 3)).toBe(true)
  })

  it('small CSS width and small physical width → NOT big screen', () => {
    expect(computeIsBigScreen(800, 1280, 1)).toBe(false)
    expect(computeIsBigScreen(360, 800, 2)).toBe(false) // 720 physical
  })
})

describe('useBigScreenLayout', () => {
  it('initializes big-screen from the viewport and does not throw', () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 800 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 1280 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 1 })
    _resetForTest()
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

  it('fresh init with no persisted ratio keeps splitRatio at 0.5', () => {
    const { splitRatio } = useBigScreenLayout()
    expect(splitRatio.value).toBe(0.5)
  })

  it('big-screen mode makes getBigScreenState expose chat + leftTab as active tabs', () => {
    const { isBigScreen, leftTab } = getBigScreenState()
    _setBigScreenForTest(true)
    switchLeftTab('terminal')
    expect(isBigScreen.value).toBe(true)
    expect(leftTab.value).toBe('terminal')
  })
})

describe('resolveLeftTabOnEnter', () => {
  it('adopts a non-chat activeTab as the left tab', () => {
    expect(resolveLeftTabOnEnter('terminal', 'browse')).toBe('terminal')
    expect(resolveLeftTabOnEnter('settings', 'browse')).toBe('settings')
  })

  it('keeps persisted leftTab when activeTab is chat', () => {
    expect(resolveLeftTabOnEnter('chat', 'settings')).toBe('settings')
    expect(resolveLeftTabOnEnter('chat', 'browse')).toBe('browse')
  })

  it('falls back to browse for invalid persisted value', () => {
    expect(resolveLeftTabOnEnter('chat', 'not-a-tab')).toBe('browse')
  })
})

describe('activePane focus tracking', () => {
  it('resolveActivePaneOnEnter: chat → right, any left tab → left', () => {
    expect(resolveActivePaneOnEnter('chat')).toBe('right')
    expect(resolveActivePaneOnEnter('browse')).toBe('left')
    expect(resolveActivePaneOnEnter('terminal')).toBe('left')
  })

  it('setActivePane updates the shared activePane ref', () => {
    const { activePane } = useBigScreenLayout()
    expect(activePane.value).toBe('right')
    setActivePane('left')
    expect(activePane.value).toBe('left')
    setActivePane('right')
    expect(activePane.value).toBe('right')
  })

  it('resetBigScreenState resets activePane to right', () => {
    setActivePane('left')
    resetBigScreenState()
    const { activePane } = useBigScreenLayout()
    expect(activePane.value).toBe('right')
  })
})

describe('useBigScreenLayout viewport wiring', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('activates for a high-DPR landscape tablet and reacts to resize/rotation', async () => {
    vi.resetModules()
    // 960 CSS × 2.5 = 2400 physical px, landscape → big screen
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 960 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 600 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 2.5 })
    const mod = await import('@/composables/useBigScreenLayout')
    expect(mod.getBigScreenState().isBigScreen.value).toBe(true)

    // Rotate to portrait → back to single column
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 600 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 960 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getBigScreenState().isBigScreen.value).toBe(false)

    // Phone portrait, high DPR → stays single column
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 430 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 900 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 3 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getBigScreenState().isBigScreen.value).toBe(false)
  })
})
