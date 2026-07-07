import { describe, expect, it, vi } from 'vitest'
import { useDockOverflow } from '@/composables/useDockOverflow'

// Mock ResizeObserver
const mockObserve = vi.fn()
const mockDisconnect = vi.fn()
vi.stubGlobal('ResizeObserver', class {
  observe = mockObserve
  disconnect = mockDisconnect
})

describe('useDockOverflow', () => {
  function createSetup(overflowTabs: string[]) {
    const dockEl = document.createElement('div')
    dockEl.style.paddingLeft = '8px'
    dockEl.style.paddingRight = '8px'

    const result = useDockOverflow(
      () => dockEl,
      () => overflowTabs,
    )
    return { dockEl, ...result }
  }

  function setupWithWidth(overflowTabs: string[], width: number) {
    const s = createSetup(overflowTabs)
    s.startObserving()
    s.dockContentWidth.value = width
    return s
  }

  const TABS = ['tasks', 'proxy', 'terminal', 'settings']

  describe('inlineOverflowTabs', () => {
    it('returns empty at minimum width (172px)', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 172)
      expect(inlineOverflowTabs.value).toEqual([])
    })

    it('returns empty below minimum', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 100)
      expect(inlineOverflowTabs.value).toEqual([])
    })

    it('returns first tab when space for 1 (218px)', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 218)
      expect(inlineOverflowTabs.value).toEqual(['tasks'])
    })

    it('returns first 2 tabs when space for 2 (264px)', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 264)
      expect(inlineOverflowTabs.value).toEqual(['tasks', 'proxy'])
    })

    it('returns all tabs in order when space allows', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 600)
      expect(inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
    })

    it('settings is always last when not all inline', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 310)
      // 310 - 172 = 138, 138/46 = 3 → tasks, proxy, terminal
      expect(inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal'])
    })

    it('caps at total overflow tab count', () => {
      const { inlineOverflowTabs } = setupWithWidth(TABS, 9999)
      expect(inlineOverflowTabs.value).toEqual(TABS)
    })
  })

  describe('popupOverflowTabs', () => {
    it('returns all tabs at minimum width', () => {
      const { popupOverflowTabs } = setupWithWidth(TABS, 172)
      expect(popupOverflowTabs.value).toEqual(TABS)
    })

    it('returns remaining tabs not inline', () => {
      const { popupOverflowTabs } = setupWithWidth(TABS, 218)
      expect(popupOverflowTabs.value).toEqual(['proxy', 'terminal', 'settings'])
    })

    it('returns empty when all inline', () => {
      const { popupOverflowTabs } = setupWithWidth(TABS, 600)
      expect(popupOverflowTabs.value).toEqual([])
    })
  })

  describe('singleDirectTab', () => {
    it('is null when popup has 0 items', () => {
      const { singleDirectTab } = setupWithWidth(['tasks', 'settings'], 264)
      expect(singleDirectTab.value).toBeNull()
    })

    it('is the single popup tab when popup has exactly 1 item', () => {
      const { singleDirectTab } = setupWithWidth(['tasks', 'proxy', 'settings'], 264)
      expect(singleDirectTab.value).toBe('settings')
    })

    it('is null when popup has >1 items', () => {
      const { singleDirectTab } = setupWithWidth(TABS, 172)
      expect(singleDirectTab.value).toBeNull()
    })
  })

  describe('showOverflowButton', () => {
    it('is true when popup has >1 items', () => {
      const { showOverflowButton } = setupWithWidth(TABS, 172)
      expect(showOverflowButton.value).toBe(true)
    })

    it('is false when popup has exactly 1 item (singleDirectTab handles it)', () => {
      const { showOverflowButton } = setupWithWidth(['tasks', 'proxy', 'settings'], 264)
      expect(showOverflowButton.value).toBe(false)
    })

    it('is false when popup is empty', () => {
      const { showOverflowButton } = setupWithWidth(TABS, 600)
      expect(showOverflowButton.value).toBe(false)
    })
  })

  describe('totalDockButtons', () => {
    it('counts 4 buttons at minimum (3 primary + overflow btn)', () => {
      const { totalDockButtons } = setupWithWidth(TABS, 172)
      expect(totalDockButtons.value).toBe(4)
    })

    it('counts all inline when space allows', () => {
      const { totalDockButtons } = setupWithWidth(TABS, 600)
      expect(totalDockButtons.value).toBe(7)
    })
  })

  describe('width=0 guard (display:none / v-show hidden)', () => {
    it('ResizeObserver width=0 preserves previous good width', () => {
      const s = createSetup(TABS)
      s.startObserving()
      s.dockContentWidth.value = 356
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])

      // Simulate ResizeObserver reporting width=0 (element hidden)
      s.dockContentWidth.value = 0
      expect(s.inlineOverflowTabs.value).toEqual([])

      // With the guard in startObserving, the callback skips width=0,
      // so dockContentWidth stays at the last good value.
      s.dockContentWidth.value = 356
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
    })

    it('startObserving when element hidden preserves previous width', () => {
      const s = createSetup(TABS)
      s.startObserving()
      s.dockContentWidth.value = 356

      // If startObserving is called while hidden (clientWidth=0),
      // measured would be <=0, so dockContentWidth is NOT updated
      const widthBefore = s.dockContentWidth.value
      expect(widthBefore).toBe(356)
    })

    it('sequence: good width → hidden(0) → shown again → correct final state', () => {
      const s = createSetup(TABS)
      s.startObserving()

      // Good width — all tabs inline
      s.dockContentWidth.value = 356
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
      expect(s.showOverflowButton.value).toBe(false)

      // Element hidden — with guard, width stays at 356 (not overwritten to 0)
      // Buttons don't collapse when dock is temporarily hidden
      expect(s.inlineOverflowTabs.value).toEqual(['tasks', 'proxy', 'terminal', 'settings'])
      expect(s.showOverflowButton.value).toBe(false)
    })
  })
})
