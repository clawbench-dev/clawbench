import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import CodePreviewBody, { type FormattedCodeLine } from '@/components/file/CodePreviewBody.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        codePreview: {
          title: 'Code Preview',
          loading: 'Loading...',
          retry: 'Retry',
          linesRemaining: '{n} remaining',
          loadingMoreLines: 'Loading more...',
        },
      },
    },
  },
})

function makeLines(): FormattedCodeLine[] {
  return [
    { lineNum: 10, html: 'const a = 1', isTarget: false },
    { lineNum: 11, html: 'const b = 2', isTarget: true },
    { lineNum: 12, html: 'const c = 3', isTarget: true },
    { lineNum: 13, html: 'const d = 4', isTarget: false },
  ]
}

function mountBody(overrides: Record<string, unknown> = {}) {
  const loadMoreAbove = vi.fn()
  const loadMoreBelow = vi.fn()
  const wrapper = mount(CodePreviewBody, {
    global: { plugins: [i18n] },
    props: {
      status: 'ready',
      errorMessageText: '',
      errorCode: null,
      isWordWrap: true,
      showLineNumbers: true,
      codeLines: makeLines(),
      matchingLineIndices: [] as number[],
      activeMatchIndex: 0,
      remainingAbove: 0,
      remainingBelow: 0,
      loadingDirection: null,
      loadMoreAbove,
      loadMoreBelow,
      ...overrides,
    },
  })
  return { wrapper, loadMoreAbove, loadMoreBelow }
}

/** Give the scroll container real geometry so the threshold math is testable. */
function setGeometry(
  el: Element,
  geo: { scrollTop: number; scrollHeight: number; clientHeight: number }
) {
  for (const [key, value] of Object.entries(geo)) {
    Object.defineProperty(el, key, { configurable: true, writable: true, value })
  }
}

describe('CodePreviewBody.vue', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('sets gutter-width CSS var from the widest visible line number', () => {
    const { wrapper } = mountBody()
    const lines = wrapper.find('.code-preview-lines')
    // makeLines() spans lines 10..13 (two digits).
    expect(lines.attributes('style')).toContain('--gutter-digits: 2')

    // Three-digit range -> gutter widens to 3.
    const wide = mountBody({
      codeLines: [
        { lineNum: 98, html: 'a', isTarget: false },
        { lineNum: 100, html: 'b', isTarget: false },
        { lineNum: 101, html: 'c', isTarget: false },
      ],
    })
    expect(wide.wrapper.find('.code-preview-lines').attributes('style')).toContain('--gutter-digits: 3')

    // Single-digit range stays minimal.
    const tiny = mountBody({
      codeLines: [{ lineNum: 1, html: 'a', isTarget: false }],
    })
    expect(tiny.wrapper.find('.code-preview-lines').attributes('style')).toContain('--gutter-digits: 1')
  })

  it('renders loading status with aria-live', () => {
    const { wrapper } = mountBody({ status: 'loading' })
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('Loading')
    expect(wrapper.find('.code-preview-line-row').exists()).toBe(false)
  })

  it('renders error status and emits refresh only for network errors', async () => {
    const { wrapper } = mountBody({ status: 'error', errorCode: 'network', errorMessageText: 'boom' })
    expect(wrapper.find('[role="status"]').text()).toContain('boom')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    // Non-network error code: no retry button
    const noRetry = mountBody({ status: 'error', errorCode: 'not-found', errorMessageText: 'missing' })
    expect(noRetry.wrapper.find('button').exists()).toBe(false)
  })

  it('renders code rows with target/matching line classes', () => {
    const { wrapper } = mountBody({
      matchingLineIndices: [0, 2],
      activeMatchIndex: 1, // matchingLineIndices[1] === 2 -> row 2 is the current match
    })
    const rows = wrapper.findAll('.code-preview-line-row')
    expect(rows).toHaveLength(4)
    expect(rows[0].classes()).toContain('is-search-match')
    expect(rows[1].classes()).toContain('is-target-line')
    expect(rows[2].classes()).toContain('is-current-search-match')
    expect(rows[3].classes()).not.toContain('is-search-match')
    expect(rows[1].get('.code-preview-line-number').text()).toBe('11')
  })

  it('shows a remaining-lines hint and no expand buttons', () => {
    const { wrapper } = mountBody({ remainingAbove: 9, remainingBelow: 20 })

    // The bars exist purely as status lines: loading is driven by scrolling.
    const above = wrapper.find('.expand-above')
    const below = wrapper.find('.expand-below')
    expect(above.exists()).toBe(true)
    expect(below.exists()).toBe(true)
    expect(above.get('.code-preview-expand-hint').text()).toContain('9')
    expect(below.get('.code-preview-expand-hint').text()).toContain('20')

    // Nothing to click any more — the buttons were removed with the line cap.
    expect(wrapper.find('.code-preview-expand-btn').exists()).toBe(false)
    expect(wrapper.find('.code-preview-expand-actions').exists()).toBe(false)

    // No remaining lines -> no bars at all.
    const none = mountBody()
    expect(none.wrapper.find('.expand-above').exists()).toBe(false)
    expect(none.wrapper.find('.expand-below').exists()).toBe(false)
  })

  it('shows a spinner on the side currently loading', () => {
    const above = mountBody({ remainingAbove: 9, loadingDirection: 'above' })
    expect(above.wrapper.find('.expand-above .code-preview-expand-spinner').exists()).toBe(true)
    expect(above.wrapper.find('.expand-below').exists()).toBe(false)

    const below = mountBody({ remainingBelow: 9, loadingDirection: 'below' })
    expect(below.wrapper.find('.expand-below .code-preview-expand-spinner').exists()).toBe(true)
  })

  it('loads more when scrolled to the bottom', async () => {
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100 })
    const el = wrapper.find('.code-preview-scroll').element
    // Near the bottom (within the 240px threshold).
    setGeometry(el, { scrollTop: 800, scrollHeight: 1200, clientHeight: 400 })

    await wrapper.find('.code-preview-scroll').trigger('scroll')
    expect(loadMoreBelow).toHaveBeenCalledTimes(1)
  })

  it('does not load when scrolled in the middle', async () => {
    const { wrapper, loadMoreAbove, loadMoreBelow } = mountBody({ remainingAbove: 50, remainingBelow: 50 })
    const el = wrapper.find('.code-preview-scroll').element
    setGeometry(el, { scrollTop: 1000, scrollHeight: 4000, clientHeight: 400 })

    await wrapper.find('.code-preview-scroll').trigger('scroll')
    expect(loadMoreBelow).not.toHaveBeenCalled()
    expect(loadMoreAbove).not.toHaveBeenCalled()
  })

  it('loads above when scrolled to the top', async () => {
    const { wrapper, loadMoreAbove, loadMoreBelow } = mountBody({ remainingAbove: 50, remainingBelow: 50 })
    const el = wrapper.find('.code-preview-scroll').element
    setGeometry(el, { scrollTop: 0, scrollHeight: 4000, clientHeight: 400 })

    await wrapper.find('.code-preview-scroll').trigger('scroll')
    expect(loadMoreAbove).toHaveBeenCalledTimes(1)
    expect(loadMoreBelow).not.toHaveBeenCalled()
  })

  it('stops loading when loadMoreBlocked is set', async () => {
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100, loadMoreBlocked: true })
    const el = wrapper.find('.code-preview-scroll').element
    setGeometry(el, { scrollTop: 800, scrollHeight: 1200, clientHeight: 400 })

    await wrapper.find('.code-preview-scroll').trigger('scroll')
    expect(loadMoreBelow).not.toHaveBeenCalled()

    // The hint stays so the user still sees how much is left.
    expect(wrapper.find('.expand-below .code-preview-expand-hint').text()).toContain('100')
  })

  it('auto-fills the viewport when the slice is too short to scroll', async () => {
    // 4 lines in a tall pane: no scrollbar, so no scroll event would ever fire.
    // Without the fill loop the remaining lines would be unreachable.
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100 })
    const el = wrapper.find('.code-preview-scroll').element
    setGeometry(el, { scrollTop: 0, scrollHeight: 80, clientHeight: 400 })

    await vi.waitFor(() => {
      expect(loadMoreBelow).toHaveBeenCalled()
    })
  })

  it('does not auto-fill once the content overflows', async () => {
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100 })
    const el = wrapper.find('.code-preview-scroll').element
    setGeometry(el, { scrollTop: 0, scrollHeight: 2000, clientHeight: 400 })

    // Give the watcher a chance to run; nothing should be requested.
    await Promise.resolve()
    await Promise.resolve()
    expect(loadMoreBelow).not.toHaveBeenCalled()
  })

  it('scrollLineIntoView calls scrollIntoView on the matching row', () => {
    const { wrapper } = mountBody()
    const row = wrapper.findAll('.code-preview-line-row')[2]
    const scrollSpy = vi.fn()
    Object.defineProperty(row.element, 'scrollIntoView', {
      configurable: true,
      value: scrollSpy,
    })
    ;(wrapper.vm as unknown as { scrollLineIntoView: (i: number) => void }).scrollLineIntoView(2)
    expect(scrollSpy).toHaveBeenCalled()
  })

  it('exposes the scroll container element', () => {
    const { wrapper } = mountBody()
    const exposed = wrapper.vm as unknown as { scrollContainer: HTMLElement | null }
    expect(exposed.scrollContainer?.classList.contains('code-preview-scroll')).toBe(true)
  })

  it('scrollToTargetLine scrolls the pane to center the is-target-line range', async () => {
    const { wrapper } = mountBody()
    const scrollEl = wrapper.find('.code-preview-scroll')
    // Mock geometry: tall scroll container; two stacked target rows far down.
    Object.defineProperty(scrollEl.element, 'clientHeight', { configurable: true, value: 500 })
    const targetRows = wrapper.findAll('.code-preview-line-row.is-target-line')
    expect(targetRows).toHaveLength(2)
    targetRows.forEach((row, i) => {
      Object.defineProperty(row.element, 'offsetTop', { configurable: true, value: 900 + i * 20 })
      Object.defineProperty(row.element, 'offsetParent', { configurable: true, value: scrollEl.element })
      Object.defineProperty(row.element, 'clientHeight', { configurable: true, value: 20 })
    })
    Object.defineProperty(scrollEl.element, 'scrollTop', { configurable: true, writable: true, value: 0 })

    const exposed = wrapper.vm as unknown as { scrollToTargetLine: () => void }
    exposed.scrollToTargetLine()
    await vi.waitFor(() => {
      expect(scrollEl.element.scrollTop).toBe(900 - Math.floor((500 - 40) / 2))
    })
  })

  it('preserves the read position when loading above inserts content', async () => {
    const { wrapper, loadMoreAbove } = mountBody({ remainingAbove: 50, remainingBelow: 50 })
    const el = wrapper.find('.code-preview-scroll').element
    setGeometry(el, { scrollTop: 300, scrollHeight: 4000, clientHeight: 400 })

    // The load grows the content by 200px; scrollTop must shift by that much or
    // the user's reading position jumps.
    loadMoreAbove.mockImplementation(async () => {
      setGeometry(el, { scrollTop: 300, scrollHeight: 4200, clientHeight: 400 })
    })

    await (wrapper.vm as unknown as { loadAbove: () => Promise<void> }).loadAbove()
    expect(el.scrollTop).toBe(500)
  })
})
