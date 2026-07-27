import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import GitBranchRow from '@/components/git/GitBranchRow.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/git/SwipeToDeleteRow.vue', () => ({
  default: {
    name: 'SwipeToDeleteRow',
    template: '<div class="swipe-stub"><slot /></div>',
    props: ['deletable'],
    emits: ['delete'],
  },
}))

const makeBranch = (overrides: Record<string, unknown> = {}) => ({
  name: 'feature',
  isCurrent: false,
  isDefault: false,
  ahead: 0,
  behind: 0,
  ...overrides,
})

function mountRow(props: Record<string, unknown> = {}) {
  return mount(GitBranchRow, {
    props: {
      branch: makeBranch(),
      ...props,
    },
    global: {
      stubs: {
        GitBranch: true,
        SwipeToDeleteRow: {
          name: 'SwipeToDeleteRow',
          template: '<div class="swipe-stub"><slot /></div>',
          props: ['deletable'],
          emits: ['delete'],
        },
      },
    },
  })
}

describe('GitBranchRow', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  describe('click handling', () => {
    it('emits switch when clicking non-current, non-disabled branch', async () => {
      const branch = makeBranch({ name: 'dev' })
      const wrapper = mountRow({ branch })
      await wrapper.find('.git-branch-row').trigger('click')
      expect(wrapper.emitted('switch')).toBeTruthy()
      expect(wrapper.emitted('switch')![0][0]).toEqual(branch)
    })

    it('does not emit switch when branch is current', async () => {
      const wrapper = mountRow({ branch: makeBranch({ isCurrent: true }) })
      await wrapper.find('.git-branch-row').trigger('click')
      expect(wrapper.emitted('switch')).toBeFalsy()
    })

    it('does not emit switch when disabled', async () => {
      const wrapper = mountRow({ disabled: true })
      await wrapper.find('.git-branch-row').trigger('click')
      expect(wrapper.emitted('switch')).toBeFalsy()
    })

    it('does not emit switch when already switching', async () => {
      const wrapper = mountRow()
      await wrapper.find('.git-branch-row').trigger('click')
      expect(wrapper.emitted('switch')).toHaveLength(1)
      // Click again while switching
      await wrapper.find('.git-branch-row').trigger('click')
      expect(wrapper.emitted('switch')).toHaveLength(1)
    })

    it('emits switch only once per click', async () => {
      const branch = makeBranch({ name: 'dev' })
      const wrapper = mountRow({ branch })
      await wrapper.find('.git-branch-row').trigger('click')
      expect(wrapper.emitted('switch')).toHaveLength(1)
    })
  })

  describe('deletable logic', () => {
    it('passes deletable=true when branch is not current and not default', () => {
      const wrapper = mountRow({ branch: makeBranch() })
      const swipe = wrapper.findComponent({ name: 'SwipeToDeleteRow' })
      expect(swipe.props('deletable')).toBe(true)
    })

    it('passes deletable=false when branch is current', () => {
      const wrapper = mountRow({ branch: makeBranch({ isCurrent: true }) })
      const swipe = wrapper.findComponent({ name: 'SwipeToDeleteRow' })
      expect(swipe.props('deletable')).toBe(false)
    })

    it('passes deletable=false when branch is default', () => {
      const wrapper = mountRow({ branch: makeBranch({ isDefault: true }) })
      const swipe = wrapper.findComponent({ name: 'SwipeToDeleteRow' })
      expect(swipe.props('deletable')).toBe(false)
    })
  })

  describe('visual badges', () => {
    it('shows default badge when isDefault is true', () => {
      const wrapper = mountRow({ branch: makeBranch({ isDefault: true }) })
      expect(wrapper.find('.branch-default-badge').exists()).toBe(true)
    })

    it('shows ahead info when ahead > 0', () => {
      const wrapper = mountRow({ branch: makeBranch({ ahead: 3 }) })
      expect(wrapper.find('.track-ahead').exists()).toBe(true)
    })

    it('shows behind info when behind > 0', () => {
      const wrapper = mountRow({ branch: makeBranch({ behind: 2 }) })
      expect(wrapper.find('.track-behind').exists()).toBe(true)
    })

    it('has current class when branch is current', () => {
      const wrapper = mountRow({ branch: makeBranch({ isCurrent: true }) })
      expect(wrapper.find('.git-branch-row').classes()).toContain('current')
    })
  })
})
