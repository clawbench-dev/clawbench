import { describe, it, expect } from 'vitest'
import { computeOtherZoneBodyHeight, projectTintVar, OTHER_ZONE_CAP_RATIO } from '@/utils/sessionOtherZone'

describe('computeOtherZoneBodyHeight', () => {
  it('shows the whole strip when the project list leaves room for it', () => {
    // 600 available, project needs 300, other projects have 200 of content.
    expect(computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 300, otherContentH: 200, rowH: 36,
    })).toBe(200)
  })

  it('gives the project list every pixel it needs before the strip grows', () => {
    // The leftover (80) is less than one row above the floor, so the strip
    // takes exactly the leftover and the project list does not scroll.
    expect(computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 520, otherContentH: 500, rowH: 36,
    })).toBe(80)
  })

  it('keeps a two-row floor when the project list already overflows', () => {
    // Only 10px left over — below the floor, so the floor wins and the
    // project list is the side that scrolls.
    expect(computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 590, otherContentH: 500, rowH: 36,
    })).toBe(72)
  })

  it('never pads beyond its own content', () => {
    expect(computeOtherZoneBodyHeight({
      availH: 900, currentContentH: 100, otherContentH: 30, rowH: 36,
    })).toBe(30)
  })

  it('caps the strip at its share of the list area when the project list is short', () => {
    const capped = computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 100, otherContentH: 5000, rowH: 36,
    })
    expect(capped).toBe(Math.round(600 * OTHER_ZONE_CAP_RATIO))
  })

  it('never exceeds the space left after the project list, even under the cap', () => {
    // 600 avail, project needs 400 -> only 200 can go to the strip, though the
    // cap alone would have allowed 360.
    expect(computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 400, otherContentH: 5000, rowH: 36,
    })).toBe(200)
  })

  it('returns 0 when there is nothing to show', () => {
    expect(computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 100, otherContentH: 0, rowH: 36,
    })).toBe(0)
  })

  it('shows everything in an auto-sized host (no measurable height)', () => {
    expect(computeOtherZoneBodyHeight({
      availH: 0, currentContentH: 0, otherContentH: 250, rowH: 36,
    })).toBe(250)
  })

  it('falls back to the full content when the row height is not measured yet', () => {
    expect(computeOtherZoneBodyHeight({
      availH: 600, currentContentH: 100, otherContentH: 250, rowH: 0,
    })).toBe(250)
  })

  it('rounds to whole pixels', () => {
    expect(computeOtherZoneBodyHeight({
      availH: 600.6, currentContentH: 520.4, otherContentH: 500, rowH: 36,
    })).toBe(80)
  })
})

describe('projectTintVar', () => {
  it('is stable for the same path', () => {
    expect(projectTintVar('/home/me/projects/a')).toBe(projectTintVar('/home/me/projects/a'))
  })

  it('always returns one of the theme tokens', () => {
    const allowed = ['var(--color-purple)', 'var(--color-orange)', 'var(--color-green)', 'var(--color-info)']
    for (const p of ['/a', '/b/c', 'C:\\work\\x', '/很长/中文路径', '']) {
      expect(allowed).toContain(projectTintVar(p))
    }
  })

  it('spreads different paths over more than one tint', () => {
    const tints = new Set(['/a', '/b', '/c', '/d', '/e', '/f', '/g', '/h'].map(projectTintVar))
    expect(tints.size).toBeGreaterThan(1)
  })
})
