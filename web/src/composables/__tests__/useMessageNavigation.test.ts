import { describe, it, expect, beforeEach } from 'vitest'
import {
  setPendingMessageNavigation,
  consumePendingMessageNavigation,
  pendingMessageNavigation,
  _resetPendingMessageForTesting,
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
