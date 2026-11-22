import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsIndex from '@/components/settings/SettingsIndex.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { categoryItems, subPagePanelMap } from '@/components/settings/settingsFieldMap'
import { MEETING_ICON } from '@/utils/meetingIcon'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      // Dock features take their label from the plugin manifest's key, not
      // from `settings.categories.*` — the index, the manager and the panel
      // header must all say the same thing.
      nav: {
        fileManager: '文件管理',
        fileView: '文件浏览',
        tasks: '自动化任务',
        notifications: '通知中心',
        meeting: '视频会议',
        share: '分享中心',
        mail: '邮件',
        knowledgeBase: '知识库',
        portForward: '端口映射',
        stats: '数据统计',
        forgeGithub: '代码托管集成',
      },
      git: { history: { projectHistory: '项目历史' } },
      terminal: { title: '终端' },
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
          chat: '聊天会话',
          sessions: '会话列表',
          agents: 'Agent偏好',
          tasks: '自动化任务',
          terminal: '终端',
          tts: 'TTS语音',
          stt: '语音识别',
          meeting: '会议',
          rag: '会话索引',
          embedding: '文本嵌入',
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
        search: {
          placeholder: '搜索设置项，中英文都可以',
          noResults: '没有找到与「{query}」相关的设置',
          filteredTitle: '只显示匹配的设置',
          showAll: '显示全部',
          forcedHint: '当前条件下不生效',
        },
        groups: {
          projectAppearance: '项目与外观',
          aiChat: '智能体',
          dockModules: '功能',
          modelCapability: '模型',
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
  'lucide-scan-text': true,
  'lucide-arrow-left-right': true,
  'lucide-bell': true,
  'lucide-shield': true,
  'lucide-bug': true,
  'lucide-info': true,
}

function mountIndex() {
  return mount(SettingsIndex, {
    global: {
      // The real SearchInput pulls `search.*` labels from the app's full locale
      // tree; this suite only ships the settings subset, so the input is
      // replaced by a plain box that emits the same v-model updates.
      stubs: { ...globalStubs, SearchInput: SearchInputStub },
      plugins: [i18n],
    },
  })
}

const SearchInputStub = {
  name: 'SearchInput',
  // Mounted as a replacement COMPONENT, so it is not reached through the
  // attribute fallthrough that `stubs` uses: the class must be on the root.
  inheritAttrs: false,
  props: { modelValue: { type: String, default: '' }, placeholder: { type: String, default: '' } },
  emits: ['update:modelValue', 'enter'],
  template: '<input class="search-input-stub" :placeholder="placeholder" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" />',
}

describe('SettingsIndex', () => {
  it('renders 32 category rows', () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(32)
  })

  it('renders category labels', () => {
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('项目')
    expect(labels).toContain('外观')
    expect(labels).toContain('文件管理')
    expect(labels).toContain('文件浏览')
    expect(labels).toContain('项目历史')
    expect(labels).toContain('数据统计')
    expect(labels).toContain('身份')
    expect(labels).toContain('聊天会话')
    expect(labels).toContain('会话列表')
    expect(labels).toContain('自动化任务')
    expect(labels).toContain('会话索引')
    expect(labels).toContain('文本嵌入')
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
      '项目与外观', '模型', '智能体', '功能', '系统',
    ])
  })

  it('distributes every category into exactly one group', () => {
    const wrapper = mountIndex()

    const perCard = wrapper.findAll('.settings-card').map(card =>
      card.findAll('.settings-index__row').length,
    )
    // 项目与外观 (2): 项目, 外观.
    // 模型 (4): 模型提供商, 文本嵌入, 语音朗读, 语音识别.
    // 智能体 (4): 身份, 智能体, 技能, MCP.
    // 功能 (19): 功能编排, then the right-pane chat's three pages (聊天会话,
    //   会话列表, 会话索引), then one page per dock plugin in DOCK order,
    //   then 消息推送 and 用量监控 (features with a page but no dock entry).
    // 系统 (3): 调试, 安全, 关于.
    expect(perCard).toEqual([2, 4, 4, 19, 3])
    expect(perCard.reduce((a, b) => a + b, 0)).toBe(32)
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
      // 模型
      'providers', 'embedding', 'tts', 'stt',
      // 智能体
      'identity', 'agents', 'skills', 'mcp',
      // 功能
      'dockPlugins', 'chat', 'sessions', 'rag', 'files', 'fileView', 'gitHistory', 'forgeIntegration', 'tasks',
      'terminal', 'stats', 'mail', 'kb', 'meeting', 'share', 'portForward',
      'notifications', 'notification', 'usageMeter',
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
// This guard reads the REAL locale files and the RENDERED rows rather than the
// hand-written message subset above: that subset is precisely what made the bug
// invisible (it silently lacks skills / mcp / rag / forgeIntegration). Reading
// the DOM (not the component source) also covers the rows the 功能 group
// DERIVES from the plugin registry — a source-text scrape cannot see those.
describe('SettingsIndex category i18n coverage', () => {
  const localeFiles = import.meta.glob('../../../i18n/locales/*.ts', { eager: true }) as Record<
    string,
    Record<string, unknown>
  >

  /** Every category id the index actually renders, in order. */
  function renderedCategoryIds(): string[] {
    return mountIndex()
      .findAll('.settings-index__row')
      .map((row) => row.attributes('data-category') ?? '')
      .filter((id) => id !== '')
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
    expect(renderedCategoryIds().length).toBeGreaterThan(10)
  })

  // A category defined in the field map but missing from the index is
  // unreachable: its rows exist, its i18n exists, and no user can navigate to
  // it. The reverse (an index row with no category) renders an empty page.
  it('lists every field-map category exactly once, and nothing else', () => {
    const ids = renderedCategoryIds()
    // Sub-page routes ('tts:tts_engine') have their own category entry for the
    // panel they render, but they are reached from a parent page, not the index.
    const subPageIds = new Set(Object.keys(subPagePanelMap).map(id => id.split(':')[1]))
    const declared = Object.keys(categoryItems).filter(id => !subPageIds.has(id))
    expect([...ids].sort()).toEqual([...declared].sort())
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('every declared category id has a non-empty label in every locale', () => {
    const ids = renderedCategoryIds()
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

// ── Search (issue #691) ──
//
// The index searches the REAL app locale tree (settingsSearch imports `@/i18n`),
// not the message subset above, so these assertions read the production labels.
describe('SettingsIndex search', () => {
  // The search index reads the REAL app i18n instance (`@/i18n`), whose locale
  // is persisted per machine/test run. Pin it to Chinese so the queries below
  // ("邮件", "收信间隔") are the ones the user typed.
  beforeEach(async () => {
    const appI18n = (await import('@/i18n')).default
    appI18n.global.locale.value = 'zh'
  })

  async function typeQuery(wrapper: ReturnType<typeof mountIndex>, text: string) {
    const input = wrapper.find('.search-input-stub')
    await input.setValue(text)
  }

  it('replaces the category list with matching rows', async () => {
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-index__hit').exists()).toBe(false)

    await typeQuery(wrapper, '邮件')

    const rows = wrapper.findAll('.settings-index__hit')
    expect(rows.length).toBeGreaterThan(0)
    // The unfiltered list is gone, not merely appended to.
    expect(wrapper.find('[data-category="project"]').exists()).toBe(false)
    const titles = wrapper.findAll('.settings-card__header').map(el => el.text())
    expect(titles).toContain('邮件')
  })

  it('finds a row by the word from its description', async () => {
    const wrapper = mountIndex()

    // 「追踪像素」 appears only in the mailImages row's description.
    await typeQuery(wrapper, '追踪像素')

    const labels = wrapper.findAll('.settings-index__hit .settings-index__label').map(el => el.text())
    expect(labels.some(l => l.includes('图片'))).toBe(true)
  })

  it('finds a row by an English word in the Chinese UI', async () => {
    const wrapper = mountIndex()

    await typeQuery(wrapper, 'IMAP')

    const labels = wrapper.findAll('.settings-index__hit .settings-index__label').map(el => el.text())
    expect(labels.some(l => l.includes('邮箱') || l.includes('收信'))).toBe(true)
  })

  it('opens a row hit narrowed to that row, with the query', async () => {
    const wrapper = mountIndex()
    await typeQuery(wrapper, '每多少分钟')

    const row = wrapper.find('[data-settings-hit="mail.sync_interval_minutes"]')
    expect(row.exists(), 'the interval row is reachable by its label').toBe(true)
    await row.trigger('click')

    const opened = wrapper.emitted('openSetting')
    expect(opened).toBeTruthy()
    const [categoryId, key, query] = opened![0] as [string, string, string]
    expect(categoryId).toBe('mail')
    expect(key).toBe('mail.sync_interval_minutes')
    expect(query).toBe('每多少分钟')
  })

  it('opens the page (unfiltered) when the category itself is picked', async () => {
    const wrapper = mountIndex()
    await typeQuery(wrapper, '邮件')

    // The category hit is the first row of its card (search ranks it below
    // real rows, but within 邮件 the category row is the broad answer).
    const row = wrapper.findAll('.settings-index__hit').find(r => !r.find('.settings-index__hit-desc').exists())!
    await row.trigger('click')

    expect(wrapper.emitted('openSetting')).toBeFalsy()
    expect(wrapper.emitted('navigate')).toBeTruthy()
  })

  it('shows the empty state for a query that matches nothing', async () => {
    const wrapper = mountIndex()

    await typeQuery(wrapper, 'zzzz-no-such-setting')

    expect(wrapper.findAll('.settings-index__hit').length).toBe(0)
    expect(wrapper.find('.settings-index__empty').exists()).toBe(true)
    expect(wrapper.findAll('.settings-card').length).toBe(0)
  })

  it('clearing the query brings the category list back', async () => {
    const wrapper = mountIndex()
    await typeQuery(wrapper, '邮件')
    await typeQuery(wrapper, '')

    expect(wrapper.findAll('.settings-index__row').length).toBe(32)
  })
})

// ── Highlighting (issue #691: 命中结果高亮展示) ──
describe('SettingsIndex search highlight', () => {
  beforeEach(async () => {
    const appI18n = (await import('@/i18n')).default
    appI18n.global.locale.value = 'zh'
  })

  it('marks the typed words in the label and the description', async () => {
    const wrapper = mountIndex()
    await wrapper.find('.search-input-stub').setValue('收信')

    const marks = wrapper.findAll('.settings-highlight__mark').map(m => m.text())
    expect(marks.length).toBeGreaterThan(0)
    expect(marks.every(m => m === '收信')).toBe(true)

    // The interval row's label is 每多少分钟收一次信 (no contiguous 收信), so its
    // mark must come from the description — the reason it is in the list.
    const row = wrapper.find('[data-settings-hit="mail.sync_interval_minutes"]')
    expect(row.exists()).toBe(true)
    expect(row.find('.settings-highlight__mark').exists()).toBe(true)
  })

  it('marks nothing when the query does not appear in the text', async () => {
    const wrapper = mountIndex()
    await wrapper.find('.search-input-stub').setValue('邮件')

    // 邮件功能 (the category row) is marked; a row matched only through its
    // panel title must not paint an empty <mark>.
    for (const mark of wrapper.findAll('.settings-highlight__mark')) {
      expect(mark.text().trim()).not.toBe('')
    }
  })

  it('leaves the text intact around the marks', async () => {
    const wrapper = mountIndex()
    await wrapper.find('.search-input-stub').setValue('收信')

    const row = wrapper.find('[data-settings-hit="mail.sync_interval_minutes"]')
    const label = row.find('.settings-index__label').text()
    expect(label).toBe('每多少分钟收一次信')
  })
})
