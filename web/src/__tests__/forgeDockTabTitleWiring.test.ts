import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * The forge dock tab is labelled after the integration the project actually
 * uses ("GitHub integration" / "GitLab integration") instead of a
 * platform-neutral "issues & PRs" phrase, so the overflow menu says which
 * service it opens.
 *
 * The runtime lives in `components/forge/dock.ts` now. App.vue has no component
 * test (its only tests read it as source text), so the WIRING is pinned here:
 * the title/icon/availability/badge must come from that module, and the dock
 * must consume it through the shared resolvers rather than special-casing forge.
 */
describe('forge dock runtime', () => {
  const forge = readWebFile('src/components/forge/dock.ts')

  it('names the tab after the bound service', () => {
    expect(forge).toContain('forgeTabTitleKey(platform.value)')
    expect(forge).toContain('title:')
  })

  it('draws the GitHub/GitLab brand from the binding', () => {
    expect(forge).toContain('forgeDockIconKind(platform.value)')
    expect(forge).toContain('icon:')
  })

  it('reads the server feature switch, not a per-device preference', () => {
    expect(forge).toContain('serverConfig')
    expect(forge).toContain('.enabled !== false')
    expect(forge, 'the per-device key must not drive this').not.toContain('localConfig')
  })

  it('feeds the unread badge from the forge composable', () => {
    expect(forge).toContain('forgeUnreadCount')
    expect(forge).toContain('badge:')
  })
})

describe('App.vue consumes the forge runtime', () => {
  const app = readWebFile('src/App.vue')

  it('routes title and icon through the shared resolvers', () => {
    expect(app).toContain('resolveDockTitle(tab)')
    expect(app).toContain('resolveDockIcon(tab)')
    expect(app).not.toContain("if (tab === 'forge') return t(forgeTabTitleKey")
    expect(app).not.toContain('function wideDockTabTitle')
  })

  it('has exactly ONE title lookup for both docks and the overflow tooltip', () => {
    const labels = app.match(/(?:v-dock-tip|:aria-label|:title)="dockTabTitle\(tab\)"/g) ?? []
    expect(labels.length).toBeGreaterThanOrEqual(3)
  })

  it('lets the overflow button tooltip reuse the dock title', () => {
    const start = app.indexOf('const overflowButtonTitle')
    const body = app.slice(start, start + app.slice(start).indexOf('\n})'))
    expect(body).toContain('dockTabTitle(activeTab.value)')
    expect(body).not.toContain('meta.titleKey')
  })

  it('filters the dock by a single runtime availability set', () => {
    const start = app.indexOf('const overflowTabs = computed')
    expect(start, 'overflowTabs computed not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 400)
    expect(body).toContain('unavailable: unavailableDockTabs.value')
    expect(body).not.toContain('forgeHidden:')
  })

  it('builds the unavailable set from the runtime', () => {
    const start = app.indexOf('const unavailableDockTabs = computed')
    expect(start, 'unavailableDockTabs not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 300)
    expect(body).toContain('isDockPluginAvailable(id)')
  })

  it('leaves the tab when availability changes', () => {
    expect(app).toContain('watch(unavailableDockTabs')
    expect(app).toContain('shouldLeaveUnavailableDockTab(current)')
  })

  it('refuses programmatic navigation into an unavailable plugin', () => {
    const start = app.indexOf('function switchTab(')
    expect(start, 'switchTab not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 1200)
    expect(body).toContain('isDockTabId(tab) && !isDockPluginAvailable(tab)')
  })

  it('refuses the forge deep link BEFORE publishing its target', () => {
    const start = app.indexOf('function handleOpenForge(')
    expect(start, 'handleOpenForge not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 3000)
    const guard = body.indexOf("if (tabBlocked('forge'))")
    expect(guard).toBeGreaterThan(-1)
    expect(body.indexOf('setPendingForgeTarget(')).toBeGreaterThan(guard)
    expect(body).toContain('clearPendingForgeTarget()')
  })

  it('does not restore a disabled forge panel on cold start / project switch', () => {
    const start = app.indexOf('function applyProjectPanel')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + 800)
    expect(body).toContain('isForgeTabHidden.value')
  })

  it('does not enter wide-screen mode on a hidden/unavailable forge panel', () => {
    const start = app.indexOf('watch(isWideScreen, (val) =>')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + 2000)
    expect(body).toContain('tabBlocked(candidate)')
  })

  it('does not special-case forge in the overflow badge', () => {
    const start = app.indexOf('const overflowBadgeCount')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + 400)
    expect(body).toContain('popupOverflowTabs.value.reduce')
    expect(body).not.toContain('forgeUnreadCount')
  })
})

describe('forge dock tab locale values', () => {
  it('names the integration in both locales', () => {
    expect(zh.nav.forgeGithub).toBe('GitHub 集成')
    expect(zh.nav.forgeGitlab).toBe('GitLab 集成')
    expect(en.nav.forgeGithub).toBe('GitHub Integration')
    expect(en.nav.forgeGitlab).toBe('GitLab Integration')
  })

  it('keeps the two brand labels distinct', () => {
    expect(zh.nav.forgeGithub).not.toBe(zh.nav.forgeGitlab)
    expect(en.nav.forgeGithub).not.toBe(en.nav.forgeGitlab)
  })
})
