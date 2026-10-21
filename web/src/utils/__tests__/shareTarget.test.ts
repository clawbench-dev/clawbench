import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { consumeShareTargetResult, stageShareTargetResult, SHARE_TARGET_PARAM } from '../shareTarget'

// jsdom's default URL is http://localhost:3000/. These helpers set the query
// string the way the backend's 303 redirect would, so the parse path is
// exercised through window.location rather than through a private helper.
function setSearch(search: string) {
  window.history.replaceState(null, '', `/${search}`)
}

describe('consumeShareTargetResult', () => {
  beforeEach(() => setSearch(''))
  afterEach(() => setSearch(''))

  it('returns null when the parameter is absent', () => {
    expect(consumeShareTargetResult()).toBeNull()
  })

  it('parses a JSON array of stored paths', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('["a.png","b.png"]')}`)
    expect(consumeShareTargetResult()).toEqual({ paths: ['a.png', 'b.png'], failed: 0 })
  })

  it('treats an empty array as "nothing was stored", not success', () => {
    // A success with zero files would silently do nothing on screen.
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('[]')}`)
    expect(consumeShareTargetResult()).toEqual({ paths: [], failure: 'empty', failed: 0 })
  })

  it('drops non-string entries inside the array', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('["a.png",null,42,""]')}`)
    expect(consumeShareTargetResult()).toEqual({ paths: ['a.png'], failed: 0 })
  })

  it('maps the known failure reasons', () => {
    for (const reason of ['empty', 'save', 'invalid'] as const) {
      setSearch(`?${SHARE_TARGET_PARAM}=${reason}`)
      expect(consumeShareTargetResult()).toEqual({ paths: [], failure: reason, failed: 0 })
    }
  })

  it('treats an unknown value as invalid rather than guessing', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=something-else`)
    expect(consumeShareTargetResult()).toEqual({ paths: [], failure: 'invalid', failed: 0 })
  })

  it('treats malformed JSON as invalid instead of throwing', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('["a.png"')}`)
    // One call only: consuming also strips the parameter, so a second call
    // would legitimately return null and the assertion would test nothing.
    let result
    expect(() => { result = consumeShareTargetResult() }).not.toThrow()
    expect(result).toEqual({ paths: [], failure: 'invalid', failed: 0 })
  })

  it('treats a blank value as empty', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=`)
    expect(consumeShareTargetResult()).toEqual({ paths: [], failure: 'empty', failed: 0 })
  })

  it('strips the parameter so a reload does not re-attach the same files', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('["a.png"]')}&other=1`)
    consumeShareTargetResult()

    const params = new URLSearchParams(window.location.search)
    expect(params.has(SHARE_TARGET_PARAM)).toBe(false)
    // Unrelated parameters must survive the rewrite.
    expect(params.get('other')).toBe('1')
  })

  it('strips the parameter even when parsing fails', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('not json at all')}`)
    consumeShareTargetResult()
    expect(new URLSearchParams(window.location.search).has(SHARE_TARGET_PARAM)).toBe(false)
  })

  it('is idempotent: a second call finds nothing', () => {
    setSearch(`?${SHARE_TARGET_PARAM}=${encodeURIComponent('["a.png"]')}`)
    expect(consumeShareTargetResult()).not.toBeNull()
    expect(consumeShareTargetResult()).toBeNull()
  })
})

describe('stageShareTargetResult', () => {
  /** Records what was attached; `already` simulates an existing card. */
  function makeAttach(already: string[] = []) {
    const attached: string[] = []
    const attach = (p: string) => {
      if (already.includes(p)) return false
      attached.push(p)
      return true
    }
    return { attach, attached }
  }

  it('attaches every path and reports the count', () => {
    const { attach, attached } = makeAttach()
    const out = stageShareTargetResult({ paths: ['a.png', 'b.png'], failed: 0 }, attach)

    expect(attached).toEqual(['a.png', 'b.png'])
    expect(out.toastKey).toBe('chat.attach.shareTargetAdded')
    expect(out.toastParams).toEqual({ n: 2 })
    expect(out.variant.type).toBe('success')
  })

  it('maps each failure reason to its own message', () => {
    const cases = {
      empty: 'chat.attach.shareTargetEmpty',
      save: 'chat.attach.shareTargetSaveFailed',
      invalid: 'chat.attach.shareTargetInvalid',
    } as const
    for (const [reason, key] of Object.entries(cases)) {
      const { attach, attached } = makeAttach()
      const out = stageShareTargetResult({ paths: [], failure: reason as keyof typeof cases, failed: 0 }, attach)
      expect(out.toastKey).toBe(key)
      expect(out.variant.type).toBe('error')
      // Nothing is attached when the server stored nothing.
      expect(attached).toEqual([])
    }
  })

  it('says "already attached" when every path was present', () => {
    const { attach } = makeAttach(['a.png'])
    const out = stageShareTargetResult({ paths: ['a.png'], failed: 0 }, attach)
    expect(out.toastKey).toBe('chat.attach.alreadyAttached')
  })

  it('reports a partial batch instead of claiming full success', () => {
    // The whole point: 2 of 5 files made it, and the message must say so.
    const { attach, attached } = makeAttach()
    const out = stageShareTargetResult({ paths: ['a.png', 'b.png'], failed: 3 }, attach)

    expect(attached).toHaveLength(2)
    expect(out.toastKey).toBe('chat.attach.shareTargetPartial')
    expect(out.toastParams).toEqual({ n: 2, failed: 3 })
    // Not "success": the user has to notice something was dropped.
    expect(out.variant.type).toBe('error')
  })

  it('counts only the files it actually attached', () => {
    // One of the two was already present, so n must be 1, not 2.
    const { attach } = makeAttach(['a.png'])
    const out = stageShareTargetResult({ paths: ['a.png', 'b.png'], failed: 0 }, attach)
    expect(out.toastParams).toEqual({ n: 1 })
  })

  it('always returns a toast — a silent share reads as a no-op', () => {
    const cases: Parameters<typeof stageShareTargetResult>[0][] = [
      { paths: [], failure: 'empty', failed: 0 },
      { paths: [], failure: 'save', failed: 0 },
      { paths: [], failure: 'invalid', failed: 0 },
      { paths: ['a.png'], failed: 0 },
      { paths: ['a.png'], failed: 1 },
    ]
    for (const result of cases) {
      const { attach } = makeAttach()
      const out = stageShareTargetResult(result, attach)
      expect(out.toastKey, JSON.stringify(result)).toMatch(/^chat\.attach\./)
    }
  })
})

describe('stageShareTargetResult — already-attached vs failed', () => {
  it('does not let "already attached" hide a failed file', () => {
    // Every path was already present AND one file was dropped. Reporting only
    // "already attached" would silently swallow the failure.
    const out = stageShareTargetResult({ paths: ['a.png'], failed: 1 }, () => false)
    expect(out.toastKey).toBe('chat.attach.shareTargetPartial')
    expect(out.toastParams).toEqual({ n: 0, failed: 1 })
    expect(out.variant.type).toBe('error')
  })
})
