import { describe, expect, it } from 'vitest'

// Inline extractPlainText logic (same as in ChatMessageList.vue)
function extractPlainText(content: string): string {
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
    } catch { /* ignore parse error */ }
  }
  return content
}

function truncateUserMsg(msg: { content?: string; files?: string[] }, t: (key: string) => string, maxLen = 40): string {
  const text = extractPlainText(msg.content || '')
  if (!text && msg.files && msg.files.length > 0) {
    return `[${t('chat.messageList.userMsgIndexAttachment')}]`
  }
  return text.length > maxLen ? text.slice(0, maxLen) + '…' : text
}

const mockT = (key: string) => key === 'chat.messageList.userMsgIndexAttachment' ? 'Attachment' : key

describe('extractPlainText', () => {
  it('returns empty string for empty content', () => {
    expect(extractPlainText('')).toBe('')
  })

  it('returns raw text for plain text content', () => {
    expect(extractPlainText('Hello world')).toBe('Hello world')
  })

  it('extracts text from single-block JSON', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(extractPlainText(content)).toBe('Hello from blocks')
  })

  it('concatenates multiple text blocks with space', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'Part one' },
        { type: 'text', text: 'Part two' },
      ],
    })
    expect(extractPlainText(content)).toBe('Part one Part two')
  })

  it('ignores non-text blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'thinking', text: 'Inner thought' },
        { type: 'text', text: 'Visible text' },
        { type: 'tool_use', name: 'bash' },
      ],
    })
    expect(extractPlainText(content)).toBe('Visible text')
  })

  it('returns raw content for malformed JSON', () => {
    expect(extractPlainText('{"blocks": invalid')).toBe('{"blocks": invalid')
  })

  it('returns raw content for JSON without blocks', () => {
    expect(extractPlainText('{"foo": "bar"}')).toBe('{"foo": "bar"}')
  })

  it('returns empty string for blocks array with no text blocks', () => {
    const content = JSON.stringify({ blocks: [{ type: 'tool_use', name: 'bash' }] })
    expect(extractPlainText(content)).toBe('')
  })
})

describe('truncateUserMsg', () => {
  it('truncates long text', () => {
    expect(truncateUserMsg({ content: 'a'.repeat(50) }, mockT)).toBe('a'.repeat(40) + '…')
  })

  it('keeps short text as-is', () => {
    expect(truncateUserMsg({ content: 'Short message' }, mockT)).toBe('Short message')
  })

  it('handles block-format JSON content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(truncateUserMsg({ content }, mockT)).toBe('Hello from blocks')
  })

  it('shows attachment label for empty content with files', () => {
    expect(truncateUserMsg({ content: '', files: ['file.go'] }, mockT)).toBe('[Attachment]')
  })

  it('shows attachment label for no content with files', () => {
    expect(truncateUserMsg({ files: ['file.go'] }, mockT)).toBe('[Attachment]')
  })

  it('prefers text over attachment label', () => {
    expect(truncateUserMsg({ content: 'Has text', files: ['file.go'] }, mockT)).toBe('Has text')
  })

  it('shows empty string for empty content without files', () => {
    expect(truncateUserMsg({ content: '' }, mockT)).toBe('')
  })

  it('respects custom maxLen', () => {
    expect(truncateUserMsg({ content: 'Hello world' }, mockT, 5)).toBe('Hello…')
  })
})
