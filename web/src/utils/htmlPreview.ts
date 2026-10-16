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
 * Give the previewed document the app's scrollbar bar.
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
 * Colours come from the sheet's fallbacks (a neutral grey thumb, transparent
 * track): the previewed document's own theme is unknown, so the app's themed
 * colours would be arbitrary there. The width split (4px on a coarse pointer,
 * 8px otherwise) still follows the app, since the media feature is evaluated in
 * this document.
 *
 * Returns true when the sheet is present afterwards (installed now or already
 * there), false when the document was unreachable.
 */
export function installPreviewScrollbar(iframe: HTMLIFrameElement | null): boolean {
  if (!iframe) return false
  let doc: Document | null
  try {
    doc = iframe.contentDocument
  } catch {
    // Cross-origin (sandbox lost allow-same-origin) — nothing we can do.
    return false
  }
  if (!doc || !doc.head) return false
  if (doc.head.querySelector(`style[${SCROLLBAR_STYLE_ATTR}]`)) return true

  const style = doc.createElement('style')
  style.setAttribute(SCROLLBAR_STYLE_ATTR, '')
  style.textContent = scrollbarCss
  doc.head.insertBefore(style, doc.head.firstChild)
  return true
}
