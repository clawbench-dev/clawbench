import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import GitCommitList from '@/components/git/GitCommitList.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// Mock IntersectionObserver
const mockObserve = vi.fn()
const mockDisconnect = vi.fn()
const mockIntersectionObserverInstance = {
  observe: mockObserve,
  disconnect: mockDisconnect,
  unobserve: vi.fn(),
}
const mockIntersectionObserver = vi.fn(function (this: typeof mockIntersectionObserverInstance) {
  return mockIntersectionObserverInstance
})
globalThis.IntersectionObserver = mockIntersectionObserver as any

// Mock lucide-vue-next icons
vi.mock('lucide-vue-next', () => ({
  FileText: { template: '<svg />' },
  Info: { template: '<svg />' },
  RefreshCw: { template: '<svg />' },
  GitBranch: { template: '<svg />' },
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: { template: '<input class="search-stub" />' },
}))

vi.mock('@/components/git/GitGraph.vue', () => ({
  default: { template: '<div class="graph-stub" />' },
}))

vi.mock('@/utils/gitGraph', () => ({
  refLabelText: (ref: string) => ref,
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (d: string) => d,
  formatDateTime: (d: string) => d,
}))

function createCommits(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    sha: `sha${i}`.padEnd(40, '0'),
    msg: `Commit ${i}`,
    date: '2025-01-01',
    author: 'Test',
    fileCount: 1,
    refs: [],
  }))
}

function mountList(props = {}) {
  return mount(GitCommitList, {
    props: {
      commits: createCommits(5),
      isGit: true,
      hasMore: true,
      loadingMore: false,
      ...props,
    },
  })
}

describe('GitCommitList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockIntersectionObserver.mockClear()
    mockObserve.mockClear()
    mockDisconnect.mockClear()
  })

  describe('IntersectionObserver setup on mount', () => {
    it('creates IntersectionObserver and observes sentinel on mount', async () => {
      mountList()
      await flushPromises()
      await nextTick()
      await nextTick()

      // observeList() is called on mount via nextTick
      expect(mockIntersectionObserver).toHaveBeenCalled()
      expect(mockObserve).toHaveBeenCalled()
    })

    it('re-attaches observer when component is re-mounted after view switch', async () => {
      // Simulates: navigate from chat commit ID → files view → back to commits
      const wrapper = mountList()
      await flushPromises()
      await nextTick()
      await nextTick()

      // First mount: observer created
      expect(mockIntersectionObserver).toHaveBeenCalled()
      expect(mockObserve).toHaveBeenCalled()

      // Unmount (simulates switching to files view)
      wrapper.unmount()
      expect(mockDisconnect).toHaveBeenCalled()

      // Re-mount (simulates switching back to commits view)
      mockIntersectionObserver.mockClear()
      mockObserve.mockClear()
      mockDisconnect.mockClear()

      mountList()
      await flushPromises()
      await nextTick()
      await nextTick()

      // Observer should be created again on re-mount
      expect(mockIntersectionObserver).toHaveBeenCalled()
      expect(mockObserve).toHaveBeenCalled()
    })

    it('emits load-more when sentinel intersects and not loading', async () => {
      const wrapper = mountList()
      await flushPromises()
      await nextTick()
      await nextTick()

      // Simulate intersection
      const observerCallback = mockIntersectionObserver.mock.calls[0][0]
      observerCallback([{ isIntersecting: true }])

      expect(wrapper.emitted('load-more')).toBeTruthy()
    })

    it('does not emit load-more when loadingMore is true', async () => {
      const wrapper = mountList({ loadingMore: true })
      await flushPromises()
      await nextTick()
      await nextTick()

      const observerCallback = mockIntersectionObserver.mock.calls[0][0]
      observerCallback([{ isIntersecting: true }])

      expect(wrapper.emitted('load-more')).toBeFalsy()
    })

    it('does not emit load-more when hasMore is false', async () => {
      const wrapper = mountList({ hasMore: false })
      await flushPromises()
      await nextTick()
      await nextTick()

      const observerCallback = mockIntersectionObserver.mock.calls[0][0]
      observerCallback([{ isIntersecting: true }])

      expect(wrapper.emitted('load-more')).toBeFalsy()
    })
  })

  describe('exposed observeList method', () => {
    it('observeList() creates new observer and disconnects previous one', async () => {
      const wrapper = mountList()
      await flushPromises()
      await nextTick()
      await nextTick()

      mockIntersectionObserver.mockClear()
      mockObserve.mockClear()
      mockDisconnect.mockClear()

      // Manually call observeList (as parent would)
      wrapper.vm.observeList()

      expect(mockDisconnect).toHaveBeenCalled()
      expect(mockIntersectionObserver).toHaveBeenCalled()
      expect(mockObserve).toHaveBeenCalled()
    })

    it('unobserveList() disconnects observer', async () => {
      const wrapper = mountList()
      await flushPromises()
      await nextTick()
      await nextTick()

      mockDisconnect.mockClear()
      wrapper.vm.unobserveList()

      expect(mockDisconnect).toHaveBeenCalled()
    })
  })
})
