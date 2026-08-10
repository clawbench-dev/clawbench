/**
 * Pure utility functions for double-click copy behavior.
 * Extracted from useDoubleClickCopy composable for testability.
 */

/**
 * Check if an href is an external link that the browser should follow
 * (http/https/ftp/mailto/tel/callto/sms/cid/xmpp, or protocol-relative).
 * file: is intentionally NOT external — it is handled as an in-app file link.
 */
export function isExternalLink(href: string): boolean {
  return /^(?:(?:f|ht)tps?:|mailto:|tel:|callto:|sms:|cid:|xmpp:|\/\/)/i.test(href)
}

/**
 * Check if an href is an anchor link (starts with #).
 */
export function isAnchorLink(href: string): boolean {
  return href.startsWith('#')
}

/**
 * Slugify a string for heading ID matching.
 * Converts to lowercase, replaces non-word/non-CJK chars with dashes,
 * and strips leading/trailing dashes.
 */
export function slugifyForHeading(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\w\u4e00-\u9fa5]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

/**
 * Strip leading numbering from text.
 * E.g. "5. 第四部分" → "第四部分"
 * E.g. "3: Something" → "Something"
 */
export function stripLeadingNumbering(text: string): string {
  return text.replace(/^[\d\s.、:：]+/, '').trim()
}
