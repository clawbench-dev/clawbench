import { describe, expect, it } from 'vitest'
import { toolCallSummary } from '@/utils/chatBlocks.ts'

describe('toolCallSummary', () => {
  // ── Edge cases ──
  it('returns empty string for null input', () => {
    expect(toolCallSummary({ input: null })).toBe('')
  })

  it('returns empty string for undefined input', () => {
    expect(toolCallSummary({ input: undefined })).toBe('')
  })

  it('returns empty string for empty input object', () => {
    expect(toolCallSummary({ input: {} })).toBe('')
  })

  it('returns empty string for block without input field', () => {
    expect(toolCallSummary({} as any)).toBe('')
  })

  // ── AskUserQuestion ──
  it('shows question header for AskUserQuestion', () => {
    expect(toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [{ header: 'Choose approach', question: 'Which one?' }] },
    })).toBe('Choose approach')
  })

  it('shows question text when header is missing', () => {
    expect(toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [{ question: 'Which approach to use?' }] },
    })).toBe('Which approach to use?')
  })

  it('shows full question text without truncation', () => {
    const longQuestion = 'A'.repeat(70)
    const result = toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [{ question: longQuestion }] },
    })
    expect(result).toBe(longQuestion)
  })

  it('AskUserQuestion with empty questions array returns empty', () => {
    expect(toolCallSummary({
      name: 'AskUserQuestion',
      input: { questions: [] },
    })).toBe('')
  })

  it('AskUserQuestion is case-insensitive', () => {
    expect(toolCallSummary({
      name: 'askuserquestion',
      input: { questions: [{ header: 'Pick' }] },
    })).toBe('Pick')
  })

  // ── Description priority ──
  it('prefers description over file_path', () => {
    expect(toolCallSummary({
      input: { description: 'Fix the bug', file_path: '/tmp/test.go' },
    })).toBe('Fix the bug')
  })

  it('prefers description over command', () => {
    expect(toolCallSummary({
      input: { description: 'Run tests', command: 'npm test' },
    })).toBe('Run tests')
  })

  it('prefers description over pattern', () => {
    expect(toolCallSummary({
      input: { description: 'Search for TODOs', pattern: 'TODO' },
    })).toBe('Search for TODOs')
  })

  // ── file_path ──
  it('shows baseName for file_path', () => {
    expect(toolCallSummary({
      input: { file_path: '/home/user/project/main.go' },
    })).toBe('main.go')
  })

  it('shows baseName for file_path with no directory', () => {
    expect(toolCallSummary({
      input: { file_path: 'main.go' },
    })).toBe('main.go')
  })

  // ── command ──
  it('shows command', () => {
    expect(toolCallSummary({
      input: { command: 'npm test' },
    })).toBe('npm test')
  })

  it('shows full command without truncation', () => {
    const longCmd = 'npx '.repeat(20)
    const result = toolCallSummary({ input: { command: longCmd } })
    expect(result).toBe(longCmd)
  })

  // ── pattern (Grep/Glob) ──
  it('shows pattern for Grep', () => {
    expect(toolCallSummary({
      name: 'Grep',
      input: { pattern: 'TODO', path: './src' },
    })).toBe('TODO')
  })

  it('shows pattern for Glob', () => {
    expect(toolCallSummary({
      name: 'Glob',
      input: { pattern: '**/*.go', path: '.' },
    })).toBe('**/*.go')
  })

  it('shows full pattern without truncation', () => {
    const longPattern = '['.repeat(70) + 'test' + ']'.repeat(70)
    const result = toolCallSummary({ input: { pattern: longPattern } })
    expect(result).toBe(longPattern)
  })

  it('pattern has lower priority than file_path', () => {
    expect(toolCallSummary({
      input: { file_path: 'main.go', pattern: 'TODO' },
    })).toBe('main.go')
  })

  // ── query (WebSearch) ──
  it('shows query for WebSearch', () => {
    expect(toolCallSummary({
      name: 'WebSearch',
      input: { query: 'golang testing best practices' },
    })).toBe('golang testing best practices')
  })

  it('shows full query without truncation', () => {
    const longQuery = 'how to '.repeat(15)
    const result = toolCallSummary({ input: { query: longQuery } })
    expect(result).toBe(longQuery)
  })

  it('query has lower priority than pattern', () => {
    expect(toolCallSummary({
      input: { pattern: 'TODO', query: 'search query' },
    })).toBe('TODO')
  })

  // ── url (WebFetch) ──
  it('shows url for WebFetch', () => {
    expect(toolCallSummary({
      name: 'WebFetch',
      input: { url: 'https://example.com' },
    })).toBe('https://example.com')
  })

  it('shows full url without truncation', () => {
    const longUrl = 'https://' + 'a'.repeat(100) + '.com'
    const result = toolCallSummary({ input: { url: longUrl } })
    expect(result).toBe(longUrl)
  })

  it('url has lower priority than query', () => {
    expect(toolCallSummary({
      input: { query: 'search query', url: 'https://example.com' },
    })).toBe('search query')
  })

  // ── skill (Skill) ──
  it('shows skill name for Skill tool', () => {
    expect(toolCallSummary({
      name: 'Skill',
      input: { skill: 'commit' },
    })).toBe('commit')
  })

  it('skill has lower priority than url', () => {
    expect(toolCallSummary({
      input: { url: 'https://example.com', skill: 'commit' },
    })).toBe('https://example.com')
  })

  // ── prompt (Agent) ──
  it('shows prompt for Agent tool when no description', () => {
    expect(toolCallSummary({
      name: 'Agent',
      input: { prompt: 'Research the codebase' },
    })).toBe('Research the codebase')
  })

  it('shows description instead of prompt for Agent', () => {
    expect(toolCallSummary({
      name: 'Agent',
      input: { description: 'Fix the bug', prompt: 'Long detailed prompt...' },
    })).toBe('Fix the bug')
  })

  it('shows full prompt for Agent without truncation', () => {
    const longPrompt = 'P'.repeat(70)
    const result = toolCallSummary({
      name: 'Agent',
      input: { prompt: longPrompt },
    })
    expect(result).toBe(longPrompt)
  })

  it('prompt fallback for non-Agent tools goes to firstVal', () => {
    expect(toolCallSummary({
      name: 'Bash',
      input: { prompt: 'Some prompt' },
    })).toBe('Some prompt')
  })

  // ── path ──
  it('shows baseName for path', () => {
    expect(toolCallSummary({
      input: { path: '/home/user/project/src' },
    })).toBe('src')
  })

  // ── src_path + dst_path ──
  it('shows src → dst for move operations', () => {
    expect(toolCallSummary({
      input: { src_path: '/old/path/file.go', dst_path: '/new/path/file.go' },
    })).toBe('file.go → file.go')
  })

  it('src_path without dst_path falls through to firstVal', () => {
    // src_path is only processed when dst_path is also present
    expect(toolCallSummary({
      input: { src_path: '/old/path/file.go' },
    })).toBe('/old/path/file.go')
  })

  // ── Fallback: first value ──
  it('shows first string value as fallback', () => {
    expect(toolCallSummary({
      input: { custom_field: 'hello world' },
    })).toBe('hello world')
  })

  it('shows first value regardless of length', () => {
    const longVal = 'X'.repeat(80)
    expect(toolCallSummary({
      input: { custom_field: longVal },
    })).toBe(longVal)
  })

  it('ignores first value if not a string', () => {
    expect(toolCallSummary({
      input: { count: 42 },
    })).toBe('')
  })

  it('uses first string value as fallback when first value is a string', () => {
    expect(toolCallSummary({
      input: { text: 'hello', num: 42 },
    })).toBe('hello')
  })

  it('picks first string value by lexicographic key order, skipping non-strings', () => {
    // Deterministic fallback (mirrors Go ExtractSummary): sort keys, take the
    // first value that is a string. Non-string values are skipped.
    expect(toolCallSummary({
      input: { num: 42, text: 'hello' },
    })).toBe('hello')
  })

  it('deterministic fallback orders keys lexicographically', () => {
    expect(toolCallSummary({
      input: { zeta: 'last', alpha: 'first' },
    })).toBe('first')
  })
})
