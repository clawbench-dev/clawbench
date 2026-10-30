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

  it('hands outbound link clicks to the system browser', async () => {
    // The sandbox has no `allow-popups`, so a previewed page's `target="_blank"`
    // link is a dead tap unless the component takes the activation over. This
    // is the wiring test: the helper's own boundary rules live in
    // utils/__tests__/htmlPreview.test.ts.
    const open = vi.fn()
    ;(window as unknown as { ClawBenchNative: unknown }).ClawBenchNative = { openExternalUrl: open }
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    // jsdom starts the frame's document empty; write one in, as a real load does.
    doc.open()
    doc.write(
      '<html><body><a href="https://github.com/clawbench-dev/clawbench" target="_blank">GitHub</a></body></html>',
    )
    doc.close()
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()

    const event = new (doc.defaultView!.MouseEvent)('click', {
      bubbles: true,
      cancelable: true,
      button: 0,
    })
    doc.querySelector('a')!.dispatchEvent(event)

    expect(open).toHaveBeenCalledWith('https://github.com/clawbench-dev/clawbench')
    expect(event.defaultPrevented).toBe(true)
    delete (window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative
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

  it('also resets on the srcdoc branch, where htmlSrc never changes', async () => {
    // External (absolute-path) files take the srcdoc branch and keep htmlSrc at
    // '' for their whole life. An htmlSrc-only watcher never fired there, so a
    // 3000px external document followed by a 200px one kept the stale inline
    // height until the new document loaded — the card visibly collapsed.
    const wrapper = mountBody({ htmlSrc: '', htmlContent: '<html><body>tall</body></html>' })
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    doc.open()
    doc.write('<html><body><div>tall</div></body></html>')
    doc.close()
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 3000 })
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()
    expect(iframe.style.height).toBe('3000px')

    // Same branch (htmlSrc stays ''), different file.
    await wrapper.setProps({
      filePath: 'elsewhere/short.html',
      htmlContent: '<html><body>short</body></html>',
    })

    const next = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    expect(next.style.height).toBe('')
  })
})

/**
 * `load` fires before the document's webfonts settle, so a layout that depends
 * on one is measured at its fallback-font height. The frame then stays too
 * short and the card shows a scrollbar over text that would have fit.
 *
 * The fix is ONE compensation pass on `document.fonts.ready`, not an observer:
 * an observer on a `height: 100vh` document feeds back (measure → grow frame →
 * grow viewport → measure again).
 */
describe('HtmlPreviewBody — webfont reflow compensation', () => {
  /** Mount a loaded document with a controllable `fonts.ready`. */
  async function loadWithFonts(wrapper: ReturnType<typeof mountBody>, height: number) {
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    doc.open()
    doc.write('<html><body><div>text</div></body></html>')
    doc.close()
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: height })
    let resolveFonts!: () => void
    const ready = new Promise<void>((res) => { resolveFonts = res })
    Object.defineProperty(doc, 'fonts', {
      configurable: true,
      value: { ready },
    })
    iframe.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()
    return { iframe, doc, resolveFonts: () => resolveFonts() }
  }

  it('grows the frame to the post-webfont height', async () => {
    const wrapper = mountBody()
    const { iframe, doc, resolveFonts } = await loadWithFonts(wrapper, 900)
    expect(iframe.style.height).toBe('900px')

    // The webfont arrives and the text reflows taller.
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 1200 })
    resolveFonts()
    await Promise.resolve()
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    expect(iframe.style.height).toBe('1200px')
  })

  it('never shrinks the frame after the webfont settles', async () => {
    // A reflow that shortens the document must not resize the frame under a
    // reader mid-scroll; the earlier (slightly generous) height stays.
    const wrapper = mountBody()
    const { iframe, doc, resolveFonts } = await loadWithFonts(wrapper, 1200)

    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 800 })
    resolveFonts()
    await Promise.resolve()
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    expect(iframe.style.height).toBe('1200px')
  })

  it('caps how far a single reflow may grow the frame', async () => {
    // The compensation runs once, but a `vh`-sized document's reported height
    // is derived from the frame's own height, so an unbounded adoption would
    // multiply the card. Real font reflows add tens of percent.
    const wrapper = mountBody()
    const { iframe, doc, resolveFonts } = await loadWithFonts(wrapper, 100)

    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 100_000 })
    resolveFonts()
    await Promise.resolve()
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    expect(iframe.style.height).toBe('100px')
  })

  it('ignores a fonts promise from a superseded document', async () => {
    // The user switched files while the old document's font load was still in
    // flight. The pass must re-read the CURRENT document: adopting the old
    // one's height would resize the frame to a number that describes neither
    // file. (The height is within the growth cap here, so only reading the
    // current document can reject it.)
    const wrapper = mountBody()
    const { doc, resolveFonts } = await loadWithFonts(wrapper, 900)

    await wrapper.setProps({ filePath: 'other.html', htmlSrc: '/api/fs/raw/public/other.html?t=2' })
    const next = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    expect(next.style.height).toBe('')

    // The new document loads and measures itself.
    const nextDoc = next.contentDocument!
    nextDoc.open()
    nextDoc.write('<html><body><div>new</div></body></html>')
    nextDoc.close()
    Object.defineProperty(nextDoc.body, 'scrollHeight', { configurable: true, value: 700 })
    next.dispatchEvent(new Event('load'))
    await wrapper.vm.$nextTick()
    expect(next.style.height).toBe('700px')

    // The OLD document's fonts now settle and it reports a taller height.
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 2000 })
    resolveFonts()
    await Promise.resolve()
    await Promise.resolve()
    await wrapper.vm.$nextTick()

    expect(next.style.height).toBe('700px')
  })

  it('does not throw when the document has no fonts API', async () => {
    // jsdom has no `document.fonts`; neither do some older engines for a
    // sub-document. The absence must leave the plain measurement in place.
    const wrapper = mountBody()
    const iframe = wrapper.find('iframe.html-preview-iframe').element as HTMLIFrameElement
    const doc = iframe.contentDocument!
    doc.open()
    doc.write('<html><body><div>no fonts api</div></body></html>')
    doc.close()
    Object.defineProperty(doc.body, 'scrollHeight', { configurable: true, value: 640 })

    expect(() => iframe.dispatchEvent(new Event('load'))).not.toThrow()
    await wrapper.vm.$nextTick()
    expect(iframe.style.height).toBe('640px')
  })
})
