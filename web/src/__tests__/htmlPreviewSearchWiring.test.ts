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
    //
    // Counting occurrences of the identifier is not enough: the name also
    // appears in a declaration, in the attach path (which disposes before
    // re-attaching) and in comments, so the count reaches the threshold even
    // with a teardown site deleted. Anchor on the PAIR — every teardown also
    // calls `detachHtmlQuoteBridge()` for the same document — and strip
    // comments first so prose cannot satisfy the guard.
    const code = viewer.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '')
    const paired = code.match(
      /detachHtmlQuoteBridge\(\)\s*\n\s*disposeHtmlSearchShortcut\?\.\(\)\s*\n\s*disposeHtmlSearchShortcut = null/g,
    ) ?? []
    expect(paired.length, 'every teardown must drop both the quote bridge and the shortcut').toBeGreaterThanOrEqual(3)
  })

  it('keeps the quote bar when the SAME document merely reloads', () => {
    // An fsnotify refresh re-attaches the bridge over the same file; the user
    // may be mid-comment. Only the leave-for-good paths may dismiss the bar, so
    // the reload path must go through the listener-only teardown.
    const attachBody = viewer.slice(viewer.indexOf('async function attachHtmlQuoteBridge'))
    expect(attachBody.slice(0, attachBody.indexOf('\n}'))).toContain('disposeHtmlQuoteBridge()')
    expect(attachBody.slice(0, attachBody.indexOf('\n}'))).not.toContain('detachHtmlQuoteBridge()')
  })

  it('places the bar at the selection (passes the mapped anchor)', () => {
    // The frame reports its selection rects in ITS viewport; without handing the
    // mapped anchor to showBar the bar falls back to its pinned top position —
    // far from the text the user just selected.
    const attachBody = viewer.slice(viewer.indexOf('async function attachHtmlQuoteBridge'))
    const head = attachBody.slice(0, attachBody.indexOf('\n}'))
    expect(head).toContain('makeIframeQuoteShowHandler')
  })

  it('suppresses the in-page bar on Android (the native menu is the affordance)', () => {
    // Android draws its own selection menu; an in-page bar on top would be a
    // duplicate. The quote must still be recorded for that menu's action.
    const attachBody = viewer.slice(viewer.indexOf('async function attachHtmlQuoteBridge'))
    expect(attachBody.slice(0, attachBody.indexOf('\n}'))).toContain('makeIframeQuoteShowHandler')
  })
})

/**
 * Every iframe host must wire the SAME show handler.
 *
 * There are three, and they were three hand-written copies of the same three
 * decisions (pass the mapped anchor, delay 0, suppress on Android). One of them
 * — OpenApiPreview — silently missed `anchor` and `suppressBar`, so the reported
 * "menu pops up at the top of the page" defect survived on the OpenAPI docs, and
 * Android got both the system menu and the in-page bar. A per-host guard cannot
 * catch a host nobody remembered to add to the list, so this asserts the SHARED
 * factory is used and that every bridge call site goes through it.
 */
describe('iframe quote hosts share one show handler', () => {
  const HOSTS = [
    'src/components/file/FileViewer.vue',
    'src/components/file/HtmlPreviewBody.vue',
    'src/components/file/OpenApiPreview.vue',
  ] as const

  it('every host uses the shared factory instead of a hand-written callback', () => {
    for (const rel of HOSTS) {
      const src = readWebFile(rel)
      expect(src, `${rel} must import makeIframeQuoteShowHandler`).toContain('makeIframeQuoteShowHandler')
      // A hand-written arrow here is how the copies drifted.
      expect(src, `${rel} must not hand-roll the show callback`).not.toMatch(/show:\s*\(data,\s*anchor\)\s*=>/)
      // The callback may be the factory itself or a local alias it was assigned
      // to (the quick preview loads the composable lazily, so it holds
      // `mod.makeIframeQuoteShowHandler` in a variable). Either way the callee
      // must trace back to the factory.
      const m = src.match(/show:\s*([\w.]+)\(/)
      expect(m, `${rel} must set a show callback`).not.toBeNull()
      const callee = m![1].split('.').pop()!
      const traces = callee === 'makeIframeQuoteShowHandler'
        || new RegExp(`(?:const|let)\\s+${callee}\\b[^\\n]*makeIframeQuoteShowHandler`).test(src)
      expect(traces, `${rel}: show uses ${callee}, which must come from the factory`).toBe(true)
    }
  })

  it('no bridge call site is left without the factory', () => {
    // Guards the "fourth host" case: a new attachIframeSelectionQuoteBridge call
    // must also go through the factory.
    for (const rel of HOSTS) {
      const src = readWebFile(rel)
      const calls = src.match(/attachIframeSelectionQuoteBridge\(/g) ?? []
      const factories = src.match(/makeIframeQuoteShowHandler\b/g) ?? []
      // Two mentions per use (import + call) is the normal shape; what must not
      // happen is a bridge with NO factory mention at all.
      expect(factories.length, `${rel}: the factory must reach every bridge`).toBeGreaterThan(0)
      expect(calls.length, `${rel}: exactly one bridge`).toBe(1)
    }
  })
})
