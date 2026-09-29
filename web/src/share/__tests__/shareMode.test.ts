import { describe, expect, it, afterEach } from 'vitest'
import {
  isShareMode,
  setShareToken,
  setSharedFile,
  getShareToken,
  getSharedFilePath,
  getSharedFileName,
  shareApiUrl,
  setShareSessionData,
  clearShareSessionData,
  getShareThinking,
  getShareToolCall,
  shareDataKey,
} from '@/share/shareMode'

describe('shareMode', () => {
  afterEach(() => {
    setShareToken(null)
    setSharedFile('', '')
    clearShareSessionData()
  })

  it('is off by default and toggles with the token', () => {
    expect(isShareMode()).toBe(false)
    expect(getShareToken()).toBeNull()
    setShareToken('tok1')
    expect(isShareMode()).toBe(true)
    expect(getShareToken()).toBe('tok1')
    setShareToken('')
    expect(isShareMode()).toBe(false)
  })

  it('builds token-scoped API URLs without double slashes', () => {
    setShareToken('tok1')
    expect(shareApiUrl('file')).toBe('/api/share/tok1/file')
    expect(shareApiUrl('/file')).toBe('/api/share/tok1/file')
    expect(shareApiUrl('local/img/a.png')).toBe('/api/share/tok1/local/img/a.png')
    expect(shareApiUrl('download')).toBe('/api/share/tok1/download')
  })

  it('throws when building a URL outside share mode', () => {
    expect(() => shareApiUrl('file')).toThrow()
  })

  it('stores shared file metadata', () => {
    expect(getSharedFilePath()).toBe('')
    setSharedFile('/proj/a.md', 'a.md')
    expect(getSharedFilePath()).toBe('/proj/a.md')
    expect(getSharedFileName()).toBe('a.md')
  })
})

// The provider is what keeps the chat render chain from calling the
// authenticated detail endpoints on an anonymous share page. Every lookup must
// be a miss outside session-share mode, which is what leaves normal chat
// behavior byte-for-byte unchanged.
describe('shareMode session-share data provider', () => {
  afterEach(() => {
    clearShareSessionData()
  })

  it('misses for both maps by default (normal chat is unaffected)', () => {
    expect(getShareThinking(1, 'th_1')).toBeUndefined()
    expect(getShareToolCall(1, 'toolu_1')).toBeUndefined()
  })

  it('returns seeded thinking text and tool calls', () => {
    setShareSessionData(
      new Map([['7:th_1', 'deep thoughts']]),
      new Map([['7:toolu_1', { input: { file_path: './a.ts' }, output: 'contents', status: 'success', done: true }]])
    )

    expect(getShareThinking(7, 'th_1')).toBe('deep thoughts')
    const call = getShareToolCall(7, 'toolu_1')
    expect(call?.input).toEqual({ file_path: './a.ts' })
    expect(call?.output).toBe('contents')
    expect(call?.done).toBe(true)
  })

  it('keys on both message id and block id', () => {
    setShareSessionData(new Map([['7:th_1', 'text']]), new Map())
    // Same think_id under a different message must not resolve: think_id is only
    // unique per message.
    expect(getShareThinking(8, 'th_1')).toBeUndefined()
    expect(getShareThinking(7, 'th_2')).toBeUndefined()
  })

  it('accepts numeric and string message ids interchangeably', () => {
    setShareSessionData(new Map([['7:th_1', 'text']]), new Map())
    expect(getShareThinking('7', 'th_1')).toBe('text')
    expect(getShareThinking(7, 'th_1')).toBe('text')
  })

  it('clears back to a miss', () => {
    setShareSessionData(new Map([['1:th_1', 'x']]), new Map([['1:t1', { output: 'y' }]]))
    expect(getShareThinking(1, 'th_1')).toBe('x')

    clearShareSessionData()
    expect(getShareThinking(1, 'th_1')).toBeUndefined()
    expect(getShareToolCall(1, 't1')).toBeUndefined()
  })

  it('treats an empty block id as a miss (never a wildcard match)', () => {
    setShareSessionData(new Map(), new Map([['1:', { output: 'oops' }]]))
    expect(getShareToolCall(1, '')).toBeUndefined()
    expect(getShareThinking(1, '')).toBeUndefined()
  })

  it('builds the composite key the same way callers do', () => {
    expect(shareDataKey(7, 'th_1')).toBe('7:th_1')
    expect(shareDataKey('7', 'th_1')).toBe('7:th_1')
  })
})
