import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GitTagList from '@/components/git/GitTagList.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

function mountList(tags: Array<Record<string, unknown>> = []) {
  return mount(GitTagList, {
    props: { tags },
    global: {
      stubs: {
        Tag: true,
        Trash2: true,
      },
    },
  })
}

describe('GitTagList inline delete', () => {
  it('renders a delete button for each tag', () => {
    const wrapper = mountList([
      { name: 'v1.0', msg: 'release', date: '2025-01-15 10:00:00 +0800' },
      { name: 'v2.0', date: '2025-02-01 08:30:00 +0800' },
    ])
    expect(wrapper.findAll('.tag-action-btn').length).toBe(2)
  })

  it('emits delete-tag on delete button click without switching', async () => {
    const tag = { name: 'v1.0', date: '2025-01-15 10:00:00 +0800' }
    const wrapper = mountList([tag])
    await wrapper.find('.tag-action-btn').trigger('click')
    expect(wrapper.emitted('delete-tag')).toBeTruthy()
    expect(wrapper.emitted('delete-tag')![0][0]).toEqual(tag)
    expect(wrapper.emitted('switch-tag')).toBeFalsy()
  })

  it('emits switch-tag when the row is clicked', async () => {
    const tag = { name: 'v1.0', date: '2025-01-15 10:00:00 +0800' }
    const wrapper = mountList([tag])
    await wrapper.find('.tag-row').trigger('click')
    expect(wrapper.emitted('switch-tag')).toBeTruthy()
    expect(wrapper.emitted('switch-tag')![0][0]).toEqual(tag)
  })

  it('sorts tags by date, most recent first', () => {
    const wrapper = mountList([
      { name: 'v1.0', date: '2025-01-15 10:00:00 +0800' },
      { name: 'v3.0', date: '2025-03-01 10:00:00 +0800' },
      { name: 'v2.0', date: '2025-02-01 10:00:00 +0800' },
    ])
    const names = wrapper.findAll('.tag-name').map((n) => n.text())
    expect(names).toEqual(['v3.0', 'v2.0', 'v1.0'])
  })

  it('places tags without a date after dated tags', () => {
    const wrapper = mountList([
      { name: 'v1.0', date: '2025-01-15 10:00:00 +0800' },
      { name: 'v0.9' },
    ])
    const names = wrapper.findAll('.tag-name').map((n) => n.text())
    expect(names).toEqual(['v1.0', 'v0.9'])
  })
})
