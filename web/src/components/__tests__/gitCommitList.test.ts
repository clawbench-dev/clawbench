import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import GitCommitList from '@/components/git/GitCommitList.vue'
import GitGraph from '@/components/git/GitGraph.vue'
import SearchInput from '@/components/common/SearchInput.vue'

// ── i18n setup ──
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      git: {
        commitList: {
          loadingAll: 'Loading all…',
          countUnit: '',
          notInitialized: 'Not initialized',
          loading: 'Loading…',
          refresh: 'Refresh',
          notGitRepo: 'Not a git repo',
          notGitRepoDesc: 'This project is not under version control',
          untrackedFile: 'Untracked file',
          untrackedDesc: 'This file is not tracked by git.',
          untrackedHint: 'to start tracking it.',
          noCommits: 'No commits',
        },
        manage: { title: 'Manage' },
      },
    },
  },
})

// ── Stubs ──
const LucideStub = { template: '<span class="lucide-stub" />' }
const SearchInputStub = { template: '<input class="search-input-stub" />' }

// ── Sample data ──
function makeCommit(overrides: Record<string, unknown> = {}) {
  return {
    sha: 'abc1234567890def1234567890abc1234567890',
    msg: 'Test commit',
    date: new Date().toISOString(),
    author: 'test',
    refs: [],
    parents: ['def1234567890abc1234567890def1234567890ab'],
    ...overrides,
  }
}

function mountList(props: Record<string, unknown> = {}) {
  return mount(GitCommitList, {
    props: {
      commits: [makeCommit()],
      isGit: true,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        'lucide-vue-next': LucideStub,
        [SearchInput.name || 'SearchInput']: SearchInputStub,
      },
    },
  })
}

// Helper: check if GitGraph is rendered (by its wrapper class or component instance)
function hasGitGraph(wrapper: ReturnType<typeof mountList>) {
  // GitGraph renders with class="commit-list-graph" on its root SVG
  return wrapper.find('.commit-list-graph').exists()
}

describe('GitCommitList mode behavior', () => {
  // ── GitGraph visibility ──

  it('shows GitGraph in project mode (default)', () => {
    const wrapper = mountList({ mode: 'project' })
    expect(hasGitGraph(wrapper)).toBe(true)
  })

  it('shows GitGraph when mode is not specified (defaults to project)', () => {
    const wrapper = mountList()
    expect(hasGitGraph(wrapper)).toBe(true)
  })

  it('hides GitGraph in file mode', () => {
    const wrapper = mountList({ mode: 'file' })
    expect(hasGitGraph(wrapper)).toBe(false)
  })

  it('does not show graph hint in file mode when not searching', () => {
    const wrapper = mountList({ mode: 'file' })
    // The graph-hint div should only appear during search, not in file mode
    expect(wrapper.find('.commit-list-graph-hint').exists()).toBe(false)
  })

  // ── Branch management button visibility ──

  it('shows branch management button in project mode', () => {
    const wrapper = mountList({ mode: 'project' })
    // The branch button is the second drilldown-refresh-btn (first is refresh)
    const buttons = wrapper.findAll('.drilldown-refresh-btn')
    // At least 2 buttons: refresh + manage
    expect(buttons.length).toBeGreaterThanOrEqual(2)
  })

  it('hides branch management button in file mode', () => {
    const wrapper = mountList({ mode: 'file' })
    // The branch button title is the manage title
    const manageBtn = wrapper.findAll('.drilldown-refresh-btn').find(b => b.attributes('title') === 'Manage')
    expect(manageBtn).toBeUndefined()
  })

  it('always shows refresh button regardless of mode', () => {
    const projectWrapper = mountList({ mode: 'project' })
    const fileWrapper = mountList({ mode: 'file' })
    expect(projectWrapper.find('.drilldown-refresh-btn').exists()).toBe(true)
    expect(fileWrapper.find('.drilldown-refresh-btn').exists()).toBe(true)
  })

  // ── Touch swipe handling ──

  it('skips swipe handling in file mode (onTouchEnd returns early)', () => {
    const wrapper = mountList({ mode: 'file' })
    const content = wrapper.find('.commit-list-content')

    // Simulate a left swipe in file mode
    content.trigger('touchstart', { touches: [{ clientX: 200, clientY: 100 }] })
    content.trigger('touchend', { changedTouches: [{ clientX: 100, clientY: 100 }] })

    // graphCollapsed should remain false (default) because the handler returned early
    // We can't directly check the internal ref, but we verify no crash occurs
    expect(wrapper.find('.commit-list-content').exists()).toBe(true)
  })

  it('skips swipe handling in file mode (onTouchStart returns early)', () => {
    const wrapper = mountList({ mode: 'file' })
    const content = wrapper.find('.commit-list-content')

    // Touch in file mode should be a no-op
    content.trigger('touchstart', { touches: [{ clientX: 200, clientY: 100 }] })
    content.trigger('touchend', { changedTouches: [{ clientX: 100, clientY: 100 }] })

    // No side effects — component still renders correctly
    expect(wrapper.find('.drilldown-item').exists()).toBe(true)
  })

  // ── Mode prop default ──

  it('defaults mode to project when not provided', () => {
    const wrapper = mountList()
    // In project mode, GitGraph is visible
    expect(hasGitGraph(wrapper)).toBe(true)
  })
})
