import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

/**
 * Focused coverage for the port-scan drawer's three terminal states.
 *
 * Regression: a failed scan (request aborted at the API timeout on a busy
 * host) rendered exactly the same "no mappable ports" empty state as a scan
 * that genuinely found nothing, so the failure was invisible and the user had
 * no retry affordance.
 */

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      proxy: {
        scanTitle: '端口扫描',
        rescan: '重新扫描',
        scanInProgress: '正在扫描服务器上的端口...',
        scanNoResults: '未检测到可转发的端口',
        scanFailed: '扫描失败——无法连接到服务端',
        scanCount: '扫描到 {count} 个端口',
      },
    },
  },
})

// Mutable scan state the mocked composable reads from, so each test can put the
// drawer into a specific terminal state.
const scanState = {
  detectedPorts: ref<any[]>([]),
  scanning: ref(false),
  scanError: ref(''),
}

// Shared spy so the retry test can assert on the exact function the component
// was handed (a fresh vi.fn() per usePortForward() call would not be reachable).
const mockRescanPorts = vi.fn()

vi.mock('@/composables/usePortForward.ts', () => ({
  usePortForward: () => ({
    ports: ref([]),
    detectedPorts: scanState.detectedPorts,
    loading: ref(false),
    isAppMode: ref(false),
    sshInfo: ref(null),
    tunnelStatus: ref('unknown'),
    tunnelChecking: ref(false),
    tunnelError: ref(''),
    tunnelErrorType: ref(''),
    connectingPorts: ref(new Set()),
    localReachable: ref(new Map()),
    scanning: scanState.scanning,
    hasScanned: ref(true),
    scanError: scanState.scanError,
    registerPort: vi.fn(),
    updatePort: vi.fn(),
    unregisterPort: vi.fn(),
    setPortEnabled: vi.fn(),
    detectPorts: vi.fn(),
    rescanPorts: mockRescanPorts,
    checkTunnelHealth: vi.fn(),
    openPortWithCheck: vi.fn(),
    openInExternalBrowser: vi.fn(),
    reconnectPort: vi.fn(),
  }),
}))

vi.mock('@/composables/useTabDrawer.ts', () => ({
  useTabDrawer: () => ({ effectiveOpen: ref(true), open: vi.fn(), close: vi.fn() }),
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

describe('ProxyPanelContent port scan states', () => {
  beforeEach(() => {
    scanState.detectedPorts.value = []
    scanState.scanning.value = false
    scanState.scanError.value = ''
  })

  it('shows the empty state when the scan succeeded with no ports', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.port-scan-empty').exists()).toBe(true)
    expect(wrapper.find('.port-scan-error').exists()).toBe(false)
    expect(wrapper.text()).toContain('未检测到可转发的端口')
  })

  it('shows the failure state (not the empty state) when the scan errored', () => {
    scanState.scanError.value = 'Request timed out'
    const wrapper = mountPanel()

    expect(wrapper.find('.port-scan-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('扫描失败——无法连接到服务端')
    // The whole point of the fix: the failure must NOT be reported as "no ports".
    expect(wrapper.text()).not.toContain('未检测到可转发的端口')
  })

  it('offers a retry that re-runs the scan from the failure state', async () => {
    scanState.scanError.value = 'Request timed out'
    const wrapper = mountPanel()

    const retry = wrapper.find('.port-scan-retry')
    expect(retry.exists()).toBe(true)
    await retry.trigger('click')

    expect(mockRescanPorts).toHaveBeenCalled()
  })

  it('shows results and no error block when ports were found', () => {
    scanState.detectedPorts.value = [
      { port: 8080, protocol: 'http', processName: 'node', processArgs: '' },
    ]
    const wrapper = mountPanel()

    expect(wrapper.find('.port-scan-results').exists()).toBe(true)
    expect(wrapper.find('.port-scan-error').exists()).toBe(false)
    expect(wrapper.find('.port-scan-empty').exists()).toBe(false)
  })

  it('prefers the loading state while a rescan is in flight', () => {
    scanState.scanning.value = true
    scanState.scanError.value = 'stale error from the previous attempt'
    const wrapper = mountPanel()

    expect(wrapper.find('.port-scan-loading').exists()).toBe(true)
    expect(wrapper.find('.port-scan-error').exists()).toBe(false)
  })
})
