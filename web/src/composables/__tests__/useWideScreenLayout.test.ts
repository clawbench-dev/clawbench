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
  setChatCollapsed,
  computeIsWideScreen,
  WIDE_SCREEN_DOCK_TABS,
  WIDE_SCREEN_SPLIT_RATIO_KEY,
  WIDE_SCREEN_CHAT_COLLAPSED_KEY,
  WIDE_SCREEN_PRIMARY_TABS,
  wideDockTabOrder,
} from '@/composables/useWideScreenLayout'
import { DOCK_TABS, DOCK_TAB_IDS, isDockTabId, secondaryDockTabs } from '@/composables/dockTabs'
import enMessages from '@/i18n/locales/en'
import zhMessages from '@/i18n/locales/zh'
import { DOCK_TABS_WITH_ICONS } from '@/composables/dockTabMeta'

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

  it('switchLeftTab ignores invalid tabs and applies valid ones', () => {
    const { leftTab } = useWideScreenLayout()
    switchLeftTab('terminal')
    expect(leftTab.value).toBe('terminal')
    switchLeftTab('not-a-tab' as never)
    expect(leftTab.value).toBe('terminal')
  })

  it('view is a wide-screen dock tab and can be switched to', () => {
    expect(WIDE_SCREEN_DOCK_TABS).toContain('view')
    switchLeftTab('view')
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

  it('starts on browse and keeps no cross-init memory of its own', () => {
    // leftTab is no longer persisted here — remembering it per project is
    // useProjectPanel.ts's job (issue #474). This module must always come up on
    // its default, so a re-init cannot resurrect another project's tab.
    switchLeftTab('settings')
    _resetForTest()
    const { leftTab } = useWideScreenLayout()
    expect(leftTab.value).toBe('browse')
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

describe('chatCollapsed (dock chat toggle)', () => {
  it('defaults to expanded (chat visible, false)', () => {
    const { chatCollapsed } = useWideScreenLayout()
    expect(chatCollapsed.value).toBe(false)
  })

  it('setChatCollapsed toggles the shared state and persists', () => {
    const { chatCollapsed } = useWideScreenLayout()
    setChatCollapsed(true)
    expect(chatCollapsed.value).toBe(true)
    expect(localStorage.getItem(WIDE_SCREEN_CHAT_COLLAPSED_KEY)).toBe('1')
    setChatCollapsed(false)
    expect(chatCollapsed.value).toBe(false)
    expect(localStorage.getItem(WIDE_SCREEN_CHAT_COLLAPSED_KEY)).toBe('0')
  })

  it('restores persisted collapsed state on init', () => {
    localStorage.setItem(WIDE_SCREEN_CHAT_COLLAPSED_KEY, '1')
    _resetForTest()
    const { chatCollapsed } = useWideScreenLayout()
    expect(chatCollapsed.value).toBe(true)
  })

  it('resetWideScreenState resets to expanded (chat visible)', () => {
    setChatCollapsed(true)
    resetWideScreenState()
    localStorage.clear() // init would otherwise restore the persisted '1'
    _resetForTest()
    const { chatCollapsed } = useWideScreenLayout()
    expect(chatCollapsed.value).toBe(false)
  })

  it('hiding chat forces the left pane open (mutual exclusion)', () => {
    // Regression: with both panes collapsed the content area would go blank.
    const { leftCollapsed, chatCollapsed } = useWideScreenLayout()
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true)
    setChatCollapsed(true) // hide chat while left is collapsed
    expect(chatCollapsed.value).toBe(true)
    expect(leftCollapsed.value).toBe(false) // left auto-reopens
  })

  it('collapsing the left pane is a no-op while chat is hidden', () => {
    const { leftCollapsed, chatCollapsed } = useWideScreenLayout()
    setChatCollapsed(true)
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(false) // stays open
    setChatCollapsed(false)
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true) // now allowed
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
  // Secondary tabs = the registry minus the primary head, in registry order.
  const SECONDARY_TABS = DOCK_TABS.filter((t) => !t.primary).map((t) => t.id)

  it('puts the fixed primary tabs first, then the secondary tabs in given order', () => {
    const all = wideDockTabOrder(SECONDARY_TABS)
    expect(all).toEqual([...WIDE_SCREEN_PRIMARY_TABS, ...SECONDARY_TABS])
    expect(all).toEqual(WIDE_SCREEN_DOCK_TABS)
  })

  it('preserves secondary-tab order after filtering (terminal/proxy disabled)', () => {
    expect(wideDockTabOrder(['forge', 'tasks', 'settings'])).toEqual(['browse', 'view', 'history', 'forge', 'tasks', 'settings'])
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

describe('dock tab registry (single source of truth)', () => {
  it('derives the switch whitelist from the registry — no second hand-written list', () => {
    // The whole point of the registry: the whitelist is the registry's ids, so
    // the two cannot diverge. This is deliberately an identity check on the
    // exported array: if someone reintroduces a separately maintained list, the
    // reference stops matching DOCK_TAB_IDS and this fails.
    expect(WIDE_SCREEN_DOCK_TABS).toBe(DOCK_TAB_IDS)
    expect(WIDE_SCREEN_DOCK_TABS).toEqual(DOCK_TABS.map((t) => t.id))
  })

  it('has unique ids and every id is reachable by the type guard', () => {
    const ids = DOCK_TABS.map((t) => t.id)
    expect(new Set(ids).size).toBe(ids.length)
    for (const id of ids) expect(isDockTabId(id)).toBe(true)
  })

  it('primary tabs are a prefix-ordered subset of the registry', () => {
    const primary = DOCK_TABS.filter((t) => t.primary).map((t) => t.id)
    expect(primary).toEqual([...WIDE_SCREEN_PRIMARY_TABS])
    // Primary tabs must come first in registry order, because the dock renders
    // WIDE_SCREEN_PRIMARY_TABS before the secondary list.
    expect(DOCK_TABS.map((t) => t.id).slice(0, primary.length)).toEqual(primary)
  })

  it('places port mapping second-to-last, with settings still the final tab', () => {
    // The dock order is a product decision, not an implementation detail: port
    // mapping is an operational tab that sits with the other tools, while
    // settings stays the last tab (the "exit" affordance at the end of the
    // dock). Asserting the tail rather than the whole list keeps this from
    // breaking every time an unrelated tab is inserted earlier.
    const ids = DOCK_TABS.map((t) => t.id)
    expect(ids[ids.length - 1]).toBe('settings')
    expect(ids[ids.length - 2]).toBe('proxy')
  })

  it('keeps port mapping after terminal in the rendered secondary list', () => {
    // Guards the concrete move: proxy used to sit between terminal and stats,
    // so the rendered order changed even though the id set did not. Without
    // this, a revert of the registry edit would still pass every other test.
    const rendered = secondaryDockTabs()
    expect(rendered.indexOf('proxy')).toBeGreaterThan(rendered.indexOf('terminal'))
    expect(rendered.indexOf('proxy')).toBeGreaterThan(rendered.indexOf('stats'))
    expect(rendered.indexOf('proxy')).toBeLessThan(rendered.indexOf('settings'))
  })

  it('every registry entry carries an i18n title key', () => {
    for (const tab of DOCK_TABS) {
      expect(tab.titleKey, `${tab.id} has no titleKey`).toMatch(/^[a-z]+\.[A-Za-z]/)
    }
  })

  it('every title key actually RESOLVES in both locales', () => {
    // The shape check above passes for a typo'd key like "nav.overveiw", which
    // would render the raw key as the tab's tooltip with no error anywhere.
    for (const [locale, messages] of Object.entries({ en: enMessages, zh: zhMessages })) {
      for (const tab of DOCK_TABS) {
        const value = tab.titleKey.split('.').reduce<unknown>(
          (acc, part) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[part] : undefined),
          messages,
        )
        expect(typeof value, `${tab.titleKey} missing in ${locale}`).toBe('string')
        expect(value, `${tab.titleKey} is empty in ${locale}`).not.toBe('')
      }
    }
  })

  it('every registry entry has an icon attached in the icon module', () => {
    // Icons live in dockTabMeta.ts (kept out of dockTabs.ts so that importing
    // the ids does not pull lucide into composable module graphs). Both halves
    // must cover exactly the same ids.
    expect(DOCK_TABS_WITH_ICONS.map((t) => t.id)).toEqual(DOCK_TABS.map((t) => t.id))
    for (const tab of DOCK_TABS_WITH_ICONS) {
      expect(tab.icon, `${tab.id} has no icon`).toBeTruthy()
    }
  })

  it('rejects unknown ids', () => {
    expect(isDockTabId('not-a-tab')).toBe(false)
    expect(isDockTabId('')).toBe(false)
  })
})

describe('wide dock tab reachability (regression)', () => {
  // A tab rendered in the wide dock but not switchable is a dead button:
  // switchLeftTab() rejects it, so clicking does nothing visible. That was the
  // forge tab bug — it was rendered by the wide dock (App.vue's overflowTabs,
  // which starts with 'forge') but missing from the whitelist.
  //
  // The render list comes from secondaryDockTabs(), the same function App.vue's
  // overflowTabs computed calls, so this asserts the real render list rather
  // than the registry compared against itself.
  const RENDERED_ALL = wideDockTabOrder(secondaryDockTabs())

  it('every tab the wide dock renders can actually be switched to', () => {
    // Direct whitelist coverage: the dock renders exactly these tabs, so the
    // switch whitelist must contain every one of them.
    expect(RENDERED_ALL.filter((tab) => !WIDE_SCREEN_DOCK_TABS.includes(tab))).toEqual([])

    for (const tab of RENDERED_ALL) {
      resetWideScreenState()
      // Start from a tab that is guaranteed different from the target, so the
      // switch below is a real transition rather than the same-tab early return.
      switchLeftTab(tab === 'browse' ? 'settings' : 'browse')
      const setActiveTab = vi.fn()
      registerWideScreenCallbacks({ setActiveTab })
      switchLeftTab(tab)
      const { leftTab } = useWideScreenLayout()
      expect(leftTab.value, `dock tab "${tab}" is rendered but not switchable`).toBe(tab)
      expect(setActiveTab, `dock tab "${tab}" did not sync activeTab`).toHaveBeenCalledWith(tab)
    }
  })

  it('still holds for every runtime gate combination', () => {
    // The gates change *which* tabs render; all four combinations must remain
    // switchable. This is the case the old registry-vs-registry test could not
    // see, because the gates live outside the registry.
    for (const terminalDisabled of [false, true]) {
      for (const sshDisabled of [false, true]) {
        const rendered = wideDockTabOrder(secondaryDockTabs({ terminalDisabled, sshDisabled }))
        expect(
          rendered.filter((tab) => !WIDE_SCREEN_DOCK_TABS.includes(tab)),
          `gate combination terminalDisabled=${terminalDisabled} sshDisabled=${sshDisabled}`,
        ).toEqual([])
      }
    }
  })

  it('gates hide exactly terminal and proxy, and nothing else', () => {
    const all = secondaryDockTabs()
    expect(all).toContain('terminal')
    expect(all).toContain('proxy')
    expect(secondaryDockTabs({ terminalDisabled: true })).not.toContain('terminal')
    expect(secondaryDockTabs({ terminalDisabled: true })).toContain('proxy')
    expect(secondaryDockTabs({ sshDisabled: true })).not.toContain('proxy')
    expect(secondaryDockTabs({ sshDisabled: true })).toContain('terminal')
    expect(secondaryDockTabs({ terminalDisabled: true, sshDisabled: true })).not.toContain('terminal')
    expect(secondaryDockTabs({ terminalDisabled: true, sshDisabled: true })).not.toContain('proxy')
    // Order is registry order with the gated ones removed, never reordered.
    expect(secondaryDockTabs({ terminalDisabled: true, sshDisabled: true })).toEqual(
      all.filter((t) => t !== 'terminal' && t !== 'proxy'),
    )
  })

  it('forge is switchable', () => {
    expect(WIDE_SCREEN_DOCK_TABS).toContain('forge')
    switchLeftTab('forge')
    const { leftTab } = useWideScreenLayout()
    expect(leftTab.value).toBe('forge')
  })

  it('resolveLeftTabOnEnter accepts forge as the current narrow-mode tab', () => {
    expect(resolveLeftTabOnEnter('forge', 'browse')).toBe('forge')
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
