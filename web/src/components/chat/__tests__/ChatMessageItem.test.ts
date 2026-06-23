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

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        message: {
          expandFull: '展开',
          collapse: '收起',
        },
      },
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
          getSummary: vi.fn(() => null),
          getPhaseLabel: vi.fn(() => ''),
        },
        chatRender: {},
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
})
