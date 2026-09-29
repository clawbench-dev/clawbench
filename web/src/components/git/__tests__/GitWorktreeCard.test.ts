import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import GitWorktreeCard from '@/components/git/GitWorktreeCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const makeWorktree = (overrides: Record<string, unknown> = {}) => ({
  path: '/repo/.worktrees/feature-a',
  branch: 'feature-a',
  isCurrent: false,
  isMain: false,
  dirty: false,
  locked: false,
  missing: false,
  changeCount: 0,
  untrackedCount: 0,
  ...overrides,
})

function mountCard(worktree: Record<string, unknown>) {
  return mount(GitWorktreeCard, {
    props: { worktree },
    global: {
      stubs: {
        FolderTree: true,
        LogIn: true,
        Trash2: true,
      },
    },
  })
}

describe('GitWorktreeCard inline actions', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => { vi.useRealTimers() })

  it('shows a main badge for the main worktree', () => {
    const wrapper = mountCard(makeWorktree({ isMain: true }))
    expect(wrapper.find('.wt-badge-main').exists()).toBe(true)
  })

  it('does not show a main badge for a linked worktree', () => {
    const wrapper = mountCard(makeWorktree({ isMain: false }))
    expect(wrapper.find('.wt-badge-main').exists()).toBe(false)
  })

  it('shows an enabled delete button for a non-current worktree', () => {
    const wrapper = mountCard(makeWorktree())
    expect(wrapper.findAll('.wt-action-btn').length).toBe(1)
    const btn = wrapper.find('.wt-action-delete')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('keeps the delete button visible but disabled for the current worktree', () => {
    const wrapper = mountCard(makeWorktree({ isCurrent: true }))
    const btn = wrapper.find('.wt-action-delete')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('title')).toBe('git.manage.cannotDeleteCurrentWorktree')
    expect(btn.classes()).toContain('is-disabled')
  })

  it('keeps the delete button visible but disabled for the main worktree', () => {
    // git refuses to remove the main working tree even with --force.
    const wrapper = mountCard(makeWorktree({ isMain: true }))
    const btn = wrapper.find('.wt-action-delete')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('title')).toBe('git.manage.cannotDeleteMainWorktree')
  })

  it('keeps the delete button visible but disabled for a locked worktree', () => {
    // git needs `remove -f -f`; the backend only ever sends a single -f.
    const wrapper = mountCard(makeWorktree({ locked: true }))
    const btn = wrapper.find('.wt-action-delete')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.attributes('title')).toBe('git.manage.cannotDeleteLockedWorktree')
  })

  it('keeps delete enabled when worktree is missing', () => {
    const wrapper = mountCard(makeWorktree({ missing: true }))
    const btn = wrapper.find('.wt-action-delete')
    // missing worktrees still remove cleanly
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('keeps delete enabled when worktree is dirty', () => {
    const wrapper = mountCard(makeWorktree({ dirty: true, changeCount: 2 }))
    const btn = wrapper.find('.wt-action-delete')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('emits switch when the row is clicked', async () => {
    const wt = makeWorktree()
    const wrapper = mountCard(wt)
    await wrapper.find('.git-worktree-row').trigger('click')
    expect(wrapper.emitted('switch')).toBeTruthy()
    expect(wrapper.emitted('switch')![0][0]).toEqual(wt)
  })

  it('emits delete when the delete button is clicked without also switching', async () => {
    const wt = makeWorktree()
    const wrapper = mountCard(wt)
    await wrapper.find('.wt-action-delete').trigger('click')
    expect(wrapper.emitted('delete')).toBeTruthy()
    expect(wrapper.emitted('delete')![0][0]).toEqual(wt)
    expect(wrapper.emitted('switch')).toBeFalsy()
  })

  it('does not emit delete for a disabled (current) worktree, even via a programmatic click', async () => {
    const wrapper = mountCard(makeWorktree({ isCurrent: true }))
    // `trigger` respects the `disabled` attribute and would pass vacuously.
    // Dispatch directly to prove the handler guards itself too (real browsers
    // do deliver programmatic clicks to disabled buttons).
    const el = wrapper.find('.wt-action-delete').element as HTMLButtonElement
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('delete')).toBeFalsy()
  })
})
