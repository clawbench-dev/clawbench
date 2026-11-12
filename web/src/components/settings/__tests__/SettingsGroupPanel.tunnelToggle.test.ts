import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick } from 'vue'
import SettingsGroupPanel from '@/components/settings/SettingsGroupPanel.vue'
import type { GroupPanelConfig } from '@/components/settings/settingsFieldMap'

/**
 * The port-forward panel's transport row.
 *
 * The transport used to be a host-local preference with its own picker row and
 * bridge read/write/readback dance. It is now an ordinary SERVER field
 * (`port_forward.transport`, declared in settingsFieldMap): the server enforces
 * it and every client follows, so this panel only has to (a) render it on every
 * host including plain web, (b) hide the SSH-only rows while the draft is h2,
 * and (c) reconnect the native tunnel after a save that changed it.
 *
 * What remains host-specific is the tunnel STATUS row, which still reads the
 * port-forward composable's shared state (see the last describe blocks).
 */

// ── Mocks ──

const mockInitSnapshot = vi.fn()
const mockHandleSave = vi.fn().mockResolvedValue({
  needsRestart: false,
  changedColdFields: [],
  changedKeys: [],
  failed: false,
})
const localValues = reactive<Record<string, unknown>>({})
// Plain object, not a ref: it is created in a hoisted factory and only ever
// read through `.value` by the component's computeds.
const mockHasChanges = vi.hoisted(() => ({ value: false }))

vi.mock('@/composables/usePanelSnapshot', () => ({
  usePanelSnapshot: () => ({
    localValues,
    saving: ref(false),
    serverError: ref(''),
    hotReloadWarning: ref(''),
    hasFailedSave: ref(false),
    hasChanges: mockHasChanges,
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
const mockAppMode = vi.hoisted(() => ({
  isAppMode: { value: false }, isDesktopApp: { value: false }, isHarmonyApp: { value: false },
}))
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockAppMode.isAppMode, isDesktopApp: mockAppMode.isDesktopApp }),
}))

// The component reads the host axis through usePlatformDetect, NOT useAppMode.
// The real composable copies useAppMode into module-level refs behind a
// `platformInitialized` guard, so it would snapshot whatever the FIRST test in
// this file set and ignore every later change — which is exactly the
// order-dependent failure this mock prevents. Deriving the axes per call from
// mockAppMode keeps the suite's platform setup meaningful for every test.
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({
    isElectron: { value: mockAppMode.isDesktopApp.value },
    isAndroidApp: { value: mockAppMode.isAppMode.value && !mockAppMode.isDesktopApp.value && !mockAppMode.isHarmonyApp.value },
    isHarmonyApp: mockAppMode.isHarmonyApp,
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
  const sshInfo = ref<{ enabled: boolean; port?: number } | null>(null)
  const transportAllowsH2 = ref(false)
  const loadSSHInfo = vi.fn().mockResolvedValue(undefined)
  // The status row gates its verdict on there being something to carry: with
  // zero enabled mappings the native service is stopped on purpose, so
  // "disconnected" describes an idle state rather than a failure.
  const ports = ref<Array<{ localPort: number; port: number; enabled: boolean }>>([])
  // Link probe: the temporary-mapping test the row offers while there is
  // nothing to carry. The mock always succeeds; the real flow is pinned in
  // usePortForward.test.ts.
  const probingTunnel = ref(false)
  const probeTunnelLink = vi.fn().mockResolvedValue('ok')
  const transportAnnotation = vi.fn().mockReturnValue('（SSH）')
  Object.assign(mockTunnel, {
    tunnelStatus, tunnelChecking, tunnelMessage, checkTunnelHealth,
    sshInfo, transportAllowsH2, loadSSHInfo, ports,
    probingTunnel, probeTunnelLink, transportAnnotation,
  })
  return {
    usePortForward: () => ({
      tunnelStatus, tunnelChecking, tunnelMessage, checkTunnelHealth,
      sshInfo, transportAllowsH2, loadSSHInfo, ports,
      probingTunnel, probeTunnelLink, transportAnnotation,
    }),
  }
})

const mockReconnectTunnel = vi.hoisted(() => ({ fn: vi.fn() }))
vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => undefined,
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
          portForwardPort: 'SSH 隧道端口',
          portForwardPortAuto: '自动',
          portForwardPortAutoResolved: '自动（{port}）',
          portForwardTransport: '隧道传输方式',
          portForwardTransportDesc: '服务端决定用哪条通道',
          tunnelStatusConnected: '隧道已连接',
          tunnelStatusServerReady: '服务器已就绪，可用 App 或 ssh 建立隧道',
          tunnelStatusServerReadyH2: '服务器已就绪，端口映射走 H2（主端口）',
          tunnelStatusUnavailable: '端口映射不可用：SSH 隧道服务器已关闭，且当前传输方式不支持 H2',
          tunnelStatusChecking: '正在检测隧道状态…',
          tunnelStatusUnknown: '隧道状态未知',
          tunnelStatusDraftPending: '传输方式已修改，保存后自动重连生效',
          tunnelStatusNoPorts: '暂无转发端口，添加端口后隧道会自动建立',
        },
      },
      proxy: { transportSsh: 'SSH', transportH2: 'HTTP/2', retryCheck: '重新检测', probeLink: '测试链路', probeLinkBusy: '测试中…', probeLinkAndSave: '保存并测试' },
      portForward: {
        tunnelProbeOk: '链路可用{transport}',
        tunnelProbeFailed: '链路不可用{transport}',
      },
    },
  },
})

// ── Helpers ──

function makePortForwardConfig(): GroupPanelConfig {
  return {
    panelId: 'portForward',
    // Mirrors settingsFieldMap: the transport is a normal server field, and the
    // port row is the SSH-only one.
    commonFields: [
      {
        labelKey: 'settings.items.portForwardTransport',
        key: 'port_forward.transport',
        type: 'select',
        source: 'server',
        options: [
          { labelKey: 'proxy.transportH2', value: 'h2' },
          { labelKey: 'proxy.transportSsh', value: 'ssh' },
        ],
      },
      {
        labelKey: 'settings.items.portForwardPort',
        key: 'port_forward.port',
        type: 'number',
        source: 'server',
        // Mirrors the real field spec: 0 renders as the localized "auto"
        // sentinel, never a raw 0. The panel layers the resolved port on top.
        displayTransform: (v: unknown) => v === 0 ? i18n.global.t('settings.items.portForwardPortAuto') : v,
      },
    ],
  }
}

let mountedPanels: ReturnType<typeof mount>[] = []

function mountPanel(config: GroupPanelConfig = makePortForwardConfig()) {
  const wrapper = mount(SettingsGroupPanel, {
    props: { config, showTitle: false },
    global: {
      plugins: [i18n],
      stubs: {
        SettingsItem: {
          name: 'SettingsItem',
          props: ['label', 'description', 'type', 'modelValue', 'options', 'min', 'max', 'step', 'needsRestart', 'disabled', 'forceClose', 'defaultValue', 'displayFormat', 'displayTransform', 'noDivider', 'progress', 'refreshable', 'refreshing'],
          template: '<div class="mock-settings-item" :data-key="label" :data-type="type" :data-value="modelValue" :data-display="displayTransform ? displayTransform(modelValue) : modelValue"><button v-if="type === \'switch\'" class="mock-switch" @click="$emit(\'update:modelValue\', !modelValue)">toggle</button></div>',
          emits: ['update:modelValue', 'editToggle', 'descToggle', 'click', 'refresh'],
        },
        BottomSheet: { template: '<div><slot /></div>' },
        ChevronRight: { template: '<span>></span>' },
        RefreshCw: { template: '<span>↻</span>' },
      },
    },
  })
  mountedPanels.push(wrapper)
  return wrapper
}

type Wrapper = ReturnType<typeof mountPanel>

/** The transport select — the only select this panel renders. */
function findTransportSelect(wrapper: Wrapper) {
  return wrapper.findAllComponents({ name: 'SettingsItem' }).find(i => i.props('type') === 'select')
}

/** A field row by its i18n label, so a reorder cannot silently retarget it. */
function findField(wrapper: Wrapper, label: string) {
  return wrapper.findAllComponents({ name: 'SettingsItem' }).find(i => i.props('label') === label)
}

function saveButton(wrapper: Wrapper) {
  return wrapper.find('.group-panel__save-btn')
}

async function selectTransport(wrapper: Wrapper, value: 'ssh' | 'h2') {
  await findTransportSelect(wrapper)!.vm.$emit('update:modelValue', value)
  await nextTick()
}

beforeEach(() => {
  vi.clearAllMocks()
  mockHandleSave.mockResolvedValue({ needsRestart: false, changedColdFields: [], changedKeys: [], failed: false })
  for (const key of Object.keys(localValues)) delete localValues[key]
  localValues['port_forward.enabled'] = true
  localValues['port_forward.port'] = 0
  localValues['port_forward.transport'] = 'h2'
  mockHasChanges.value = false
  mockAppMode.isAppMode.value = false
  mockAppMode.isDesktopApp.value = false
  mockAppMode.isHarmonyApp.value = false
  mockReconnectTunnel.fn.mockReset()
  mockReconnectTunnel.fn.mockResolvedValue(true)
  ;(mockTunnel.tunnelStatus as { value: string }).value = 'unknown'
  ;(mockTunnel.tunnelChecking as { value: boolean }).value = false
  ;(mockTunnel.probingTunnel as { value: boolean }).value = false
  ;(mockTunnel.tunnelMessage as { value: string }).value = ''
  ;(mockTunnel.checkTunnelHealth as ReturnType<typeof vi.fn>).mockClear()
  ;(mockTunnel.sshInfo as { value: unknown }).value = null
  ;(mockTunnel.transportAllowsH2 as { value: boolean }).value = false
  ;(mockTunnel.loadSSHInfo as ReturnType<typeof vi.fn>).mockClear()
  // One enabled mapping by default: the status row only reports a tunnel
  // verdict when there is something for the tunnel to carry (see the idle-state
  // cases below, which empty this list on purpose).
  ;(mockTunnel.ports as { value: unknown }).value = [
    { localPort: 3000, port: 3000, enabled: true },
  ]
})

afterEach(() => {
  mountedPanels.forEach(wrapper => wrapper.unmount())
  mountedPanels = []
  vi.restoreAllMocks()
})

// ── The transport is a server field, visible everywhere ──

describe('transport field: platform coverage', () => {
  it('renders in plain web mode — this is what the server switch fixed', async () => {
    // It used to be a host-local preference, so the browser had no row at all
    // (and no native bridge to read one from).
    const wrapper = mountPanel()
    await flushPromises()
    expect(findTransportSelect(wrapper)).toBeTruthy()
  })

  it('renders on the Android shell and the Electron shell too', async () => {
    mockAppMode.isAppMode.value = true
    const android = mountPanel()
    await flushPromises()
    expect(findTransportSelect(android)).toBeTruthy()

    mockAppMode.isDesktopApp.value = true
    const desktop = mountPanel()
    await flushPromises()
    expect(findTransportSelect(desktop)).toBeTruthy()
  })

  it('offers exactly ssh and h2 — the retired "both" is not a choice', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const options = findTransportSelect(wrapper)!.props('options') as Array<{ value: string }>
    expect(options.map(o => o.value).sort()).toEqual(['h2', 'ssh'])
  })

  it('does not render on a different panel', async () => {
    const wrapper = mountPanel({ panelId: 'terminal', commonFields: [] })
    await flushPromises()
    expect(findTransportSelect(wrapper)).toBeUndefined()
  })
})

// ── Draft hides the SSH-only rows ──

describe('h2 hides the SSH-only rows', () => {
  it('hides the port row while the draft is h2, in web mode too', async () => {
    // The server does not open the SSH listener at all with transport h2, so the
    // port configures nothing. The draft (not the saved value) drives this, so
    // the row reacts as the user edits.
    const wrapper = mountPanel()
    await flushPromises()
    expect(findField(wrapper, 'SSH 隧道端口')).toBeUndefined()
  })

  it('keeps the port row while the draft is ssh', async () => {
    localValues['port_forward.transport'] = 'ssh'
    const wrapper = mountPanel()
    await flushPromises()
    expect(findField(wrapper, 'SSH 隧道端口')).toBeTruthy()
  })

  it('shows and hides the port row as the draft changes, before saving', async () => {
    localValues['port_forward.transport'] = 'ssh'
    const wrapper = mountPanel()
    await flushPromises()
    expect(findField(wrapper, 'SSH 隧道端口')).toBeTruthy()

    await selectTransport(wrapper, 'h2')
    expect(findField(wrapper, 'SSH 隧道端口')).toBeUndefined()

    await selectTransport(wrapper, 'ssh')
    expect(findField(wrapper, 'SSH 隧道端口')).toBeTruthy()
  })

  it('writes the selection into the panel draft (the PATCH source)', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await selectTransport(wrapper, 'ssh')
    // usePanelSnapshot turns this into { port_forward: { transport: 'ssh' } }.
    expect(localValues['port_forward.transport']).toBe('ssh')
  })
})

// ── Save: reconnect the native tunnel only when the transport changed ──

describe('save after a transport change', () => {
  it('reconnects and re-checks the verdict when the transport changed', async () => {
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    mockHandleSave.mockResolvedValue({
      needsRestart: false, changedColdFields: [], changedKeys: ['port_forward.transport'], failed: false,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockHandleSave).toHaveBeenCalledOnce()
    expect(mockReconnectTunnel.fn).toHaveBeenCalledOnce()
    // The verdict on screen names the OLD wire until it is recomputed.
    expect(mockTunnel.checkTunnelHealth).toHaveBeenCalledWith(true)
    expect(mockToastShow).toHaveBeenCalledWith('配置已保存', expect.anything())
  })

  it('does NOT reconnect when only another field changed', async () => {
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    mockHandleSave.mockResolvedValue({
      needsRestart: false, changedColdFields: [], changedKeys: ['port_forward.port'], failed: false,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
  })

  it('does not reconnect in web mode — there is no client tunnel', async () => {
    mockHasChanges.value = true
    mockHandleSave.mockResolvedValue({
      needsRestart: false, changedColdFields: [], changedKeys: ['port_forward.transport'], failed: false,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
    expect(mockToastShow).toHaveBeenCalledWith('配置已保存', expect.anything())
  })

  it('skips the reconnect when there is nothing forwarded (the service is idle)', async () => {
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    ;(mockTunnel.ports as { value: unknown }).value = []
    mockHandleSave.mockResolvedValue({
      needsRestart: false, changedColdFields: [], changedKeys: ['port_forward.transport'], failed: false,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
    expect(mockToastShow).toHaveBeenCalledWith('配置已保存', expect.anything())
  })

  it('reports a failed reconnect instead of claiming the save worked', async () => {
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    mockReconnectTunnel.fn.mockRejectedValue(new Error('boom'))
    mockHandleSave.mockResolvedValue({
      needsRestart: false, changedColdFields: [], changedKeys: ['port_forward.transport'], failed: false,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockToastShow).toHaveBeenCalledWith(
      '传输方式已保存，但隧道重连失败，端口映射暂不可用',
      expect.objectContaining({ type: 'error' }),
    )
    expect(mockToastShow).not.toHaveBeenCalledWith('配置已保存', expect.anything())
  })

  it('does nothing else when the PATCH itself failed', async () => {
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    mockHandleSave.mockResolvedValue({
      needsRestart: false, changedColdFields: [], changedKeys: ['port_forward.transport'], failed: true,
    })

    const wrapper = mountPanel()
    await flushPromises()
    await saveButton(wrapper).trigger('click')
    await flushPromises()

    expect(mockReconnectTunnel.fn).not.toHaveBeenCalled()
    expect(mockToastShow).not.toHaveBeenCalled()
  })
})

// ── Tunnel status row ──

describe('tunnel status row', () => {
  it('renders in plain web mode — it replaces the removed test button', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').exists()).toBe(true)
  })

  it('renders on native hosts too', async () => {
    mockAppMode.isAppMode.value = true
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').exists()).toBe(true)
  })

  it('stays out on a panel that is not portForward', async () => {
    const wrapper = mountPanel({ panelId: 'terminal', commonFields: [] })
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').exists()).toBe(false)
  })

  it('reports web mode ready over H2 when the SSH listener is off', async () => {
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: false }
    ;(mockTunnel.transportAllowsH2 as { value: boolean }).value = true
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('端口映射走 H2')
  })

  it('reports web mode ready over SSH when the listener is up', async () => {
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: true, port: 20001 }
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('服务器已就绪')
  })

  it('reports web mode unavailable when neither wire can carry forwards', async () => {
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: false }
    ;(mockTunnel.transportAllowsH2 as { value: boolean }).value = false
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('端口映射不可用')
  })

  it('reports a connected tunnel', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'ok'
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('隧道已连接')
  })

  it('surfaces the composable message for a problem state', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'disconnected'
    ;(mockTunnel.tunnelMessage as { value: string }).value = '隧道未连接（SSH）'
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('隧道未连接')
  })

  it('shows a neutral idle state instead of the disconnected verdict when nothing is forwarded', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'disconnected'
    ;(mockTunnel.ports as { value: unknown }).value = []
    const wrapper = mountPanel()
    await flushPromises()
    const text = wrapper.find('.group-panel__tunnel-status').text()
    expect(text).toContain('暂无转发端口')
    expect(text).not.toContain('未连接')
  })

  it('shows a pending hint while the panel has unsaved edits', async () => {
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'ok'
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('保存后自动重连生效')
  })

  it('offers the link probe instead of a plain recheck when nothing is forwarded', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.ports as { value: unknown }).value = []
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-retry').text()).toBe('测试链路')
  })

  it('applies pending edits before probing them', async () => {
    // Probing first would report a verdict for the wire the user is no longer
    // looking at.
    mockAppMode.isAppMode.value = true
    mockHasChanges.value = true
    ;(mockTunnel.ports as { value: unknown }).value = []

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('.group-panel__tunnel-retry').trigger('click')
    await flushPromises()

    expect(mockHandleSave).toHaveBeenCalledOnce()
    expect(mockTunnel.probeTunnelLink).toHaveBeenCalledOnce()
    expect(mockToastShow).toHaveBeenCalledWith('链路可用（SSH）', expect.anything())
  })

  it('falls back to the plain check when the host cannot probe', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.ports as { value: unknown }).value = []
    ;(mockTunnel.probeTunnelLink as ReturnType<typeof vi.fn>).mockResolvedValueOnce('unsupported')

    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('.group-panel__tunnel-retry').trigger('click')
    await flushPromises()

    expect(mockTunnel.checkTunnelHealth).toHaveBeenCalled()
    expect(mockToastShow).not.toHaveBeenCalled()
  })

  it('shows the checking state while a check is in flight', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.ports as { value: unknown }).value = []
    ;(mockTunnel.tunnelChecking as { value: boolean }).value = true
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-status').text()).toContain('正在检测')
  })

  it('re-runs the health check from the retry button', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.tunnelStatus as { value: string }).value = 'ok'
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('.group-panel__tunnel-retry').trigger('click')
    await flushPromises()
    expect(mockTunnel.checkTunnelHealth).toHaveBeenCalled()
  })

  it('disables the retry button while a check is running', async () => {
    mockAppMode.isAppMode.value = true
    ;(mockTunnel.tunnelChecking as { value: boolean }).value = true
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('.group-panel__tunnel-retry').attributes('disabled')).toBeDefined()
  })
})

// ── Auto port display (unchanged by the transport move) ──

describe('port-forward panel: auto port display', () => {
  it('shows 自动（<resolved>）when auto and the server reports a port', async () => {
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: true, port: 20001 }
    localValues['port_forward.transport'] = 'ssh'
    const wrapper = mountPanel()
    await flushPromises()
    expect(findField(wrapper, 'SSH 隧道端口')!.attributes('data-display')).toBe('自动（20001）')
  })

  it('degrades to plain 自动 before the probe resolves', async () => {
    localValues['port_forward.transport'] = 'ssh'
    const wrapper = mountPanel()
    await flushPromises()
    expect(findField(wrapper, 'SSH 隧道端口')!.attributes('data-display')).toBe('自动')
  })

  it('degrades to plain 自动 when SSH is disabled', async () => {
    ;(mockTunnel.sshInfo as { value: unknown }).value = { enabled: false, port: 20001 }
    localValues['port_forward.transport'] = 'ssh'
    const wrapper = mountPanel()
    await flushPromises()
    expect(findField(wrapper, 'SSH 隧道端口')!.attributes('data-display')).toBe('自动')
  })
})
