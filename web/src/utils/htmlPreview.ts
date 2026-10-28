/**
 * HTML preview iframe URL builder — the single source of truth for how an HTML
 * document is loaded into a sandboxed preview iframe (both the full-screen
 * FileViewer and the quick code-link preview pane).
 *
 * Why a URL instead of inlining the content via `srcdoc`: a srcdoc document has
 * no URL of its own and inherits the parent's base, so every relative reference
 * in the document (stylesheets, scripts, images, fonts) resolves against the
 * app root and 404s. Loading through `/api/fs/raw/<dir>/x.html` gives the
 * document its own base URL, so its relative references resolve beside it.
 *
 * This REQUIRES `allow-same-origin` on the iframe sandbox: without it the frame
 * is an opaque origin, its subresource requests are cross-site, and the
 * SameSite=Lax session cookie is withheld — the document still loads (a
 * top-level navigation counts as same-site) but none of its assets do.
 *
 * The cost, accepted deliberately and recorded in fileViewerSandbox.test.ts:
 * with same-origin the previewed HTML runs with the user's session. It can call
 * any authenticated API and read/write the parent document. Previewed HTML is
 * untrusted (AI-written or downloaded), so this is a real trade.
 */

import { isAbsolutePath } from '@/utils/path'
import { buildLocalFileUrl } from '@/utils/download'
// The app's own bar (css/scrollbar.css), injected into the preview document so
// its scrollbars follow the same logic as the app. `?raw` because the preview is
// a separate document that cannot link the app's stylesheets.
import scrollbarCss from '../../css/scrollbar.css?raw'

/**
 * Build the iframe URL for an HTML preview.
 *
 * Returns '' for an EXTERNAL (absolute-path) file, which the caller must treat
 * as "fall back to srcdoc". An external file is served as
 * `/api/fs/raw/?target=/abs/dir/index.html`, and the browser strips the query
 * when deriving the base URL — so the document's base becomes `/api/fs/raw/`,
 * and `src="pic.png"` resolves to `/api/fs/raw/pic.png`, i.e. `<project>/pic.png`.
 * That is silently the WRONG file whenever the project root happens to contain
 * one of the same name, so the URL form is worse than useless here. srcdoc keeps
 * the old (also unresolved, but never-wrong) behaviour for this case.
 *
 * `timestamp` is a cache-buster bumped when the file content changes, so a
 * rewritten HTML file re-renders instead of the iframe serving the cached
 * document.
 */
export function buildHtmlPreviewSrc(path: string, timestamp: number): string {
  if (!path) return ''
  if (isAbsolutePath(path)) return ''
  const base = buildLocalFileUrl(path)
  return base + (base.includes('?') ? '&' : '?') + `t=${timestamp}`
}

/** Marks the style element we inject, so a reload does not stack copies. */
const SCROLLBAR_STYLE_ATTR = 'data-clawbench-scrollbar'

/**
 * The colours the preview document's bar is painted with, resolved from the
 * HOST document.
 *
 * A preview document is a separate document, so it never sees the app's
 * `:root`: the shared sheet's `var(--scrollbar-thumb, …)` falls back to its
 * neutral grey, and the bar reads as "some other app's scrollbar" next to the
 * app's own themed one. These three values are therefore read from the host and
 * baked into the injected sheet.
 *
 * `hover` mirrors the app exactly — the shared sheet's hover rule reads
 * `--text-muted`, not `--scrollbar-thumb-hover` — so the two bars brighten the
 * same way under the cursor. `track` is the panel background: the user's
 * choice is that the gutter belongs to the app's surface, not to the previewed
 * page's own background.
 */
export interface PreviewScrollbarPalette {
  thumb: string
  hover: string
  track: string
}

/** Resolve the palette from the app's current theme. */
export function readHostScrollbarPalette(): PreviewScrollbarPalette {
  const style = getComputedStyle(document.documentElement)
  const read = (name: string, fallback: string) =>
    style.getPropertyValue(name).trim() || fallback
  return {
    thumb: read('--scrollbar-thumb', '#c1c1c1'),
    hover: read('--text-muted', '#999'),
    track: read('--bg-primary', '#ffffff'),
  }
}

/**
 * The palette half of the injected sheet, appended AFTER the shared geometry.
 *
 * Concrete colours rather than variables on the document's `:root`: a previewed
 * page is free to define `--scrollbar-thumb` / `--text-muted` for its own
 * palette, and setting them here would either be silently overridden by its own
 * `:root` rule (source order) or — worse — repaint parts of the page that read
 * the same names. Explicit rules can only affect the scrollbar.
 *
 * The Firefox gate is repeated because the shared sheet's copy carries the
 * fallback variables; the later block in the same <style> wins for `*`.
 */
function scrollbarPaletteCss(palette: PreviewScrollbarPalette): string {
  return `
::-webkit-scrollbar-thumb {
    background: ${palette.thumb};
}
::-webkit-scrollbar-thumb:hover {
    background: ${palette.hover};
}
::-webkit-scrollbar-track {
    background: ${palette.track};
}
@supports not selector(::-webkit-scrollbar) {
    * {
        scrollbar-color: ${palette.thumb} ${palette.track};
    }
}
`
}

/**
 * Give the previewed document the app's scrollbar bar, in the app's colours.
 *
 * The preview is a separate document loaded by URL (see buildHtmlPreviewSrc)
 * and cannot link the app's stylesheets, so without this its scrollbars are the
 * browser's native ones — a different bar from the app's, and carrying the ▲/▼
 * arrow buttons the app hides. Called from the iframe's `load` handler; the
 * frame is same-origin (the sandbox deliberately keeps `allow-same-origin`, see
 * the file header), so its document is reachable.
 *
 * The sheet is inserted as the FIRST child of <head>, so any scrollbar rule the
 * previewed document defines itself — inline, in a <style>, or in a linked
 * stylesheet — comes later in source order and wins. That is the deliberate
 * contract: the app's bar applies only where the document has not expressed its
 * own preference. Appending instead would override the document's styling,
 * which is not what a preview should do.
 *
 * The width split (4px on a coarse pointer, 8px otherwise) follows the app,
 * since the media feature is evaluated in this document.
 *
 * Re-running this on a document that already has the sheet REWRITES it with the
 * current palette (see installPreviewStyle) — that is what watchPreviewTheme
 * relies on after a theme switch.
 *
 * Returns true when the sheet is present afterwards (installed now or already
 * there), false when the document was unreachable.
 */
export function installPreviewScrollbar(
  iframe: HTMLIFrameElement | null,
  palette: PreviewScrollbarPalette = readHostScrollbarPalette(),
): boolean {
  return installPreviewStyle(
    iframe,
    SCROLLBAR_STYLE_ATTR,
    scrollbarCss + scrollbarPaletteCss(palette),
  )
}

/**
 * Repaint the preview document's app-supplied sheets when the host theme
 * changes, and return a disposer.
 *
 * The injected colours are resolved values, so a theme switch while a preview is
 * open would leave the bar in the previous theme's palette until the document
 * reloaded (which may be never — the user can sit on one preview). The search
 * highlight is re-installed for the same reason: it inlines the accent.
 *
 * Registered per load (a reload is a new document, but the host listener is
 * unchanged) — the caller disposes the previous registration first, exactly like
 * the other in-frame hooks.
 */
export function watchPreviewTheme(
  getIframe: () => HTMLIFrameElement | null,
): () => void {
  const onThemeChange = () => {
    const iframe = getIframe()
    if (!iframe) return
    installPreviewScrollbar(iframe)
    installPreviewSearchStyles(iframe)
  }
  window.addEventListener('clawbench-theme-change', onThemeChange)
  return () => window.removeEventListener('clawbench-theme-change', onThemeChange)
}

/** Marks the search-highlight style element we inject into a preview document. */
const SEARCH_STYLE_ATTR = 'data-clawbench-search'

/**
 * Search-match highlight colours for the previewed document.
 *
 * Mirrors PreviewSearchBar's `.markdown-body mark.md-search-match` rules
 * (search-bar.css) so a find in an HTML preview looks exactly like a find in a
 * rendered markdown preview. The mark classes are shared via documentSearch.ts;
 * only the stylesheet lives here, because a preview document cannot link the
 * app's CSS — so the accent is resolved from the host and inlined.
 *
 * The active-match flash needs its own copy of the keyframes: `line-flash`
 * lives in the HOST document (assets/code-viewer.css) and does not apply
 * inside the frame. The name is prefixed so a previewed document that happens
 * to define `line-flash` itself cannot collide with ours.
 */
function searchHighlightCss(accent: string): string {
  return `
mark.md-search-match {
  background: color-mix(in srgb, ${accent} 28%, transparent);
  color: inherit;
  padding: 0;
}
mark.md-search-match-active {
  background: color-mix(in srgb, ${accent} 55%, transparent);
}
@keyframes clawbench-search-flash {
  0%, 100% { background: transparent; }
  14%      { background: color-mix(in srgb, ${accent} 40%, transparent); }
  45%      { background: color-mix(in srgb, ${accent} 18%, transparent); }
}
mark.md-search-match.search-match-flash {
  animation: clawbench-search-flash 0.7s ease-out 1;
}
@media (prefers-reduced-motion: reduce) {
  mark.md-search-match.search-match-flash { animation: none !important; }
}
`
}

/** The app's accent, for the preview document's search highlights. */
function hostAccentColor(): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--accent-color').trim()
  return v || '#4a90d9'
}

/**
 * Give the previewed document the search-match highlight styles.
 *
 * Appended (not inserted first, unlike the scrollbar sheet): these rules only
 * ever target our own `mark.md-search-match*` elements, which the document
 * itself never produces, so there is nothing to defer to — and appending keeps
 * them last, i.e. they cannot be overridden by a broad `mark { }` rule the
 * previewed document happens to declare.
 *
 * Idempotent, so it can run on every iframe load. `accent` is a parameter for
 * tests; production reads it from the host theme.
 */
export function installPreviewSearchStyles(
  iframe: HTMLIFrameElement | null,
  accent: string = hostAccentColor(),
): boolean {
  return installPreviewStyle(iframe, SEARCH_STYLE_ATTR, searchHighlightCss(accent), false)
}

/**
 * Shared injector for the preview document's app-supplied stylesheets.
 *
 * Idempotent by attribute: a second call REPLACES the existing sheet's text
 * rather than stacking a copy. Rewriting (not returning early) is what lets a
 * theme switch repaint the sheet in place — the resolved colours are baked into
 * the text, so keeping the old node would keep the old palette.
 *
 * A sheet that is already present keeps its original position: `first` decides
 * where a NEW sheet lands, and moving an existing one on every repaint would
 * silently change which rules win against the previewed document's own.
 */
function installPreviewStyle(
  iframe: HTMLIFrameElement | null,
  attr: string,
  css: string,
  first = true,
): boolean {
  if (!iframe) return false
  let doc: Document | null
  try {
    doc = iframe.contentDocument
  } catch {
    // Cross-origin (sandbox lost allow-same-origin) — nothing we can do.
    return false
  }
  if (!doc || !doc.head) return false
  const existing = doc.head.querySelector(`style[${attr}]`)
  if (existing) {
    if (existing.textContent !== css) existing.textContent = css
    return true
  }

  const style = doc.createElement('style')
  style.setAttribute(attr, '')
  style.textContent = css
  if (first) doc.head.insertBefore(style, doc.head.firstChild)
  else doc.head.appendChild(style)
  return true
}
