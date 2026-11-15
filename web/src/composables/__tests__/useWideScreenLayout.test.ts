import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import {
  useWideScreenLayout,
  getWideScreenState,
  switchLeftTab,
  showDockTab,
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
  isChatPanelVisible,
  WIDE_SCREEN_DOCK_TABS,
} from '@/composables/useWideScreenLayout'
import { DOCK_TABS, DOCK_TAB_IDS, dockTabs, isDockTabId, type DockTabId } from '@/composables/dockTabs'
import { PANEL_KEY_PREFIX, _resetLayoutScopeForTest, setLayoutProjectRoot, setLayoutScopeMode } from '@/utils/panelState'
import { portForwardUnavailable } from '@/utils/portForwardUtils'
import { DEFAULT_RATIO } from '@/utils/splitRatio'
import enMessages from '@/i18n/locales/en'
import zhMessages from '@/i18n/locales/zh'
import { DOCK_TABS_WITH_ICONS } from '@/composables/dockTabMeta'

// Persistence keys are owned by the panel-state registry.
const WIDE_SCREEN_SPLIT_RATIO_KEY = PANEL_KEY_PREFIX + 'split-ratio'
const WIDE_SCREEN_CHAT_COLLAPSED_KEY = PANEL_KEY_PREFIX + 'chat-collapsed'
const LEFT_COLLAPSED_KEY = PANEL_KEY_PREFIX + 'left-collapsed'

beforeEach(() => {
  _resetForTest()
  resetWideScreenState()
  localStorage.clear()
})

describe('computeIsWideScreen', () => {
  it('CSS width ≥960 → wide screen', () => {
    expect(computeIsWideScreen(1280, 1)).toBe(true)
    expect(computeIsWideScreen(1024, 1)).toBe(true)
    expect(computeIsWideScreen(960, 1)).toBe(true)
  })

  it('CSS width <960 → NOT wide screen, regardless of device size', () => {
    expect(computeIsWideScreen(959, 1)).toBe(false)
    expect(computeIsWideScreen(800, 1)).toBe(false)
    expect(computeIsWideScreen(430, 1)).toBe(false)
    expect(computeIsWideScreen(360, 1)).toBe(false)
  })

  it('is pure CSS width: a physically wide landscape phone is NOT wide screen', () => {
    // 1080×1920 phone at DPR 2.75 in landscape → 698 CSS px. It is physically
    // 1920px wide, but the layout only ever has 698px, so two columns do not fit.
    // (The removed physical-width clause used to call this "wide", which was
    // self-contradictory: a 960 CSS px desktop window was not.)
    expect(computeIsWideScreen(698, 1)).toBe(false)
  })

  it('high-DPR tablet at 960 CSS px IS wide screen (the case the physical clause existed for)', () => {
    // 2400 physical px at DPR 2.5 → 960 CSS px. Folding it into the threshold
    // keeps this case working without a physical-pixel branch.
    expect(computeIsWideScreen(960, 1)).toBe(true)
  })

  it('does not depend on screen dimensions or DPR (no such parameters)', () => {
    // The signature is (cssWidth, uiScale) — this is the structural guarantee
    // that device size can no longer influence the decision. The scale IS a
    // legitimate input: at a larger scale the same window holds fewer DESIGN
    // pixels, so the panels genuinely do not fit side by side any more.
    //
    // `uiScale` has NO default (a default made a forgotten argument silently
    // mean "ignore the scale"), so `Function.length` is 2 — arity is part of
    // the contract here, not just the parameter names.
    const src = computeIsWideScreen.toString()
    // Only the parameter list: the function NAME contains "Screen", which would
    // otherwise trip the negative assertion below.
    const params = src.slice(src.indexOf('(') + 1, src.indexOf(')')).replace(/\s+/g, ' ')
    expect(params).toContain('cssWidth')
    expect(params).toContain('uiScale')
    expect(params, 'uiScale must not have a default value').not.toMatch(/uiScale\s*=/)
    expect(params).not.toMatch(/screen|dpr|devicePixel/i)
  })

  it('divides the viewport by the UI scale before comparing', () => {
    // 960 design px is the threshold, so the same window flips as it grows.
    expect(computeIsWideScreen(960, 1)).toBe(true)
    expect(computeIsWideScreen(1440, 1.5)).toBe(true)   // exactly 960 design px
    expect(computeIsWideScreen(1439, 1.5)).toBe(false)  // 959.3 design px
    expect(computeIsWideScreen(1920, 2)).toBe(true)     // exactly 960 design px
    expect(computeIsWideScreen(1919, 2)).toBe(false)
  })

  it('treats a nonsensical scale as 1', () => {
    // A first paint before the applier has written --ui-scale, or a
    // hand-edited 0/NaN, must not make the decision collapse.
    expect(computeIsWideScreen(1280, 1)).toBe(true)
    expect(computeIsWideScreen(1280, 0)).toBe(true)
    expect(computeIsWideScreen(1280, NaN)).toBe(true)
    expect(computeIsWideScreen(1280, -2)).toBe(true)
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

  it('showDockTab re-asserts the narrow-mode active tab even when leftTab matches', () => {
    const setActiveTab = vi.fn()
    registerWideScreenCallbacks({ setActiveTab })
    switchLeftTab('kb')
    setActiveTab.mockClear()

    // The narrow-mode bug this exists for: leftTab is already kb (it is
    // persisted), activeTab is chat, and switchLeftTab would early-return.
    showDockTab('kb')
    expect(setActiveTab).toHaveBeenCalledWith('kb')
  })

  it('showDockTab expands a collapsed left column on a wide screen', () => {
    _setWideScreenForTest(true)
    setLeftCollapsed(true)
    showDockTab('kb')
    const { leftCollapsed } = useWideScreenLayout()
    expect(leftCollapsed.value).toBe(false)
  })

  it('showDockTab ignores invalid tabs', () => {
    const { leftTab } = useWideScreenLayout()
    showDockTab('not-a-tab' as never)
    expect(leftTab.value).toBe('browse')
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

  it('fresh init with no persisted ratio uses the 4:6 default (left narrower)', () => {
    const { splitRatio } = useWideScreenLayout()
    expect(splitRatio.value).toBe(DEFAULT_RATIO)
    expect(splitRatio.value).toBeCloseTo(0.4)
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
  it('defaults to collapsed (left column hidden)', () => {
    // A fresh view opens on chat alone; the user opts into the left column by
    // clicking a dock tab.
    const { leftCollapsed } = useWideScreenLayout()
    expect(leftCollapsed.value).toBe(true)
  })

  it('setLeftCollapsed toggles the shared state', () => {
    const { leftCollapsed } = useWideScreenLayout()
    setLeftCollapsed(false)
    expect(leftCollapsed.value).toBe(false)
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true)
  })

  it('resetWideScreenState resets to collapsed', () => {
    setLeftCollapsed(false)
    resetWideScreenState()
    // The flag is persisted now, so a re-init would restore the stored '0' —
    // clear storage first (same pattern as the chatCollapsed reset test).
    localStorage.clear()
    _resetForTest()
    const { leftCollapsed } = useWideScreenLayout()
    expect(leftCollapsed.value).toBe(true)
  })

  it('does NOT expand the collapsed pane on a tab switch', () => {
    // Deliberate: switchLeftTab is also the programmatic restore path (cold
    // start / project switch), so expanding here would override a column the
    // user had hidden. Callers that represent a user switching tabs expand
    // explicitly (switchTab's wide branch, handleWideDockTabClick).
    const { leftCollapsed } = useWideScreenLayout()
    switchLeftTab('history')
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true)
    switchLeftTab('tasks')
    expect(leftCollapsed.value).toBe(true)
  })

  it('persists the collapsed flag globally', () => {
    setLeftCollapsed(true)
    expect(localStorage.getItem(LEFT_COLLAPSED_KEY)).toBe('1')
    setLeftCollapsed(false)
    expect(localStorage.getItem(LEFT_COLLAPSED_KEY)).toBe('0')
  })

  it('regression: a programmatic tab restore leaves a hidden column hidden', () => {
    // The reported bug: hide the left column, switch projects (which restores
    // that project's remembered tab through switchLeftTab), and the column
    // reappeared. The collapse is a global device state, so a project switch
    // must not disturb it — neither by the implicit expand that used to live in
    // switchLeftTab, nor by reading a per-project collapsed record.
    const { leftCollapsed } = useWideScreenLayout()
    setLeftCollapsed(true)
    expect(leftCollapsed.value).toBe(true)

    // Simulate applyProjectPanel's restore path: switchLeftTab only.
    switchLeftTab('history')
    switchLeftTab('view')
    expect(leftCollapsed.value).toBe(true)

    // A fresh layout init (e.g. reload) still reads the global flag.
    _resetForTest()
    expect(useWideScreenLayout().leftCollapsed.value).toBe(true)
  })

  it('restores the persisted collapsed flag on init', () => {
    localStorage.setItem(LEFT_COLLAPSED_KEY, '0')
    _resetForTest()
    const { leftCollapsed } = useWideScreenLayout()
    expect(leftCollapsed.value).toBe(false)
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

describe('isChatPanelVisible', () => {
  it('narrow screen: visible only while the chat tab is the active tab', () => {
    expect(isChatPanelVisible({ isWideScreen: false, chatCollapsed: false, activeTab: 'chat' })).toBe(true)
    expect(isChatPanelVisible({ isWideScreen: false, chatCollapsed: false, activeTab: 'browse' })).toBe(false)
    expect(isChatPanelVisible({ isWideScreen: false, chatCollapsed: false, activeTab: 'terminal' })).toBe(false)
  })

  it('wide screen with chat expanded: always visible (the pane is docked)', () => {
    // The wide-screen layout pins the chat column on the right regardless of
    // which left-column tab is selected, so every left tab still shows chat.
    expect(isChatPanelVisible({ isWideScreen: true, chatCollapsed: false, activeTab: 'chat' })).toBe(true)
    expect(isChatPanelVisible({ isWideScreen: true, chatCollapsed: false, activeTab: 'browse' })).toBe(true)
    expect(isChatPanelVisible({ isWideScreen: true, chatCollapsed: false, activeTab: 'terminal' })).toBe(true)
  })

  it('wide screen with chat collapsed: NOT visible on any tab', () => {
    // Collapsing the chat pane gives it display:none (SplitView rightCollapsed),
    // so the user cannot be looking at it — even while activeTab says "chat".
    expect(isChatPanelVisible({ isWideScreen: true, chatCollapsed: true, activeTab: 'chat' })).toBe(false)
    expect(isChatPanelVisible({ isWideScreen: true, chatCollapsed: true, activeTab: 'browse' })).toBe(false)
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

describe('wide dock order', () => {
  it('is the registry order — one list, no head/tail concatenation', () => {
    expect(WIDE_SCREEN_DOCK_TABS).toEqual(DOCK_TAB_IDS)
    expect(dockTabs()).toEqual(DOCK_TAB_IDS)
  })

  it('is deterministic regardless of any runtime geometry', () => {
    // The whole visible dock never depends on measured space — regression guard
    // for the old height-measured overflow that collapsed tabs into a popup.
    expect(dockTabs()).toEqual(dockTabs())
    expect(dockTabs()).toHaveLength(DOCK_TAB_IDS.length)
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

  it('has three permanent tabs, and none of them has a fixed position', () => {
    // The old "primary" flag meant both "cannot be hidden" and "rendered in the
    // fixed head". Only the first survives: permanent tabs keep their default
    // slots but the user may drag them anywhere.
    const permanent = DOCK_TABS.filter((t) => !t.closable).map((t) => t.id)
    expect(permanent).toEqual(['browse', 'view', 'settings'])
    const order = [...DOCK_TAB_IDS].reverse()
    expect(dockTabs({ order })).toEqual(order)
  })

  it('places notifications immediately before settings in the DEFAULT order', () => {
    // The dock order is a product decision, not an implementation detail:
    // notifications is the last stop before leaving the tool tabs, while
    // settings is the "exit" affordance at the end. This is the default only —
    // both can be dragged elsewhere.
    const ids = DOCK_TABS.map((t) => t.id)
    expect(ids[ids.length - 1]).toBe('settings')
    expect(ids[ids.length - 2]).toBe('notifications')
  })

  it('keeps port mapping before notifications in the rendered list', () => {
    // Guards the concrete move: notifications used to sit between tasks and
    // terminal, so the rendered order changed even though the id set did not.
    // Without this, a revert of the registry edit would still pass every other
    // test.
    const rendered = dockTabs()
    expect(rendered.indexOf('proxy')).toBeGreaterThan(rendered.indexOf('terminal'))
    expect(rendered.indexOf('proxy')).toBeGreaterThan(rendered.indexOf('stats'))
    expect(rendered.indexOf('notifications')).toBeGreaterThan(rendered.indexOf('proxy'))
    expect(rendered.indexOf('notifications')).toBeLessThan(rendered.indexOf('settings'))
  })

  it('renders the sharing tab immediately before port mapping', () => {
    // The user asked for that exact slot: both tabs are "connections this
    // machine offers", and port mapping must stay the last of the pair. The
    // share centre sits on the other side of 会议, keeping the outgoing-access
    // trio together: 会议 → 分享 → 端口映射.
    const rendered = dockTabs()
    expect(rendered.indexOf('live')).toBe(rendered.indexOf('share') - 1)
    expect(rendered.indexOf('share')).toBe(rendered.indexOf('proxy') - 1)
  })

  it('keeps the meeting tab visible on every host', () => {
    // 会议 is no longer gated on capture: the record list is readable
    // everywhere, and a voice meeting only needs a microphone. What the host
    // cannot do is explained inside the create page.
    expect(dockTabs()).toContain('live')
    expect(dockTabs({ unavailable: new Set<DockTabId>(['proxy']) })).toContain('live')
  })

  it('has no separate notes tab any more', () => {
    // 听记 and 桌面共享 were two entries for one pipeline; they are now one.
    expect(DOCK_TAB_IDS).not.toContain('notes')
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
  // forge tab bug — it was rendered by the wide dock but missing from the
  // whitelist.
  //
  // The render list comes from dockTabs(), the same function App.vue's
  // visibleDockTabs computed calls, so this asserts the real render list rather
  // than the registry compared against itself.
  const RENDERED_ALL = dockTabs()

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
    // The gates change *which* tabs render; every combination must remain
    // switchable. This is the case the old registry-vs-registry test could not
    // see, because the gates live outside the registry.
    const featureSets: (Set<DockTabId> | undefined)[] = [
      undefined,
      new Set<DockTabId>(['forge', 'mail', 'tasks', 'share', 'kb']),
      new Set<DockTabId>(['forge', 'mail', 'tasks', 'share', 'kb', 'terminal']),
    ]
    const unavailableSets: (Set<DockTabId> | undefined)[] = [
      undefined,
      new Set<DockTabId>(['terminal']),
      new Set<DockTabId>(['terminal', 'proxy']),
    ]
    for (const unavailable of unavailableSets) {
      for (const featureDisabled of featureSets) {
        const rendered = dockTabs({ unavailable, featureDisabled })
        expect(
          rendered.filter((tab) => !WIDE_SCREEN_DOCK_TABS.includes(tab)),
          `gate combination unavailable=${[...(unavailable ?? [])].join('+') || '-'} features=${[...(featureDisabled ?? [])].join('+') || '-'}`,
        ).toEqual([])
      }
    }
  })

  it('gates hide exactly terminal and proxy, and nothing else', () => {
    const all = dockTabs()
    expect(all).toContain('terminal')
    expect(all).toContain('proxy')
    expect(dockTabs({ unavailable: new Set<DockTabId>(['terminal']) })).not.toContain('terminal')
    expect(dockTabs({ unavailable: new Set<DockTabId>(['terminal']) })).toContain('proxy')
    expect(dockTabs({ unavailable: new Set<DockTabId>(['proxy']) })).not.toContain('proxy')
    expect(dockTabs({ unavailable: new Set<DockTabId>(['proxy']) })).toContain('terminal')
    const both = new Set<DockTabId>(['terminal', 'proxy'])
    expect(dockTabs({ unavailable: both })).not.toContain('terminal')
    expect(dockTabs({ unavailable: both })).not.toContain('proxy')
    // Order is the registry order with the gated ones removed, never reordered.
    expect(dockTabs({ unavailable: both })).toEqual(
      all.filter((t) => t !== 'terminal' && t !== 'proxy'),
    )
  })

  it('hides only the forge entry when the feature is switched off', () => {
    // Issue #590: turning the integration off removes its dock entry. It must
    // disappear from the rendered list and NOTHING else may move.
    const hidden = dockTabs({ featureDisabled: new Set<DockTabId>(['forge']) })
    expect(hidden).not.toContain('forge')
    expect(hidden).toEqual(dockTabs().filter((t) => t !== 'forge'))
    // The other gates stay independent.
    expect(hidden).toContain('terminal')
    expect(hidden).toContain('proxy')
    expect(
      dockTabs({ unavailable: new Set<DockTabId>(['terminal', 'proxy']) }),
    ).toContain('forge')
  })

  it('shows the forge entry when the switch is unset', () => {
    // `undefined` is "config not loaded yet" (or a server that predates the
    // switch) and must read as visible — hiding the tab during the first paint
    // would make it flicker in once /api/config arrives.
    expect(dockTabs({})).toContain('forge')
    expect(dockTabs({ featureDisabled: new Set<DockTabId>() })).toContain('forge')
  })

  it('hides only the mail entry when the mail feature is switched off', () => {
    // Same contract as the forge gate: the entry disappears, nothing else moves.
    const hidden = dockTabs({ featureDisabled: new Set<DockTabId>(['mail']) })
    expect(hidden).not.toContain('mail')
    expect(hidden).toEqual(dockTabs().filter((t) => t !== 'mail'))
    expect(hidden).toContain('forge')
    expect(hidden).toContain('terminal')
    expect(dockTabs({ featureDisabled: new Set<DockTabId>(['forge']) })).toContain('mail')
  })

  it('keeps the proxy tab on an h2-only install (SSH off, h2 allowed)', () => {
    // Regression: the dock gate used to be driven by the SSH listener alone, so
    // `port_forward.enabled: false` + `transport: h2|both` — the exact scenario
    // this feature targets — filtered the port-mapping tab out of the dock and
    // made its "port mapping unavailable" banner unreachable. The shared
    // predicate reads such an install as AVAILABLE, and that boolean is what
    // App.vue folds into the `unavailable` set.
    expect(portForwardUnavailable(false, true)).toBe(false)
    const unavailable = new Set<DockTabId>()
    if (portForwardUnavailable(false, true)) unavailable.add('proxy')
    expect(dockTabs({ unavailable })).toContain('proxy')
  })

  it('still hides the proxy tab when neither SSH nor h2 can carry the tunnel', () => {
    // The old, correct behaviour must survive: SSH off with no h2 path really is
    // "unavailable", so the tab stays hidden.
    expect(portForwardUnavailable(false, false)).toBe(true)
    const unavailable = new Set<DockTabId>()
    if (portForwardUnavailable(false, false)) unavailable.add('proxy')
    expect(dockTabs({ unavailable })).not.toContain('proxy')
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

  it('activates for a 960 CSS px tablet and reacts to resize/rotation', async () => {
    vi.resetModules()
    // 960 CSS px (2400 physical at DPR 2.5) → wide screen on CSS width alone.
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 960 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 600 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 2.5 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 960 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 600 })
    const mod = await import('@/composables/useWideScreenLayout')
    expect(mod.getWideScreenState().isWideScreen.value).toBe(true)

    // Rotate to portrait: CSS width drops below 960 → back to single column.
    // (Orientation is not consulted; the width change alone drives this.)
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 600 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 960 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 600 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 960 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getWideScreenState().isWideScreen.value).toBe(false)

    // Phone portrait, high DPR → stays single column (CSS width 430 < 960).
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 430 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 900 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 3 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 430 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 900 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getWideScreenState().isWideScreen.value).toBe(false)
  })

  it('portrait tablet at 960 CSS px IS wide screen (orientation is not consulted)', async () => {
    vi.resetModules()
    // Portrait tablet: 960×1600 CSS. Under the old physical+landscape rule this
    // was NOT wide (portrait). It now is: 960 CSS px is enough room for two
    // columns, and orientation is irrelevant to a layout-space decision.
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 960 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 1600 })
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 2.5 })
    Object.defineProperty(window.screen, 'width', { configurable: true, value: 960 })
    Object.defineProperty(window.screen, 'height', { configurable: true, value: 1600 })
    const mod = await import('@/composables/useWideScreenLayout')
    expect(mod.getWideScreenState().isWideScreen.value).toBe(true)

    // Soft keyboard shrinks the viewport height; width is unchanged, so the
    // layout stays wide-screen (this is the stability the old landscape gate
    // was protecting — now guaranteed structurally, since only width is read).
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 900 })
    window.dispatchEvent(new Event('resize'))
    expect(mod.getWideScreenState().isWideScreen.value).toBe(true)
  })
})

describe('useWideScreenLayout — layout memory scope', () => {
  const ROOT_A = '/project/a'
  beforeEach(() => {
    _resetForTest()
    _resetLayoutScopeForTest()
    localStorage.clear()
  })
  afterEach(() => _resetLayoutScopeForTest())

  it('re-reads split ratio and collapses when the scope switches, without a reload', async () => {
    // The module guards its load behind `initialized`, so a missing epoch
    // watcher would leave the on-screen split untouched and make the setting
    // look broken until the next reload.
    localStorage.setItem(PANEL_KEY_PREFIX + 'split-ratio', '0.5')
    localStorage.setItem(PANEL_KEY_PREFIX + 'chat-collapsed', '0')
    const { splitRatio, chatCollapsed } = useWideScreenLayout()
    expect(splitRatio.value).toBe(0.5)

    localStorage.setItem(PANEL_KEY_PREFIX + 'split-ratio:' + ROOT_A, '0.7')
    localStorage.setItem(PANEL_KEY_PREFIX + 'chat-collapsed:' + ROOT_A, '1')
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    await nextTick()

    expect(splitRatio.value).toBe(0.7)
    expect(chatCollapsed.value).toBe(true)
  })

  it('persists the split ratio to the project key in project mode', async () => {
    const { splitRatio } = useWideScreenLayout()
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    await nextTick()

    setSplitRatio(0.35)
    expect(splitRatio.value).toBe(0.35)
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'split-ratio:' + ROOT_A)).toBe('0.35')
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'split-ratio')).toBeNull()
  })

  it('keeps leftTab per-project regardless of the scope mode', async () => {
    // leftTab is pinned: "remember layout globally" must not make every project
    // share one tab (issue #474).
    localStorage.setItem(PANEL_KEY_PREFIX + 'left-tab:' + ROOT_A, 'tasks')
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('global')
    await nextTick()
    expect(localStorage.getItem(PANEL_KEY_PREFIX + 'left-tab')).toBeNull()
  })
})
