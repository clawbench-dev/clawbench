import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import DiffDrawer from '../DiffDrawer.vue'

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
  it('renders when visible is true', () => {
    const wrapper = mountDrawer({ visible: true })
    expect(wrapper.find('.diff-drawer').exists()).toBe(true)
  })

  it('does not render when visible is false', () => {
    const wrapper = mountDrawer({ visible: false })
    expect(wrapper.find('.diff-drawer').exists()).toBe(false)
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

  it('emits close when close button is clicked', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.diff-drawer-close').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
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
})
