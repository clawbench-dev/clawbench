import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, shallowMount, VueWrapper, flushPromises } from '@vue/test-utils'
import { ref, computed, nextTick } from 'vue'
import i18n from '@/i18n'

// ── Module mocks ──
// Control the terminal tab manager so we can simulate the "all sessions closed"
// state (activeTab === null) without real xterm/WebSocket instances.
const mockTabs = ref<any[]>([])
const mockActiveTabId = ref('')
const mockActiveTab = computed(() => mockTabs.value.find((t) => t.id === mockActiveTabId.value) || null)
const mockSession = {
  sendInput: vi.fn(),
  sendClose: vi.fn(),
  disconnect: vi.fn(),
  connect: vi.fn().mockResolvedValue(undefined),
  connectionState: 'disconnected',
  sessionId: '',
  wsOpen: false,
}
const mockTabManager = {
  tabs: mockTabs,
  activeTabId: mockActiveTabId,
  activeTab: mockActiveTab,
  createTab: vi.fn(),
  closeTab: vi.fn(() => ({ switchToId: null })),
  switchTab: vi.fn(),
  updateTabCwd: vi.fn(),
  syncTabSessionId: vi.fn(),
  mountTabXterm: vi.fn(),
  disconnectAll: vi.fn(),
  connectTab: vi.fn(),
  connectActiveTab: vi.fn(),
  disposeAll: vi.fn(),
  updateFontSize: vi.fn(),
  updateTheme: vi.fn(),
  getTab: vi.fn(),
}
vi.mock('@/composables/useTerminalTabs', () => ({
  useTerminalTabs: () => mockTabManager,
}))

vi.mock('@/composables/useTerminalKeys', () => ({
  useTerminalKeys: () => ({
    activeModifiers: ref({}),
    toggleModifier: vi.fn(),
    processInput: (d: string) => d,
    clearOnceModifiers: vi.fn(),
    reset: vi.fn(),
    send: vi.fn(),
    sendArrowUp: vi.fn(),
    sendArrowDown: vi.fn(),
    sendArrowLeft: vi.fn(),
    sendArrowRight: vi.fn(),
    sendPageUp: vi.fn(),
    sendPageDown: vi.fn(),
    sendTab: vi.fn(),
    sendCtrlC: vi.fn(),
    sendCtrlZ: vi.fn(),
    sendCtrlS: vi.fn(),
    sendCtrlD: vi.fn(),
    sendCtrlL: vi.fn(),
    sendCtrlR: vi.fn(),
    sendEscape: vi.fn(),
  }),
}))

vi.mock('@/composables/useTerminalGestures', () => ({
  selectionCellsToSelect: vi.fn(),
  shouldPreventTerminalContextMenu: vi.fn(() => false),
  useTerminalGestures: () => ({
    mode: ref('browse'),
    setMode: vi.fn(),
    cycleMode: vi.fn(),
    attach: vi.fn(),
    detach: vi.fn(),
  }),
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    state: ref({ visible: false }),
    confirm: vi.fn(),
    prompt: vi.fn(),
    alert: vi.fn(),
  }),
}))

vi.mock('@/composables/useQuickCommands', () => ({
  useQuickCommands: () => ({
    commands: ref([]),
    visibleCommands: ref([]),
    autoExecCommand: ref(null),
    fetchCommands: vi.fn().mockResolvedValue(undefined),
    addCommand: vi.fn(),
    updateCommand: vi.fn(),
    deleteCommand: vi.fn(),
    reorderCommands: vi.fn(),
    showEditDialog: ref(false),
  }),
}))

vi.mock('@/composables/useKeyConfig', () => ({
  useKeyConfig: () => ({
    keyItems: ref([]),
    symbolItems: ref([]),
    loading: ref(false),
    fetchConfig: vi.fn().mockResolvedValue(undefined),
    saveConfig: vi.fn(),
    selectedKeyIds: ref([]),
    selectedSymbolIds: ref([]),
    selectedKeys: ref([]),
    selectedSymbols: ref([]),
  }),
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: ref(false),
    isOpen: ref(false),
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  localConfig: {} as Record<string, unknown>,
  setLocalConfig: vi.fn(),
  useSettingsConfig: () => ({
    getServerValueWithDefault: vi.fn(() => 10),
    useSettingsConfig: undefined,
  }),
}))

vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({ isPC: ref(false) }),
}))

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: ref(false) }),
}))

vi.mock('@/utils/terminalThemes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/terminalThemes')>()
  return {
    ...actual,
    loadThemesModule: vi.fn().mockResolvedValue({}),
    resolveTheme: vi.fn().mockResolvedValue({ background: '#000000' }),
  }
})

vi.mock('@/utils/clipboard', () => ({
  copyText: vi.fn(),
}))

// Real composables under test
import TerminalPanelContent from '@/components/terminal/TerminalPanelContent.vue'
import { useTerminalKeyboard } from '@/composables/useTerminalKeyboard'

let wrapper: VueWrapper<any> | null = null
let container: HTMLDivElement
let originalVisualViewport: VisualViewport | null
let originalInnerHeight: number
let visualViewportResizeHandler: (() => void) | null = null
// Single stable visualViewport object whose height we mutate (jsdom has none).
let vvHeight = 800

beforeEach(() => {
  container = document.createElement('div')
  document.body.appendChild(container)
  originalInnerHeight = window.innerHeight
  originalVisualViewport = window.visualViewport
  visualViewportResizeHandler = null
  vvHeight = 800
  Object.defineProperty(window, 'innerHeight', {
    value: 800,
    writable: true,
    configurable: true,
  })
  Object.defineProperty(window, 'visualViewport', {
    value: {
      get height() { return vvHeight },
      get offsetTop() { return 0 },
      addEventListener: (_type: string, cb: () => void) => { visualViewportResizeHandler = cb },
      removeEventListener: vi.fn(),
    },
    writable: true,
    configurable: true,
  })
})

afterEach(async () => {
  if (wrapper) {
    wrapper.unmount()
    wrapper = null
  }
  mockTabs.value = []
  mockActiveTabId.value = ''
  Object.defineProperty(window, 'innerHeight', {
    value: originalInnerHeight,
    writable: true,
    configurable: true,
  })
  if (originalVisualViewport) {
    Object.defineProperty(window, 'visualViewport', {
      value: originalVisualViewport,
      writable: true,
      configurable: true,
    })
  }
  if (container.parentNode) document.body.removeChild(container)
})

/** Simulate the soft keyboard opening: shrink visualViewport then fire resize. */
function openKeyboard() {
  vvHeight = 600
  visualViewportResizeHandler?.()
}

describe('TerminalPanelContent keyboard → Dock hiding', () => {
  it('hides the Dock when the keyboard opens after re-activating the panel with no session tabs and creating a new one', async () => {
    const { keyboardHeight } = useTerminalKeyboard()
    keyboardHeight.value = 0

    wrapper = shallowMount(TerminalPanelContent, {
      props: { active: false },
      global: { plugins: [i18n] },
    })
    await flushPromises()

    // Panel becomes active while every session tab is closed (activeTab === null).
    // startWatching must still be invoked so the visualViewport listener exists.
    await wrapper.setProps({ active: true })
    await nextTick()

    // User creates a new session tab → active container becomes available.
    const containerEl = document.createElement('div')
    document.body.appendChild(containerEl)
    mockTabs.value = [{ id: 't1', session: mockSession, container: containerEl, xterm: null, fitAddon: null }]
    mockActiveTabId.value = 't1'
    await nextTick()

    // Keyboard opens — the Dock-hiding shared height must rise even though the
    // panel was (re-)activated with no sessions and the tab was created later.
    openKeyboard()
    await nextTick()
    expect(keyboardHeight.value).toBeGreaterThan(0)

    if (containerEl.parentNode) document.body.removeChild(containerEl)
  })

  it('restores the shared keyboard height to 0 when the keyboard closes', async () => {
    const { keyboardHeight } = useTerminalKeyboard()
    keyboardHeight.value = 0

    const containerEl = document.createElement('div')
    document.body.appendChild(containerEl)
    mockTabs.value = [{ id: 't1', session: mockSession, container: containerEl, xterm: null, fitAddon: null }]
    mockActiveTabId.value = 't1'

    wrapper = shallowMount(TerminalPanelContent, {
      props: { active: true },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    await nextTick()

    openKeyboard()
    await nextTick()
    expect(keyboardHeight.value).toBeGreaterThan(0)

    // Keyboard closes: visualViewport restores to full height.
    vvHeight = 800
    visualViewportResizeHandler?.()
    await nextTick()
    expect(keyboardHeight.value).toBe(0)

    if (containerEl.parentNode) document.body.removeChild(containerEl)
  })
})