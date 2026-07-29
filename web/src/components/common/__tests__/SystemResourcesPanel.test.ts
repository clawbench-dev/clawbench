import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
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

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

function mountPanel(props?: { showLogout?: boolean }) {
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
      },
    },
  })
}

describe('SystemResourcesPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockResources.value = { ...mockData }
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

  it('emits logout event when logout button is clicked', async () => {
    const wrapper = mountPanel({ showLogout: true })
    await wrapper.find('.logout-btn').trigger('click')
    expect(wrapper.emitted('logout')).toBeTruthy()
  })
})
