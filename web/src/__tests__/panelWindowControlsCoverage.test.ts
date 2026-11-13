import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the left column draws ONE header row.
 *
 * The shared tab icon and the two window controls are overlaid on the panel's
 * own header (App.vue's `.panel-head`), and every header row reserves the two
 * ends with padding. There is no second title bar and no per-panel copy of the
 * controls — either one reintroduces the two-row look, and a per-panel copy is
 * what drill-down pages used to cover up.
 *
 * Source-level assertions because mounting App.vue means mounting the app; the
 * invariants here are structural.
 */
const HEADERS = [
  '.dir-toolbar',
  '.file-header-bar',
  '.settings-page__header',
  '.share-header',
  '.forge-header',
  '.list-header',
  '.detail-header',
  '.form-header',
  '.exec-detail-header',
  '.drilldown-header',
  '.proxy-header',
  '.meeting-header',
  '.live-panel__header',
  '.mail-bar__top',
  '.mail-bar__search-row',
  '.mail-reader__head',
  '.mail-compose__head',
  '.stats-tab-bar',
  '.terminal-tab-bar',
]

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

describe('one panel header row', () => {
  const APP = readWebFile('src/App.vue')

  it('overlays the tab icon and the window controls on the header row', () => {
    expect(APP).toContain('class="panel-head"')
    expect(APP).toContain('wideDockTabIcon(leftTab)')
    expect(APP).toContain('<PanelWindowControls />')
    expect(APP).toMatch(/<div v-if="isWideScreen" class="panel-head">/)
    // It must not push the panels down — that would be a second row again.
    expect(APP).not.toMatch(/\.col-left--chrome > \.tab-panel\s*\{[^}]*top:/)
  })

  it('reserves the left and right ends of every header row', () => {
    const css = readWebFile('css/components.css')
    const rule = css.match(/\.col-left--chrome \.tab-panel :is\(([\s\S]*?)\)\s*\{([\s\S]*?)\}/)
    expect(rule, 'the reservation rule must exist').not.toBeNull()
    for (const cls of HEADERS) {
      expect(rule![1], `${cls} must reserve its ends`).toContain(cls)
    }
    expect(rule![2]).toMatch(/padding-left:\s*calc\(32 \* var\(--dp\)\)/)
    expect(rule![2]).toMatch(/padding-right:\s*calc\(var\(--space-2\) \+ 52 \* var\(--dp\)\)/)
    expect(rule![2]).toMatch(/height:\s*var\(--header-height\)/)
  })

  it('is not duplicated inside any panel view', () => {
    for (const file of PANEL_VIEWS) {
      expect(readWebFile(file), `${file} must not render the window controls`).not.toContain(
        'PanelWindowControls',
      )
    }
  })

  it('does not repeat the section glyph inside the headers', () => {
    // The shared head owns the tab icon. These are the page-level glyphs that
    // used to sit next to the title; they would now be a second icon.
    for (const [file, needle] of [
      ['src/components/settings/SettingsPage.vue', 'settings-page__header-icon'],
      ['src/components/settings/SettingsBreadcrumb.vue', 'crumb-icon'],
      ['src/components/task/TaskBreadcrumb.vue', 'crumb-icon'],
      ['src/components/notifications/NotificationsPanelContent.vue', '<Inbox class="crumb-icon"'],
      ['src/components/live/MeetingList.vue', 'meeting-header__icon'],
      ['src/components/live/MeetingStage.vue', 'live-panel__icon'],
      ['src/components/proxy/ProxyPanelContent.vue', 'proxy-header-icon'],
      ['src/components/share/ShareCenterPanel.vue', 'crumb-icon'],
    ] as const) {
      expect(readWebFile(file), `${file} must not repeat the section glyph`).not.toContain(needle)
    }
    // Forge keeps a Github glyph in its repo menu; only the header must be free
    // of the section icon.
    expect(readWebFile('src/components/forge/ForgePanelContent.vue')).not.toMatch(
      /forge-header-title[\s\S]{0,120}<Github/,
    )
  })

  it('reserves exactly two button-widths for the controls', () => {
    const component = readWebFile('src/components/common/PanelWindowControls.vue')
    const button = component.match(/\.panel-control\s*\{[^}]*width:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(button, 'the button width must be expressed in design px').not.toBeNull()
    expect(Number(button![1]) * 2).toBe(52)
  })
})
