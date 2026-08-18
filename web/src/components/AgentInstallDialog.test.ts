import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentInstallDialog from '@/components/AgentInstallDialog.vue'

const mockRegisterBackHandler = vi.fn(() => vi.fn())

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: (...args: any[]) => mockRegisterBackHandler(...args),
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
  vi.useRealTimers()
})

describe('AgentInstallDialog', () => {
  describe('rendering', () => {
    it('renders the dialog with backend name', () => {
      mountDialog()
      const overlay = document.querySelector('.install-overlay') as HTMLElement
      expect(overlay).toBeTruthy()
      expect(overlay.textContent).toContain('安装')
      expect(overlay.textContent).toContain('Claude Code')
    })

    it('renders the install command', () => {
      mountDialog()
      const cmd = document.querySelector('.install-cmd')
      expect(cmd?.textContent).toContain('npm i -g @anthropic/claude')
    })

    it('renders the manual install hint', () => {
      mountDialog()
      const hint = document.querySelector('.install-hint')
      expect(hint?.textContent).toContain('请手动执行以下命令')
    })

    it('renders close button', () => {
      mountDialog()
      const closeBtn = document.querySelector('.dlg-cancel')
      expect(closeBtn).toBeTruthy()
      expect(closeBtn?.textContent).toContain('关闭')
    })
  })

  describe('Escape key', () => {
    it('closes dialog on ESC key', async () => {
      const wrapper = mountDialog()
      const overlay = document.querySelector('.install-overlay') as HTMLElement
      expect(overlay).toBeTruthy()

      overlay.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('close')).toBeTruthy()
      expect(wrapper.emitted('close')).toHaveLength(1)
    })
  })

  describe('overlay click', () => {
    it('emits close when clicking overlay background', async () => {
      const wrapper = mountDialog()
      const overlay = document.querySelector('.install-overlay') as HTMLElement
      expect(overlay).toBeTruthy()

      overlay.click()
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })

  describe('close button', () => {
    it('emits close when clicking close button', async () => {
      const wrapper = mountDialog()
      const closeBtn = document.querySelector('.dlg-cancel') as HTMLElement
      expect(closeBtn).toBeTruthy()

      closeBtn.click()
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })

  describe('copy command', () => {
    it('copies command to clipboard when copy button clicked', async () => {
      vi.useFakeTimers()
      const writeText = vi.fn(() => Promise.resolve())
      Object.assign(navigator, { clipboard: { writeText } })

      const wrapper = mountDialog()
      const copyBtn = document.querySelector('.btn-copy') as HTMLElement
      expect(copyBtn).toBeTruthy()

      copyBtn.click()
      await wrapper.vm.$nextTick()

      expect(writeText).toHaveBeenCalledWith('npm i -g @anthropic/claude')
    })

    it('resets copied state after timeout', async () => {
      vi.useFakeTimers()
      const writeText = vi.fn(() => Promise.resolve())
      Object.assign(navigator, { clipboard: { writeText } })

      const wrapper = mountDialog()
      const copyBtn = document.querySelector('.btn-copy') as HTMLElement

      copyBtn.click()
      await wrapper.vm.$nextTick()

      // Advance past the 2s timeout
      vi.advanceTimersByTime(2100)
      await wrapper.vm.$nextTick()
    })
  })

  describe('back handler', () => {
    it('registers back handler on mount', () => {
      mountDialog()
      expect(mockRegisterBackHandler).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'agent-install-dialog',
          priority: 1000,
        })
      )
    })

    it('unregisters back handler on unmount', () => {
      const mockUnregister = vi.fn()
      mockRegisterBackHandler.mockReturnValue(mockUnregister)

      const wrapper = mountDialog()
      wrapper.unmount()

      expect(mockUnregister).toHaveBeenCalled()
    })
  })
})
