import { describe, it, expect, beforeEach } from 'vitest'
import {
  recordBaseline,
  getBaseline,
  clearBaseline,
  clearAllBaselines,
  baselineCount,
  MAX_BASELINE_ENTRIES,
  MAX_BASELINE_BYTES,
} from '@/composables/useFileChangeBaseline.ts'

describe('useFileChangeBaseline', () => {
  beforeEach(() => {
    clearAllBaselines()
  })

  it('records the pre-change content as the baseline on first observation', () => {
    recordBaseline('a.md', 'v1')
    expect(getBaseline('a.md')).toBe('v1')
  })

  it('does not overwrite an existing baseline (accumulates)', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('a.md', 'v2')
    expect(getBaseline('a.md')).toBe('v1')
  })

  it('drops the baseline when content returns to it', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('a.md', 'v1')
    expect(getBaseline('a.md')).toBeNull()
    expect(baselineCount()).toBe(0)
  })

  it('clears a single path', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('b.md', 'x')
    clearBaseline('a.md')
    expect(getBaseline('a.md')).toBeNull()
    expect(getBaseline('b.md')).toBe('x')
  })

  it('evicts the oldest entry past the LRU cap', () => {
    for (let i = 0; i < MAX_BASELINE_ENTRIES + 2; i++) {
      recordBaseline(`f${i}.md`, `content-${i}`)
    }
    expect(baselineCount()).toBe(MAX_BASELINE_ENTRIES)
    expect(getBaseline('f0.md')).toBeNull()
    expect(getBaseline(`f${MAX_BASELINE_ENTRIES + 1}.md`)).toBe(`content-${MAX_BASELINE_ENTRIES + 1}`)
  })

  it('refreshes LRU recency on read', () => {
    for (let i = 0; i < MAX_BASELINE_ENTRIES; i++) {
      recordBaseline(`f${i}.md`, `content-${i}`)
    }
    // Touch f0 so it is no longer the oldest.
    getBaseline('f0.md')
    recordBaseline('new.md', 'new')
    expect(getBaseline('f0.md')).toBe('content-0')
    expect(getBaseline('f1.md')).toBeNull()
  })

  it('refuses to store an oversized baseline', () => {
    const huge = 'x'.repeat(MAX_BASELINE_BYTES + 1)
    recordBaseline('huge.md', huge)
    expect(getBaseline('huge.md')).toBeNull()
    expect(baselineCount()).toBe(0)
  })

  it('clearAllBaselines empties the map', () => {
    recordBaseline('a.md', 'v1')
    recordBaseline('b.md', 'v2')
    clearAllBaselines()
    expect(baselineCount()).toBe(0)
  })
})
