import { describe, expect, it } from 'vitest'
import { computeRecentReferencedFiles, computeHasFileGroups, computeAttachMenuItemCount } from '@/utils/chatInputUtils.ts'

describe('computeRecentReferencedFiles', () => {
  it('returns empty for null messages', () => {
    expect(computeRecentReferencedFiles(null, [], null)).toEqual([])
  })
  it('returns empty for empty messages', () => {
    expect(computeRecentReferencedFiles([], [], null)).toEqual([])
  })
  it('skips non-user messages', () => {
    const msgs = [{ role: 'assistant', files: ['/a.go'] }]
    expect(computeRecentReferencedFiles(msgs, [], null)).toEqual([])
  })
  it('counts file occurrences from string arrays', () => {
    const msgs = [
      { role: 'user', files: ['/a.go', '/b.go'] },
      { role: 'user', files: ['/a.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toHaveLength(2)
    expect(result.find(f => f.path === '/a.go')?.count).toBe(2)
    expect(result.find(f => f.path === '/b.go')?.count).toBe(1)
  })
  it('handles files as objects with path property', () => {
    const msgs = [
      { role: 'user', files: [{ path: '/a.go' }] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toEqual([{ path: '/a.go', count: 1 }])
  })
  it('excludes attached files', () => {
    const msgs = [
      { role: 'user', files: ['/a.go', '/b.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, ['/a.go'], null)
    expect(result).toEqual([{ path: '/b.go', count: 1 }])
  })
  it('excludes current file', () => {
    const msgs = [
      { role: 'user', files: ['/a.go', '/b.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], '/a.go')
    expect(result).toEqual([{ path: '/b.go', count: 1 }])
  })
  it('sorts by count descending', () => {
    const msgs = [
      { role: 'user', files: ['/rare.go'] },
      { role: 'user', files: ['/common.go'] },
      { role: 'user', files: ['/common.go'] },
      { role: 'user', files: ['/common.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result[0].path).toBe('/common.go')
    expect(result[0].count).toBe(3)
    expect(result[1].path).toBe('/rare.go')
    expect(result[1].count).toBe(1)
  })
  it('limits to top 20', () => {
    const msgs = Array.from({ length: 25 }, (_, i) => ({
      role: 'user',
      files: [`/file${i}.go`],
    }))
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toHaveLength(20)
  })
  it('skips files with null/undefined path', () => {
    const msgs = [
      { role: 'user', files: [null, undefined, '/a.go'] as any },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toEqual([{ path: '/a.go', count: 1 }])
  })
  it('skips messages without files', () => {
    const msgs = [
      { role: 'user' },
      { role: 'user', files: ['/a.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toEqual([{ path: '/a.go', count: 1 }])
  })
})

describe('computeHasFileGroups', () => {
  it('returns false when nothing to show', () => {
    expect(computeHasFileGroups(null, null, [], [], 0)).toBe(false)
  })
  it('returns true when current file is not attached', () => {
    expect(computeHasFileGroups('/a.go', null, [], [], 0)).toBe(true)
  })
  it('returns false when current file is already attached', () => {
    expect(computeHasFileGroups('/a.go', null, ['/a.go'], [], 0)).toBe(false)
  })
  it('returns true when current dir is not attached', () => {
    expect(computeHasFileGroups(null, '/src', [], [], 0)).toBe(true)
  })
  it('returns true when recent references exist', () => {
    expect(computeHasFileGroups(null, null, [], [{ path: '/a.go', count: 1 }], 0)).toBe(true)
  })
  it('returns false when current dir is already attached', () => {
    expect(computeHasFileGroups(null, '/src', ['/src'], [], 0)).toBe(false)
  })
  it('returns true when recent shares exist', () => {
    expect(computeHasFileGroups(null, null, [], [], 2)).toBe(true)
  })
})

describe('computeAttachMenuItemCount', () => {
  it('counts upload button only when nothing else', () => {
    expect(computeAttachMenuItemCount(null, null, [], [], 0)).toBe(1)
  })
  it('counts current file + upload button', () => {
    expect(computeAttachMenuItemCount('/a.go', null, [], [], 0)).toBe(2)
  })
  it('counts current dir + upload button', () => {
    expect(computeAttachMenuItemCount(null, '/src', [], [], 0)).toBe(2)
  })
  it('does not count already-attached current file', () => {
    expect(computeAttachMenuItemCount('/a.go', null, ['/a.go'], [], 0)).toBe(1)
  })
  it('counts recent references', () => {
    expect(computeAttachMenuItemCount(null, null, [], [
      { path: '/a.go', count: 1 },
      { path: '/b.go', count: 2 },
    ], 0)).toBe(3) // 2 refs + 1 upload button
  })
  it('counts all groups together', () => {
    expect(computeAttachMenuItemCount('/a.go', '/src', [], [
      { path: '/c.go', count: 1 },
    ], 0)).toBe(4) // 1 current + 1 dir + 1 ref + 1 upload
  })
  it('counts recent shares', () => {
    expect(computeAttachMenuItemCount(null, null, [], [], 3)).toBe(4) // 3 shares + 1 upload
  })
})
