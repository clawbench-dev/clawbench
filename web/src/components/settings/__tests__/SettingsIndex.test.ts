import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
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
          chat: '聊天',
          agents: 'Agent偏好',
          files: '文件',
          terminal: '终端',
          tts: 'TTS语音',
          summarization: '摘要',
          rag: 'RAG记忆',
          portForward: '端口转发',
          push: '推送',
          security: '安全',
          android: 'Android',
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
  'lucide-map-pin': true,
  'lucide-message-square': true,
  'lucide-bot': true,
  'lucide-folder-open': true,
  'lucide-terminal': true,
  'lucide-volume2': true,
  'lucide-sparkles': true,
  'lucide-brain': true,
  'lucide-arrow-left-right': true,
  'lucide-bell': true,
  'lucide-shield': true,
  'lucide-smartphone': true,
  'lucide-info': true,
}

// Create a mutable ref so tests can toggle app mode
const isAppModeRef = ref(false)
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: isAppModeRef }),
}))

function mountIndex() {
  return mount(SettingsIndex, {
    global: { stubs: globalStubs, plugins: [i18n] },
  })
}

describe('SettingsIndex', () => {
  it('renders 13 category rows in web mode (no Android)', () => {
    isAppModeRef.value = false
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(13)
  })

  it('renders category labels in web mode', () => {
    isAppModeRef.value = false
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('外观')
    expect(labels).toContain('项目')
    expect(labels).toContain('聊天')
    expect(labels).toContain('文件')
    expect(labels).toContain('端口转发')
    expect(labels).toContain('推送')
    expect(labels).toContain('安全')
    expect(labels).toContain('关于')
  })

  it('emits navigate with categoryId when row clicked', async () => {
    isAppModeRef.value = false
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    await rows[0].trigger('click')

    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['appearance'])
  })

  it('emits correct categoryId for each row in web mode', async () => {
    isAppModeRef.value = false
    const wrapper = mountIndex()

    const expectedIds = [
      'appearance', 'project', 'chat', 'agents', 'files', 'terminal',
      'tts', 'summarization', 'rag', 'portForward', 'push', 'security', 'about',
    ]

    const rows = wrapper.findAll('.settings-index__row')
    for (let i = 0; i < expectedIds.length; i++) {
      await rows[i].trigger('click')
      expect(wrapper.emitted('navigate')![i]).toEqual([expectedIds[i]])
    }
  })

  it('shows 14 categories including Android in app mode', () => {
    isAppModeRef.value = true
    const wrapper = mountIndex()

    const rows = wrapper.findAll('.settings-index__row')
    expect(rows.length).toBe(14)

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).toContain('Android')

    // Reset
    isAppModeRef.value = false
  })

  it('does not show Android category in web mode', () => {
    isAppModeRef.value = false
    const wrapper = mountIndex()

    const labels = wrapper.findAll('.settings-index__label').map(el => el.text())
    expect(labels).not.toContain('Android')
  })
})
