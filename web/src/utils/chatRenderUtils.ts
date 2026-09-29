/**
 * Pure functions extracted from useChatRender composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */

import { parseAskQuestionXML } from '@/utils/xmlParser.ts'

/** Audio file extensions that should be converted to inline audio players */
const AUDIO_EXTENSIONS = ['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.wma', '.opus']

/** Video file extensions that should be converted to inline video players */
const VIDEO_EXTENSIONS = ['.mp4', '.mkv', '.avi', '.mov', '.webm', '.flv', '.wmv', '.m4v', '.3gp', '.m3u8']

/**
 * Image extensions that the /api/fs/thumb endpoint can rasterize to a JPEG
 * thumbnail (standard-library decoders). SVG/WebP/AVIF/TIFF are excluded and
 * keep serving the original file. GIF is excluded to preserve animation.
 */
export const THUMB_EXTENSIONS = ['.png', '.jpg', '.jpeg']

/** Desktop (PC) inline thumbnail width passed to /api/fs/thumb (clamped 50–1600 by backend). */
export const THUMB_DEFAULT_WIDTH = 1200
/** Mobile inline thumbnail width — smaller viewport needs a smaller, cheaper thumbnail. */
export const THUMB_MOBILE_WIDTH = 640

/**
 * Resolve the inline thumbnail width for the current device. Phones/tablets
 * have small viewports, so a 480px thumbnail is sharp enough and decodes much
 * cheaper than the desktop 800px. Pure function: callers inject isPC.
 */
export function getThumbWidth(isPC: boolean): number {
  return isPC ? THUMB_DEFAULT_WIDTH : THUMB_MOBILE_WIDTH
}

/**
 * Media src forms that are NOT local files and must never be rewritten:
 * remote URLs, protocol-relative URLs, inline data URIs, and URLs already
 * served by one of our own file endpoints (a re-render can see these).
 */
const NON_LOCAL_MEDIA_SRC_RE = /^(https?:|\/\/|data:|\/api\/fs\/)/i

/** A media src resolved to the URL that serves its bytes. */
interface ResolvedMediaSrc {
  /** URL the browser fetches. */
  url: string
  /**
   * Path argument to hand to /api/fs/thumb, or null when the src cannot be
   * thumbnailed (remote/embedded, or no project root to resolve against).
   * For the project-relative form this is already segment-encoded, which is
   * what buildThumbUrl expects; for the external form it is the raw absolute
   * path (the thumb endpoint decodes its query param once).
   */
  thumbPath: string | null
}

/**
 * Resolve a media src to a served URL (plus the matching thumbnail path).
 *
 * - Remote / embedded / already-served srcs pass through untouched.
 * - A project-relative path, or an absolute path INSIDE the project, is served
 *   as `/api/fs/raw/<project-relative>` (segment-encoded per part).
 * - An absolute path OUTSIDE the project is served as
 *   `/api/fs/raw/?target=<absolute>`: the same endpoint accepts absolute
 *   paths directly. Previously EVERY "/"-prefixed src was classified as an
 *   "external URL" and left alone, so the browser requested it from the site
 *   root and got a 404 — an AI-written `![](/tmp/chart.png)` never rendered.
 */
function resolveMediaSrc(src: string, projectRoot?: string): ResolvedMediaSrc | null {
  if (!src || NON_LOCAL_MEDIA_SRC_RE.test(src)) return null

  if (src.startsWith('/')) {
    // Absolute path. Decide project-internal vs external by prefix, so an
    // in-project absolute path keeps the stable relative URL (and its cache
    // key) while everything else is served through the ?path= form.
    if (projectRoot) {
      const normRoot = projectRoot.replace(/\/+$/, '')
      if (src === normRoot) return null // the project root itself is not a file
      if (src.startsWith(normRoot + '/')) {
        const rel = src.slice(normRoot.length + 1)
        return { url: localFileUrlForRelative(rel), thumbPath: encodeSegments(rel) }
      }
    }
    return { url: `/api/fs/raw/?target=${encodeURIComponent(src)}`, thumbPath: encodeSegments(src) }
  }

  if (!projectRoot) return null
  return { url: localFileUrlForRelative(src), thumbPath: encodeSegments(src) }
}

/** Segment-encode a project-relative path, tolerating malformed escapes. */
function encodeSegments(rel: string): string {
  let decoded = rel
  try { decoded = decodeURIComponent(rel) } catch { /* malformed encoding, use as-is */ }
  return decoded.split('/').map((s: string) => encodeURIComponent(s)).join('/')
}

/** Build the `/api/fs/raw/<rel>` URL for an already segment-encoded rel. */
function localFileUrlForRelative(rel: string): string {
  return `/api/fs/raw/${encodeSegments(rel)}`
}

/**
 * Rewrite a media path to a served URL, or return the src unchanged when it is
 * not a local file. Shared by the image/audio/video rewriting steps so all
 * media types resolve identically (including project-external absolute paths).
 */
function resolveLocalMediaSrc(src: string, projectRoot?: string): string {
  return resolveMediaSrc(src, projectRoot)?.url ?? src
}

/**
 * Rewrite image URLs in HTML: convert local file paths to /api/fs/raw/ URLs.
 * For raster formats the thumb endpoint can decode, the inline src is rewritten to a
 * lightweight JPEG thumbnail (/api/fs/thumb?target=...) and the original full-size
 * URL is stored in data-full-src (used by the lightbox to show the full image).
 * Skips remote/embedded URLs. Applies thumbnail styling.
 *
 * Every <img> also gets the `lightbox-img` marker class (+ `chat-img` in chat)
 * so the media-block factory (annotateMediaBlocks) lifts it into the unified
 * bordered figure afterwards. No wrapper span is produced here anymore.
 */
export function rewriteImageUrls(html: string, projectRoot: string, thumbWidth: number = THUMB_DEFAULT_WIDTH): string {
  return html.replace(/<img([^>]*)>/g, (_match, attrs) => {
    let cleanAttrs = attrs.replace(/\s*style="[^"]*"/i, '').replace(/\s*class="[^"]*"/i, '')
    const srcMatch = cleanAttrs.match(/\bsrc="([^"]*)"/)
    if (srcMatch) {
      const src = srcMatch[1]
      const resolved = resolveMediaSrc(src, projectRoot)
      if (resolved && resolved.url !== src) {
        cleanAttrs = cleanAttrs.replace(`src="${src}"`, `src="${escapeHtmlAttr(resolved.url)}"`)
        if (isThumbExtension(src) && resolved.thumbPath) {
          // Inline src → thumbnail; keep the original for the lightbox.
          const fullSrc = escapeHtmlAttr(resolved.url)
          const thumbSrc = buildThumbUrl(resolved.thumbPath, thumbWidth)
          cleanAttrs = cleanAttrs.replace(/src="[^"]*"/, `src="${thumbSrc}" data-full-src="${fullSrc}"`)
        }
      }
    }
    return `<img${cleanAttrs} class="chat-img lightbox-img">`
  })
}

/** True if the file path has an extension the thumb endpoint can rasterize. */
export function isThumbExtension(path: string): boolean {
  const lower = path.toLowerCase()
  return THUMB_EXTENSIONS.some(ext => lower.endsWith(ext))
}

/** HTML void elements — no closing tag, so they must never enter the tag stack. */
const VOID_TAGS = new Set([
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input',
  'link', 'meta', 'param', 'source', 'track', 'wbr',
])

/**
 * Mark bare inline <svg> elements (returned directly by the AI, not rendered
 * from markdown image syntax) with the `lightbox-svg` marker class so the
 * media-block factory (annotateMediaBlocks) lifts them into the unified
 * bordered figure. No wrapper span is produced here anymore.
 *
 * Runs on the rendered HTML string BEFORE mermaid diagrams are produced —
 * at this stage mermaid is still a <pre> code block (no svg). Marking (instead
 * of wrapping) keeps every later <a href>-anchored regex step working: no new
 * wrapper <span> is introduced that could break structural matches.
 *
 * The marker class also makes repeated application idempotent.
 *
 * SVGs already inside an interactive UI element injected by the pipeline
 * (e.g. the lucide icon inside a .chat-file-open-btn button) are skipped —
 * they are not content images and must not get a lightbox affordance.
 *
 * SVGs inside KaTeX-rendered markup are skipped too. KaTeX draws stretchy
 * delimiters (\underbrace, \overbrace, \sqrt, \xrightarrow, \vec …) with its
 * OWN internal <svg> glyph fragments. Those are part of the formula's layout,
 * not content media: marking one makes annotateMediaBlocks lift it into a
 * block-level figure, which tears the formula apart (issue #473).
 */
export function markInlineSvgs(html: string): string {
  const result: string[] = []
  const tagRe = /<\/?([a-zA-Z][a-zA-Z0-9-]*)\b[^>]*>/g
  let lastIndex = 0
  let match: RegExpExecArray | null
  // Stack entries carry the class-derived KaTeX flag alongside the tag name so
  // the ancestry checks below need no second pass over the markup.
  const tagStack: Array<{ name: string; katex: boolean }> = []

  while ((match = tagRe.exec(html))) {
    const tag = match[0]
    const name = match[1].toLowerCase()

    if (name === 'svg') {
      if (tag.startsWith('</')) {
        // Closing svg — the matching open (if any) was pushed as 'svg'.
        if (tagStack[tagStack.length - 1]?.name === 'svg') tagStack.pop()
        continue
      }
      // Opening svg
      const openIndex = match.index
      const openTag = tag

      // Skip content inside interactive UI elements injected by the pipeline.
      const inInteractive = tagStack.some(t => t.name === 'button' || t.name === 'a')

      // Skip KaTeX typography (see the doc comment above).
      const inKatex = tagStack.some(t => t.katex)

      // Idempotency: skip SVGs already carrying the lightbox-svg marker class.
      const alreadyMarked = /(?:^|\s)class\s*=\s*("|')[^"']*\blightbox-svg\b[^"']*\1/i.test(openTag)

      if (alreadyMarked || inInteractive || inKatex) {
        // Treat this svg as a balanced unit: skip its full span untouched.
        const depth = countSvgDepth(html, openIndex + openTag.length)
        if (depth >= 0) {
          result.push(html.slice(lastIndex, openIndex))
          result.push(html.slice(openIndex, depth))
          lastIndex = depth
          tagRe.lastIndex = depth
          continue
        }
        // Unbalanced svg — leave as-is
        tagStack.push({ name: 'svg', katex: false })
        continue
      }

      // Balance-count to the matching close tag (handles nested <svg>).
      const closeEnd = countSvgDepth(html, openIndex + openTag.length)
      if (closeEnd >= 0) {
        const innerHtml = html.slice(openIndex + openTag.length, closeEnd - '</svg>'.length)
        // Add the lightbox-svg marker class, preserving any existing class.
        // Handles both single- and double-quoted class attributes. `(?:^|\s)`
        // (not `\b`) keeps `data-class="…"` from being treated as the class.
        const tagged = /((?:^|\s)class\s*=\s*("|')[^"']*)\2/i.test(openTag)
          ? openTag.replace(/((?:^|\s)class\s*=\s*("|')[^"']*)\2/i, '$1 lightbox-svg$2')
          : openTag.replace(/\/?>$/, ' class="lightbox-svg">')

        result.push(html.slice(lastIndex, openIndex))
        result.push(`${tagged}${innerHtml}</svg>`)
        lastIndex = closeEnd
        tagRe.lastIndex = closeEnd
        continue
      }

      // Unbalanced — treat as normal element for stack tracking
      tagStack.push({ name: 'svg', katex: false })
      continue
    }

    // Track non-svg tags for the interactive-container / KaTeX-ancestry
    // heuristics. A tag counts as KaTeX when its class carries one of the
    // markers KaTeX actually stamps — `katex`, `katex-display`, `katex-html`,
    // `katex-mathml` and `katex-error` — and every internal glyph svg lives
    // under one of them. The suffix set is an explicit allow-list, NOT a
    // `-\w+` wildcard: an AI-authored wrapper like `class="katex-widget"` is
    // not KaTeX markup, and its content svg must keep the media marker.
    if (tag.startsWith('</')) {
      if (tagStack[tagStack.length - 1]?.name === name) tagStack.pop()
    } else if (!/\/>$/.test(tag) && !VOID_TAGS.has(name)) {
      // Void elements (img/br/…) have no closing tag: pushing one would leave a
      // stale entry that never pops, leaking its flags onto every later element
      // — e.g. a <br> inside .katex would suppress the marker on a genuine
      // content svg further down the document.
      // `(?:^|\s)class` (not `\bclass`) so an attribute merely *ending* in
      // "class" — e.g. `data-class="katex"` — is not mistaken for the class.
      const cls = tag.match(/(?:^|\s)class\s*=\s*("([^"]*)"|'([^']*)')/i)
      const classAttr = cls ? (cls[2] ?? cls[3] ?? '') : ''
      const katex = /(^|\s)katex(-display|-html|-mathml|-error)?(\s|$)/.test(classAttr)
      tagStack.push({ name, katex })
    }
  }

  result.push(html.slice(lastIndex))
  return result.join('')
}

/** @deprecated Renamed to markInlineSvgs — kept as an alias for callers/tests. */
export const wrapInlineSvgs = markInlineSvgs

/**
 * Find the end offset (just past the closing </svg>) of the svg element whose
 * open tag ends at `from`. Counts nesting depth so inner <svg> elements are
 * included. Returns -1 when the svg is unclosed/ unbalanced.
 */
function countSvgDepth(html: string, from: number): number {
  let depth = 1
  let pos = from
  while (depth > 0) {
    const tail = html.slice(pos)
    const nextOpen = tail.search(/<svg\b[^>]*>/i)
    const nextClose = tail.indexOf('</svg>')
    if (nextClose === -1) return -1
    if (nextOpen !== -1 && nextOpen < nextClose) {
      depth++
      const nestedTag = tail.slice(nextOpen).match(/^<svg\b[^>]*>/i)![0]
      pos += nextOpen + nestedTag.length
    } else {
      depth--
      if (depth === 0) return pos + nextClose + '</svg>'.length
      pos += nextClose + '</svg>'.length
    }
  }
  return -1
}

/**
 * Build a thumbnail URL for a project-relative, already-segment-encoded path.
 * The URL is kept stable (no cache-buster) so the backend's ETag/Last-Modified
 * revalidation returns fresh content as soon as the source file changes.
 */
export function buildThumbUrl(relPath: string, width: number = THUMB_DEFAULT_WIDTH): string {
  return `/api/fs/thumb?target=${relPath}&w=${width}`
}

/** Escape HTML special characters in attribute values to prevent XSS (ISS-247) */
function escapeHtmlAttr(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

/**
 * Matches a rendered markdown link and captures its href plus inner text.
 *
 * The href is NOT assumed to be the only attribute: `annotateExternalLinkTargets`
 * stamps `target`/`rel` onto external links earlier in the pipeline, and the
 * localhost annotator does the same for its own wrappers. Requiring
 * `<a href="…">` with nothing else made those anchors stop matching, so an
 * external .mp3/.mp4 link silently degraded from an inline player back to a
 * plain link. Extra attributes on either side of href are tolerated; the
 * captured href and inner text are all the media converters need.
 */
const MARKDOWN_LINK_RE = /<a\s+[^>]*href="([^"]+)"[^>]*>([^<]*)<\/a>/g

/**
 * Convert audio file links to inline audio players.
 * Replaces <a href="...mp3"> links with <audio> elements.
 * Project-relative paths (not /api/fs/raw/ or external URLs) are rewritten
 * to /api/fs/raw/ URLs so the browser can load them, mirroring image handling.
 */
export function convertAudioLinks(html: string, projectRoot?: string): string {
  return html.replace(MARKDOWN_LINK_RE, (match, href) => {
    const lower = href.toLowerCase()
    if (AUDIO_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      const src = resolveLocalMediaSrc(href, projectRoot)
      const safeHref = escapeHtmlAttr(src)
      return `<div class="chat-audio-wrapper"><audio src="${safeHref}" controls class="chat-audio-player"></audio></div>`
    }
    return match
  })
}

/**
 * Convert video file links to inline video players.
 * Replaces <a href="...mp4"> links with <video> elements, rewriting
 * project-relative paths to /api/fs/raw/ URLs like audio/images.
 */
export function convertVideoLinks(html: string, projectRoot?: string): string {
  return html.replace(MARKDOWN_LINK_RE, (match, href) => {
    const lower = href.toLowerCase()
    if (VIDEO_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      const src = resolveLocalMediaSrc(href, projectRoot)
      const safeHref = escapeHtmlAttr(src)
      return `<div class="chat-video-wrapper"><video src="${safeHref}" controls class="chat-video-player"></video></div>`
    }
    return match
  })
}

/**
 * Parse ask-question content from XML format.
 *
 * Delegates to the canonical parser (`@/utils/askQuestion.ts`). Returns null if
 * parsing fails or no valid questions were found. The content may include the
 * <clawbench-ask-question> wrapper or be a bare payload.
 */
export function parseAskQuestionContent(rawContent: string): { questions: Array<Record<string, unknown>> } | null {
  return parseAskQuestionXML(rawContent) as { questions: Array<Record<string, unknown>> } | null
}

/** Export audio/video extensions for testing */
export { AUDIO_EXTENSIONS, VIDEO_EXTENSIONS }
