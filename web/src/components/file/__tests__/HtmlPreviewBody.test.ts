import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import HtmlPreviewBody from '@/components/file/HtmlPreviewBody.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        codePreview: {
          loading: 'Loading...',
          retry: 'Retry',
        },
      },
    },
  },
})

function mountBody(overrides: Record<string, unknown> = {}) {
  return mount(HtmlPreviewBody, {
    global: { plugins: [i18n] },
    props: {
      status: 'ready',
      errorMessageText: '',
      errorCode: null,
      htmlSrc: '/api/fs/raw/public/index.html?t=1',
      htmlContent: '<html><body>hi</body></html>',
      filePath: 'public/index.html',
      ...overrides,
    },
  })
}

describe('HtmlPreviewBody.vue', () => {
  it('renders a sandboxed iframe with allow-scripts + allow-same-origin', () => {
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe')
    expect(iframe.exists()).toBe(true)
    // The deliberate trade: same-origin is required so the document's own
    // relative assets load. See fileViewerSandbox.test.ts.
    expect(iframe.attributes('sandbox')).toBe('allow-scripts allow-same-origin')
  })

  it('loads the document by URL when htmlSrc is present', () => {
    const wrapper = mountBody({ htmlSrc: '/api/fs/raw/a.html?t=9' })
    const iframe = wrapper.find('iframe.html-preview-iframe')
    expect(iframe.attributes('src')).toBe('/api/fs/raw/a.html?t=9')
    expect(iframe.attributes('srcdoc')).toBeUndefined()
  })

  it('falls back to srcdoc when htmlSrc is empty (external file)', () => {
    const wrapper = mountBody({ htmlSrc: '', htmlContent: '<html><body>external</body></html>' })
    const iframe = wrapper.find('iframe.html-preview-iframe')
    expect(iframe.attributes('src')).toBeUndefined()
    expect(iframe.attributes('srcdoc')).toContain('external')
  })

  it('shows the loading shell while status is loading', () => {
    const wrapper = mountBody({ status: 'loading' })
    expect(wrapper.find('.code-preview-status').exists()).toBe(true)
    expect(wrapper.find('iframe.html-preview-iframe').exists()).toBe(false)
  })

  it('shows the error shell with a retry button for network errors', async () => {
    const wrapper = mountBody({ status: 'error', errorCode: 'network', errorMessageText: 'boom' })
    expect(wrapper.find('.code-preview-status').text()).toContain('boom')
    const retry = wrapper.find('.code-preview-status button')
    expect(retry.exists()).toBe(true)
    await retry.trigger('click')
    expect(wrapper.emitted('refresh')).toBeTruthy()
  })

  it('exposes no-op scroll helpers so the parent surface is identical', () => {
    const wrapper = mountBody()
    // Both must be callable without throwing — the rendered view has no rows.
    expect(() => (wrapper.vm as any).scrollToTargetLine()).not.toThrow()
    expect(() => (wrapper.vm as any).scrollLineIntoView(3)).not.toThrow()
    expect((wrapper.vm as any).scrollContainer).toBeNull()
  })
})
