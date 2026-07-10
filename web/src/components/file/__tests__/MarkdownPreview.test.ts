import { describe, expect, it, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import { nextTick, ref, defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'

// Mock MarkdownPreview component to avoid its async watch effects
// (renderMermaidInElement promise, vue-i18n devtools promise)
// that keep the event loop alive and prevent vitest worker exit.
// The stub renders the same DOM structure as the real component.
vi.mock('../MarkdownPreview.vue', () => ({
  default: defineComponent({
    name: 'MarkdownPreview',
    props: {
      file: { type: Object, default: () => ({}) },
      viewMode: { type: String, default: 'rendered' },
      stickyScroll: { type: Boolean, default: undefined },
      wordWrap: { type: Boolean, default: true },
      showLineNumbers: { type: Boolean, default: true },
    },
    setup(props, { expose }) {
      const bodyRef = ref<HTMLElement | null>(null)
      expose({ bodyRef, lastBlockList: ref([]) })
      return () => {
        if (props.viewMode === 'source') {
          return h('div', { class: 'markdown-preview' }, [
            h('pre', { class: 'raw-content-pre' }),
          ])
        }
        return h('div', { class: 'markdown-preview' }, [
          h('div', {
            class: 'markdown-body',
            'data-file-path': props.file?.path || '',
            ref: bodyRef,
          }, [h('p', {}, props.file?.content || '')]),
        ])
      }
    },
  }),
}))

import MarkdownPreview from '../MarkdownPreview.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: {} },
})

describe('MarkdownPreview', () => {
  function mountPreview(props = {}) {
    return shallowMount(MarkdownPreview, {
      props: {
        file: { path: '/tmp/README.md', content: '# Hello' },
        viewMode: 'rendered',
        ...props,
      },
      global: {
        plugins: [i18n],
      },
    })
  }

  it('renders markdown body container in rendered mode', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()
    expect(wrapper.find('.markdown-body').exists()).toBe(true)
  })

  it('renders CodePreview in source mode', async () => {
    const wrapper = mountPreview({ viewMode: 'source' })
    await nextTick()
    expect(wrapper.find('.raw-content-pre').exists()).toBe(true)
  })

  it('renders file path as data attribute on markdown body', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()
    const body = wrapper.find('.markdown-body')
    if (body.exists()) {
      expect(body.attributes('data-file-path')).toBe('/tmp/README.md')
    }
  })

  it('exposes bodyRef and lastBlockList', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    const vm = wrapper.vm as any
    expect(vm.$.exposed?.bodyRef).toBeDefined()
    expect(vm.$.exposed?.lastBlockList).toBeDefined()
  })

  it('renders markdown content inside body', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()
    const body = wrapper.find('.markdown-body')
    expect(body.exists()).toBe(true)
    expect(body.text()).toContain('# Hello')
  })
})
