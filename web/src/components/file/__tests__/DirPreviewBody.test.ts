import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import DirPreviewBody from '@/components/file/DirPreviewBody.vue'

vi.mock('vue-i18n', () => ({
  // Interpolate params so assertions can see the resolved count, e.g.
  // t('...count', { n: 2 }) -> '...count 2'.
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) =>
      params ? `${k} ${Object.values(params).join(' ')}` : k,
  }),
}))

// Keep the real isThumbable contract (it decides which entries get a thumbnail)
// but pin the URL shape so assertions do not depend on joinPath internals.
vi.mock('@/utils/fileManager', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/fileManager')>()
  return {
    ...actual,
    buildThumbUrl: (dir: string, name: string, w = 200) =>
      `/api/file/thumb?path=${dir}/${name}&w=${w}`,
  }
})

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
    // Target Close explicitly — it is no longer the only toolbar button.
    const closeBtn = wrapper.findAll('.dir-preview-actions .dir-preview-btn')[1]
    await closeBtn.trigger('click')
    expect(wrapper.emitted('closed')).toBeTruthy()
  })

  it('renders an open-directory button using the same icon as open-file', () => {
    // "Open directory" lives in the toolbar next to Close and uses ExternalLink,
    // the same icon the file card's "open file" control uses.
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }] })
    const btns = wrapper.findAll('.dir-preview-actions .dir-preview-btn')
    expect(btns).toHaveLength(2)
    expect(btns[0].attributes('title')).toBe('file.codePreview.revealInTree')
    // Both controls share the same icon component (stubbed here as svg-less, so
    // assert on the icon element the component renders).
    expect(btns[0].find('svg').exists()).toBe(true)
    expect(btns[1].attributes('title')).toBe('file.dirPreview.close')
  })

  it('emits open-self from the open-directory button, not closed', async () => {
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }] })
    const openBtn = wrapper.findAll('.dir-preview-actions .dir-preview-btn')[0]
    await openBtn.trigger('click')
    expect(wrapper.emitted('open-self')).toHaveLength(1)
    expect(wrapper.emitted('closed')).toBeFalsy()
  })

  it('omits the open-directory button in chromeless mode with the toolbar', () => {
    // chromeless suppresses the whole toolbar, so both controls go with it.
    const wrapper = mountBody({ entries: [{ name: 'a.ts', type: 'file' }], chromeless: true })
    expect(wrapper.find('.dir-preview-actions').exists()).toBe(false)
    expect(wrapper.emitted('open-self')).toBeFalsy()
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

  it('renders a thumbnail for a thumbable image when the directory path is known', () => {
    const wrapper = mountBody({
      dirPath: 'assets',
      entries: [
        { name: 'logo.png', type: 'image' },
        { name: 'main.ts', type: 'file' },
      ],
    })
    const thumb = wrapper.find('.dir-preview-thumb')
    expect(thumb.exists()).toBe(true)
    // Same endpoint the file manager list uses, built from the directory path.
    expect(thumb.attributes('src')).toContain('/api/file/thumb?path=assets/logo.png')
    expect(thumb.attributes('loading')).toBe('lazy')
    // Non-image entries keep their type icon.
    expect(wrapper.findAll('.dir-preview-icon')).toHaveLength(1)
  })

  it('falls back to the type icon when the directory path is unknown', () => {
    // Without dirPath a thumbnail URL cannot be built, so no <img> is rendered.
    const wrapper = mountBody({ entries: [{ name: 'logo.png', type: 'image' }] })
    expect(wrapper.find('.dir-preview-thumb').exists()).toBe(false)
    expect(wrapper.find('.dir-preview-icon').exists()).toBe(true)
  })

  it('falls back to the type icon after a thumbnail fails to load', async () => {
    const wrapper = mountBody({
      dirPath: 'assets',
      entries: [{ name: 'broken.png', type: 'image' }],
    })
    expect(wrapper.find('.dir-preview-thumb').exists()).toBe(true)
    await wrapper.find('.dir-preview-thumb').trigger('error')
    // The failed entry swaps to its icon instead of retrying the request.
    expect(wrapper.find('.dir-preview-thumb').exists()).toBe(false)
    expect(wrapper.find('.dir-preview-icon').exists()).toBe(true)
  })

  it('does not request thumbnails for directories', () => {
    const wrapper = mountBody({
      dirPath: 'assets',
      entries: [{ name: 'sub', type: 'dir' }],
    })
    expect(wrapper.find('.dir-preview-thumb').exists()).toBe(false)
    expect(wrapper.find('.dir-preview-icon').exists()).toBe(true)
  })

  it('scopes thumbnail failures to the full path, not the bare name', async () => {
    // The pane reuses ONE instance as the user navigates between directories,
    // and two directories can each hold `logo.png`. A failure recorded under the
    // bare name would wrongly suppress the valid thumbnail after moving to a
    // sibling directory, so the error key must include the directory.
    const wrapper = mountBody({
      dirPath: 'assets',
      entries: [{ name: 'logo.png', type: 'image' }],
    })
    await wrapper.find('.dir-preview-thumb').trigger('error')
    expect(wrapper.find('.dir-preview-thumb').exists()).toBe(false)

    // Same instance, different directory, same file name → thumbnail returns.
    await wrapper.setProps({ dirPath: 'public' })
    expect(wrapper.find('.dir-preview-thumb').exists()).toBe(true)
    expect(wrapper.find('.dir-preview-thumb').attributes('src')).toContain('public/logo.png')
  })

  it('sizes the listing for comfortable reading (enlarged entries)', () => {
    // Enlarged at the user's request: 16px icon / 12px text / 4x6px padding /
    // 150px tracks were too small. jsdom does not load SFC <style>, so assert
    // against the source.
    const src = readFileSync(
      resolve(__dirname, '../../../components/file/DirPreviewBody.vue'),
      'utf8',
    )
    const grid = src.match(/\.dir-preview-grid\s*\{([^}]*)\}/)
    expect(grid, '.dir-preview-grid rule must exist').not.toBeNull()
    expect(grid![1]).toMatch(/minmax\(180px/)

    const item = src.match(/\.dir-preview-item\s*\{([^}]*)\}/)
    expect(item, '.dir-preview-item rule must exist').not.toBeNull()
    expect(item![1]).toMatch(/font-size:\s*var\(--font-size-md\)/)
    expect(item![1]).toMatch(/padding:\s*var\(--space-3\)\s*var\(--space-4\)/)

    // Icon size is a prop on FileIcon, so assert the markup.
    expect(src).toMatch(/:size="20"/)

    const thumb = src.match(/\.dir-preview-thumb\s*\{([^}]*)\}/)
    expect(thumb, '.dir-preview-thumb rule must exist').not.toBeNull()
    expect(thumb![1]).toMatch(/width:\s*20px/)
    expect(thumb![1]).toMatch(/height:\s*20px/)
  })
})
