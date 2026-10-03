import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, nextTick } from 'vue'

/**
 * Coverage for the two refresh affordances on the port-forward panel:
 *   1. the header refresh button (manual),
 *   2. the automatic refresh when the panel becomes the visible tab.
 *
 * Both route through `refreshPortForward`, so these tests assert on that exact
 * function the component was handed rather than on the underlying composable.
 */

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { loading: '加载中', refresh: '刷新' },
      nav: { portForward: '端口映射' },
      proxy: {
        scanTitle: '端口扫描',
        rescan: '重新扫描',
        scanInProgress: '正在扫描服务器上的端口...',
        scanNoResults: '未检测到可转发的端口',
        scanFailed: '扫描失败——无法连接到服务端',
        scanCount: '扫描到 {count} 个端口',
        addPort: '添加端口',
        noPorts: '暂无转发端口',
        emptyHint: '添加服务器上的端口，即可在浏览器或其他应用中直接访问',
      },
    },
  },
})

// Shared spies so assertions target the same functions the component received.
const mockRefresh = vi.fn()

vi.mock('@/composables/usePortForward.ts', () => ({
  usePortForward: () => ({
    ports: ref([]),
    detectedPorts: ref([]),
    loading: ref(false),
    refreshing: ref(false),
    isAppMode: ref(false),
    sshInfo: ref(null),
    tunnelStatus: ref('unknown'),
    tunnelChecking: ref(false),
    tunnelError: ref(''),
    tunnelErrorType: ref(''),
    activeTransport: ref(''),
    connectingPorts: ref(new Set()),
    localReachable: ref(new Map()),
    scanning: ref(false),
    hasScanned: ref(true),
    scanError: ref(''),
    registerPort: vi.fn(),
    updatePort: vi.fn(),
    unregisterPort: vi.fn(),
    setPortEnabled: vi.fn(),
    detectPorts: vi.fn(),
    rescanPorts: vi.fn(),
    checkTunnelHealth: vi.fn(),
    refreshPortForward: mockRefresh,
    openPortWithCheck: vi.fn(),
    openInExternalBrowser: vi.fn(),
    reconnectPort: vi.fn(),
  }),
}))

vi.mock('@/composables/useTabDrawer.ts', () => ({
  useTabDrawer: () => ({ effectiveOpen: ref(false), open: vi.fn(), close: vi.fn() }),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }),
}))

vi.mock('@/composables/usePlatformDetect.ts', async () => {
  const { ref } = await import('vue')
  return {
    usePlatformDetect: () => ({
      isElectron: ref(false),
      isAndroidApp: ref(false),
      isWebApp: ref(true),
      isNativeApp: ref(false),
      isTouchPrimary: ref(false),
    }),
    isWindowsUA: false,
    isMacDesktopUA: false,
    isLinuxDesktopUA: false,
  }
})

vi.mock('@/utils/portForwardUtils.ts', async () => {
  const actual = await vi.importActual<typeof import('@/utils/portForwardUtils.ts')>('@/utils/portForwardUtils.ts')
  return { sshInstallHint: () => null, portForwardUnavailable: actual.portForwardUnavailable }
})

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, template: `<span class="${name}" />` })
  return {
    XCircle: stub('i-xcircle'), AlertTriangle: stub('i-alert'), Info: stub('i-info'),
    Plus: stub('i-plus'), Search: stub('i-search'), Lock: stub('i-lock'),
    Copy: stub('i-copy'), Smartphone: stub('i-phone'), ChevronDown: stub('i-chevron'),
    Network: stub('i-network'), Server: stub('i-server'), CircleAlert: stub('i-circle-alert'),
    Settings: stub('i-settings'),
  }
})

import ProxyPanelContent from '@/components/proxy/ProxyPanelContent.vue'

function mountPanel(active = true) {
  return mount(ProxyPanelContent, {
    props: { active },
    global: {
      plugins: [i18n],
      stubs: {
        ProxyPortItem: true,
        ModalDialog: { template: '<div><slot name="header" /><slot /></div>' },
        BottomSheet: { template: '<div class="bs"><slot name="header" /><slot /></div>' },
        LoadingIndicator: true,
        // Forward the click so the header button's handler actually runs.
        RefreshButton: {
          name: 'RefreshButton',
          emits: ['click'],
          template: '<button class="rb" @click="$emit(\'click\')" />',
        },
      },
    },
  })
}

describe('ProxyPanelContent refresh', () => {
  beforeEach(() => {
    mockRefresh.mockClear()
  })

  it('refreshes when the header button is clicked', async () => {
    const wrapper = mountPanel(true)
    mockRefresh.mockClear() // drop the mount-time auto-refresh call

    const button = wrapper.find('[data-action="proxy-refresh"]')
    expect(button.exists()).toBe(true)
    await button.trigger('click')

    expect(mockRefresh).toHaveBeenCalledTimes(1)
  })

  it('auto-refreshes on mount when the panel is already active', () => {
    // Remembered-panel restore: the proxy tab is active before this component
    // is ever mounted, so an immediate watch is the only thing that fires.
    mountPanel(true)
    expect(mockRefresh).toHaveBeenCalledTimes(1)
  })

  it('does not refresh on mount when the panel is not active', () => {
    mountPanel(false)
    expect(mockRefresh).not.toHaveBeenCalled()
  })

  it('auto-refreshes when the panel becomes active', async () => {
    const wrapper = mountPanel(false)
    expect(mockRefresh).not.toHaveBeenCalled()

    await wrapper.setProps({ active: true })
    await nextTick()
    expect(mockRefresh).toHaveBeenCalledTimes(1)
  })

  it('does not re-refresh when already active and the prop is set again', async () => {
    const wrapper = mountPanel(true)
    mockRefresh.mockClear()

    await wrapper.setProps({ active: true })
    await nextTick()
    expect(mockRefresh).not.toHaveBeenCalled()
  })

  it('dispatches open-port-forward-settings from the header config button', async () => {
    // The button is the only affordance that jumps to the port-mapping settings
    // panel. It dispatches an event rather than switching tabs directly so
    // App.vue can record the proxy panel as the return origin (Back lands here).
    const wrapper = mountPanel(true)
    const button = wrapper.find('[data-action="proxy-open-settings"]')
    expect(button.exists()).toBe(true)

    const seen: CustomEvent[] = []
    const listener = (e: Event) => { seen.push(e as CustomEvent) }
    window.addEventListener('open-port-forward-settings', listener)
    try {
      await button.trigger('click')
    } finally {
      window.removeEventListener('open-port-forward-settings', listener)
    }

    expect(seen).toHaveLength(1)
  })
})
