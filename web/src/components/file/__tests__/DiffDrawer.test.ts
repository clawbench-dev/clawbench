import { describe, expect, it, vi } from 'vitest'
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
  clearDiffMarkers: vi.fn(),
}))

// Mock store
vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { currentFile: { path: '/test/file.txt' } },
    selectFile: vi.fn(),
  },
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
          revert: 'Revert',
          revertConfirm: 'Revert to the previous content?',
          revertSuccess: 'Reverted',
          revertFailed: 'Revert failed',
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

  it('hides revert button when diffOldContent is null', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.diff-revert-btn').exists()).toBe(false)
  })

  it('shows revert button when diffOldContent is set', async () => {
    const { diffOldContent } = await import('@/composables/useMarkdownDiff.ts')
    diffOldContent.value = 'old content'
    const wrapper = mountDrawer()
    expect(wrapper.find('.diff-revert-btn').exists()).toBe(true)
    expect(wrapper.find('.diff-revert-btn').text()).toBe('Revert')
    diffOldContent.value = null
  })
})
