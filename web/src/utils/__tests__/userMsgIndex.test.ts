import { describe, expect, it } from 'vitest'
import {
  extractPlainText,
  formatIndexMsg,
  matchIndexMsg,
  assistantIndexText,
  truncateIndexText,
  INDEX_TEXT_MAX_LENGTH,
  type IndexRowLabels,
} from '@/utils/userMsgIndexUtils.ts'

const labels: IndexRowLabels = { attachment: 'Attachment', noText: '(no text)' }

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

describe('truncateIndexText', () => {
  it('leaves text at or below the cap untouched', () => {
    expect(truncateIndexText('short', 10)).toBe('short')
    expect(truncateIndexText('a'.repeat(10), 10)).toBe('a'.repeat(10))
  })

  it('cuts text past the cap and appends an ellipsis', () => {
    const result = truncateIndexText('a'.repeat(11), 10)
    expect(result).toBe('a'.repeat(10) + '…')
    expect([...result]).toHaveLength(11)
  })

  it('counts code points so surrogate pairs are never split', () => {
    // Each emoji is one code point but two UTF-16 units.
    const emoji = '😀'.repeat(5)
    expect(truncateIndexText(emoji, 3)).toBe('😀😀😀…')
    expect(truncateIndexText(emoji, 5)).toBe(emoji)
  })

  it('handles empty input and a non-positive cap', () => {
    expect(truncateIndexText('', 10)).toBe('')
    expect(truncateIndexText('anything', 0)).toBe('')
  })
})

describe('formatIndexMsg — user rows (legacy formatUserMsg coverage)', () => {
  it('honors an explicit maxLen override', () => {
    expect(formatIndexMsg({ role: 'user', content: 'abcdef' }, labels, 3)).toBe('abc…')
  })

  it('keeps short text as-is', () => {
    expect(formatIndexMsg({ role: 'user', content: 'Short message' }, labels)).toBe('Short message')
  })

  it('handles block-format JSON content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(formatIndexMsg({ role: 'user', content }, labels)).toBe('Hello from blocks')
  })

  it('shows attachment label for no content with files', () => {
    expect(formatIndexMsg({ role: 'user', files: ['file.go'] }, labels)).toBe('[Attachment]')
  })

  it('prefers text over attachment label', () => {
    expect(formatIndexMsg({ role: 'user', content: 'Has text', files: ['file.go'] }, labels)).toBe('Has text')
  })

  it('shows empty string for empty content without files', () => {
    expect(formatIndexMsg({ role: 'user', content: '' }, labels)).toBe('')
  })
})

describe('matchIndexMsg — legacy matchUserMsg coverage', () => {
  const userLabels: IndexRowLabels = { attachment: '附件', noText: '(无文字)' }

  it('matches content case-insensitively', () => {
    expect(matchIndexMsg({ role: 'user', content: 'Fix BUG in Parser' }, 'bug')).toBe(true)
    expect(matchIndexMsg({ role: 'user', content: 'Fix BUG in Parser' }, 'fix')).toBe(true)
    expect(matchIndexMsg({ role: 'user', content: 'Fix BUG in Parser' }, 'parser')).toBe(true)
  })

  it('matches against text inside JSON block content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'Hello from blocks' }] })
    expect(matchIndexMsg({ role: 'user', content }, 'blocks')).toBe(true)
  })

  it('matches attachment-only message by object file path basename', () => {
    const msg = { role: 'user', content: '', files: [{ path: 'src/foo/bar.ts', isDir: false }] }
    expect(matchIndexMsg(msg, 'bar.ts')).toBe(true)
  })

  it('matches legacy string attachment entries', () => {
    expect(matchIndexMsg({ role: 'user', content: '', files: ['notes.txt'] }, 'notes')).toBe(true)
  })

  it('matches basename of a nested path', () => {
    expect(matchIndexMsg({ role: 'user', files: [{ path: '/a/b/main.go' }] }, 'main')).toBe(true)
  })

  it('matches file path case-insensitively', () => {
    expect(matchIndexMsg({ role: 'user', files: [{ path: '/SRC/Main.go' }] }, 'main.go')).toBe(true)
  })

  it('handles mixed string and object attachment shapes', () => {
    const msg = { role: 'user', files: ['a.txt', { path: 'b/c.go' }] }
    expect(matchIndexMsg(msg, 'c.go')).toBe(true)
  })

  it('matches a message with both text and attachments via its content', () => {
    expect(matchIndexMsg({ role: 'user', content: 'Has text', files: [{ path: 'x.ts' }] }, 'text')).toBe(true)
  })

  it('matches the attachment label for attachment-only messages when provided', () => {
    const msg = { role: 'user', content: '', files: [{ path: 'src/foo/bar.ts', isDir: false }] }
    expect(matchIndexMsg(msg, '附件', userLabels)).toBe(true)
    expect(matchIndexMsg(msg, 'attachment', { attachment: 'Attachment', noText: '(no text)' })).toBe(true)
    // Label not matched when no labels are passed.
    expect(matchIndexMsg(msg, '附件')).toBe(false)
  })

  it('does not match the attachment label when the message has no attachments', () => {
    expect(matchIndexMsg({ role: 'user', content: 'plain text' }, '附件', userLabels)).toBe(false)
  })

  it('returns false when nothing matches', () => {
    expect(matchIndexMsg({ role: 'user', content: 'plain', files: [{ path: 'a/b.ts' }] }, 'zzz')).toBe(false)
    expect(matchIndexMsg({ role: 'user', content: '', files: [] }, 'zzz')).toBe(false)
  })

  it('returns false for missing/empty message with a non-empty query', () => {
    expect(matchIndexMsg(undefined as never, 'x')).toBe(false)
    expect(matchIndexMsg(null as never, 'x')).toBe(false)
    expect(matchIndexMsg({}, 'x')).toBe(false)
  })

  it('handles Windows backslash paths', () => {
    expect(matchIndexMsg({ role: 'user', files: [{ path: 'C:\\src\\Main.go' }] }, 'src/Main')).toBe(true)
  })
})

describe('assistantIndexText', () => {
  it('prefers the stored summary over the reply content', () => {
    expect(assistantIndexText({ summary: 'the summary', content: 'the full reply' })).toBe('the summary')
  })

  it('falls back to the reply content when no summary exists', () => {
    expect(assistantIndexText({ content: 'the full reply' })).toBe('the full reply')
  })

  it('extracts text from block-format JSON content', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: 'block answer' }] })
    expect(assistantIndexText({ content })).toBe('block answer')
  })

  it('returns empty when neither summary nor content carries text', () => {
    expect(assistantIndexText({})).toBe('')
    expect(assistantIndexText({ content: '' })).toBe('')
    // A tool-only turn has no answer text.
    expect(assistantIndexText({ content: JSON.stringify({ blocks: [{ type: 'tool_use', name: 'Bash' }] }) })).toBe('')
  })

  it('falls back to parsed blocks for in-memory messages', () => {
    // The offline fallback path passes already-parsed messages, whose text lives
    // in `blocks` rather than raw JSON `content`.
    expect(assistantIndexText({ blocks: [{ type: 'text', text: 'block reply' }] })).toBe('block reply')
  })

  it('ignores non-text blocks when falling back to blocks', () => {
    const blocks = [
      { type: 'thinking', text: 'inner' },
      { type: 'tool_use', text: 'Bash' },
      { type: 'text', text: 'the answer' },
    ]
    expect(assistantIndexText({ blocks })).toBe('the answer')
  })

  it('prefers summary over blocks', () => {
    expect(assistantIndexText({ summary: 'sum', blocks: [{ type: 'text', text: 'blocks' }] })).toBe('sum')
  })
})

describe('formatIndexMsg', () => {
  it('formats a user row from its content', () => {
    expect(formatIndexMsg({ role: 'user', content: 'Fix the bug' }, labels)).toBe('Fix the bug')
  })

  it('truncates a user row at the cap', () => {
    const result = formatIndexMsg({ role: 'user', content: 'a'.repeat(200) }, labels)
    expect(result).toBe('a'.repeat(INDEX_TEXT_MAX_LENGTH) + '…')
  })

  it('shows the attachment label for an attachment-only user row', () => {
    expect(formatIndexMsg({ role: 'user', content: '', files: ['f.go'] }, labels)).toBe('[Attachment]')
  })

  it('formats an assistant row from its summary', () => {
    expect(formatIndexMsg({ role: 'assistant', summary: 'Done, tests pass' }, labels)).toBe('Done, tests pass')
  })

  it('falls back to the reply content for an assistant row with no summary', () => {
    expect(formatIndexMsg({ role: 'assistant', content: 'fallback text' }, labels)).toBe('fallback text')
  })

  it('truncates an assistant row at the cap', () => {
    const result = formatIndexMsg({ role: 'assistant', summary: 'b'.repeat(200) }, labels)
    expect(result).toBe('b'.repeat(INDEX_TEXT_MAX_LENGTH) + '…')
  })

  it('shows the no-text placeholder for a textless assistant row', () => {
    expect(formatIndexMsg({ role: 'assistant', content: '' }, labels)).toBe('(no text)')
  })

  it('treats a row with no role as a user row', () => {
    expect(formatIndexMsg({ content: 'legacy row' }, labels)).toBe('legacy row')
  })
})

describe('matchIndexMsg', () => {
  it('matches everything for an empty query', () => {
    expect(matchIndexMsg({ role: 'assistant', summary: 'x' }, '')).toBe(true)
    expect(matchIndexMsg({ role: 'user', content: 'x' }, '   ')).toBe(true)
  })

  it('matches a user row on its content', () => {
    expect(matchIndexMsg({ role: 'user', content: 'Fix the Parser' }, 'parser')).toBe(true)
    expect(matchIndexMsg({ role: 'user', content: 'Fix the Parser' }, 'zzz')).toBe(false)
  })

  it('matches an assistant row on its summary', () => {
    expect(matchIndexMsg({ role: 'assistant', summary: 'Refactored the parser' }, 'refactor')).toBe(true)
  })

  it('matches an assistant row on its fallback content when there is no summary', () => {
    expect(matchIndexMsg({ role: 'assistant', content: 'fallback needle' }, 'needle')).toBe(true)
  })

  it('does not match an assistant row on content when a summary is present', () => {
    // The row displays the summary; content is not visible, so it must not match.
    expect(matchIndexMsg({ role: 'assistant', summary: 'visible', content: 'hidden needle' }, 'needle')).toBe(false)
  })

  it('matches the no-text placeholder when provided', () => {
    const msg = { role: 'assistant', content: '' }
    expect(matchIndexMsg(msg, 'no text', labels)).toBe(true)
    expect(matchIndexMsg(msg, 'no text')).toBe(false)
  })

  it('matches attachment path/basename on user rows', () => {
    expect(matchIndexMsg({ role: 'user', content: '', files: [{ path: 'src/foo/bar.ts' }] }, 'bar.ts')).toBe(true)
    expect(matchIndexMsg({ role: 'user', content: '', files: [{ path: 'src/foo/bar.ts' }] }, 'src/foo')).toBe(true)
  })

  it('matches the attachment label only when provided', () => {
    const msg = { role: 'user', content: '', files: [{ path: 'a.ts' }] }
    expect(matchIndexMsg(msg, 'Attachment', labels)).toBe(true)
    expect(matchIndexMsg(msg, 'Attachment')).toBe(false)
  })

  it('returns false for a missing message with a non-empty query', () => {
    expect(matchIndexMsg(null as never, 'x')).toBe(false)
  })
})
