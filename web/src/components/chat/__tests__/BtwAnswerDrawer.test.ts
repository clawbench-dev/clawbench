import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, ref } from 'vue'
import BtwAnswerDrawer from '../BtwAnswerDrawer.vue'

vi.mock('lucide-vue-next', () => ({
  Sparkles: { name: 'SparklesIcon', render: () => null },
}))

// BottomSheet teleports to <body>; stub it as a passthrough so the drawer's
// content is reachable in the wrapper.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
    template: '<div class="bs-mock" :data-open="String(open)"><slot name="header" /><slot /></div>',
  },
}))

// Capture the props ChatMessageItem receives, so we can assert the synthetic
// message and the render maps are forwarded exactly as the chat list does.
const captured = vi.hoisted(() => ({ props: [] as any[] }))
vi.mock('../ChatMessageItem.vue', () => ({
  default: defineComponent({
    name: 'ChatMessageItem',
    props: ['msg', 'index', 'expandedTools', 'blockTasks', 'blockAskQuestions', 'agents', 'staticBlockCache', 'active', 'isLastAssistant', 'isLastMessage', 'hideSessionActions', 'readOnly'],
    setup(props) {
      captured.props.push(props)
      return () => h('div', { class: 'cmi-mock' }, props.msg?.blocks?.[0]?.text || '')
    },
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        btw: { title: 'By the way', answering: 'Answering…', failed: 'Failed', close: 'Close' },
      },
    },
  },
})

function mountDrawer(props = {}) {
  return mount(BtwAnswerDrawer, {
    props: {
      question: '为什么并发一高就慢',
      answer: '# 因为连接池只有 2 个连接。',
      answerId: 1,
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
  it('renders the answer through ChatMessageItem', () => {
    const wrapper = mountDrawer()
    const item = wrapper.find('.cmi-mock')
    expect(item.exists()).toBe(true)
    expect(item.text()).toContain('因为连接池只有 2 个连接。')
  })

  it('builds a synthetic assistant message with streaming disabled', () => {
    mountDrawer()
    const last = captured.props[captured.props.length - 1]
    // streaming:false is what selects the full (non-streaming) render branch —
    // the same branch a settled chat message takes.
    expect(last.msg.role).toBe('assistant')
    expect(last.msg.streaming).toBe(false)
    expect(last.msg.cancelled).toBe(false)
    expect(last.msg.blocks).toEqual([{ type: 'text', text: '# 因为连接池只有 2 个连接。' }])
  })

  it('forwards the host render maps so the pipeline is shared, not re-created', () => {
    // Vue reactive-proxies props, so identity cannot be compared. Non-default
    // values prove the drawer passes the host's maps through instead of falling
    // back to its own empty defaults (which would give a separate render chain
    // and diverge from the chat list).
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

  it('renders the message read-only with session actions hidden', () => {
    mountDrawer()
    const last = captured.props[captured.props.length - 1]
    // /btw has no live session context to fork/rewind, and the answer is not a
    // real message — the per-message session actions must be suppressed.
    expect(last.readOnly).toBe(true)
    expect(last.hideSessionActions).toBe(true)
  })

  it('shows the question above the answer', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.btw-question').text()).toContain('为什么并发一高就慢')
  })

  it('renders no message when the answer is empty', () => {
    const wrapper = mountDrawer({ answer: '' })
    expect(wrapper.find('.cmi-mock').exists()).toBe(false)
  })

  it('re-ids the message per answer so a repeat question remounts', () => {
    mountDrawer({ answerId: 7 })
    const last = captured.props[captured.props.length - 1]
    expect(last.msg.id).toBe('btw-7')
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
