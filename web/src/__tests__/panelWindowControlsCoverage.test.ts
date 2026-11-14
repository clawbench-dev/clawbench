import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the left column draws ONE header row.
 *
 * The two window controls are overlaid on the panel's own header (App.vue's
 * `.panel-head`), and every header row reserves the right end with padding.
 * There is no second title bar and no per-panel copy of the controls — either
 * one reintroduces the two-row look, and a per-panel copy is what drill-down
 * pages used to cover up.
 *
 * The section icon is deliberately NOT in the shared overlay: each header
 * owns its own leading glyph (`.panel-header-icon`) so drill-down pages can
 * choose a different mark or none at all.
 *
 * Source-level assertions because mounting App.vue means mounting the app; the
 * invariants here are structural.
 */
const HEADERS = [
  '.dir-toolbar',
  '.file-header-bar',
  '.view-empty-header',
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
  '.mail-bar',
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

  it('overlays only the window controls on the header row', () => {
    expect(APP).toContain('class="panel-head"')
    expect(APP).toContain('<PanelWindowControls />')
    expect(APP).toMatch(/<div v-if="isWideScreen" class="panel-head">/)
    // The shared head must not carry a section icon — each header defines its
    // own, and a second copy would show two glyphs in the same row.
    expect(APP).not.toContain('wideDockTabIcon(leftTab)')
    expect(APP).not.toContain('panel-head-icon')
    // It must not push the panels down — that would be a second row again.
    expect(APP).not.toMatch(/\.col-left--chrome > \.tab-panel\s*\{[^}]*top:/)
  })

  it('reserves the right end of every header row for the controls', () => {
    const css = readWebFile('css/components.css')
    const rule = css.match(/\.col-left--chrome \.tab-panel :is\(([\s\S]*?)\)\s*\{([\s\S]*?)\}/)
    expect(rule, 'the reservation rule must exist').not.toBeNull()
    for (const cls of HEADERS) {
      expect(rule![1], `${cls} must reserve its right end`).toContain(cls)
    }
    // The left inset is owned by the shared token rule below, not by the
    // wide-screen chrome rule.
    expect(rule![2]).not.toMatch(/padding-left:/)
    expect(rule![2]).toMatch(
      /padding-right:\s*calc\(var\(--panel-header-pad-x\) \+ 52 \* var\(--dp\) \+ var\(--space-2\)\)/,
    )
    expect(rule![2]).toMatch(/height:\s*var\(--header-height\)/)
    expect(rule![2]).toMatch(/background:\s*var\(--bg-secondary\)/)
    expect(rule![2]).toMatch(/border-bottom:\s*1px solid var\(--border-color\)/)
  })

  it('uses one horizontal inset token for every header in both layouts', () => {
    const css = readWebFile('css/components.css')
    const rule = css.match(/\.col-left \.tab-panel :is\(([\s\S]*?)\)\s*\{([\s\S]*?)\}/)
    expect(rule, 'the shared inset rule must exist').not.toBeNull()
    for (const cls of HEADERS) {
      expect(rule![1], `${cls} must share the inset`).toContain(cls)
    }
    expect(rule![2]).toMatch(/padding-left:\s*var\(--panel-header-pad-x\)/)
    expect(rule![2]).toMatch(/padding-right:\s*var\(--panel-header-pad-x\)/)

    const vars = readWebFile('css/variables.css')
    expect(vars, 'the horizontal-inset token must be declared once').toMatch(
      /--panel-header-pad-x:\s*var\(--space-7\)/,
    )
  })

  it('insets the window controls by the same token', () => {
    // Otherwise the right-most control sits flush against the panel edge while
    // the section glyph on the left has a margin — the asymmetry this token
    // exists to prevent.
    const controls = readWebFile('src/components/common/PanelWindowControls.vue')
    const rule = controls.match(/\.panel-window-controls\s*\{([\s\S]*?)\}/)
    expect(rule, '.panel-window-controls rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/right:\s*var\(--panel-header-pad-x\)/)
  })

  it('keeps the empty-state header row in narrow mode too', () => {
    // The wide-screen chrome rule is scoped to `.col-left--chrome`, which only
    // exists on wide screens. The empty state has no toolbar of its own, so it
    // must carry the row geometry itself — otherwise narrow screens render its
    // title flush against the panel edge with no surface and no divider. Its
    // left inset comes from the shared token rule, not from this block.
    expect(APP).toContain("'col-left--chrome': isWideScreen")
    const rule = APP.match(/\.view-empty-header\s*\{([\s\S]*?)\}/)
    expect(rule, '.view-empty-header rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/height:\s*var\(--header-height\)/)
    expect(rule![1]).toMatch(/background:\s*var\(--bg-secondary\)/)
    expect(rule![1]).toMatch(/border-bottom:\s*1px solid var\(--border-color\)/)
  })

  it('gives the section glyph one shared style for every header', () => {
    const css = readWebFile('css/components.css')
    const icon = css.match(/\.panel-header-icon\s*\{([\s\S]*?)\}/)
    expect(icon, '.panel-header-icon rule must exist').not.toBeNull()
    expect(icon![1]).toMatch(/flex:\s*0 0 auto/)
    expect(icon![1]).toMatch(/align-self:\s*center/)
    expect(icon![1]).toMatch(/color:\s*var\(--text-muted\)/)
  })

  it('renders the section glyph inside each panel header, not in the shared head', () => {
    // Every panel's primary header owns its leading glyph. The list mirrors
    // the dock tab registry: a new tab without an icon here renders a bare
    // header with no section identity.
    for (const file of [
      'src/App.vue', // file view's empty-state header
      'src/components/file/FileManagerContent.vue',
      'src/components/file/FileHeader.vue',
      'src/components/settings/SettingsPage.vue',
      'src/components/settings/SettingsBreadcrumb.vue',
      'src/components/share/ShareCenterPanel.vue',
      'src/components/kb/KnowledgePanel.vue',
      'src/components/forge/ForgePanelContent.vue',
      'src/components/task/TaskBreadcrumb.vue',
      'src/components/git/GitBreadcrumb.vue',
      'src/components/notifications/NotificationsPanelContent.vue',
      'src/components/proxy/ProxyPanelContent.vue',
      'src/components/live/MeetingList.vue',
      'src/components/live/MeetingCreate.vue',
      'src/components/live/MeetingJoin.vue',
      'src/components/live/MeetingDetail.vue',
      'src/components/live/MeetingStage.vue',
      'src/components/mail/MailFolderBar.vue',
      'src/components/mail/MailReader.vue',
      'src/components/mail/MailCompose.vue',
      'src/components/stats/StatsTabHost.vue',
      'src/components/terminal/TerminalPanelContent.vue',
      'src/components/forge/ForgeDetail.vue',
      'src/components/forge/ForgePipelineDetail.vue',
    ]) {
      expect(readWebFile(file), `${file} must own its section glyph`).toContain(
        'class="panel-header-icon"',
      )
    }
  })

  it('is not duplicated inside any panel view', () => {
    for (const file of PANEL_VIEWS) {
      expect(readWebFile(file), `${file} must not render the window controls`).not.toContain(
        'PanelWindowControls',
      )
    }
  })

  it('flattens the mail bar wrapper to one 36px row', () => {
    // The mail bar is a padded wrapper around its row; left alone it renders
    // 44px tall, the only header that is taller than the rest.
    const css = readWebFile('css/components.css')
    const mail = css.match(/\.col-left--chrome \.tab-panel :is\(\.mail-bar[^)]*\)\s*\{([^}]*)\}/)
    expect(mail, 'the mail wrapper must own the row height').not.toBeNull()
    expect(mail![1]).toMatch(/padding-top:\s*0/)
    expect(mail![1]).toMatch(/padding-bottom:\s*0/)
    const fill = css.match(/\.col-left--chrome \.tab-panel \.mail-bar > :is\([^)]*\)\s*\{([^}]*)\}/)
    expect(fill, 'the inner row must fill the wrapper').not.toBeNull()
    expect(fill![1]).toMatch(/flex:\s*1 1 auto/)
  })

  it('reserves exactly two button-widths for the controls', () => {
    const component = readWebFile('src/components/common/PanelWindowControls.vue')
    const button = component.match(/\.panel-control\s*\{[^}]*width:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(button, 'the button width must be expressed in design px').not.toBeNull()
    expect(Number(button![1]) * 2).toBe(52)
  })
})
