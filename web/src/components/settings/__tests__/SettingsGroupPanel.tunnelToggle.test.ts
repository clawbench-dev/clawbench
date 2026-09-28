import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import SettingsGroupPanel from '@/components/settings/SettingsGroupPanel.vue'
import type { GroupPanelConfig } from '@/components/settings/settingsFieldMap'
import { readAndroidBridge, androidBridgeExposes } from '@/testUtils/androidBridgeContract'

/**
 * The Android-only h2 tunnel toggle in the port-forward panel.
 *
 * It is a dedicated row (not a commonFields entry), because its truth source is
 * Android SharedPreferences: routing it through usePanelSnapshot would either
 * write localStorage ('local') or 400 the entire save ('server', since
 * validatePatchFields rejects unknown keys). These tests pin the three things
 * that make the dedicated row correct: the Android-only platform gate, the
 * async initial value with legacy-host degradation, and the write-on-toggle +
 * reconnect flow.
 */

// ── Mocks ──

const mockInitSnapshot = vi.fn()
const mockHandleSave = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] })
const localValues = reactive<Record<string, unknown>>({})

vi.mock('@/composables/usePanelSnapshot', () => ({
  usePanelSnapshot: () => ({
    localValues,
    saving: ref(false),
    serverError: ref(''),
    hotReloadWarning: ref(''),
    hasFailedSave: ref(false),
    hasChanges: ref(false),
    canSave: ref(true),
    needsRestartHint: ref(false),
    initSnapshot: mockInitSnapshot,
    handleSave: mockHandleSave,
  }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: vi.fn().mockReturnValue(undefined),
    localConfig: reactive<Record<string, unknown>>({}),
  }),
}))

vi.mock('@/composables/useSettingsNavigation', () => ({
  useSettingsNavigation: () => ({ registerGuard: vi.fn(), unregisterGuard: vi.fn() }),
}))

vi.mock('@/composables/useConnectivityTest', () => ({
  useConnectivityTest: () => ({
    testing: ref(false),
    testResults: ref([]),
    runTests: vi.fn().mockResolvedValue(undefined),
    clearResults: vi.fn(),
  }),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({ open: vi.fn(), effectiveOpen: ref(false), close: vi.fn() }),
}))

vi.mock('@/composables/useFrp', () => ({
  useFrp: () => ({ frpState: reactive({ state: 'disabled', remotePort: 0, sshRemotePort: 0 }) }),
}))

vi.mock('@/composables/useRagStatus', () => ({
  useRagStatus: () => ({
    status: ref({ available: false, mode: 'none', embedder_healthy: false, total_messages: 0, indexed_messages: 0, embedded_messages: 0 }),
    refresh: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))

vi.mock('@/composables/useRagRebuild', () => ({
  startRebuild: vi.fn(),
  rebuildStatus: ref({ kind: '', status: 'idle', phase: '', total: 0, processed: 0, progress_pct: 0, elapsed_ms: 0 }),
}))

vi.mock('@/utils/api', () => ({ apiPost: vi.fn().mockResolvedValue(undefined) }))

// Platform mode holders. Plain objects (not refs) — the factory is hoisted
// above the vue import, and the component reads `.value` off them directly.
const mockAppMode = vi.hoisted(() => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }))
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockAppMode.isAppMode, isDesktopApp: mockAppMode.isDesktopApp }),
}))

// Bridge holder. `getNative` returns whatever `native` currently is, so each
// case can install the host shape it wants (new method / absent method / none).
const mockNative = vi.hoisted(() => ({ current: undefined as unknown }))
const mockReconnectTunnel = vi.hoisted(() => ({ fn: vi.fn() }))
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => mockNative.current,
  reconnectTunnel: (...args: unknown[]) => mockReconnectTunnel.fn(...args),
}))

// ── i18n ──

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        panel: { saved: '已保存', saving: '保存中…', save: '保存', testing: '测试中…', testConnectivity: '测试连通性', needsRestartHint: '需要重启生效' },
        items: {
          portForwardEnabled: '启用端口映射',
          portForwardPort: '端口',
          portForwardH2: 'H2 模式（实验）',
          portForwardH2Desc: '改用 HTTP/2 隧道',
          portForwardH2ReconnectHint: '切换将在下次重连后生效',
          portForwardH2Reconnect: '立即重连',
        },
      },
      portForward: { tunnelReconnected: '隧道已重连' },
    },
  },
})

// ── Helpers ──

function makePortForwardConfig(): GroupPanelConfig {
  return {
    panelId: 'portForward',
    enableKey: 'port_forward.enabled',
    enableLabelKey: 'settings.items.portForwardEnabled',
    commonFields: [
      { labelKey: 'settings.items.portForwardPort', key: 'port_forward.port', type: 'number', source: 'server' },
    ],
    hasConnectivityTest: true,
    getTestCategories: (values) => [{ category: 'port_forward', values }],
  }
}

function mountPanel(config: GroupPanelConfig = makePortForwardConfig()) {
  return mount(SettingsGroupPanel, {
    props: { config, showTitle: false },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsItem: {
          name: 'SettingsItem',
          props: ['label', 'description', 'type', 'modelValue', 'options', 'min', 'max', 'step', 'needsRestart', 'disabled', 'forceClose', 'defaultValue', 'displayFormat', 'displayTransform', 'noDivider', 'progress', 'refreshable', 'refreshing'],
          template: '<div class="mock-settings-item" :data-key="label" :data-type="type" :data-value="modelValue"><button v-if="type === \'switch\'" class="mock-switch" @click="$emit(\'update:modelValue\', !modelValue)">toggle</button></div>',
          emits: ['update:modelValue', 'editToggle', 'descToggle', 'click', 'refresh'],
        },
        BottomSheet: { template: '<div><slot /></div>' },
        ChevronRight: { template: '<span>></span>' },
        RefreshCw: { template: '<span>↻</span>' },
      },
    },
  })
}

/** The h2 toggle row's SettingsItem (the only switch in this panel). */
function findToggle(wrapper: ReturnType<typeof mountPanel>) {
  return wrapper.findAllComponents({ name: 'SettingsItem' }).find(i => i.props('type') === 'switch')
}

beforeEach(() => {
  vi.clearAllMocks()
  for (const key of Object.keys(localValues)) delete localValues[key]
  localValues['port_forward.enabled'] = true
  localValues['port_forward.port'] = 0
  mockAppMode.isAppMode.value = false
  mockAppMode.isDesktopApp.value = false
  mockNative.current = undefined
  mockReconnectTunnel.fn.mockReset()
  mockReconnectTunnel.fn.mockResolvedValue(true)
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Platform gate ──

describe('h2 toggle: platform gate', () => {
  it('renders in the Android WebView shell', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)).toBeTruthy()
  })

  it('hides in plain web mode', async () => {
    mockAppMode.isAppMode.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)).toBeFalsy()
  })

  it('hides in the Electron desktop shell (isAppMode is also true there)', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)).toBeFalsy()
  })

  it('hides on a non-portForward panel even on Android', async () => {
    mockAppMode.isAppMode.value = true
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel({ panelId: 'terminal', commonFields: [] })
    await flushPromises()

    expect(findToggle(wrapper)).toBeFalsy()
  })
})

// ── Async initial value / degradation ──

describe('h2 toggle: initial value', () => {
  beforeEach(() => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
  })

  it('stays hidden until the async initial value arrives', async () => {
    let resolve: (v: boolean) => void = () => {}
    mockNative.current = {
      getTunnelTransportH2Enabled: () => new Promise<boolean>(r => { resolve = r }),
    }

    const wrapper = mountPanel()
    await nextTick()
    // Pending: must not flash a false "off".
    expect(findToggle(wrapper)).toBeFalsy()

    resolve(true)
    await flushPromises()
    expect(findToggle(wrapper)).toBeTruthy()
  })

  it('reflects a true initial value', async () => {
    mockNative.current = { getTunnelTransportH2Enabled: () => true }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)!.props('modelValue')).toBe(true)
  })

  it('reflects a false initial value', async () => {
    mockNative.current = { getTunnelTransportH2Enabled: () => Promise.resolve(false) }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)!.props('modelValue')).toBe(false)
  })

  it('hides on a legacy host without the getter (never shows a fake off)', async () => {
    mockNative.current = { isNativeApp: () => true }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)).toBeFalsy()
  })

  it('hides when the host has the setter but not the getter (the 2026 bug)', async () => {
    // This is the exact shape the real Android host shipped with before the
    // fix: the bridge had setTunnelTransportH2Enabled but no getter, because
    // the older getTunnelTransport() was kept for the status display and the
    // missing read accessor was never noticed. The row must stay hidden rather
    // than render a switch whose initial value could never be read — pinning
    // that the getter, not the setter, is the visibility precondition.
    mockNative.current = { setTunnelTransportH2Enabled: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)).toBeFalsy()
  })

  it('hides when the getter rejects', async () => {
    mockNative.current = { getTunnelTransportH2Enabled: () => Promise.reject(new Error('bridge')) }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findToggle(wrapper)).toBeFalsy()
  })
})

// ── Toggle + reconnect ──

describe('h2 toggle: interaction', () => {
  beforeEach(() => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    mockNative.current = {
      getTunnelTransportH2Enabled: () => false,
      setTunnelTransportH2Enabled: vi.fn(),
    }
  })

  it('writes the new value through the setter on change', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    const item = findToggle(wrapper)!
    await item.vm.$emit('update:modelValue', true)

    const setter = (mockNative.current as { setTunnelTransportH2Enabled: ReturnType<typeof vi.fn> }).setTunnelTransportH2Enabled
    expect(setter).toHaveBeenCalledWith(true)
    expect(findToggle(wrapper)!.props('modelValue')).toBe(true)
  })

  it('shows the reconnect hint and button alongside the row', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.group-panel__h2-hint').exists()).toBe(true)
    expect(wrapper.find('.group-panel__h2-hint').text()).toContain('切换将在下次重连后生效')
    expect(wrapper.find('.group-panel__h2-reconnect').exists()).toBe(true)
  })

  it('reconnects and toasts the existing message on success', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.group-panel__h2-reconnect').trigger('click')
    await flushPromises()

    expect(mockReconnectTunnel.fn).toHaveBeenCalledOnce()
    expect(mockToastShow).toHaveBeenCalledWith('隧道已重连', expect.objectContaining({ type: 'success' }))
  })

  it('does not toast when the reconnect fails', async () => {
    mockReconnectTunnel.fn.mockResolvedValue(false)
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.find('.group-panel__h2-reconnect').trigger('click')
    await flushPromises()

    expect(mockToastShow).not.toHaveBeenCalled()
  })
})

// ── Real-host contract ──

/**
 * The root cause of the 2026 h2-toggle bug was that every test above used a
 * hand-built fake host, so they only ever proved "the row works when the getter
 * is present" — never that the shipped Android host has it. The tests here read
 * the REAL Android bridge source instead.
 *
 * A fully generic "every optional method declared in clawbenchNative.ts exists
 * on Android" check is not expressible: most optional methods are deliberately
 * Electron-only (nativeNotify, setZoomFactor, isDesktopApp), so optionality in
 * the TS interface carries no host information. What IS checkable — and what
 * this bug actually needed — is that the methods the Android host is required
 * to expose for this feature are both implemented and bridged.
 *
 * The `readAndroidBridge` / `androidBridgeExposes` helpers live in
 * `@/testUtils/androidBridgeContract` so other contract specs share them.
 */
describe('h2 toggle: real Android host contract', () => {
  const src = readAndroidBridge()

  it('the Android bridge implements the getter the row requires', () => {
    // SettingsGroupPanel.vue hides the row when `getTunnelTransportH2Enabled`
    // is absent, so without this the toggle can never be turned on in the app.
    expect(androidBridgeExposes(src, 'getTunnelTransportH2Enabled')).toBe(true)
  })

  it('the Android bridge implements the setter the row writes through', () => {
    expect(androidBridgeExposes(src, 'setTunnelTransportH2Enabled')).toBe(true)
  })

  it('the getter delegates to the SharedPreferences source of truth', () => {
    // Pins that the getter reads the same store the setter writes rather than
    // returning a constant (which would make the row render a stale value).
    expect(src).toContain('BackgroundService.isTunnelTransportH2Enabled(activity)')
  })
})
