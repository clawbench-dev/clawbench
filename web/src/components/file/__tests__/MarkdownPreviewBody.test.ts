import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MarkdownPreviewBody from '@/components/file/MarkdownPreviewBody.vue'

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

function mountBody(overrides: Record<string, unknown> = {}) {
  const expandAboveLines = vi.fn()
  const expandBelowLines = vi.fn()
  const wrapper = mount(MarkdownPreviewBody, {
    global: { plugins: [i18n] },
    props: {
      status: 'ready',
      errorMessageText: '',
      errorCode: null,
      renderedHtml: '<h1>Hello</h1>\n<p>World</p>',
      filePath: 'docs/guide.md',
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

describe('MarkdownPreviewBody.vue', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the markdown HTML inside .markdown-body > .markdown-content', () => {
    const { wrapper } = mountBody()
    expect(wrapper.find('.md-preview-scroll').exists()).toBe(true)
    const body = wrapper.find('.markdown-body.md-preview-body')
    expect(body.exists()).toBe(true)
    expect(body.attributes('data-file-path')).toBe('docs/guide.md')
    const content = body.find('.markdown-content')
    expect(content.exists()).toBe(true)
    expect(content.find('h1').text()).toBe('Hello')
  })

  it('shows loading status with aria-live', () => {
    const { wrapper } = mountBody({ status: 'loading' })
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('Loading')
    expect(wrapper.find('.markdown-content').exists()).toBe(false)
  })

  it('shows error status and emits refresh only for network errors', async () => {
    const { wrapper } = mountBody({ status: 'error', errorCode: 'network', errorMessageText: 'boom' })
    expect(wrapper.find('[role="status"]').text()).toContain('boom')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    const noRetry = mountBody({ status: 'error', errorCode: 'not-found', errorMessageText: 'missing' })
    expect(noRetry.wrapper.find('button').exists()).toBe(false)
  })

  it('shows expand bars only when lines remain and triggers the expand handlers', async () => {
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

  it('exposes a scroll container and no-op navigation helpers', () => {
    const { wrapper } = mountBody()
    const exposed = wrapper.vm as unknown as {
      scrollContainer: HTMLElement | null
      scrollToTargetLine: () => void
      scrollLineIntoView: (i: number) => void
    }
    expect(exposed.scrollContainer?.classList.contains('md-preview-scroll')).toBe(true)
    // No-op helpers must not throw (the parent's bodyRef surface expects them).
    expect(() => exposed.scrollToTargetLine()).not.toThrow()
    expect(() => exposed.scrollLineIntoView(3)).not.toThrow()
  })
})
