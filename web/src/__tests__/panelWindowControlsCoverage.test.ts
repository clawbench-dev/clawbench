import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the left-column window controls live in ONE title bar above the panel,
 * not inside each panel's own header.
 *
 * The per-header design failed in practice: a drill-down page (meeting detail,
 * mail reader, task detail) replaces the header row, so the controls vanished
 * exactly when the user was deepest in the panel. One bar that is a sibling of
 * the TabPanels cannot be covered by anything a panel renders.
 *
 * Source-level assertions because mounting App.vue is mounting the whole app;
 * the failure this prevents (a panel growing its own copy again) is structural.
 */
const PANEL_VIEWS = [
  'src/components/file/FileManagerContent.vue',
  'src/components/file/FileHeader.vue',
  'src/components/git/GitCommitList.vue',
  'src/components/git/GitHistoryContent.vue',
  'src/components/live/MeetingList.vue',
  'src/components/live/MeetingCreate.vue',
  'src/components/live/MeetingJoin.vue',
  'src/components/live/MeetingDetail.vue',
  'src/components/live/MeetingStage.vue',
  'src/components/mail/MailFolderBar.vue',
  'src/components/mail/MailReader.vue',
  'src/components/mail/MailCompose.vue',
  'src/components/share/ShareCenterPanel.vue',
  'src/components/proxy/ProxyPanelContent.vue',
  'src/components/terminal/TerminalPanelContent.vue',
  'src/components/forge/ForgePanelContent.vue',
  'src/components/task/TaskListPage.vue',
  'src/components/task/TaskDetailPage.vue',
  'src/components/task/TaskFormPage.vue',
  'src/components/task/TaskExecDetail.vue',
  'src/components/notifications/NotificationsPanelContent.vue',
  'src/components/stats/StatsTabHost.vue',
  'src/components/settings/SettingsPage.vue',
]

describe('panel title bar', () => {
  const APP = readWebFile('src/App.vue')

  it('renders one bar with the tab icon, its name and the window controls', () => {
    expect(APP).toContain('class="panel-titlebar"')
    expect(APP).toContain('wideDockTabIcon(leftTab)')
    expect(APP).toContain('dockTabTitle(leftTab)')
    expect(APP).toContain('<PanelWindowControls />')
    // Wide screens only: a single-column layout has no second pane to manage.
    expect(APP).toMatch(/<div v-if="isWideScreen" class="panel-titlebar">/)
  })

  it('sits above the TabPanels, which start below it', () => {
    const bar = APP.indexOf('class="panel-titlebar"')
    const firstPanel = APP.indexOf('<TabPanel')
    expect(bar).toBeGreaterThan(-1)
    expect(firstPanel).toBeGreaterThan(bar)
    expect(APP).toMatch(/\.col-left--chrome > \.tab-panel\s*\{[^}]*top:\s*var\(--header-height\)/)
  })

  it('is not duplicated inside any panel view', () => {
    // A panel-owned copy is exactly what the drill-down pages covered up.
    for (const file of PANEL_VIEWS) {
      expect(readWebFile(file), `${file} must not render the window controls`).not.toContain(
        'PanelWindowControls',
      )
    }
  })

  it('reserves exactly two button-widths for the controls', () => {
    const component = readWebFile('src/components/common/PanelWindowControls.vue')
    const button = component.match(/\.panel-control\s*\{[^}]*width:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(button, 'the button width must be expressed in design px').not.toBeNull()
    const buttonWidth = Number(button![1])

    const reserve = APP.match(/padding-right:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(reserve, 'the title bar must reserve room for the controls').not.toBeNull()
    expect(Number(reserve![1])).toBe(buttonWidth * 2)
  })
})
