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
          projectFiles: '项目与文件',
          chat: '聊天',
          agents: 'Agent偏好',
          terminal: '终端',
          tts: 'TTS语音',
          stt: '语音识别',
          rag: '会话搜索',
          portForward: '端口映射',
          frp: '内网穿透',
          notification: '消息通知',
          security: '安全',
          debug: '调试',
          about: '关于',
        },
        groups: {
          appearanceFiles: '外观与文件',
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
  'lucide-globe': true,
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
  it('renders 16 category rows', () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(16)
  })

  it('renders category labels', () => {
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('外观')
    expect(labels).toContain('项目与文件')
    expect(labels).toContain('聊天')
    expect(labels).toContain('端口映射')
    expect(labels).toContain('内网穿透')
    expect(labels).toContain('安全')
    expect(labels).toContain('调试')
    expect(labels).toContain('关于')
  })

  it('groups categories into 5 titled cards', () => {
    const wrapper = mountIndex()

    const cards = wrapper.findAll('.settings-card')
    expect(cards.length).toBe(5)

    const titles = wrapper.findAll('.settings-card__header').map(el => el.text())
    expect(titles).toEqual(['外观与文件', 'AI 与对话', '连接与集成', '通知与安全', '系统与关于'])
  })

  it('distributes every category into exactly one group', () => {
    const wrapper = mountIndex()

    const perCard = wrapper.findAll('.settings-card').map(card =>
      card.findAll('.settings-index__row').length,
    )
    expect(perCard).toEqual([2, 6, 4, 2, 2])
    expect(perCard.reduce((a, b) => a + b, 0)).toBe(16)
  })

  it('emits navigate with categoryId when row clicked', async () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    await rows[0].trigger('click')

    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['appearance'])
  })

  it('emits correct categoryId for each row', async () => {
    const wrapper = mountIndex()

    const expectedIds = [
      'appearance', 'projectFiles',
      'chat', 'agents', 'aiSummary', 'rag', 'tts', 'stt',
      'terminal', 'portForward', 'frp', 'forgeIntegration',
      'notification', 'security',
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
