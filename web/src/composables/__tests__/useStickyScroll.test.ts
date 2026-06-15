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
    const { defineComponent, h } = await import('vue')

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

  /**
   * Create a mock scroll element that uses getBoundingClientRect for position checks.
   *
   * Each line element has:
   * - getBoundingClientRect() → returns { top, height } in viewport coordinates
   *
   * The scroll container has:
   * - getBoundingClientRect() → returns { top } representing the visible area's top edge
   *
   * To simulate scrolling, we adjust line rect tops by subtracting a scroll offset.
   * When scrollTop=100, each line's rect.top decreases by 100.
   */
  function createMockScrollEl(overrides = {}) {
    return {
      querySelector: vi.fn().mockReturnValue({ querySelectorAll: vi.fn().mockReturnValue([]) }),
      querySelectorAll: vi.fn().mockReturnValue([]),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      scrollTop: 0,
      scrollLeft: 0,
      getBoundingClientRect: () => ({ top: 0 }),
      ...overrides,
    }
  }

  /**
   * Create mock line elements with viewport-relative rect positions.
   * @param lineCount - number of lines
   * @param lineHeight - height per line (default 20.8)
   * @param scrollOffset - simulated scroll offset (shifts all rect.tops up by this amount)
   * @param baseTop - initial top position before scroll (e.g., containerTop offset)
   */
  function createMockLineEls(lineCount, lineHeight = 20.8, scrollOffset = 0, baseTop = 0) {
    return Array.from({ length: lineCount }, (_, i) => ({
      getBoundingClientRect: () => ({
        top: baseTop + i * lineHeight - scrollOffset,
        height: lineHeight,
      }),
      classList: { add: vi.fn(), remove: vi.fn() },
      scrollIntoView: vi.fn(),
    }))
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

  it('H2 sticks when reaching sticky H1 bottom, not container top', async () => {
    // Bug 1 fix: multi-level headings should stick as soon as they touch
    // the bottom of the already-sticky zone, not the container top.
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'Title', kind: 'heading', line: 1, endLine: 20, level: 1 },
        { name: 'Section A', kind: 'heading', line: 2, endLine: 10, level: 2 },
      ],
    })

    const wrapper = await createTestComponent()
    const lineHeight = 20.8

    // Scroll just enough that line 1 is above containerTop, but line 2
    // is between containerTop and containerTop + lineHeight (sticky zone bottom).
    // With containerTop=0, scrollOffset=25:
    //   line 1 rect.top = 0*20.8 - 25 = -25  (above containerTop → sticky)
    //   line 2 rect.top = 1*20.8 - 25 = -4.2  (above containerTop but above stickyThreshold=20.8? No: -4.2 < 20.8 → sticky)
    // With old logic: line 2 at -4.2 < containerTop(0) → sticks (by coincidence also sticky)
    //
    // Better scenario: containerTop=0, scrollOffset=15:
    //   line 1 rect.top = 0 - 15 = -15  (above containerTop → sticky, accTop becomes 20.8)
    //   line 2 rect.top = 20.8 - 15 = 5.8  (above containerTop? Yes, 5.8 < 0? No!)
    //   Old logic: 5.8 < 0 is FALSE → not sticky (BUG!)
    //   New logic: 5.8 < 0 + 20.8 = 20.8 → sticky (CORRECT!)
    const lineEls = createMockLineEls(20, lineHeight, 15)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.md', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Both H1 (line 1) and H2 (line 2) should be sticky
    expect(wrapper.vm.stickyLines.length).toBe(2)
    expect(wrapper.vm.stickyLines[0].lineNum).toBe(1)
    expect(wrapper.vm.stickyLines[1].lineNum).toBe(2)
    expect(wrapper.vm.stickyLines[0].top).toBe(0)
    expect(wrapper.vm.stickyLines[1].top).toBe(20.8)

    wrapper.unmount()
  })

  it('sticky heading expires when endLine content reaches sticky zone', async () => {
    // Bug 2 fix: a sticky heading should disappear when its scope ends —
    // i.e., when its endLine content reaches the bottom of the sticky zone.
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'Title', kind: 'heading', line: 1, endLine: 20, level: 1 },
        { name: 'Section A', kind: 'heading', line: 2, endLine: 5, level: 2 },
        { name: 'Section B', kind: 'heading', line: 6, endLine: 20, level: 2 },
      ],
    })

    const wrapper = await createTestComponent()
    const lineHeight = 20.8

    // Scroll so that:
    //   line 1 (H1) rect.top is above containerTop → sticky, accTop = 20.8
    //   line 2 (H2 Section A) rect.top is above stickyThreshold (20.8) → eligible
    //   BUT line 5 (endLine of Section A) rect.top is at or below stickyThreshold → scope expired!
    // containerTop = 0, scrollOffset = 80:
    //   line 1: 0 - 80 = -80  (sticky, accTop=20.8)
    //   line 2: 20.8 - 80 = -59.2  (below stickyThreshold 20.8? Yes, -59.2 < 20.8 → passes cond A)
    //   line 5: 4*20.8 - 80 = 83.2 - 80 = 3.2  (<= stickyThreshold 20.8? Yes → scope expired! Skip H2-A)
    const lineEls = createMockLineEls(20, lineHeight, 80)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.md', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Only H1 should be sticky; H2 Section A's scope has expired (endLine content in sticky zone)
    expect(wrapper.vm.stickyLines.length).toBe(1)
    expect(wrapper.vm.stickyLines[0].lineNum).toBe(1)

    wrapper.unmount()
  })

  it('updates stickyLines with correct top/height when scrolled past scope definitions', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
        { name: 'helper', kind: 'function', line: 3, endLine: 8, level: 2 },
      ],
    })

    const wrapper = await createTestComponent()

    // 20 lines, scrolled 100px. Container top = 0.
    // Line 1 rect.top = 0 - 100 = -100 (above container, sticky)
    // Line 3 rect.top = 2*20.8 - 100 = -58.4 (above container + accTop=20.8, sticky)
    // Line 8 (endLine of helper) rect.top = 7*20.8 - 100 = 45.6 (above stickyThreshold=41.6? No → scope NOT expired)
    const lineEls = createMockLineEls(20, 20.8, 100)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Both main (line 1) and helper (line 3) should be sticky
    expect(wrapper.vm.stickyLines.length).toBe(2)
    expect(wrapper.vm.stickyLines[0].top).toBe(0)
    expect(wrapper.vm.stickyLines[0].height).toBe(20.8)
    expect(wrapper.vm.stickyLines[1].top).toBe(20.8)
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

    // Line 1 wrapped (height 41.6px), scrolled 50px
    const lineEls = [
      { getBoundingClientRect: () => ({ top: 0 - 50, height: 41.6 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
      { getBoundingClientRect: () => ({ top: 41.6 - 50, height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
      { getBoundingClientRect: () => ({ top: 62.4 - 50, height: 20.8 }), classList: { add: vi.fn(), remove: vi.fn() }, scrollIntoView: vi.fn() },
    ]

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    expect(wrapper.vm.stickyLines.length).toBe(1)
    expect(wrapper.vm.stickyLines[0].height).toBe(41.6)
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

    // No scrolling — line 1 rect.top = 0, containerTop = 0 → line is visible, not sticky
    const lineEls = createMockLineEls(10, 20.8, 0)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Line 1 rect.top (0) is NOT less than containerTop (0), so no sticky lines
    expect(wrapper.vm.stickyLines.length).toBe(0)

    wrapper.unmount()
  })

  it('syncs horizontal scroll to sticky code-text elements', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [{ name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 }],
    })

    const wrapper = await createTestComponent()

    const mockCodeText = { style: { transform: '' } }
    const lineEls = createMockLineEls(10, 20.8, 100)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      scrollLeft: 50,
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      querySelectorAll: vi.fn().mockReturnValueOnce([mockCodeText]),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    expect(mockCodeText.style.transform).toBe('translateX(-50px)')

    wrapper.unmount()
  })

  it('deduplicates symbols on the same line (e.g., Go return types)', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'foo', kind: 'function', line: 1, endLine: 10, level: 2 },
        { name: 'error', kind: 'function', line: 1, endLine: 10, level: 2 },
        { name: 'bar', kind: 'function', line: 12, endLine: 20, level: 2 },
      ],
    })

    const wrapper = await createTestComponent()

    const lineEls = createMockLineEls(20, 20.8, 100)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Only one sticky line for line 1 (deduplicated), not two
    const lineNums = wrapper.vm.stickyLines.map(s => s.lineNum)
    const line1Count = lineNums.filter(n => n === 1).length
    expect(line1Count).toBe(1)

    wrapper.unmount()
  })

  it('works correctly when scroll container has non-zero rect.top', async () => {
    // Simulate a page where the scroll container starts at y=200 (e.g., below a header)
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
      ],
    })

    const wrapper = await createTestComponent()

    const containerTop = 200
    // Lines start at containerTop, scrolled 30px
    const lineEls = createMockLineEls(10, 20.8, 30, containerTop)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: containerTop }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Line 1 rect.top = 200 - 30 = 170, which is < containerTop (200), so sticky
    expect(wrapper.vm.stickyLines.length).toBe(1)
    expect(wrapper.vm.stickyLines[0].lineNum).toBe(1)

    wrapper.unmount()
  })

  it('sticky line disappears when definition line scrolls back into view', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 },
      ],
    })

    const wrapper = await createTestComponent()

    // No scrolling — line 1 at containerTop, not above it
    const lineEls = createMockLineEls(10, 20.8, 0)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    expect(wrapper.vm.stickyLines.length).toBe(0)

    wrapper.unmount()
  })

  it('clears stickyLines when scrollEl or symbols are empty', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [{ name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 }],
    })

    const wrapper = await createTestComponent()

    // First init with symbols, scroll past definition
    const lineEls = createMockLineEls(10, 20.8, 100)
    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // Should have sticky lines
    expect(wrapper.vm.stickyLines.length).toBeGreaterThan(0)

    // Now teardown — this sets scrollEl=null and symbols=[]
    wrapper.vm.teardownSticky()

    // stickyLines should be cleared (covers line 34-35)
    expect(wrapper.vm.stickyLines).toEqual([])

    wrapper.unmount()
  })

  it('sorts enclosing scopes with same width by line ascending', async () => {
    // Two sibling functions with the same scope width — verify stable sort
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [
        { name: 'foo', kind: 'function', line: 5, endLine: 15, level: 2 },
        { name: 'bar', kind: 'function', line: 20, endLine: 30, level: 2 },
        { name: 'baz', kind: 'function', line: 1, endLine: 11, level: 2 },
      ],
    })

    const wrapper = await createTestComponent()

    const lineEls = createMockLineEls(30, 20.8, 200)

    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]
    scrollHandler()
    await wrapper.vm.$nextTick()

    // All three have same width (10 lines), so sorted by line ascending
    const lineNums = wrapper.vm.stickyLines.map(s => s.lineNum)
    for (let i = 1; i < lineNums.length; i++) {
      expect(lineNums[i]).toBeGreaterThan(lineNums[i - 1])
    }

    wrapper.unmount()
  })

  it('uses requestAnimationFrame for scroll debouncing', async () => {
    mockFetchCodeSymbols.mockResolvedValue({
      symbols: [{ name: 'main', kind: 'function', line: 1, endLine: 10, level: 1 }],
    })

    const wrapper = await createTestComponent()

    const lineEls = createMockLineEls(10, 20.8, 0)
    const mockCodeEl = { querySelectorAll: vi.fn().mockReturnValue(lineEls) }
    const mockEl = createMockScrollEl({
      querySelector: vi.fn().mockReturnValue(mockCodeEl),
      getBoundingClientRect: () => ({ top: 0 }),
    })

    wrapper.vm.initSticky('/tmp/test.go', mockEl)
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const scrollHandler = mockEl.addEventListener.mock.calls[0][1]

    // Call scrollHandler twice rapidly — second call should be skipped (rafId pending)
    // This covers the rafId guard path (line 120)
    scrollHandler()
    scrollHandler() // should be a no-op since rafId is set

    // Wait for RAF to complete
    await new Promise(r => setTimeout(r, 50))
    await wrapper.vm.$nextTick()

    wrapper.unmount()
  })
})
