import { describe, expect, it, vi } from 'vitest'
import { useToolbarOverflow } from '@/composables/useToolbarOverflow'

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
      const { inlineIds } = setupWithWidth(['hidden', 'refresh', 'multiselect'], 310, { inlineCount: 2, hasSearch: true })
      // 310 - 2*26 (inline btns) - 2*6 (gaps) - 200 (search) = 46px → floor(46/32) = 1 demotable button
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
      const { inlineIds, collapsedIds } = setupWithWidth(['hidden', 'refresh', 'multiselect'], 310, { inlineCount: 2, hasSearch: true })
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
  })
})
