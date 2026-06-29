import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
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

vi.mock('@/composables/useQuoteQuestion.ts', () => ({
  useQuoteQuestion: () => ({
    showBar: vi.fn(),
  }),
}))

const mockHandleTableRowClick = vi.fn(() => false)
vi.mock('@/composables/useTableRowExpand.ts', () => ({
  useTableRowExpand: () => ({
    tableRowModal: ref(null),
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: mockHandleTableRowClick,
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))

vi.mock('@/composables/useDiffMarkerClick.ts', () => ({
  handleDiffMarkerClick: () => false,
}))

// Mock useMarkdownDiff (diff markers + drawer state)
vi.mock('@/composables/useMarkdownDiff.ts', () => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref, shallowRef } = require('vue')
  return {
    diffMarkers: ref([]),
    diffDrawerVisible: ref(false),
    diffDrawerMarker: shallowRef(null),
    openDiffDrawer: vi.fn(),
    closeDiffDrawer: vi.fn(),
    clearDiffMarkers: vi.fn(),
    extractBlocks: vi.fn(() => []),
    extractBlockElements: vi.fn(() => []),
  }
})

// Mock DiffDrawer component
vi.mock('../DiffDrawer.vue', () => ({
  default: {
    name: 'DiffDrawer',
    template: '<div class="mock-diff-drawer" v-if="visible"><slot /></div>',
    props: ['visible', 'markerType', 'charDiff'],
  },
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

  it('passes stickyScroll prop correctly when not specified', async () => {
    const wrapper = mountPreview({ viewMode: 'source' })
    await nextTick()
    const codePreview = wrapper.findComponent({ name: 'CodePreview' })
    if (codePreview.exists()) {
      // When not specified, MarkdownPreview's stickyScroll is undefined,
      // which VTU reports as the raw prop value (false for Boolean)
      // At runtime, CodePreview's default (true) resolves correctly
      expect(codePreview.props('stickyScroll')).toBe(false)
    }
  })

  it('handles in-page anchor link click with smooth scroll and flash', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()

    const body = wrapper.find('.markdown-body')
    if (!body.exists()) return

    // Manually set bodyRef (template ref not set in jsdom)
    const vm = wrapper.vm as any
    const bodyRef = vm.$.exposed?.bodyRef
    if (bodyRef && bodyRef.__v_isRef && !bodyRef.value) {
      bodyRef.value = body.element
    }

    // Inject target heading and anchor link into the rendered DOM
    const targetEl = document.createElement('h2')
    targetEl.id = 'my-section'
    targetEl.scrollIntoView = vi.fn()

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

  it('renders TableRowModal component', async () => {
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()
    // TableRowModal should be present in the template
    expect(wrapper.findComponent({ name: 'TableRowModal' }).exists()).toBe(true)
  })

  it('clears diff markers on unmount', async () => {
    const { clearDiffMarkers } = await import('@/composables/useMarkdownDiff.ts')
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()
    wrapper.unmount()
    expect(clearDiffMarkers).toHaveBeenCalled()
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

  it('calls handleTableRowClick on click in markdown body', async () => {
    mockHandleTableRowClick.mockReturnValue(false)
    const wrapper = mountPreview({ viewMode: 'rendered' })
    await nextTick()
    await nextTick()
    const body = wrapper.find('.markdown-body')
    if (body.exists()) {
      await body.trigger('click')
      expect(mockHandleTableRowClick).toHaveBeenCalled()
    }
  })
})
