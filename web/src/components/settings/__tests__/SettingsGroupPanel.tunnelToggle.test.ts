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

// appLog relays to native/server; keep it inert in unit tests.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Platform mode holders. Plain objects (not refs) — the factory is hoisted
// above the vue import, and the component reads `.value` off them directly.
const mockAppMode = vi.hoisted(() => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }))
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockAppMode.isAppMode, isDesktopApp: mockAppMode.isDesktopApp }),
}))

// The component reads the host axis through usePlatformDetect, NOT useAppMode.
// The real composable copies useAppMode into module-level refs behind a
// `platformInitialized` guard, so it would snapshot whatever the FIRST test in
// this file set and ignore every later change — which is exactly the
// order-dependent failure this mock prevents. Deriving the axes per call from
// mockAppMode keeps the suite's platform setup meaningful for every test:
//   isElectron  = isDesktopApp
//   isAndroidApp = isAppMode && !isDesktopApp
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({
    isElectron: { value: mockAppMode.isDesktopApp.value },
    isAndroidApp: { value: mockAppMode.isAppMode.value && !mockAppMode.isDesktopApp.value },
    isWebApp: { value: !mockAppMode.isAppMode.value },
    isNativeApp: { value: mockAppMode.isAppMode.value },
    isTouchPrimary: { value: false },
  }),
}))

// The status row reads the port-forward composable's shared state. Mock it so
// the row is deterministic and the suite never touches the network layer.
// Real refs, not bare `{ value }` objects: the component's template binds
// `:disabled="tunnelChecking"`, and only a real ref is auto-unwrapped there — a
// plain object is always truthy and would disable the button permanently.
const mockTunnel = vi.hoisted(() => ({}) as Record<string, unknown>)
vi.mock('@/composables/usePortForward', async () => {
  const { ref } = await import('vue')
  const tunnelStatus = ref('unknown')
  const tunnelChecking = ref(false)
  const tunnelMessage = ref('')
  const checkTunnelHealth = vi.fn().mockResolvedValue(undefined)
  // Web mode's status row reads these instead of the client-tunnel state: with
  // no native tunnel there is nothing client-side to report, so readiness comes
  // from the server (SSH listener up, or an h2-capable transport).
  const sshInfo = ref<{ enabled: boolean } | null>(null)
  const transportAllowsH2 = ref(false)
  const loadSSHInfo = vi.fn().mockResolvedValue(undefined)
  Object.assign(mockTunnel, {
    tunnelStatus, tunnelChecking, tunnelMessage, checkTunnelHealth,
    sshInfo, transportAllowsH2, loadSSHInfo,
  })
  return {
    usePortForward: () => ({
      tunnelStatus, tunnelChecking, tunnelMessage, checkTunnelHealth,
      sshInfo, transportAllowsH2, loadSSHInfo,
    }),
  }
})

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
        panel: {
          saved: '配置已保存', saving: '保存中…', save: '保存', testing: '测试中…',
          testConnectivity: '测试连通性', needsRestartHint: '需要重启生效',
          transportApplyFailed: '传输方式已保存，但隧道重连失败，端口映射暂不可用',
        },
        items: {
          portForwardEnabled: 'SSH 隧道服务器',
          portForwardEnabledDesc: '监听独立端口，供手动 ssh -L 或桌面端连接',
          portForwardPort: 'SSH 隧道端口',
          portForwardTransport: '隧道传输方式',
          portForwardTransportDesc: '选择端口映射使用的传输通道',
          tunnelStatusConnected: '隧道已连接',
          tunnelStatusServerReady: '服务器已就绪，可用 App 或 ssh 建立隧道',
          tunnelStatusServerReadyH2: '服务器已就绪，端口映射走 H2（主端口）',
          tunnelStatusUnavailable: '端口映射不可用：SSH 隧道服务器已关闭，且当前传输方式不支持 H2',
          tunnelStatusChecking: '正在检测隧道状态…',
          tunnelStatusUnknown: '隧道状态未知',
        },
      },
      proxy: { transportSsh: 'SSH', transportH2: 'HTTP/2', retryCheck: '重新检测' },
    },
  },
})

// ── Helpers ──

function makePortForwardConfig(): GroupPanelConfig {
  return {
    panelId: 'portForward',
    // No enableKey: the SSH listener is always on, so the panel has no switch.
    commonFields: [
      { labelKey: 'settings.items.portForwardPort', key: 'port_forward.port', type: 'number', source: 'server' },
    ],
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

/** The transport picker's SettingsItem (the only select in this panel). */
function findTransportSelect(wrapper: ReturnType<typeof mountPanel>) {
  return wrapper.findAllComponents({ name: 'SettingsItem' }).find(i => i.props('type') === 'select')
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
  ;(mockTunnel.tunnelStatus as { value: string }).value = 'unknown'
  ;(mockTunnel.tunnelChecking as { value: boolean }).value = false
  ;(mockTunnel.tunnelMessage as { value: string }).value = ''
  ;(mockTunnel.checkTunnelHealth as ReturnType<typeof vi.fn>).mockClear()
  ;(mockTunnel.sshInfo as { value: unknown }).value = null
  ;(mockTunnel.transportAllowsH2 as { value: boolean }).value = false
  ;(mockTunnel.loadSSHInfo as ReturnType<typeof vi.fn>).mockClear()
})

afterEach(() => {
  vi.restoreAllMocks()
})

// ── Platform gate ──

describe('transport picker: platform gate', () => {
  it('renders in the Android WebView shell', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => false, setTunnelTransportH2Enabled: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeTruthy()
  })

  it('hides in plain web mode', async () => {
    mockAppMode.isAppMode.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })

  it('renders the same select on the Electron desktop shell', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeTruthy()
  })

  it('hides on a non-portForward panel even on Android', async () => {
    mockAppMode.isAppMode.value = true
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel({ panelId: 'terminal', commonFields: [] })
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })
})

// ── Async initial value / degradation ──

describe('transport picker: Android initial value', () => {
  beforeEach(() => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
  })

  it('stays hidden until the async initial value arrives', async () => {
    let resolve: (v: boolean) => void = () => {}
    mockNative.current = {
      getTunnelTransportH2Enabled: () => new Promise<boolean>(r => { resolve = r }),
      setTunnelTransportH2Enabled: vi.fn(),
    }

    const wrapper = mountPanel()
    await nextTick()
    // Pending: must not flash a default before the real value arrives.
    expect(findTransportSelect(wrapper)).toBeFalsy()

    resolve(true)
    await flushPromises()
    expect(findTransportSelect(wrapper)).toBeTruthy()
  })

  it('maps the boolean ON to the h2 option', async () => {
    mockNative.current = { getTunnelTransportH2Enabled: () => true, setTunnelTransportH2Enabled: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
  })

  it('maps the boolean OFF to the ssh option', async () => {
    mockNative.current = { getTunnelTransportH2Enabled: () => Promise.resolve(false), setTunnelTransportH2Enabled: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('ssh')
  })

  it('hides on a legacy host without the getter (never shows a fake default)', async () => {
    mockNative.current = { isNativeApp: () => true }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })

  it('hides when the host has the setter but not the getter (the 2026 bug)', async () => {
    // This is the exact shape the real Android host shipped with before the
    // fix: the bridge had setTunnelTransportH2Enabled but no getter, because
    // the older getTunnelTransport() was kept for the status display and the
    // missing read accessor was never noticed. The row must stay hidden rather
    // than render a picker whose initial value could never be read — pinning
    // that the getter, not the setter, is the visibility precondition.
    mockNative.current = { setTunnelTransportH2Enabled: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })

  it('hides when the getter rejects', async () => {
    mockNative.current = { getTunnelTransportH2Enabled: () => Promise.reject(new Error('bridge')) }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })
})

// ── Draft + reconnect (Android) ──

describe('transport picker: Android draft + save', () => {
  /** The panel's Save button. */
  function saveButton(wrapper: ReturnType<typeof mountPanel>) {
    return wrapper.find('.group-panel__save-btn')
  }

  /**
   * A fake Android bridge backed by a mutable value, like the real
   * SharedPreferences: the setter must actually change what the getter returns,
   * otherwise the new readback verification (WARN-401) treats the write as a
   * silent failure. `getTunnelTransportH2Enabled` is a plain sync function (the
   * real @JavascriptInterface method is synchronous), so this is faithful.
   */
  function statefulAndroidHost(initial: boolean) {
    let stored = initial
    const setTunnelTransportH2Enabled = vi.fn((on: boolean) => { stored = on })
    return {
      host: {
        getTunnelTransportH2Enabled: () => stored,
        setTunnelTransportH2Enabled,
      },
      setTunnelTransportH2Enabled,
      get stored() { return stored },
    }
  }

  beforeEach(() => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    const h = statefulAndroidHost(false)
    mockNative.current = h.host
  })

  it('does NOT write on change — the value stays a draft', async () => {
    // Writing on change made the Save button lie: the preference was already
    // persisted while the panel still reported itself clean.
    const wrapper = mountPanel()
    await flushPromises()

    const setter = (mockNative.current as { setTunnelTransportH2Enabled: ReturnType<typeof vi.fn> }).setTunnelTransportH2Enabled
    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await flushPromises()

    expect(setter).not.toHaveBeenCalled()
    // The row reflects the draft so the user sees what they are about to save.
    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
  })

  it('enables Save when the transport is the only thing changed', async () => {
    // Without folding the draft into the dirty flag the button stays disabled
    // and the change can never be committed.
    const wrapper = mountPanel()
    await flushPromises()

    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await flushPromises()

    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('writes the boolean and reconnects on save', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    const setter = (mockNative.current as { setTunnelTransportH2Enabled: ReturnType<typeof vi.fn> }).setTunnelTransportH2Enabled
    // The picker's 'h2' is the Android bridge's boolean true.
    expect(setter).toHaveBeenCalledWith(true)
    expect(mockReconnectTunnel.fn).toHaveBeenCalledOnce()
    // Committed: the draft is now the persisted value, so the panel is clean
    // again (otherwise Save stays enabled forever).
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
  })

  it('writes the boolean false when switching back to ssh', async () => {
    // The inverse mapping: picking 'ssh' must store false, not leave the host
    // on h2. Pins the direction of the conversion (a constant `true` write
    // would pass the h2 case above).
    const h = statefulAndroidHost(true)
    mockNative.current = h.host
    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'ssh')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(h.setTunnelTransportH2Enabled).toHaveBeenCalledWith(false)
    expect(h.stored).toBe(false)
  })

  it('does not reconnect when only a server field changed', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    // Touch a commonFields entry, not the transport.
    localValues['port_forward.port'] = 12345
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
  })

  it('keeps the draft and warns when the native write rejects', async () => {
    // A rejected write means nothing was persisted, so the tunnel is untouched.
    // The draft must survive so the user can retry, and the failure must not be
    // reported as a plain success.
    const setter = vi.fn().mockRejectedValue(new Error('bridge'))
    mockNative.current = {
      getTunnelTransportH2Enabled: () => false,
      setTunnelTransportH2Enabled: setter,
    }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(setter).toHaveBeenCalledWith(true)
    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
    expect(mockToastShow).not.toHaveBeenCalledWith('配置已保存', expect.anything())
    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
  })

  it('keeps the draft and warns when the native write is silently dropped', async () => {
    // Regression (WARN-401): the Android setter is a synchronous void method, so
    // a no-op write (stubbed context / wrong key) used to be indistinguishable
    // from success — the panel went clean while the host kept the old value and
    // the user could not re-trigger the save. The readback must catch it.
    const setter = vi.fn() // accepts the call, changes nothing
    mockNative.current = {
      getTunnelTransportH2Enabled: () => false,
      setTunnelTransportH2Enabled: setter,
    }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(setter).toHaveBeenCalledWith(true)
    // Still a draft (not committed), and the tunnel was not touched.
    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
    expect(mockToastShow).not.toHaveBeenCalledWith('配置已保存', expect.anything())
    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
  })

  it('keeps the stored preference when the reconnect fails', async () => {
    // The preference WAS accepted and persisted, so reverting it would make the
    // host and the UI disagree. The user is warned instead.
    mockReconnectTunnel.fn.mockResolvedValue(false)
    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    const setter = (mockNative.current as { setTunnelTransportH2Enabled: ReturnType<typeof vi.fn> }).setTunnelTransportH2Enabled
    expect(setter).toHaveBeenCalledWith(true)
    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
    expect(mockToastShow).toHaveBeenCalledWith(
      '传输方式已保存，但隧道重连失败，端口映射暂不可用',
      expect.objectContaining({ type: 'error' }),
    )
  })

  it('no longer renders a reconnect hint or button', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.group-panel__h2-hint').exists()).toBe(false)
    expect(wrapper.find('.group-panel__h2-reconnect').exists()).toBe(false)
  })
})

// ── Desktop transport picker (Electron) ──

/**
 * The desktop shell renders the SAME two-valued picker Android gets; only the
 * bridge vocabulary differs (Electron store vs SharedPreferences). The
 * automatic mode was removed, so the choice is always one concrete wire.
 */
describe('desktop transport picker: platform gate', () => {
  it('renders in the Electron shell', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeTruthy()
  })

  it('hides in plain web mode', async () => {
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })

  it('hides on a non-portForward panel even in Electron', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel({ panelId: 'terminal', commonFields: [] })
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })
})

describe('desktop transport picker: initial value', () => {
  beforeEach(() => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
  })

  it('offers exactly two transports with the shared labels', async () => {
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    const options = findTransportSelect(wrapper)!.props('options') as Array<{ label: string; value: string }>
    // No 'both': the automatic mode was removed, so the choice is always one
    // concrete wire.
    expect(options.map(o => o.value)).toEqual(['ssh', 'h2'])
    // Labels come from the same keys the panel's "current transport" line uses.
    expect(options.map(o => o.label)).toEqual(['SSH', 'HTTP/2'])
  })

  it('reflects the stored preference', async () => {
    mockNative.current = { getTunnelTransport: () => 'h2', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
  })

  it('hides on a host without the setter (an older desktop build)', async () => {
    // No setter means the choice could not persist, so the row must not appear.
    mockNative.current = { getTunnelTransport: () => 'ssh' }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })

  it('hides when the host reports a value it cannot interpret', async () => {
    mockNative.current = { getTunnelTransport: () => 'quic', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })

  it('hides when the getter rejects', async () => {
    mockNative.current = {
      getTunnelTransport: () => Promise.reject(new Error('bridge')),
      setTunnelTransport: vi.fn(),
    }

    const wrapper = mountPanel()
    await flushPromises()

    expect(findTransportSelect(wrapper)).toBeFalsy()
  })
})

describe('desktop transport picker: draft + save', () => {
  function saveButton(wrapper: ReturnType<typeof mountPanel>) {
    return wrapper.find('.group-panel__save-btn')
  }

  beforeEach(() => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
  })

  it('does NOT write on change — the value stays a draft', async () => {
    const setter = vi.fn().mockResolvedValue(true)
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: setter }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await flushPromises()

    expect(setter).not.toHaveBeenCalled()
    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
  })

  it('enables Save when the transport is the only thing changed', async () => {
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn().mockResolvedValue(true) }

    const wrapper = mountPanel()
    await flushPromises()
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await flushPromises()

    expect(saveButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('writes the preference and reconnects on save', async () => {
    const setter = vi.fn().mockResolvedValue(true)
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: setter }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(setter).toHaveBeenCalledWith('h2')
    expect(mockReconnectTunnel.fn).toHaveBeenCalledOnce()
    // Committed: the draft is now the persisted value, so the panel is clean
    // again (otherwise Save stays enabled forever).
    expect(saveButton(wrapper).attributes('disabled')).toBeDefined()
  })

  it('keeps the draft and warns when the host rejects the value', async () => {
    const setter = vi.fn().mockResolvedValue(false)
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: setter }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(setter).toHaveBeenCalledWith('h2')
    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
    expect(mockToastShow).not.toHaveBeenCalledWith('配置已保存', expect.anything())
    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
  })

  it('keeps the draft and warns when the setter throws', async () => {
    const setter = vi.fn().mockRejectedValue(new Error('bridge'))
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: setter }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
  })

  it('keeps the stored preference when the reconnect fails', async () => {
    mockReconnectTunnel.fn.mockResolvedValue(false)
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn().mockResolvedValue(true) }

    const wrapper = mountPanel()
    await flushPromises()

    await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', 'h2')
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(findTransportSelect(wrapper)!.props('modelValue')).toBe('h2')
    expect(mockToastShow).toHaveBeenCalledWith(
      '传输方式已保存，但隧道重连失败，端口映射暂不可用',
      expect.objectContaining({ type: 'error' }),
    )
  })

  it('no longer renders a reconnect hint or button', async () => {
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn().mockResolvedValue(true) }

    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.find('.group-panel__h2-hint').exists()).toBe(false)
    expect(wrapper.find('.group-panel__h2-reconnect').exists()).toBe(false)
  })
})

// ── SSH-only rows hidden on an h2-only tunnel ──

/**
 * `port_forward.port` configures the SSH SERVER, not port mapping: the h2 wire
 * rides the main HTTP port and never reads it. When this client is on h2 that
 * row configures a port it never dials, so the panel hides it.
 *
 * There is no enable switch to reason about any more — the listener is pinned
 * on server-side, so the panel only ever hides an input, never a control.
 */
describe('h2-only transport: SSH-server rows', () => {
  /** The rendered port-mapping-port row, if any. */
  function portRow(wrapper: ReturnType<typeof mountPanel>) {
    return wrapper.findAllComponents({ name: 'SettingsItem' })
      .find(i => i.props('label') === 'SSH 隧道端口')
  }

  it('shows the port row on the default (SSH) desktop transport', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(portRow(wrapper)).toBeTruthy()
  })

  it('hides the port row on desktop H2', async () => {
    // Only the port row (a port nothing reads on h2) goes; the transport picker
    // itself is never hidden, since it is how the user would switch back.
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'h2', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(portRow(wrapper)).toBeFalsy()
  })

  it('hides the port row when the host reports the migrated h2 preference', async () => {
    // A store holding the retired 'both' is migrated to 'h2' by the main
    // process, so this is what such a host reports back — and it hides the row
    // exactly like a natively-chosen h2.
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'h2', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(portRow(wrapper)).toBeFalsy()
  })

  it('hides the port row on Android when the picker reads h2', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => true }

    const wrapper = mountPanel()
    await flushPromises()

    expect(portRow(wrapper)).toBeFalsy()
  })

  it('keeps the port row on Android when the picker reads ssh', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel()
    await flushPromises()

    expect(portRow(wrapper)).toBeTruthy()
  })

  it('keeps the port row in plain web mode', async () => {
    mockNative.current = undefined

    const wrapper = mountPanel()
    await flushPromises()

    expect(portRow(wrapper)).toBeTruthy()
  })
})

// ── Tunnel status row ──

/**
 * The row exists because applying a transport change takes the tunnel down and
 * rebuilds it — bounded by CONNECT_TIMEOUT_MS (20s) — with nothing else on
 * screen saying what is happening or whether it worked.
 */
describe('tunnel status row', () => {
  function statusRow(wrapper: ReturnType<typeof mountPanel>) {
    return wrapper.find('.group-panel__tunnel-status')
  }

  it('renders alongside the desktop picker', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).exists()).toBe(true)
  })

  it('renders alongside the Android toggle', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = false
    mockNative.current = { getTunnelTransportH2Enabled: () => false }

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).exists()).toBe(true)
  })

  it('renders in plain web mode too — it replaces the removed test button', async () => {
    // Web mode has no client tunnel, but the row still answers the question the
    // old "Test Connection" button answered: can this server carry forwards?
    mockNative.current = undefined
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: true }

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).exists()).toBe(true)
    expect(statusRow(wrapper).classes()).toContain('group-panel__tunnel-status--ok')
    expect(statusRow(wrapper).text()).toContain('服务器已就绪')
  })

  it('reports web mode ready over H2 when the SSH listener is off', async () => {
    mockNative.current = undefined
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: false }
    ;(mockTunnel.transportAllowsH2 as { value: boolean }).value = true

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).classes()).toContain('group-panel__tunnel-status--ok')
    expect(statusRow(wrapper).text()).toContain('H2')
  })

  it('reports web mode unavailable when neither wire can carry forwards', async () => {
    mockNative.current = undefined
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: false }
    ;(mockTunnel.transportAllowsH2 as { value: boolean }).value = false

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).classes()).toContain('group-panel__tunnel-status--disconnected')
    expect(statusRow(wrapper).text()).toContain('端口映射不可用')
  })

  it('stays out on a legacy host that cannot report a transport', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh' } // no setter

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).exists()).toBe(false)
  })

  it('reports a connected tunnel', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'ok'

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).classes()).toContain('group-panel__tunnel-status--ok')
    expect(statusRow(wrapper).text()).toContain('隧道已连接')
  })

  it('surfaces the composable message for a problem state', async () => {
    // tunnelMessage is already localized and carries the transport annotation,
    // so the row must show it verbatim rather than re-deriving the wording.
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'disconnected'
    ;(mockTunnel.tunnelMessage as { value: string }).value = '隧道未连接（SSH）'

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).classes()).toContain('group-panel__tunnel-status--disconnected')
    expect(statusRow(wrapper).text()).toContain('隧道未连接（SSH）')
  })

  it('shows the checking state while a probe is in flight', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }
    ;(mockTunnel.tunnelChecking as { value: boolean }).value = true

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).classes()).toContain('group-panel__tunnel-status--checking')
    expect(statusRow(wrapper).text()).toContain('正在检测隧道状态')
  })

  it('re-runs the health check from the retry button', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'disconnected'

    const wrapper = mountPanel()
    await flushPromises()

    await statusRow(wrapper).find('.group-panel__tunnel-retry').trigger('click')
    await flushPromises()

    expect(mockTunnel.checkTunnelHealth as ReturnType<typeof vi.fn>).toHaveBeenCalledOnce()
  })

  it('disables the retry button while a check is running', async () => {
    mockAppMode.isAppMode.value = true
    mockAppMode.isDesktopApp.value = true
    mockNative.current = { getTunnelTransport: () => 'ssh', setTunnelTransport: vi.fn() }
    ;(mockTunnel.tunnelChecking as { value: boolean }).value = true

    const wrapper = mountPanel()
    await flushPromises()

    expect(statusRow(wrapper).find('.group-panel__tunnel-retry').attributes('disabled')).toBeDefined()
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
describe('transport picker: real Android host contract', () => {
  const src = readAndroidBridge()

  it('the Android bridge implements the getter the row requires', () => {
    // SettingsGroupPanel.vue hides the row when `getTunnelTransportH2Enabled`
    // is absent, so without this the picker can never show the real value.
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
