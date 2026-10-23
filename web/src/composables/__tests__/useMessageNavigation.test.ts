import { describe, it, expect, beforeEach } from 'vitest'
import {
  setPendingMessageNavigation,
  consumePendingMessageNavigation,
  pendingMessageNavigation,
  _resetPendingMessageForTesting,
  discardPendingMessageNavigation,
} from '@/composables/useMessageNavigation.ts'

beforeEach(() => {
  _resetPendingMessageForTesting()
})

describe('useMessageNavigation', () => {
  it('parks a request for a later consumer', () => {
    setPendingMessageNavigation('sess-abc', 42)

    expect(pendingMessageNavigation.value).toEqual({ sessionId: 'sess-abc', messageId: 42 })
  })

  // Consuming (rather than reading) is what makes the jump fire exactly once.
  // A read-only accessor would re-scroll the user away on every later render.
  it('clears the request when consumed', () => {
    setPendingMessageNavigation('sess-abc', 42)

    expect(consumePendingMessageNavigation()).toEqual({ sessionId: 'sess-abc', messageId: 42 })
    expect(pendingMessageNavigation.value).toBeNull()
    expect(consumePendingMessageNavigation()).toBeNull()
  })

  // A request with no target is not a request. Storing it would make the chat
  // panel later try to scroll to a message id of 0.
  it('ignores a request missing either half', () => {
    setPendingMessageNavigation('', 42)
    expect(pendingMessageNavigation.value).toBeNull()

    setPendingMessageNavigation('sess-abc', 0)
    expect(pendingMessageNavigation.value).toBeNull()
  })

  it('replaces an earlier request rather than queueing', () => {
    setPendingMessageNavigation('sess-a', 1)
    setPendingMessageNavigation('sess-b', 2)

    // The user's latest click is what they want; queueing would make them sit
    // through jumps they already moved on from.
    expect(consumePendingMessageNavigation()).toEqual({ sessionId: 'sess-b', messageId: 2 })
    expect(consumePendingMessageNavigation()).toBeNull()
  })
})

describe('discardPendingMessageNavigation', () => {
  it('drops a request whose target session is gone', () => {
    // The chat panel only consumes a request when the CURRENT session id
    // matches, so a session that never opens (deleted → 404) leaves the request
    // parked forever. The user later opening an unrelated session that reuses
    // the id would be scrolled to a stale message and told it is missing.
    setPendingMessageNavigation('sess-dead', 7)
    discardPendingMessageNavigation('sess-dead')
    expect(pendingMessageNavigation.value).toBeNull()
  })

  it('keeps a request for a different session', () => {
    // One session failing must not cancel a jump the user asked for elsewhere.
    setPendingMessageNavigation('sess-other', 9)
    discardPendingMessageNavigation('sess-dead')
    expect(pendingMessageNavigation.value).toEqual({ sessionId: 'sess-other', messageId: 9 })
  })
})
