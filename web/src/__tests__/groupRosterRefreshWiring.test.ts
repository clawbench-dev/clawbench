import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring guard for the group-roster refresh hook.
 *
 * A forked group's members are brand-new rows whose avatars are generated
 * ASYNCHRONOUSLY after the session switch, so the roster fetched on entry
 * predates them. Without a second refresh the fallback icons stick until the
 * user switches away and back — the reported symptom ("切回就有自定义头像，
 * 刚打开是默认头像").
 *
 * useChatSession cannot reach the roster directly: App.vue owns the single
 * useGroupMembers instance (the chat header and the settings sheet both read
 * it), so the refresher travels through a registered hook. A behaviour test
 * covers fork's side of the contract; these assertions cover the OTHER half —
 * that App.vue actually registers a refresher. If that registration is dropped,
 * refreshGroupRosterIfRegistered becomes a silent no-op and every fork test
 * still passes.
 */
describe('group roster refresh wiring', () => {
  const app = readWebFile('src/App.vue')

  it('registers the refresher with the identity module', () => {
    expect(app).toMatch(/registerGroupRosterRefresh\(refreshGroupMembers\)/)
  })

  it('imports the registration helper from useSessionIdentity', () => {
    // A stale/renamed import would make the call above a ReferenceError at
    // runtime rather than a silent no-op, but pinning it keeps the two halves
    // of the wiring in one place.
    expect(app).toMatch(/import \{[^}]*registerGroupRosterRefresh[^}]*\} from '\.\/composables\/useSessionIdentity\.ts'/)
  })

  it('registers a refresher that actually reaches the roster', () => {
    // Guard against wiring a no-op stub: the registered function must be the
    // roster's own refresh, not an inline lambda that does nothing.
    const registered = app.match(/registerGroupRosterRefresh\(([^)]*)\)/)
    expect(registered).not.toBeNull()
    expect(registered![1].trim()).toBe('refreshGroupMembers')
  })
})
