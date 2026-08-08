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
  it('shows switch and delete buttons for a non-current worktree', () => {
    const wrapper = mountCard(makeWorktree())
    expect(wrapper.find('.wt-action-btn').exists()).toBe(true)
    expect(wrapper.findAll('.wt-action-btn').length).toBe(2)
  })

  it('hides switch and delete buttons for the current worktree', () => {
    const wrapper = mountCard(makeWorktree({ isCurrent: true }))
    expect(wrapper.find('.wt-action-btn').exists()).toBe(false)
  })

  it('hides switch button but keeps delete when worktree is missing', () => {
    const wrapper = mountCard(makeWorktree({ missing: true }))
    const buttons = wrapper.findAll('.wt-action-btn')
    // delete button still available (missing worktree can be removed)
    expect(buttons.length).toBe(1)
    expect(buttons[0].classes()).toContain('wt-action-delete')
  })

  it('emits switch when the switch button is clicked', async () => {
    const wt = makeWorktree()
    const wrapper = mountCard(wt)
    await wrapper.find('.wt-action-btn').trigger('click')
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
