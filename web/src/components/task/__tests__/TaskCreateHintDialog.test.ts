import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// ── Mocks ──

const mockRegisterBackHandler = vi.fn((_: unknown) => vi.fn())
vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: (arg: unknown) => mockRegisterBackHandler(arg),
  PRIORITY_OVERLAY: 1000,
}))

const mockCopyText = vi.fn((_text: string, onSuccess?: () => void) => onSuccess?.())
vi.mock('@/utils/clipboard', () => ({
  copyText: (text: string, onSuccess?: () => void) => mockCopyText(text, onSuccess),
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, props: { size: Number }, template: '<svg />' })
  return { Sparkles: stub('Sparkles'), Copy: stub('Copy'), Check: stub('Check') }
})

import TaskCreateHintDialog from '../TaskCreateHintDialog.vue'
import { TASK_CREATE_HINT_KEY, isTaskCreateHintDismissed, dismissTaskCreateHint } from '@/composables/useTaskCreateHint'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { copy: '复制', copied: '已复制' },
      task: {
        createHint: {
          title: '使用 AI 管理任务更便捷',
          desc: '在对话中直接描述需求',
          exampleLabel: '试试在对话中输入：',
          exampleCommand: '/cb-task 每天下午6点帮我把今天的工作整理成日报',
          manual: '我要手动创建',
          dontShowAgain: '不再提示',
        },
      },
    },
  },
})

function mountDialog(open = true) {
  return mount(TaskCreateHintDialog, {
    props: { open },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

function body() {
  return document.body
}

beforeEach(() => {
  localStorage.clear()
  mockRegisterBackHandler.mockReset()
  mockRegisterBackHandler.mockReturnValue(vi.fn())
  mockCopyText.mockClear()
})

afterEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.useRealTimers()
})

describe('TaskCreateHintDialog', () => {
  describe('rendering', () => {
    it('renders the title, description and the real /cb-task example', () => {
      mountDialog()
      const text = body().textContent || ''
      expect(text).toContain('使用 AI 管理任务更便捷')
      expect(text).toContain('在对话中直接描述需求')
      // The example must be the actual built-in command. A "/task" shorthand
      // would be forwarded to the agent as an unknown slash command and create
      // nothing, so the hint would teach a command that silently does nothing.
      expect(body().querySelector('.tch-cmd')?.textContent)
        .toBe('/cb-task 每天下午6点帮我把今天的工作整理成日报')
    })

    it('renders both footer actions', () => {
      mountDialog()
      const buttons = Array.from(body().querySelectorAll('.modal-footer .fbtn'))
      expect(buttons.map(b => b.textContent?.trim()))
        .toEqual(['不再提示', '我要手动创建'])
    })
  })

  describe('manual create', () => {
    it('emits manual when the primary button is clicked', async () => {
      const wrapper = mountDialog()
      const manualBtn = Array.from(body().querySelectorAll('.modal-footer .fbtn'))
        .find(b => b.textContent?.includes('我要手动创建')) as HTMLElement

      manualBtn.click()
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('manual')).toHaveLength(1)
      // Choosing the manual route is not a dismissal — the hint must still be
      // shown on the next "+" click.
      expect(wrapper.emitted('close')).toBeFalsy()
      expect(isTaskCreateHintDismissed()).toBe(false)
    })
  })

  describe('dont show again', () => {
    it('persists the dismissal and closes without opening the form', async () => {
      const wrapper = mountDialog()
      const dontShowBtn = Array.from(body().querySelectorAll('.modal-footer .fbtn'))
        .find(b => b.textContent?.includes('不再提示')) as HTMLElement

      dontShowBtn.click()
      await wrapper.vm.$nextTick()

      expect(isTaskCreateHintDismissed()).toBe(true)
      expect(localStorage.getItem(TASK_CREATE_HINT_KEY)).toBe('true')
      expect(wrapper.emitted('close')).toHaveLength(1)
      // A dismissal answers "stop showing this", so it must NOT also launch the
      // form the user never asked for.
      expect(wrapper.emitted('manual')).toBeFalsy()
    })
  })

  describe('copy example', () => {
    it('copies the example command and flips the icon', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog()
      const copyBtn = body().querySelector('.tch-copy') as HTMLElement

      copyBtn.click()
      await wrapper.vm.$nextTick()

      expect(mockCopyText).toHaveBeenCalledWith(
        '/cb-task 每天下午6点帮我把今天的工作整理成日报',
        expect.any(Function),
      )
      expect(wrapper.findComponent({ name: 'Check' }).exists()).toBe(true)

      // The check mark is a transient affordance; it must revert.
      vi.advanceTimersByTime(2100)
      await wrapper.vm.$nextTick()
      expect(wrapper.findComponent({ name: 'Check' }).exists()).toBe(false)
      expect(wrapper.findComponent({ name: 'Copy' }).exists()).toBe(true)
    })

    it('resets the copied state when reopened', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog()
      ;(body().querySelector('.tch-copy') as HTMLElement).click()
      await wrapper.vm.$nextTick()
      expect(wrapper.findComponent({ name: 'Check' }).exists()).toBe(true)

      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true })
      await wrapper.vm.$nextTick()

      // A stale check mark on reopen would claim a copy that never happened
      // for this visit.
      expect(wrapper.findComponent({ name: 'Check' }).exists()).toBe(false)
    })
  })

  describe('back handler', () => {
    it('registers an overlay-priority handler while open', () => {
      mountDialog()
      expect(mockRegisterBackHandler).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'task-create-hint', priority: 1000 }),
      )
    })

    it('closes (rather than falling through) on back', async () => {
      const wrapper = mountDialog()
      const handler = mockRegisterBackHandler.mock.calls[0][0] as { goBack: () => void }

      handler.goBack()
      await wrapper.vm.$nextTick()

      // If back were left unhandled here, the task tab's drill-down handler
      // would run instead and let back exit the app with the dialog still up.
      expect(wrapper.emitted('close')).toHaveLength(1)
    })

    it('unregisters when closed', async () => {
      const unregister = vi.fn()
      mockRegisterBackHandler.mockReturnValue(unregister)
      const wrapper = mountDialog()

      await wrapper.setProps({ open: false })
      expect(unregister).toHaveBeenCalled()
    })
  })
})

describe('useTaskCreateHint storage', () => {
  it('defaults to not dismissed', () => {
    expect(isTaskCreateHintDismissed()).toBe(false)
  })

  it('stays dismissed after dismissTaskCreateHint', () => {
    dismissTaskCreateHint()
    expect(isTaskCreateHintDismissed()).toBe(true)
  })

  it('degrades to "not dismissed" when storage throws', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage disabled')
    })
    // A storage failure must not propagate — the worst case is the hint shows
    // again, never a broken task-creation flow.
    expect(() => isTaskCreateHintDismissed()).not.toThrow()
    expect(isTaskCreateHintDismissed()).toBe(false)
    getItem.mockRestore()
  })

  it('does not throw when storage write fails', () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    expect(() => dismissTaskCreateHint()).not.toThrow()
    setItem.mockRestore()
  })
})
