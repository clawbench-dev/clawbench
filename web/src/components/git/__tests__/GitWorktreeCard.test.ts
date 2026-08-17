import { describe, expect, it, vi } from 'vitest'
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
  it('shows a main badge for the main worktree', () => {
    const wrapper = mountCard(makeWorktree({ isMain: true }))
    expect(wrapper.find('.wt-badge-main').exists()).toBe(true)
  })

  it('does not show a main badge for a linked worktree', () => {
    const wrapper = mountCard(makeWorktree({ isMain: false }))
    expect(wrapper.find('.wt-badge-main').exists()).toBe(false)
  })

  it('shows delete button for a non-current worktree', () => {
    const wrapper = mountCard(makeWorktree())
    expect(wrapper.findAll('.wt-action-btn').length).toBe(1)
    expect(wrapper.find('.wt-action-delete').exists()).toBe(true)
  })

  it('hides action buttons for the current worktree', () => {
    const wrapper = mountCard(makeWorktree({ isCurrent: true }))
    expect(wrapper.find('.wt-action-btn').exists()).toBe(false)
  })

  it('keeps delete button when worktree is missing', () => {
    const wrapper = mountCard(makeWorktree({ missing: true }))
    const buttons = wrapper.findAll('.wt-action-btn')
    // delete button still available (missing worktree can be removed)
    expect(buttons.length).toBe(1)
    expect(buttons[0].classes()).toContain('wt-action-delete')
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
})
