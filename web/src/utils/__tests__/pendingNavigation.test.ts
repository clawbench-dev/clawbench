import { describe, expect, it } from 'vitest'
import { decidePendingNavigation, jumpFailureMessageKey } from '@/utils/pendingNavigation'

/**
 * The chat panel's pending-navigation consumer, extracted as a pure decision so
 * every branch can be asserted directly. These are the branches that broke
 * during issue #580's development:
 *
 *  - consuming while a session switch is in flight searched an empty array,
 *    concluded the message was gone and showed a spurious failure toast;
 *  - the failure toast used the search wording for quote/notification jumps.
 */
describe('decidePendingNavigation', () => {
  const req = (over: Partial<{ sessionId: string; messageId: number; source: 'search' | 'quote' | 'notification' }> = {}) => ({
    sessionId: 's1',
    messageId: 42,
    source: 'search' as const,
    ...over,
  })

  it('does nothing when no request is parked', () => {
    expect(decidePendingNavigation(null, 's1', false)).toEqual({ kind: 'skip', reason: 'none' })
  })

  it('waits while the request targets another session', () => {
    // Acting early would search the wrong session's list and silently do
    // nothing — the request must survive until its session is on screen.
    expect(decidePendingNavigation(req({ sessionId: 's2' }), 's1', false))
      .toEqual({ kind: 'skip', reason: 'other-session' })
  })

  it('waits while a session switch is in flight', () => {
    // switchSession clears the array synchronously and refetches; consuming now
    // would report "message is gone" for a message that is merely not loaded.
    expect(decidePendingNavigation(req(), 's1', true))
      .toEqual({ kind: 'skip', reason: 'switching' })
  })

  it('jumps once the target session is current and the switch has settled', () => {
    expect(decidePendingNavigation(req(), 's1', false))
      .toEqual({ kind: 'jump', messageId: 42, source: 'search' })
  })

  it('jumps even when the target message is not loaded yet', () => {
    // The message id is not part of the decision on purpose: jumpToUserMessage
    // pages older history in. Requiring it to be present first is the bug that
    // made a search hit for an older message do nothing.
    expect(decidePendingNavigation(req({ messageId: 999999 }), 's1', false).kind).toBe('jump')
  })

  it('carries the source through so the caller can pick the right copy', () => {
    expect(decidePendingNavigation(req({ source: 'quote' }), 's1', false))
      .toMatchObject({ source: 'quote' })
    expect(decidePendingNavigation(req({ source: 'notification' }), 's1', false))
      .toMatchObject({ source: 'notification' })
    // Absent source (older callers) must not be mistaken for a search.
    const noSource = decidePendingNavigation({ sessionId: 's1', messageId: 1 }, 's1', false)
    expect(noSource).toEqual({ kind: 'jump', messageId: 1, source: undefined })
  })
})

describe('jumpFailureMessageKey', () => {
  it('only claims a search for search-sourced jumps', () => {
    expect(jumpFailureMessageKey('search')).toBe('sessionSearch.jumpFailed')
    // Quote and notification jumps share the channel; telling the reader "that
    // search result is gone" when they clicked a quote would be wrong.
    expect(jumpFailureMessageKey('quote')).toBe('sessionSearch.jumpFailedGeneric')
    expect(jumpFailureMessageKey('notification')).toBe('sessionSearch.jumpFailedGeneric')
    expect(jumpFailureMessageKey(undefined)).toBe('sessionSearch.jumpFailedGeneric')
  })
})
