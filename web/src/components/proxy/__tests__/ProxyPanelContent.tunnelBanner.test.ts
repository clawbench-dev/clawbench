import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

/**
 * Host-aware tunnel banners.
 *
 * Both native hosts report `isAppMode === true` (it is just `isNativeApp()`),
 * so the port-forward panel used to lump the Electron desktop shell in with the
 * Android WebView: it showed the Android-only background-permission tip
 * ("otherwise the SSH tunnel is killed when the app goes to background"), which
 * does not apply to a desktop window.
 */

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { loading: '加载中', cancel: '取消', confirm: '确定', edit: '编辑', delete: '删除' },
      proxy: {
        title: '端口映射',
        noPorts: '暂无端口',
        emptyHint: '提示',
        addPort: '添加端口',
        scanTitle: '端口扫描',
        backgroundTip: '后台权限提示（Android 专属）',
        appRecommendation: '建议使用 ClawBench APP',
        tunnelGuide: '手动 SSH 隧道',
        tunnelDisconnected: 'SSH 隧道未连接',
        portsNoResponse: '转发端口无响应',
        retryCheck: '重新检测',
      },
    },
  },
})

// Shared reactive state the mocks read from, so each case can put the panel into
// a specific host + tunnel state.
const host = {
  isAppMode: ref(false),
  isDesktopApp: ref(false),
}
const tunnel = {
  sshInfo: ref<any>(null),
  tunnelStatus: ref<string>('unknown'),
}

vi.mock('@/composables/usePortForward.ts', () => ({
  usePortForward: () => ({
    ports: ref([]),
    detectedPorts: ref([]),
    loading: ref(false),
    // The component reads isAppMode from here (and isDesktopApp from
    // useAppMode); keep both pointing at the same refs.
    isAppMode: host.isAppMode,
    sshInfo: tunnel.sshInfo,
    tunnelStatus: tunnel.tunnelStatus,
    tunnelChecking: ref(false),
    tunnelError: ref(''),
    tunnelErrorType: ref(''),
    // Transport state the panel reads for its status annotations and the
    // h2-only availability clause in the SSH-disabled banner. Empty/false =
    // no h2 capability, so the banner keeps its original meaning.
    activeTransport: ref(''),
    transportAllowsH2: ref(false),
    transportAnnotation: vi.fn(() => ''),
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
    openPortWithCheck: vi.fn(),
    openInExternalBrowser: vi.fn(),
    copyServerAddress: vi.fn(),
    reconnectPort: vi.fn(),
  }),
}))

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: host.isAppMode, isDesktopApp: host.isDesktopApp }),
}))

vi.mock('@/composables/useTabDrawer.ts', () => ({
  useTabDrawer: () => ({ effectiveOpen: ref(true), open: vi.fn(), close: vi.fn() }),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }),
}))

// The component reads the host axes through usePlatformDetect, not useAppMode.
// Deriving them from `host` per call keeps each case's host setup meaningful
// (the real composable snapshots the first call behind a guard). Real refs, not
// bare `{ value }` objects: the template binds them directly, and a plain object
// would always be truthy.
vi.mock('@/composables/usePlatformDetect.ts', async () => {
  const { ref } = await import('vue')
  return {
    usePlatformDetect: () => ({
      isElectron: ref(host.isDesktopApp.value),
      isAndroidApp: ref(host.isAppMode.value && !host.isDesktopApp.value),
      isWebApp: ref(!host.isAppMode.value),
      isNativeApp: ref(host.isAppMode.value),
      isTouchPrimary: ref(false),
    }),
    isWindowsUA: false,
    isMacDesktopUA: false,
    isLinuxDesktopUA: false,
  }
})

vi.mock('@/utils/portForwardUtils.ts', () => ({
  sshInstallHint: () => null,
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, template: `<span class="${name}" />` })
  return {
    XCircle: stub('i-xcircle'),
    AlertTriangle: stub('i-alert'),
    Info: stub('i-info'),
    Plus: stub('i-plus'),
    Search: stub('i-search'),
    Lock: stub('i-lock'),
    Copy: stub('i-copy'),
    Smartphone: stub('i-phone'),
    ChevronDown: stub('i-chevron'),
    Network: stub('i-network'),
    Server: stub('i-server'),
    CircleAlert: stub('i-circle-alert'),
  }
})

import ProxyPanelContent from '@/components/proxy/ProxyPanelContent.vue'

function mountPanel() {
  return mount(ProxyPanelContent, {
    props: { show: true },
    global: {
      plugins: [i18n],
      stubs: {
        ProxyPortItem: true,
        ModalDialog: { template: '<div><slot name="header" /><slot /></div>' },
        BottomSheet: { template: '<div class="bs"><slot name="header" /><slot /></div>' },
        LoadingIndicator: true,
        RefreshButton: { template: '<button class="rb" />' },
      },
    },
  })
}

function setHost(appMode: boolean, desktopApp: boolean) {
  host.isAppMode.value = appMode
  host.isDesktopApp.value = desktopApp
}

describe('ProxyPanelContent tunnel banners by host', () => {
  beforeEach(() => {
    setHost(false, false)
    tunnel.sshInfo.value = { enabled: true, command: 'ssh -L ...' }
    tunnel.tunnelStatus.value = 'ok'
  })

  it('shows the Android background tip in the Android WebView shell', () => {
    setHost(true, false)
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('后台权限提示（Android 专属）')
  })

  it('does NOT show the Android background tip in the Electron desktop shell', () => {
    // The tip is about the OS killing a backgrounded Android app — meaningless
    // for a desktop window, which keeps running while minimized.
    setHost(true, true)
    const wrapper = mountPanel()
    expect(wrapper.text()).not.toContain('后台权限提示（Android 专属）')
  })
})
