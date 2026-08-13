import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import TerminalInputDrawer from '@/components/terminal/TerminalInputDrawer.vue'

// ── Mocks ────────────────────────────────────────────────────
const { mockReadClipboardText, mockToastShow } = vi.hoisted(() => ({
  mockReadClipboardText: vi.fn(),
  mockToastShow: vi.fn(),
}))

vi.mock('@/utils/clipboard', () => ({
  readClipboardText: mockReadClipboardText,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// ── i18n ─────────────────────────────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      terminal: {
        input: '输入',
        inputPlaceholder: '输入要发送到终端的内容…',
        inputSend: '输入到终端',
        inputFillClipboard: '填入剪贴板内容',
        clipboardEmpty: '剪贴板为空',
        clipboardReadFailed: '无法读取剪贴板',
      },
    },
  },
})

let wrapper: VueWrapper<any> | null = null
let container: HTMLDivElement

beforeEach(() => {
  mockReadClipboardText.mockReset()
  mockToastShow.mockReset()
  container = document.createElement('div')
  document.body.appendChild(container)
})

afterEach(() => {
  if (wrapper) {
    wrapper.unmount()
    wrapper = null
  }
  if (container.parentNode) {
    document.body.removeChild(container)
  }
})

/** Find element in document.body (includes teleported BottomSheet content) */
function $(selector: string) {
  return document.body.querySelector(selector) as HTMLElement | null
}

/** Internal text ref (auto-unwrapped via vm) */
function getText() {
  return (wrapper!.vm as any).text as string
}

/** Click a teleported element. Uses dispatchEvent because JSDOM treats
 *  `.click()` on a `disabled` button as a no-op (disabled reflection quirk). */
function click(el: HTMLElement | null) {
  el!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

async function fillFromClipboard() {
  click($('.ti-btn-fill'))
  await nextTick()
  await nextTick()
}

function mountDrawer(open = true) {
  wrapper = mount(TerminalInputDrawer, {
    props: { open },
    global: { plugins: [i18n] },
    attachTo: container,
  })
  return wrapper
}

describe('TerminalInputDrawer', () => {
  describe('rendering', () => {
    it('renders a textarea when open', async () => {
      mountDrawer()
      await nextTick()
      expect($('textarea.ti-textarea')).toBeTruthy()
    })

    it('renders the fill-from-clipboard and send buttons in the header', async () => {
      mountDrawer()
      await nextTick()
      expect($('.ti-btn-fill')).toBeTruthy()
      expect($('.ti-btn-send')).toBeTruthy()
    })
  })

  describe('fill from clipboard', () => {
    it('loads clipboard content into the editor', async () => {
      mockReadClipboardText.mockResolvedValue('hello clipboard')
      mountDrawer()
      await nextTick()

      await fillFromClipboard()

      expect(mockReadClipboardText).toHaveBeenCalled()
      expect(getText()).toBe('hello clipboard')
    })

    it('shows a toast and keeps the editor empty when clipboard is empty', async () => {
      mockReadClipboardText.mockResolvedValue('')
      mountDrawer()
      await nextTick()

      await fillFromClipboard()

      expect(mockToastShow).toHaveBeenCalledWith('剪贴板为空', expect.anything())
      expect(getText()).toBe('')
    })

    it('shows an error toast when clipboard read fails', async () => {
      mockReadClipboardText.mockRejectedValue(new Error('denied'))
      mountDrawer()
      await nextTick()

      await fillFromClipboard()

      expect(mockToastShow).toHaveBeenCalledWith('无法读取剪贴板', expect.anything())
    })
  })

  describe('input', () => {
    it('emits input with the editor content and closes', async () => {
      mockReadClipboardText.mockResolvedValue('ls -la')
      mountDrawer()
      await nextTick()
      await fillFromClipboard()
      expect(getText()).toBe('ls -la')

      click($('.ti-btn-send'))
      await nextTick()

      const inputEmits = wrapper!.emitted('input')
      expect(inputEmits).toBeTruthy()
      expect(inputEmits![0]).toEqual(['ls -la'])
      expect(wrapper!.emitted('close')).toBeTruthy()
    })

    it('does not emit input when the editor is empty', async () => {
      mountDrawer()
      await nextTick()

      click($('.ti-btn-send'))
      await nextTick()

      expect(wrapper!.emitted('input')).toBeUndefined()
    })
  })

  describe('clear', () => {
    it('clears the editor content', async () => {
      mockReadClipboardText.mockResolvedValue('to be cleared')
      mountDrawer()
      await nextTick()
      await fillFromClipboard()
      expect(getText()).toBe('to be cleared')

      click($('.ti-btn-clear'))
      await nextTick()

      expect(getText()).toBe('')
    })
  })
})
