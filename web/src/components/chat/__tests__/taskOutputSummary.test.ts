import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ContentBlocks from '@/components/chat/ContentBlocks.vue'
import i18n from '@/i18n'

/**
 * End-to-end guard for issue #682: the TaskOutput / TaskStop card strip must
 * describe the background task, not just echo its random id.
 *
 * The id alone is all the lookup call carries; the description lives on the
 * Bash block of the SAME message that launched the task. Same approach as
 * taskToolPillTitle.test.ts: no mock for `@/utils/icons` (the real title helper
 * must be consulted), and the summary function is a real one so a pass proves
 * the strip actually renders what the component derives.
 */
const LucideStub = { template: '<span />' }

const passThroughSummary = (b: { summary?: string }) => b.summary || ''

function launchBlock(taskId: string) {
  return {
    type: 'tool_use',
    name: 'Bash',
    id: `call_bash_${taskId}`,
    input: { description: '跑全量测试落盘并取汇总行', command: 'go test ./... 2>&1 | tee /tmp/t.log', run_in_background: true },
    output: `Status: Running in background with task_id: ${taskId}\nReason: Launched directly in background.`,
    done: true,
  }
}

function lookupBlock(taskId: string) {
  return { type: 'tool_use', name: 'TaskOutput', id: `call_out_${taskId}`, input: { task_id: taskId, block: true }, summary: taskId, done: true }
}

function mountWithBlocks(blocks: Record<string, unknown>[], extraProps: Record<string, unknown> = {}) {
  return mount(ContentBlocks, {
    props: {
      blocks,
      msgId: 'msg-bg-1',
      ...extraProps,
      renderTextBlock: (text: string) => `<p>${text}</p>`,
      formatToolInput: () => '',
      toolCallSummary: passThroughSummary,
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
        Users: LucideStub,
        CircleSlash: LucideStub,
        // Tool-call folding is ON by default and would collapse both cards into
        // one row; this guard is about a card's own strip.
        RollingText: LucideStub,
      },
    },
  })
}

describe('TaskOutput card strip — background task label (issue #682)', () => {
  it('shows the launcher description, with the id kept as secondary info', () => {
    const wrapper = mountWithBlocks([launchBlock('Ooj0bz'), lookupBlock('Ooj0bz')])
    // The plain tool pill carries no data-tool-name; find it by its title.
    const lookupCard = wrapper.findAll('.chat-tool-call').find((c) => c.find('.tool-name').text() === 'View background task output')
    expect(lookupCard).toBeTruthy()
    expect(lookupCard!.find('.tool-summary').text()).toBe('跑全量测试落盘并取汇总行 · Ooj0bz')
    // The launcher card is not relabelled: its summary is whatever the generic
    // summarizer derived (its own description), not the "<desc> · <id>" form.
    const launcherCard = wrapper.findAll('.chat-tool-call').find((c) => c.find('.tool-name').text() === 'Bash')
    expect(launcherCard!.find('.tool-summary').exists()).toBe(false)
    wrapper.unmount()
  })

  it('labels a slim (reloaded) lookup block from its summary', () => {
    // Historical rows carry no input on a plain pill; the id lives in `summary`.
    const slim = { type: 'tool_use', name: 'TaskOutput', id: 'call_out_slim', input: null, summary: 'Ooj0bz', done: true }
    const wrapper = mountWithBlocks([launchBlock('Ooj0bz'), slim])
    const card = wrapper.findAll('.chat-tool-call').find((c) => c.find('.tool-name').text() === 'View background task output')
    expect(card!.find('.tool-summary').text()).toBe('跑全量测试落盘并取汇总行 · Ooj0bz')
    wrapper.unmount()
  })

  it('finds the launcher in the message root from a nested (sub-agent) instance', () => {
    // A nested instance renders only the sub-agent's own blocks; the launcher of
    // the task it looked up sits at message level. Searching props.blocks (the
    // nested array) instead of the root would silently drop the label.
    const wrapper = mountWithBlocks([lookupBlock('Ooj0bz')], {
      nested: true,
      rootBlocks: [launchBlock('Ooj0bz'), lookupBlock('Ooj0bz')],
    })
    const card = wrapper.findAll('.chat-tool-call').find((c) => c.find('.tool-name').text() === 'View background task output')
    expect(card!.find('.tool-summary').text()).toBe('跑全量测试落盘并取汇总行 · Ooj0bz')
    wrapper.unmount()
  })

  it('keeps the bare id when the launcher is not in the message', () => {
    const wrapper = mountWithBlocks([lookupBlock('KYAfrK')])
    expect(wrapper.findAll('.tool-summary').map((s) => s.text())).toEqual(['KYAfrK'])
    wrapper.unmount()
  })
})
