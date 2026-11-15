import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The mail feature's master switch is a server setting that hides the dock
 * entry, and its unread poller is a background resource. Both used to be wired
 * in App.vue; they live in `components/mail/dock.ts` now. These assertions pin
 * the wiring so a future refactor cannot silently leave the switch or the
 * poller disconnected.
 */
describe('mail dock runtime', () => {
  const mail = readWebFile('src/components/mail/dock.ts')

  it('reads the switch from the SERVER config, not a per-device preference', () => {
    expect(mail).toContain('serverConfig')
    expect(mail).toContain('.enabled !== false')
    expect(mail, 'the per-device key must not drive this').not.toContain('localConfig')
  })

  it('feeds the unread badge from the mail composable', () => {
    expect(mail).toContain('total')
    expect(mail).toContain('badge:')
  })

  it('declares the unread poller as a background resource with a cleanup', () => {
    expect(mail).toContain('startMailUnreadPolling()')
    expect(mail).toContain('return stopMailUnreadPolling')
    expect(mail).toContain('startBackground:')
  })
})

describe('App.vue consumes the mail runtime', () => {
  const app = readWebFile('src/App.vue')

  it('filters the dock by the shared runtime availability set', () => {
    const start = app.indexOf('const visibleDockTabs = computed')
    expect(start, 'visibleDockTabs computed not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 500)
    expect(body).toContain('unavailable: unavailableDockTabs.value')
    expect(body).toContain('featureDisabled: featureDisabledDockTabs.value')
    expect(body).not.toContain('mailHidden:')
  })

  it('leaves the tab when the feature becomes unavailable', () => {
    expect(app).toContain('watch(unavailableDockTabs')
    expect(app).toContain('shouldLeaveUnavailableDockTab(current)')
  })

  it('refuses programmatic navigation into an unavailable plugin', () => {
    const start = app.indexOf('function switchTab(')
    expect(start, 'switchTab not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 1200)
    expect(body).toContain('isDockTabId(tab) && !isDockPluginAvailable(tab)')
  })

  it('does not restore a disabled mail panel on cold start / project switch', () => {
    const start = app.indexOf('function applyProjectPanel')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + 800)
    expect(body).toContain('isMailTabHidden.value')
  })

  it('leaves the poller to the lifecycle, not to App.vue', () => {
    expect(app).toContain('useDockLifecycle()')
    expect(app).not.toContain('startMailUnreadPolling()')
    expect(app).not.toContain('stopMailUnreadPolling()')
  })
})
