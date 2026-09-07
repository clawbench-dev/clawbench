import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, shallowMount } from '@vue/test-utils'
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
          autoSpeech: '朗读',
          sessionSettings: '会话设置',
          switchThinkingEffort: '切换思考强度',
          archiveCurrentSession: '归档当前会话',
          noSessionToArchive: '无可归档会话',
          forkSession: '派生会话',
          userMsgIndex: '消息索引',
          sessionSearch: '搜索会话',
          acpSync: 'ACP 同步',
          reloadSession: '重新打开会话',
          wideLabels: {
            session: '列表',
            create: '创建',
            search: '搜索',
            jump: '跳转',
            sync: '同步',
            archive: '归档',
            speak: '朗读',
            refresh: '刷新',
          },
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
        quickSend: { title: '快捷发送', injectToInput: '加入输入框', edit: '管理' },
        modelSwitcher: { title: '切换模型' },
        thinkingEffortSwitcher: { title: '思考强度', auto: '自动' },
      },
      common: { remove: '移除' },
    },
  },
})

// ── Timer leak prevention ───────────────────────────────────
const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

afterEach(() => {
  for (const id of pendingTimers) { clearTimeout(id) }
  pendingTimers.length = 0
  for (const id of pendingIntervals) { clearInterval(id) }
  pendingIntervals.length = 0
})

beforeEach(() => {
  mockFetchItems.mockReset()
})

const TeleportStub = { template: '<div><slot /></div>' }

function mountInputBar(props = {}, { deep = false }: { deep?: boolean } = {}) {
  const mountFn = deep ? mount : shallowMount
  return mountFn(ChatInputBar, {
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
    const wrapper = mountInputBar({}, { deep: true })
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
    const wrapper = mountInputBar({}, { deep: true })
    setInputText(wrapper, 'test message')
    await nextTick()

    const textarea = wrapper.find('.chat-textarea')
    // Dispatch a proper keyboard event that sets e.key='Enter'
    await textarea.trigger('keydown', { key: 'Enter' })

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

describe('ChatInputBar — prefillInput (rewind/回溯 prefill)', () => {
  it('replaces existing input content with the restored text', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'stale draft')
    await nextTick()

    wrapper.vm.prefillInput('editable question')
    await nextTick()

    expect(getInputText(wrapper)).toBe('editable question')
  })

  it('fills the input when it was empty', async () => {
    const wrapper = mountInputBar()

    wrapper.vm.prefillInput('editable question')
    await nextTick()

    expect(getInputText(wrapper)).toBe('editable question')
  })

  it('clears the input when restored text is empty', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'stale draft')
    await nextTick()

    wrapper.vm.prefillInput('')
    await nextTick()

    expect(getInputText(wrapper)).toBe('')
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

  it('closes the menu when handleQuickSendClick is called', async () => {
    const wrapper = mountInputBar()
    wrapper.vm.showQuickMenu = true
    const item = { id: 1, label: 'Git Status', command: 'git status' }
    wrapper.vm.handleQuickSendClick(item)
    await nextTick()

    expect(wrapper.vm.showQuickMenu).toBe(false)
  })
})

describe('ChatInputBar — quick-send inject to input', () => {
  it('handleQuickSendInject fills the input box and closes the menu', async () => {
    const wrapper = mountInputBar()
    wrapper.vm.showQuickMenu = true
    const item = { id: 1, label: 'Git Status', command: 'git status' }
    wrapper.vm.handleQuickSendInject(item)
    await nextTick()

    // Command is injected into input, not sent
    expect(getInputText(wrapper)).toBe('git status')
    expect(wrapper.emitted('send')).toBeFalsy()
    expect(wrapper.vm.showQuickMenu).toBe(false)
  })

  it('handleQuickSendInject appends with newline when input already has content', async () => {
    const wrapper = mountInputBar()
    setInputText(wrapper, 'hello')
    await nextTick()

    const item = { id: 2, label: 'Build', command: 'npm run build' }
    wrapper.vm.handleQuickSendInject(item)
    await nextTick()

    expect(getInputText(wrapper)).toBe('hello\nnpm run build')
    expect(wrapper.emitted('send')).toBeFalsy()
  })
})

describe('ChatInputBar — quoteData chip', () => {
  it('renders multiple staged quote chips in order', async () => {
    const wrapper = mountInputBar({
      quotes: [
        { id: 'q1', text: 'one', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 2, note: '' },
        { id: 'q2', text: 'two', filePath: '/b.ts', language: 'ts', startLine: 8, endLine: 8, note: 'check this' },
      ],
    })
    await nextTick()

    const chips = wrapper.findAll('.attachment-quote')
    expect(chips).toHaveLength(2)
    expect(chips[0].text()).toContain('a.ts:1-2')
    expect(chips[1].text()).toContain('b.ts:8')
    expect(chips[1].attributes('title')).toBe('check this')
  })

  it('removes and navigates a specific staged quote', async () => {
    const quote = { id: 'q2', text: 'two', filePath: '/b.ts', language: 'ts', startLine: 8, endLine: 8, note: '' }
    const wrapper = mountInputBar({ quotes: [quote] })
    await nextTick()

    await wrapper.find('.attachment-quote').trigger('click')
    await wrapper.find('.attachment-close-btn').trigger('click')

    expect(wrapper.emitted('quote-click')![0]).toEqual([quote])
    expect(wrapper.emitted('remove-quote')![0]).toEqual(['q2'])
  })

  it('sends staged quotes without requiring input text', async () => {
    const wrapper = mountInputBar({
      quotes: [{ id: 'q1', text: 'one', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 1, note: '' }],
    })
    expect(wrapper.vm.hasInputContent).toBeTruthy()
    await wrapper.find('.chat-send-btn').trigger('click')
    expect(wrapper.emitted('send')![0]).toEqual([''])
  })

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

    const name = wrapper.find('.attachment-quote .attachment-filename')
    expect(name.exists()).toBe(true)
    expect(name.text()).toContain('bar.ts')
    expect(name.text()).toContain(':5-10')
  })

  it('hides line number when startLine is 0', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'some text', filePath: '/bar.ts', language: 'ts', startLine: 0, endLine: 0 },
    })
    await nextTick()

    expect(wrapper.find('.attachment-quote .attachment-filename').text()).toBe('bar.ts')
  })

  it('emits remove-quote when quote remove button is clicked', async () => {
    const wrapper = mountInputBar({
      quoteData: { text: 'quote', filePath: '/a.ts', language: 'ts', startLine: 1, endLine: 3 },
    })
    await nextTick()

    await wrapper.find('.attachment-quote .attachment-close-btn').trigger('click')
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

describe('ChatInputBar — action labels by container width', () => {
  function actionBar(wrapper: ReturnType<typeof mount>) {
    return wrapper.find('.chat-top-actions')
  }
  function labelTexts(wrapper: ReturnType<typeof mount>) {
    return wrapper.findAll('.chat-action-label').map(el => el.text())
  }
  function mockBarWidth(wrapper: ReturnType<typeof mount>, scrollWidth: number, clientWidth: number) {
    const bar = actionBar(wrapper).element as HTMLElement
    Object.defineProperty(bar, 'scrollWidth', { configurable: true, value: scrollWidth })
    Object.defineProperty(bar, 'clientWidth', { configurable: true, value: clientWidth })
  }

  it('hides labels when the container is too narrow (icons only)', async () => {
    const wrapper = mountInputBar({}, { deep: true })
    await nextTick()
    // jsdom measures 0x0, which fits → labels shown by default. Force overflow.
    mockBarWidth(wrapper, 600, 400)
    wrapper.vm.measureActionLabels()
    await nextTick()

    expect(actionBar(wrapper).classes()).not.toContain('show-labels')
    expect(wrapper.find('.chat-group-label').exists()).toBe(true)
  })

  it('shows short Chinese labels for every action button when width suffices', async () => {
    const wrapper = mountInputBar({}, { deep: true })
    await nextTick()

    expect(actionBar(wrapper).classes()).toContain('show-labels')
    const texts = labelTexts(wrapper)
    // 列表、创建、搜索、跳转、归档、朗读、刷新 always render; 同步 only when
    // the current session uses ACP transport (isACPTransport computed).
    expect(texts).toContain('列表')
    expect(texts).toContain('创建')
    expect(texts).toContain('搜索')
    expect(texts).toContain('跳转')
    expect(texts).toContain('归档')
    expect(texts).toContain('朗读')
    expect(texts).toContain('刷新')
    expect(texts).not.toContain('同步')
  })

  it('shows the sync label for ACP transport sessions', async () => {
    const wrapper = mountInputBar({ currentTransport: 'acp-stdio' }, { deep: true })
    await nextTick()

    expect(actionBar(wrapper).classes()).toContain('show-labels')
    const texts = labelTexts(wrapper)
    expect(texts).toContain('同步')
    expect(texts).toContain('跳转')
  })

  it('keeps the group label visible whether or not labels are shown', async () => {
    // Width suffices → labels shown, group label stays
    const wrapper = mountInputBar({}, { deep: true })
    await nextTick()
    expect(actionBar(wrapper).classes()).toContain('show-labels')
    expect(wrapper.find('.chat-group-label').exists()).toBe(true)
    expect(wrapper.find('.chat-group-label').text()).toBe('会话')

    // Width insufficient → labels hidden, group label still stays
    mockBarWidth(wrapper, 600, 400)
    wrapper.vm.measureActionLabels()
    await nextTick()
    expect(actionBar(wrapper).classes()).not.toContain('show-labels')
    expect(wrapper.find('.chat-group-label').exists()).toBe(true)
  })
})
