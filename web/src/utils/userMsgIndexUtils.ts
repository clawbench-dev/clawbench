/**
 * Extracts plain text from user message content.
 * Handles both plain text and block-format JSON ({"blocks":[...]}).
 */
export function extractPlainText(content: string): string {
  if (!content) return ''
  if (content.startsWith('{"blocks":')) {
    try {
      const parsed = JSON.parse(content)
      if (parsed.blocks && Array.isArray(parsed.blocks)) {
        return parsed.blocks
          .filter((b: { type: string; text?: string }) => b.type === 'text' && b.text)
          .map((b: { type: string; text?: string }) => b.text)
          .join(' ')
      }
    } catch { /* ignore parse error, fall through */ }
  }
  return content
}

const USER_MSG_TRUNCATE_LEN = 40

/**
 * Truncates a user message for display in the index list.
 * Returns [Attachment] label for attachment-only messages.
 */
export function truncateUserMsg(msg: { content?: string; files?: string[] }, attachmentLabel: string): string {
  const text = extractPlainText(msg.content || '')
  if (!text && msg.files && msg.files.length > 0) {
    return `[${attachmentLabel}]`
  }
  return text.length > USER_MSG_TRUNCATE_LEN ? text.slice(0, USER_MSG_TRUNCATE_LEN) + '…' : text
}
