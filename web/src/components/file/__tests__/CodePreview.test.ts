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
})
