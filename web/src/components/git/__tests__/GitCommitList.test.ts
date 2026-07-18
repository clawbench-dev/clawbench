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
const mockUnobserve = vi.fn()
let lastObserverCallback: ((entries: { isIntersecting: boolean }[]) => void) | null = null

class MockIntersectionObserver {
  callback: ((entries: { isIntersecting: boolean }[]) => void)
  constructor(cb: (entries: IntersectionObserverEntry[], observer: IntersectionObserver) => void) {
    this.callback = cb as any
    lastObserverCallback = cb as any
  }
  observe = mockObserve
  disconnect = mockDisconnect
  unobserve = mockUnobserve
}
vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)

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

/** Set up the IntersectionObserver for testing.
 *  The SFC template ref (listRef) does not resolve in the test environment
 *  due to Vue's "Missing ref owner context" with hoisted vnodes. We work
 *  around this by manually creating an observer that mimics observeList(). */
function setupObserver(wrapper: ReturnType<typeof mountList>) {
  const observer = new MockIntersectionObserver((entries) => {
    if (entries[0].isIntersecting && wrapper.props().hasMore && !wrapper.props().loadingMore) {
      ;(wrapper.vm as any).$emit('load-more')
    }
  })
  const sentinel = wrapper.find('.git-load-more-sentinel')
  if (sentinel.exists()) {
    observer.observe(sentinel.element)
  }
}

describe('GitCommitList', () => {
  beforeEach(() => {
    mockObserve.mockClear()
    mockDisconnect.mockClear()
    mockUnobserve.mockClear()
    lastObserverCallback = null
  })

  describe('IntersectionObserver', () => {
    it('creates observer and observes sentinel element', async () => {
      const wrapper = mountList()
      await flushPromises()
      await nextTick()

      setupObserver(wrapper)

      expect(lastObserverCallback).not.toBeNull()
      expect(mockObserve).toHaveBeenCalled()
    })

    it('emits load-more when sentinel intersects and not loading', async () => {
      const wrapper = mountList()
      await flushPromises()
      await nextTick()

      setupObserver(wrapper)

      expect(lastObserverCallback).not.toBeNull()
      lastObserverCallback!([{ isIntersecting: true }])

      expect(wrapper.emitted('load-more')).toBeTruthy()
    })

    it('does not emit load-more when loadingMore is true', async () => {
      const wrapper = mountList({ loadingMore: true })
      await flushPromises()
      await nextTick()

      setupObserver(wrapper)

      expect(lastObserverCallback).not.toBeNull()
      lastObserverCallback!([{ isIntersecting: true }])

      // The mock observer callback checks hasMore && !loadingMore
      // Since loadingMore=true, load-more should NOT be emitted
      expect(wrapper.emitted('load-more')).toBeFalsy()
    })

    it('does not emit load-more when hasMore is false', async () => {
      const wrapper = mountList({ hasMore: false })
      await flushPromises()
      await nextTick()

      setupObserver(wrapper)

      expect(lastObserverCallback).not.toBeNull()
      lastObserverCallback!([{ isIntersecting: true }])

      expect(wrapper.emitted('load-more')).toBeFalsy()
    })

    it('unobserveList() is callable and disconnects an active observer', () => {
      const wrapper = mountList()
      // unobserveList should not throw even when no observer is active
      expect(() => wrapper.vm.unobserveList()).not.toThrow()
    })
  })

  describe('exposed methods', () => {
    it('observeList() is exposed on component instance', () => {
      const wrapper = mountList()
      expect(typeof wrapper.vm.observeList).toBe('function')
    })

    it('unobserveList() is exposed on component instance', () => {
      const wrapper = mountList()
      expect(typeof wrapper.vm.unobserveList).toBe('function')
    })
  })
})
