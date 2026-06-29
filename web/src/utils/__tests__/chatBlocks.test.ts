import { describe, expect, it, vi } from 'vitest'
import {
  parseAssistantContent,
  toolCallSummary,
  hasImagesInContent,
  formatDetailTime,
  formatMessageTime,
  truncate,
} from '@/utils/chatBlocks.ts'
import {
  humanizeCron,
  repeatLabel,
} from '@/utils/format.ts'

// Mock i18n to return the key for predictable assertions
vi.mock('@/i18n', () => ({
  default: {
    global: {
      locale: { value: 'en' },
      t: vi.fn((key: string, params?: Record<string, unknown>) => {
        // Return a predictable string based on key + params
        if (params) {
          const parts = Object.entries(params).map(([k, v]) => `${k}=${v}`)
          return `${key}:${parts.join(',')}`
        }
        return key
      }),
    },
  },
}))

vi.mock('@/composables/useLocale', () => ({
  gt: vi.fn((key: string, params?: Record<string, unknown>) => {
    if (params) {
      const parts = Object.entries(params).map(([k, v]) => `${k}=${v}`)
      return `${key}:${parts.join(',')}`
    }
    return key
  }),
}))

// ── parseAssistantContent ──

describe('parseAssistantContent', () => {
  it('handles null content', () => {
    expect(parseAssistantContent(null as any)).toEqual({ blocks: [], metadata: null })
  })

  it('handles undefined content', () => {
    expect(parseAssistantContent(undefined as any)).toEqual({ blocks: [], metadata: null })
  })

  it('handles empty string', () => {
    expect(parseAssistantContent('')).toEqual({ blocks: [], metadata: null })
  })

  it('falls back to text block for non-JSON', () => {
    expect(parseAssistantContent('hello world')).toEqual({
      blocks: [{ type: 'text', text: 'hello world' }],
      metadata: null,
    })
  })

  it('falls back to text block for JSON without blocks', () => {
    const content = JSON.stringify({ error: 'not blocks' })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].type).toBe('text')
  })

  it('parses blocks with metadata', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'text', text: 'hi' }],
      metadata: { tokens: 100, model: 'gpt-4' },
    })
    const result = parseAssistantContent(content)
    expect(result.metadata).toEqual({ tokens: 100, model: 'gpt-4' })
  })

  it('parses cancelled flag', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'text', text: 'partial' }],
      cancelled: true,
    })
    expect(parseAssistantContent(content).cancelled).toBe(true)
  })

  it('defaults cancelled to false', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'text', text: 'done' }],
    })
    expect(parseAssistantContent(content).cancelled).toBe(false)
  })

  it('marks tool_use as done when missing', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'tool_use', name: 'Read', id: '1', input: {} }],
    })
    expect(parseAssistantContent(content).blocks[0].done).toBe(true)
  })

  it('marks tool_use as done when false', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'tool_use', name: 'Read', id: '1', input: {}, done: false }],
    })
    expect(parseAssistantContent(content).blocks[0].done).toBe(true)
  })

  it('preserves done=true', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'tool_use', name: 'Read', id: '1', input: {}, done: true }],
    })
    expect(parseAssistantContent(content).blocks[0].done).toBe(true)
  })

  it('migrates input.output to output field (Codex backward compat)', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'tool_use', name: 'Bash', id: '1', input: { command: 'ls', output: 'file.go' }, done: true }],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].output).toBe('file.go')
    expect(result.blocks[0].input.output).toBeUndefined()
  })

  it('does not overwrite existing output with input.output', () => {
    const content = JSON.stringify({
      blocks: [{ type: 'tool_use', name: 'Bash', id: '1', input: { command: 'ls', output: 'legacy' }, done: true, output: 'modern' }],
    })
    expect(parseAssistantContent(content).blocks[0].output).toBe('modern')
  })

  // ── Deduplication ──

  it('deduplicates tool_use by id - keeps richer input', () => {
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

  it('deduplicates - keeps previous when current is empty', () => {
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

  it('merges tool_use when both have input', () => {
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

  it('preserves output and status during dedup', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/a.go' }, done: true },
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/a.go' }, done: true, output: 'contents', status: 'success' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0].output).toBe('contents')
    expect(result.blocks[0].status).toBe('success')
  })

  it('handles tool_use without id (no dedup)', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'tool_use', name: 'Bash', input: { command: 'ls' } },
        { type: 'tool_use', name: 'Bash', input: { command: 'pwd' } },
      ],
    })
    expect(parseAssistantContent(content).blocks).toHaveLength(2)
  })

  it('handles text blocks interleaved with tool_use', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'Starting...' },
        { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/a.go' } },
        { type: 'text', text: 'Result:' },
        { type: 'tool_use', name: 'Grep', id: '2', input: { pattern: 'TODO' } },
      ],
    })
    expect(parseAssistantContent(content).blocks).toHaveLength(4)
  })

  // ── {"blocks":null} guard (fcfb228c regression test) ──

  it('treats {"blocks":null} as empty blocks instead of rendering as text', () => {
    const result = parseAssistantContent('{"blocks":null}')
    expect(result.blocks).toEqual([])
    expect(result.metadata).toBeNull()
  })

  it('preserves metadata when blocks is null', () => {
    const result = parseAssistantContent('{"blocks":null,"metadata":{"model":"gpt-4"}}')
    expect(result.blocks).toEqual([])
    expect(result.metadata).toEqual({ model: 'gpt-4' })
  })

  it('preserves cancelled flag when blocks is null', () => {
    const result = parseAssistantContent('{"blocks":null,"cancelled":true}')
    expect(result.blocks).toEqual([])
    expect(result.cancelled).toBe(true)
  })

  it('preserves both metadata and cancelled when blocks is null', () => {
    const result = parseAssistantContent('{"blocks":null,"metadata":{"wallMs":100},"cancelled":true}')
    expect(result.blocks).toEqual([])
    expect(result.metadata).toEqual({ wallMs: 100 })
    expect(result.cancelled).toBe(true)
  })

  // ── Thinking block _key assignment ──

  it('assigns _key to thinking blocks parsed from DB', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'thinking', text: 'first thought' },
        { type: 'tool_use', name: 'Read', id: '1', input: {} },
        { type: 'thinking', text: 'second thought' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0]._key).toBe('thinking-0')
    expect(result.blocks[2]._key).toBe('thinking-1')
  })

  it('does not overwrite existing _key on thinking blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'thinking', text: 'thought', _key: 'thinking-5' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0]._key).toBe('thinking-5')
  })

  it('does not assign _key to non-thinking blocks', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'text', text: 'hello' },
        { type: 'tool_use', name: 'Read', id: '1', input: {} },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0]._key).toBeUndefined()
    expect(result.blocks[1]._key).toBeUndefined()
  })

  it('assigns sequential _key across multiple thinking blocks with interleaved tools', () => {
    const content = JSON.stringify({
      blocks: [
        { type: 'thinking', text: 'think1' },
        { type: 'tool_use', name: 'Read', id: 'r1', input: {} },
        { type: 'thinking', text: 'think2' },
        { type: 'tool_use', name: 'Write', id: 'w1', input: {} },
        { type: 'thinking', text: 'think3' },
      ],
    })
    const result = parseAssistantContent(content)
    expect(result.blocks[0]._key).toBe('thinking-0')
    expect(result.blocks[2]._key).toBe('thinking-1')
    expect(result.blocks[4]._key).toBe('thinking-2')
  })
})

// ── toolCallSummary ──

describe('toolCallSummary', () => {
  it('returns empty for null input', () => {
    expect(toolCallSummary({ input: null })).toBe('')
  })

  it('returns empty for undefined input', () => {
    expect(toolCallSummary({ input: undefined })).toBe('')
  })

  it('returns empty for empty input object', () => {
    expect(toolCallSummary({ input: {} })).toBe('')
  })

  it('shows header for AskUserQuestion', () => {
    expect(toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [{ header: 'Pick', question: 'Which?' }] },
    })).toBe('Pick')
  })

  it('shows question when header is empty', () => {
    expect(toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [{ question: 'Which one?' }] },
    })).toBe('Which one?')
  })

  it('shows full AskUserQuestion text without truncation', () => {
    const long = 'A'.repeat(70)
    const result = toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [{ question: long }] },
    })
    expect(result).toBe(long)
  })

  it('prefers description over file_path', () => {
    expect(toolCallSummary({
      input: { description: 'Fix bug', file_path: '/test.go' },
    })).toBe('Fix bug')
  })

  it('shows baseName for file_path', () => {
    expect(toolCallSummary({
      input: { file_path: '/home/user/main.go' },
    })).toBe('main.go')
  })

  it('shows full command without truncation', () => {
    const result = toolCallSummary({ input: { command: 'npx '.repeat(20) } })
    expect(result).toBe('npx '.repeat(20))
  })

  it('shows pattern for Grep', () => {
    expect(toolCallSummary({ name: 'Grep', input: { pattern: 'TODO' } })).toBe('TODO')
  })

  it('shows query for WebSearch', () => {
    expect(toolCallSummary({ name: 'WebSearch', input: { query: 'golang test' } })).toBe('golang test')
  })

  it('shows url for WebFetch', () => {
    expect(toolCallSummary({ name: 'WebFetch', input: { url: 'https://example.com' } })).toBe('https://example.com')
  })

  it('shows skill name', () => {
    expect(toolCallSummary({ input: { skill: 'commit' } })).toBe('commit')
  })

  it('shows prompt for Agent tool', () => {
    expect(toolCallSummary({ name: 'Agent', input: { prompt: 'Research this' } })).toBe('Research this')
  })

  it('shows baseName for path', () => {
    expect(toolCallSummary({ input: { path: '/home/user/src' } })).toBe('src')
  })

  it('shows src → dst for move', () => {
    expect(toolCallSummary({
      input: { src_path: '/old/file.go', dst_path: '/new/file.go' },
    })).toBe('file.go → file.go')
  })

  it('uses first string value as fallback', () => {
    expect(toolCallSummary({ input: { custom: 'hello' } })).toBe('hello')
  })

  it('shows first value regardless of length', () => {
    expect(toolCallSummary({ input: { data: 'X'.repeat(80) } })).toBe('X'.repeat(80))
  })

  it('ignores non-string first value', () => {
    expect(toolCallSummary({ input: { count: 42 } })).toBe('')
  })

  it('returns empty when input is a string (partial JSON from streaming)', () => {
    expect(toolCallSummary({ input: '{' })).toBe('')
    expect(toolCallSummary({ input: '{"command":"ls"}' })).toBe('')
  })

  it('returns empty when input is an array', () => {
    expect(toolCallSummary({ input: ['a', 'b'] })).toBe('')
  })

  // Slim format: block.summary takes priority over computing from input
  it('prefers block.summary over input-derived summary', () => {
    expect(toolCallSummary({
      summary: 'pre-extracted summary',
      input: { description: 'input description', file_path: '/test.go' },
    })).toBe('pre-extracted summary')
  })

  it('falls back to input when summary is empty string', () => {
    expect(toolCallSummary({
      summary: '',
      input: { file_path: '/main.go' },
    })).toBe('main.go')
  })
})

// ── hasImagesInContent ──

describe('hasImagesInContent', () => {
  it('detects markdown image', () => {
    expect(hasImagesInContent('![alt](url)')).toBe(true)
  })

  it('returns false for plain text', () => {
    expect(hasImagesInContent('hello world')).toBe(false)
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
    expect(hasImagesInContent('![a](b) and ![c](d)')).toBe(true)
  })

  it('detects reference-style image', () => {
    expect(hasImagesInContent('![alt][ref]')).toBe(true)
  })
})

// ── formatDetailTime ──

describe('formatDetailTime', () => {
  it('formats to YYYY-MM-DD HH:mm:ss', () => {
    const result = formatDetailTime('2026-01-15T14:30:45.000Z')
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })

  it('zero-pads components', () => {
    const result = formatDetailTime('2026-03-05T09:05:03.000Z')
    expect(result).toContain('03')
    expect(result).toContain('05')
  })
})

// ── humanizeCron ──

describe('humanizeCron', () => {
  it('returns expression as-is for non-5-part cron', () => {
    expect(humanizeCron('invalid')).toBe('invalid')
  })

  it('returns expression for 4-part cron', () => {
    expect(humanizeCron('* * * *')).toBe('* * * *')
  })

  it('returns expression for 6-part cron', () => {
    expect(humanizeCron('* * * * * *')).toBe('* * * * * *')
  })

  it('returns expression for empty string', () => {
    expect(humanizeCron('')).toBe('')
  })

  it('renders "every N minutes" for */N * * * *', () => {
    const result = humanizeCron('*/5 * * * *')
    expect(result).toContain('cron.everyMinutes')
    expect(result).toContain('count=5')
  })

  it('renders "every N hours" for 0 */N * * *', () => {
    const result = humanizeCron('0 */2 * * *')
    expect(result).toContain('cron.everyHours')
    expect(result).toContain('count=2')
  })

  it('renders "daily at HH:00" for 0 HH * * *', () => {
    const result = humanizeCron('0 9 * * *')
    expect(result).toContain('cron.daily')
    expect(result).toContain('9:00')
  })

  it('renders "weekdays at HH:00" for 0 HH * * 1-5', () => {
    const result = humanizeCron('0 8 * * 1-5')
    expect(result).toContain('cron.weekdays')
    expect(result).toContain('8:00')
  })

  it('returns expression for patterns not matching any known format', () => {
    expect(humanizeCron('30 4 1 1 *')).toBe('30 4 1 1 *')
  })

  it('renders "every 1 minutes" for */1 * * * *', () => {
    const result = humanizeCron('*/1 * * * *')
    expect(result).toContain('cron.everyMinutes')
    expect(result).toContain('count=1')
  })

  it('renders "every 1 hours" for 0 */1 * * *', () => {
    const result = humanizeCron('0 */1 * * *')
    expect(result).toContain('cron.everyHours')
    expect(result).toContain('count=1')
  })
})

// ── repeatLabel ──

describe('repeatLabel', () => {
  it('renders "once" mode', () => {
    const result = repeatLabel('once', 1)
    expect(result).toContain('task.repeat.once')
  })

  it('renders "limited" mode with count', () => {
    const result = repeatLabel('limited', 5)
    expect(result).toContain('task.repeat.times')
    expect(result).toContain('count=5')
  })

  it('renders "unlimited" mode (default)', () => {
    const result = repeatLabel('unlimited', 0)
    expect(result).toContain('task.repeat.unlimited')
  })

  it('renders unknown mode as unlimited fallback', () => {
    const result = repeatLabel('unknown', 0)
    expect(result).toContain('task.repeat.unlimited')
  })

  it('renders limited mode with count=1', () => {
    const result = repeatLabel('limited', 1)
    expect(result).toContain('task.repeat.times')
    expect(result).toContain('count=1')
  })
})

// ── truncate ──

describe('truncate', () => {
  it('returns empty for null', () => {
    expect(truncate(null, 10)).toBe('')
  })

  it('returns empty for undefined', () => {
    expect(truncate(undefined, 10)).toBe('')
  })

  it('returns empty for empty string', () => {
    expect(truncate('', 10)).toBe('')
  })

  it('returns string unchanged when shorter', () => {
    expect(truncate('hello', 10)).toBe('hello')
  })

  it('truncates and adds ellipsis', () => {
    expect(truncate('hello world', 5)).toBe('hello...')
  })

  it('handles exact length', () => {
    expect(truncate('hello', 5)).toBe('hello')
  })

  it('handles Unicode/emoji', () => {
    expect(truncate('🎉🎊🎁', 2)).toBe('🎉🎊...')
  })

  it('handles limit of 0', () => {
    expect(truncate('hello', 0)).toBe('...')
  })

  it('handles single character', () => {
    expect(truncate('ab', 1)).toBe('a...')
  })

  it('handles Chinese characters', () => {
    expect(truncate('你好世界再见', 3)).toBe('你好世...')
  })

  it('does not break surrogate pairs', () => {
    const emoji = '👨‍👩‍👧‍👦👨‍👩‍👧‍👦'
    const result = truncate(emoji, 1)
    // Should truncate at codepoint boundary, not in the middle of a grapheme
    expect(result.endsWith('...')).toBe(true)
  })
})

// ── formatMessageTime ──

describe('formatMessageTime', () => {
  it('shows "just now" for timestamps less than 1 minute ago', () => {
    const now = new Date().toISOString()
    const result = formatMessageTime(now)
    expect(result).toContain('time.justNow')
  })

  it('shows "minutes ago" for timestamps within the last hour', () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60000).toISOString()
    const result = formatMessageTime(fiveMinAgo)
    expect(result).toContain('time.minutesAgo')
    expect(result).toContain('count=5')
  })

  it('shows "hours ago" for timestamps within the last day', () => {
    const twoHoursAgo = new Date(Date.now() - 2 * 3600000).toISOString()
    const result = formatMessageTime(twoHoursAgo)
    expect(result).toContain('time.hoursAgo')
    expect(result).toContain('count=2')
  })

  it('shows "days ago" for timestamps within the last week', () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 86400000).toISOString()
    const result = formatMessageTime(threeDaysAgo)
    expect(result).toContain('time.daysAgo')
    expect(result).toContain('count=3')
  })

  it('shows date for timestamps older than 7 days', () => {
    const tenDaysAgo = new Date(Date.now() - 10 * 86400000).toISOString()
    const result = formatMessageTime(tenDaysAgo)
    // Falls back to locale date format, should NOT contain "time." i18n keys
    expect(result).not.toContain('time.justNow')
    expect(result).not.toContain('time.minutesAgo')
    expect(result).not.toContain('time.hoursAgo')
    expect(result).not.toContain('time.daysAgo')
    // Should contain a date-like string (digits and delimiters)
    expect(result.length).toBeGreaterThan(0)
  })

  it('shows 1 minute ago correctly', () => {
    const oneMinAgo = new Date(Date.now() - 60000).toISOString()
    const result = formatMessageTime(oneMinAgo)
    expect(result).toContain('time.minutesAgo')
    expect(result).toContain('count=1')
  })

  it('shows 59 minutes ago correctly', () => {
    const fiftyNineMinAgo = new Date(Date.now() - 59 * 60000).toISOString()
    const result = formatMessageTime(fiftyNineMinAgo)
    expect(result).toContain('time.minutesAgo')
    expect(result).toContain('count=59')
  })

  it('shows 23 hours ago correctly', () => {
    const twentyThreeHoursAgo = new Date(Date.now() - 23 * 3600000).toISOString()
    const result = formatMessageTime(twentyThreeHoursAgo)
    expect(result).toContain('time.hoursAgo')
    expect(result).toContain('count=23')
  })

  it('shows 6 days ago correctly', () => {
    const sixDaysAgo = new Date(Date.now() - 6 * 86400000).toISOString()
    const result = formatMessageTime(sixDaysAgo)
    expect(result).toContain('time.daysAgo')
    expect(result).toContain('count=6')
  })
})

// ── formatDetailTime (enhanced) ──

describe('formatDetailTime', () => {
  it('formats to YYYY-MM-DD HH:mm:ss', () => {
    const result = formatDetailTime('2026-01-15T14:30:45.000Z')
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })

  it('zero-pads components', () => {
    const result = formatDetailTime('2026-03-05T09:05:03.000Z')
    expect(result).toContain('03')
    expect(result).toContain('05')
  })

  it('formats midnight correctly', () => {
    // Use local time string to avoid timezone offset issues
    const result = formatDetailTime('2026-12-25T00:00:00')
    expect(result).toBe('2026-12-25 00:00:00')
  })

  it('formats end of day correctly', () => {
    const result = formatDetailTime('2026-06-30T23:59:59')
    expect(result).toBe('2026-06-30 23:59:59')
  })

  it('handles ISO date string without timezone', () => {
    const result = formatDetailTime('2026-07-01T12:30:00')
    expect(result).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })

  it('produces consistent format for same input', () => {
    const input = '2026-01-15T14:30:45.000Z'
    expect(formatDetailTime(input)).toBe(formatDetailTime(input))
  })
})
