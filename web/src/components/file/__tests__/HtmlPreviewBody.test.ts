import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
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

/**
 * Height contract for the previewed document.
 *
 * The surfaces that host this body are content-sized (the floating card and the
 * touch sheet both grow to fit what they show). A frame whose only height came
 * from `height: 100%` / `flex: 1` therefore had no definite size to resolve
 * against and fell back to the replaced-element default of 150px — a full-page
 * document opened as a short strip (measured in Chromium: card 207px for a
 * 1123px document). The body now measures the document on `load` and feeds that
 * back as the frame's height, which the card's `max-height` then caps.
 */
describe('HtmlPreviewBody — document height', () => {
  /**
   * Put a body into the frame's document and fire `load`.
   *
   * jsdom neither fetches `src` nor renders `srcdoc`, so the frame's document
   * starts empty (`body === null`). Writing one in gives the component the same
   * thing a real load provides. jsdom has no layout engine either, so
   * `scrollHeight` is always 0 — stub it to what a document of this height
   * would report.
   */
  async function loadWithBodyHeight(
    wrapper: ReturnType<typeof mountBody>,
    height: number,
  ) {
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    doc.open()
    doc.write('<html><body><div>document</div></body></html>')
    doc.close()
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: height })
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()
    return { iframe, doc }
  }

  it('sizes the frame to the loaded document', async () => {
    const wrapper = mountBody()
    const { iframe } = await loadWithBodyHeight(wrapper, 1123)

    expect(iframe.style.height).toBe('1123px')
  })

  it('leaves the frame unmeasured before the document loads', () => {
    // An inline height of 0 would collapse the card while the frame is still
    // loading; unset keeps the stylesheet's `100%`.
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    expect(iframe.style.height).toBe('')
  })

  it('re-measures on reload instead of keeping the previous document height', async () => {
    // A reload is a new document; a stale height would leave the card sized for
    // the file that was open before.
    const wrapper = mountBody()
    await loadWithBodyHeight(wrapper, 1123)
    const { iframe } = await loadWithBodyHeight(wrapper, 200)

    expect(iframe.style.height).toBe('200px')
  })

  it('falls back to documentElement when the body reports no height', async () => {
    // A document that styles its body out of flow (or leaves it empty) reports
    // nothing on `body`; the root element is the remaining source of truth.
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    doc.open()
    doc.write('<html><body></body></html>')
    doc.close()
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 0 })
    Object.defineProperty(doc.documentElement, 'scrollHeight', { configurable: true, value: 640 })
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()

    expect(iframe.style.height).toBe('640px')
  })

  it('ignores a document that reports no height at all', async () => {
    // A frame whose document is unreachable/empty must not be pinned to 0px,
    // which would hide the preview entirely.
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()

    expect(iframe.style.height).toBe('')
  })
})

/**
 * jsdom has no CSS engine, so the flex-basis half of the contract is a
 * source check. `flex: 1` expands to `1 1 0%`; a zero basis ignores the inline
 * height the component measures and the frame collapses to 150px again —
 * verified in Chromium: with `flex: 1` the measured 1123px document still
 * rendered a 150px frame, with `flex: 1 1 auto` the card grew to its cap.
 */
describe('HtmlPreviewBody — iframe must honour the measured height', () => {
  const source = readFileSync(
    resolve(__dirname, '../HtmlPreviewBody.vue'),
    'utf8',
  )

  it('keeps a content-based flex-basis on the frame', () => {
    const rule = source.match(/\.html-preview-iframe\s*\{([\s\S]*?)\}/)
    expect(rule, '.html-preview-iframe rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/flex:\s*1 1 auto/)
    // The shorthand that would silently reset the basis to 0.
    expect(rule![1]).not.toMatch(/flex:\s*1\s*;/)
  })
})

describe('HtmlPreviewBody — document height resets with the target', () => {
  it('drops the previous document height when the file changes', async () => {
    // A new target mounts a frame that would otherwise keep the OLD document's
    // height until its own load fires — a tall file followed by a short one
    // would flash at the stale size.
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    doc.open()
    doc.write('<html><body><div>tall</div></body></html>')
    doc.close()
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 1123 })
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()
    expect(iframe.style.height).toBe('1123px')

    await wrapper.setProps({ htmlSrc: '/api/fs/raw/public/other.html?t=2' })

    const next = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    expect(next.style.height).toBe('')
  })
})
