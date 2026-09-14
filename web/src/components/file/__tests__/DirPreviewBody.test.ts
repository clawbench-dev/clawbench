import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import DirPreviewBody from '@/components/file/DirPreviewBody.vue'

vi.mock('vue-i18n', () => ({
  // Interpolate params so assertions can see the resolved count, e.g.
  // t('...count', { n: 2 }) -> '...count 2'.
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) =>
      params ? `${k} ${Object.values(params).join(' ')}` : k,
  }),
}))

const FileIconStub = { name: 'FileIcon', props: ['path', 'isDir', 'size'], template: '<i class="file-icon" />' }
const LoadingStub = { name: 'LoadingIndicator', template: '<i class="loading" />' }

function mountBody(props: Record<string, unknown> = {}) {
  return mount(DirPreviewBody, {
    props: {
      entries: [],
      loading: false,
      error: false,
      visible: () => true,
      dirName: 'src',
      ...props,
    },
    global: {
      stubs: {
        FileIcon: FileIconStub,
        LoadingIndicator: LoadingStub,
      },
    },
  })
}

describe('DirPreviewBody', () => {
  it('renders one item per visible entry', () => {
    const wrapper = mountBody({
      entries: [
        { name: 'src', type: 'dir' },
        { name: 'main.ts', type: 'file' },
      ],
    })
    const items = wrapper.findAll('.dir-preview-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('src')
    expect(items[1].text()).toContain('main.ts')
  })

  it('applies the visible() predicate (hidden-file toggle)', () => {
    const wrapper = mountBody({
      entries: [
        { name: '.env', type: 'file' },
        { name: 'index.ts', type: 'file' },
      ],
      visible: (e: { name: string }) => !e.name.startsWith('.'),
    })
    const names = wrapper.findAll('.dir-preview-item').map(i => i.text())
    expect(names).toEqual(['index.ts'])
  })

  it('marks directories distinctly', () => {
    const wrapper = mountBody({ entries: [{ name: 'src', type: 'dir' }, { name: 'a.ts', type: 'file' }] })
    const items = wrapper.findAll('.dir-preview-item')
    expect(items[0].classes()).toContain('is-dir')
    expect(items[1].classes()).not.toContain('is-dir')
  })

  it('emits open-dir when a directory is clicked', async () => {
    const wrapper = mountBody({ entries: [{ name: 'src', type: 'dir' }] })
    await wrapper.find('.dir-preview-item').trigger('click')
    expect(wrapper.emitted('open-dir')![0]).toEqual(['src'])
    expect(wrapper.emitted('open-file')).toBeFalsy()
  })

  it('emits open-file when a file is clicked', async () => {
    const wrapper = mountBody({ entries: [{ name: 'main.ts', type: 'file' }] })
    await wrapper.find('.dir-preview-item').trigger('click')
    expect(wrapper.emitted('open-file')![0]).toEqual(['main.ts'])
    expect(wrapper.emitted('open-dir')).toBeFalsy()
  })

  it('treats an image entry as a file, not a directory', async () => {
    const wrapper = mountBody({ entries: [{ name: 'logo.png', type: 'image' }] })
    await wrapper.find('.dir-preview-item').trigger('click')
    expect(wrapper.emitted('open-file')![0]).toEqual(['logo.png'])
  })

  it('shows the empty state when there are no visible entries', () => {
    const wrapper = mountBody({ entries: [] })
    expect(wrapper.find('.dir-preview-state').exists()).toBe(true)
    expect(wrapper.find('.dir-preview-grid').exists()).toBe(false)
  })

  it('shows the error state on failure', () => {
    const wrapper = mountBody({ error: true })
    expect(wrapper.find('.dir-preview-error').exists()).toBe(true)
  })

  it('shows a spinner only while loading with nothing to show yet', () => {
    const withEntries = mountBody({ loading: true, entries: [{ name: 'a.ts', type: 'file' }] })
    expect(withEntries.find('.loading').exists()).toBe(false)

    const withoutEntries = mountBody({ loading: true, entries: [] })
    expect(withoutEntries.find('.loading').exists()).toBe(true)
  })

  it('emits closed from the toolbar close button', async () => {
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }] })
    await wrapper.find('.dir-preview-btn').trigger('click')
    expect(wrapper.emitted('closed')).toBeTruthy()
  })

  it('renders a toolbar with the directory name and visible entry count', () => {
    const wrapper = mountBody({
      dirName: 'components',
      entries: [
        { name: 'a.ts', type: 'file' },
        { name: 'b.ts', type: 'file' },
        { name: '.hidden', type: 'file' },
      ],
      visible: (e: { name: string }) => !e.name.startsWith('.'),
    })
    const meta = wrapper.find('.dir-preview-meta')
    expect(meta.exists()).toBe(true)
    expect(meta.find('.dir-preview-title').text()).toBe('components')
    // Count reflects VISIBLE entries (hidden ones excluded), matching the grid.
    expect(meta.find('.dir-preview-count').text()).toContain('2')
    expect(wrapper.findAll('.dir-preview-item')).toHaveLength(2)
  })

  it('keeps the toolbar outside the scroll pane so it never scrolls away', () => {
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }] })
    const meta = wrapper.find('.dir-preview-meta').element
    const scroll = wrapper.find('.dir-preview-scroll').element
    // The toolbar is a sibling of the scroll pane, not inside it.
    expect(scroll.contains(meta)).toBe(false)
  })

  it('renders a symlink badge only for symlinked entries', () => {
    const wrapper = mountBody({
      entries: [
        { name: 'link', type: 'dir', symlink: true },
        { name: 'plain', type: 'dir' },
      ],
    })
    const items = wrapper.findAll('.dir-preview-item')
    expect(items[0].find('.dir-preview-symlink').exists()).toBe(true)
    expect(items[1].find('.dir-preview-symlink').exists()).toBe(false)
  })

  it('uses an auto-fill grid so the column count follows the pane width', () => {
    // The multi-column reflow is CSS-driven; assert the grid is in place and
    // that the class contract the stylesheet relies on is present.
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }] })
    expect(wrapper.find('.dir-preview-grid').exists()).toBe(true)
  })

  it('omits its own toolbar when the host already provides one (chromeless)', () => {
    // The floating preview card renders a title row AND a meta row; without
    // this the pane would stack a third bar (measured 31px of wasted height).
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }], chromeless: true })
    expect(wrapper.find('.dir-preview-meta').exists()).toBe(false)
    // The listing itself is untouched.
    expect(wrapper.find('.dir-preview-grid').exists()).toBe(true)
    expect(wrapper.findAll('.dir-preview-item')).toHaveLength(1)
  })

  it('keeps its toolbar by default (the docked pane relies on it)', () => {
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }] })
    expect(wrapper.find('.dir-preview-meta').exists()).toBe(true)
  })
})
