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
 * The runtime lives in `plugins/forge/plugin.ts` now. App.vue has no component
 * test (its only tests read it as source text), so the WIRING is pinned here:
 * the title/icon/availability/badge must come from that module, and the dock
 * must consume it through the shared resolvers rather than special-casing forge.
 */
describe('forge dock runtime', () => {
  const forge = readWebFile('src/plugins/forge/plugin.ts')

  it('names the tab after the bound service', () => {
    expect(forge).toContain('forgeTabTitleKey(platform.value)')
    expect(forge).toContain('title:')
  })

  it('draws the GitHub/GitLab brand from the binding', () => {
    expect(forge).toContain('forgeDockIconKind(platform.value)')
    expect(forge).toContain('icon:')
  })

  it('declares the server feature switch and leaves the value to the registry', () => {
    // The manifest names the switch (that is what the manager writes and what
    // the dock gates on); the VALUE comes from /api/features, so the plugin must
    // not carry its own config read.
    expect(forge).toContain("feature: 'forge.enabled'")
    expect(forge, 'the per-device key must not drive this').not.toContain('localConfig')
    expect(forge, 'availability is the capability axis, not the feature axis').not.toContain(
      'available:',
    )
  })

  it('feeds the unread badge from the forge composable', () => {
    expect(forge).toContain('forgeUnreadCount')
    expect(forge).toContain('badge:')
  })
})

describe('App.vue consumes the forge runtime', () => {
  const app = readWebFile('src/App.vue')
  const button = readWebFile('src/components/dock/DockTabButton.vue')
  const forge = readWebFile('src/plugins/forge/plugin.ts')

  it('routes title and icon through the shared resolvers', () => {
    // The dock entries are rendered by one component, which resolves the label
    // and the glyph through the runtime; App.vue itself never names forge.
    expect(button).toContain('resolveDockTitle(props.tab)')
    expect(button).toContain('resolveDockIcon(props.tab)')
    expect(app).not.toContain("if (tab === 'forge') return t(forgeTabTitleKey")
    expect(app).not.toContain('function wideDockTabTitle')
  })

  it('has exactly ONE title lookup for every dock button', () => {
    // Every surface renders DockTabButton, so the label cannot diverge between
    // the wide dock, the narrow row and the overflow popup.
    const sites = app.match(/<DockTabButton/g) ?? []
    expect(sites.length).toBeGreaterThanOrEqual(4)
    expect(button.match(/const title = computed/g) ?? []).toHaveLength(1)
  })

  it('lets the overflow button tooltip reuse the dock title', () => {
    const start = app.indexOf('const overflowButtonTitle')
    const body = app.slice(start, start + app.slice(start).indexOf('\n})'))
    expect(body).toContain('dockTabTitle(activeTab.value)')
    expect(body).not.toContain('meta.titleKey')
  })

  it('filters the dock by a single runtime availability set', () => {
    const start = app.indexOf('const visibleDockTabs = computed')
    expect(start, 'visibleDockTabs computed not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 500)
    expect(body).toContain('unavailable: unavailableDockTabs.value')
    expect(body).toContain('featureDisabled: featureDisabledDockTabs.value')
    expect(body).not.toContain('forgeHidden:')
  })

  it('builds the availability sets from the one composable', () => {
    // The three axes (server feature / host capability / user preference) are
    // resolved in useDockAvailability; App.vue only reads them.
    expect(app).toContain('const { availability: dockAvailability } = useDockAvailability()')
    expect(app).toContain('dockAvailability.value.featureDisabled')
    expect(app).toContain('dockAvailability.value.unavailable')
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
    // The handler moved into the plugin (deep links are declarations now), but
    // the ORDER is the whole point of it and is preserved verbatim: refuse →
    // clear → publish → jump.
    expect(app).not.toContain('function handleOpenForge')
    const start = forge.indexOf('clawbench-open-forge')
    expect(start, 'forge plugin does not declare the deep link').toBeGreaterThan(-1)
    const body = forge.slice(start)
    const guard = body.indexOf("ctx.tabBlocked('forge')")
    expect(guard).toBeGreaterThan(-1)
    expect(body.indexOf('setPendingForgeTarget(')).toBeGreaterThan(guard)
    expect(body).toContain('clearPendingForgeTarget()')
  })

  it('does not restore a disabled forge panel on cold start / project switch', () => {
    const start = app.indexOf('function applyProjectPanel')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + 800)
    // The remembered tab is resolved against the whole availability object, so
    // "switched off" and "hidden" take the same path.
    expect(body).toContain('dockAvailability.value')
    expect(body).toContain('resolvePanelTab(')
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
