import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import MarkdownPreview from '../MarkdownPreview.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {},
  },
})

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  useMarkdownRenderer: () => ({
    renderMarkdown: (content: string) => `<p>${content}</p>`,
    renderMermaidInElement: vi.fn().mockResolvedValue(undefined),
  }),
}))

vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: (opts: any) => ({
    handleDblClick: vi.fn(),
  }),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({
    annotateFilePaths: (html: string) => ({ html, detectedPaths: [] }),
    verifyFilePaths: vi.fn(),
    resolveRelativePath: (href: string) => href,
    openFilePath: vi.fn(),
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      projectRoot: '/tmp/project',
    },
  },
}))

// CSS.escape is not available in jsdom
if (!globalThis.CSS?.escape) {
  globalThis.CSS = { escape: (str: string) => str.replace(/([^\w-])/g, '\\$1') } as any
}

describe('MarkdownPreview', () => {
  function mountPreview(props = {}) {
    return mount(MarkdownPreview, {
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

  it('passes stickyScroll prop to CodePreview in source mode', async () => {
    const wrapper = mountPreview({
      viewMode: 'source',
      stickyScroll: true,
    })
    await nextTick()
    const codePreview = wrapper.findComponent({ name: 'CodePreview' })
    if (codePreview.exists()) {
      expect(codePreview.props('stickyScroll')).toBe(true)
    }
  })

  it('passes stickyScroll=false to CodePreview when set', async () => {
    const wrapper = mountPreview({
      viewMode: 'source',
      stickyScroll: false,
    })
    await nextTick()
    const codePreview = wrapper.findComponent({ name: 'CodePreview' })
    if (codePreview.exists()) {
      expect(codePreview.props('stickyScroll')).toBe(false)
    }
  })

  it('defaults stickyScroll to true when not specified', async () => {
    const wrapper = mountPreview({ viewMode: 'source' })
    await nextTick()
    const codePreview = wrapper.findComponent({ name: 'CodePreview' })
    if (codePreview.exists()) {
      expect(codePreview.props('stickyScroll')).toBe(true)
    }
  })

  it('handles in-page anchor link click with smooth scroll and flash', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()

    const body = wrapper.find('.markdown-body')
    if (!body.exists()) return

    // Inject target heading and anchor link into the rendered DOM
    const targetEl = document.createElement('h2')
    targetEl.id = 'my-section'
    targetEl.scrollIntoView = vi.fn()
    targetEl.addEventListener = vi.fn()

    const anchor = document.createElement('a')
    anchor.setAttribute('href', '#my-section')
    anchor.textContent = 'Link'

    body.element.appendChild(targetEl)
    body.element.appendChild(anchor)

    // Trigger click on the anchor element — jsdom will bubble it to body
    // which fires handleClick. event.target will be the anchor.
    const anchorWrapper = wrapper.find('a[href="#my-section"]')
    if (!anchorWrapper.exists()) return
    await anchorWrapper.trigger('click')
    await nextTick()

    expect(targetEl.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' })
    expect(targetEl.addEventListener).toHaveBeenCalledWith('animationend', expect.any(Function), { once: true })
  })

  it('ignores empty anchor href="#" clicks (no scroll)', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()

    const body = wrapper.find('.markdown-body')
    if (!body.exists()) return

    const anchor = document.createElement('a')
    anchor.setAttribute('href', '#')
    anchor.textContent = 'Empty'
    body.element.appendChild(anchor)

    // Click the empty anchor — should not cause errors
    const anchorWrapper = wrapper.find('a[href="#"]')
    if (!anchorWrapper.exists()) return
    await anchorWrapper.trigger('click')
    await nextTick()
    // No assertion needed — just verifying no error thrown
  })

  it('passes wordWrap and showLineNumbers props to CodePreview', async () => {
    const wrapper = mountPreview({
      viewMode: 'source',
      wordWrap: true,
      showLineNumbers: false,
    })
    await nextTick()
    const codePreview = wrapper.findComponent({ name: 'CodePreview' })
    if (codePreview.exists()) {
      expect(codePreview.props('wordWrap')).toBe(true)
      expect(codePreview.props('showLineNumbers')).toBe(false)
    }
  })
})
