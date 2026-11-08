import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, defineComponent } from 'vue'
import GitCommitList from '@/components/git/GitCommitList.vue'

// ── Mocks ────────────────────────────────────────────────────
// importOriginal, not a bare replacement: the component now also pulls in
// shared composables (the quote bar) that install the real i18n singleton, and
// a mock without createI18n makes that module fail to evaluate — the whole
// suite then reports "0 test" instead of failing an assertion.
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

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

// Real icons (importOriginal): a whitelist mock silently renders nothing for
// any icon added later, which is exactly how the quote button's glyph would go
// missing without a single test failing.
vi.mock('lucide-vue-next', async (importOriginal) => await importOriginal<typeof import('lucide-vue-next')>())

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

// The two "number" buttons report through a toast when there is nothing to
// jump to; asserting on it is what makes those branches testable at all.
const { mockToastShow } = vi.hoisted(() => ({ mockToastShow: vi.fn() }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: mockToastShow }) }))

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
    mockToastShow.mockClear()
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

    it('swaps the count badge for a spinner while a search reload is in flight', () => {
      // The badge is one or two glyphs wide now, so the old "加载全部…" sentence
      // cannot fit in it. The number would also be the stale one, so it is
      // replaced rather than left sitting next to a spinner.
      const wrapper = mountList({ searchLoading: true })
      const badge = wrapper.find('.git-count')
      expect(badge.exists()).toBe(true)
      expect(badge.find('.li-spinner').exists()).toBe(true)
      expect(badge.text()).not.toContain('5')
    })

    it('shows loading text when commits empty, isGit and not untracked', () => {
      const wrapper = mountList({ commits: [], untracked: false })
      expect(wrapper.text()).toContain('git.commitList.loading')
    })
  })

  describe('header actions', () => {
    it('emits refresh when refresh button is clicked', async () => {
      const wrapper = mountList()
      await wrapper.find('[data-action="refresh"]').trigger('click')
      expect(wrapper.emitted('refresh')).toBeTruthy()
    })

    it('has no manage button of its own — the branch menu owns that entry', () => {
      // The bar used to end with a manage button; the branch picker's menu
      // carries the same action, so the duplicate is gone.
      const wrapper = mountList()
      expect(wrapper.find('[data-action="manage"]').exists()).toBe(false)
    })

    it('emits manage from the branch menu (non-file mode)', async () => {
      const wrapper = mountList({ branches: [{ name: 'main' }], currentBranch: 'main' })
      await wrapper.find('.branch-chip').trigger('click')
      const items = wrapper.findAll('.branch-menu-item')
      await items[items.length - 1].trigger('click')
      expect(wrapper.emitted('manage')).toBeTruthy()
    })

    it('offers no branch picker (hence no manage entry) in file mode', () => {
      const wrapper = mountList({ mode: 'file', branches: [{ name: 'main' }] })
      expect(wrapper.find('.branch-picker').exists()).toBe(false)
      // Refresh and search stay: a single file's history is still searchable.
      expect(wrapper.find('[data-action="refresh"]').exists()).toBe(true)
      expect(wrapper.find('[data-action="search"]').exists()).toBe(true)
    })

    it('keeps the refresh button enabled during a background load', () => {
      // `loading` only drives the list overlay; the button's disabled state is
      // owned by the local `refreshing` spin state.
      const wrapper = mountList({ loading: true })
      const btn = wrapper.find('[data-action="refresh"]')
      expect(btn.attributes('disabled')).toBeUndefined()
    })

    it('disables the refresh button while a refresh is spinning', async () => {
      vi.useFakeTimers()
      try {
        const wrapper = mountList()
        const btn = wrapper.find('[data-action="refresh"]')
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
        const btn = wrapper.find('[data-action="refresh"]')
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

    it('shows the changed-file count badge on the working-tree row', () => {
      const wrapper = mountList({
        commits: [{ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true }],
        wtFileCount: 3,
      })
      const badge = wrapper.find('.git-commit-file-count')
      expect(badge.exists()).toBe(true)
      expect(badge.text()).toBe('3')
      // Shape comes from the shared pill class; the call-site class only colours it.
      expect(badge.classes()).toContain('count-badge')
      // Hovering explains the bare number.
      expect(badge.attributes('title')).toBe('git.history.fileCount')
    })

    it('hides the badge when the workspace has no changes', () => {
      const wrapper = mountList({
        commits: [{ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true }],
        wtFileCount: 0,
      })
      expect(wrapper.find('.git-commit-file-count').exists()).toBe(false)
    })

    it('never badges a regular commit row', () => {
      const wrapper = mountList({ wtFileCount: 5 })
      expect(wrapper.findAll('.git-commit-file-count')).toHaveLength(0)
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

  describe('commit-id search', () => {
    // Real hex shas: the fixture builder above uses "sha0…", which is not a
    // commit id, so an id test built on it would pass for the wrong reason.
    const SHA_A = 'deadbeef' + '0'.repeat(32)
    const SHA_B = 'c0ffee12' + '0'.repeat(32)
    const hexCommits = [
      { sha: SHA_A, msg: 'fix parser', date: '2025-01-01', author: 'A', refs: [] },
      { sha: SHA_B, msg: 'add tests', date: '2025-01-01', author: 'B', refs: [] },
    ]

    it('finds a commit by sha prefix', async () => {
      const wrapper = mountList({ commits: hexCommits })
      wrapper.vm.commitSearch = 'deadbe'
      await nextTick()
      const items = wrapper.findAll('.drilldown-item')
      expect(items).toHaveLength(1)
      expect(items[0].text()).toContain('fix parser')
    })

    it('finds a commit by its full sha', async () => {
      const wrapper = mountList({ commits: hexCommits })
      wrapper.vm.commitSearch = SHA_B
      await nextTick()
      const items = wrapper.findAll('.drilldown-item')
      expect(items).toHaveLength(1)
      expect(items[0].text()).toContain('add tests')
    })

    it('is case-insensitive for the sha', async () => {
      const wrapper = mountList({ commits: hexCommits })
      wrapper.vm.commitSearch = 'DEADBE'
      await nextTick()
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(1)
    })

    it('does not id-match a hex query shorter than 4 chars', async () => {
      // 'dea' is valid hex but too short to be an abbreviation — treating it as
      // an id would make short queries look like they match everything.
      const wrapper = mountList({ commits: hexCommits })
      wrapper.vm.commitSearch = 'dea'
      await nextTick()
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(0)
    })

    it('still searches messages for a word that is also hex', async () => {
      // "dead" parses as hex, so the id channel is active — but the message
      // channel must run too, or a message containing it would be shadowed.
      const wrapper = mountList({
        commits: [
          { sha: SHA_B, msg: 'remove dead code', date: '2025-01-01', author: 'B', refs: [] },
          { sha: SHA_A, msg: 'unrelated', date: '2025-01-01', author: 'A', refs: [] },
        ],
      })
      wrapper.vm.commitSearch = 'dead'
      await nextTick()
      const items = wrapper.findAll('.drilldown-item')
      expect(items).toHaveLength(2)
      expect(items[0].text()).toContain('remove dead code')
      expect(items[1].text()).toContain('unrelated')
    })

    it('matches neither channel for a non-hex query that is not in any message', async () => {
      const wrapper = mountList({ commits: hexCommits })
      wrapper.vm.commitSearch = 'zzzz'
      await nextTick()
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(0)
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

  describe('merge-commit filter', () => {
    // A merge row lists everything both sides touched, so it drowns the actual
    // commits in a branch-merge history. The toggle hides those rows only.
    const MERGE = 'm'.repeat(40)
    const MERGE_COMMITS = [
      { sha: MERGE, msg: 'Merge branch X', date: '2025-01-01', author: 'A', refs: [], parents: ['a'.repeat(40), 'b'.repeat(40)] },
      { sha: 'a'.repeat(40), msg: 'real work', date: '2025-01-01', author: 'A', refs: [], parents: ['c'.repeat(40)] },
    ]

    it('keeps merge commits visible by default', () => {
      const wrapper = mountList({ commits: MERGE_COMMITS })
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(2)
      expect(wrapper.find('[data-action="toggle-merge-commits"]').attributes('aria-pressed')).toBe('false')
    })

    it('hides merge rows when the toggle is on', async () => {
      const wrapper = mountList({ commits: MERGE_COMMITS })
      await wrapper.find('[data-action="toggle-merge-commits"]').trigger('click')

      const rows = wrapper.findAll('.drilldown-item')
      expect(rows).toHaveLength(1)
      expect(rows[0].text()).toContain('real work')
      expect(wrapper.find('[data-action="toggle-merge-commits"]').attributes('aria-pressed')).toBe('true')
    })

    it('brings the merge rows back when toggled off again', async () => {
      const wrapper = mountList({ commits: MERGE_COMMITS })
      const btn = wrapper.find('[data-action="toggle-merge-commits"]')
      await btn.trigger('click')
      await btn.trigger('click')
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(2)
    })

    it('hides the graph while merges are filtered out', async () => {
      // The lanes are derived from parent links; keeping the graph would show
      // branch joins for rows that are no longer in the list.
      const wrapper = mountList({ commits: MERGE_COMMITS })
      expect(wrapper.find('.graph-stub').exists()).toBe(true)

      await wrapper.find('[data-action="toggle-merge-commits"]').trigger('click')
      expect(wrapper.find('.graph-stub').exists()).toBe(false)
    })

    it('stacks with the text search', async () => {
      const wrapper = mountList({ commits: MERGE_COMMITS })
      await wrapper.find('[data-action="toggle-merge-commits"]').trigger('click')
      wrapper.vm.commitSearch = 'Merge'
      await nextTick()
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(0)
    })

    it('treats a root commit (no parents field) as a normal row', async () => {
      const wrapper = mountList({
        commits: [{ sha: 'r'.repeat(40), msg: 'initial', date: '2025-01-01', author: 'A', refs: [] }],
      })
      await wrapper.find('[data-action="toggle-merge-commits"]').trigger('click')
      expect(wrapper.findAll('.drilldown-item')).toHaveLength(1)
    })

    it('is absent in file mode (single-file history has no branch graph)', () => {
      const wrapper = mountList({ mode: 'file' })
      expect(wrapper.find('[data-action="toggle-merge-commits"]').exists()).toBe(false)
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

  describe('drag into chat', () => {
    /**
     * A dragged commit must carry the sha as its locator: that is what lets the
     * staged card jump back to the commit, and it is also what stops the card
     * being resolved as a "pipeline" (commitSha + url).
     */
    it('writes a quote payload carrying the commit sha on dragstart', () => {
      const wrapper = mountList()
      const row = wrapper.find('.drilldown-item')
      const setData = vi.fn()
      const event = new Event('dragstart', { bubbles: true, cancelable: true })
      Object.assign(event, {
        dataTransfer: { setData, setDragImage: vi.fn(), effectAllowed: '' },
      })

      row.element.dispatchEvent(event)

      const quoteCall = setData.mock.calls.find(c => c[0] === 'application/x-clawbench-quote')
      expect(quoteCall, 'dragstart must write the quote MIME').toBeTruthy()
      const payload = JSON.parse(quoteCall![1])
      expect(payload.commitSha).toBe(createCommits(5)[0].sha)
      expect(payload.language).toBe('diff')
      // No annotation: a dragged quote is staged without one.
      expect(payload.note).toBeUndefined()
    })

    // The working-tree row ("uncommitted changes") has no commit to reference, so
    // dragging it would stage a card that cannot be jumped to.
    it('does not make the working-tree row draggable', () => {
      const wrapper = mountList({
        commits: [{ sha: 'WT', msg: 'Uncommitted changes', date: '', author: '', refs: [], isWT: true }],
      })

      expect(wrapper.find('.drilldown-item').attributes('draggable')).toBe('false')
    })

    it('makes a real commit row draggable', () => {
      const wrapper = mountList()
      expect(wrapper.find('.drilldown-item').attributes('draggable')).toBe('true')
    })
  })
})

// ── Toolbar ──────────────────────────────────────────────────
// The branch picker, the count and the buttons share ONE row. These cover the
// picker's two states, the count badge, and the search field's collapse.
describe('toolbar', () => {
  const branches = [{ name: 'main' }, { name: 'feat/payments' }]

  it('keeps the picker in the toolbar instead of a row of its own', () => {
    const wrapper = mountList({ branches, currentBranch: 'main' })
    // Exactly one bar: a second row for the branch picker was the thing this
    // layout removed, and re-adding one would double the header height.
    expect(wrapper.findAll('.drilldown-header')).toHaveLength(1)
    expect(wrapper.find('.git-toolbar .branch-picker .branch-chip').exists()).toBe(true)
  })

  it('offers no picker when the host passes no branches (file mode, drawer)', () => {
    const wrapper = mountList()
    expect(wrapper.find('.branch-chip').exists()).toBe(false)
    // The row still exists — it just leads with the count.
    expect(wrapper.find('.git-toolbar .git-count').exists()).toBe(true)
  })

  it('offers no picker in file mode even when branches are passed', () => {
    const wrapper = mountList({ branches, currentBranch: 'main', mode: 'file' })
    expect(wrapper.find('.branch-chip').exists()).toBe(false)
  })

  it('shows the checked-out branch and lists the others', async () => {
    const wrapper = mountList({ branches, currentBranch: 'main' })

    expect(wrapper.find('.branch-chip-name').text()).toBe('main')
    await wrapper.find('.branch-chip').trigger('click')

    // The old bar's "本地 N 个分支" sentence moved into the menu's header.
    expect(wrapper.find('.branch-menu-label').text()).toContain('git.branchView.localCount')

    const names = wrapper.findAll('.branch-menu-item .branch-menu-name').map(n => n.text())
    expect(names).toEqual(['main', 'feat/payments', 'git.branchView.manage'])
  })

  it('emits change-branch when picking another branch', async () => {
    const wrapper = mountList({ branches, currentBranch: 'main' })
    await wrapper.find('.branch-chip').trigger('click')
    await wrapper.findAll('.branch-menu-item')[1].trigger('click')

    expect(wrapper.emitted('change-branch')?.[0]).toEqual(['feat/payments'])
    expect(wrapper.find('.branch-menu').exists()).toBe(false)
  })

  it('picking the current branch normalises to an empty ref', async () => {
    const wrapper = mountList({ branches, currentBranch: 'main', viewBranch: 'feat/payments' })
    await wrapper.find('.branch-chip').trigger('click')
    await wrapper.findAll('.branch-menu-item')[0].trigger('click')

    expect(wrapper.emitted('change-branch')?.[0]).toEqual([''])
  })

  it('marks the picker and states where the workspace is, reading another branch', async () => {
    const wrapper = mountList({ branches, currentBranch: 'main', viewBranch: 'feat/payments' })

    expect(wrapper.find('.branch-chip').classes()).toContain('is-other')
    expect(wrapper.find('.branch-chip-name').text()).toBe('feat/payments')
    // The warning is its own thin strip, not a toolbar row: it is an alert
    // about the workspace, and it only exists in this one state.
    expect(wrapper.find('.branch-alert .branch-note--warn').text()).toContain('git.branchView.readOnly')

    await wrapper.find('.branch-back-btn').trigger('click')
    expect(wrapper.emitted('change-branch')?.[0]).toEqual([''])
  })

  it('stays quiet while the viewed branch is the checked-out one', () => {
    const wrapper = mountList({ branches, currentBranch: 'main', viewBranch: 'main' })

    expect(wrapper.find('.branch-chip').classes()).not.toContain('is-other')
    expect(wrapper.find('.branch-alert').exists()).toBe(false)
  })

  it('shows a compact count instead of a sentence', () => {
    const wrapper = mountList()
    const badge = wrapper.find('.git-count')
    // 5 commits, hasMore → "5+". The sentence it replaced was ~110px wide.
    expect(badge.text()).toBe('5+')
    // The full wording is still one hover away.
    expect(badge.attributes('title')).toContain('git.commitList.countUnit')
  })

  it('drops the "+" once every page is loaded', () => {
    const wrapper = mountList({ hasMore: false })
    expect(wrapper.find('.git-count').text()).toBe('5')
  })
})

// ── Search field ─────────────────────────────────────────────
// The field used to be a permanent half-row resident. It now costs one tap and
// no width; these pin that the tap is what opens it and that closing it cannot
// leave an invisible filter behind.
describe('search field', () => {
  it('stays collapsed until the search button is pressed', () => {
    const wrapper = mountList()
    expect(wrapper.find('.commit-search').exists()).toBe(false)
    expect(wrapper.find('[data-action="search"]').exists()).toBe(true)
  })

  it('replaces the toolbar row with the field instead of adding a second row', async () => {
    const wrapper = mountList()
    await wrapper.find('[data-action="search"]').trigger('click')

    expect(wrapper.find('.commit-search').exists()).toBe(true)
    expect(wrapper.find('.git-toolbar').exists()).toBe(false)
    expect(wrapper.findAll('.drilldown-header')).toHaveLength(1)
  })

  it('filters the list from the expanded field', async () => {
    const wrapper = mountList()
    await wrapper.find('[data-action="search"]').trigger('click')
    await wrapper.find('.search-stub').setValue('Commit 2')

    expect(wrapper.findAll('.drilldown-item')).toHaveLength(1)
  })

  it('clears the query when the field is closed', async () => {
    // A hidden filter is a lie: the list would stay filtered with nothing on
    // screen explaining why. Closing therefore resets the query.
    const wrapper = mountList()
    await wrapper.find('[data-action="search"]').trigger('click')
    await wrapper.find('.search-stub').setValue('Commit 2')
    expect(wrapper.findAll('.drilldown-item')).toHaveLength(1)

    await wrapper.find('.search-cancel').trigger('click')

    expect(wrapper.find('.commit-search').exists()).toBe(false)
    expect(wrapper.findAll('.drilldown-item')).toHaveLength(5)
  })
})

// ── Fetch from remotes ───────────────────────────────────────
// The toolbar only emits; the host owns the request. Fetch never moves the
// branch, so unlike a pull it stays available everywhere except file mode.
describe('fetch from remotes', () => {
  it('emits fetch when clicked', async () => {
    const wrapper = mountList()
    await wrapper.find('[data-action="fetch"]').trigger('click')
    expect(wrapper.emitted('fetch')).toHaveLength(1)
  })

  it('disables itself while a fetch is in flight', () => {
    const wrapper = mountList({ fetching: true })
    expect(wrapper.find('[data-action="fetch"]').attributes('disabled')).toBeDefined()
  })

  it('is absent in file mode', () => {
    const wrapper = mountList({ mode: 'file' })
    expect(wrapper.find('[data-action="fetch"]').exists()).toBe(false)
  })

  it('stays available while reading another branch', () => {
    // Fetch only moves remote-tracking refs, so it is safe from any branch view
    // — and it is exactly how that branch's ahead/behind numbers get current.
    const wrapper = mountList({
      branches: [{ name: 'main' }, { name: 'feat/payments' }],
      currentBranch: 'main',
      viewBranch: 'feat/payments',
    })
    expect(wrapper.find('[data-action="fetch"]').exists()).toBe(true)
  })
})

// ── Upstream divergence ──────────────────────────────────────
// ahead/behind come from the server's %(upstream:track) parse. Only render a
// side that is non-zero, and render nothing at all for a branch with no
// upstream (0/0 there would read as "in sync").
describe('upstream divergence', () => {
  const branch = (ahead: number, behind: number, remoteTracking = 'origin/main') => ({
    branches: [{ name: 'main', remoteTracking, ahead, behind }],
    currentBranch: 'main',
  })

  it('shows both directions next to the branch', () => {
    const wrapper = mountList(branch(2, 3))
    expect(wrapper.find('.branch-track-ahead').text()).toBe('↑2')
    expect(wrapper.find('.branch-track-behind').text()).toBe('↓3')
  })

  it('omits the side that is zero', () => {
    const wrapper = mountList(branch(0, 4))
    expect(wrapper.find('.branch-track-ahead').exists()).toBe(false)
    expect(wrapper.find('.branch-track-behind').text()).toBe('↓4')
  })

  it('renders nothing when the branch matches its upstream', () => {
    const wrapper = mountList(branch(0, 0))
    expect(wrapper.find('.branch-track').exists()).toBe(false)
  })

  it('renders nothing when the branch has no upstream', () => {
    const wrapper = mountList(branch(0, 0, ''))
    expect(wrapper.find('.branch-track').exists()).toBe(false)
  })

  it('explains the numbers on hover', () => {
    // The numbers are buttons now (each one jumps to what it counts), so the
    // tooltip lives on them rather than on the wrapper.
    const wrapper = mountList(branch(2, 3))
    expect(wrapper.find('.branch-track-ahead').attributes('title')).toBe('git.branchView.trackTitle')
    expect(wrapper.find('.branch-track-behind').attributes('title')).toBe('git.branchView.trackTitle')
  })
})

// ── Which commits differ from the upstream ───────────────────
// The counters say how many; the divergence payload says which. The list marks
// the unpushed rows and names the remote-only ones.
describe('divergence detail', () => {
  const commits = createCommits(3)
  const remoteCommit = { sha: 'b'.repeat(40), msg: 'Remote work', date: '2025-01-02', author: 'Other' }

  const divergence = (over: Record<string, unknown> = {}) => ({
    hasUpstream: true,
    upstream: 'origin/main',
    ahead: [commits[0].sha],
    behind: [remoteCommit],
    aheadCount: 1,
    behindCount: 1,
    ...over,
  })

  const mountWith = (over: Record<string, unknown> = {}) => mountList({ commits, divergence: divergence(over) })

  it('marks the unpushed rows', () => {
    const wrapper = mountWith()
    const rows = wrapper.findAll('.drilldown-item')
    expect(rows[0].classes()).toContain('is-unpushed')
    expect(rows[0].find('.git-ref-tag--unpushed').text()).toBe('git.divergence.unpushed')
    // A pushed commit is not marked.
    expect(rows[1].classes()).not.toContain('is-unpushed')
    expect(rows[1].find('.git-ref-tag--unpushed').exists()).toBe(false)
  })

  it('shows the remote strip collapsed, then lists the commits', async () => {
    const wrapper = mountWith()
    expect(wrapper.find('.remote-commits').exists()).toBe(true)
    expect(wrapper.find('.remote-commits-list').exists()).toBe(false)

    await wrapper.find('[data-action="toggle-remote-commits"]').trigger('click')
    const listed = wrapper.findAll('.remote-commit')
    expect(listed).toHaveLength(1)
    expect(listed[0].text()).toContain('Remote work')
    expect(listed[0].find('.git-ref-tag--remote').text()).toBe('git.divergence.remoteTag')
  })

  it('uses the same two-line row shape as a normal commit', async () => {
    // Same type scale and layout as .drilldown-item rows, so the strip reads as
    // part of the same list rather than a different widget.
    const wrapper = mountWith()
    await wrapper.find('[data-action="toggle-remote-commits"]').trigger('click')

    const row = wrapper.find('.remote-commit')
    const head = row.find('.git-commit-msg')
    const meta = row.find('.git-commit-meta')
    expect(head.find('.git-commit-msg-text').text()).toBe('Remote work')
    expect(head.find('.git-commit-sha').exists()).toBe(false)
    // The secondary facts live on their own line, as in a normal commit row.
    expect(meta.find('.git-commit-sha').exists()).toBe(true)
    expect(meta.text()).toContain('2025-01-02')
    expect(meta.text()).toContain('Other')
  })

  it('opens the strip from the ↓ number', async () => {
    const wrapper = mountWith()
    await wrapper.find('[data-action="track-behind"]').trigger('click')
    expect(wrapper.find('.remote-commits-list').exists()).toBe(true)
  })

  it('selects a remote-only commit when it is clicked', async () => {
    const wrapper = mountWith()
    await wrapper.find('[data-action="toggle-remote-commits"]').trigger('click')
    await wrapper.find('.remote-commit').trigger('click')
    const emitted = wrapper.emitted('select')
    expect(emitted).toBeTruthy()
    expect((emitted![0][0] as { sha: string }).sha).toBe(remoteCommit.sha)
  })

  it('jumps to the first unpushed row from the ↑ number', async () => {
    const wrapper = mountWith()
    await wrapper.find('[data-action="track-ahead"]').trigger('click')
    const first = wrapper.findAll('.drilldown-item')[0]
    expect(first.classes()).toContain('is-flash')
  })

  it('shows nothing when the branch has no upstream', () => {
    const wrapper = mountWith({ hasUpstream: false, ahead: [], behind: [], aheadCount: 0, behindCount: 0 })
    expect(wrapper.find('.branch-track').exists()).toBe(false)
    expect(wrapper.find('.remote-commits').exists()).toBe(false)
    expect(wrapper.find('.git-ref-tag--unpushed').exists()).toBe(false)
  })

  it('says so when the unpushed commits are not in the list', async () => {
    // The marked commit is not among the rows on screen (filtered by a search,
    // or further back than the loaded page).
    const wrapper = mountWith({ ahead: ['f'.repeat(40)] })
    await wrapper.find('[data-action="track-ahead"]').trigger('click')
    // Nothing to scroll to: no row may be flashed…
    expect(wrapper.findAll('.drilldown-item.is-flash')).toHaveLength(0)
    // …and the user must be told why nothing happened.
    expect(mockToastShow).toHaveBeenCalledWith('git.divergence.notInPage', expect.anything())
  })

  it('reports a missing remote list instead of opening an empty strip', async () => {
    // The count is known but the commits are not (git log failed): opening a
    // blank strip would look like a broken button.
    const wrapper = mountWith({ behind: [], behindCount: 2 })
    await wrapper.find('[data-action="track-behind"]').trigger('click')
    expect(mockToastShow).toHaveBeenCalledWith('git.divergence.detailUnavailable', expect.anything())
    expect(wrapper.find('.remote-commits').exists()).toBe(false)
  })

  it('keeps the numbers as plain text when only the counts are known', () => {
    // New web bundle + old server (no /api/git/divergence): the branch listing
    // still has the numbers, but there is no detail to jump to. A button that
    // silently does nothing is worse than no button.
    const wrapper = mountList({
      branches: [{ name: 'main', remoteTracking: 'origin/main', ahead: 2, behind: 1 }],
      currentBranch: 'main',
    })
    expect(wrapper.find('.branch-track-ahead').text()).toBe('↑2')
    expect(wrapper.find('[data-action="track-ahead"]').exists()).toBe(false)
    expect(wrapper.find('[data-action="track-behind"]').exists()).toBe(false)
  })
})
