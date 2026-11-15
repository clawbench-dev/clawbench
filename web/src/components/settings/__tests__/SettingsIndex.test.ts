import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsIndex from '@/components/settings/SettingsIndex.vue'
import { categoryItems, subPagePanelMap } from '@/components/settings/settingsFieldMap'
import { MEETING_ICON } from '@/utils/meetingIcon'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        categories: {
          appearance: '外观',
          project: '项目',
          dockPlugins: '功能编排',
          files: '文件管理',
          fileView: '文件浏览',
          gitHistory: 'Git 历史',
          stats: '统计',
          identity: '身份',
          chat: '聊天',
          sessions: '会话管理',
          agents: 'Agent偏好',
          tasks: '任务',
          terminal: '终端',
          tts: 'TTS语音',
          stt: '语音识别',
          meeting: '会议',
          rag: 'RAG 检索',
          kb: '知识库',
          portForward: '端口映射',
          mail: '邮件',
          share: '分享中心',
          notification: '消息推送',
          notifications: '通知中心',
          usageMeter: '用量监控',
          security: '安全',
          debug: '调试',
          about: '关于',
        },
        groups: {
          projectAppearance: '项目与外观',
          aiChat: 'AI 与对话',
          dockModules: '功能模块',
          modelCapability: '模型能力',
          system: '系统',
        },
      },
    },
  },
})

// Stub lucide-vue-next icons
const globalStubs = {
  'lucide-chevron-right': true,
  'lucide-messages-square': true,
  'lucide-history': true,
  'lucide-list-todo': true,
  'lucide-chart-column': true,
  'lucide-share-2': true,
  'lucide-palette': true,
  'lucide-layout-grid': true,
  'lucide-eye': true,
  'lucide-book-open': true,
  'lucide-inbox': true,
  'lucide-folder-tree': true,
  'lucide-user-round': true,
  'lucide-message-square': true,
  'lucide-bot': true,
  'lucide-terminal': true,
  'lucide-volume2': true,
  'lucide-mic': true,
  'lucide-brain': true,
  'lucide-arrow-left-right': true,
  'lucide-bell': true,
  'lucide-shield': true,
  'lucide-bug': true,
  'lucide-info': true,
}

function mountIndex() {
  return mount(SettingsIndex, {
    global: { stubs: globalStubs, plugins: [i18n] },
  })
}

describe('SettingsIndex', () => {
  it('renders 31 category rows', () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(31)
  })

  it('renders category labels', () => {
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('项目')
    expect(labels).toContain('外观')
    expect(labels).toContain('文件管理')
    expect(labels).toContain('文件浏览')
    expect(labels).toContain('Git 历史')
    expect(labels).toContain('统计')
    expect(labels).toContain('身份')
    expect(labels).toContain('聊天')
    expect(labels).toContain('会话管理')
    expect(labels).toContain('任务')
    expect(labels).toContain('RAG 检索')
    expect(labels).toContain('邮件')
    expect(labels).toContain('分享中心')
    expect(labels).toContain('端口映射')
    expect(labels).toContain('消息推送')
    expect(labels).toContain('用量监控')
    expect(labels).toContain('安全')
    expect(labels).toContain('调试')
    expect(labels).toContain('关于')
  })

  it('groups categories into 5 titled cards', () => {
    const wrapper = mountIndex()

    const cards = wrapper.findAll('.settings-card')
    expect(cards.length).toBe(5)

    const titles = wrapper.findAll('.settings-card__header').map(el => el.text())
    expect(titles).toEqual([
      '项目与外观', 'AI 与对话', '功能模块', '模型能力', '系统',
    ])
  })

  it('distributes every category into exactly one group', () => {
    const wrapper = mountIndex()

    const perCard = wrapper.findAll('.settings-card').map(card =>
      card.findAll('.settings-index__row').length,
    )
    // 项目与外观 (2): 项目, 外观.
    // AI 与对话 (6): 身份, 聊天, 会话管理, 智能体, 技能, MCP.
    // 功能模块 (16): 功能编排 + one page per dock plugin in DOCK order, then
    //   消息推送 and 用量监控 (features with a page but no dock entry).
    // 模型能力 (4): 模型提供商, RAG 检索, 语音朗读, 语音识别.
    // 系统 (3): 调试, 安全, 关于.
    expect(perCard).toEqual([2, 6, 16, 4, 3])
    expect(perCard.reduce((a, b) => a + b, 0)).toBe(31)
  })

  // The dock tab, the meeting list header and the quote chip all draw
  // MEETING_ICON (utils/meetingIcon). This row used to hard-code a second,
  // unrelated meeting glyph, so the settings page and the tab it configures
  // showed different pictures for the same feature. Compare the rendered SVG
  // against the shared constant instead of naming an icon: the assertion must
  // survive a future change of MEETING_ICON and still catch a local override.
  it('draws the shared meeting icon on the meeting row', () => {
    const wrapper = mountIndex()

    const row = wrapper.find('[data-category="meeting"]')
    expect(row.exists()).toBe(true)
    // Geometry, not the whole markup: size/scoped attributes legitimately
    // differ from a bare render, but the glyph paths are the icon's identity.
    const paths = (html: string) =>
      [...html.matchAll(/<path d="([^"]+)"/g)].map((m) => m[1])
    expect(paths(row.find('svg').html())).toEqual(paths(mount(MEETING_ICON).html()))
  })

  it('emits navigate with categoryId when row clicked', async () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    await rows[0].trigger('click')

    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['project'])
  })

  it('emits correct categoryId for each row', async () => {
    const wrapper = mountIndex()

    const expectedIds = [
      // 项目与外观
      'project', 'appearance',
      // AI 与对话
      'identity', 'chat', 'sessions', 'agents', 'skills', 'mcp',
      // 功能模块
      'dockPlugins', 'files', 'fileView', 'gitHistory', 'forgeIntegration', 'tasks',
      'terminal', 'stats', 'mail', 'kb', 'meeting', 'share', 'portForward',
      'notifications', 'notification', 'usageMeter',
      // 模型能力
      'providers', 'rag', 'tts', 'stt',
      // 系统
      'debug', 'security', 'about',
    ]

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(expectedIds.length)
    for (let i = 0; i < expectedIds.length; i++) {
      await rows[i].trigger('click')
      expect(wrapper.emitted('navigate')![i]).toEqual([expectedIds[i]])
    }
  })
})

// The category label is `t('settings.categories.<id>')`, and vue-i18n renders a
// MISSING key as the key itself — no error, no warning, no build failure. Adding
// a category without its translation therefore ships a literal
// "settings.categories.mcp" into the UI, which is exactly what happened.
//
// This guard reads the REAL locale files and the component SOURCE rather than
// the hand-written message subset above: that subset is precisely what made the
// bug invisible (it silently lacks skills / mcp / rag / forgeIntegration).
describe('SettingsIndex category i18n coverage', () => {
  const localeFiles = import.meta.glob('../../../i18n/locales/*.ts', { eager: true }) as Record<
    string,
    Record<string, unknown>
  >
  const componentSource = Object.entries(
    import.meta.glob('@/components/settings/SettingsIndex.vue', {
      eager: true,
      query: '?raw',
      import: 'default',
    }) as Record<string, string>,
  )[0]?.[1]

  /** categoryIdsFromSource extracts the ids declared in groupDefs. */
  function categoryIdsFromSource(src: string): string[] {
    return [...src.matchAll(/\{ id: '([^']+)', icon:/g)].map((m) => m[1])
  }

  function categoriesOf(mod: Record<string, unknown>): Record<string, unknown> {
    // Locale files use `export default {...}`, so the message tree lives under
    // `default` in the module namespace. Fall back to the namespace itself so
    // this keeps working if a locale ever switches to named exports.
    const root = ((mod.default ?? mod) as Record<string, unknown>)
    const settings = (root.settings ?? {}) as Record<string, unknown>
    return (settings.categories ?? {}) as Record<string, unknown>
  }

  it('declares category ids', () => {
    expect(componentSource).toBeTruthy()
    expect(categoryIdsFromSource(componentSource as string).length).toBeGreaterThan(10)
  })

  // A category defined in the field map but missing from the index is
  // unreachable: its rows exist, its i18n exists, and no user can navigate to
  // it. The reverse (an index row with no category) renders an empty page.
  it('lists every field-map category exactly once, and nothing else', () => {
    const ids = categoryIdsFromSource(componentSource as string)
    // Sub-page routes ('tts:tts_engine') have their own category entry for the
    // panel they render, but they are reached from a parent page, not the index.
    const subPageIds = new Set(Object.keys(subPagePanelMap).map(id => id.split(':')[1]))
    const declared = Object.keys(categoryItems).filter(id => !subPageIds.has(id))
    expect([...ids].sort()).toEqual([...declared].sort())
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('every declared category id has a non-empty label in every locale', () => {
    const ids = categoryIdsFromSource(componentSource as string)
    const locales = Object.entries(localeFiles)
    expect(locales.length).toBeGreaterThanOrEqual(2)

    for (const [file, mod] of locales) {
      const cats = categoriesOf(mod)
      expect(cats, `${file} must define settings.categories`).toBeTruthy()
      for (const id of ids) {
        const label = cats[id]
        expect(label, `${file} is missing settings.categories.${id}`).toBeDefined()
        expect(typeof label, `${file}: settings.categories.${id} must be a string`).toBe('string')
        expect(String(label).trim(), `${file}: settings.categories.${id} must not be empty`).not.toBe('')
      }
    }
  })
})
