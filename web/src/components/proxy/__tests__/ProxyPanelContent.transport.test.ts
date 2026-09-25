import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

/**
 * The transport row in the port-forwarding panel.
 *
 * The h2 stream tunnel and the legacy SSH tunnel are indistinguishable from the
 * UI's point of view — same ports, same URLs — so the panel surfaces which wire
 * is actually carrying them. The value comes from the native bridge and is
 * therefore optional: a host that predates the transport methods (older
 * Android/Electron builds) must render no row at all rather than a placeholder
 * or a wrong label.
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
      },
    },
  },
})

// Mutable per test: app mode gates the transport row (web mode has no tunnel).
const isAppMode = ref(true)
const activeTransport = ref('')

vi.mock('@/composables/usePortForward.ts', () => ({
  usePortForward: () => ({
    ports: ref([]),
    detectedPorts: ref([]),
    loading: ref(false),
    isAppMode,
    sshInfo: ref(null),
    tunnelStatus: ref('unknown'),
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

describe('ProxyPanelContent transport row', () => {
  beforeEach(() => {
    isAppMode.value = true
    activeTransport.value = ''
  })

  it('labels the SSH transport', () => {
    activeTransport.value = 'ssh'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-transport').exists()).toBe(true)
    expect(wrapper.find('.tunnel-transport-key').text()).toBe('传输方式')
    expect(wrapper.find('.tunnel-transport-value').text()).toBe('SSH')
  })

  it('labels the HTTP/2 transport', () => {
    activeTransport.value = 'h2'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-transport-value').text()).toBe('HTTP/2')
  })

  it('labels an unresolved both-preference as auto', () => {
    // 'both' is a preference, not a wire: before anything connects the native
    // layer can only report the preference, which reads as "auto".
    activeTransport.value = 'both'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-transport-value').text()).toBe('自动')
  })

  it('hides the row when the host cannot report a transport', () => {
    // Android / older Electron: both optional bridge methods are absent, so
    // activeTransport stays ''. A placeholder here would be a lie.
    activeTransport.value = ''
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-transport').exists()).toBe(false)
  })

  it('hides the row in web mode', () => {
    // No local tunnel exists in a plain browser, so there is nothing to label.
    isAppMode.value = false
    activeTransport.value = 'h2'
    const wrapper = mountPanel()

    expect(wrapper.find('.tunnel-transport').exists()).toBe(false)
  })
})
