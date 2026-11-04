import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The mail feature's master switch is a server setting that hides the dock
 * entry. App.vue has no component test (its only tests read it as source text),
 * so the WIRING is pinned here: the pure filter in dockTabs.ts is covered by
 * unit tests, but nothing there can see whether App.vue actually passes the flag
 * through. Dropping an argument would leave a settings switch that silently
 * does nothing — the same gap the forge visibility test exists for.
 */
describe('mail dock tab visibility wiring', () => {
  const app = readWebFile('src/App.vue')

  it('reads the switch from the SERVER config, not a per-device preference', () => {
    // Turning mail off stops the background IMAP sync on the server, so the
    // dock must agree on every client. A local copy would let one device keep
    // showing an entry the server has disabled.
    const start = app.indexOf('const isMailTabHidden')
    expect(start, 'isMailTabHidden not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 260)
    expect(body).toContain('.enabled === false')
    expect(body).toContain('serverConfig.value')
    expect(body, 'the per-device key must not drive this').not.toContain('localConfig')
  })

  it('passes the flag into the one render-list builder both docks use', () => {
    const start = app.indexOf('const overflowTabs = computed')
    expect(start, 'overflowTabs computed not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n)'))
    expect(body).toContain('mailHidden: isMailTabHidden.value')
  })

  it('leaves the tab and stops the unread poller when the switch is turned off', () => {
    // Without the switch-away the panel stays mounted with no dock entry
    // pointing at it; without the stop the badge poller keeps hitting
    // /api/mail/unread for a feature the user disabled.
    const start = app.indexOf('watch(isMailTabHidden')
    expect(start, 'watch(isMailTabHidden) not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n})'))
    expect(body).toContain('stopMailUnreadPolling()')
    expect(body).toMatch(/if \(hidden && panelIsActive\('mail'\)\)/)
    expect(body).toContain('startMailUnreadPolling()')
  })

  it('refuses programmatic navigation into a disabled mail panel', () => {
    expect(app).toContain("if (tab === 'mail' && mailTabBlocked()) return")
  })

  it('does not restore a disabled mail panel on cold start / project switch', () => {
    // `resolvePanelTab` only knows the dock registry, and mail is still a valid
    // id — so the restore path must pass the switch through, or a project last
    // visited on mail lands the user on a panel whose entry is gone.
    const start = app.indexOf('function applyProjectPanel')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n}'))
    expect(body).toContain('isMailTabHidden.value')
  })
})
