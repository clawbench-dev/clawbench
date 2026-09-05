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
          expandAbove: 'Expand {n} above',
          expandBelow: 'Expand {n} below',
          expandToTop: 'Expand to top',
          expandToBottom: 'Expand to bottom',
          linesRemaining: '{n} remaining',
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
  const expandAboveLines = vi.fn()
  const expandBelowLines = vi.fn()
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
      stepAbove: 0,
      stepBelow: 0,
      expandAboveLines,
      expandBelowLines,
      ...overrides,
    },
  })
  return { wrapper, expandAboveLines, expandBelowLines }
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

  it('shows expand bars only when lines remain and triggers expand handlers', async () => {
    const { wrapper, expandAboveLines, expandBelowLines } = mountBody({
      remainingAbove: 9,
      remainingBelow: 20,
      stepAbove: 9,
      stepBelow: 10,
    })
    expect(wrapper.find('.expand-above').exists()).toBe(true)
    expect(wrapper.find('.expand-below').exists()).toBe(true)

    await wrapper.find('.expand-above button').trigger('click')
    expect(expandAboveLines).toHaveBeenCalledWith(9)

    await wrapper.find('.expand-below button').trigger('click')
    expect(expandBelowLines).toHaveBeenCalledWith(10)

    // No remaining lines -> bars hidden
    const none = mountBody()
    expect(none.wrapper.find('.expand-above').exists()).toBe(false)
    expect(none.wrapper.find('.expand-below').exists()).toBe(false)
  })

  it('renders "expand all" buttons only when more than one step remains', () => {
    const more = mountBody({ remainingAbove: 30, remainingBelow: 25, stepAbove: 10, stepBelow: 10 })
    expect(more.wrapper.findAll('.expand-all')).toHaveLength(2)

    const exact = mountBody({ remainingAbove: 10, remainingBelow: 5, stepAbove: 10, stepBelow: 5 })
    expect(exact.wrapper.findAll('.expand-all')).toHaveLength(0)
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
})
