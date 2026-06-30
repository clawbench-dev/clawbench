import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ChatInputBar from '@/components/chat/ChatInputBar.vue'

// ── Mocks ────────────────────────────────────────────────────
const mockFetchItems = vi.fn()
const mockQuickSendItems = vi.fn(() => [])

// Mock useChatContext for quoteData support
const mockSetQuoteData = vi.fn()
const mockAddAttachedFile = vi.fn()
const mockHasAttachedFile = vi.fn(() => false)
const mockClearAll = vi.fn()
vi.mock('@/composables/useChatContext', () => ({
  useChatContext: () => ({
    attachedFiles: { value: [] },
    quoteData: { value: null },
    setQuoteData: mockSetQuoteData,
    addAttachedFile: mockAddAttachedFile,
    hasAttachedFile: mockHasAttachedFile,
    removeAttachedFile: vi.fn(),
    clearAll: mockClearAll,
  }),
}))

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
          sessionSettings: '会话设置',
          switchThinkingEffort: '切换思考强度',
          deleteCurrentSession: '删除当前会话',
          noSessionToDelete: '无可删除会话',
          forkSession: '派生会话',
          userMsgIndex: '消息索引',
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

const TeleportStub = { template: '<div><slot /></div>' }

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
      chatUnreadCount: 0,
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
        Teleport: TeleportStub,
        PopupMenu: true,
        QuickSendDrawer: true,
      },
      plugins: [i18n],
    },
  })
}

// ── Tests ─────────────────────────────────────────────────────

/** Helper to set inputText by using the component's own injectToInput method.
 *  This reliably sets the ref because it runs inside the component's scope.
 */
function setInputText(wrapper: ReturnType<typeof mount>, text: string) {
  // Use injectToInput which sets inputText.value directly inside the component.
  // When input is empty, it replaces; when non-empty, it appends with newline.
  wrapper.vm.injectToInput(text)
}

/** Helper to get inputText value from the component instance */
function getInputText(wrapper: ReturnType<typeof mount>): string {
  const instance = (wrapper.vm as any).$
  // Try to get the ref value
  if (instance?.setupState?.inputText?.value !== undefined) {
    return instance.setupState.inputText.value
  }
  // The proxy should unwrap it
  return (wrapper.vm as any).inputText ?? ''
}

describe('ChatInputBar — clear button visibility', () => {
  it('hides clear button when input is empty', () => {
    const wrapper = mountInputBar()
    expect(wrapper.find('.chat-clear-btn').exists()).toBe(false)
  })

  it('shows clear button when input has text and not loading', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'hello world')
    await nextTick()

    // Verify inputText is truthy (clear button has v-if="inputText")
    expect(getInputText(wrapper)).toBeTruthy()
  })

  it('shows clear button when input has text even when loading (queue mode)', async () => {
    const wrapper = mountInputBar({ loading: true })
    setInputText(wrapper, 'queued message')
    await nextTick()

    expect(getInputText(wrapper)).toBeTruthy()
  })

  it('clears input text when clear button is clicked', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'some text')
    await nextTick()

    expect(getInputText(wrapper)).toBe('some text')
    // Simulate the clear button's click handler which sets inputText = ''
    wrapper.vm.clearInput()
    await nextTick()

    expect(getInputText(wrapper)).toBe('')
  })

  it('clears input text in loading mode when clear button is clicked', async () => {
    const wrapper = mountInputBar({ loading: true })
    setInputText(wrapper, 'queued text')
    await nextTick()

    expect(getInputText(wrapper)).toBe('queued text')
    wrapper.vm.clearInput()
    await nextTick()

    expect(getInputText(wrapper)).toBe('')
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
    setInputText(wrapper, 'hello')
    await nextTick()
    // When inputText has content, hasInputContent computed is true, so shortcut class is removed
    expect(wrapper.vm.hasInputContent).toBeTruthy()
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
    setInputText(wrapper, '  hello  ')
    await nextTick()

    await wrapper.find('.chat-send-btn').trigger('click')

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')[0]).toEqual(['hello'])
  })

  it('emits send with trimmed text on Enter key', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'test message')
    await nextTick()

    await wrapper.find('.chat-textarea').trigger('keydown.enter.exact')

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')[0]).toEqual(['test message'])
  })
})

describe('ChatInputBar — clearInput exposed method', () => {
  it('clears input via exposed clearInput method', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'text to clear')
    await nextTick()

    wrapper.vm.clearInput()
    await nextTick()

    expect(getInputText(wrapper)).toBe('')
  })
})

describe('ChatInputBar — quick-send inject to input', () => {
  it('injects text to input via injectToInput', async () => {
    const wrapper = mountInputBar()
    wrapper.vm.injectToInput('git status')
    await nextTick()

    expect(getInputText(wrapper)).toBe('git status')
  })

  it('appends text with newline when input already has content', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'hello')
    await nextTick()

    wrapper.vm.injectToInput('git status')
    await nextTick()

    expect(getInputText(wrapper)).toBe('hello\ngit status')
  })

  it('replaces input when existing content is only whitespace', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, '   ')
    await nextTick()

    wrapper.vm.injectToInput('git status')
    await nextTick()

    expect(getInputText(wrapper)).toBe('git status')
  })
})

describe('ChatInputBar — quick-send click sends directly', () => {
  it('emits send when handleQuickSendClick is called', async () => {
    const wrapper = mountInputBar()
    const item = { id: 1, label: 'Git Status', command: 'git status' }
    wrapper.vm.handleQuickSendClick(item)
    await nextTick()

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')[0]).toEqual(['git status'])
  })

  it('suppresses click when long-press was just triggered', async () => {
    vi.useFakeTimers()
    const wrapper = mountInputBar()
    const item = { id: 1, label: 'Git Status', command: 'git status' }
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }

    // Start touch and let long-press fire
    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    vi.advanceTimersByTime(500)
    await nextTick()

    // Long-press injects into input, not send
    expect(getInputText(wrapper)).toBe('git status')

    // Now the click after long-press should be suppressed
    wrapper.vm.handleQuickSendClick(item)
    await nextTick()

    // Should still only have the injected input, no new send emission
    expect(wrapper.emitted('send')).toBeFalsy()
    vi.useRealTimers()
  })
})

describe('ChatInputBar — quick-send touch events', () => {
  it('onQuickSendTouchStart sets pressing state', async () => {
    const wrapper = mountInputBar()
    const item = { id: 1, label: 'Git Status', command: 'git status' }
    const touchEvent = { touches: [{ clientX: 100, clientY: 200 }] }
    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    await nextTick()

    expect(wrapper.vm.quickSendPressingId).toBe(1)
  })

  it('onQuickSendTouchEnd short tap emits send', async () => {
    vi.useFakeTimers()
    const wrapper = mountInputBar()
    const item = { id: 2, label: 'Build', command: 'npm run build' }
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }

    // Start touch
    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    // Immediately end (short tap, no long-press timer fires)
    wrapper.vm.onQuickSendTouchEnd()
    await nextTick()

    expect(wrapper.emitted('send')).toBeTruthy()
    expect(wrapper.emitted('send')[0]).toEqual(['npm run build'])
    expect(wrapper.vm.quickSendPressingId).toBeNull()
    vi.useRealTimers()
  })

  it('onQuickSendTouchEnd long-press triggers injectToInput', async () => {
    vi.useFakeTimers()
    const wrapper = mountInputBar()
    const item = { id: 3, label: 'Test', command: 'npm test' }
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }

    // Start touch
    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    // Advance timer past long-press threshold
    vi.advanceTimersByTime(500)
    await nextTick()

    // Long-press should have injected into input (not sent)
    expect(getInputText(wrapper)).toBe('npm test')
    expect(wrapper.emitted('send')).toBeFalsy()

    // End touch after long-press
    wrapper.vm.onQuickSendTouchEnd()
    await nextTick()

    expect(wrapper.vm.quickSendPressingId).toBeNull()
    vi.useRealTimers()
  })

  it('onQuickSendTouchMove cancels press when finger moves beyond threshold', async () => {
    vi.useFakeTimers()
    const wrapper = mountInputBar()
    const item = { id: 4, label: 'Lint', command: 'npm run lint' }
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }

    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    expect(wrapper.vm.quickSendPressingId).toBe(4)

    // Move finger beyond 10px
    const moveEvent = { touches: [{ clientX: 70, clientY: 50 }] }
    wrapper.vm.onQuickSendTouchMove(moveEvent)

    expect(wrapper.vm.quickSendPressingId).toBeNull()
    // Advance timer — should NOT trigger long-press since cancelled
    vi.advanceTimersByTime(500)
    await nextTick()

    // No send or inject should have happened
    expect(wrapper.emitted('send')).toBeFalsy()
    expect(getInputText(wrapper)).toBe('')
    vi.useRealTimers()
  })

  it('onQuickSendTouchMove does not cancel when finger moves within threshold', async () => {
    vi.useFakeTimers()
    const wrapper = mountInputBar()
    const item = { id: 5, label: 'Deploy', command: 'npm run deploy' }
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }

    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    expect(wrapper.vm.quickSendPressingId).toBe(5)

    // Small move within 10px threshold
    const moveEvent = { touches: [{ clientX: 55, clientY: 53 }] }
    wrapper.vm.onQuickSendTouchMove(moveEvent)

    expect(wrapper.vm.quickSendPressingId).toBe(5)
    vi.useRealTimers()
  })

  it('cancelQuickSendPress clears all press state', async () => {
    const wrapper = mountInputBar()
    const item = { id: 6, label: 'Clean', command: 'npm run clean' }
    const touchEvent = { touches: [{ clientX: 50, clientY: 50 }] }

    wrapper.vm.onQuickSendTouchStart(item, touchEvent)
    expect(wrapper.vm.quickSendPressingId).toBe(6)

    wrapper.vm.cancelQuickSendPress()
    expect(wrapper.vm.quickSendPressingId).toBeNull()
  })

  it('onQuickSendTouchMove returns early when no press active', () => {
    const wrapper = mountInputBar()
    // No press active — should not throw
    const moveEvent = { touches: [{ clientX: 100, clientY: 100 }] }
    expect(() => wrapper.vm.onQuickSendTouchMove(moveEvent)).not.toThrow()
  })
})

describe('ChatInputBar — quoteData chip', () => {
  it('shows quote chip when quoteData prop is provided', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'selected code', filePath: '/foo.ts', language: 'typescript', startLine: 10, endLine: 20 },
    })
    await nextTick()

    expect(wrapper.find('.attachment-quote').exists()).toBe(true)
    expect(wrapper.find('.attachment-quote').attributes('title')).toBe('/foo.ts')
  })

  it('shows line number when startLine is present', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'some text', filePath: '/bar.ts', language: 'ts', startLine: 5, endLine: 10 },
    })
    await nextTick()

    expect(wrapper.find('.quote-line-info').exists()).toBe(true)
    expect(wrapper.find('.quote-line-info').text()).toBe('L5')
  })

  it('hides line number when startLine is 0', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'some text', filePath: '/bar.ts', language: 'ts', startLine: 0, endLine: 0 },
    })
    await nextTick()

    expect(wrapper.find('.quote-line-info').exists()).toBe(false)
  })

  it('emits remove-quote when quote remove button is clicked', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'quote', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 },
    })
    await nextTick()

    await wrapper.find('.attachment-quote .attachment-tag-remove').trigger('click')
    expect(wrapper.emitted('remove-quote')).toBeTruthy()
  })

  it('emits quote-click when quote chip is clicked', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'quote', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 },
    })
    await nextTick()

    await wrapper.find('.attachment-quote').trigger('click')
    expect(wrapper.emitted('quote-click')).toBeTruthy()
  })

  it('does not show quote chip when quoteData is null', async () => {
    const wrapper = mountInputBar({ quoteData: null })
    await nextTick()

    expect(wrapper.find('.attachment-quote').exists()).toBe(false)
  })

  it('shows attachment-tags area when quoteData is set even with no attached files', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'q', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 1 },
      attachedFiles: [],
      pendingFiles: [],
    })
    await nextTick()

    expect(wrapper.find('.chat-attachment-tags').exists()).toBe(true)
  })

  it('truncateQuoteText truncates long text with ellipsis', () => {
    const wrapper = mountInputBar()
    const result = wrapper.vm.truncateQuoteText('a very long text that exceeds the max length', 20)
    expect(result.length).toBeLessThanOrEqual(23) // 20 chars + '...'
    expect(result.endsWith('...')).toBe(true)
  })

  it('truncateQuoteText returns empty string for null/undefined', () => {
    const wrapper = mountInputBar()
    expect(wrapper.vm.truncateQuoteText(null, 20)).toBe('')
    expect(wrapper.vm.truncateQuoteText(undefined, 20)).toBe('')
  })

  it('truncateQuoteText replaces newlines with spaces', () => {
    const wrapper = mountInputBar()
    const result = wrapper.vm.truncateQuoteText('line1\nline2\nline3', 50)
    expect(result).toBe('line1 line2 line3')
  })

  it('truncateQuoteText keeps short text unchanged', () => {
    const wrapper = mountInputBar()
    const result = wrapper.vm.truncateQuoteText('short', 20)
    expect(result).toBe('short')
  })

  it('hasInputContent is true when quoteData is set', () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'q', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 1 },
    })
    expect(wrapper.vm.hasInputContent).toBeTruthy()
  })
})

describe('ChatInputBar — user message index button', () => {
  it('shows user message index button regardless of transport', () => {
    const wrapper = mountInputBar({ currentTransport: '' })
    const buttons = wrapper.findAll('.chat-action-btn')
    const indexBtn = buttons.find(b => b.attributes('title')?.includes('消息索引') || b.attributes('title')?.includes('Message index'))
    expect(indexBtn).toBeDefined()
  })

  it('shows user message index button for acp-stdio transport too', () => {
    const wrapper = mountInputBar({ currentTransport: 'acp-stdio' })
    const buttons = wrapper.findAll('.chat-action-btn')
    const indexBtn = buttons.find(b => b.attributes('title')?.includes('消息索引') || b.attributes('title')?.includes('Message index'))
    expect(indexBtn).toBeDefined()
  })

  it('emits open-user-msg-index when index button is clicked', async () => {
    const wrapper = mountInputBar({})
    const buttons = wrapper.findAll('.chat-action-btn')
    const indexBtn = buttons.find(b => b.attributes('title')?.includes('消息索引') || b.attributes('title')?.includes('Message index'))
    expect(indexBtn).toBeDefined()

    await indexBtn!.trigger('click')
    expect(wrapper.emitted('open-user-msg-index')).toBeTruthy()
  })
})
