import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

/**
 * Transport annotation on the tunnel status banners/toasts.
 *
 * The tunnel may be carried by SSH or the h2 stream tunnel, so status copy is
 * transport-neutral and gets a parenthesized annotation only when a single wire
 * is known. `''` (unknown) and `'both'` (a preference, not a wire) must render
 * the bare neutral wording — guessing would be worse than saying nothing.
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
        transportAnnotation: '（{transport}）',
        tunnelDisconnected: '隧道未连接{transport}',
        tunnelDisconnectedDetail: '端口映射将无法使用，请检查网络或重新打开页面',
        tunnelErrorAuth: 'SSH 认证失败',
        tunnelErrorNetwork: 'SSH 服务器不可达',
        tunnelErrorHostKey: 'SSH 服务器密钥已变更',
        portsNoResponse: '转发端口无响应',
        tunnelConnectedButNoResponse: '隧道已连接{transport}，但所有端口的服务均未响应',
        backgroundTip: '使用外部浏览器时，请确保已授予应用后台运行权限，否则 APP 进入后台后隧道会被系统终止{transport}',
        retryCheck: '重新检测',
        reconnectPort: '重连',
        copyCommand: '复制命令',
        toast: {
          checkFailed: '检测失败',
          tunnelRecovered: '隧道已恢复{transport}',
          tunnelConnectedNoResponse: '隧道已连接{transport}，但端口服务未响应',
          tunnelStillDisconnected: '隧道仍未连接{transport}',
          portsStillNoResponse: '端口服务仍未响应',
        },
      },
    },
  },
})

// Mutable per test: which wire is known, and the tunnel's current status.
const isAppMode = ref(true)
const activeTransport = ref('')
const tunnelStatus = ref('unknown')

// Mirrors the composable's real transportAnnotation(): resolve the label key for
// a concrete wire and wrap it via the `proxy.transportAnnotation` message, or
// return '' when no single wire is known. Kept in the mock so the component test
// exercises the template/script wiring against the real i18n messages.
function transportAnnotation(): string {
  const key = activeTransport.value === 'ssh' ? 'proxy.transportSsh'
    : activeTransport.value === 'h2' ? 'proxy.transportH2'
      : ''
  return key ? i18n.global.t('proxy.transportAnnotation', { transport: i18n.global.t(key) }) : ''
}

const mockToastShow = vi.fn()

vi.mock('@/composables/usePortForward.ts', () => ({
  usePortForward: () => ({
    ports: ref([]),
    detectedPorts: ref([]),
    loading: ref(false),
    isAppMode,
    sshInfo: ref(null),
    tunnelStatus,
    tunnelChecking: ref(false),
    tunnelError: ref(''),
    tunnelErrorType: ref(''),
    activeTransport,
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
    // The retry handler re-checks health and toasts based on the resulting
    // status; simulate a successful recovery.
    checkTunnelHealth: vi.fn().mockImplementation(async () => { tunnelStatus.value = 'ok' }),
    transportAnnotation,
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
  useToast: () => ({ show: mockToastShow, dismiss: vi.fn() }),
}))

vi.mock('@/composables/usePlatformDetect.ts', () => ({
  isWindowsUA: false,
  isMacDesktopUA: false,
  isLinuxDesktopUA: false,
}))

vi.mock('@/utils/portForwardUtils.ts', () => ({
  sshInstallHint: () => null,
}))

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

describe('ProxyPanelContent transport annotation', () => {
  beforeEach(() => {
    isAppMode.value = true
    activeTransport.value = ''
    tunnelStatus.value = 'unknown'
    mockToastShow.mockReset()
  })

  it('appends （HTTP/2） to the disconnected banner when h2 carried the tunnel', () => {
    activeTransport.value = 'h2'
    tunnelStatus.value = 'disconnected'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-banner-title').text()).toBe('隧道未连接（HTTP/2）')
  })

  it('appends （SSH） to the disconnected banner when SSH carried the tunnel', () => {
    activeTransport.value = 'ssh'
    tunnelStatus.value = 'disconnected'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-banner-title').text()).toBe('隧道未连接（SSH）')
  })

  it('renders the bare neutral wording when the transport is unknown', () => {
    // Android / older Electron: the optional bridge methods are absent, so the
    // wire cannot be determined. No annotation rather than a guess.
    activeTransport.value = ''
    tunnelStatus.value = 'disconnected'
    const wrapper = mountPanel()

    const text = wrapper.find('.tunnel-banner-title').text()
    expect(text).toBe('隧道未连接')
    expect(text).not.toContain('（')
  })

  it('renders the bare neutral wording for the both preference', () => {
    // 'both' is a preference, not a wire: the winner is not determinable.
    activeTransport.value = 'both'
    tunnelStatus.value = 'disconnected'
    const wrapper = mountPanel()

    const text = wrapper.find('.tunnel-banner-title').text()
    expect(text).toBe('隧道未连接')
    expect(text).not.toContain('（')
  })

  it('annotates the degraded banner detail with the known wire', () => {
    activeTransport.value = 'h2'
    tunnelStatus.value = 'degraded'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-banner-detail').text()).toBe('隧道已连接（HTTP/2），但所有端口的服务均未响应')
  })

  it('annotates the ok-state background tip with the known wire', () => {
    activeTransport.value = 'ssh'
    tunnelStatus.value = 'ok'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-banner-detail').text()).toContain('隧道会被系统终止（SSH）')
  })

  it('annotates the retry toast with the known wire', async () => {
    activeTransport.value = 'h2'
    tunnelStatus.value = 'disconnected'
    const wrapper = mountPanel()

    await wrapper.find('.rb').trigger('click')
    await Promise.resolve()

    expect(mockToastShow).toHaveBeenCalledWith('隧道已恢复（HTTP/2）', expect.objectContaining({ type: 'success' }))
  })

  it('leaves the retry toast unannotated when no wire is known', async () => {
    activeTransport.value = ''
    tunnelStatus.value = 'disconnected'
    const wrapper = mountPanel()

    await wrapper.find('.rb').trigger('click')
    await Promise.resolve()

    expect(mockToastShow).toHaveBeenCalledWith('隧道已恢复', expect.objectContaining({ type: 'success' }))
  })
})
