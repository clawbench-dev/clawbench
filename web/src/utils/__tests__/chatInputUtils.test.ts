import { describe, expect, it } from 'vitest'
import { computeRecentReferencedFiles, isImeCompositionEvent } from '@/utils/chatInputUtils.ts'

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
  it('handles files as FileEntry objects', () => {
    const msgs = [
      { role: 'user', files: [{ path: '/a.go', isDir: false }] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toEqual([{ path: '/a.go', count: 1, isDir: false }])
  })
  it('excludes attached files', () => {
    const msgs = [
      { role: 'user', files: ['/a.go', '/b.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [{ path: '/a.go', isDir: false }], null)
    expect(result).toEqual([{ path: '/b.go', count: 1, isDir: false }])
  })
  it('excludes current file', () => {
    const msgs = [
      { role: 'user', files: ['/a.go', '/b.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], '/a.go')
    expect(result).toEqual([{ path: '/b.go', count: 1, isDir: false }])
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
    expect(result).toEqual([{ path: '/a.go', count: 1, isDir: false }])
  })
  it('skips messages without files', () => {
    const msgs = [
      { role: 'user' },
      { role: 'user', files: ['/a.go'] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toEqual([{ path: '/a.go', count: 1, isDir: false }])
  })
  it('propagates isDir for directory references', () => {
    const msgs = [
      { role: 'user', files: [{ path: '/src', isDir: true }, { path: '/a.go', isDir: false }] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result.find(f => f.path === '/src')?.isDir).toBe(true)
    expect(result.find(f => f.path === '/a.go')?.isDir).toBe(false)
  })
  it('keeps isDir true when a directory is referenced more than once', () => {
    const msgs = [
      { role: 'user', files: [{ path: '/src', isDir: true }] },
      { role: 'user', files: [{ path: '/src', isDir: true }] },
    ]
    const result = computeRecentReferencedFiles(msgs, [], null)
    expect(result).toEqual([{ path: '/src', count: 2, isDir: true }])
  })
})

describe('isImeCompositionEvent', () => {
  it('returns true when isComposing is set', () => {
    expect(isImeCompositionEvent({ isComposing: true })).toBe(true)
  })
  it('returns true when keyCode is 229 (WebKit fallback)', () => {
    expect(isImeCompositionEvent({ isComposing: false, keyCode: 229 })).toBe(true)
    expect(isImeCompositionEvent({ keyCode: 229 })).toBe(true)
  })
  it('returns false for a normal Enter keystroke', () => {
    expect(isImeCompositionEvent({ isComposing: false, keyCode: 13 })).toBe(false)
    expect(isImeCompositionEvent({})).toBe(false)
    expect(isImeCompositionEvent({ isComposing: false })).toBe(false)
  })
})
