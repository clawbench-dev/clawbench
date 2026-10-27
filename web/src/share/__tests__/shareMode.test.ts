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
  shareDocUrl,
  encodeShareDocDir,
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

describe('shareDocUrl / encodeShareDocDir', () => {
  afterEach(() => setShareToken(null))

  it('builds a path-addressed URL under the token', () => {
    setShareToken('tok1')
    expect(shareDocUrl('KEY', 'index.html')).toBe('/api/share/tok1/doc/KEY/index.html')
    // No rel → the directory key alone (the share SPA always supplies a file,
    // but the builder must not emit a stray double slash).
    expect(shareDocUrl('KEY')).toBe('/api/share/tok1/doc/KEY/')
  })

  it('encodes the directory as unpadded base64url', () => {
    // Vectors pinned against Go's base64.RawURLEncoding: the share SPA and
    // serveShareDoc must produce identical bytes or every relative asset 404s.
    expect(encodeShareDocDir('/tmp')).toBe('L3RtcA')
    expect(encodeShareDocDir('/')).toBe('Lw')
    expect(encodeShareDocDir('/a b/c')).toBe('L2EgYi9j')
    for (const key of ['L3RtcA', 'Lw', 'L2EgYi9j']) {
      // A path segment must not carry padding, '+', or '/'.
      expect(key).not.toMatch(/[=+/]/)
    }
  })

  it('round-trips a non-ASCII directory', () => {
    const dir = '/项目/子目录'
    const key = encodeShareDocDir(dir)
    expect(key).not.toMatch(/[=+/]/)
    // btoa cannot take non-Latin1 directly, so the encoder must go through
    // UTF-8 bytes — decoding the base64 must give back the same UTF-8 string.
    const binary = atob(key.replace(/-/g, '+').replace(/_/g, '/'))
    const bytes = Uint8Array.from(binary, c => c.charCodeAt(0))
    expect(new TextDecoder().decode(bytes)).toBe(dir)
  })
})
