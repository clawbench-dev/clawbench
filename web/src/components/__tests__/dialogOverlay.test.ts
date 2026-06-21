import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import DialogOverlay from '@/components/common/DialogOverlay.vue'

// --- Mock useDialog ---
function createDialogMock() {
  const state = ref({
    visible: false,
    type: 'confirm' as 'confirm' | 'prompt' | 'alert',
    title: '',
    message: '',
    value: '',
    placeholder: '',
    confirmText: '',
    cancelText: '',
    dangerous: false,
    resolve: null as ((v: string | boolean | null) => void) | null,
  })

  const mockResolve = vi.fn<(result: string | boolean | null) => void>()

  return { state, mockResolve }
}

const { state: dlgState, mockResolve } = createDialogMock()

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    state: dlgState,
    resolve: mockResolve,
  }),
}))

// --- i18n ---
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { cancel: 'Cancel', ok: 'OK', confirm: 'Confirm' },
    },
  },
})

// --- Mount helper ---
const TeleportStub = { template: '<div><slot /></div>' }

function mountDialog() {
  return mount(DialogOverlay, {
    global: {
      stubs: { Teleport: TeleportStub },
      plugins: [i18n],
    },
  })
}

// Helper: set state and wait for reactivity
async function openDialog(overrides: Partial<typeof dlgState.value> = {}) {
  dlgState.value.visible = false
  await nextTick()
  Object.assign(dlgState.value, { visible: true, type: 'confirm', title: '', message: '', value: '', placeholder: '', confirmText: '', cancelText: '', dangerous: false, resolve: null, ...overrides })
  await nextTick()
}

describe('DialogOverlay', () => {
  beforeEach(() => {
    mockResolve.mockClear()
    dlgState.value.visible = false
  })
  it('renders nothing when not visible', () => {
    dlgState.value.visible = false
    const wrapper = mountDialog()
    expect(wrapper.find('.dlg-overlay').exists()).toBe(false)
  })

  describe('Alert dialog', () => {
    it('shows title and message', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'alert', title: 'Alert Title', message: 'Alert message' })

      expect(wrapper.find('.dlg-title').text()).toBe('Alert Title')
      expect(wrapper.find('.dlg-msg').text()).toBe('Alert message')
    })

    it('shows only OK button', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'alert' })

      expect(wrapper.find('.dlg-cancel').exists()).toBe(false)
      expect(wrapper.find('.dlg-ok').text()).toBe('OK')
    })

    it('resolves true when OK is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'alert' })

      await wrapper.find('.dlg-ok').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith(true)
    })
  })

  describe('Confirm dialog', () => {
    it('shows Cancel and Confirm buttons', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm', message: 'Sure?' })

      expect(wrapper.find('.dlg-cancel').text()).toBe('Cancel')
      expect(wrapper.find('.dlg-ok').text()).toBe('Confirm')
    })

    it('resolves true when Confirm is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm' })

      await wrapper.find('.dlg-ok').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith(true)
    })

    it('resolves false when Cancel is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm' })

      await wrapper.find('.dlg-cancel').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith(false)
    })
  })

  describe('Prompt dialog', () => {
    it('shows input field', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt', placeholder: 'Enter value' })

      const input = wrapper.find('.dlg-input')
      expect(input.exists()).toBe(true)
      expect(input.attributes('placeholder')).toBe('Enter value')
    })

    it('resolves input value when Confirm is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt', value: 'hello' })

      await wrapper.find('.dlg-input').setValue('my input')
      await wrapper.find('.dlg-ok').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith('my input')
    })

    it('resolves null when Cancel is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt' })

      await wrapper.find('.dlg-cancel').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith(null)
    })

    it('resolves null when input is empty and Confirm is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt', value: '' })

      await wrapper.find('.dlg-ok').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith(null)
    })
  })

  describe('Dangerous confirm', () => {
    it('applies dlg-danger class when dangerous is true', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm', dangerous: true })

      expect(wrapper.find('.dlg-ok').classes()).toContain('dlg-danger')
    })

    it('does not apply dlg-danger class when dangerous is false', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm', dangerous: false })

      expect(wrapper.find('.dlg-ok').classes()).not.toContain('dlg-danger')
    })
  })

  describe('Custom button text', () => {
    it('uses cancelText override', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm', cancelText: 'Nope' })

      expect(wrapper.find('.dlg-cancel').text()).toBe('Nope')
    })

    it('uses confirmText override', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm', confirmText: 'Do it' })

      expect(wrapper.find('.dlg-ok').text()).toBe('Do it')
    })

    it('uses confirmText for alert dialog', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'alert', confirmText: 'Got it' })

      expect(wrapper.find('.dlg-ok').text()).toBe('Got it')
    })
  })

  describe('Overlay click', () => {
    it('calls handleCancel when overlay is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm' })

      await wrapper.find('.dlg-overlay').trigger('click')
      expect(mockResolve).toHaveBeenCalledWith(false)
    })

    it('does not call handleCancel when dialog box is clicked', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'confirm' })

      await wrapper.find('.dlg-box').trigger('click')
      expect(mockResolve).not.toHaveBeenCalled()
    })
  })

  describe('Input reset on open', () => {
    it('resets inputVal to dlg.state.value.value when visible becomes true', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt', value: 'prefilled' })
      await nextTick()

      expect((wrapper.find('.dlg-input').element as HTMLInputElement).value).toBe('prefilled')
    })

    it('resets inputVal to empty string when value is empty', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt', value: '' })
      await nextTick()

      expect((wrapper.find('.dlg-input').element as HTMLInputElement).value).toBe('')
    })
  })

  describe('Enter key on prompt input', () => {
    it('triggers handleConfirm on Enter keydown', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'prompt', value: 'test value' })

      await wrapper.find('.dlg-input').trigger('keydown.enter')
      expect(mockResolve).toHaveBeenCalledWith('test value')
    })
  })

  describe('No title', () => {
    it('does not render title element when title is empty', async () => {
      const wrapper = mountDialog()
      await openDialog({ type: 'alert', title: '', message: 'No title' })

      expect(wrapper.find('.dlg-title').exists()).toBe(false)
    })
  })

  describe('Long message word-break', () => {
    it('renders long path without overflow', async () => {
      const wrapper = mountDialog()
      const longPath = '/home/user/projects/some-very-long-path-name/.codebuddy/worktrees/fix-lint-v0.48.0'
      await openDialog({ type: 'alert', message: `fatal: '${longPath}' contains modified or untracked files` })

      const msg = wrapper.find('.dlg-msg')
      expect(msg.exists()).toBe(true)
      expect(msg.text()).toContain(longPath)
      // Verify word-break style is applied
      const style = getComputedStyle(msg.element)
      expect(style.wordBreak).toBe('break-word')
    })
  })
})
