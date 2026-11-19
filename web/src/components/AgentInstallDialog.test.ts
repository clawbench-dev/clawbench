import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AgentInstallDialog from '@/components/AgentInstallDialog.vue'

const mockRegisterBackHandler = vi.fn((_: unknown) => vi.fn())
const mockInstallBackend = vi.fn()

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: (arg: unknown) => mockRegisterBackHandler(arg),
  PRIORITY_OVERLAY: 1000,
}))

// The HTTP layer is mocked: this file asserts the dialog's own wiring (button →
// install call → rendered outcome), not the request shape. The request shape is
// covered by useAgentInstall.test.ts.
vi.mock('@/composables/useAgentInstall', async () => {
  const { ref } = await import('vue')
  return {
    installBackend: (...args: unknown[]) => mockInstallBackend(...args),
    // Real-ish state machine so the component's rendering is exercised: the
    // composable is thin (spinner + error + result), and stubbing it entirely
    // would leave the template untested.
    useAgentInstall: () => {
      const installing = ref(false)
      const error = ref('')
      const result = ref(null as unknown)
      return {
        installing,
        error,
        result,
        run: async (backendId: string) => {
          installing.value = true
          error.value = ''
          result.value = null
          try {
            const res = await mockInstallBackend(backendId)
            result.value = res
            return res
          } catch (err) {
            error.value = err instanceof Error ? err.message : String(err)
            return null
          } finally {
            installing.value = false
          }
        },
        reset: () => { error.value = ''; result.value = null },
      }
    },
  }
})

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      welcomeInfo: {
        install: '安装',
        manualInstallHint: '安装命令：',
        installRunsOnServer: '安装在运行 ClawBench 的机器上',
        installSourceAuto: '安装时会自动挑选可用的下载源',
        installActualCommand: '实际执行：',
        installNow: '开始安装',
        installing: '正在安装…',
        installSuccess: '安装成功',
        installSuccessNeedsRescan: '安装成功但需要重新扫描',
        installFailedTitle: '安装失败',
      },
      common: { close: '关闭' },
    },
  },
})

/** Text of every note under the dialog, in render order. */
function noteTexts(): string[] {
  return Array.from(document.querySelectorAll('.install-note')).map((n) => n.textContent ?? '')
}

function mountDialog() {
  return mount(AgentInstallDialog, {
    props: {
      backendId: 'claude',
      backendName: 'Claude Code',
      installCmd: 'npm install -g @anthropic-ai/claude-code',
    },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

beforeEach(() => {
  mockInstallBackend.mockReset()
  mockInstallBackend.mockResolvedValue({
    ok: true,
    backend: 'claude',
    command: 'npm install -g @anthropic-ai/claude-code',
    output: 'added 1 package',
    durationMs: 1200,
    refreshed: true,
  })
})

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
      expect(cmd?.textContent).toContain('npm install -g @anthropic-ai/claude-code')
    })

    it('renders the manual install hint', () => {
      mountDialog()
      const hint = document.querySelector('.install-hint')
      expect(hint?.textContent).toContain('安装命令：')
    })

    it('states that the install runs on the server, not on this device', () => {
      // A remote server or container installs THERE. Saying so up front is what
      // prevents "it says installed but my laptop has nothing".
      mountDialog()
      expect(noteTexts().some((text) => text.includes('运行 ClawBench 的机器'))).toBe(true)
    })

    it('says the source is picked at install time for an npm install', () => {
      // The displayed command carries no source, so without this line the user
      // reads "npm install …" as "it will use npmjs".
      mountDialog()
      expect(noteTexts().some((text) => text.includes('自动挑选'))).toBe(true)
    })

    it('does not promise automatic source selection for a shell installer', () => {
      // `curl | bash` has no source to pick; the note would be a lie.
      mount(AgentInstallDialog, {
        props: { backendId: 'x', backendName: 'X', installCmd: 'curl -fsSL https://get.qoder.dev | bash' },
        global: { plugins: [i18n] },
        attachTo: document.body,
      })
      expect(noteTexts().some((text) => text.includes('自动挑选'))).toBe(false)
    })

    it('renders close button', () => {
      mountDialog()
      const closeBtn = document.querySelector('.install-actions .fbtn')
      expect(closeBtn).toBeTruthy()
      expect(closeBtn?.textContent).toContain('关闭')
    })

    it('offers the run button next to Close', () => {
      mountDialog()
      const runBtn = document.querySelector('.install-run-btn')
      expect(runBtn).toBeTruthy()
      expect(runBtn?.textContent).toContain('开始安装')
    })

    it('hides the run button when the backend has no install command', () => {
      // Without a command there is nothing to run; leaving the button would
      // offer an action that can only fail.
      mount(AgentInstallDialog, {
        props: { backendId: 'x', backendName: 'X', installCmd: '' },
        global: { plugins: [i18n] },
        attachTo: document.body,
      })
      expect(document.querySelector('.install-run-btn')).toBeNull()
    })
  })

  describe('installing', () => {
    it('calls the server with the backend id, never the command', async () => {
      // The command is the server's own registry value. Sending it from here
      // would turn the endpoint into remote execution of a client-supplied
      // string.
      const wrapper = mountDialog()
      const runBtn = document.querySelector('.install-run-btn') as HTMLElement
      runBtn.click()
      await flushPromises()

      expect(mockInstallBackend).toHaveBeenCalledWith('claude')
      expect(wrapper.emitted('installed')).toBeTruthy()
    })

    it('shows success and the installer output on success', async () => {
      mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      const status = document.querySelector('.install-status--ok')
      expect(status).toBeTruthy()
      expect(status?.textContent).toContain('安装成功')
      expect(status?.textContent).toContain('added 1 package')
      // The run button is replaced by the outcome: pressing install twice would
      // only hit the server's busy refusal.
      expect(document.querySelector('.install-run-btn')).toBeNull()
    })

    it('shows the command that actually ran when a source was added', async () => {
      // The source is chosen by the server's probe, so this is the only place
      // the user can see which one won.
      mockInstallBackend.mockResolvedValue({
        ok: true,
        backend: 'claude',
        command: 'npm install -g @anthropic-ai/claude-code --registry=https://registry.npmmirror.com',
        output: '',
        durationMs: 10,
        refreshed: true,
      })
      mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      const actual = document.querySelector('.install-cmd-row--actual')
      expect(actual).toBeTruthy()
      expect(actual?.textContent).toContain('--registry=https://registry.npmmirror.com')
    })

    it('does not repeat the command when the source made no difference', async () => {
      mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      expect(document.querySelector('.install-cmd-row--actual')).toBeNull()
    })

    it('asks for a rescan when the server could not refresh', async () => {
      mockInstallBackend.mockResolvedValue({
        ok: true, backend: 'claude', command: 'x', output: '', durationMs: 10, refreshed: false,
      })
      mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      expect(document.querySelector('.install-status--ok')?.textContent).toContain('重新扫描')
    })

    it('shows the server error message verbatim', async () => {
      // The message is the actionable part (missing npm, or npm's own output).
      // Replacing it with a generic sentence would throw away the only clue.
      mockInstallBackend.mockRejectedValue(new Error('未检测到 npm，请先安装 Node.js'))
      mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      const status = document.querySelector('.install-status--error')
      expect(status).toBeTruthy()
      expect(status?.textContent).toContain('安装失败')
      expect(status?.textContent).toContain('未检测到 npm')
    })

    it('keeps the run button after a failure so the user can retry', async () => {
      mockInstallBackend.mockRejectedValue(new Error('boom'))
      mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      expect(document.querySelector('.install-run-btn')).toBeTruthy()
    })

    it('ignores a backdrop click while installing', async () => {
      // Losing the output mid-install is the worst moment to lose it, and the
      // backdrop is the easiest accidental target. Escape and Close still work.
      let resolveInstall!: (v: unknown) => void
      mockInstallBackend.mockReturnValue(new Promise((res) => { resolveInstall = res }))

      const wrapper = mountDialog()
      ;(document.querySelector('.install-run-btn') as HTMLElement).click()
      await flushPromises()

      ;(document.querySelector('.install-overlay') as HTMLElement).click()
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('close')).toBeFalsy()

      resolveInstall({ ok: true, backend: 'claude', command: 'x', output: '', durationMs: 1, refreshed: true })
      await flushPromises()
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
      const closeBtn = document.querySelector('.install-actions .fbtn') as HTMLElement
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

      mountDialog()
      const copyBtn = document.querySelector('.btn-copy') as HTMLElement
      expect(copyBtn).toBeTruthy()

      copyBtn.click()
      await flushPromises()

      expect(writeText).toHaveBeenCalledWith('npm install -g @anthropic-ai/claude-code')
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
