import { describe, expect, it, beforeEach } from 'vitest'
import {
  pendingForgeNavigation,
  setPendingForgeNavigation,
  consumePendingForgeNavigation,
  _resetPendingForgeNavigationForTesting,
} from '@/composables/useForgeNavigation'

describe('useForgeNavigation', () => {
  beforeEach(() => {
    _resetPendingForgeNavigationForTesting()
  })

  it('starts with nothing pending', () => {
    expect(pendingForgeNavigation.value).toBeNull()
  })

  it('exposes the request reactively', () => {
    // Reactive, not a plain variable: switchTab() no-ops when the target tab is
    // already showing, so the consumer needs a change to react to.
    setPendingForgeNavigation({ type: 'pr', number: 455, runId: 0 })
    expect(pendingForgeNavigation.value).toEqual({ type: 'pr', number: 455, runId: 0 })
  })

  it('consume clears the pending request', () => {
    setPendingForgeNavigation({ type: 'pipeline', number: 0, runId: 555 })

    const got = consumePendingForgeNavigation()
    expect(got).toEqual({ type: 'pipeline', number: 0, runId: 555 })

    // Clearing is what makes it one-shot: without this the consumer would
    // re-open the same item every time the tab is activated.
    expect(pendingForgeNavigation.value).toBeNull()
    expect(consumePendingForgeNavigation()).toBeNull()
  })

  it('a later request replaces an unconsumed one', () => {
    setPendingForgeNavigation({ type: 'issue', number: 1, runId: 0 })
    setPendingForgeNavigation({ type: 'pr', number: 2, runId: 0 })

    expect(consumePendingForgeNavigation()).toEqual({ type: 'pr', number: 2, runId: 0 })
  })
})
