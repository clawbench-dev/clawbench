import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'

// Mock lucide icons
vi.mock('lucide-vue-next', () => ({
  Sparkles: { name: 'SparklesIcon', render: () => null },
}))

// Mock BottomSheet
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
  },
}))

// Mock useMessageClusters
const mockClusters = ref<any[]>([])
const mockLoaded = ref(false)
const mockLoading = ref(false)
const mockComputing = ref(false)
const mockProgress = ref<any>({ status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '' })
const mockMode = ref('')
const mockUpdatedAt = ref('')
const mockFetchClusters = vi.fn()
const mockStartCompute = vi.fn()
const mockStopPolling = vi.fn()

vi.mock('@/composables/useMessageClusters', () => ({
  useMessageClusters: () => ({
    clusters: mockClusters,
    loaded: mockLoaded,
    loading: mockLoading,
    computing: mockComputing,
    progress: mockProgress,
    mode: mockMode,
    updatedAt: mockUpdatedAt,
    fetchClusters: mockFetchClusters,
    startCompute: mockStartCompute,
    stopPolling: mockStopPolling,
  }),
  MessageCluster: {},
}))

// Mock useQuickSend
const mockAddItem = vi.fn()
vi.mock('@/composables/useQuickSend', () => ({
  useQuickSend: () => ({
    addItem: mockAddItem,
  }),
}))

// Mock useToast
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({
    show: vi.fn(),
  }),
}))

// Mock useTabDrawer
const mockDrawerOpen = vi.fn()
const mockDrawerClose = vi.fn()
vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: ref(true),
    isOpen: ref(false),
    open: mockDrawerOpen,
    close: mockDrawerClose,
    toggle: vi.fn(),
  }),
}))

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import MessageClustersDrawer from '@/components/chat/MessageClustersDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        messageClusters: {
          title: 'Message Recommendations',
          loading: 'Loading...',
          computing: 'Analyzing messages...',
          noCache: 'No analysis results yet.',
          firstAnalyze: 'Start Analysis',
          reanalyze: 'Re-analyze',
          addQuickSend: 'Add to Quick Send',
          cacheStatus: 'Mode: {mode} | Updated: {updatedAt}',
          error: 'Analysis failed',
          retry: 'Retry',
          phase_extracting: 'Extracting messages ({msgCount} found, {elapsed})',
          phase_clustering: 'Clustering messages ({elapsed})',
          phase_saving: 'Saving results ({elapsed})',
          recommendations: 'Recommendations',
        },
        quickSend: {
          itemSaved: 'Item saved',
        },
      },
    },
  },
})

function mountDrawer() {
  return mount(MessageClustersDrawer, {
    global: { plugins: [i18n] },
  })
}

describe('MessageClustersDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockClusters.value = []
    mockLoaded.value = false
    mockLoading.value = false
    mockComputing.value = false
    mockProgress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '' }
    mockMode.value = ''
    mockUpdatedAt.value = ''
  })

  it('renders empty state when no cache', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-empty').exists()).toBe(true)
    expect(wrapper.find('.mc-empty').text()).toContain('No analysis results yet')
    expect(wrapper.find('.mc-btn.primary').exists()).toBe(true)
  })

  it('renders computing state with progress bar', () => {
    mockComputing.value = true
    mockProgress.value = { status: 'computing', phase: 'extracting', msg_count: 10, elapsed_ms: 500, mode: 'auto' }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-computing').exists()).toBe(true)
    expect(wrapper.find('.mc-progress-bar').exists()).toBe(true)
  })

  it('renders error state', () => {
    mockProgress.value = { status: 'error', phase: '', msg_count: 0, elapsed_ms: 0, mode: '' }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-error').exists()).toBe(true)
    expect(wrapper.find('.mc-error').text()).toContain('Analysis failed')
  })

  it('renders loading state', () => {
    mockLoading.value = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-loading').exists()).toBe(true)
  })

  it('renders cached results with clusters', () => {
    mockLoaded.value = true
    mockClusters.value = [
      { id: 1, representative: 'Fix the bug', variants: ['fix bug', 'bug fix'], total_count: 5, representative_count: 3 },
    ]
    mockMode.value = 'auto'
    mockUpdatedAt.value = '2026-08-01'
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto' }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-results').exists()).toBe(true)
    expect(wrapper.find('.mc-cluster-item').exists()).toBe(true)
    expect(wrapper.find('.mc-cluster-representative').text()).toContain('Fix the bug')
  })

  it('calls startCompute when "Start Analysis" button is clicked', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-btn.primary').trigger('click')
    expect(mockStartCompute).toHaveBeenCalledOnce()
  })

  it('calls startCompute when "Retry" button is clicked', async () => {
    mockProgress.value = { status: 'error', phase: '', msg_count: 0, elapsed_ms: 0, mode: '' }
    const wrapper = mountDrawer()
    await wrapper.find('.mc-btn').trigger('click')
    expect(mockStartCompute).toHaveBeenCalledOnce()
  })

  it('calls addItem when "Add to Quick Send" is clicked', async () => {
    mockLoaded.value = true
    mockClusters.value = [
      { id: 1, representative: 'Fix the bug', variants: [], total_count: 3, representative_count: 2 },
    ]
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto' }
    mockAddItem.mockResolvedValue(true)
    const wrapper = mountDrawer()
    await wrapper.find('.mc-btn.add').trigger('click')
    expect(mockAddItem).toHaveBeenCalledWith({ label: 'Fix the bug', command: 'Fix the bug' })
  })

  it('open() sets visible and calls fetchClusters', async () => {
    mockFetchClusters.mockResolvedValue(undefined)
    const wrapper = mountDrawer()
    await wrapper.vm.open()
    expect(mockFetchClusters).toHaveBeenCalledOnce()
  })
})
