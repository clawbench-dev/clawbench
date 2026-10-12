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
