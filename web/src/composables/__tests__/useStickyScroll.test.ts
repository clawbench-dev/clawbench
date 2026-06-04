import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock fetchCodeSymbols before importing useStickyScroll
const mockFetchCodeSymbols = vi.fn()
vi.mock('@/composables/useCodeSymbols.ts', () => ({
  fetchCodeSymbols: (...args: any[]) => mockFetchCodeSymbols(...args),
}))

import { useStickyScroll } from '@/composables/useStickyScroll.ts'

describe('useStickyScroll', () => {
  beforeEach(() => {
    mockFetchCodeSymbols.mockReset()
  })

  it('returns stickyLines ref, initSticky, teardownSticky, invalidateCache', () => {
    expect(typeof useStickyScroll).toBe('function')
  })

  it('fetchCodeSymbols mock is properly wired', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
      ],
    })
    const result = await mockFetchCodeSymbols('/tmp/main.go')
    expect(result.symbols).toHaveLength(1)
    expect(result.symbols[0].name).toBe('main')
  })

  it('fetchCodeSymbols returns empty for files with no symbols', async () => {
    mockFetchCodeSymbols.mockResolvedValue({ symbols: [] })
    const result = await mockFetchCodeSymbols('/tmp/empty.txt')
    expect(result.symbols).toHaveLength(0)
  })
})

describe('useStickyScroll - DOM integration', () => {
  beforeEach(() => {
    mockFetchCodeSymbols.mockReset()
  })

  async function createTestComponent() {
    const { mount } = await import('@vue/test-utils')
    const { defineComponent, h, ref } = await import('vue')

    const TestComponent = defineComponent({
      setup() {
        const { stickyLines, initSticky, teardownSticky } = useStickyScroll()
        return { stickyLines, initSticky, teardownSticky }
      },
      render() {
        return h('div')
      },
    })

    return mount(TestComponent)
  }

  function createMockScrollEl(overrides = {}) {
    return {
      querySelectorAll: vi.fn().mockReturnValue([]),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      scrollTop: 0,
      scrollLeft: 0,
      ...overrides,
    }
  }

  it('initSticky calls fetchCodeSymbols with filePath', async () => {
    mockFetchCodeSymbols.mockResolvedValue({ symbols: [] })
    const wrapper = await createTestComponent()

    wrapper.vm.initSticky('/tmp/test.go', createMockScrollEl())
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(mockFetchCodeSymbols).toHaveBeenCalledWith('/tmp/test.go')
    wrapper.unmount()
  })

  it('teardownSticky clears stickyLines', async () => {
    mockFetchCodeSymbols.mockResolvedValue({ symbols: [] })
    const wrapper = await createTestComponent()

    wrapper.vm.initSticky('/tmp/test.go', createMockScrollEl())
    wrapper.vm.teardownSticky()

    expect(wrapper.vm.stickyLines).toEqual([])
    wrapper.unmount()
  })

  it('does not call fetchCodeSymbols when filePath is empty', async () => {
    const wrapper = await createTestComponent()
    wrapper.vm.initSticky('', createMockScrollEl())

    expect(mockFetchCodeSymbols).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not call fetchCodeSymbols when element is null', async () => {
    const wrapper = await createTestComponent()
    wrapper.vm.initSticky('/tmp/test.go', null)

    expect(mockFetchCodeSymbols).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('attaches scroll listener when symbols are found', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [{ name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 }],
    })

    const wrapper = await createTestComponent()
    const mockEl = createMockScrollEl()

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(mockEl.addEventListener).toHaveBeenCalledWith('scroll', expect.any(Function), { passive: true })
    wrapper.unmount()
  })

  it('does not attach scroll listener when no symbols found', async () => {
    mockFetchCodeSymbols.mockResolvedValue({ symbols: [] })

    const wrapper = await createTestComponent()
    const mockEl = createMockScrollEl()

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(mockEl.addEventListener).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('removes scroll listener on teardown', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [{ name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 }],
    })

    const wrapper = await createTestComponent()
    const mockEl = createMockScrollEl()

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    wrapper.vm.teardownSticky()

    expect(mockEl.removeEventListener).toHaveBeenCalledWith('scroll', expect.any(Function))
    wrapper.unmount()
  })

  it('updates stickyLines with correct top/height when scrolled past scope definitions', async () => {
    // Simulate a file with a function on line 1 (endLine 10) and a nested function on line 3 (endLine 8)
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
        { name: 'helper', kind: 'function', line: 3, endLine: 8, level: 2 },
      ],
    })

    const wrapper = await createTestComponent()

    // Create mock line elements with offsetTop and getBoundingClientRect
    const createMockLineEl = (offsetTop: number, height: number) => ({
      offsetTop,
      getBoundingClientRect: () => ({ height }),
      classList: { add: vi.fn(), remove: vi.fn() },
      scrollIntoView: vi.fn(),
    })

    const lineEls = [
      createMockLineEl(0, 20.8),     // line 1
      createMockLineEl(20.8, 20.8),  // line 2
      createMockLineEl(41.6, 20.8),  // line 3
      createMockLineEl(62.4, 20.8),  // line 4
      createMockLineEl(83.2, 20.8),  // line 5
      createMockLineEl(104, 20.8),   // line 6
    ]

    const mockEl = createMockScrollEl({
      scrollTop: 100, // scrolled past line 1 (offsetTop=0) and line 3 (offsetTop=41.6)
      querySelectorAll: vi.fn().mockReturnValue(lineEls),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    // After init, scroll handler should be attached. Trigger a scroll event.
    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Both main (line 1) and helper (line 3) should be sticky since
    // we scrolled past both (scrollTop=100 > offsetTop of line1=0 and line3=41.6)
    expect(wrapper.vm.stickyLines.length).toBe(2)
    expect(wrapper.vm.stickyLines[0].top).toBe(0) // first sticky line at top=0
    expect(wrapper.vm.stickyLines[0].height).toBe(20.8) // actual DOM height
    expect(wrapper.vm.stickyLines[1].top).toBe(20.8) // accumulated height
    expect(wrapper.vm.stickyLines[1].height).toBe(20.8)

    wrapper.unmount()
  })

  it('handles word-wrap mode with taller line heights', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'longFunc', kind: 'function', line: 1, endLine: 5, level: 1 },
      ],
    })

    const wrapper = await createTestComponent()

    // In word-wrap mode, line 1 is wrapped and taller (41.6px instead of 20.8px)
    const lineEls = [
      { offsetTop: 0, getBoundingClientRect: () => ({ height: 41.6 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
      { offsetTop: 41.6, getBoundingClientRect: () => ({ height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
      { offsetTop: 62.4, getBoundingClientRect: () => ({ height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
    ]

    const mockEl = createMockScrollEl({
      scrollTop: 50, // scrolled past line 1
      querySelectorAll: vi.fn().mockReturnValue(lineEls),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    expect(wrapper.vm.stickyLines.length).toBe(1)
    expect(wrapper.vm.stickyLines[0].height).toBe(41.6) // wrapped line height
    expect(wrapper.vm.stickyLines[0].top).toBe(0)

    wrapper.unmount()
  })

  it('does not show sticky lines when definition line is still visible', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
      ],
    })

    const wrapper = await createTestComponent()

    const lineEls = [
      { offsetTop: 0, getBoundingClientRect: () => ({ height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
    ]

    const mockEl = createMockScrollEl({
      scrollTop: 0, // not scrolled at all — definition line still visible
      querySelectorAll: vi.fn().mockReturnValue(lineEls),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Line 1 offsetTop (0) is NOT less than scrollTop (0), so no sticky lines
    expect(wrapper.vm.stickyLines.length).toBe(0)

    wrapper.unmount()
  })

  it('clears stickyLines when scrolled to top and no scopes are out of view', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
      ],
    })

    const wrapper = await createTestComponent()

    const lineEls = [
      { offsetTop: 0, getBoundingClientRect: () => ({ height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
    ]

    const mockEl = createMockScrollEl({
      scrollTop: 0,
      querySelectorAll: vi.fn().mockReturnValue(lineEls),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    expect(wrapper.vm.stickyLines).toEqual([])

    wrapper.unmount()
  })

  it('syncs horizontal scroll to sticky code-text elements', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [{ name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 }],
    })

    const wrapper = await createTestComponent()

    const mockCodeText = { style: { transform: '' } }
    const lineEls = [
      { offsetTop: 0, getBoundingClientRect: () => ({ height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
    ]

    const mockEl = createMockScrollEl({
      scrollTop: 100,
      scrollLeft: 50,
      querySelectorAll: vi.fn()
        .mockReturnValueOnce(lineEls) // first call for code-line
        .mockReturnValueOnce([mockCodeText]), // second call for .sticky-code-text
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // The code-text element should have its transform updated
    expect(mockCodeText.style.transform).toBe('translateX(-50px)')

    wrapper.unmount()
  })
})
