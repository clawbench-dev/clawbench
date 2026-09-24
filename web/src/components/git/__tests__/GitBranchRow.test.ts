import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import GitBranchRow from '@/components/git/GitBranchRow.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
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
        Trash2: true,
      },
    },
  })
}

describe('GitBranchRow', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
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

    it('does not emit switch when a text selection is active (drag-select ends with a click)', async () => {
      const wrapper = mountRow({ branch: makeBranch({ name: 'dev' }) })
      vi.spyOn(window, 'getSelection').mockReturnValue({
        toString: () => 'dev',
      } as unknown as Selection)

      await wrapper.find('.git-branch-row').trigger('click')

      expect(wrapper.emitted('switch')).toBeFalsy()
      vi.restoreAllMocks()
    })

    it('still emits switch when nothing is selected', async () => {
      const wrapper = mountRow({ branch: makeBranch({ name: 'dev' }) })
      vi.spyOn(window, 'getSelection').mockReturnValue({
        toString: () => '',
      } as unknown as Selection)

      await wrapper.find('.git-branch-row').trigger('click')

      expect(wrapper.emitted('switch')).toHaveLength(1)
      vi.restoreAllMocks()
    })
  })

  describe('delete button', () => {
    it('shows an enabled delete button when branch is not current and not default', () => {
      const wrapper = mountRow({ branch: makeBranch() })
      const btn = wrapper.find('.branch-action-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.attributes('disabled')).toBeUndefined()
      expect(btn.attributes('title')).toBe('git.manage.deleteBranch')
    })

    it('keeps the delete button visible but disabled when branch is current', () => {
      const wrapper = mountRow({ branch: makeBranch({ isCurrent: true }) })
      const btn = wrapper.find('.branch-action-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.attributes('disabled')).toBeDefined()
      expect(btn.attributes('title')).toBe('git.manage.cannotDeleteCurrent')
      expect(btn.classes()).toContain('is-disabled')
    })

    it('keeps the delete button visible but disabled when branch is default', () => {
      const wrapper = mountRow({ branch: makeBranch({ isDefault: true }) })
      const btn = wrapper.find('.branch-action-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.attributes('disabled')).toBeDefined()
      expect(btn.attributes('title')).toBe('git.manage.cannotDeleteDefault')
    })

    it('emits delete without switching when the delete button is clicked', async () => {
      const branch = makeBranch({ name: 'dev' })
      const wrapper = mountRow({ branch })
      await wrapper.find('.branch-action-btn').trigger('click')
      expect(wrapper.emitted('delete')).toBeTruthy()
      expect(wrapper.emitted('delete')![0][0]).toEqual(branch)
      expect(wrapper.emitted('switch')).toBeFalsy()
    })

    it('does not emit delete for a disabled (current) branch, even via a programmatic click', async () => {
      const wrapper = mountRow({ branch: makeBranch({ isCurrent: true }) })
      // `trigger` respects the `disabled` attribute and would pass vacuously.
      // Dispatch directly to prove the handler guards itself too (real browsers
      // do deliver programmatic clicks to disabled buttons).
      const el = wrapper.find('.branch-action-btn').element as HTMLButtonElement
      el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('delete')).toBeFalsy()
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
