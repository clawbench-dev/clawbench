import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

/**
 * The "SSH not enabled" banner in the port-forwarding panel.
 *
 * The banner must key off whether port forwarding is available at all, not off
 * whether the SSH listener is up. An h2-only install (operator disabled
 * `port_forward.enabled` and carries the same forwards over the HTTP/2 stream
 * tunnel) forwards ports perfectly well, so telling its users to "enable SSH"
 * is a lie. It shows only when neither wire can carry the traffic.
 *
 * The h2 availability input is the panel's `transportAllowsH2` from
 * usePortForward() — the same server-config read the composable's health gate
 * uses (`tunnelTransportAllowsH2()`), so the banner and the health check agree.
 */

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { loading: '加载中', cancel: '取消', confirm: '确定' },
      proxy: {
        title: '端口映射',
        noPorts: '暂无端口',
        emptyHint: '提示',
        addPort: '添加端口',
        editPort: '编辑端口',
        protocolLabel: '协议',
        directionLabel: '方向',
        directionForward: '服务器 → 本地',
        directionReverse: '本地 → 服务器',
        directionForwardHint: '在本机访问服务器上的服务（ssh -L）',
        directionReverseHint: '把本机服务暴露到服务器（ssh -R）',
        portLabelForward: '服务器端口',
        portLabelReverse: '本机端口',
        hostLabelForward: '目标主机（可选）',
        hostLabelReverse: '本机目标主机（可选）',
        namePlaceholder: '名称（可选）',
        scanTitle: '端口扫描',
        rescan: '重新扫描',
        scanInProgress: '正在扫描...',
        scanNoResults: '未检测到可转发的端口',
        scanFailed: '扫描失败',
        scanCount: '扫描到 {count} 个端口',
        copyServerAddress: '复制服务器侧地址',
        transportLabel: '传输方式',
        transportSsh: 'SSH',
        transportH2: 'HTTP/2',
        transportAuto: '自动',
        // Generic wording: the banner is about port forwarding being
        // unavailable, not about SSH specifically.
        tunnelNoSsh: '端口映射未启用，请在服务端 config.yaml 中配置 port_forward.enabled: true',
      },
    },
  },
})

// Mutable per test: web mode gates the banner, and these two inputs decide it.
const isAppMode = ref(false)
const sshInfo = ref(null)
const transportAllowsH2 = ref(false)

vi.mock('@/composables/usePortForward.ts', () => ({
  usePortForward: () => ({
    ports: ref([]),
    detectedPorts: ref([]),
    loading: ref(false),
    isAppMode,
    sshInfo,
    tunnelStatus: ref('unknown'),
    tunnelChecking: ref(false),
    tunnelError: ref(''),
    tunnelErrorType: ref(''),
    activeTransport: ref(''),
    transportAllowsH2,
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

vi.mock('@/composables/useTabDrawer.ts', () => ({
  useTabDrawer: () => ({ effectiveOpen: ref(false), open: vi.fn(), close: vi.fn() }),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }),
}))

vi.mock('@/composables/usePlatformDetect.ts', () => ({
  isWindowsUA: false,
  isMacDesktopUA: false,
  isLinuxDesktopUA: false,
}))

vi.mock('@/utils/portForwardUtils.ts', async () => {
  // Keep the REAL portForwardUnavailable: the panel's banner gate now uses it,
  // and the banner tests must exercise the shipped predicate rather than a
  // stub that could silently disagree with it.
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

/** The banner is the only `.tunnel-banner.warning` in web mode. */
function noSshBanner(wrapper: ReturnType<typeof mountPanel>) {
  return wrapper.find('.tunnel-banner.warning')
}

describe('ProxyPanelContent SSH-not-enabled banner', () => {
  beforeEach(() => {
    isAppMode.value = false
    sshInfo.value = { enabled: false }
    transportAllowsH2.value = false
  })

  it('shows the banner when neither SSH nor h2 can carry the tunnel', () => {
    const wrapper = mountPanel()

    expect(noSshBanner(wrapper).exists()).toBe(true)
    expect(noSshBanner(wrapper).text()).toContain('端口映射未启用')
  })

  it('hides the banner when h2 carries the tunnel despite SSH being off', () => {
    // h2-only install: no SSH listener, but port forwarding works over the
    // stream tunnel. Telling the user to enable SSH here is the bug.
    sshInfo.value = { enabled: false }
    transportAllowsH2.value = true
    const wrapper = mountPanel()

    expect(noSshBanner(wrapper).exists()).toBe(false)
  })

  it('hides the banner when SSH is enabled', () => {
    sshInfo.value = { enabled: true }
    transportAllowsH2.value = false
    const wrapper = mountPanel()

    expect(noSshBanner(wrapper).exists()).toBe(false)
  })

  it('hides the banner in app mode even when neither wire is available', () => {
    // The banner is web-only: the app's own tunnel status banners own that
    // surface (and a native host decides its own transport).
    isAppMode.value = true
    const wrapper = mountPanel()

    expect(noSshBanner(wrapper).exists()).toBe(false)
  })

  it('hides the banner when sshInfo has not loaded yet', () => {
    // Unknown SSH state is not "SSH disabled" — don't accuse the operator.
    sshInfo.value = null
    const wrapper = mountPanel()

    expect(noSshBanner(wrapper).exists()).toBe(false)
  })
})
