import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import GitBranchRow from '@/components/git/GitBranchRow.vue'
import { installDragClickGuard } from '@/utils/dragClickGuard'

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

    /**
     * End-to-end proof that the row is still protected after its own
     * `hasActiveTextSelection()` guard was removed in favour of the single
     * document-level dragClickGuard. Without the guard installed this test
     * fails — which is exactly the regression it exists to catch, since the row
     * no longer checks the selection itself.
     */
    it('does not emit switch when the click was really a drag-select', async () => {
      // attachTo is required: the guard listens on `document`, and a detached
      // wrapper's events never bubble out of the test container.
      const wrapper = mount(GitBranchRow, {
        props: { branch: makeBranch({ name: 'feature/login' }) },
        global: { stubs: { GitBranch: true, Trash2: true } },
        attachTo: document.body,
      })
      const row = wrapper.find('.git-branch-row').element as HTMLElement
      const dispose = installDragClickGuard()

      try {
        const down = new Event('pointerdown', { bubbles: true, cancelable: true })
        Object.assign(down, { clientX: 10, clientY: 10, pointerType: 'mouse', button: 0 })
        row.dispatchEvent(down)

        // Dragged 40px to select the branch name, then released.
        row.dispatchEvent(new MouseEvent('click', {
          bubbles: true, cancelable: true, clientX: 50, clientY: 10, detail: 1,
        }))

        expect(wrapper.emitted('switch')).toBeFalsy()

        // A genuine click on the same row still works.
        const down2 = new Event('pointerdown', { bubbles: true, cancelable: true })
        Object.assign(down2, { clientX: 10, clientY: 10, pointerType: 'mouse', button: 0 })
        row.dispatchEvent(down2)
        row.dispatchEvent(new MouseEvent('click', {
          bubbles: true, cancelable: true, clientX: 10, clientY: 10, detail: 1,
        }))

        expect(wrapper.emitted('switch')).toHaveLength(1)
      } finally {
        dispose()
        wrapper.unmount()
      }
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
