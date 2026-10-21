import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Ctrl+F on a rendered HTML preview must open the inline find bar, not the
 * SearchDrawer.
 *
 * The rendered preview is an iframe holding its own document. The SearchDrawer
 * scans the file's SOURCE text, so on the rendered view it could only list
 * matches that are nowhere on screen — and its "jump" would do nothing. The
 * inline bar (PreviewSearchBar with `iframe`) searches and highlights inside
 * the previewed document instead.
 *
 * App.vue has no component test (its only tests read it as source), and the
 * route lives in a computed + a branch inside openFileSearch, so these guards
 * pin the wiring a refactor can silently drop.
 */
describe('Ctrl+F routing for the rendered HTML preview', () => {
  const app = readWebFile('src/App.vue')

  function bodyOf(fn: string) {
    const body = app.slice(app.indexOf(`function ${fn}`))
    return body.slice(0, body.indexOf('\n}'))
  }

  it('declares an isHtmlRenderedView computed gated on isHtml + rendered mode', () => {
    expect(app).toContain('const isHtmlRenderedView = computed(')
    const body = app.slice(app.indexOf('const isHtmlRenderedView = computed('))
    const head = body.slice(0, body.indexOf('\n})'))
    expect(head).toContain('ft.isHtml')
    expect(head).toContain("markdownViewMode.value === 'rendered'")
  })

  it('routes openFileSearch through the inline bar for HTML rendered view', () => {
    const head = bodyOf('openFileSearch')
    expect(head).toContain('isHtmlRenderedView.value')
    expect(head).toContain('focusSearchInput()')
    // The drawer is the fallback only; it must not be the HTML branch.
    expect(head.indexOf('isHtmlRenderedView.value')).toBeLessThan(
      head.indexOf('openFileViewSearchDrawer()'),
    )
  })

  it('clears the search highlight when the HTML view swaps to raw/source', () => {
    // Otherwise the header button stays lit after the inline bar unmounts.
    expect(app).toMatch(/isMarkdownRenderedView\.value,\s*isHtmlRenderedView\.value/)
  })

  it('enables the quote bridge for the rendered preview (chatQuote)', () => {
    // The bridge is what lets a text selection inside the iframe surface the
    // quote bar at all — the host document never sees that selection.
    expect(app).toMatch(/:chat-quote="true"/)
  })
})

describe('HTML preview find bar wiring', () => {
  const viewer = readWebFile('src/components/file/FileViewer.vue')

  it('renders PreviewSearchBar with the preview iframe', () => {
    expect(viewer).toContain('<PreviewSearchBar')
    expect(viewer).toMatch(/ref="htmlSearchBarRef"[\s\S]{0,200}:iframe="htmlPreviewRef"/)
  })

  it('routes focusSearchInput to the HTML bar in rendered mode', () => {
    const body = viewer.slice(viewer.indexOf('function focusSearchInput'))
    const head = body.slice(0, body.indexOf('\n}'))
    expect(head).toContain('htmlSearchBarRef.value?.focus')
    expect(head).toContain('isHtml.value')
  })

  it('attaches the selection→quote bridge on iframe load and disposes it', () => {
    expect(viewer).toContain('attachIframeSelectionQuoteBridge')
    const loadBody = viewer.slice(viewer.indexOf('function onHtmlPreviewLoad'))
    expect(loadBody.slice(0, loadBody.indexOf('\n}'))).toContain('attachHtmlQuoteBridge')
    // Unmounting must drop the bridge: its listeners live on the frame's own
    // document, which nothing can see afterwards.
    const unmountBody = viewer.slice(viewer.indexOf('scrollRestore.dispose()'))
    expect(unmountBody.slice(0, unmountBody.indexOf('\n})'))).toContain('detachHtmlQuoteBridge()')
  })

  it('forwards Ctrl+F pressed INSIDE the previewed document', () => {
    // A keydown in the frame never reaches App.vue's document-level handler, so
    // without this the browser's own find bar opens instead of ours.
    expect(viewer).toContain('installIframeSearchShortcut')
    const loadBody = viewer.slice(viewer.indexOf('function onHtmlPreviewLoad'))
    expect(loadBody.slice(0, loadBody.indexOf('\n}'))).toContain('attachHtmlSearchShortcut')
    expect(viewer).toContain("emit('openSearch')")
  })

  it('drops the in-frame shortcut listener when the preview goes away', () => {
    // The listener lives on the frame's window: leaving it attached keeps a
    // handler alive on a document nothing can see. There are three teardown
    // paths (unmount, file switch, rendered→source) and all must drop it.
    const teardowns = viewer.split('disposeHtmlSearchShortcut?.()').length - 1
    expect(teardowns).toBeGreaterThanOrEqual(3)
  })

  it('keeps the quote bar when the SAME document merely reloads', () => {
    // An fsnotify refresh re-attaches the bridge over the same file; the user
    // may be mid-comment. Only the leave-for-good paths may dismiss the bar, so
    // the reload path must go through the listener-only teardown.
    const attachBody = viewer.slice(viewer.indexOf('async function attachHtmlQuoteBridge'))
    expect(attachBody.slice(0, attachBody.indexOf('\n}'))).toContain('disposeHtmlQuoteBridge()')
    expect(attachBody.slice(0, attachBody.indexOf('\n}'))).not.toContain('detachHtmlQuoteBridge()')
  })
})
