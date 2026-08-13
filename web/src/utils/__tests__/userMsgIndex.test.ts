import { describe, expect, it } from 'vitest'
import { extractPlainText, formatUserMsg } from '@/utils/userMsgIndexUtils.ts'

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

describe('formatUserMsg', () => {
  const attachmentLabel = 'Attachment'

  it('returns the full text for long content', () => {
    expect(formatUserMsg({ content: 'a'.repeat(200) }, attachmentLabel)).toBe('a'.repeat(200))
  })

  it('keeps short text as-is', () => {
    expect(formatUserMsg({ content: 'Short message' }, attachmentLabel)).toBe('Short message')
  })

  it('handles block-format JSON content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(formatUserMsg({ content }, attachmentLabel)).toBe('Hello from blocks')
  })

  it('shows attachment label for empty content with files', () => {
    expect(formatUserMsg({ content: '', files: ['file.go'] }, attachmentLabel)).toBe('[Attachment]')
  })

  it('shows attachment label for no content with files', () => {
    expect(formatUserMsg({ files: ['file.go'] }, attachmentLabel)).toBe('[Attachment]')
  })

  it('prefers text over attachment label', () => {
    expect(formatUserMsg({ content: 'Has text', files: ['file.go'] }, attachmentLabel)).toBe('Has text')
  })

  it('shows empty string for empty content without files', () => {
    expect(formatUserMsg({ content: '' }, attachmentLabel)).toBe('')
  })
})
