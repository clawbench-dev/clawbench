import { describe, expect, it, beforeEach } from 'vitest'
import { useSessionShare } from '@/composables/useSessionShare'

/**
 * The module-level share set backs the session-row badge and the drawer. It is
 * a singleton, so every test resets it first — otherwise state leaks between
 * tests and a "badge absent" assertion could pass for the wrong reason.
 */
describe('useSessionShare', () => {
  const { markShared, markUnshared, isSessionShared, hasAnySharedSession, setSharedSessionIds, resetSessionShareState } = useSessionShare()

  beforeEach(() => {
    resetSessionShareState()
  })

  it('reports a session as unshared initially', () => {
    expect(isSessionShared('s1')).toBe(false)
  })

  // Drives whether the management button is rendered at all.
  describe('hasAnySharedSession', () => {
    it('is false when nothing is shared', () => {
      expect(hasAnySharedSession.value).toBe(false)
    })

    it('is true as soon as one session is shared', () => {
      markShared('s1')
      expect(hasAnySharedSession.value).toBe(true)
    })

    // The button must disappear again once the last share goes, otherwise it
    // would linger with an empty drawer behind it.
    it('goes false again when the last share is removed', () => {
      markShared('s1')
      markUnshared('s1')
      expect(hasAnySharedSession.value).toBe(false)
    })

    it('stays true while other shares remain', () => {
      markShared('s1')
      markShared('s2')
      markUnshared('s1')
      expect(hasAnySharedSession.value).toBe(true)
    })

    it('tracks the authoritative list on replace', () => {
      setSharedSessionIds(['a', 'b'])
      expect(hasAnySharedSession.value).toBe(true)

      setSharedSessionIds([])
      expect(hasAnySharedSession.value).toBe(false)
    })

    // The list is the sole seed, so an empty result must not read as "unknown".
    it('is false after an empty list is loaded', () => {
      setSharedSessionIds([])
      expect(hasAnySharedSession.value).toBe(false)
    })
  })

  it('marks a session shared and unshared', () => {
    markShared('s1')
    expect(isSessionShared('s1')).toBe(true)

    markUnshared('s1')
    expect(isSessionShared('s1')).toBe(false)
  })

  it('ignores empty ids', () => {
    markShared('')
    expect(isSessionShared('')).toBe(false)
    // An empty id must never enter the set: isSessionShared('') would then be
    // true for every caller that forgot to guard.
    expect(isSessionShared('s1')).toBe(false)
  })

  // setSharedSessionIds REPLACES the set, so a revoked share cannot linger from
  // a previous fetch.
  it('replaces the whole set', () => {
    markShared('stale')
    setSharedSessionIds(['a', 'b'])
    expect(isSessionShared('stale')).toBe(false)
    expect(isSessionShared('a')).toBe(true)
    expect(isSessionShared('b')).toBe(true)
  })

  it('skips empty entries when replacing the set', () => {
    setSharedSessionIds(['a', '', 'b'])
    expect(isSessionShared('a')).toBe(true)
    expect(isSessionShared('b')).toBe(true)
    expect(isSessionShared('')).toBe(false)
  })

  it('clears everything on reset', () => {
    markShared('s1')
    markShared('s2')
    resetSessionShareState()
    expect(isSessionShared('s1')).toBe(false)
    expect(isSessionShared('s2')).toBe(false)
  })
})
