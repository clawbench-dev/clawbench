import { describe, expect, it, vi } from 'vitest'
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
          rag: 'RAG记忆',
          portForward: '端口转发',
          frp: 'FRP内网穿透',
          notification: '消息通知',
          security: '安全',
          debug: '调试',
          about: '关于',
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
  it('renders 14 category rows', () => {
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(14)
  })

  it('renders category labels', () => {
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('外观')
    expect(labels).toContain('项目与文件')
    expect(labels).toContain('聊天')
    expect(labels).toContain('端口转发')
    expect(labels).toContain('FRP内网穿透')
    expect(labels).toContain('安全')
    expect(labels).toContain('调试')
    expect(labels).toContain('关于')
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
      'appearance', 'projectFiles', 'chat', 'agents', 'terminal',
      'tts', 'stt', 'rag', 'portForward', 'frp', 'notification', 'security', 'debug', 'about',
    ]

    const rows = wrapper.findAll('.settings-index__row')
    for (let i = 0; i < expectedIds.length; i++) {
      await rows[i].trigger('click')
      expect(wrapper.emitted('navigate')![i]).toEqual([expectedIds[i]])
    }
  })
})
