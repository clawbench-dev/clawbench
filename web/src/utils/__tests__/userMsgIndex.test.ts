import { describe, expect, it } from 'vitest'
import { extractPlainText, truncateUserMsg } from '@/utils/userMsgIndexUtils.ts'

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
  const attachmentLabel = 'Attachment'

  it('truncates long text', () => {
    expect(truncateUserMsg({ content: 'a'.repeat(50) }, attachmentLabel)).toBe('a'.repeat(40) + '…')
  })

  it('keeps short text as-is', () => {
    expect(truncateUserMsg({ content: 'Short message' }, attachmentLabel)).toBe('Short message')
  })

  it('handles block-format JSON content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(truncateUserMsg({ content }, attachmentLabel)).toBe('Hello from blocks')
  })

  it('shows attachment label for empty content with files', () => {
    expect(truncateUserMsg({ content: '', files: ['file.go'] }, attachmentLabel)).toBe('[Attachment]')
  })

  it('shows attachment label for no content with files', () => {
    expect(truncateUserMsg({ files: ['file.go'] }, attachmentLabel)).toBe('[Attachment]')
  })

  it('prefers text over attachment label', () => {
    expect(truncateUserMsg({ content: 'Has text', files: ['file.go'] }, attachmentLabel)).toBe('Has text')
  })

  it('shows empty string for empty content without files', () => {
    expect(truncateUserMsg({ content: '' }, attachmentLabel)).toBe('')
  })
})
