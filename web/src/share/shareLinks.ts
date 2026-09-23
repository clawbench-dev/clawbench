/**
 * Relative-link navigation for the public file-share view.
 *
 * A shared markdown document often references sibling files
 * (`[guide](./docs/guide.md)`). The in-app annotation pipeline turns such links
 * into click targets that open the file through the authenticated viewer, which
 * the anonymous share SPA cannot use — so in share mode that annotation is
 * skipped entirely (see useMarkdownRenderPipeline.buildMarkdownPreviewDom).
 *
 * This module adds the share-only equivalent: every LOCAL RELATIVE link is
 * rewritten to a deep link into the same share (`/share/{token}?path=<abs>`) and
 * tagged with `data-share-path`. The rewrite means the link keeps working
 * without JS (new tab, middle click, copy-link), while ShareView intercepts
 * plain clicks to switch the document in place instead of reloading.
 *
 * Scope is deliberately narrow:
 * - only `<a href>` relative links — no inline-code paths, no bare text paths
 *   (those need the auth-protected batch-exists endpoint and would misfire);
 * - external links, in-page anchors, `data:`/`javascript:` hrefs and absolute
 *   paths are left untouched;
 * - directories are NOT supported: the token API exposes no listing endpoint, so
 *   a link to a directory simply fails to load and is reported in place.
 */

import { isAbsolutePath, isWindowsAbsolutePath, splitPath } from '@/utils/path.ts'
import { isAnchorLink, isExternalLink } from '@/utils/doubleClickUtils.ts'
import { parseFileUri } from '@/composables/useFilePathAnnotation.ts'
import { getShareToken } from '@/share/shareMode'

/** Event dispatched when a share link is clicked; detail = { path }. */
export const SHARE_OPEN_FILE_EVENT = 'share-open-file'

/** Attribute carrying the resolved absolute target of a share link. */
export const SHARE_PATH_ATTR = 'data-share-path'

/** Class added to annotated share links (styling / test hook). */
export const SHARE_LINK_CLASS = 'share-file-link'

/** Hrefs that must never be treated as a relative file reference. */
function isNonFileHref(href: string): boolean {
  if (isAnchorLink(href)) return true
  if (isExternalLink(href)) return true
  return /^(?:data|javascript|vbscript|blob):/i.test(href)
}

/**
 * Resolve a relative reference against an absolute base directory, collapsing
 * `.` / `..` and preserving the root.
 *
 * Not `resolveRelativePath` from useFilePathAnnotation: that helper drops the
 * leading separator (it is written for project-relative paths), which would turn
 * an absolute `/repo/docs` + `guide.md` into the rootless `repo/docs/guide.md`.
 */
export function resolveShareTarget(baseDir: string, rel: string): string {
  const absolute = isAbsolutePath(baseDir)
  const out: string[] = []
  for (const part of splitPath(baseDir + '/' + rel)) {
    if (part === '' || part === '.') continue
    if (part === '..') {
      if (out.length > 0) out.pop()
      continue
    }
    out.push(part)
  }
  if (out.length === 0) return ''
  const joined = out.join('/')
  // A Windows drive ("E:") must stay the first segment, not gain a leading "/".
  if (isWindowsAbsolutePath(joined + '/')) return joined
  return absolute ? '/' + joined : joined
}

/** Deep link into this share for a target file ('' → the shared file itself). */
export function shareDeepLink(target: string): string {
  const token = getShareToken() || ''
  const base = `/share/${encodeURIComponent(token)}`
  if (!target) return base
  return `${base}?path=${encodeURIComponent(target)}`
}

/**
 * Rewrite local relative `<a href>` links in an already-parsed document to
 * share deep links, mutating it in place. Returns the number annotated.
 */
export function annotateShareLinksIn(doc: Document, baseDir: string): number {
  if (!baseDir) return 0
  let count = 0
  for (const a of doc.querySelectorAll('a[href]')) {
    const raw = a.getAttribute('href') || ''
    if (!raw || isNonFileHref(raw)) continue

    const parsed = parseFileUri(raw)
    if (!parsed.path) continue
    // A leading "/" is a site-root reference in HTML, not a share file.
    if (isAbsolutePath(parsed.path)) continue

    const target = resolveShareTarget(baseDir, parsed.path)
    if (!target) continue

    a.setAttribute(SHARE_PATH_ATTR, target)
    a.classList.add(SHARE_LINK_CLASS)
    // Keep the link usable without the click interceptor (new tab / middle
    // click / copy address): the default navigation lands on the deep link.
    a.setAttribute('href', shareDeepLink(target))
    count += 1
  }
  return count
}

/** String-in / string-out wrapper used by the markdown preview pipeline. */
export function annotateShareLinks(html: string, baseDir: string): string {
  if (!html || !baseDir) return html
  const doc = new DOMParser().parseFromString(html, 'text/html')
  annotateShareLinksIn(doc, baseDir)
  return doc.body.innerHTML
}

/**
 * Intercept a plain click on an annotated share link and ask the share view to
 * switch documents in place. Returns true when the event was handled.
 *
 * Modifier clicks (Ctrl/Cmd) are left to the browser so the deep link opens in a
 * new tab, matching the usual link affordance.
 */
export function handleShareLinkClick(event: MouseEvent): boolean {
  const anchor = (event.target as HTMLElement | null)?.closest<HTMLAnchorElement>(
    `a[${SHARE_PATH_ATTR}]`
  )
  if (!anchor) return false
  const path = anchor.getAttribute(SHARE_PATH_ATTR)
  if (!path) return false

  if (event.ctrlKey || event.metaKey || event.shiftKey) return false

  event.preventDefault()
  event.stopPropagation()
  window.dispatchEvent(new CustomEvent(SHARE_OPEN_FILE_EVENT, { detail: { path } }))
  return true
}
