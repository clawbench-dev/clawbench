/**
 * Pure functions extracted from useChatRender composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */

import { parseAskQuestionXML, parseRagResultsXML } from '@/utils/xmlParser.ts'

/** Audio file extensions that should be converted to inline audio players */
const AUDIO_EXTENSIONS = ['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.wma', '.opus']

/**
 * Rewrite image URLs in HTML: convert local project file paths to /api/local-file/ URLs.
 * Skips absolute/external URLs. Applies thumbnail styling.
 */
export function rewriteImageUrls(html: string, projectRoot: string): string {
  return html.replace(/<img([^>]*)>/g, (_match, attrs) => {
    let cleanAttrs = attrs.replace(/\s*style="[^"]*"/i, '').replace(/\s*class="[^"]*"/i, '')
    const srcMatch = cleanAttrs.match(/\bsrc="([^"]*)"/)
    if (srcMatch) {
      const src = srcMatch[1]
      // Skip absolute/external URLs
      if (/^(https?:|\/\/|^\/)/i.test(src)) {
        return `<img${cleanAttrs} style="max-width: 200px; max-height: 200px; object-fit: cover; border-radius: 6px; margin: 4px 0; cursor: pointer;" class="lightbox-img">`
      }
      // Try to resolve as a project-local path
      if (projectRoot) {
        const absolutePath = src.startsWith('/')
          ? src
          : `${projectRoot}/${src}`
        if (absolutePath.startsWith(projectRoot + '/') || absolutePath === projectRoot) {
          const rel = absolutePath.slice(projectRoot.length + 1)
          // Encode each path segment to handle CJK/special characters
          let decoded = rel
          try { decoded = decodeURIComponent(rel) } catch { /* malformed encoding, use as-is */ }
          const encoded = decoded.split('/').map((s: string) => encodeURIComponent(s)).join('/')
          cleanAttrs = cleanAttrs.replace(`src="${src}"`, `src="/api/local-file/${encoded}?t=${Date.now()}"`)
        }
      }
    }
    return `<img${cleanAttrs} style="max-width: 200px; max-height: 200px; object-fit: cover; border-radius: 6px; margin: 4px 0; cursor: pointer;" class="lightbox-img">`
  })
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
 */
export function convertAudioLinks(html: string): string {
  return html.replace(/<a href="([^"]+)">([^<]*)<\/a>/g, (match, href) => {
    const lower = href.toLowerCase()
    if (AUDIO_EXTENSIONS.some(ext => lower.endsWith(ext))) {
      const safeHref = escapeHtmlAttr(href)
      return `<div class="chat-audio-wrapper"><audio src="${safeHref}" controls class="chat-audio-player"></audio></div>`
    }
    return match
  })
}

/**
 * Parse ask-question content from XML or JSON format.
 * Tries XML first, falls back to JSON if XML fails.
 * Returns null if parsing fails or no valid questions found.
 */
export function parseAskQuestionContent(rawContent: string): { questions: any[] } | null {
  return parseAskQuestionXML(rawContent)
}

/**
 * Parse rag-results content from XML format.
 * Returns null if parsing fails or no rag-items found.
 */
export function parseRagResultsContent(rawContent: string): { sessionId: string; sessionTitle: string; createdAt: string; summary: string }[] | null {
  const items = parseRagResultsXML(rawContent)
  return items.length > 0 ? items : null
}

/** Export audio extensions for testing */
export { AUDIO_EXTENSIONS }
