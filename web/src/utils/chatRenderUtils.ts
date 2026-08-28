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
 * Image extensions that the /api/file/thumb endpoint can rasterize to a JPEG
 * thumbnail (standard-library decoders). SVG/WebP/AVIF/TIFF are excluded and
 * keep serving the original file. GIF is excluded to preserve animation.
 */
export const THUMB_EXTENSIONS = ['.png', '.jpg', '.jpeg']

/** Desktop (PC) inline thumbnail width passed to /api/file/thumb (clamped 50–1600 by backend). */
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
 * Rewrite a project-relative media path to a /api/local-file/ URL.
 * Returns the rewritten URL, or the original src unchanged when:
 *  - it is an absolute/external URL (http(s)://, protocol-relative //, or /api/local-file/),
 *  - it is an absolute path outside the project root,
 *  - no projectRoot is provided.
 * Shared by image/audio/video media rewriting so all media types resolve the same way.
 */
function resolveLocalMediaSrc(src: string, projectRoot?: string): string {
  if (!projectRoot) return src
  if (/^(https?:|\/\/|\/api\/local-file\/)/i.test(src)) return src
  const absolutePath = src.startsWith('/') ? src : `${projectRoot}/${src}`
  if (!(absolutePath.startsWith(projectRoot + '/') || absolutePath === projectRoot)) {
    return src
  }
  const rel = absolutePath.slice(projectRoot.length + 1)
  // Encode each path segment to handle CJK/special characters
  let decoded = rel
  try { decoded = decodeURIComponent(rel) } catch { /* malformed encoding, use as-is */ }
  return `/api/local-file/${decoded.split('/').map((s: string) => encodeURIComponent(s)).join('/')}`
}

/**
 * Rewrite image URLs in HTML: convert local project file paths to /api/local-file/ URLs.
 * For raster formats the thumb endpoint can decode, the inline src is rewritten to a
 * lightweight JPEG thumbnail (/api/file/thumb?path=...) and the original full-size
 * URL is stored in data-full-src (used by the lightbox to show the full image).
 * Skips absolute/external URLs. Applies thumbnail styling.
 */
export function rewriteImageUrls(html: string, projectRoot: string, thumbWidth: number = THUMB_DEFAULT_WIDTH): string {
  return html.replace(/<img([^>]*)>/g, (_match, attrs) => {
    let cleanAttrs = attrs.replace(/\s*style="[^"]*"/i, '').replace(/\s*class="[^"]*"/i, '')
    const srcMatch = cleanAttrs.match(/\bsrc="([^"]*)"/)
    if (srcMatch) {
      const src = srcMatch[1]
      // Skip absolute/external URLs
      if (/^(https?:|\/\/|^\/)/i.test(src)) {
        return `<span class="lightbox-img-wrap"><img${cleanAttrs} class="chat-img lightbox-img"><span class="lightbox-expand-icon"></span></span>`
      }
      // Try to resolve as a project-local path
      if (projectRoot) {
        const rewritten = resolveLocalMediaSrc(src, projectRoot)
        if (rewritten !== src) {
          cleanAttrs = cleanAttrs.replace(`src="${src}"`, `src="${rewritten}"`)
          const rel = rewritten.replace(/^\/api\/local-file\//, '')
          if (isThumbExtension(src)) {
            // Inline src → thumbnail; keep the original for the lightbox.
            // rel is already segment-encoded (CJK → %XX); the backend decodes
            // the query param once, so pass it through unencoded.
            const fullSrc = escapeHtmlAttr(rewritten)
            const thumbSrc = buildThumbUrl(rel, thumbWidth)
            cleanAttrs = cleanAttrs.replace(/src="[^"]*"/, `src="${thumbSrc}" data-full-src="${fullSrc}"`)
          }
        }
      }
    }
    return `<span class="lightbox-img-wrap"><img${cleanAttrs} class="chat-img lightbox-img"><span class="lightbox-expand-icon"></span></span>`
  })
}

/** True if the file path has an extension the thumb endpoint can rasterize. */
export function isThumbExtension(path: string): boolean {
  const lower = path.toLowerCase()
  return THUMB_EXTENSIONS.some(ext => lower.endsWith(ext))
}

/**
 * Wrap bare inline <svg> elements (returned directly by the AI, not rendered
 * from markdown image syntax) in a lightbox wrapper so they get the same
 * "view" affordance as raster images: a top-right expand icon on hover.
 *
 * Runs on the rendered HTML string BEFORE mermaid diagrams are produced —
 * at this stage mermaid is still a <pre> code block (no svg), and mermaid.ts
 * later adds its own expand icon at the DOM level. The wrapper marks the svg
 * with a .lightbox-svg class so repeated application is idempotent.
 *
 * Callers MUST invoke this AFTER all <a href>-anchored regex steps (audio/video
 * link conversion, path/commit/localhost annotations): the wrapper <span>
 * breaks those regexes' structural matches across the svg content.
 *
 * SVGs already inside an interactive UI element injected by the pipeline
 * (e.g. the lucide icon inside a .chat-file-open-btn button) are skipped —
 * they are not content images and must not get a lightbox affordance.
 */
export function wrapInlineSvgs(html: string): string {
  const result: string[] = []
  const tagRe = /<\/?([a-zA-Z][a-zA-Z0-9-]*)\b[^>]*>/g
  let lastIndex = 0
  let match: RegExpExecArray | null
  const tagStack: string[] = []

  while ((match = tagRe.exec(html))) {
    const tag = match[0]
    const name = match[1].toLowerCase()

    if (name === 'svg') {
      if (tag.startsWith('</')) {
        // Closing svg — the matching open (if any) was pushed as 'svg'.
        if (tagStack[tagStack.length - 1] === 'svg') tagStack.pop()
        continue
      }
      // Opening svg
      const openIndex = match.index
      const openTag = tag

      // Skip content inside interactive UI elements injected by the pipeline.
      const inInteractive = tagStack.some(t => t === 'button' || t === 'a')

      // Idempotency: skip SVGs we already wrapped — the wrapper adds the
      // .lightbox-svg marker class to the svg's own opening tag.
      const alreadyWrapped = /\bclass\s*=\s*("|')[^"']*\blightbox-svg\b[^"']*\1/i.test(openTag)

      if (alreadyWrapped || inInteractive) {
        // Treat this svg as a balanced unit: skip its full span without wrapping.
        const depth = countSvgDepth(html, openIndex + openTag.length)
        if (depth >= 0) {
          result.push(html.slice(lastIndex, openIndex))
          result.push(html.slice(openIndex, depth))
          lastIndex = depth
          tagRe.lastIndex = depth
          continue
        }
        // Unbalanced svg — leave as-is
        tagStack.push('svg')
        continue
      }

      // Balance-count to the matching close tag (handles nested <svg>).
      const closeEnd = countSvgDepth(html, openIndex + openTag.length)
      if (closeEnd >= 0) {
        const innerHtml = html.slice(openIndex + openTag.length, closeEnd - '</svg>'.length)
        // Add the lightbox-svg marker class, preserving any existing class.
        // Handles both single- and double-quoted class attributes.
        const tagged = /(\bclass\s*=\s*("|')[^"']*)\2/i.test(openTag)
          ? openTag.replace(/(\bclass\s*=\s*("|')[^"']*)\2/i, '$1 lightbox-svg$2')
          : openTag.replace(/\/?>$/, ' class="lightbox-svg">')

        result.push(html.slice(lastIndex, openIndex))
        result.push(`<span class="lightbox-svg-wrap">${tagged}${innerHtml}</svg><span class="lightbox-expand-icon"></span></span>`)
        lastIndex = closeEnd
        tagRe.lastIndex = closeEnd
        continue
      }

      // Unbalanced — treat as normal element for stack tracking
      tagStack.push('svg')
      continue
    }

    // Track non-svg tags for the interactive-container heuristic.
    if (tag.startsWith('</')) {
      if (tagStack[tagStack.length - 1] === name) tagStack.pop()
    } else if (!/\/>$/.test(tag)) {
      tagStack.push(name)
    }
  }

  result.push(html.slice(lastIndex))
  return result.join('')
}

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
  return `/api/file/thumb?path=${relPath}&w=${width}`
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
 * Convert audio file links to inline audio players.
 * Replaces <a href="...mp3"> links with <audio> elements.
 * Project-relative paths (not /api/local-file/ or external URLs) are rewritten
 * to /api/local-file/ URLs so the browser can load them, mirroring image handling.
 */
export function convertAudioLinks(html: string, projectRoot?: string): string {
  return html.replace(/<a href="([^"]+)">([^<]*)<\/a>/g, (match, href) => {
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
 * project-relative paths to /api/local-file/ URLs like audio/images.
 */
export function convertVideoLinks(html: string, projectRoot?: string): string {
  return html.replace(/<a href="([^"]+)">([^<]*)<\/a>/g, (match, href) => {
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
 * Parse ask-question content from XML or JSON format.
 * Tries XML first, falls back to JSON if XML fails.
 * Returns null if parsing fails or no valid questions found.
 */
export function parseAskQuestionContent(rawContent: string): { questions: Array<Record<string, unknown>> } | null {
  return parseAskQuestionXML(rawContent) as { questions: Array<Record<string, unknown>> } | null
}

/** Export audio/video extensions for testing */
export { AUDIO_EXTENSIONS, VIDEO_EXTENSIONS }
