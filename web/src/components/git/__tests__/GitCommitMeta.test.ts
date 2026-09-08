import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
  createI18n: () => ({
    global: { locale: { value: 'en' } },
    install() {},
  }),
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
