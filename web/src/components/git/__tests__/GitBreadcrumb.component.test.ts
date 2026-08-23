import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import GitBreadcrumb from '@/components/git/GitBreadcrumb.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      git: {
        breadcrumb: {
          fileHistory: 'File history',
          commitList: 'Commits',
          workingTree: 'WT',
          openFile: 'Open',
        },
        manage: { title: 'Manage' },
      },
    },
  },
})

function mountCrumb(props: Record<string, unknown> = {}) {
  return mount(GitBreadcrumb, {
    props: { mode: 'project', currentView: 'commits', ...props },
    global: { plugins: [i18n] },
  })
}

describe('GitBreadcrumb — mount', () => {
  it('mounts without errors', () => {
    const wrapper = mountCrumb()
    expect(wrapper.find('.git-breadcrumb').exists()).toBe(true)
  })

  it('renders commit-list label in project mode', () => {
    const wrapper = mountCrumb({ mode: 'project', currentView: 'commits' })
    expect(wrapper.text()).toContain('Commits')
  })

  it('renders file-history label in file mode', () => {
    const wrapper = mountCrumb({ mode: 'file', currentView: 'commits' })
    expect(wrapper.text()).toContain('File history')
  })

  it('emits navigate("commits") when root crumb is clicked', async () => {
    const wrapper = mountCrumb({ mode: 'project', currentView: 'files' })
    const root = wrapper.find('.git-crumb')
    await root.trigger('click')
    expect(wrapper.emitted('navigate')?.[0]).toEqual(['commits'])
  })

  it('does not emit navigate when already on commits view', async () => {
    const wrapper = mountCrumb({ mode: 'project', currentView: 'commits' })
    await wrapper.find('.git-crumb').trigger('click')
    expect(wrapper.emitted('navigate')).toBeFalsy()
  })

  it('renders manage crumb when currentView is manage', () => {
    const wrapper = mountCrumb({ currentView: 'manage' })
    expect(wrapper.text()).toContain('Manage')
  })

  it('renders selected commit SHA prefix when commit selected', () => {
    const wrapper = mountCrumb({
      currentView: 'files',
      selectedCommit: { sha: 'abcdef1234567890', msg: 'test' },
    })
    expect(wrapper.text()).toContain('abcdef1')
  })

  it('renders working tree label for isWT commit', () => {
    const wrapper = mountCrumb({
      currentView: 'files',
      selectedCommit: { sha: 'HEAD', isWT: true },
    })
    expect(wrapper.text()).toContain('WT')
  })

  it('renders file crumb in project mode diff view', () => {
    const wrapper = mountCrumb({
      currentView: 'diff',
      selectedCommit: { sha: 'abcdef1' },
      selectedFilePath: 'src/components/foo.vue',
    })
    expect(wrapper.text()).toContain('foo.vue')
  })

  it('hides file crumb in file mode even with selectedFilePath', () => {
    const wrapper = mountCrumb({
      mode: 'file',
      currentView: 'diff',
      selectedCommit: { sha: 'abcdef1' },
      selectedFilePath: 'src/components/foo.vue',
    })
    expect(wrapper.text()).not.toContain('foo.vue')
  })

  it('emits open-file with selectedFilePath when file open button is clicked', async () => {
    const wrapper = mountCrumb({
      mode: 'project',
      currentView: 'diff',
      selectedCommit: { sha: 'abcdef1' },
      selectedFilePath: 'src/foo.ts',
    })
    const btn = wrapper.find('.git-file-open-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(wrapper.emitted('open-file')?.[0]).toEqual(['src/foo.ts'])
  })

  it('emits navigate("files") in project mode when commit crumb clicked in diff view', async () => {
    const wrapper = mountCrumb({
      mode: 'project',
      currentView: 'diff',
      selectedCommit: { sha: 'abcdef1' },
      selectedFilePath: 'src/foo.ts',
    })
    const crumbs = wrapper.findAll('.git-crumb')
    const commitCrumb = crumbs[1]
    await commitCrumb.trigger('click')
    expect(wrapper.emitted('navigate')?.[0]).toEqual(['files'])
  })

  it('emits navigate("commits") in file mode when commit crumb clicked in diff view', async () => {
    const wrapper = mountCrumb({
      mode: 'file',
      currentView: 'diff',
      selectedCommit: { sha: 'abcdef1' },
    })
    const crumbs = wrapper.findAll('.git-crumb')
    await crumbs[1].trigger('click')
    expect(wrapper.emitted('navigate')?.[0]).toEqual(['commits'])
  })
})