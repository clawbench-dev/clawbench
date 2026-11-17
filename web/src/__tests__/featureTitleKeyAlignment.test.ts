import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * A feature's name must come from ONE i18n key on every surface.
 *
 * The dock label, the 功能编排 row, the settings index row and the panel
 * header all describe the same thing, so a second key is a second chance to
 * drift — which is how the git history tab ended up as 项目历史 in the manager,
 * Git 历史 in settings and 提交列表 in its own header. The runtime mapping
 * (`dockCategoryTitleKey`) covers the settings pages; this spec pins the
 * headers, which read the key directly.
 *
 * `readWebFile` tolerates both test-runner working directories.
 */
const HEADERS: ReadonlyArray<{
  file: string
  /** The key/expression the header must use. */
  uses: string
  /** The divergent label that must not come back. */
  forbids?: string
}> = [
  { file: 'src/components/task/TaskBreadcrumb.vue', uses: "t('nav.tasks')", forbids: "t('task.title')" },
  {
    file: 'src/components/kb/KnowledgePanel.vue',
    uses: "t('nav.knowledgeBase')",
    forbids: "t('kb.title')",
  },
  {
    file: 'src/components/live/MeetingList.vue',
    uses: "t('nav.meeting')",
    forbids: 'meeting.list.title',
  },
  {
    file: 'src/components/share/ShareCenterPanel.vue',
    uses: "t('nav.share')",
    forbids: 'shareCenter.title',
  },
  {
    file: 'src/components/notifications/NotificationsPanelContent.vue',
    uses: "t('nav.notifications')",
    forbids: 'notifications.title',
  },
  { file: 'src/components/proxy/ProxyPanelContent.vue', uses: "t('nav.portForward')" },
  { file: 'src/components/terminal/TerminalPanelContent.vue', uses: "t('terminal.title')" },
  {
    file: 'src/components/git/GitBreadcrumb.vue',
    uses: "t('git.history.projectHistory')",
    forbids: 'git.breadcrumb.commitList',
  },
  {
    // Forge names the bound service, so its "key" is the shared resolver
    // rather than one literal string.
    file: 'src/components/forge/ForgePanelContent.vue',
    uses: 'forgeTabTitleKey(forgePlatform)',
    forbids: "t('nav.forge')",
  },
]

describe('feature names come from the dock manifest key', () => {
  it.each(HEADERS)('$file uses $uses', ({ file, uses, forbids }) => {
    const source = readWebFile(file)
    expect(source).toContain(uses)
    if (forbids) expect(source).not.toContain(forbids)
  })

  it('resolves settings category labels through the plugin manifest', () => {
    // Both the index rows and the breadcrumb on a drilled-down page must use
    // the dock key; `settings.categories.*` stays as the fallback for
    // categories that are not dock features.
    expect(readWebFile('src/components/settings/SettingsIndex.vue')).toContain(
      'dockCategoryTitleKey(cat.id)',
    )
    expect(readWebFile('src/components/settings/SettingsPage.vue')).toContain(
      'dockCategoryTitleKey(cat)',
    )
  })
})
