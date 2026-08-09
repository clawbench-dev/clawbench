import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentInstallDialog from '@/components/AgentInstallDialog.vue'

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: vi.fn(() => vi.fn()),
  PRIORITY_OVERLAY: 1000,
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      welcomeInfo: {
        install: '安装',
        manualInstallHint: '请手动执行以下命令',
      },
      common: { close: '关闭' },
    },
  },
})

function mountDialog() {
  return mount(AgentInstallDialog, {
    props: {
      backendName: 'Claude Code',
      installCmd: 'npm i -g @anthropic/claude',
    },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

afterEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('AgentInstallDialog — Escape 关闭', () => {
  it('按 ESC 关闭对话框', async () => {
    const wrapper = mountDialog()
    const overlay = document.querySelector('.install-overlay') as HTMLElement
    expect(overlay).toBeTruthy()

    overlay.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('渲染安装命令内容', () => {
    mountDialog()
    const cmd = document.querySelector('.install-cmd')
    expect(cmd?.textContent).toContain('npm i -g @anthropic/claude')
  })
})
