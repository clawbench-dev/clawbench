import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'

// Mocks for composables and stores used by ChatMessageItem
vi.mock('@/composables/useDoubleClickCopy', () => ({
  useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }),
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ openFilePath: vi.fn() }),
  openFilePath: vi.fn(),
}))

vi.mock('@/composables/useLocalhostAnnotation', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn() }),
}))

vi.mock('@/composables/useAutoSpeech', () => ({
  extractSpeakableText: (blocks: any[]) => {
    if (!blocks || blocks.length === 0) return ''
    return 'test text'
  },
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn() }),
}))

vi.mock('@/utils/chatStreamUtils', () => ({
  extractFileChanges: (blocks: any[]) => {
    const created: string[] = []
    const modified: string[] = []
    for (const block of blocks || []) {
      if (block.type !== 'tool_use' || !block.done) continue
      const filePath = block.file_path || block.input?.file_path
      if (!filePath) continue
      if (block.name === 'Write') created.push(filePath)
      else if (block.name === 'Edit') modified.push(filePath)
    }
    return { created, modified }
  },
}))

vi.mock('@/utils/format', () => ({
  formatDuration: (ms: number) => `${ms}ms`,
}))

vi.mock('@/utils/clipboard', () => ({
  copyText: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/home/user/project' } },
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({ effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn(), toggle: vi.fn() }),
}))

// Mock child components that have complex props/dependencies
vi.mock('@/components/chat/ContentBlocks.vue', () => ({
  default: { name: 'ContentBlocks', template: '<div class="content-blocks-stub" />' },
}))
vi.mock('@/components/chat/FileAttachmentList.vue', () => ({
  default: { name: 'FileAttachmentList', template: '<div class="file-attachment-list-stub" />' },
}))
vi.mock('@/components/common/SummaryToggle.vue', () => ({
  default: { name: 'SummaryToggle', template: '<span class="summary-toggle-stub" />' },
}))
vi.mock('@/components/chat/FileChangesDrawer.vue', () => ({
  default: { name: 'FileChangesDrawer', template: '<div class="file-changes-drawer-stub" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        message: {
          expandFull: '展开',
          collapse: '收起',
          copy: '复制',
          readAloud: '朗读',
          speaking: '正在朗读',
          viewDetails: '详情',
        },
        contentBlocks: { cancelled: '已中断' },
        pending: { queuing: '排队中' },
        fileChanges: { title: '文件变更' },
        speech: { summarizing: '总结中' },
      },
      common: { remove: '移除', copy: '复制' },
    },
  },
})

function createWrapper(props = {}) {
  return mount(ChatMessageItem, {
    global: {
      plugins: [i18n],
      provide: {
        autoSpeech: {
          isActive: vi.fn(() => false),
          isGeneratingText: vi.fn(() => false),
          isPlayingAudio: vi.fn(() => false),
          playAudio: vi.fn(),
          stopAudio: vi.fn(),
          speakText: vi.fn(),
          getSummary: vi.fn(() => null),
          getPhaseLabel: vi.fn(() => ''),
        },
        chatRender: {
          renderTextBlock: vi.fn(),
          toolCallSummary: vi.fn(),
          formatToolInput: vi.fn(),
          humanizeCron: vi.fn(),
          repeatLabel: vi.fn(),
          truncate: vi.fn(),
          hasImagesInContent: vi.fn(() => false),
        },
        chatSession: {
          getAgentBackend: vi.fn(() => ''),
          getAgentName: vi.fn(() => ''),
        },
      },
    },
    props: {
      msg: { id: '1', role: 'user', content: 'hello', blocks: [] },
      index: 0,
      active: true,
      ...props,
    },
  })
}

describe('ChatMessageItem', () => {
  it('renders user message with wrapper', () => {
    const wrapper = createWrapper()
    expect(wrapper.find('.msg-content-wrapper').exists()).toBe(true)
    expect(wrapper.find('.chat-message').classes()).toContain('user')
  })

  it('renders assistant message', () => {
    const wrapper = createWrapper({
      msg: { id: '2', role: 'assistant', content: 'response', blocks: [] },
    })
    expect(wrapper.find('.chat-message').classes()).toContain('assistant')
  })

  it('shows pending hint for pending messages', () => {
    const wrapper = createWrapper({
      msg: { id: '3', role: 'user', content: 'hello', blocks: [], pending: true },
    })
    expect(wrapper.find('.pending-hint').exists()).toBe(true)
  })

  it('does not show pending hint for non-pending messages', () => {
    const wrapper = createWrapper()
    expect(wrapper.find('.pending-hint').exists()).toBe(false)
  })

  it('applies pending class when message is pending', () => {
    const wrapper = createWrapper({
      msg: { id: '4', role: 'user', content: 'hello', blocks: [], pending: true },
    })
    expect(wrapper.find('.chat-message').classes()).toContain('pending')
  })

  it('renders meta bar for non-streaming assistant message with content', () => {
    const wrapper = createWrapper({
      msg: { id: '5', role: 'assistant', content: 'response', blocks: [{ type: 'text', text: 'Hello world' }] },
    })
    expect(wrapper.find('.chat-meta-bar').exists()).toBe(true)
  })

  it('does not render meta bar for streaming assistant message', () => {
    const wrapper = createWrapper({
      msg: { id: '6', role: 'assistant', content: '...', blocks: [{ type: 'text', text: '...' }], streaming: true },
    })
    expect(wrapper.find('.chat-meta-bar').exists()).toBe(false)
  })

  it('renders meta bar and summary toggle for summarized assistant message with empty blocks', () => {
    const wrapper = createWrapper({
      msg: { id: 's1', role: 'assistant', content: '', blocks: [], summary: 'Short summary', showingSummary: true, streaming: false },
    })
    expect(wrapper.find('.chat-meta-bar').exists()).toBe(true)
    expect(wrapper.find('.summary-toggle-stub').exists()).toBe(true)
  })

  it('applies has-metadata class when assistant message has metadata', () => {
    const wrapper = createWrapper({
      msg: { id: '7', role: 'assistant', content: 'response', blocks: [], metadata: { wallMs: 100 } },
    })
    expect(wrapper.find('.chat-message').classes()).toContain('has-metadata')
  })

  it('emits remove-pending when pending remove button is clicked', async () => {
    const wrapper = createWrapper({
      msg: { id: '8', role: 'user', content: 'hello', blocks: [], pending: true },
    })
    const btn = wrapper.find('.pending-remove')
    await btn.trigger('click')
    expect(wrapper.emitted('remove-pending')).toBeTruthy()
  })

  it('renders data-msg-key attribute with msg id', () => {
    const wrapper = createWrapper({
      msg: { id: 'test-id-42', role: 'user', content: 'hello', blocks: [] },
    })
    expect(wrapper.find('.chat-message').attributes('data-msg-key')).toBe('db-test-id-42')
  })

  describe('cancelled marker', () => {
    it('shows cancelled mark when cancelled and last block is not thinking', () => {
      const wrapper = createWrapper({
        msg: { id: 'c1', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Hello' }], cancelled: true },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(true)
    })

    it('hides cancelled mark when last block is thinking', () => {
      const wrapper = createWrapper({
        msg: { id: 'c2', role: 'assistant', content: '', blocks: [{ type: 'thinking', text: 'Thought', done: true }], cancelled: true },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(false)
    })

    it('hides cancelled mark when not cancelled', () => {
      const wrapper = createWrapper({
        msg: { id: 'c3', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Hello' }], cancelled: false },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(false)
    })

    it('renders cancelled mark after file changes banner', () => {
      const wrapper = createWrapper({
        msg: { id: 'c4', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/foo.ts' }, { type: 'text', text: 'Done' }], cancelled: true, streaming: false },
      })
      const banner = wrapper.find('.chat-file-changes-banner')
      const mark = wrapper.find('.chat-cancelled-mark')
      // Both should exist
      expect(banner.exists()).toBe(true)
      expect(mark.exists()).toBe(true)
      // Cancelled mark should come after banner in DOM order
      const allElements = wrapper.findAll('.chat-file-changes-banner, .chat-cancelled-mark')
      expect(allElements[0].classes()).toContain('chat-file-changes-banner')
      expect(allElements[1].classes()).toContain('chat-cancelled-mark')
    })
  })
})
