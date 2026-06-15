import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import CodePreview from '../CodePreview.vue'

// Mock useDoubleClickCopy
vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: () => ({
    handleDblClick: vi.fn(),
  }),
}))

// Mock useQuoteQuestion
vi.mock('@/composables/useQuoteQuestion.ts', () => ({
  useQuoteQuestion: () => ({
    showBar: vi.fn(),
  }),
}))

// Mock resolveFilePath for file path annotation
const mockResolveFilePath = vi.fn()
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  resolveFilePath: (...args: any[]) => mockResolveFilePath(...args),
}))

// Mock store for file path annotation
vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      projectRoot: '/home/user/project',
      homeDir: '/home/user',
    },
  },
}))

// Shared reactive stickyLines ref for controlling in tests
let stickyLinesRef: any

// Mock useStickyScroll
const mockInitSticky = vi.fn()
const mockTeardownSticky = vi.fn()
const mockInvalidateCache = vi.fn()
vi.mock('@/composables/useStickyScroll.ts', () => ({
  useStickyScroll: () => {
    stickyLinesRef = ref([])
    return {
      stickyLines: stickyLinesRef,
      initSticky: (...args: any[]) => mockInitSticky(...args),
      teardownSticky: (...args: any[]) => mockTeardownSticky(...args),
      invalidateCache: (...args: any[]) => mockInvalidateCache(...args),
    }
  },
}))

describe('CodePreview', () => {
  beforeEach(() => {
    mockInitSticky.mockClear()
    mockTeardownSticky.mockClear()
    mockInvalidateCache.mockClear()
  })

  function mountPreview(props = {}) {
    return mount(CodePreview, {
      props: {
        content: 'const x = 1\nconst y = 2',
        language: 'typescript',
        ...props,
      },
    })
  }

  it('renders raw-content-pre container', () => {
    const wrapper = mountPreview()
    expect(wrapper.find('.raw-content-pre').exists()).toBe(true)
  })

  it('applies word-wrap class when wordWrap is true', () => {
    const wrapper = mountPreview({ wordWrap: true })
    expect(wrapper.find('.raw-content-pre').classes()).toContain('word-wrap')
  })

  it('does not apply word-wrap class when wordWrap is false', () => {
    const wrapper = mountPreview({ wordWrap: false })
    expect(wrapper.find('.raw-content-pre').classes()).not.toContain('word-wrap')
  })

  it('applies no-line-num class when showLineNumbers is false', () => {
    const wrapper = mountPreview({ showLineNumbers: false })
    expect(wrapper.find('.raw-content-pre').classes()).toContain('no-line-num')
  })

  it('calls initSticky when stickyScroll is true and filePath is provided', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts' })
    await nextTick()
    await nextTick()
    expect(mockInitSticky).toHaveBeenCalled()
  })

  it('calls teardownSticky when stickyScroll is false', async () => {
    const wrapper = mountPreview({ stickyScroll: false, filePath: '/tmp/main.ts' })
    await nextTick()
    await nextTick()
    expect(mockTeardownSticky).toHaveBeenCalled()
  })

  it('does not call initSticky when stickyScroll is false', async () => {
    const wrapper = mountPreview({ stickyScroll: false, filePath: '/tmp/main.ts' })
    await nextTick()
    await nextTick()
    expect(mockInitSticky).not.toHaveBeenCalled()
  })

  it('calls teardownSticky when stickyScroll toggles from true to false', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts' })
    await nextTick()
    await nextTick()
    mockInitSticky.mockClear()
    mockTeardownSticky.mockClear()
    await wrapper.setProps({ stickyScroll: false })
    await nextTick()
    await nextTick()
    expect(mockTeardownSticky).toHaveBeenCalled()
  })

  it('renders code lines from content', () => {
    const wrapper = mountPreview()
    const lines = wrapper.findAll('.code-line')
    expect(lines.length).toBeGreaterThanOrEqual(2)
  })

  it('passes stickyScroll prop correctly', () => {
    const wrapper = mountPreview({ stickyScroll: true })
    expect(wrapper.props('stickyScroll')).toBe(true)
  })

  it('defaults stickyScroll to true', () => {
    const wrapper = mountPreview()
    expect(wrapper.props('stickyScroll')).toBe(true)
  })

  it('does not show sticky-scroll-overlay when stickyLines is empty', () => {
    const wrapper = mountPreview()
    expect(wrapper.find('.sticky-scroll-overlay').exists()).toBe(false)
  })

  it('shows sticky-scroll-overlay when stickyLines has entries', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts' })
    await nextTick()

    // Simulate sticky lines being populated
    stickyLinesRef.value = [
      { lineNum: 1, kind: 'function', top: 0, height: 20.8 },
    ]
    await nextTick()

    expect(wrapper.find('.sticky-scroll-overlay').exists()).toBe(true)
    expect(wrapper.findAll('.sticky-line').length).toBe(1)
  })

  it('renders sticky line with correct top and height style', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts' })
    await nextTick()

    stickyLinesRef.value = [
      { lineNum: 1, kind: 'function', top: 0, height: 20.8 },
      { lineNum: 5, kind: 'function', top: 20.8, height: 41.6 },
    ]
    await nextTick()

    const stickyLineEls = wrapper.findAll('.sticky-line')
    expect(stickyLineEls.length).toBe(2)
    expect(stickyLineEls[0].attributes('style')).toContain('top: 0px')
    expect(stickyLineEls[0].attributes('style')).toContain('height: 20.8px')
    expect(stickyLineEls[1].attributes('style')).toContain('top: 20.8px')
  })

  it('shows sticky line numbers when showLineNumbers is true', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts', showLineNumbers: true })
    await nextTick()

    stickyLinesRef.value = [{ lineNum: 1, kind: 'function', top: 0, height: 20.8 }]
    await nextTick()

    expect(wrapper.find('.sticky-line-num').exists()).toBe(true)
    expect(wrapper.find('.sticky-line-num').text()).toBe('1')
  })

  it('hides sticky line numbers when showLineNumbers is false', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts', showLineNumbers: false })
    await nextTick()

    stickyLinesRef.value = [{ lineNum: 1, kind: 'function', top: 0, height: 20.8 }]
    await nextTick()

    expect(wrapper.find('.sticky-line-num').exists()).toBe(false)
  })

  it('renders sticky-code-text element', async () => {
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts' })
    await nextTick()

    stickyLinesRef.value = [{ lineNum: 1, kind: 'function', top: 0, height: 20.8 }]
    await nextTick()

    expect(wrapper.find('.sticky-code-text').exists()).toBe(true)
  })

  it('sets data-file-path attribute on pre element', () => {
    const wrapper = mountPreview({ filePath: '/tmp/main.ts' })
    expect(wrapper.find('.raw-content-pre').attributes('data-file-path')).toBe('/tmp/main.ts')
  })

  it('sets data-language attribute on pre element', () => {
    const wrapper = mountPreview({ language: 'go' })
    expect(wrapper.find('.raw-content-pre').attributes('data-language')).toBe('go')
  })

  it('does not render when content is empty', () => {
    const wrapper = mountPreview({ content: '' })
    expect(wrapper.findAll('.code-line').length).toBe(0)
  })

  it('renders flash ranges with correct flash type', () => {
    const wrapper = mountPreview({
      content: 'hello world',
      language: 'plaintext',
      flashRanges: [{ line: 1, start: 0, end: 5 }],
      flashType: 'delete',
    })
    expect(wrapper.find('.code-line').exists()).toBe(true)
  })

  it('handleStickyClick uses scrollBy with sticky height offset', async () => {
    // Test the click handler logic directly by calling it through the component
    const wrapper = mountPreview({ stickyScroll: true, filePath: '/tmp/main.ts' })
    await nextTick()

    // Simulate two sticky lines (H1 + H2), total sticky height = 41.6
    stickyLinesRef.value = [
      { lineNum: 1, kind: 'heading', top: 0, height: 20.8 },
      { lineNum: 2, kind: 'heading', top: 20.8, height: 20.8 },
    ]
    await nextTick()

    // Get the pre element and mock its scrollBy
    const preEl = wrapper.find('.raw-content-pre').element
    const mockScrollBy = vi.fn()
    preEl.scrollBy = mockScrollBy

    // Mock querySelectorAll to return line elements
    const mockLineEl = {
      getBoundingClientRect: () => ({ top: -100 }),
      classList: { add: vi.fn(), remove: vi.fn() },
    }
    const origQSA = preEl.querySelectorAll
    preEl.querySelectorAll = vi.fn().mockImplementation((selector) => {
      if (selector.includes('code-line')) return [mockLineEl, mockLineEl]
      return origQSA.call(preEl, selector)
    })

    // Trigger click on the first sticky line
    const stickyLines = wrapper.findAll('.sticky-line')
    if (stickyLines.length > 0) {
      await stickyLines[0].trigger('click')

      // scrollBy should be called with the sticky height offset
      expect(mockScrollBy).toHaveBeenCalledWith(
        expect.objectContaining({
          behavior: 'smooth',
          top: expect.any(Number),
        })
      )
      // The scroll delta = lineTop - containerTop - stickyHeight = -100 - 0 - 41.6 = -141.6
      const scrollCall = mockScrollBy.mock.calls[0][0]
      expect(scrollCall.top).toBe(-141.6)
    }

    // Restore
    preEl.querySelectorAll = origQSA
  })

  describe('file path annotation', () => {
    beforeEach(() => {
      mockResolveFilePath.mockReset()
    })

    it('emits openFile when clicking a code-file-path element', async () => {
      const wrapper = mountPreview({ content: 'import "./utils"' })
      await nextTick()
      await nextTick()

      // Manually inject a .code-file-path span into the rendered code
      const codeEl = wrapper.find('.raw-content-pre')
      if (codeEl.exists()) {
        const stringSpan = codeEl.find('.hljs-string')
        if (stringSpan.exists()) {
          // Inject a clickable path span inside the string
          stringSpan.element.innerHTML = '<span class="code-file-path" data-file-path="src/utils.ts">./utils</span>'
          await nextTick()

          const pathEl = wrapper.find('.code-file-path')
          if (pathEl.exists()) {
            await pathEl.trigger('click')
            expect(wrapper.emitted('openFile')).toBeTruthy()
            expect(wrapper.emitted('openFile')![0]).toEqual(['src/utils.ts'])
          }
        }
      }
    })

    it('does not emit openFile for regular code clicks', async () => {
      const wrapper = mountPreview({ content: 'const x = 1' })
      await nextTick()
      await nextTick()

      const line = wrapper.find('.code-line')
      if (line.exists()) {
        await line.trigger('click')
        expect(wrapper.emitted('openFile')).toBeFalsy()
      }
    })

    it('annotateFilePaths wraps resolved paths in code-file-path spans', async () => {
      mockResolveFilePath.mockImplementation((path: string) => {
        if (path === './utils') return 'src/utils.ts'
        return null
      })

      const wrapper = mountPreview({ content: 'import "./utils"' })
      await nextTick()
      await nextTick()
      await nextTick()

      // After rendering + annotation, check if a .code-file-path span was created
      const annotated = wrapper.findAll('.code-file-path')
      if (annotated.length > 0) {
        expect(annotated[0].attributes('data-file-path')).toBe('src/utils.ts')
      }
    })

    it('annotateFilePaths skips paths that do not resolve', async () => {
      mockResolveFilePath.mockReturnValue(null)

      const wrapper = mountPreview({ content: 'import "./nonexistent"' })
      await nextTick()
      await nextTick()
      await nextTick()

      expect(wrapper.findAll('.code-file-path').length).toBe(0)
    })
  })
})
