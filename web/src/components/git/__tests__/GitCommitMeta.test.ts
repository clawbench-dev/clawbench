import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
  createI18n: () => ({
    global: { locale: { value: 'en' } },
    install() {},
  }),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  FILE_OPEN_ICON_SVG: '<svg class="file-open-icon"></svg>',
}))

import GitCommitMeta from '@/components/git/GitCommitMeta.vue'

function mountMeta(props: Record<string, unknown>) {
  return mount(GitCommitMeta, { props })
}

describe('GitCommitMeta — file path rows', () => {
  it('renders file name + path rows when filePath is present', () => {
    const wrapper = mountMeta({
      commit: { sha: 'abc1234567', author: 'me', date: new Date().toISOString(), msg: 'fix' },
      filePath: 'web/src/foo.ts',
    })
    const rows = wrapper.findAll('.diff-meta-row')
    const fileRow = rows[0]
    const pathRow = rows[1]
    expect(fileRow.find('.diff-meta-label').text()).toBe('git.commitMeta.file')
    expect(fileRow.find('.diff-meta-value').text()).toBe('foo.ts')
    expect(pathRow.find('.diff-meta-label').text()).toBe('git.commitMeta.path')
    expect(pathRow.find('.diff-meta-value').text()).toBe('web/src/foo.ts')
    expect(rows[2].find('.diff-meta-label').text()).toBe('SHA')
  })

  it('shows file rows for working-tree meta too', () => {
    const wrapper = mountMeta({
      commit: { isWT: true },
      isWorkingTree: true,
      filePath: 'README.md',
    })
    const fileRows = wrapper.findAll('.diff-meta-row')
    expect(fileRows).toHaveLength(3) // file + path + description
    expect(fileRows[0].find('.diff-meta-value').text()).toBe('README.md')
    expect(fileRows[1].find('.diff-meta-value').text()).toBe('README.md')
    expect(fileRows[2].find('.diff-meta-label').text()).toBe('git.commitMeta.description')
  })

  it('omits file rows when filePath is empty', () => {
    const wrapper = mountMeta({
      commit: { sha: 'abc1234567', author: 'me', date: new Date().toISOString(), msg: 'fix' },
    })
    expect(wrapper.find('.diff-meta-file-name').exists()).toBe(false)
    expect(wrapper.find('.diff-meta-file-path').exists()).toBe(false)
    expect(wrapper.text()).toContain('SHA')
  })
})

describe('GitCommitMeta — file annotations', () => {
  const PATH = 'docs/spec/core/chat-flow.md'

  function mountWithPath(filePath = PATH) {
    return mountMeta({
      commit: { sha: 'a313de64deadbeef', author: 'xulongzhe', date: new Date().toISOString(), msg: 'feat' },
      filePath,
    })
  }

  it('emits open-file with the path when the file-name row is clicked', async () => {
    const wrapper = mountWithPath()
    await wrapper.find('.diff-meta-file-name').trigger('click')
    expect(wrapper.emitted('open-file')).toEqual([[PATH]])
  })

  it('emits reveal-file with the path when the path row is clicked', async () => {
    const wrapper = mountWithPath()
    await wrapper.find('.diff-meta-file-path').trigger('click')
    expect(wrapper.emitted('reveal-file')).toEqual([[PATH]])
    // The row must not open the file — that is the other row's action.
    expect(wrapper.emitted('open-file')).toBeFalsy()
  })

  it('renders an explicit button per row, each wired to its own action', async () => {
    const wrapper = mountWithPath()
    const buttons = wrapper.findAll('.diff-meta-open-btn')
    expect(buttons).toHaveLength(2)

    // The buttons repeat the row actions so the affordance is discoverable
    // without hovering the label.
    await buttons[0].trigger('click')
    expect(wrapper.emitted('open-file')).toEqual([[PATH]])
    expect(wrapper.emitted('reveal-file')).toBeFalsy()

    await buttons[1].trigger('click')
    expect(wrapper.emitted('reveal-file')).toEqual([[PATH]])
    // Neither action ran twice.
    expect(wrapper.emitted('open-file')).toHaveLength(1)
  })

  it('renders an icon inside each button', () => {
    const wrapper = mountWithPath()
    expect(wrapper.findAll('.diff-meta-open-btn svg')).toHaveLength(2)
  })

  it('does not render annotation rows without a file path', () => {
    const wrapper = mountMeta({
      commit: { sha: 'abc1234567', author: 'me', date: new Date().toISOString(), msg: 'fix' },
    })
    expect(wrapper.find('.diff-meta-annotation').exists()).toBe(false)
    expect(wrapper.find('.diff-meta-open-btn').exists()).toBe(false)
  })
})
