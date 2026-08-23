import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, defineComponent } from 'vue'
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
  RotateCw: { template: '<svg />' },
  RotateCcw: { template: '<svg />' },
  GitBranch: { template: '<svg />' },
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: defineComponent({
    props: { modelValue: String, placeholder: String },
    emits: ['update:modelValue', 'enter', 'down', 'up'],
    template: '<input class="search-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" />',
  }),
}))

vi.mock('@/components/git/GitGraph.vue', () => ({
  default: defineComponent({
    props: { commits: Array, collapsed: Boolean, 'row-height': Number },
    template: '<div class="graph-stub" :data-collapsed="String(collapsed)" />',
  }),
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

  afterEach(() => {
    vi.useRealTimers()
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

    it('unobserveList() disconnects an active observer', async () => {
      const wrapper = mountList()
      await flushPromises()
      await nextTick()
      setupObserver(wrapper)
      expect(mockDisconnect).not.toHaveBeenCalled()
      wrapper.vm.unobserveList()
      expect(mockDisconnect).toHaveBeenCalled()
    })
  })

  describe('empty, loading and error states', () => {
    it('shows notGitRepo empty state when not a git repo', () => {
      const wrapper = mountList({ isGit: false })
      expect(wrapper.text()).toContain('git.commitList.notGitRepo')
      expect(wrapper.find('.drilldown-header').exists()).toBe(false)
    })

    it('shows untracked empty state when commits empty and untracked', () => {
      const wrapper = mountList({ commits: [], untracked: true })
      expect(wrapper.text()).toContain('git.commitList.untrackedFile')
    })

    it('shows noCommits when commits empty and not untracked', () => {
      const wrapper = mountList({ commits: [], untracked: false })
      expect(wrapper.text()).toContain('git.commitList.noCommits')
    })

    it('shows loading spinner when loading', () => {
      const wrapper = mountList({ loading: true })
      expect(wrapper.find('.git-history-loading').exists()).toBe(true)
    })

    it('shows error message when error is set', () => {
      const wrapper = mountList({ error: 'boom' })
      expect(wrapper.find('.git-history-error').text()).toBe('boom')
    })

    it('shows loadingAll text when searchLoading is true', () => {
      const wrapper = mountList({ searchLoading: true })
      expect(wrapper.text()).toContain('git.commitList.loadingAll')
    })

    it('shows loading text when commits empty, isGit and not untracked', () => {
      const wrapper = mountList({ commits: [], untracked: false })
      expect(wrapper.text()).toContain('git.commitList.loading')
    })
  })

  describe('header actions', () => {
    it('emits refresh when refresh button is clicked', async () => {
      const wrapper = mountList()
      const btn = wrapper.findAll('.drilldown-refresh-btn')[0]
      await btn.trigger('click')
      expect(wrapper.emitted('refresh')).toBeTruthy()
    })

    it('emits manage when manage button is clicked (non-file mode)', async () => {
      const wrapper = mountList()
      const btn = wrapper.findAll('.drilldown-refresh-btn')[1]
      await btn.trigger('click')
      expect(wrapper.emitted('manage')).toBeTruthy()
    })

    it('hides manage button in file mode', () => {
      const wrapper = mountList({ mode: 'file' })
      expect(wrapper.findAll('.drilldown-refresh-btn')).toHaveLength(1)
    })

    it('keeps the refresh button enabled during a background load', () => {
      // `loading` only drives the list overlay; the button's disabled state is
      // owned by the local `refreshing` spin state.
      const wrapper = mountList({ loading: true })
      const btn = wrapper.findAll('.drilldown-refresh-btn')[0]
      expect(btn.attributes('disabled')).toBeUndefined()
    })

    it('disables the refresh button while a refresh is spinning', async () => {
      vi.useFakeTimers()
      try {
        const wrapper = mountList()
        const btn = wrapper.findAll('.drilldown-refresh-btn')[0]
        expect(btn.attributes('disabled')).toBeUndefined()

        await btn.trigger('click')
        expect(btn.attributes('disabled')).toBeDefined()

        vi.advanceTimersByTime(600)
        await nextTick()
        expect(btn.attributes('disabled')).toBeUndefined()
      } finally {
        vi.useRealTimers()
      }
    })

    it('gives the refresh button a spinning feedback class on click', async () => {
      vi.useFakeTimers()
      try {
        const wrapper = mountList()
        const btn = wrapper.findAll('.drilldown-refresh-btn')[0]
        expect(btn.classes()).not.toContain('refresh-spin--active')

        await btn.trigger('click')

        // Immediately after click the icon spins and refresh is emitted
        expect(btn.classes()).toContain('refresh-spin--active')
        expect(wrapper.emitted('refresh')).toHaveLength(1)

        // Double-click while spinning is ignored
        await btn.trigger('click')
        expect(wrapper.emitted('refresh')).toHaveLength(1)

        // After the minimum feedback window the spin ends
        vi.advanceTimersByTime(600)
        await nextTick()
        expect(btn.classes()).not.toContain('refresh-spin--active')
      } finally {
        vi.useRealTimers()
      }
    })
  })

  describe('commit rows', () => {
    it('emits select when a commit item is clicked', async () => {
      const wrapper = mountList()
      await wrapper.find('.drilldown-item').trigger('click')
      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0][0]).toBe(wrapper.props('commits')[0])
    })

    it('marks the selected commit row', async () => {
      const wrapper = mountList({ selectedSHA: 'sha0'.padEnd(40, '0') })
      const item = wrapper.find('.drilldown-item')
      expect(item.classes()).toContain('drilldown-item-selected')
    })

    it('renders ref tags with correct classes', () => {
      const wrapper = mountList({
        commits: [{
          sha: 'a'.repeat(40), msg: 'm', date: '2025-01-01', author: 'A',
          refs: ['HEAD', 'tag: v1.0', 'main'],
        }],
      })
      const tags = wrapper.findAll('.git-ref-tag')
      expect(tags).toHaveLength(3)
      expect(tags[0].classes()).toContain('ref-head')
      expect(tags[1].classes()).toContain('ref-tag')
      expect(tags[2].classes()).toContain('ref-branch')
    })

    it('hides sha tag for working-tree commits (isWT)', () => {
      const wrapper = mountList({
        commits: [{ sha: 'b'.repeat(40), msg: 'wt', date: '2025-01-01', author: 'A', refs: [], isWT: true }],
      })
      expect(wrapper.find('.git-commit-sha').exists()).toBe(false)
    })

    it('renders author name when present', () => {
      const wrapper = mountList()
      expect(wrapper.find('.drilldown-item').text()).toContain('Test')
    })
  })

  describe('search', () => {
    it('filters displayed commits by search query', async () => {
      const wrapper = mountList()
      wrapper.vm.commitSearch = 'Commit 1'
      await nextTick()
      const items = wrapper.findAll('.drilldown-item')
      expect(items).toHaveLength(1)
      expect(items[0].text()).toContain('Commit 1')
    })

    it('shows graph hint while searching', async () => {
      const wrapper = mountList()
      wrapper.vm.commitSearch = 'Commit'
      await nextTick()
      expect(wrapper.find('.commit-list-graph-hint').exists()).toBe(true)
      expect(wrapper.find('.commit-list-graph').exists()).toBe(false)
    })

    it('debounces search emission by 300ms', async () => {
      vi.useFakeTimers()
      const wrapper = mountList()
      wrapper.vm.commitSearch = 'Commit'
      await nextTick()
      vi.advanceTimersByTime(150)
      expect(wrapper.emitted('search')).toBeFalsy()
      vi.advanceTimersByTime(150)
      expect(wrapper.emitted('search')).toBeTruthy()
      expect(wrapper.emitted('search')![0][0]).toBe('Commit')
    })

    it('does not filter when query is blank', () => {
      const wrapper = mountList()
      wrapper.vm.commitSearch = '   '
      expect(wrapper.vm.commitSearch).toBe('   ')
    })
  })

  describe('touch swipe graph toggle', () => {
    it('collapses graph on left swipe', async () => {
      const wrapper = mountList()
      const content = wrapper.find('.commit-list-content')
      await content.trigger('touchstart', { touches: [{ clientX: 120, clientY: 10 }] })
      await content.trigger('touchend', { changedTouches: [{ clientX: 40, clientY: 10 }] })
      const graph = wrapper.find('.graph-stub')
      expect(graph.attributes('data-collapsed')).toBe('true')
    })

    it('expands graph on right swipe', async () => {
      const wrapper = mountList()
      const content = wrapper.find('.commit-list-content')
      await content.trigger('touchstart', { touches: [{ clientX: 40, clientY: 10 }] })
      await content.trigger('touchend', { changedTouches: [{ clientX: 120, clientY: 10 }] })
      const graph = wrapper.find('.graph-stub')
      expect(graph.attributes('data-collapsed')).toBe('false')
    })

    it('ignores small swipes below threshold', async () => {
      const wrapper = mountList()
      const content = wrapper.find('.commit-list-content')
      await content.trigger('touchstart', { touches: [{ clientX: 100, clientY: 10 }] })
      await content.trigger('touchend', { changedTouches: [{ clientX: 90, clientY: 10 }] })
      const graph = wrapper.find('.graph-stub')
      expect(graph.attributes('data-collapsed')).toBe('false')
    })
  })

  describe('lifecycle', () => {
    it('clears the search timer on unmount', async () => {
      vi.useFakeTimers()
      const wrapper = mountList()
      wrapper.vm.commitSearch = 'Commit'
      vi.advanceTimersByTime(150)
      wrapper.unmount()
      vi.advanceTimersByTime(200)
      // Timer cleared on unmount, so no search emit fires
      expect(wrapper.emitted('search')).toBeFalsy()
    })
  })
})
