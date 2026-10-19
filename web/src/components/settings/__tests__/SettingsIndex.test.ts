import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SettingsIndex from '@/components/settings/SettingsIndex.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        categories: {
          appearance: '外观',
          project: '项目',
          files: '文件',
          chat: '聊天',
          agents: 'Agent偏好',
          terminal: '终端',
          tts: 'TTS语音',
          stt: '语音识别',
          rag: '会话搜索',
          portForward: '端口映射',
          notification: '消息通知',
          usageMeter: '用量',
          security: '安全',
          debug: '调试',
          about: '关于',
        },
        groups: {
          projectAppearance: '项目与外观',
          aiChat: 'AI 与对话',
          connectivity: '连接与集成',
          notifySecurity: '通知与安全',
          systemAbout: '系统与关于',
        },
      },
    },
  },
})

// Stub lucide-vue-next icons
const globalStubs = {
  'lucide-chevron-right': true,
  'lucide-palette': true,
  'lucide-folder-tree': true,
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
  it('renders 19 category rows', () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(19)
  })

  it('renders category labels', () => {
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('项目')
    expect(labels).toContain('外观')
    expect(labels).toContain('文件')
    expect(labels).toContain('聊天')
    expect(labels).toContain('端口映射')
    expect(labels).toContain('用量')
    expect(labels).toContain('安全')
    expect(labels).toContain('调试')
    expect(labels).toContain('关于')
  })

  it('groups categories into 5 titled cards', () => {
    const wrapper = mountIndex()

    const cards = wrapper.findAll('.settings-card')
    expect(cards.length).toBe(5)

    const titles = wrapper.findAll('.settings-card__header').map(el => el.text())
    expect(titles).toEqual(['项目与外观', 'AI 与对话', '连接与集成', '通知与安全', '系统与关于'])
  })

  it('distributes every category into exactly one group', () => {
    const wrapper = mountIndex()

    const perCard = wrapper.findAll('.settings-card').map(card =>
      card.findAll('.settings-index__row').length,
    )
    // 通知与安全 holds three rows: 推送通知, 用量, 安全. The usage meter sits
    // beside push notifications rather than inside that category.
    // AI 与对话 gained MCP 服务 (7 → 8).
    expect(perCard).toEqual([3, 8, 3, 3, 2])
    expect(perCard.reduce((a, b) => a + b, 0)).toBe(19)
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
      'project', 'appearance', 'files',
      'chat', 'agents', 'skills', 'mcp', 'aiSummary', 'rag', 'tts', 'stt',
      'terminal', 'portForward', 'forgeIntegration',
      'notification', 'usageMeter', 'security',
      'debug', 'about',
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
// bug invisible (it silently lacks skills / mcp / aiSummary / forgeIntegration).
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
