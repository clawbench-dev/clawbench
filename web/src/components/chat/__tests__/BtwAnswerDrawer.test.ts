import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'
import BtwAnswerDrawer from '../BtwAnswerDrawer.vue'

vi.mock('lucide-vue-next', () => ({
  MessageCircleQuestion: { name: 'MessageCircleQuestionIcon', render: () => null },
}))

// BottomSheet teleports to <body>; stub it as a passthrough.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
    template: '<div class="bs-mock" :data-open="String(open)"><slot name="header" /><slot /></div>',
  },
}))

// Capture the props each ChatMessageItem receives so we can assert both the
// synthetic question (user) and answer (assistant) messages.
const captured = vi.hoisted(() => ({ props: [] as any[] }))
vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg', 'index', 'expandedTools', 'blockTasks', 'blockAskQuestions', 'agents', 'staticBlockCache', 'active', 'isLastAssistant', 'isLastMessage', 'hideSessionActions', 'readOnly'],
    setup(props) {
      captured.props.push(props)
      return () => h('div', { class: `cmi-mock cmi-${props.msg?.role}` }, props.msg?.blocks?.[0]?.text || '')
    },
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        btw: {
          title: 'By the way',
          answering: 'Answering…',
          failed: 'Failed',
          failedWithReason: 'Failed to answer: {reason}',
          anchorLabel: 'Asked by the way',
          anchorTitle: 'View the side question asked here',
          close: 'Close',
        },
      },
    },
  },
})

function mountDrawer(props = {}) {
  return mount(BtwAnswerDrawer, {
    props: {
      records: [{ id: 1, question: '为什么并发一高就慢', answer: '# 因为连接池只有 2 个连接。', error: '', createdAt: '2026-09-29T00:00:00Z' }],
      expandedTools: {},
      blockTasks: {},
      blockAskQuestions: {},
      staticBlockCache: {},
      ...props,
    },
    global: { plugins: [i18n] },
  })
}

describe('BtwAnswerDrawer', () => {
  it('renders the question as a user bubble and the answer as an assistant bubble', () => {
    const wrapper = mountDrawer()
    // Both go through ChatMessageItem, so the chat-area styling applies.
    expect(wrapper.find('.cmi-user').exists()).toBe(true)
    expect(wrapper.find('.cmi-assistant').exists()).toBe(true)
    expect(wrapper.find('.cmi-user').text()).toContain('为什么并发一高就慢')
    expect(wrapper.find('.cmi-assistant').text()).toContain('因为连接池只有 2 个连接。')
  })

  it('builds the question as a user message and the answer as a settled assistant message', () => {
    captured.props.length = 0
    mountDrawer()
    const [q, a] = captured.props
    expect(q.msg.role).toBe('user')
    expect(q.msg.blocks).toEqual([{ type: 'text', text: '为什么并发一高就慢' }])
    expect(a.msg.role).toBe('assistant')
    // streaming:false selects the full non-streaming render branch, same as a
    // settled chat message.
    expect(a.msg.streaming).toBe(false)
    expect(a.msg.blocks).toEqual([{ type: 'text', text: '# 因为连接池只有 2 个连接。' }])
  })

  it('renders one exchange per record, in order', () => {
    const wrapper = mountDrawer({
      records: [
        { id: 1, question: 'Q1', answer: 'A1', error: '' },
        { id: 2, question: 'Q2', answer: 'A2', error: '' },
      ],
    })
    const exchanges = wrapper.findAll('.btw-exchange')
    expect(exchanges).toHaveLength(2)
    expect(exchanges[0].text()).toContain('Q1')
    expect(exchanges[1].text()).toContain('Q2')
  })

  it('shows the failure reason in the assistant bubble when the answer is missing', () => {
    captured.props.length = 0
    const wrapper = mountDrawer({
      records: [{ id: 3, question: '会失败吗', answer: '', error: 'upstream 401' }],
    })
    const a = captured.props.find(p => p.msg?.role === 'assistant')
    expect(a.msg.blocks[0].text).toContain('upstream 401')
    expect(wrapper.find('.cmi-user').text()).toContain('会失败吗')
  })

  it('forwards the host render maps so the pipeline is shared, not re-created', () => {
    captured.props.length = 0
    mountDrawer({
      expandedTools: { toolA: true },
      blockTasks: { taskA: 1 },
      blockAskQuestions: { askA: 1 },
      staticBlockCache: { cacheA: 1 },
    })
    const last = captured.props[captured.props.length - 1]
    expect(last.expandedTools).toEqual({ toolA: true })
    expect(last.blockTasks).toEqual({ taskA: 1 })
    expect(last.blockAskQuestions).toEqual({ askA: 1 })
    expect(last.staticBlockCache).toEqual({ cacheA: 1 })
  })

  it('renders read-only with session actions hidden', () => {
    captured.props.length = 0
    mountDrawer()
    for (const p of captured.props) {
      expect(p.readOnly).toBe(true)
      expect(p.hideSessionActions).toBe(true)
    }
  })

  it('keeps vertical breathing room so bubbles do not touch the edges', async () => {
    // The body has no padding of its own, so the content wrapper must supply
    // top AND bottom padding; a zero top made the first bubble sit flush
    // against the header line.
    const raw = await import('../BtwAnswerDrawer.vue?raw')
    const src = typeof raw.default === 'string' ? raw.default : ''
    const block = src.slice(src.indexOf('.btw-content {'), src.indexOf('.btw-exchange'))
    const m = block.match(/padding:\s*([^;]+);/)
    expect(m, '.btw-content must declare padding').toBeTruthy()
    // Shorthand is "top <horizontal> bottom".
    const [top, , bottom] = m![1].trim().split(/\s+/)
    expect(top, 'top padding must be non-zero').toMatch(/var\(--space-/)
    expect(bottom, 'bottom padding must be non-zero').toMatch(/var\(--space-/)
  })

  it('renders nothing when there are no records', () => {
    const wrapper = mountDrawer({ records: [] })
    expect(wrapper.find('.cmi-mock').exists()).toBe(false)
  })

  it('exposes open/close bound to the tab drawer', async () => {
    const wrapper = mountDrawer()
    expect(wrapper.vm.isOpen).toBe(false)
    wrapper.vm.open()
    await wrapper.vm.$nextTick()
    expect(wrapper.vm.isOpen).toBe(true)
    wrapper.vm.close()
    await wrapper.vm.$nextTick()
    expect(wrapper.vm.isOpen).toBe(false)
  })
})
