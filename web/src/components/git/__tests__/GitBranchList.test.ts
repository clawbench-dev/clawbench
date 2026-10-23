import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import GitBranchList from '@/components/git/GitBranchList.vue'
import { PANEL_KEY_PREFIX } from '@/utils/panelState'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/git/GitBranchRow.vue', () => ({
  default: {
    name: 'GitBranchRow',
    template: '<div class="branch-row-stub" />',
    props: ['branch', 'disabled'],
    emits: ['switch', 'delete'],
  },
}))

const makeBranch = (name: string, overrides: Record<string, unknown> = {}) => ({
  name,
  isCurrent: false,
  isDefault: false,
  ahead: 0,
  behind: 0,
  ...overrides,
})

beforeEach(() => {
  localStorage.clear()
})

function mountList(props: Record<string, unknown> = {}) {
  return mount(GitBranchList, {
    props: {
      branches: [],
      ...props,
    },
    global: {
      stubs: {
        ChevronDown: true,
        ChevronRight: true,
      },
    },
  })
}

describe('GitBranchList', () => {
  describe('header rendering', () => {
    it('renders header with title and branch count', () => {
      const wrapper = mountList({ branches: [makeBranch('main'), makeBranch('dev')] })
      expect(wrapper.find('.section-title').text()).toBe('git.manage.branches')
      expect(wrapper.find('.section-count').text()).toBe('2')
    })

    it('hides count when no branches', () => {
      const wrapper = mountList()
      expect(wrapper.find('.section-count').exists()).toBe(false)
    })

    it('shows stash badge when stashCount > 0', () => {
      const wrapper = mountList({ stashCount: 3 })
      expect(wrapper.find('.stash-badge').exists()).toBe(true)
    })

    it('hides stash badge when stashCount is 0', () => {
      const wrapper = mountList({ stashCount: 0 })
      expect(wrapper.find('.stash-badge').exists()).toBe(false)
    })

    it('hides header when hideHeader is true', () => {
      const wrapper = mountList({ hideHeader: true })
      expect(wrapper.find('.section-header').exists()).toBe(false)
    })

    it('always shows body when hideHeader is true', () => {
      const wrapper = mountList({ hideHeader: true })
      expect(wrapper.find('.section-body').exists()).toBe(true)
    })
  })

  describe('states', () => {
    it('shows a placeholder when loading', () => {
      const wrapper = mountList({ loading: true })
      expect(wrapper.find('.section-skeleton').exists()).toBe(true)
    })

    it('shows error with retry button when error', () => {
      const wrapper = mountList({ error: true })
      expect(wrapper.find('.section-error').exists()).toBe(true)
      expect(wrapper.find('.retry-btn').exists()).toBe(true)
    })

    it('emits retry when retry button clicked', async () => {
      const wrapper = mountList({ error: true })
      await wrapper.find('.retry-btn').trigger('click')
      expect(wrapper.emitted('retry')).toBeTruthy()
    })

    it('shows empty message when no branches', () => {
      const wrapper = mountList()
      expect(wrapper.find('.section-empty').exists()).toBe(true)
    })
  })

  describe('sortedBranches', () => {
    it('sorts default first, then current, then alphabetical', () => {
      const branches = [
        makeBranch('zebra'),
        makeBranch('alpha', { isCurrent: true }),
        makeBranch('middle', { isDefault: true }),
      ]
      const wrapper = mountList({ branches })
      const rows = wrapper.findAllComponents({ name: 'GitBranchRow' })
      expect(rows[0].props('branch').name).toBe('middle')
      expect(rows[1].props('branch').name).toBe('alpha')
      expect(rows[2].props('branch').name).toBe('zebra')
    })

    it('sorts branches alphabetically when no special flags', () => {
      const branches = [makeBranch('ccc'), makeBranch('aaa'), makeBranch('bbb')]
      const wrapper = mountList({ branches })
      const rows = wrapper.findAllComponents({ name: 'GitBranchRow' })
      expect(rows.map(r => r.props('branch').name)).toEqual(['aaa', 'bbb', 'ccc'])
    })

    it('sorts default before current', () => {
      const branches = [
        makeBranch('feature', { isCurrent: true }),
        makeBranch('main', { isDefault: true }),
      ]
      const wrapper = mountList({ branches })
      const rows = wrapper.findAllComponents({ name: 'GitBranchRow' })
      expect(rows[0].props('branch').name).toBe('main')
    })
  })

  describe('event forwarding', () => {
    it('passes checkoutInProgress as disabled to branch rows', () => {
      const wrapper = mountList({
        branches: [makeBranch('main')],
        checkoutInProgress: true,
      })
      const row = wrapper.findComponent({ name: 'GitBranchRow' })
      expect(row.props('disabled')).toBe(true)
    })

    it('passes disabled=false when checkoutInProgress is false', () => {
      const wrapper = mountList({
        branches: [makeBranch('main')],
        checkoutInProgress: false,
      })
      const row = wrapper.findComponent({ name: 'GitBranchRow' })
      expect(row.props('disabled')).toBe(false)
    })
  })
})

describe('GitBranchList — collapsed persistence', () => {
  const KEY = PANEL_KEY_PREFIX + 'git-branch-collapsed'

  beforeEach(() => localStorage.clear())

  function collapsedOf(wrapper: ReturnType<typeof mountList>): boolean {
    const state = (wrapper.vm as unknown as { $: { setupState: Record<string, unknown> } }).$.setupState
    const raw = state.collapsed as { value?: boolean } | boolean
    return typeof raw === 'object' ? raw.value === true : raw === true
  }

  it('uses the initialCollapsed prop when nothing is stored', async () => {
    // `null` (never chosen) must stay distinguishable from an explicit false,
    // or the component's own default could never apply again.
    const wrapper = mountList({ initialCollapsed: true })
    await nextTick()
    expect(collapsedOf(wrapper)).toBe(true)
  })

  it('overrides the prop once the user has chosen', async () => {
    localStorage.setItem(KEY, '0')
    const wrapper = mountList({ initialCollapsed: true })
    await nextTick()
    expect(collapsedOf(wrapper)).toBe(false)
  })

  it('writes the registry key, not a bare legacy key', async () => {
    const wrapper = mountList({ initialCollapsed: false })
    await nextTick()
    await wrapper.find('.section-header').trigger('click')
    expect(localStorage.getItem(KEY)).toBe('1')
    expect(localStorage.getItem('git-branch-collapsed')).toBeNull()
  })
})
