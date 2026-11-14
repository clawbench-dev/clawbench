import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import { DOCK_TAB_IDS } from '@/composables/dockTabs'
import { getDockPluginRuntime } from '@/composables/dockPluginRuntime'

/**
 * Every left-dock tab except the two core file tabs is rendered through
 * DockPanelHost, which looks up `dock.ts`'s `panel`. A missing descriptor would
 * render an empty panel with no error — the tab would look broken.
 */
describe('dock panel coverage', () => {
  for (const id of DOCK_TAB_IDS) {
    if (id === 'browse' || id === 'view') continue

    it(`${id} declares a panel`, () => {
      const panel = getDockPluginRuntime(id)?.panel
      expect(panel, `${id} has no dock.ts panel`).toBeDefined()
      expect(panel?.component || panel?.load, `${id} panel has no component/loader`).toBeTruthy()
    })
  }
})

describe('App.vue uses the generic panel host', () => {
  const app = readWebFile('src/App.vue')

  it('renders DockPanelHost for the non-core tabs', () => {
    expect(app).toContain('<DockPanelHost')
    expect(app).toContain('genericDockTabs')
  })

  it('does not import optional panels directly', () => {
    const panels = [
      'ForgePanelContent',
      'TaskTab',
      'SettingsPage',
      'NotificationsPanelContent',
      'StatsTabHost',
      'GitHistoryContent',
      'ProxyPanelContent',
      'MeetingPanel',
      'MailPanel',
      'ShareCenterPanel',
      'KnowledgePanel',
      'TerminalPanelContent',
    ]
    for (const name of panels) {
      expect(app, `App.vue still imports ${name}`).not.toContain(`import ${name}`)
      expect(app, `App.vue still declares ${name} as an async component`).not.toContain(
        `const ${name} = defineAsyncComponent`,
      )
    }
  })
})
