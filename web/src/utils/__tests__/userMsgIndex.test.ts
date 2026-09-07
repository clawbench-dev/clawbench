import { describe, expect, it } from 'vitest'
import { extractPlainText, formatUserMsg, matchUserMsg } from '@/utils/userMsgIndexUtils.ts'

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

  it('unwraps nested ACP notification JSON in a text block', () => {
    const content = JSON.stringify({
      blocks: [
        {
          text: JSON.stringify({
            content: { text: 'hi', type: 'text' },
            messageId: '85d9b9a9-00a4-4ea1-8abe-0d0ef6bc2426',
            sessionUpdate: 'user_message_chunk',
          }),
          type: 'text',
        },
      ],
    })
    expect(extractPlainText(content)).toBe('hi')
  })

  it('unwraps nested ACP notification JSON with Chinese text', () => {
    const content = JSON.stringify({
      blocks: [
        {
          text: JSON.stringify({
            content: { text: '你好', type: 'text' },
            messageId: 'm1',
            sessionUpdate: 'user_message_chunk',
          }),
          type: 'text',
        },
      ],
    })
    expect(extractPlainText(content)).toBe('你好')
  })

  it('extracts text from a bare content-array JSON', () => {
    const content = JSON.stringify([
      { type: 'text', text: 'hello from array' },
      { type: 'image', image: {} },
    ])
    expect(extractPlainText(content)).toBe('hello from array')
  })

  it('skips thinking elements in bare content-array JSON', () => {
    const content = JSON.stringify([
      { type: 'thinking', text: 'inner reasoning' },
      { type: 'text', text: 'final answer' },
    ])
    expect(extractPlainText(content)).toBe('final answer')
  })

  it('degrades gracefully on pathologically deep nesting', () => {
    let nested = '"leaf"'
    for (let i = 0; i < 12; i++) {
      nested = `{"text":${nested}}`
    }
    expect(extractPlainText(nested)).toBe('')
  })

  it('extracts text from an ACP notification wrapper directly', () => {
    const content = JSON.stringify({
      content: { text: '直接存的通知', type: 'text' },
      messageId: 'abc',
      sessionUpdate: 'user_message_chunk',
    })
    expect(extractPlainText(content)).toBe('直接存的通知')
  })

  it('handles blocks JSON with leading whitespace', () => {
    const content = '{\n  "blocks": [{"type": "text", "text": "换行格式"}]\n}'
    expect(extractPlainText(content)).toBe('换行格式')
  })

  it('returns empty string for blocks with empty text', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: '' }] })
    expect(extractPlainText(content)).toBe('')
  })

  it('returns raw content for unknown JSON object', () => {
    const content = JSON.stringify({ foo: 'bar' })
    expect(extractPlainText(content)).toBe(content)
  })

  it('returns raw content for non-JSON text starting with bracket', () => {
    expect(extractPlainText('[PWA] Service Worker skipped')).toBe('[PWA] Service Worker skipped')
  })

  it('returns empty string for blocks with only thinking', () => {
    const content = JSON.stringify({ blocks: [{ type: 'thinking', text: 'inner thought' }] })
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

describe('matchUserMsg', () => {
  it('matches everything for an empty or whitespace query', () => {
    expect(matchUserMsg({ content: 'anything' }, '')).toBe(true)
    expect(matchUserMsg({ content: 'anything' }, '   ')).toBe(true)
  })

  it('matches content case-insensitively', () => {
    expect(matchUserMsg({ content: 'Fix BUG in Parser' }, 'bug')).toBe(true)
    expect(matchUserMsg({ content: 'Fix BUG in Parser' }, 'fix')).toBe(true)
    expect(matchUserMsg({ content: 'Fix BUG in Parser' }, 'parser')).toBe(true)
  })

  it('matches against text inside JSON block content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(matchUserMsg({ content }, 'blocks')).toBe(true)
  })

  it('matches attachment-only message by object file path basename', () => {
    const msg = { content: '', files: [{ path: 'src/foo/bar.ts', isDir: false }] }
    expect(matchUserMsg(msg, 'bar.ts')).toBe(true)
  })

  it('matches attachment full path (not just basename)', () => {
    const msg = { content: '', files: [{ path: 'src/foo/bar.ts', isDir: false }] }
    expect(matchUserMsg(msg, 'src/foo')).toBe(true)
  })

  it('matches legacy string attachment entries', () => {
    expect(matchUserMsg({ content: '', files: ['notes.txt'] }, 'notes')).toBe(true)
  })

  it('matches basename of a nested path', () => {
    expect(matchUserMsg({ files: [{ path: '/a/b/main.go' }] }, 'main')).toBe(true)
  })

  it('matches file path case-insensitively', () => {
    expect(matchUserMsg({ files: [{ path: '/SRC/Main.go' }] }, 'main.go')).toBe(true)
  })

  it('handles mixed string and object attachment shapes', () => {
    const msg = { files: ['a.txt', { path: 'b/c.go' }] }
    expect(matchUserMsg(msg, 'c.go')).toBe(true)
  })

  it('matches a message with both text and attachments via its content', () => {
    expect(matchUserMsg({ content: 'Has text', files: [{ path: 'x.ts' }] }, 'text')).toBe(true)
  })

  it('matches the attachment label for attachment-only messages when provided', () => {
    const msg = { content: '', files: [{ path: 'src/foo/bar.ts', isDir: false }] }
    expect(matchUserMsg(msg, '附件', '附件')).toBe(true)
    expect(matchUserMsg(msg, 'attachment', 'Attachment')).toBe(true)
    // Label not matched case-insensitively when no label is passed.
    expect(matchUserMsg(msg, '附件')).toBe(false)
  })

  it('does not match the attachment label when the message has no attachments', () => {
    expect(matchUserMsg({ content: 'plain text' }, '附件', '附件')).toBe(false)
  })

  it('returns false when nothing matches', () => {
    expect(matchUserMsg({ content: 'plain', files: [{ path: 'a/b.ts' }] }, 'zzz')).toBe(false)
    expect(matchUserMsg({ content: '', files: [] }, 'zzz')).toBe(false)
  })

  it('returns false for missing/empty message with a non-empty query', () => {
    expect(matchUserMsg(undefined as never, 'x')).toBe(false)
    expect(matchUserMsg(null as never, 'x')).toBe(false)
    expect(matchUserMsg({}, 'x')).toBe(false)
  })

  it('handles Windows backslash paths', () => {
    expect(matchUserMsg({ files: [{ path: 'C:\\src\\Main.go' }] }, 'src/Main')).toBe(true)
  })
})
