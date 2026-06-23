import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import DialogOverlay from '@/components/common/DialogOverlay.vue'

// --- Shared mock state ---
// We create a fresh ref for each test and inject it via the mock factory
let dlgState = ref({
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

let mockResolve = vi.fn<(result: string | boolean | null) => void>()

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
let wrapper: VueWrapper<any> | null = null
let container: HTMLDivElement

/**
 * Mount DialogOverlay with the current dlgState.
 * Must be called AFTER setting dlgState.value to desired values.
 */
function mountDialog() {
  container = document.createElement('div')
  document.body.appendChild(container)
  wrapper = mount(DialogOverlay, {
    attachTo: container,
    global: { plugins: [i18n] },
  })
  return wrapper
}

/** Find element in document.body (includes teleported content) */
function $(selector: string) {
  return document.body.querySelector(selector) as HTMLElement | null
}

/** Reset mock state for a new test */
function resetState(overrides: Partial<typeof dlgState.value> = {}) {
  dlgState = ref({
    visible: false,
    type: 'confirm',
    title: '',
    message: '',
    value: '',
    placeholder: '',
    confirmText: '',
    cancelText: '',
    dangerous: false,
    resolve: null,
    ...overrides,
  })
  mockResolve = vi.fn()
}

describe('DialogOverlay', () => {
  afterEach(() => {
    if (wrapper) {
      wrapper.unmount()
      wrapper = null
    }
    document.body.querySelectorAll('.dlg-overlay').forEach(el => el.remove())
    if (container?.parentNode) {
      document.body.removeChild(container)
    }
    vi.restoreAllMocks()
  })

  it('renders nothing when not visible', async () => {
    resetState({ visible: false })
    mountDialog()
    await nextTick()

    expect($('.dlg-overlay')).toBeFalsy()
  })

  describe('Alert dialog', () => {
    it('shows title and message', async () => {
      resetState({ visible: true, type: 'alert', title: 'Alert Title', message: 'Alert message' })
      mountDialog()
      await nextTick()

      expect($('.dlg-title')?.textContent).toBe('Alert Title')
      expect($('.dlg-msg')?.textContent).toBe('Alert message')
    })

    it('shows only OK button', async () => {
      resetState({ visible: true, type: 'alert' })
      mountDialog()
      await nextTick()

      expect($('.dlg-cancel')).toBeFalsy()
      expect($('.dlg-ok')?.textContent).toBe('OK')
    })

    it('resolves true when OK is clicked', async () => {
      resetState({ visible: true, type: 'alert' })
      mountDialog()
      await nextTick()

      $('.dlg-ok')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith(true)
    })
  })

  describe('Confirm dialog', () => {
    it('shows Cancel and Confirm buttons', async () => {
      resetState({ visible: true, type: 'confirm', message: 'Sure?' })
      mountDialog()
      await nextTick()

      expect($('.dlg-cancel')?.textContent).toBe('Cancel')
      expect($('.dlg-ok')?.textContent).toBe('Confirm')
    })

    it('resolves true when Confirm is clicked', async () => {
      resetState({ visible: true, type: 'confirm' })
      mountDialog()
      await nextTick()

      $('.dlg-ok')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith(true)
    })

    it('resolves false when Cancel is clicked', async () => {
      resetState({ visible: true, type: 'confirm' })
      mountDialog()
      await nextTick()

      $('.dlg-cancel')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith(false)
    })
  })

  describe('Prompt dialog', () => {
    it('shows input field', async () => {
      resetState({ visible: true, type: 'prompt', placeholder: 'Enter value' })
      mountDialog()
      await nextTick()

      const input = $('.dlg-input')
      expect(input).toBeTruthy()
      expect(input?.getAttribute('placeholder')).toBe('Enter value')
    })

    it('resolves input value when Confirm is clicked', async () => {
      resetState({ visible: true, type: 'prompt', value: 'hello' })
      mountDialog()
      await nextTick()

      // Set inputVal directly via component instance since v-model on
      // teleported elements doesn't respond to native input events in jsdom
      wrapper!.vm.inputVal = 'my input'
      await nextTick()

      $('.dlg-ok')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith('my input')
    })

    it('resolves null when Cancel is clicked', async () => {
      resetState({ visible: true, type: 'prompt' })
      mountDialog()
      await nextTick()

      $('.dlg-cancel')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith(null)
    })

    it('resolves null when input is empty and Confirm is clicked', async () => {
      resetState({ visible: true, type: 'prompt', value: '' })
      mountDialog()
      await nextTick()

      $('.dlg-ok')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith(null)
    })
  })

  describe('Dangerous confirm', () => {
    it('applies dlg-danger class when dangerous is true', async () => {
      resetState({ visible: true, type: 'confirm', dangerous: true })
      mountDialog()
      await nextTick()

      expect($('.dlg-ok')?.classList.contains('dlg-danger')).toBe(true)
    })

    it('does not apply dlg-danger class when dangerous is false', async () => {
      resetState({ visible: true, type: 'confirm', dangerous: false })
      mountDialog()
      await nextTick()

      expect($('.dlg-ok')?.classList.contains('dlg-danger')).toBe(false)
    })
  })

  describe('Custom button text', () => {
    it('uses cancelText override', async () => {
      resetState({ visible: true, type: 'confirm', cancelText: 'Nope' })
      mountDialog()
      await nextTick()

      expect($('.dlg-cancel')?.textContent).toBe('Nope')
    })

    it('uses confirmText override', async () => {
      resetState({ visible: true, type: 'confirm', confirmText: 'Do it' })
      mountDialog()
      await nextTick()

      expect($('.dlg-ok')?.textContent).toBe('Do it')
    })

    it('uses confirmText for alert dialog', async () => {
      resetState({ visible: true, type: 'alert', confirmText: 'Got it' })
      mountDialog()
      await nextTick()

      expect($('.dlg-ok')?.textContent).toBe('Got it')
    })
  })

  describe('Overlay click', () => {
    it('calls handleCancel when overlay is clicked', async () => {
      resetState({ visible: true, type: 'confirm' })
      mountDialog()
      await nextTick()

      // @click.self fires when click target is the overlay itself
      const overlay = $('.dlg-overlay')!
      overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith(false)
    })

    it('does not call handleCancel when dialog box is clicked', async () => {
      resetState({ visible: true, type: 'confirm' })
      mountDialog()
      await nextTick()

      // Click the box — @click.stop on dlg-box prevents bubbling
      const box = $('.dlg-box')!
      box.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(mockResolve).not.toHaveBeenCalled()
    })
  })

  describe('Input reset on open', () => {
    it('resets inputVal to dlg.state.value.value when visible becomes true', async () => {
      // Mount with visible=false first, then set visible=true to trigger the watcher
      resetState({ visible: false, type: 'prompt', value: 'prefilled' })
      mountDialog()
      await nextTick()

      // Now open the dialog
      dlgState.value.visible = true
      await nextTick()
      await nextTick()

      // Check the component's inputVal reactive state
      expect(wrapper!.vm.inputVal).toBe('prefilled')
    })

    it('resets inputVal to empty string when value is empty', async () => {
      resetState({ visible: false, type: 'prompt', value: '' })
      mountDialog()
      await nextTick()

      dlgState.value.visible = true
      await nextTick()
      await nextTick()

      expect(wrapper!.vm.inputVal).toBe('')
    })
  })

  describe('Enter key on prompt input', () => {
    it('triggers handleConfirm on Enter keydown', async () => {
      resetState({ visible: true, type: 'prompt', value: 'test value' })
      mountDialog()
      await nextTick()

      // The watcher doesn't run on initial mount (no immediate:true), so set inputVal directly
      wrapper!.vm.inputVal = 'test value'
      await nextTick()

      // Trigger Enter keydown on the input element
      const input = $('.dlg-input')!
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
      await nextTick()
      expect(mockResolve).toHaveBeenCalledWith('test value')
    })
  })

  describe('No title', () => {
    it('does not render title element when title is empty', async () => {
      resetState({ visible: true, type: 'alert', title: '', message: 'No title' })
      mountDialog()
      await nextTick()

      expect($('.dlg-title')).toBeFalsy()
    })
  })

  describe('Long message word-break', () => {
    it('renders long path without overflow', async () => {
      const longPath = '/home/user/projects/some-very-long-path-name/.codebuddy/worktrees/fix-lint-v0.48.0'
      resetState({ visible: true, type: 'alert', message: `fatal: '${longPath}' contains modified or untracked files` })
      mountDialog()
      await nextTick()

      const msg = $('.dlg-msg')
      expect(msg).toBeTruthy()
      expect(msg?.textContent).toContain(longPath)
      // Verify word-break style is applied via CSS
      const style = getComputedStyle(msg!)
      expect(style.wordBreak).toBe('break-word')
    })
  })
})
