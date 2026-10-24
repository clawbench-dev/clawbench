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

  it('routes the compact dock title through the platform-aware key', () => {
    const body = functionBody('dockTabTitle')
    expect(body).toContain("if (tab === 'forge')")
    expect(body).toContain('forgeTabTitleKey(forgePlatform.value)')
  })

  it('routes the wide dock title through the platform-aware key', () => {
    const body = functionBody('wideDockTabTitle')
    expect(body).toContain("if (tab === 'forge')")
    expect(body).toContain('forgeTabTitleKey(forgePlatform.value)')
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
