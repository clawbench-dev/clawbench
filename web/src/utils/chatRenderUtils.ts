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
