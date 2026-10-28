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
 * App.vue has no component test (its only tests read it as source text), and
 * `dockTabTitle` is a plain function inside `<script setup>` — not reachable
 * from jsdom. The behaviour that matters is the ROUTING: every title lookup the
 * dock uses must go through `forgeTabTitleKey`, otherwise one of the three
 * surfaces (narrow popup, narrow inline, wide dock) silently keeps the old
 * neutral label. That is exactly the class of divergence this guard pins.
 */
describe('forge dock tab title wiring', () => {
  const app = readWebFile('src/App.vue')

  function functionBody(name: string): string {
    const start = app.indexOf(`function ${name}(`)
    expect(start, `${name} not found in App.vue`).toBeGreaterThan(-1)
    const body = app.slice(start)
    return body.slice(0, body.indexOf('\n}'))
  }

  it('routes the dock title through the platform-aware key', () => {
    const body = functionBody('dockTabTitle')
    expect(body).toContain("if (tab === 'forge')")
    expect(body).toContain('forgeTabTitleKey(forgePlatform.value)')
  })

  it('has exactly ONE title lookup for both docks and the overflow tooltip', () => {
    // Three surfaces (narrow popup, narrow inline, wide dock) plus the overflow
    // tooltip. Two copies of the same function is what forced the forge branch
    // to be written twice and let one surface silently keep the neutral label;
    // the wide dock's duplicate is gone, so every surface must call the same
    // function.
    expect(app).not.toContain('function wideDockTabTitle')
    expect(app).not.toMatch(/:\s*title="wideDockTabTitle\(/)
    // The wide dock's buttons now use the shared lookup.
    const wideDockButtons = app.match(/:title="dockTabTitle\(tab\)"/g) ?? []
    expect(wideDockButtons.length).toBeGreaterThanOrEqual(3)
  })

  it('lets the overflow button tooltip reuse the dock title', () => {
    // Re-deriving the title here from the registry is what let the tooltip keep
    // the neutral label after the menu item was renamed.
    const start = app.indexOf('const overflowButtonTitle')
    const body = app.slice(start, start + app.slice(start).indexOf('\n})'))
    expect(body).toContain('dockTabTitle(activeTab.value)')
    expect(body).not.toContain('meta.titleKey')
  })
})

describe('forge dock tab visibility wiring (issue #590)', () => {
  // App.vue has no component test, so the WIRING is pinned as source text: the
  // pure filter in dockTabs.ts is covered by unit tests, but nothing there can
  // see whether App.vue actually passes the preference through. Dropping the
  // argument would leave a settings switch that silently does nothing — the
  // same class of gap as the old render-list/whitelist divergence.
  const app = readWebFile('src/App.vue')

  it('reads the switch from the SERVER config, not a per-device preference', () => {
    // Disabling stops polling and unlinks every project's repository on the
    // server, so the dock must agree on every client. A local copy would let one
    // device keep showing an entry the server has disabled (and the reverse).
    const start = app.indexOf('const isForgeTabHidden')
    expect(start, 'isForgeTabHidden not found in App.vue').toBeGreaterThan(-1)
    // The computed body spans two lines (the optional chain is cast through a
    // Record because the config ref is untyped), so slice a window rather than
    // up to the first newline.
    const body = app.slice(start, start + 260)
    expect(body).toContain('.enabled === false')
    expect(body).toContain('serverConfig.value')
    expect(body, 'the per-device key must not drive this').not.toContain('localConfig')
  })

  it('passes the flag into the one render-list builder both docks use', () => {
    const start = app.indexOf('const overflowTabs = computed')
    expect(start, 'overflowTabs computed not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n)'))
    expect(body).toContain('forgeHidden: isForgeTabHidden.value')
  })

  it('leaves the tab when the switch is turned off mid-session', () => {
    // Without this the panel stays mounted with no dock entry pointing at it.
    expect(app).toContain('watch(isForgeTabHidden')
    expect(app).toMatch(/if \(hidden && panelIsActive\('forge'\)\)/)
  })

  it('does not count hidden forge items in the overflow badge', () => {
    // A badge advertising unread items behind a menu that cannot show them.
    const start = app.indexOf('const overflowBadgeCount')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n})'))
    expect(body).toContain('if (!isForgeTabHidden.value) count += forgeUnreadCount.value')
  })

  it('does not restore a disabled forge panel on cold start / project switch', () => {
    // `resolvePanelTab` only knows the dock registry, and forge is still a
    // valid id — so the restore path must pass the switch through, or a project
    // last visited on forge lands the user on a panel whose entry is gone.
    const start = app.indexOf('function applyProjectPanel')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n}'))
    expect(body).toContain('isForgeTabHidden.value')
  })

  it('refuses to navigate to a disabled integration from inside switchTab', () => {
    // The concrete misses are `returnToOrigin()`/`abandonJump()`, which replay a
    // remembered tab by calling switchTab('forge') from another module, and a
    // stale notification tapped after the switch was turned off. With the
    // integration really disabled the panel has nothing to show, so the
    // navigation is refused rather than silently re-enabling it.
    const start = app.indexOf('function switchTab(')
    expect(start, 'switchTab not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + app.slice(start).indexOf('\n  if (isWideScreen'))
    expect(body).toContain("if (tab === 'forge' && forgeTabBlocked()) return")

    // The rule lives in a named helper defined AFTER the `const`s it reads:
    // switchTab is hoisted, so an inlined rule would be a latent TDZ crash the
    // day anything calls switchTab during setup.
    const helper = app.indexOf('function forgeTabBlocked')
    expect(helper, 'forgeTabBlocked not found in App.vue').toBeGreaterThan(-1)
    expect(helper).toBeGreaterThan(app.indexOf('const isForgeTabHidden'))

    // Refusing must NOT re-enable the integration behind the user's back.
    expect(app).not.toContain('revealHiddenForgeEntry')
    expect(app).not.toContain("setSetting('showForgeTab'")
  })

  it('refuses the deep link BEFORE publishing its target', () => {
    // switchTab's own guard fires too late here: handleOpenForge has already
    // published a pending target and registered a back-origin, so the next time
    // the panel mounts (after re-enabling) it would jump to a stale item and
    // Back would point at a jump that never happened.
    const start = app.indexOf('function handleOpenForge(')
    expect(start, 'handleOpenForge not found in App.vue').toBeGreaterThan(-1)
    const body = app.slice(start, start + 3000)
    const guard = body.indexOf('if (forgeTabBlocked())')
    expect(guard).toBeGreaterThan(-1)
    expect(body.indexOf('setPendingForgeTarget(')).toBeGreaterThan(guard)
    expect(body).toContain('clearPendingForgeTarget()')
  })

  it('keeps the guard ahead of switchTab\'s early returns', () => {
    // switchTab returns early for 'chat' on a wide screen and for a no-op
    // re-selection. The forge guard must run before both, otherwise "already on
    // forge" (a plausible re-entry from the navigation coordinator) would slip
    // through.
    const start = app.indexOf('function switchTab(')
    const body = app.slice(start, start + 3000)
    const guard = body.indexOf('forgeTabBlocked()')
    expect(guard).toBeGreaterThan(-1)
    expect(body.indexOf('if (isWideScreen.value)')).toBeGreaterThan(guard)
    expect(body.indexOf('activeTab.value === tab')).toBeGreaterThan(guard)
  })

  it('does not enter wide-screen mode on a hidden forge panel', () => {
    // This path assigns leftTab/activeTab through switchLeftTab, NOT switchTab,
    // so the guard above cannot see it: widening the window would restore a
    // panel whose entry is gone and misplace the dock indicator.
    const start = app.indexOf('watch(isWideScreen, (val) =>')
    expect(start).toBeGreaterThan(-1)
    const body = app.slice(start, start + 2000)
    expect(body).toContain("candidate === 'forge' && forgeTabBlocked() ? 'browse' : candidate")
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
