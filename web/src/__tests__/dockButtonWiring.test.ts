import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import { resolveDockPlugin } from '@/composables/dockPluginRuntime'

/**
 * The dock shell must not know any plugin by name.
 *
 * Before this refactor App.vue decided the button's state classes with
 * `tab === 'tasks'` (three of them), hid the meeting dot with a `tab === 'live'`
 * special case, and routed the terminal click with `if (tab === 'terminal')` —
 * so a new plugin that wanted any of that had to edit App.vue. Everything is now
 * declared by the plugin (`appearance`, `onClick`) and rendered by the shared
 * button.
 *
 * These are source guards on purpose: the failure mode is "a second, private
 * copy of the logic grew back", which a rendered-DOM test cannot see while the
 * shared path still works.
 */
describe('dock button wiring', () => {
  const app = readWebFile('src/App.vue')
  const button = readWebFile('src/components/dock/DockTabButton.vue')

  it('renders every dock surface through the shared button', () => {
    // wide dock, narrow chat, narrow inline list, single-direct item, popup row
    const sites = app.match(/<DockTabButton/g) ?? []
    expect(sites.length).toBeGreaterThanOrEqual(5)
    // …and the raw button markup is gone from the dock area.
    const dock = app.slice(app.indexOf('class="wide-dock"'), app.indexOf('class="content-area"'))
    expect(dock).not.toContain('<button class="dock-btn"')
  })

  it('decides no plugin state in the shell', () => {
    for (const pattern of [
      "tab === 'tasks'",
      "tab === 'live'",
      "tab === 'terminal'",
      "singleDirectTab === 'terminal'",
    ]) {
      expect(app, `App.vue still branches on ${pattern}`).not.toContain(pattern)
    }
    // The state classes come from the plugin declaration.
    expect(button).toContain('appearance.value?.unread?.()')
    expect(button).toContain('appearance.value?.running?.()')
    expect(button).toContain('appearance.value?.justCompleted?.()')
    expect(button).toContain('appearance.value?.liveDot?.()')
  })

  it('runs a plugin click hook before switching tabs, on both narrow paths', () => {
    expect(app).not.toContain('function handleDockTerminal')
    const calls = app.match(/runDockOnClick\(tab, dockPanelHost, dockPanelExtras\)/g) ?? []
    // inline overflow row + overflow popup row
    expect(calls.length).toBe(2)
    expect(resolveDockPlugin('terminal')?.onClick).toBeTypeOf('function')
  })

  it('keeps no per-tab animation state in the shell', () => {
    for (const leftover of ['badgeAnims', 'chatBadgeAnim', 'dockTabBadgeAnim', 'leftBadgeCounts']) {
      expect(app, `App.vue still keeps ${leftover}`).not.toContain(leftover)
    }
    // The overflow button's own aggregate badge is the one exception, and it
    // shares the composable rather than hand-rolling the re-arm.
    expect(app).toContain('useBadgePop(')
  })

  it('declares the appearance states on the plugins that have them', () => {
    expect(resolveDockPlugin('tasks')?.appearance?.unread).toBeTypeOf('function')
    expect(resolveDockPlugin('tasks')?.appearance?.running).toBeTypeOf('function')
    expect(resolveDockPlugin('tasks')?.appearance?.justCompleted).toBeTypeOf('function')
    expect(resolveDockPlugin('live')?.appearance?.liveDot).toBeTypeOf('function')
    // chat keeps its unread + running marks even though it is not a dock tab.
    expect(resolveDockPlugin('chat')?.appearance?.unread).toBeTypeOf('function')
    expect(resolveDockPlugin('chat')?.appearance?.running).toBeTypeOf('function')
  })
})
