import { describe, expect, it, vi } from 'vitest'
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
    // Attached to the document: jsdom only materializes `contentDocument` /
    // `contentWindow` for a frame that is actually in the tree, and the
    // in-document hooks are attached to that window.
    attachTo: document.body,
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

/**
 * The quick-preview card renders the same sandboxed document as the full-screen
 * viewer, but the in-document hooks live inside the FRAME's document — the host
 * cannot see its keydowns. Wiring them up here is what makes Ctrl+F work in the
 * card instead of falling through to the browser's own find bar.
 *
 * jsdom leaves `contentWindow` null for an unattached frame, so the listener is
 * driven through the frame's own document (the window and the document share
 * one event path for a bubbling keydown).
 */
describe('HtmlPreviewBody — in-document hooks', () => {
  /** Dispatch `load` and let the handler (and its awaits) run. */
  async function loadFrame(wrapper: ReturnType<typeof mountBody>) {
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    iframe.dispatchEvent(new Event('load'))
    await Promise.resolve()
    await Promise.resolve()
    return iframe
  }

  /** Fire Ctrl+F inside the previewed document. */
  function pressCtrlF(iframe: HTMLIFrameElement): KeyboardEvent {
    const doc = iframe.contentDocument!
    const event = new (doc.defaultView!.KeyboardEvent)('keydown', {
      key: 'f',
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    })
    doc.dispatchEvent(event)
    return event
  }

  it('forwards Ctrl+F from inside the frame to the caller', async () => {
    const onOpenSearch = vi.fn()
    const wrapper = mountBody({ onOpenSearch })
    const iframe = await loadFrame(wrapper)

    pressCtrlF(iframe)

    expect(onOpenSearch).toHaveBeenCalledTimes(1)
  })

  it('does not claim Ctrl+F when no handler was supplied', async () => {
    // Without a find bar to open, the chord must stay with the browser rather
    // than being swallowed into nothing.
    const wrapper = mountBody()
    const iframe = await loadFrame(wrapper)

    const event = pressCtrlF(iframe)

    expect(event.defaultPrevented).toBe(false)
  })

  it('re-attaches the shortcut after the document reloads', async () => {
    // A reload is a NEW document: the previous listener died with the old one.
    // `load` fires again for every navigation, so the handler must dispose the
    // old hook and attach a fresh one — attaching twice would open the bar
    // twice on a single press.
    const onOpenSearch = vi.fn()
    const wrapper = mountBody({ onOpenSearch })
    const iframe = await loadFrame(wrapper)

    // A second load event is what a re-navigation delivers.
    await loadFrame(wrapper)

    pressCtrlF(iframe)

    expect(onOpenSearch).toHaveBeenCalledTimes(1)
  })

  it('removes the shortcut listener on unmount', async () => {
    const onOpenSearch = vi.fn()
    const wrapper = mountBody({ onOpenSearch })
    const iframe = await loadFrame(wrapper)
    const doc = iframe.contentDocument!

    wrapper.unmount()

    const event = new (doc.defaultView!.KeyboardEvent)('keydown', {
      key: 'f',
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    })
    doc.dispatchEvent(event)

    expect(onOpenSearch).not.toHaveBeenCalled()
    // And the chord is no longer claimed.
    expect(event.defaultPrevented).toBe(false)
  })
})
