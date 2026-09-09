import { describe, expect, it } from 'vitest'
import {
  activeEntryIndex,
  centeredScrollTop,
} from '../planScroll'
import type { PlanEntry } from '@/composables/usePlanProgress'

const mk = (status: PlanEntry['status'], priority: PlanEntry['priority'] = 'medium'): PlanEntry =>
  ({ content: 'x', status, priority })

describe('activeEntryIndex', () => {
  it('returns -1 for an empty list', () => {
    expect(activeEntryIndex([])).toBe(-1)
  })

  it('returns -1 when no entry is in_progress', () => {
    const entries = [mk('completed'), mk('pending'), mk('pending')]
    expect(activeEntryIndex(entries)).toBe(-1)
  })

  it('returns the index of the in_progress entry', () => {
    const entries = [mk('completed'), mk('in_progress'), mk('pending')]
    expect(activeEntryIndex(entries)).toBe(1)
  })
})

describe('centeredScrollTop', () => {
  it('centers a row that lies below the viewport center', () => {
    // Container 200px tall, content 1000px, row center at 700px from the top.
    // scrollTop starts at 0 → target = 0 + 700 - 100 = 600.
    expect(centeredScrollTop({ scrollTop: 0, rowCenter: 700, containerHeight: 200, maxScrollTop: 800 })).toBe(600)
  })

  it('centers a row that currently lies below the viewport', () => {
    // User has scrolled to 500 (viewport 500…700 in a 200px container); the
    // active row's center is 300px below the viewport top, i.e. document y=800.
    // Centering requires viewport top = 800 − 100 = 700.
    expect(centeredScrollTop({ scrollTop: 500, rowCenter: 300, containerHeight: 200, maxScrollTop: 800 })).toBe(700)
  })

  it('never scrolls negative', () => {
    expect(centeredScrollTop({ scrollTop: 0, rowCenter: 50, containerHeight: 200, maxScrollTop: 800 })).toBe(0)
    // A row near the top of a slightly-scrolled container produces a negative
    // target → clamped to 0.
    expect(centeredScrollTop({ scrollTop: 10, rowCenter: 14, containerHeight: 200, maxScrollTop: 800 })).toBe(0)
  })

  it('clamps to the max scrollable offset', () => {
    expect(centeredScrollTop({ scrollTop: 0, rowCenter: 1000, containerHeight: 200, maxScrollTop: 800 })).toBe(800)
  })

  it('handles a non-scrollable container (maxScrollTop 0)', () => {
    expect(centeredScrollTop({ scrollTop: 0, rowCenter: 40, containerHeight: 100, maxScrollTop: 0 })).toBe(0)
  })
})
