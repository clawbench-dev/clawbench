import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import DiffDrawer from '../DiffDrawer.vue'

// Mock BottomSheet (teleported, complex to test inline)
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="mock-bottom-sheet" v-if="open"><slot name="header" /><slot /></div>',
    props: ['open', 'title', 'auto', 'transparentOverlay'],
    emits: ['close'],
  },
}))

// Mock useMarkdownDiff exports
vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  diffOldContent: { value: null },
  diffOldFilePath: { value: null },
  clearDiffMarkers: vi.fn(),
}))

// Mock store
vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { currentFile: { path: '/test/file.txt', content: 'current content' } },
    selectFile: vi.fn().mockResolvedValue(undefined),
  },
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: mockToastShow, dismiss: vi.fn() }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      git: {
        diffView: {
          modified: 'Modified',
          deleted: 'Deleted',
          added: 'Added',
          noDiffDetails: 'No diff details',
          undo: 'Undo',
          redo: 'Redo',
          undoSuccess: 'Undone',
          undoFailed: 'Undo failed',
          redoSuccess: 'Redone',
          redoFailed: 'Redo failed',
        },
      },
      common: {
        close: 'Close',
      },
    },
  },
})

function mountDrawer(props = {}) {
  return mount(DiffDrawer, {
    props: {
      visible: true,
      markerType: 'modified',
      ...props,
    },
    global: {
      plugins: [i18n],
    },
  })
}

describe('DiffDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('passes visible prop to BottomSheet', () => {
    const wrapper = mountDrawer({ visible: true })
    expect(wrapper.find('.mock-bottom-sheet').exists()).toBe(true)
  })

  it('passes transparentOverlay to BottomSheet', () => {
    const wrapper = mountDrawer({ visible: true })
    const bs = wrapper.findComponent({ name: 'BottomSheet' })
    expect(bs.props('transparentOverlay')).toBe(true)
  })

  it('shows title based on markerType', () => {
    const wrapper = mountDrawer({ markerType: 'modified' })
    expect(wrapper.find('.diff-drawer-title').text()).toBe('Modified')
  })

  it('shows deleted title for deleted markerType', () => {
    const wrapper = mountDrawer({ markerType: 'deleted' })
    expect(wrapper.find('.diff-drawer-title').text()).toBe('Deleted')
  })

  it('shows added title for added markerType', () => {
    const wrapper = mountDrawer({ markerType: 'added' })
    expect(wrapper.find('.diff-drawer-title').text()).toBe('Added')
  })

  it('shows empty message when no diff data', () => {
    const wrapper = mountDrawer({ charDiff: null, diffLines: undefined })
    expect(wrapper.find('.diff-drawer-empty').exists()).toBe(true)
  })

  it('renders diff table when diffLines provided', () => {
    const wrapper = mountDrawer({
      diffLines: [
        { type: 'ctx', oldLine: 1, newLine: 1, content: 'hello' },
        { type: 'del', oldLine: 2, newLine: null, content: 'world' },
        { type: 'add', oldLine: null, newLine: 2, content: 'universe' },
      ],
    })
    expect(wrapper.find('.diff-table').exists()).toBe(true)
    const rows = wrapper.findAll('.diff-line')
    expect(rows).toHaveLength(3)
    expect(rows[0].classes()).toContain('diff-line-ctx')
    expect(rows[1].classes()).toContain('diff-line-del')
    expect(rows[2].classes()).toContain('diff-line-add')
  })

  it('does not render line numbers or prefix in diff table', () => {
    const wrapper = mountDrawer({
      diffLines: [
        { type: 'del', oldLine: 2, newLine: null, content: 'world' },
      ],
    })
    expect(wrapper.find('.diff-linum').exists()).toBe(false)
    expect(wrapper.find('.diff-prefix').exists()).toBe(false)
  })

  it('renders inline char diff when charDiff provided without diffLines', () => {
    const wrapper = mountDrawer({
      charDiff: {
        oldText: 'hello world',
        newText: 'hello universe',
        changes: [
          { value: 'hello ', removed: false, added: false, count: 1 },
          { value: 'world', removed: true, added: false, count: 1 },
          { value: 'universe', removed: false, added: true, count: 1 },
        ],
      },
    })
    expect(wrapper.find('.diff-inline-view').exists()).toBe(true)
    const segments = wrapper.findAll('.diff-inline-view span')
    expect(segments).toHaveLength(3)
    expect(segments[1].classes()).toContain('diff-seg-del')
    expect(segments[2].classes()).toContain('diff-seg-add')
  })

  it('prefers diffLines over charDiff when both provided', () => {
    const wrapper = mountDrawer({
      diffLines: [
        { type: 'ctx', oldLine: 1, newLine: 1, content: 'same' },
      ],
      charDiff: {
        oldText: 'a',
        newText: 'b',
        changes: [{ value: 'a', removed: true, added: false, count: 1 }],
      },
    })
    expect(wrapper.find('.diff-table').exists()).toBe(true)
    expect(wrapper.find('.diff-inline-view').exists()).toBe(false)
  })

  it('applies ellipsis class via isEllipsis flag', () => {
    const wrapper = mountDrawer({
      diffLines: [
        { type: 'ctx', oldLine: 1, newLine: 1, content: 'hello' },
        { type: 'ctx', content: '⋯', oldLine: null, newLine: null, isEllipsis: true },
        { type: 'add', oldLine: null, newLine: 2, content: 'world' },
      ],
    })
    const rows = wrapper.findAll('.diff-line')
    expect(rows[0].classes()).not.toContain('diff-line-ellipsis')
    expect(rows[1].classes()).toContain('diff-line-ellipsis')
    expect(rows[2].classes()).not.toContain('diff-line-ellipsis')
  })

  it('hides Undo button when diffOldContent is null', () => {
    const wrapper = mountDrawer()
    expect(wrapper.findAll('.diff-action-btn').length).toBe(0)
  })

  it('shows Undo button when diffOldContent is set', async () => {
    const { diffOldContent } = await import('@/composables/useMarkdownDiff.ts')
    diffOldContent.value = 'old content'
    const wrapper = mountDrawer()
    const btns = wrapper.findAll('.diff-action-btn')
    expect(btns.length).toBe(1)
    expect(btns[0].text()).toBe('Undo')
    diffOldContent.value = null
  })

  it('calls handleUndo on Undo click and shows success toast', async () => {
    const { diffOldContent, diffOldFilePath } = await import('@/composables/useMarkdownDiff.ts')
    diffOldContent.value = 'old content'
    diffOldFilePath.value = '/test/file.txt'

    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as any)

    const wrapper = mountDrawer()
    const undoBtn = wrapper.findAll('.diff-action-btn')[0]
    await undoBtn.trigger('click')
    await nextTick()

    expect(globalThis.fetch).toHaveBeenCalledWith('/api/file/write', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ path: '/test/file.txt', content: 'old content' }),
    }))
    expect(mockToastShow).toHaveBeenCalledWith('Undone', { type: 'success' })

    diffOldContent.value = null
    diffOldFilePath.value = null
    vi.restoreAllMocks()
  })

  it('shows error toast on undo failure', async () => {
    const { diffOldContent, diffOldFilePath } = await import('@/composables/useMarkdownDiff.ts')
    diffOldContent.value = 'old content'
    diffOldFilePath.value = '/test/file.txt'

    vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: false } as any)

    const wrapper = mountDrawer()
    const undoBtn = wrapper.findAll('.diff-action-btn')[0]
    await undoBtn.trigger('click')
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Undo failed', { type: 'error' })

    diffOldContent.value = null
    diffOldFilePath.value = null
    vi.restoreAllMocks()
  })

  it('does not undo when file path mismatch', async () => {
    const { diffOldContent, diffOldFilePath } = await import('@/composables/useMarkdownDiff.ts')
    diffOldContent.value = 'old content'
    diffOldFilePath.value = '/different/file.txt'

    const fetchSpy = vi.spyOn(globalThis, 'fetch')

    const wrapper = mountDrawer()
    const undoBtn = wrapper.findAll('.diff-action-btn')[0]
    await undoBtn.trigger('click')

    expect(fetchSpy).not.toHaveBeenCalled()

    diffOldContent.value = null
    diffOldFilePath.value = null
    vi.restoreAllMocks()
  })
})
