import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  useWideScreenLayout,
  getWideScreenState,
  switchLeftTab,
  setSplitRatio,
  resetWideScreenState,
  registerWideScreenCallbacks,
  _setWideScreenForTest,
  _resetForTest,
  resolveLeftTabOnEnter,
  resolveActivePaneOnEnter,
  setActivePane,
  setLeftCollapsed,
  computeIsWideScreen,
  WIDE_SCREEN_DOCK_TABS,
  WIDE_SCREEN_LEFT_TAB_KEY,
  WIDE_SCREEN_SPLIT_RATIO_KEY,
  WIDE_SCREEN_PRIMARY_TABS,
  wideDockTabOrder,
} from '@/composables/useWideScreenLayout'

beforeEach(() => {
  _resetForTest()
  resetWideScreenState()
  localStorage.clear()
})

describe('computeIsWideScreen', () => {
  it('desktop: CSS width ≥1024 → wide screen', () => {
    expect(computeIsWideScreen(1280, 1280, 800, 1)).toBe(true)
    expect(computeIsWideScreen(1024, 1024, 768, 1)).toBe(true)
  })

  it('high-DPR tablet landscape (CSS <1024) → wide screen via physical width', () => {
    // 2400 physical px at DPR 2.5 → CSS 960 (the user's tablet case)
    expect(computeIsWideScreen(960, 960, 600, 2.5)).toBe(true)
  })

  it('high-DPR phone portrait → NOT wide screen (landscape gate)', () => {
    // 430×3 = 1290 physical ≥1280, but portrait
    expect(computeIsWideScreen(430, 430, 900, 3)).toBe(false)
  })

  it('high-DPR phone landscape → wide screen (wide physical viewport)', () => {
    expect(computeIsWideScreen(844, 844, 390, 3)).toBe(true)
  })

  it('small CSS width and small physical width → NOT wide screen', () => {
    expect(computeIsWideScreen(800, 800, 1280, 1)).toBe(false)
    expect(computeIsWideScreen(360, 360, 800, 2)).toBe(false) // 720 physical
  })

  it('keyboard opening on portrait tablet does NOT trigger wide screen', () => {
    // Portrait tablet: screen 960×1600, DPR 2.5 → physical width 2400 ≥ 1280
    // Without keyboard: cssWidth(960) < cssHeight(1600) → not wide
    // With keyboard: window.innerHeight shrinks to 900, but screen stays 960×1600
    // The landscape check uses screen dimensions, so it stays portrait
    expect(computeIsWideScreen(960, 960, 1600, 2.5)).toBe(false)
  })
})

describe('useWideScreenLayout', () => {
  it('initializes wide-screen from the viewport and does not throw', () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 800 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 1280 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 1 })
    _resetForTest()
    const { isWideScreen } = useWideScreenLayout()
    expect(isWideScreen.value).toBe(false)
  })

  it('leftTab defaults to browse and is clamped to allowed tabs', () => {
    const { leftTab } = useWideScreenLayout()
    expect(leftTab.value).toBe('browse')
    expect(WIDE_SCREEN_DOCK_TABS).toContain(leftTab.value)
  })

  it('switchLeftTab ignores invalid tabs and persists valid ones', () => {
    switchLeftTab('terminal')
    expect(localStorage.getItem(WIDE_SCREEN_LEFT_TAB_KEY)).toBe('terminal')
    switchLeftTab('not-a-tab' as never)
    expect(localStorage.getItem(WIDE_SCREEN_LEFT_TAB_KEY)).toBe('terminal')
  })

  it('view is a wide-screen dock tab and can be switched/persisted to', () => {
    expect(WIDE_SCREEN_DOCK_TABS).toContain('view')
    switchLeftTab('view')
    expect(localStorage.getItem(WIDE_SCREEN_LEFT_TAB_KEY)).toBe('view')
    const { leftTab } = useWideScreenLayout()
    expect(leftTab.value).toBe('view')
  })

  it('switchLeftTab runs registered side-effects and activeTab setter, but only on change', () => {
    const sideEffects = vi.fn()
    const setActiveTab = vi.fn()
    registerWideScreenCallbacks({ sideEffects, setActiveTab })

    switchLeftTab('tasks')
    expect(setActiveTab).toHaveBeenCalledWith('tasks')
    expect(sideEffects).toHaveBeenCalledWith('tasks')

    switchLeftTab('tasks') // same tab → early return
    expect(sideEffects).toHaveBeenCalledTimes(1)
  })

  it('setSplitRatio normalizes and persists', () => {
    setSplitRatio(1.9)
    expect(Number(localStorage.getItem(WIDE_SCREEN_SPLIT_RATIO_KEY))).toBe(1)
    setSplitRatio(0.35)
    expect(Number(localStorage.getItem(WIDE_SCREEN_SPLIT_RATIO_KEY))).toBeCloseTo(0.35)
  })

  it('restores persisted leftTab on init', () => {
    localStorage.setItem(WIDE_SCREEN_LEFT_TAB_KEY, 'settings')
    _resetForTest()
    const { leftTab } = useWideScreenLayout()
    expect(leftTab.value).toBe('settings')
  })

  it('fresh init with no persisted ratio keeps splitRatio at 0.5', () => {
    const { splitRatio } = useWideScreenLayout()
    expect(splitRatio.value).toBe(0.5)
  })

  it('wide-screen mode makes getWideScreenState expose chat + leftTab as active tabs', () => {
    const { isWideScreen, leftTab } = getWideScreenState()
    _setWideScreenForTest(true)
    switchLeftTab('terminal')
    expect(isWideScreen.value).toBe(true)
    expect(leftTab.value).toBe('terminal')
  })
})

describe('leftCollapsed (dock tab toggle)', () => {
  it('defaults to expanded (false)', () => {
    const { leftCollapsed } = useWideScreenLayout()
    expect(leftCollapsed.value).toBe(false)
  })

  it('setLeftCollapsed toggles the shared state', () => {
    const { leftCollapsed } = useWideScreenLayout()
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true)
    setLeftCollapsed(false)
    expect(leftCollapsed.value).toBe(false)
  })

  it('resetWideScreenState resets to expanded', () => {
    setLeftCollapsed(true)
    resetWideScreenState()
    const { leftCollapsed } = useWideScreenLayout()
    expect(leftCollapsed.value).toBe(false)
  })

  it('switchLeftTab expands the collapsed pane when switching to a different tab', () => {
    const { leftCollapsed } = useWideScreenLayout()
    switchLeftTab('history')
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true)
    // Switching to a different tab expands
    switchLeftTab('tasks')
    expect(leftCollapsed.value).toBe(false)
    // Re-clicking the same tab does NOT switch (stays collapsed) — toggle handled elsewhere
    setLeftCollapsed(true)
    switchLeftTab('tasks')
    expect(leftCollapsed.value).toBe(true)
  })
})

describe('resolveLeftTabOnEnter', () => {
  it('adopts a non-chat activeTab as the left tab', () => {
    expect(resolveLeftTabOnEnter('terminal', 'browse')).toBe('terminal')
    expect(resolveLeftTabOnEnter('settings', 'browse')).toBe('settings')
    expect(resolveLeftTabOnEnter('view', 'browse')).toBe('view')
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
    const { activePane } = useWideScreenLayout()
    expect(activePane.value).toBe('right')
    setActivePane('left')
    expect(activePane.value).toBe('left')
    setActivePane('right')
    expect(activePane.value).toBe('right')
  })

  it('resetWideScreenState resets activePane to right', () => {
    setActivePane('left')
    resetWideScreenState()
    const { activePane } = useWideScreenLayout()
    expect(activePane.value).toBe('right')
  })
})

describe('wideDockTabOrder', () => {
  it('puts the fixed primary tabs first, then the secondary tabs in given order', () => {
    const all = wideDockTabOrder(['tasks', 'terminal', 'proxy', 'settings'])
    expect(all).toEqual(['browse', 'view', 'history', 'tasks', 'terminal', 'proxy', 'settings'])
    expect(all).toEqual(WIDE_SCREEN_DOCK_TABS)
  })

  it('preserves secondary-tab order after filtering (terminal/proxy disabled)', () => {
    expect(wideDockTabOrder(['tasks', 'settings'])).toEqual(['browse', 'view', 'history', 'tasks', 'settings'])
    expect(wideDockTabOrder([])).toEqual(['browse', 'view', 'history'])
  })

  it('is deterministic regardless of any runtime geometry', () => {
    const order = wideDockTabOrder(['tasks', 'settings'])
    expect(order).toEqual(wideDockTabOrder(['tasks', 'settings']))
    // The whole visible dock never depends on measured space — regression guard
    // for the old height-measured overflow that collapsed tabs into a popup.
    expect(order).toHaveLength(WIDE_SCREEN_PRIMARY_TABS.length + 2)
  })
})

describe('useWideScreenLayout viewport wiring', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('activates for a high-DPR landscape tablet and reacts to resize/rotation', async () => {
    vi.resetModules()
    // 960 CSS × 2.5 = 2400 physical px, landscape → wide screen
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 960 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 600 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 2.5 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 960 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 600 })
    const mod = await import('@/composables/useWideScreenLayout')
    expect(mod.getWideScreenState().isWideScreen.value).toBe(true)

    // Rotate to portrait → back to single column
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 600 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 960 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 600 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 960 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getWideScreenState().isWideScreen.value).toBe(false)

    // Phone portrait, high DPR → stays single column
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 430 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 900 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 3 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 430 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 900 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getWideScreenState().isWideScreen.value).toBe(false)
  })

  it('keyboard opening on portrait tablet does NOT trigger wide screen', async () => {
    vi.resetModules()
    // Portrait tablet: screen 960×1600, DPR 2.5
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 960 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 1600 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 2.5 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 960 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 1600 })
    const mod = await import('@/composables/useWideScreenLayout')
    expect(mod.getWideScreenState().isWideScreen.value).toBe(false)

    // Keyboard opens: window.innerHeight shrinks, but screen stays the same
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 900 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getWideScreenState().isWideScreen.value).toBe(false)
  })
})
