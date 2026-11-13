import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: every left-column dock panel draws the three window controls in its
 * own header row.
 *
 * This is a source-level check because mounting all thirteen panels (several of
 * which are lazy, stateful views) would be far more brittle than the thing it
 * protects. The failure it prevents is concrete: a new panel ships without the
 * cluster, so that one tab has no way to close/narrow/maximize the column while
 * every other tab does.
 */
const PANEL_HEADERS = [
  'src/components/file/FileManagerContent.vue', // 文件管理
  'src/components/file/FileHeader.vue', // 文件查看
  'src/components/git/GitCommitList.vue', // 历史 · 提交列表
  'src/components/git/GitHistoryContent.vue', // 历史 · 文件/差异/管理
  'src/components/live/MeetingList.vue', // 会议 · 列表
  'src/components/live/MeetingCreate.vue', // 会议 · 创建
  'src/components/live/MeetingJoin.vue', // 会议 · 加入
  'src/components/live/MeetingDetail.vue', // 会议 · 记录详情
  'src/components/live/MeetingStage.vue', // 会议 · 进行中
  'src/components/mail/MailFolderBar.vue', // 邮件 · 列表
  'src/components/mail/MailReader.vue', // 邮件 · 阅读
  'src/components/mail/MailCompose.vue', // 邮件 · 写信
  'src/components/share/ShareCenterPanel.vue', // 分享
  'src/components/proxy/ProxyPanelContent.vue', // 端口映射
  'src/components/terminal/TerminalPanelContent.vue', // 终端
  'src/components/forge/ForgePanelContent.vue', // 集成
  'src/components/task/TaskListPage.vue', // 任务 · 列表
  'src/components/task/TaskDetailPage.vue', // 任务 · 详情
  'src/components/task/TaskFormPage.vue', // 任务 · 表单
  'src/components/task/TaskExecDetail.vue', // 任务 · 执行详情
  'src/components/notifications/NotificationsPanelContent.vue', // 通知中心
  'src/components/stats/StatsTabHost.vue', // 数据统计
  'src/components/settings/SettingsPage.vue', // 设置
]

describe('panel window controls coverage', () => {
  it('is rendered by every panel header', () => {
    for (const file of PANEL_HEADERS) {
      const src = readWebFile(file)
      expect(src, `${file} must render the shared controls`).toMatch(/<PanelWindowControls\b/)
      expect(src, `${file} must import the shared controls`).toContain(
        "from '@/components/common/PanelWindowControls.vue'",
      )
    }
  })

  it('keeps the wide-screen gate inside the component', () => {
    // Panels must not each re-implement the gate: one missing `v-if` would show
    // the controls on a phone, where they have no meaning.
    const component = readWebFile('src/components/common/PanelWindowControls.vue')
    expect(component).toContain('v-if="isWideScreen"')
    for (const file of PANEL_HEADERS) {
      expect(readWebFile(file), `${file} must not gate the controls itself`).not.toMatch(
        /<PanelWindowControls[^>]*v-if/,
      )
    }
  })

  it('reserves exactly two button-widths of header space', () => {
    const component = readWebFile('src/components/common/PanelWindowControls.vue')
    const button = component.match(/\.panel-control\s*\{[^}]*width:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(button, 'the button width must be expressed in design px').not.toBeNull()
    const buttonWidth = Number(button![1])

    const app = readWebFile('src/App.vue')
    const reserve = app.match(/--panel-controls-w:\s*calc\((\d+)\s*\*\s*var\(--dp\)\)/)
    expect(reserve, 'the reserved width must be declared on .col-left--chrome').not.toBeNull()
    expect(Number(reserve![1])).toBe(buttonWidth * 2)
  })

  it('offers the close cell only in the file viewer, and reserves its width there', () => {
    // Closing the panel is minimize. The file viewer's close closes the open
    // file, so it is the only host that opts in — and the only header that
    // reserves three cells instead of two.
    const header = readWebFile('src/components/file/FileHeader.vue')
    expect(header).toMatch(/<PanelWindowControls\s+closable\b/)
    expect(header).toMatch(/--panel-controls-w:\s*calc\(78\s*\*\s*var\(--dp\)\)/)

    for (const file of PANEL_HEADERS.filter((f) => !f.endsWith('FileHeader.vue'))) {
      expect(readWebFile(file), `${file} must not offer close`).not.toMatch(
        /<PanelWindowControls[^>]*\bclosable\b/,
      )
    }
  })

  it('reserves space in every header class that hosts the controls', () => {
    // A header that is missing from this list keeps its full width and the
    // cluster covers its right-most buttons.
    const css = readWebFile('css/components.css')
    for (const cls of [
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
    ]) {
      expect(css, `${cls} must reserve room for the controls`).toContain(cls)
    }
  })
})
