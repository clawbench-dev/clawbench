/**
 * Pure utility functions for double-click copy behavior.
 * Extracted from useDoubleClickCopy composable for testability.
 */

/**
 * Check if an href is an external link (http/https, mailto, tel, protocol-relative).
 */
export function isExternalLink(href: string): boolean {
  return /^(https?:|\/\/|mailto:|tel:)/i.test(href)
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

/**
 * Build the quote message text for a code selection.
 * Formats as: <userMessage>\n\n```<language>:<filePath>:<lineRange>\n<text>\n```
 */
export function buildQuoteMessage(
  userMessage: string,
  text: string,
  filePath: string,
  language: string,
  startLine: number,
  endLine: number,
): string {
  const langPrefix = language ? `${language}:` : ':'
  let lineSuffix = ''
  if (startLine && endLine && startLine !== endLine) {
    lineSuffix = `:${startLine}-${endLine}`
  } else if (startLine) {
    lineSuffix = `:${startLine}`
  }
  return `${userMessage.trim()}\n\n\`\`\`${langPrefix}${filePath}${lineSuffix}\n${text}\n\`\`\``
}
