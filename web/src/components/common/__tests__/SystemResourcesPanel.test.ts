import { describe, it, expect, vi, beforeEach } from 'vitest'
import { computed, ref } from 'vue'
import { mount } from '@vue/test-utils'
import SystemResourcesPanel from '../../common/SystemResourcesPanel.vue'

const mockStartPolling = vi.fn()
const mockStopPolling = vi.fn()

const mockData = {
  cpu: { percent: 45.2, core_count: 4 },
  memory: { used: 8 * 1024 * 1024 * 1024, total: 16 * 1024 * 1024 * 1024, percent: 50.0 },
  disk: { used: 100 * 1024 * 1024 * 1024, total: 500 * 1024 * 1024 * 1024, percent: 20.0 },
  disk_io: { read_rate: 1024 * 1024 * 5, write_rate: 1024 * 1024 * 2 },
  network: { upload_rate: 1024 * 100, download_rate: 1024 * 500 },
  load: { load1: 1.5, load5: 1.2, load15: 1.0 },
  errors: null,
}

const mockResources = ref({ ...mockData })

vi.mock('@/composables/useSystemResources', () => ({
  useSystemResources: () => ({
    resources: mockResources,
    startPolling: mockStartPolling,
    stopPolling: mockStopPolling,
  }),
}))

// Narrow viewport by default: the usage row only renders where there is no
// header capsule.
const mockIsWideScreen = ref(false)

vi.mock('@/composables/useWideScreenLayout', () => ({
  useWideScreenLayout: () => ({
    isWideScreen: mockIsWideScreen,
    leftTab: ref('chat'),
    activePane: ref('chat'),
  }),
}))

const mockMeterStatus = ref({
  enabled: true,
  metric: 'cost',
  dayInCycle: 5,
  periodDays: 30,
  quota: 20,
  actual: 9.5,
  expected: 10,
  remainingDays: 25,
  projectedExhaustDay: 0,
})

vi.mock('@/composables/useUsageMeter', () => ({
  useUsageMeter: () => ({
    status: mockMeterStatus,
    meterState: computed(() => {
      const s = mockMeterStatus.value
      if (!s.enabled) return 'ok'
      if (s.quota > 0 && s.actual >= s.quota) return 'full'
      if (s.actual > s.expected) return 'over'
      return 'ok'
    }),
    fillPercent: computed(() => {
      const s = mockMeterStatus.value
      return s.quota > 0 ? Math.min(100, (s.actual / s.quota) * 100) : 0
    }),
    expectedPercent: computed(() => {
      const s = mockMeterStatus.value
      return s.quota > 0 ? Math.min(100, (s.expected / s.quota) * 100) : 0
    }),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

function mountPanel(props?: { showLogout?: boolean; wsStatus?: string }) {
  return mount(SystemResourcesPanel, {
    props,
    global: {
      stubs: {
        Cpu: true,
        Activity: true,
        MemoryStick: true,
        Database: true,
        HardDriveDownload: true,
        HardDriveUpload: true,
        CloudDownload: true,
        CloudUpload: true,
        Server: true,
        LogOut: true,
        WifiOff: true,
        LoaderCircle: true,
        Gauge: true,
        ChevronRight: true,
      },
    },
  })
}

describe('SystemResourcesPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockResources.value = { ...mockData }
    mockIsWideScreen.value = false
    mockMeterStatus.value = {
      enabled: true,
      metric: 'cost',
      dayInCycle: 5,
      periodDays: 30,
      quota: 20,
      actual: 9.5,
      expected: 10,
      remainingDays: 25,
      projectedExhaustDay: 0,
    }
  })

  it('renders load average section', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('systemResources.loadAvg')
    expect(wrapper.text()).toContain('1.50')
  })

  it('renders CPU section', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('systemResources.cpu')
    expect(wrapper.text()).toContain('45.2%')
  })

  it('renders memory section with formatted bytes', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('systemResources.memory')
    expect(wrapper.text()).toContain('8.0 GB')
    expect(wrapper.text()).toContain('16.0 GB')
  })

  it('renders disk section with formatted bytes', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('systemResources.disk')
    expect(wrapper.text()).toContain('100.0 GB')
    expect(wrapper.text()).toContain('500.0 GB')
  })

  it('renders disk I/O section', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('systemResources.diskRead')
    expect(wrapper.text()).toContain('systemResources.diskWrite')
  })

  it('renders network section', () => {
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('systemResources.upload')
    expect(wrapper.text()).toContain('systemResources.download')
  })

  it('applies correct bar class for normal percent', () => {
    const wrapper = mountPanel()
    const fills = wrapper.findAll('.progress-fill')
    // CPU at 45.2% should be bar-normal
    expect(fills[0].classes()).toContain('bar-normal')
  })

  it('applies bar-warning class for 70%+ percent', () => {
    mockResources.value = { ...mockData, memory: { ...mockData.memory, percent: 75.0 } }
    const wrapper = mountPanel()
    const fills = wrapper.findAll('.progress-fill')
    // fills order: load(0), cpu(1), memory(2), disk(3)
    // Memory at 75% should be bar-warning
    expect(fills[2].classes()).toContain('bar-warning')
  })

  it('applies bar-critical class for 90%+ percent', () => {
    mockResources.value = { ...mockData, disk: { ...mockData.disk, percent: 95.0 } }
    const wrapper = mountPanel()
    const fills = wrapper.findAll('.progress-fill')
    // Disk at 95% should be bar-critical
    expect(fills[3].classes()).toContain('bar-critical')
  })

  it('exposes startPolling and stopPolling', () => {
    const wrapper = mountPanel()
    const vm = wrapper.vm as any
    expect(typeof vm.startPolling).toBe('function')
    expect(typeof vm.stopPolling).toBe('function')
  })

  it('shows dash for negative CPU percent', () => {
    mockResources.value = { ...mockData, cpu: { ...mockData.cpu, percent: -1 } }
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('—')
  })

  it('formats zero bytes correctly', () => {
    mockResources.value = { ...mockData, memory: { ...mockData.memory, used: 0 } }
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('0 B')
  })

  it('formats zero rate correctly', () => {
    mockResources.value = { ...mockData, network: { ...mockData.network, upload_rate: 0 } }
    const wrapper = mountPanel()
    expect(wrapper.text()).toContain('0 B/s')
  })

  // ── Server info header ──

  it('renders server info header with address', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.server-info-header')).toBeTruthy()
    expect(wrapper.find('.server-info-address')).toBeTruthy()
  })

  it('does not show logout button when showLogout is false', () => {
    const wrapper = mountPanel({ showLogout: false })
    expect(wrapper.find('.logout-btn').exists()).toBe(false)
  })

  it('shows logout button when showLogout is true', () => {
    const wrapper = mountPanel({ showLogout: true })
    expect(wrapper.find('.logout-btn').exists()).toBe(true)
  })

  it('renders logout button as its own row with label below the address', () => {
    const wrapper = mountPanel({ showLogout: true })
    const btn = wrapper.find('.logout-btn')
    expect(btn.exists()).toBe(true)
    // Label is visible text, not just an icon with a tooltip
    expect(btn.text()).toContain('login.logout')
    // Full-width row: separated from the server info header row
    const panel = wrapper.find('.system-resources-panel')
    expect(panel.find('.server-info-header').exists()).toBe(true)
    expect(panel.find('.logout-btn').exists()).toBe(true)
  })

  it('emits logout event when logout button is clicked', async () => {
    const wrapper = mountPanel({ showLogout: true })
    await wrapper.find('.logout-btn').trigger('click')
    expect(wrapper.emitted('logout')).toBeTruthy()
  })

  // ── Connection status ──

  it('shows resource rows when connected', () => {
    const wrapper = mountPanel({ wsStatus: 'connected' })
    expect(wrapper.find('.resource-row').exists()).toBe(true)
    expect(wrapper.find('.connection-status').exists()).toBe(false)
  })

  it('hides resource rows and shows disconnected status when wsStatus is disconnected', () => {
    const wrapper = mountPanel({ wsStatus: 'disconnected' })
    expect(wrapper.find('.resource-row').exists()).toBe(false)
    expect(wrapper.find('.connection-status').exists()).toBe(true)
    expect(wrapper.text()).toContain('systemResources.disconnected')
    expect(wrapper.find('.status-disconnected').exists()).toBe(true)
  })

  it('hides resource rows and shows reconnecting status when wsStatus is reconnecting', () => {
    const wrapper = mountPanel({ wsStatus: 'reconnecting' })
    expect(wrapper.find('.resource-row').exists()).toBe(false)
    expect(wrapper.find('.connection-status').exists()).toBe(true)
    expect(wrapper.text()).toContain('systemResources.reconnecting')
    expect(wrapper.find('.status-reconnecting').exists()).toBe(true)
  })

  it('still shows server info header when disconnected', () => {
    const wrapper = mountPanel({ wsStatus: 'disconnected' })
    expect(wrapper.find('.server-info-header').exists()).toBe(true)
    expect(wrapper.find('.server-info-address').exists()).toBe(true)
  })

  // ── Usage meter (appended last, phones only) ──

  it('renders the usage meter as the last row on narrow screens', () => {
    const wrapper = mountPanel()
    const rows = wrapper.findAll('.resource-row')
    const last = rows[rows.length - 1]
    expect(last.classes()).toContain('usage-row')
    expect(last.text()).toContain('usageMeter.label')
    // Compact: one decimal at most, no trailing ".0" (space is the point).
    expect(last.text()).toContain('$9.5')
    expect(last.text()).toContain('$20')
  })

  it('hides the usage meter on wide screens where the header capsule shows it', () => {
    mockIsWideScreen.value = true
    const wrapper = mountPanel()
    expect(wrapper.find('.usage-row').exists()).toBe(false)
  })

  it('hides the usage meter when the rule is disabled', () => {
    mockMeterStatus.value = { ...mockMeterStatus.value, enabled: false }
    const wrapper = mountPanel()
    expect(wrapper.find('.usage-row').exists()).toBe(false)
  })

  it('marks the meter as over plan when actual exceeds the baseline', () => {
    mockMeterStatus.value = { ...mockMeterStatus.value, actual: 12, expected: 10 }
    const wrapper = mountPanel()
    expect(wrapper.find('.usage-fill').classes()).toContain('is-over')
    expect(wrapper.find('.usage-badge').text()).toContain('usageMeter.badgeOver')
  })

  it('marks the meter as full once the quota is spent', () => {
    mockMeterStatus.value = { ...mockMeterStatus.value, actual: 20 }
    const wrapper = mountPanel()
    expect(wrapper.find('.usage-fill').classes()).toContain('is-full')
    expect(wrapper.find('.usage-badge').text()).toContain('usageMeter.badgeFull')
  })

  it('shows the cycle day counter under the meter bar', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.usage-meta').text()).toContain('usageMeter.dayOfCycle')
  })

  it('opens the rule settings when the usage row is tapped', async () => {
    // On a phone this row is the only usage surface, so it is also the only way
    // to reach the rule. A dead-looking row would strand the user with a number
    // they cannot explain or change.
    const wrapper = mountPanel()
    const row = wrapper.find('.usage-row')
    expect(row.element.tagName).toBe('BUTTON')
    await row.trigger('click')
    expect(wrapper.emitted('open-usage-settings')).toHaveLength(1)
  })

  it('keeps the machine rows non-interactive', () => {
    // Only the usage row navigates; wrapping the whole panel would turn every
    // metric into a button that does nothing.
    const wrapper = mountPanel()
    const rows = wrapper.findAll('.resource-row')
    const buttons = rows.filter(r => r.element.tagName === 'BUTTON')
    expect(buttons).toHaveLength(1)
    expect(buttons[0].classes()).toContain('usage-row')
  })

  it('keeps the meter out of the list when the connection is down', () => {
    const wrapper = mountPanel({ wsStatus: 'disconnected' })
    expect(wrapper.find('.usage-row').exists()).toBe(false)
  })
})
