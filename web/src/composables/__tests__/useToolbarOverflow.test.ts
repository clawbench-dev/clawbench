import { afterEach, describe, expect, it, vi } from 'vitest'
import { useToolbarOverflow } from '@/composables/useToolbarOverflow'
import { applyUIScale, UI_SCALE_EVENT } from '@/utils/uiScale'

// Mock ResizeObserver
const mockObserve = vi.fn()
const mockDisconnect = vi.fn()
vi.stubGlobal('ResizeObserver', class {
  observe = mockObserve
  disconnect = mockDisconnect
})

describe('useToolbarOverflow', () => {
  function createSetup(demotableIds: string[], opts?: Parameters<typeof useToolbarOverflow>[2]) {
    const el = document.createElement('div')
    el.style.paddingLeft = '4px'
    el.style.paddingRight = '4px'

    const result = useToolbarOverflow(
      () => el,
      () => demotableIds,
      opts,
    )
    return { el, ...result }
  }

  function setupWithWidth(demotableIds: string[], width: number, opts?: Parameters<typeof useToolbarOverflow>[2]) {
    const s = createSetup(demotableIds, opts)
    s.startObserving()
    s.contentWidth.value = width
    return s
  }

  describe('inlineIds', () => {
    it('returns all demotable IDs when width is ample', () => {
      const { inlineIds } = setupWithWidth(['hidden', 'refresh', 'multiselect'], 600, { inlineCount: 2, hasSearch: true })
      expect(inlineIds.value).toEqual(['hidden', 'refresh', 'multiselect'])
    })

    it('returns partial IDs when width is limited', () => {
      const { inlineIds } = setupWithWidth(['hidden', 'refresh', 'multiselect'], 250, { inlineCount: 2, hasSearch: true })
      // 250 - 2*26 (inline btns) - 1*6 (gap between inline btns) - 140 (search) - 6 (bridge gap) = 46px → floor(46/32) = 1 demotable button
      expect(inlineIds.value.length).toBeGreaterThanOrEqual(1)
      expect(inlineIds.value.length).toBeLessThan(3)
    })

    it('returns empty when no space for demotable buttons', () => {
      const { inlineIds } = setupWithWidth(['hidden', 'refresh'], 100, { inlineCount: 2, hasSearch: true })
      expect(inlineIds.value).toEqual([])
    })

    it('returns all demotable IDs when not yet measured', () => {
      const { inlineIds } = createSetup(['hidden', 'refresh'])
      // contentWidth is 0 → returns all (no overflow until measured)
      expect(inlineIds.value).toEqual(['hidden', 'refresh'])
    })
  })

  describe('collapsedIds', () => {
    it('returns empty when all fit inline', () => {
      const { collapsedIds } = setupWithWidth(['hidden', 'refresh'], 600, { inlineCount: 2, hasSearch: true })
      expect(collapsedIds.value).toEqual([])
    })

    it('returns IDs that dont fit inline', () => {
      const { inlineIds, collapsedIds } = setupWithWidth(['hidden', 'refresh', 'multiselect'], 250, { inlineCount: 2, hasSearch: true })
      // inlineIds + collapsedIds = all demotable IDs
      expect([...inlineIds.value, ...collapsedIds.value]).toEqual(['hidden', 'refresh', 'multiselect'])
      expect(collapsedIds.value.length).toBeGreaterThan(0)
    })
  })

  describe('without search', () => {
    it('fits more buttons when no search input', () => {
      const { inlineIds: withSearch } = setupWithWidth(['a', 'b', 'c'], 200, { inlineCount: 1, hasSearch: true })
      const { inlineIds: noSearch } = setupWithWidth(['a', 'b', 'c'], 200, { inlineCount: 1, hasSearch: false })
      expect(noSearch.value.length).toBeGreaterThanOrEqual(withSearch.value.length)
    })
  })

  describe('custom btnWidth and gap', () => {
    it('respects custom button size', () => {
      const { inlineIds } = setupWithWidth(['a', 'b'], 120, { btnWidth: 30, gap: 8, inlineCount: 1, hasSearch: true })
      expect(inlineIds.value.length).toBeLessThanOrEqual(2)
    })
  })

  describe('startObserving / stopObserving', () => {
    it('starts and stops ResizeObserver', () => {
      const { startObserving, stopObserving } = createSetup(['a'])
      startObserving()
      expect(mockObserve).toHaveBeenCalled()
      stopObserving()
      expect(mockDisconnect).toHaveBeenCalled()
    })

    it('stops listening for scale changes on stopObserving', () => {
      // The listener used to be module-level and never removed.
      const remove = vi.spyOn(window, 'removeEventListener')
      const { startObserving, stopObserving } = createSetup(['a'])
      startObserving()
      remove.mockClear()
      stopObserving()
      expect(remove.mock.calls.filter(([type]) => type === UI_SCALE_EVENT)).toHaveLength(1)
      remove.mockRestore()
    })
  })

  describe('the button footprint follows the UI scale', () => {
    afterEach(() => {
      applyUIScale(1)
    })

    it('fits fewer buttons at a larger scale for the same measured width', () => {
      // 200px, 1 always-inline button, no search:
      //   100%: 200 - 26 - 6 = 168 → floor(168/32) = 5 → all 3
      //   200%: 200 - 52 - 12 = 136 → floor(136/64) = 2 → 2 of 3
      const at100 = setupWithWidth(['a', 'b', 'c'], 200, { inlineCount: 1 })
      expect(at100.inlineIds.value).toEqual(['a', 'b', 'c'])

      applyUIScale(2)
      const at200 = setupWithWidth(['a', 'b', 'c'], 200, { inlineCount: 1 })
      expect(at200.inlineIds.value).toEqual(['a', 'b'])
    })

    it('recomputes when the scale changes without a new measurement', () => {
      const s = setupWithWidth(['a', 'b', 'c'], 200, { inlineCount: 1 })
      expect(s.inlineIds.value).toEqual(['a', 'b', 'c'])
      applyUIScale(2)
      expect(s.inlineIds.value).toEqual(['a', 'b'])
    })
  })
})
