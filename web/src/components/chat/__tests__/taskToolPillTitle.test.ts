import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ContentBlocks from '@/components/chat/ContentBlocks.vue'
import i18n from '@/i18n'

/**
 * End-to-end guard for issue #645: the task-tool card STRIP must show the
 * readable action title.
 *
 * `ContentBlocks.test.ts` mocks `@/utils/icons` with an identity
 * `toolDisplayName`, so that file can only prove the template renders whatever
 * the helper returns — never that the helper is actually consulted with the
 * task-tool map. This spec deliberately does NOT mock `@/utils/icons`, so it
 * fails if the strip ever goes back to rendering the raw wire name
 * ("TaskOutput"), which is precisely the regression the drawer had.
 */
const LucideStub = { template: '<span />' }

function mountWithTool(block: Record<string, unknown>, summary = '') {
  return mount(ContentBlocks, {
    props: {
      blocks: [block],
      msgId: 'msg-task-1',
      renderTextBlock: (text: string) => `<p>${text}</p>`,
      formatToolInput: () => '',
      toolCallSummary: () => summary,
    },
    global: {
      plugins: [i18n],
      stubs: {
        Brain: LucideStub,
        ChevronRight: LucideStub,
        ChevronDown: LucideStub,
        ChevronUp: LucideStub,
        AlertCircle: LucideStub,
        AlertTriangle: LucideStub,
        XCircle: LucideStub,
        CheckCircle2: LucideStub,
        Clock: LucideStub,
        Archive: LucideStub,
        LoadingIndicator: LucideStub,
        TaskChatCard: LucideStub,
      },
    },
  })
}

describe('task-tool card strip title', () => {
  const original = i18n.global.locale.value
  function withLocale(locale: 'en' | 'zh', fn: () => void) {
    i18n.global.locale.value = locale
    try {
      fn()
    } finally {
      i18n.global.locale.value = original
    }
  }

  it('renders the action, not the raw tool name, for TaskOutput', () => {
    withLocale('en', () => {
      const wrapper = mountWithTool(
        { type: 'tool_use', name: 'TaskOutput', input: { task_id: 'cyqjeL' }, done: true },
        'cyqjeL',
      )
      const pill = wrapper.find('.chat-tool-call')
      expect(pill.exists()).toBe(true)
      expect(pill.find('.tool-name').text()).toBe('View background task output')
      // The id stays as the SECONDARY summary, which is what the issue asked for.
      expect(pill.find('.tool-summary').text()).toBe('cyqjeL')
      wrapper.unmount()
    })
  })

  it('renders the action for TaskStop and TaskUpdate', () => {
    withLocale('zh', () => {
      const stop = mountWithTool({ type: 'tool_use', name: 'TaskStop', input: { task_id: 'cyqjeL' }, done: true }, 'cyqjeL')
      expect(stop.find('.tool-name').text()).toBe('停止后台任务')
      stop.unmount()

      const update = mountWithTool(
        { type: 'tool_use', name: 'TaskUpdate', input: { taskId: '3', status: 'in_progress' }, done: true },
        '#3 · in_progress',
      )
      expect(update.find('.tool-name').text()).toBe('更新任务')
      expect(update.find('.tool-summary').text()).toBe('#3 · in_progress')
      update.unmount()
    })
  })

  it('leaves an ordinary tool pill on its own name', () => {
    withLocale('en', () => {
      const wrapper = mountWithTool({ type: 'tool_use', name: 'Bash', input: { command: 'ls' }, done: true }, 'ls')
      expect(wrapper.find('.tool-name').text()).toBe('Bash')
      wrapper.unmount()
    })
  })
})
