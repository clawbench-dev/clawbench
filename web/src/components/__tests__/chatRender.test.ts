import { describe, expect, it } from 'vitest'
import {
  parseAssistantContent,
  hasImagesInContent,
  formatDetailTime,
  truncate,
} from '@/utils/chatBlocks.ts'

describe('parseAssistantContent', () => {
  it('returns empty blocks for null content', () => {
    expect(parseAssistantContent(null as any)).toEqual({ blocks: [], metadata: null })
  })

  it('returns empty blocks for undefined content', () => {
    expect(parseAssistantContent(undefined as any)).toEqual({ blocks: [], metadata: null })
  })

  it('returns empty blocks for empty string', () => {
    expect(parseAssistantContent('')).toEqual({ blocks: [], metadata: null })
  })

  it('returns text block for non-JSON content', () => {
    const result = parseAssistantContent('Hello, this is plain text')
    expect(result.blocks).toEqual([{ type: 'text', text: 'Hello, this is plain text' }])
    expect(result.metadata).toBeNull()
  })

  it('unwraps nested JSON serialization inside a text block', () => {
    const content = JSON.stringify({
      blocks: [
        {
          type: 'text',
          text: JSON.stringify({ content: { text: '解包后的消息', type: 'text' }, messageId: 'm1', sessionUpdate: 'user_message_chunk' }),
        },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].text).toBe('解包后的消息')
  })

  it('keeps normal text block text unchanged', () => {
    const content = JSON.stringify({ blocks: [{ type: 'text', text: '普通文本' }] })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].text).toBe('普通文本')
  })

  it('parses JSON with blocks array', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'Hello' },
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/test.go' } },
      ],
      metadata: { tokens: 100 },
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(2)
    expect(result.blocks[0].type).toBe('text')
    expect(result.blocks[1].type).toBe('tool_use')
    expect(result.metadata).toEqual({ tokens: 100 })
  })

  it('marks tool_use blocks as done when done is missing', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: {} },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].done).toBe(true)
  })

  it('marks tool_use blocks as done when done is false', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: {}, done: false },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].done).toBe(true)
  })

  it('preserves done=true on tool_use blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: {}, done: true },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].done).toBe(true)
  })

  it('deduplicates tool_use blocks by id - keeps richer input', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: {} },
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/test.go' } },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].input).toEqual({ file_path: '/test.go' })
  })

  it('deduplicates tool_use blocks - keeps previous when current is empty', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/test.go' } },
        { type: 'tool_use', name: 'Read', id: '1', input: {} },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].input).toEqual({ file_path: '/test.go' })
  })

  it('merges tool_use blocks when both have input', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/old.go' }, done: false },
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/new.go' }, done: true },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].input).toEqual({ file_path: '/new.go' })
    expect(result.blocks[0].done).toBe(true)
  })

  it('extracts cancelled flag', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'text', text: 'partial' }],
      cancelled: true,
    })
    const result = parseAssistantContent(content)
    expect(result.cancelled).toBe(true)
  })

  it('defaults cancelled to false', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'text', text: 'done' }],
    })
    const result = parseAssistantContent(content)
    expect(result.cancelled).toBe(false)
  })

  it('defaults metadata to null when not present', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'text', text: 'hello' }],
    })
    const result = parseAssistantContent(content)
    expect(result.metadata).toBeNull()
  })

  it('handles JSON without blocks array as text fallback', () => {
    const content = JSON.stringify({ message: 'not blocks' })
    const result = parseAssistantContent(content)
    expect(result.blocks).toEqual([{ type: 'text', text: content }])
  })

  it('handles text blocks interleaved with tool_use blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'Starting...' },
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/a.go' } },
        { type: 'text', text: 'Result:' },
        { type: 'tool_use', name: 'Grep', id: '2', input: { pattern: 'TODO' } },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(4)
  })

  // ── Tool output/status backward compat and dedup ──

  it('preserves output and status on tool_use blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Bash', id: 't1', input: { command: 'ls' }, done: true, output: 'file1.go\nfile2.go', status: 'success' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].output).toBe('file1.go\nfile2.go')
    expect(result.blocks[0].status).toBe('success')
  })

  it('migrates old Codex input.output to output field', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Bash', id: 't2', input: { command: 'ls', output: 'old-format-output' }, done: true },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].output).toBe('old-format-output')
    expect(result.blocks[0].input.output).toBeUndefined()
  })

  it('does not overwrite existing output with input.output', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Bash', id: 't3', input: { command: 'ls', output: 'legacy' }, done: true, output: 'new-format' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].output).toBe('new-format')
  })

  it('preserves error status on tool_use blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Bash', id: 't4', input: { command: 'bad' }, done: true, output: 'command not found', status: 'error' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].status).toBe('error')
    expect(result.blocks[0].output).toBe('command not found')
  })

  it('merges output and status during dedup - second block has output', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: 't5', input: { file_path: '/a.go' }, done: true },
        { type: 'tool_use', name: 'Read', id: 't5', input: { file_path: '/a.go' }, done: true, output: 'file contents', status: 'success' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].output).toBe('file contents')
    expect(result.blocks[0].status).toBe('success')
  })

  it('merges output from first block when second is empty', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: 't6', input: { file_path: '/a.go' }, done: true, output: 'result', status: 'success' },
        { type: 'tool_use', name: 'Read', id: 't6', input: {}, done: true },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].output).toBe('result')
    expect(result.blocks[0].status).toBe('success')
  })

  it('tool_use without output or status is valid (Codebuddy/Claude backends)', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: 't7', input: { file_path: '/a.go' }, done: true },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].output).toBeUndefined()
    expect(result.blocks[0].status).toBeUndefined()
    expect(result.blocks[0].done).toBe(true)
  })

  // ── Edge cases ──

  it('handles deeply nested JSON content without blocks', () => {
    const content = JSON.stringify({ data: { nested: true } })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(1)
    expect(result.blocks[0].type).toBe('text')
  })

  it('handles blocks array with only text blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'First paragraph' },
        { type: 'text', text: 'Second paragraph' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks).toHaveLength(2)
    expect(result.blocks[0].text).toBe('First paragraph')
    expect(result.blocks[1].text).toBe('Second paragraph')
  })

  it('handles tool_use block without id (no dedup)', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Bash', input: { command: 'ls' } },
        { type: 'tool_use', name: 'Bash', input: { command: 'pwd' } },
      ],
    })
    const result = parseAssistantContent(content)
    // No id means no dedup — both blocks preserved
    expect(result.blocks).toHaveLength(2)
  })

  it('handles malformed JSON gracefully', () => {
    const result = parseAssistantContent('{invalid json}')
    expect(result.blocks).toEqual([{ type: 'text', text: '{invalid json}' }])
  })
})

describe('hasImagesInContent', () => {
  it('detects markdown image syntax', () => {
    expect(hasImagesInContent('![alt](url)')).toBe(true)
  })

  it('returns false for text without images', () => {
    expect(hasImagesInContent('plain text')).toBe(false)
  })

  it('returns false for empty string', () => {
    expect(hasImagesInContent('')).toBe(false)
  })

  it('returns false for null', () => {
    expect(hasImagesInContent(null)).toBe(false)
  })

  it('returns false for undefined', () => {
    expect(hasImagesInContent(undefined)).toBe(false)
  })

  it('detects multiple images', () => {
    expect(hasImagesInContent('text ![a](b) more ![c](d)')).toBe(true)
  })

  it('does not match escaped image syntax', () => {
    // \\!\\[ is not an image — but includes '![' literally would match
    expect(hasImagesInContent('not an image: \\!\\[alt\\](url)')).toBe(false)
  })

  it('detects image reference-style syntax', () => {
    expect(hasImagesInContent('![alt][ref]')).toBe(true)
  })
})

describe('formatDetailTime', () => {
  it('formats ISO date string correctly', () => {
    const result = formatDetailTime('2026-01-15T14:30:45.000Z')
    expect(result).toMatch(/2026/)
    expect(result).toMatch(/01/)
    expect(result).toMatch(/15/)
  })

  it('pads single-digit months and days', () => {
    const result = formatDetailTime('2026-03-05T09:05:03.000Z')
    expect(result).toContain('03')
    expect(result).toContain('05')
  })

  it('includes hours, minutes, and seconds with zero-padding', () => {
    const result = formatDetailTime('2026-01-01T01:02:03.000Z')
    // The result may vary by timezone, but the format should be consistent
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })

  it('formats midnight correctly', () => {
    const result = formatDetailTime('2026-06-15T00:00:00.000Z')
    expect(result).toMatch(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/)
  })
})

describe('truncate', () => {
  it('returns empty for null/undefined', () => {
    expect(truncate(null, 10)).toBe('')
    expect(truncate(undefined, 10)).toBe('')
  })

  it('returns string unchanged when shorter than limit', () => {
    expect(truncate('hello', 10)).toBe('hello')
  })

  it('truncates and adds ellipsis when longer than limit', () => {
    expect(truncate('hello world', 5)).toBe('hello...')
  })

  it('handles exact length without truncation', () => {
    expect(truncate('hello', 5)).toBe('hello')
  })

  it('handles emoji/unicode correctly (runes, not bytes)', () => {
    expect(truncate('🎉🎊🎁', 2)).toBe('🎉🎊...')
  })

  it('returns empty for empty string', () => {
    expect(truncate('', 10)).toBe('')
  })

  it('truncates at unicode boundary, not byte boundary', () => {
    const chinese = '你好世界再见'
    expect(truncate(chinese, 3)).toBe('你好世...')
  })

  it('handles limit of 0', () => {
    expect(truncate('hello', 0)).toBe('...')
  })

  it('handles single character truncation', () => {
    expect(truncate('ab', 1)).toBe('a...')
  })
})
