import { describe, it, expect } from 'vitest'
import { planRemoteRemoval } from '../useRemoteSessionRemoval'

/**
 * The "my session was archived on another device" decision.
 *
 * App.vue has no component test, so this is where the behaviour is pinned: the
 * handler must leave a ghost session, but must never drag the user off a session
 * they opened in the meantime, and must fall back to creating one exactly like a
 * local archive of the last session does.
 */
describe('planRemoteRemoval', () => {
  it('leaves a session that is not the one open in this client', () => {
    expect(planRemoteRemoval('s-other', 's-open', [{ id: 's-open' }]))
      .toEqual({ action: 'none' })
  })

  it('switches to the first session the server still reports', () => {
    expect(planRemoteRemoval('s-open', 's-open', [{ id: 's-a' }, { id: 's-b' }]))
      .toEqual({ action: 'switch', sessionId: 's-a' })
  })

  it('creates a session when the project has none left', () => {
    // Same outcome as archiving the last session locally: the user must never
    // be left staring at an empty chat panel with no session behind it.
    expect(planRemoteRemoval('s-open', 's-open', []))
      .toEqual({ action: 'create' })
  })

  it('never selects the removed session, even when a stale list still contains it', () => {
    // A cached/partial response could still list the session the server just
    // reported as removed. Switching back into it would land on a panel whose
    // every request 404s.
    expect(planRemoteRemoval('s-open', 's-open', [{ id: 's-open' }, { id: 's-kept' }]))
      .toEqual({ action: 'switch', sessionId: 's-kept' })
  })

  it('creates a session when the only row listed is the removed one', () => {
    expect(planRemoteRemoval('s-open', 's-open', [{ id: 's-open' }]))
      .toEqual({ action: 'create' })
  })

  it('ignores rows with no id', () => {
    expect(planRemoteRemoval('s-open', 's-open', [{ id: '' }, { id: 's-real' }]))
      .toEqual({ action: 'switch', sessionId: 's-real' })
  })

  it('does nothing for an empty removal id (malformed event)', () => {
    expect(planRemoteRemoval('', 's-open', [{ id: 's-a' }]))
      .toEqual({ action: 'none' })
  })

  it('does nothing when no session is open', () => {
    // Nothing to leave: the panel is already empty and the list refresh the
    // caller does separately is the whole job.
    expect(planRemoteRemoval('s-open', '', [{ id: 's-a' }]))
      .toEqual({ action: 'none' })
  })
})
