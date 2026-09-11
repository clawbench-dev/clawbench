import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CompletionMenu from '../CompletionMenu.vue'
import { SOURCE_META } from '@/utils/completionSources.ts'
import type { CompletionItem } from '@/utils/completionMatch.ts'

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    messages: {
      en: {
        chat: {
          completion: {
            source: {
              recentOpen: 'Recent',
              currentDir: 'Current dir',
              recentRef: 'Referenced',
              recentUpload: 'Uploaded',
              recentShare: 'Shared',
              clawbench: 'Built-in',
              agent: 'Agent',
            },
          },
        },
      },
    },
  })
}

function mountMenu(items: CompletionItem[], activeIndex = 0) {
  return mount(CompletionMenu, {
    props: { items, activeIndex, show: true, targetElement: null },
    global: {
      plugins: [makeI18n()],
      stubs: { Teleport: { template: '<div><slot/></div>' } },
    },
  })
}

const sample: CompletionItem[] = [
  { key: 'src/chat/Input.vue', label: 'Input.vue', description: 'src/chat', source: 'current-dir' },
  { key: 'a.ts', label: 'a.ts', description: '', source: 'recent-open' },
]

describe('CompletionMenu', () => {
  it('renders one row per item', () => {
    const wrapper = mountMenu(sample)
    expect(wrapper.findAll('.completion-item')).toHaveLength(2)
  })

  it('shows basename as the main label and the directory as the description', () => {
    const wrapper = mountMenu(sample)
    const first = wrapper.findAll('.completion-item')[0]
    expect(first.find('.completion-label').text()).toBe('Input.vue')
    expect(first.find('.completion-desc').text()).toBe('src/chat')
  })

  it('renders a coloured source label for every source', () => {
    const wrapper = mountMenu(sample)
    const first = wrapper.findAll('.completion-item')[0]
    const tag = first.find('.completion-source')
    expect(tag.exists()).toBe(true)
    expect(tag.text()).toBe('Current dir')
    expect(tag.classes()).toContain('completion-source--current-dir')
  })

  it('renders the source icon for each known source', () => {
    const wrapper = mountMenu(sample)
    const first = wrapper.findAll('.completion-item')[0]
    expect(first.find('.completion-source-icon').exists()).toBe(true)
  })

  it('omits the row icon when the item has none', () => {
    const wrapper = mountMenu(sample)
    expect(wrapper.find('.completion-item-icon').exists()).toBe(false)
  })

  it('renders the row icon when provided', () => {
    const items: CompletionItem[] = [{ ...sample[0], icon: 'FileIcon' }]
    const wrapper = mount(CompletionMenu, {
      props: { items, activeIndex: 0, show: true, targetElement: null },
      global: {
        plugins: [makeI18n()],
        stubs: {
          Teleport: { template: '<div><slot/></div>' },
          FileIcon: { template: '<span class="file-icon-stub" />' },
        },
      },
    })
    expect(wrapper.find('.file-icon-stub').exists()).toBe(true)
  })

  it('highlights the matched characters by position', () => {
    const items: CompletionItem[] = [
      { key: 'src/main.ts', label: 'main.ts', description: 'src', source: 'current-dir', positions: [0, 1] },
    ]
    const wrapper = mountMenu(items)
    const marks = wrapper.findAll('.completion-label mark')
    expect(marks).toHaveLength(2)
    expect(marks.map(m => m.text()).join('')).toBe('ma')
  })

  it('marks the active row', () => {
    const wrapper = mountMenu(sample, 1)
    const rows = wrapper.findAll('.completion-item')
    expect(rows[1].classes()).toContain('completion-item--active')
    expect(rows[0].classes()).not.toContain('completion-item--active')
  })

  it('exposes a data-completion-idx attribute for scroll-into-view', () => {
    const wrapper = mountMenu(sample)
    expect(wrapper.findAll('.completion-item')[1].attributes('data-completion-idx')).toBe('1')
  })

  it('emits select with the item on mousedown', async () => {
    const wrapper = mountMenu(sample)
    await wrapper.findAll('.completion-item')[1].trigger('mousedown')
    expect(wrapper.emitted('select')).toBeTruthy()
    expect(wrapper.emitted('select')![0]).toEqual([sample[1]])
  })

  it('renders nothing when items are empty', () => {
    const wrapper = mountMenu([])
    expect(wrapper.findAll('.completion-item')).toHaveLength(0)
  })
})

describe('SOURCE_META', () => {
  it('covers all seven completion sources', () => {
    expect(Object.keys(SOURCE_META).sort()).toEqual([
      'agent',
      'clawbench',
      'current-dir',
      'recent-open',
      'recent-ref',
      'recent-share',
      'recent-upload',
    ])
  })

  it('gives every source an icon, a colour and a label key', () => {
    for (const meta of Object.values(SOURCE_META)) {
      expect(meta.icon).toBeTruthy()
      expect(meta.color).toBeTruthy()
      expect(meta.labelKey).toBeTruthy()
    }
  })
})
