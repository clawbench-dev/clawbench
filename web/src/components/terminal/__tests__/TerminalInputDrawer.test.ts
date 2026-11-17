import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises, VueWrapper } from '@vue/test-utils'
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

// The drawer now lists recently used commands. Without this the real composable
// would fire an HTTP request from jsdom: it "passes" only because the rejection
// is swallowed, which makes the result depend on the environment.
const { mockApiGet, mockApiPost } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPost: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
  apiDelete: vi.fn(),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { w: vi.fn(), d: vi.fn(), i: vi.fn(), e: vi.fn() },
}))

import { _resetCommandHistoryForTesting } from '@/composables/useCommandHistory'
import { createVoiceTarget } from '@/utils/voiceTarget'

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
  mockApiGet.mockReset().mockResolvedValue([])
  mockApiPost.mockReset().mockResolvedValue({ added: 0 })
  _resetCommandHistoryForTesting()
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

    it('offers no dictation: the box takes a command, not prose', async () => {
      mountDrawer()
      await nextTick()
      const box = $('textarea.ti-textarea')!
      box.focus()
      // Voice input is global (mic button on touch, F9 on desktop); a shell
      // command dictated out loud runs nothing, so this field must not be a
      // dictation target.
      expect(createVoiceTarget(box)).toBeNull()
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

  describe('recent commands', () => {
    it('lists the five most recent commands and fills one on tap', async () => {
      mockApiGet.mockResolvedValue(
        Array.from({ length: 8 }, (_, i) => ({
          id: i + 1,
          command: `cmd-${i + 1}`,
          use_count: 1,
          last_used_at: '',
          project_path: '',
          project_only: false,
        })),
      )
      // Mount closed then open: the refresh hangs off the open transition, so
      // mounting already-open would never fire it (and that is the real flow).
      mountDrawer(false)
      await wrapper!.setProps({ open: true })
      await flushPromises()

      const items = Array.from(document.body.querySelectorAll('.ti-recent-item')) as HTMLElement[]
      expect(items).toHaveLength(5)
      expect(items[0].textContent?.trim()).toBe('cmd-1')

      click(items[0])
      await nextTick()
      expect(getText()).toBe('cmd-1')
    })

    it('narrows the list as the user types', async () => {
      mockApiGet.mockResolvedValue([
        { id: 1, command: 'git status', use_count: 3, last_used_at: '', project_path: '', project_only: false },
        { id: 2, command: 'git log --oneline', use_count: 2, last_used_at: '', project_path: '', project_only: false },
        { id: 3, command: 'docker ps', use_count: 1, last_used_at: '', project_path: '', project_only: false },
      ])
      mountDrawer(false)
      await wrapper!.setProps({ open: true })
      await flushPromises()
      expect(document.body.querySelectorAll('.ti-recent-item')).toHaveLength(3)

      const textarea = document.body.querySelector('textarea.ti-textarea') as HTMLTextAreaElement
      textarea.value = 'git'
      textarea.dispatchEvent(new Event('input'))
      await nextTick()

      const items = Array.from(document.body.querySelectorAll('.ti-recent-item')) as HTMLElement[]
      expect(items.map(el => el.textContent?.trim())).toEqual(['git status', 'git log --oneline'])
    })

    it('hides the list when nothing matches what was typed', async () => {
      mockApiGet.mockResolvedValue([
        { id: 1, command: 'git status', use_count: 1, last_used_at: '', project_path: '', project_only: false },
      ])
      mountDrawer(false)
      await wrapper!.setProps({ open: true })
      await flushPromises()

      const textarea = document.body.querySelector('textarea.ti-textarea') as HTMLTextAreaElement
      textarea.value = 'zzz'
      textarea.dispatchEvent(new Event('input'))
      await nextTick()

      expect(document.body.querySelector('.ti-recent')).toBeNull()
    })

    it('hides the recent block when there is no history', async () => {
      mountDrawer(false)
      await wrapper!.setProps({ open: true })
      await flushPromises()
      expect($('.ti-recent')).toBeNull()
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
