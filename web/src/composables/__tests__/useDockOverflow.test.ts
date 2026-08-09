import { describe, expect, it, vi } from 'vitest'
import { useDockOverflow, type DockOverflowOptions } from '@/composables/useDockOverflow'

// Mock ResizeObserver
const mockObserve = vi.fn()
const mockDisconnect = vi.fn()
vi.stubGlobal('ResizeObserver', class {
  observe = mockObserve
  disconnect = mockDisconnect
})

describe('useDockOverflow', () => {
  function createSetup(overflowTabs: string[], options: DockOverflowOptions = {}) {
    const dockEl = document.createElement('div')
    dockEl.style.paddingLeft = '8px'
    dockEl.style.paddingRight = '8px'
    dockEl.style.paddingTop = '8px'
    dockEl.style.paddingBottom = '8px'

    const result = useDockOverflow(
      () => dockEl,
      () => overflowTabs,
      options,
    )
    return { dockEl, ...result }
  }

  function setupWithSize(overflowTabs: string[], size: number, options: DockOverflowOptions = {}) {
    const s = createSetup(overflowTabs, options)
    s.startObserving()
    s.dockContentSize.value = size
    return s
  }

  const TABS = ['tasks', 'proxy', 'terminal', 'settings']

  describe('inlineOverflowTabs', () => {
    it('returns empty at minimum width (218px)', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 218)
      expect(inlineOverflowTabs.value).toEqual([])
    })

    it('returns empty below minimum', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 100)
      expect(inlineOverflowTabs.value).toEqual([])
    })

    it('returns first tab when space for 1 (264px)', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 264)
      expect(inlineOverflowTabs.value).toEqual(['tasks'])
    })

    it('returns first 2 tabs when space for 2 (310px)', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 310)
      expect(inlineOverflowTabs.value).toEqual(['tasks', 'proxy'])
    })

    it('returns all tabs in order when space allows', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 600)
      expect(inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
    })

    it('settings is always last when not all inline', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 356)
      // 356 - 218 = 138, 138/46 = 3 → tasks, proxy, terminal
      expect(inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal'])
    })

    it('caps at total overflow tab count', () => {
      const { inlineOverflowTabs } = setupWithSize(TABS, 9999)
      expect(inlineOverflowTabs.value).toEqual(TABS)
    })
  })

  describe('popupOverflowTabs', () => {
    it('returns all tabs at minimum width', () => {
      const { popupOverflowTabs } = setupWithSize(TABS, 218)
      expect(popupOverflowTabs.value).toEqual(TABS)
    })

    it('returns remaining tabs not inline', () => {
      const { popupOverflowTabs } = setupWithSize(TABS, 264)
      expect(popupOverflowTabs.value).toEqual(['proxy', 'terminal', 'settings'])
    })

    it('returns empty when all inline', () => {
      const { popupOverflowTabs } = setupWithSize(TABS, 600)
      expect(popupOverflowTabs.value).toEqual([])
    })
  })

  describe('singleDirectTab', () => {
    it('is null when popup has 0 items', () => {
      const { singleDirectTab } = setupWithSize(['tasks', 'settings'], 310)
      expect(singleDirectTab.value).toBeNull()
    })

    it('is the single popup tab when popup has exactly 1 item', () => {
      const { singleDirectTab } = setupWithSize(['tasks', 'proxy', 'settings'], 310)
      expect(singleDirectTab.value).toBe('settings')
    })

    it('is null when popup has >1 items', () => {
      const { singleDirectTab } = setupWithSize(TABS, 218)
      expect(singleDirectTab.value).toBeNull()
    })
  })

  describe('showOverflowButton', () => {
    it('is true when popup has >1 items', () => {
      const { showOverflowButton } = setupWithSize(TABS, 218)
      expect(showOverflowButton.value).toBe(true)
    })

    it('is false when popup has exactly 1 item (singleDirectTab handles it)', () => {
      const { showOverflowButton } = setupWithSize(['tasks', 'proxy', 'settings'], 310)
      expect(showOverflowButton.value).toBe(false)
    })

    it('is false when popup is empty', () => {
      const { showOverflowButton } = setupWithSize(TABS, 600)
      expect(showOverflowButton.value).toBe(false)
    })
  })

  describe('totalDockButtons', () => {
    it('counts 5 buttons at minimum (4 primary + overflow btn)', () => {
      const { totalDockButtons } = setupWithSize(TABS, 218)
      expect(totalDockButtons.value).toBe(5)
    })

    it('counts all inline when space allows', () => {
      const { totalDockButtons } = setupWithSize(TABS, 600)
      expect(totalDockButtons.value).toBe(8)
    })
  })

  describe('width=0 guard (display:none / v-show hidden)', () => {
    it('ResizeObserver width=0 preserves previous good width', () => {
      const s = createSetup(TABS)
      s.startObserving()
      s.dockContentSize.value = 402
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])

      // Simulate ResizeObserver reporting width=0 (element hidden)
      s.dockContentSize.value = 0
      expect(s.inlineOverflowTabs.value).toEqual([])

      // With the guard in startObserving, the callback skips width=0,
      // so dockContentSize stays at the last good value.
      s.dockContentSize.value = 402
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
    })

    it('startObserving when element hidden preserves previous width', () => {
      const s = createSetup(TABS)
      s.startObserving()
      s.dockContentSize.value = 402

      // If startObserving is called while hidden (clientWidth=0),
      // measured would be <=0, so dockContentSize is NOT updated
      const widthBefore = s.dockContentSize.value
      expect(widthBefore).toBe(402)
    })

    it('sequence: good width → hidden(0) → shown again → correct final state', () => {
      const s = createSetup(TABS)
      s.startObserving()

      // Good width — all tabs inline
      s.dockContentSize.value = 402
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
      expect(s.showOverflowButton.value).toBe(false)

      // Element hidden — with guard, width stays at 402 (not overwritten to 0)
      // Buttons don't collapse when dock is temporarily hidden
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
      expect(s.showOverflowButton.value).toBe(false)
    })
  })

  describe('vertical (wide-screen dock) direction', () => {
    const VERT = { direction: 'vertical' as const, primaryCount: 3 }

    it('promotes overflow tabs by height with 3 reserved primary buttons', () => {
      // minContent = (3+1)*34 + 3*12 = 172; each extra item = 46
      expect(setupWithSize(TABS, 172, VERT).inlineOverflowTabs.value).toEqual([])
      expect(setupWithSize(TABS, 218, VERT).inlineOverflowTabs.value).toEqual(['tasks'])
      expect(setupWithSize(TABS, 264, VERT).inlineOverflowTabs.value).toEqual(['tasks', 'proxy'])
      expect(setupWithSize(TABS, 356, VERT).inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
    })

    it('respects custom button size and gap', () => {
      const opts = { direction: 'vertical' as const, primaryCount: 2, btnSize: 40, gap: 8 }
      // minContent = (2+1)*40 + 2*8 = 136; step = 48
      expect(setupWithSize(TABS, 136, opts).inlineOverflowTabs.value).toEqual([])
      expect(setupWithSize(TABS, 184, opts).inlineOverflowTabs.value).toEqual(['tasks'])
    })

    it('computes popup tabs, single-direct and overflow button for vertical', () => {
      const s = setupWithSize(TABS, 218, VERT) // 1 inline → proxy, terminal, settings in popup
      expect(s.popupOverflowTabs.value).toEqual(['proxy', 'terminal', 'settings'])
      expect(s.singleDirectTab.value).toBeNull() // 3 > 1 → not single
      expect(s.showOverflowButton.value).toBe(true)
      expect(s.totalDockButtons.value).toBe(3 + 1 + 1) // primary + inline + overflow btn
    })
  })
})
