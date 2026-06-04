import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ChatInputBar from '@/components/chat/ChatInputBar.vue'

// ── Mocks ────────────────────────────────────────────────────
const mockFetchItems = vi.fn()
const mockQuickSendItems = vi.fn(() => [])

vi.mock('@/composables/useQuickSend', () => ({
  useQuickSend: () => ({
    items: { value: [] },
    loaded: { value: true },
    showEditDialog: { value: false },
    fetchItems: mockFetchItems,
    addItem: vi.fn(),
    updateItem: vi.fn(),
    deleteItem: vi.fn(),
    reorderItems: vi.fn(),
  }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: vi.fn(),
  }),
}))

vi.mock('@/composables/useChatKeyboard', () => ({
  useChatKeyboard: () => ({
    activate: vi.fn(),
    debounceDeactivate: vi.fn(),
  }),
}))

vi.mock('@/utils/stopButtonMachine', () => ({
  createStopButtonMachine: () => ({
    click: () => ({ primed: false, confirmed: false }),
    reset: vi.fn(),
  }),
}))

// ── i18n ─────────────────────────────────────────────────────
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        actions: {
          session: '会话',
          attachment: '附件',
          autoSpeech: '自动朗读',
          switchModel: '切换模型',
          switchThinkingEffort: '切换思考强度',
          deleteCurrentSession: '删除当前会话',
          noSessionToDelete: '无可删除会话',
        },
        create: { selectAgentOrLongPress: '选择Agent' },
        delete: { confirm: '确认删除？' },
        input: {
          clearInput: '清除输入',
          placeholder: '输入消息…',
          placeholderQueue: '排队消息…',
          placeholderOptional: '添加描述（可选）',
          placeholderQuickSend: '点击⚡选指令 →',
          send: '发送',
          enqueue: '排队',
          quickMenu: '快捷指令',
          stopGenerating: '停止生成',
          confirmStop: '确认停止',
        },
        attach: {
          dropToUpload: '拖放上传',
          currentFile: '当前文件',
          currentDir: '当前目录',
          recentReferences: '最近引用',
          uploadFile: '上传文件',
          openFile: '打开文件',
        },
        quickSend: { title: '快捷发送', tapToFill: '长按发送', edit: '管理' },
        modelSwitcher: { title: '切换模型' },
        thinkingEffortSwitcher: { title: '思考强度', auto: '自动' },
      },
      common: { remove: '移除' },
    },
  },
})

beforeEach(() => {
  mockFetchItems.mockReset()
})

function mountInputBar(props = {}) {
  return mount(ChatInputBar, {
    props: {
      loading: false,
      currentFile: null,
      currentDir: null,
      pendingFiles: [],
      attachedFiles: [],
      messages: [],
      autoSpeechEnabled: false,
      currentSessionId: 'test-session-id',
      chatUnread: false,
      chatRunning: false,
      currentModelId: 'model-1',
      currentModelName: 'Test Model',
      currentThinkingEffort: '',
      thinkingEffortLevels: [],
      agentModels: [],
      isMultiModel: () => false,
      currentAgentId: 'agent-1',
      active: true,
      ...props,
    },
    global: {
      stubs: {
        teleport: true,
        PopupMenu: true,
        QuickSendDialog: true,
      },
      plugins: [i18n],
    },
  })
}

// ── Tests ─────────────────────────────────────────────────────

describe('ChatInputBar — clear button visibility', () => {
  it('hides clear button when input is empty', () => {
    const wrapper = mountInputBar()
    expect(wrapper.find('.chat-clear-btn').exists()).toBe(false)
  })

  it('shows clear button when input has text and not loading', async () => {
    const wrapper = mountInputBar()
    const textarea = wrapper.find('.chat-textarea')
    await textarea.setValue('hello world')
    await nextTick()

    expect(wrapper.find('.chat-clear-btn').exists()).toBe(true)
  })

  it('shows clear button when input has text even when loading (queue mode)', async () => {
    // This is the key fix: clear button should be visible during loading
    // so users can clear queued input text
    const wrapper = mountInputBar({ loading: true })
    const textarea = wrapper.find('.chat-textarea')
    await textarea.setValue('queued message')
    await nextTick()

    expect(wrapper.find('.chat-clear-btn').exists()).toBe(true)
  })

  it('clears input text when clear button is clicked', async () => {
    const wrapper = mountInputBar()
    const textarea = wrapper.find('.chat-textarea')
    await textarea.setValue('some text')
    await nextTick()

    expect(wrapper.find('.chat-clear-btn').exists()).toBe(true)
    await wrapper.find('.chat-clear-btn').trigger('click')
    await nextTick()

    expect(wrapper.find('.chat-textarea').element.value).toBe('')
    expect(wrapper.find('.chat-clear-btn').exists()).toBe(false)
  })

  it('clears input text in loading mode when clear button is clicked', async () => {
    const wrapper = mountInputBar({ loading: true })
    const textarea = wrapper.find('.chat-textarea')
    await textarea.setValue('queued text')
    await nextTick()

    expect(wrapper.find('.chat-clear-btn').exists()).toBe(true)
    await wrapper.find('.chat-clear-btn').trigger('click')
    await nextTick()

    expect(wrapper.find('.chat-textarea').element.value).toBe('')
  })
})

describe('ChatInputBar — input layout', () => {
  it('renders attach button and textarea in input row', () => {
    const wrapper = mountInputBar()
    expect(wrapper.find('.chat-input-row').exists()).toBe(true)
    expect(wrapper.find('.chat-attach-btn').exists()).toBe(true)
    expect(wrapper.find('.chat-textarea').exists()).toBe(true)
  })

  it('shows send button in normal mode', () => {
    const wrapper = mountInputBar()
    const sendBtn = wrapper.find('.chat-send-btn')
    expect(sendBtn.exists()).toBe(true)
    expect(sendBtn.classes()).not.toContain('queued')
  })

  it('shows queue button (orange) when loading', () => {
    const wrapper = mountInputBar({ loading: true })
    const sendBtn = wrapper.find('.chat-send-btn')
    expect(sendBtn.exists()).toBe(true)
    expect(sendBtn.classes()).toContain('queued')
  })

  it('shows shortcut style (green Zap) when input is empty', () => {
    const wrapper = mountInputBar()
    const sendBtn = wrapper.find('.chat-send-btn')
    expect(sendBtn.exists()).toBe(true)
    expect(sendBtn.classes()).toContain('shortcut')
    expect(wrapper.findComponent({ name: 'Zap' }).exists() || sendBtn.find('svg').exists()).toBe(true)
  })

  it('removes shortcut style when input has content', async () => {
    const wrapper = mountInputBar()
    await wrapper.find('.chat-textarea').setValue('hello')
    await nextTick()
    const sendBtn = wrapper.find('.chat-send-btn')
    expect(sendBtn.classes()).not.toContain('shortcut')
  })

  it('shows shortcut style in queue mode when input is empty', () => {
    const wrapper = mountInputBar({ loading: true })
    const sendBtn = wrapper.find('.chat-send-btn')
    expect(sendBtn.classes()).toContain('queued')
    expect(sendBtn.classes()).toContain('shortcut')
  })

  it('shows stop button when loading', () => {
    const wrapper = mountInputBar({ loading: true })
    expect(wrapper.find('.chat-stop-btn').exists()).toBe(true)
  })

  it('hides stop button when not loading', () => {
    const wrapper = mountInputBar()
    expect(wrapper.find('.chat-stop-btn').exists()).toBe(false)
  })
})

describe('ChatInputBar — send/queue behavior', () => {
  it('emits send with trimmed text on send button click', async () => {
    const wrapper = mountInputBar()
    await wrapper.find('.chat-textarea').setValue('  hello  ')
    await nextTick()

    await wrapper.find('.chat-send-btn').trigger('click')

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')[0]).toEqual(['hello'])
  })

  it('emits send with trimmed text on Enter key', async () => {
    const wrapper = mountInputBar()
    const textarea = wrapper.find('.chat-textarea')
    await textarea.setValue('test message')
    await nextTick()

    await textarea.trigger('keydown.enter.exact')

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')[0]).toEqual(['test message'])
  })
})

describe('ChatInputBar — clearInput exposed method', () => {
  it('clears input via exposed clearInput method', async () => {
    const wrapper = mountInputBar()
    await wrapper.find('.chat-textarea').setValue('text to clear')
    await nextTick()

    wrapper.vm.clearInput()
    await nextTick()

    expect(wrapper.find('.chat-textarea').element.value).toBe('')
  })
})

describe('ChatInputBar — quick-send inject to input', () => {
  it('injects text to input via injectToInput', async () => {
    const wrapper = mountInputBar()
    wrapper.vm.injectToInput('git status')
    await nextTick()

    expect(wrapper.find('.chat-textarea').element.value).toBe('git status')
  })

  it('appends text with newline when input already has content', async () => {
    const wrapper = mountInputBar()
    await wrapper.find('.chat-textarea').setValue('hello')
    await nextTick()

    wrapper.vm.injectToInput('git status')
    await nextTick()

    expect(wrapper.find('.chat-textarea').element.value).toBe('hello\ngit status')
  })

  it('replaces input when existing content is only whitespace', async () => {
    const wrapper = mountInputBar()
    await wrapper.find('.chat-textarea').setValue('   ')
    await nextTick()

    wrapper.vm.injectToInput('git status')
    await nextTick()

    // trim() makes the existing content empty, so no newline prefix
    expect(wrapper.find('.chat-textarea').element.value).toBe('git status')
  })
})
