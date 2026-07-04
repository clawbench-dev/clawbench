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
  extractSpeakableText: () => 'test text',
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn() }),
}))

vi.mock('@/utils/chatStreamUtils', () => ({
  extractFileChanges: () => ({ created: [], modified: [] }),
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
  useTabDrawer: () => ({ effectiveOpen: { value: false } }),
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
          getAgentIcon: vi.fn(() => ''),
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
})
