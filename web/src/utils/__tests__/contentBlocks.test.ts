import { describe, expect, it } from 'vitest'
import {
  isSevereWarning,
  getWarningText,
  statusClass,
  statusLabel,
  statusLabelSimple,
  formatTime,
  askQuestionSummary,
  blockKey,
  blockTaskKey,
  buildTaskKeyIndex,
  hasScheduledTasks,
  scheduledTaskKeys,
  extractAtCommand,
  extractSlashCommand,
} from '@/utils/contentBlocks.ts'

// ── isSevereWarning ──
describe('isSevereWarning', () => {
  it('returns true for disconnect', () => {
    expect(isSevereWarning({ reason: 'disconnect' })).toBe(true)
  })
  it('returns true for timeout', () => {
    expect(isSevereWarning({ reason: 'timeout' })).toBe(true)
  })
  it('returns true for restart', () => {
    expect(isSevereWarning({ reason: 'restart' })).toBe(true)
  })
  it('returns true for panic', () => {
    expect(isSevereWarning({ reason: 'panic' })).toBe(true)
  })
  it('returns false for parse_error', () => {
    expect(isSevereWarning({ reason: 'parse_error' })).toBe(false)
  })
  it('returns false for unknown reason', () => {
    expect(isSevereWarning({ reason: 'some_other' })).toBe(false)
  })
  it('returns false when no reason', () => {
    expect(isSevereWarning({})).toBe(false)
  })
  it('returns false when reason is undefined', () => {
    expect(isSevereWarning({ reason: undefined })).toBe(false)
  })
})

// ── getWarningText ──
describe('getWarningText', () => {
  const t = (key: string) => key // Identity function — returns key as-is

  it('returns block.text when no reason', () => {
    expect(getWarningText({ text: 'fallback text' }, t)).toBe('fallback text')
  })

  it('returns block.text when reason has no i18n mapping', () => {
    // When t() returns the key unchanged, it equals the requested key, so we fall through
    expect(getWarningText({ reason: 'unknown_reason', text: 'raw text' }, t)).toBe('raw text')
  })

  it('returns translated text when i18n key found', () => {
    const tFound = (key: string) => key === 'chat.contentBlocks.warningReasons.stderr' ? 'Standard error output' : key
    expect(getWarningText({ reason: 'stderr', text: 'some stderr' }, tFound)).toBe('Standard error output')
  })

  it('appends detail after colon for parse_error', () => {
    const tFound = (key: string) => key === 'chat.contentBlocks.warningReasons.parse_error' ? 'Parse error' : key
    expect(getWarningText({ reason: 'parse_error', text: 'parse error: unexpected token at line 5' }, tFound)).toBe('Parse error: unexpected token at line 5')
  })

  it('appends detail after newline for backend_exit', () => {
    const tFound = (key: string) => key === 'chat.contentBlocks.warningReasons.backend_exit' ? 'Backend exited' : key
    expect(getWarningText({ reason: 'backend_exit', text: 'exit code 1\nstderr output here' }, tFound)).toBe('Backend exited\nstderr output here')
  })

  it('returns translated text for parse_error when no colon in text', () => {
    const tFound = (key: string) => key === 'chat.contentBlocks.warningReasons.parse_error' ? 'Parse error' : key
    expect(getWarningText({ reason: 'parse_error', text: 'nocolon' }, tFound)).toBe('Parse error')
  })

  it('returns empty string when no reason and no text', () => {
    expect(getWarningText({}, t)).toBe('')
  })

  it('handles null/undefined text gracefully', () => {
    expect(getWarningText({ reason: undefined, text: undefined }, t)).toBe('')
  })
})

// ── statusClass ──
describe('statusClass', () => {
  it('returns status-active for active task', () => {
    expect(statusClass({ status: 'active' })).toBe('status-active')
  })
  it('returns status-paused for paused task', () => {
    expect(statusClass({ status: 'paused' })).toBe('status-paused')
  })
  it('returns status-completed for completed task', () => {
    expect(statusClass({ status: 'completed' })).toBe('status-completed')
  })
  it('returns empty string for unknown status', () => {
    expect(statusClass({ status: 'unknown' })).toBe('')
  })
})

// ── statusLabel ──
describe('statusLabel', () => {
  const t = (key: string, params?: Record<string, any>) => {
    if (key === 'chat.contentBlocks.statusRunning') return 'Running'
    if (key === 'chat.contentBlocks.statusActive') return 'Active'
    if (key === 'chat.contentBlocks.statusExecutions') return `${params?.count} runs`
    if (key === 'chat.contentBlocks.statusPaused') return 'Paused'
    if (key === 'chat.contentBlocks.statusCompleted') return 'Completed'
    return key
  }

  it('shows active with execution count', () => {
    expect(statusLabel({ status: 'active', runCount: 3, runningCount: 0 }, t)).toBe('Active (3 runs)')
  })
  it('shows running when runningCount > 0', () => {
    expect(statusLabel({ status: 'active', runCount: 5, runningCount: 1 }, t)).toBe('Running (5 runs)')
  })
  it('shows paused', () => {
    expect(statusLabel({ status: 'paused', runCount: 0, runningCount: 0 }, t)).toBe('Paused')
  })
  it('shows completed', () => {
    expect(statusLabel({ status: 'completed', runCount: 2, runningCount: 0 }, t)).toBe('Completed')
  })
  it('returns raw status for unknown', () => {
    expect(statusLabel({ status: 'error', runCount: 0, runningCount: 0 }, t)).toBe('error')
  })
})

// ── statusLabelSimple ──
describe('statusLabelSimple', () => {
  const t = (key: string) => {
    if (key === 'chat.contentBlocks.statusActive') return 'Active'
    if (key === 'chat.contentBlocks.statusPaused') return 'Paused'
    if (key === 'chat.contentBlocks.statusCompleted') return 'Completed'
    return key
  }

  it('shows active', () => { expect(statusLabelSimple({ status: 'active' }, t)).toBe('Active') })
  it('shows paused', () => { expect(statusLabelSimple({ status: 'paused' }, t)).toBe('Paused') })
  it('shows completed', () => { expect(statusLabelSimple({ status: 'completed' }, t)).toBe('Completed') })
  it('returns raw status for unknown', () => { expect(statusLabelSimple({ status: 'error' }, t)).toBe('error') })
})

// ── formatTime ──
describe('formatTime', () => {
  const t = (key: string, params?: Record<string, any>) => {
    if (key === 'chat.contentBlocks.justNow') return 'Just now'
    if (key === 'chat.contentBlocks.minutesFromNow') return `${params?.count} min from now`
    if (key === 'chat.contentBlocks.minutesAgo') return `${params?.count} min ago`
    if (key === 'chat.contentBlocks.hoursFromNow') return `${params?.count}h from now`
    if (key === 'chat.contentBlocks.hoursAgo') return `${params?.count}h ago`
    return key
  }

  it('returns empty string for null', () => {
    expect(formatTime(null, 'en', t)).toBe('')
  })
  it('returns empty string for undefined', () => {
    expect(formatTime(undefined, 'en', t)).toBe('')
  })
  it('returns empty string for empty string', () => {
    expect(formatTime('', 'en', t)).toBe('')
  })
  it('returns "just now" for timestamp within 1 minute', () => {
    const now = new Date().toISOString()
    expect(formatTime(now, 'en', t)).toBe('Just now')
  })
  it('returns "X min ago" for past timestamp within 1 hour', () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString()
    const result = formatTime(fiveMinAgo, 'en', t)
    expect(result).toMatch(/min ago/)
  })
  it('returns "X min from now" for future timestamp within 1 hour', () => {
    const fiveMinFromNow = new Date(Date.now() + 5 * 60 * 1000).toISOString()
    const result = formatTime(fiveMinFromNow, 'en', t)
    expect(result).toMatch(/min from now/)
  })
  it('returns "Xh ago" for past timestamp within 1 day', () => {
    const twoHoursAgo = new Date(Date.now() - 2 * 3600 * 1000).toISOString()
    const result = formatTime(twoHoursAgo, 'en', t)
    expect(result).toMatch(/h ago/)
  })
  it('returns locale date string for timestamp beyond 1 day', () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 86400 * 1000).toISOString()
    const result = formatTime(twoDaysAgo, 'en', t)
    // Should be a date string, not a relative time
    expect(result).toMatch(/\d{4}/)
  })
  it('uses zh-CN locale for Chinese', () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 86400 * 1000).toISOString()
    const result = formatTime(twoDaysAgo, 'zh', t)
    expect(result).toBeTruthy()
  })
})

// ── askQuestionSummary ──
describe('askQuestionSummary', () => {
  it('returns empty string for null input', () => {
    expect(askQuestionSummary(null)).toBe('')
  })
  it('returns empty string for undefined input', () => {
    expect(askQuestionSummary(undefined)).toBe('')
  })
  it('returns empty string for input without questions', () => {
    expect(askQuestionSummary({ questions: [] })).toBe('')
  })
  it('returns header when available', () => {
    expect(askQuestionSummary({ questions: [{ header: 'Approach', question: 'Which approach?' }] })).toBe('Approach')
  })
  it('returns question when no header', () => {
    expect(askQuestionSummary({ questions: [{ question: 'Which approach?' }] })).toBe('Which approach?')
  })
  it('returns empty string when header and question are empty', () => {
    expect(askQuestionSummary({ questions: [{}] })).toBe('')
  })
  it('uses first question only', () => {
    expect(askQuestionSummary({ questions: [{ header: 'First' }, { header: 'Second' }] })).toBe('First')
  })
})

// ── blockKey ──
describe('blockKey', () => {
  it('uses db prefix when msgId is provided', () => {
    expect(blockKey('abc123', 0)).toBe('db-abc123-0')
  })
  it('uses numeric msgId', () => {
    expect(blockKey(42, 3)).toBe('db-42-3')
  })
  it('uses local prefix when msgId is empty string', () => {
    expect(blockKey('', 2)).toBe('local-2')
  })
  it('handles zero msgId as falsy', () => {
    expect(blockKey(0, 1)).toBe('local-1')
  })
})

// ── blockTaskKey ──
describe('blockTaskKey', () => {
  it('constructs key from msgId and block index', () => {
    expect(blockTaskKey('abc', 2)).toBe('abc-2')
  })
  it('handles numeric msgId', () => {
    expect(blockTaskKey(42, 0)).toBe('42-0')
  })
})

// ── buildTaskKeyIndex ──
describe('buildTaskKeyIndex', () => {
  it('returns empty object when msgId is undefined', () => {
    expect(buildTaskKeyIndex(undefined, {})).toEqual({})
  })
  it('returns empty object when no matching keys', () => {
    expect(buildTaskKeyIndex('abc', { 'xyz-0-0': {} })).toEqual({})
  })
  it('groups keys by block index', () => {
    const blockTasks = {
      'abc-0-0': { task: 'task1' },
      'abc-0-1': { task: 'task2' },
      'abc-2-0': { task: 'task3' },
    }
    const index = buildTaskKeyIndex('abc', blockTasks)
    expect(index['0']).toEqual(['abc-0-0', 'abc-0-1'])
    expect(index['2']).toEqual(['abc-2-0'])
    expect(index['1']).toBeUndefined()
  })
  it('skips keys without second dash', () => {
    const blockTasks = {
      'abc-0': { task: 'skip' },
      'abc-1-0': { task: 'keep' },
    }
    const index = buildTaskKeyIndex('abc', blockTasks)
    expect(index['0']).toBeUndefined()
    expect(index['1']).toEqual(['abc-1-0'])
  })
  it('sorts keys within each group', () => {
    const blockTasks = {
      'abc-0-2': {},
      'abc-0-0': {},
      'abc-0-1': {},
    }
    const index = buildTaskKeyIndex('abc', blockTasks)
    expect(index['0']).toEqual(['abc-0-0', 'abc-0-1', 'abc-0-2'])
  })
})

// ── hasScheduledTasks ──
describe('hasScheduledTasks', () => {
  it('returns false when no tasks for block', () => {
    expect(hasScheduledTasks({}, 0)).toBe(false)
  })
  it('returns true when tasks exist for block', () => {
    expect(hasScheduledTasks({ '0': ['abc-0-0'] }, '0')).toBe(true)
  })
  it('returns false when empty array', () => {
    expect(hasScheduledTasks({ '0': [] }, '0')).toBe(false)
  })
})

// ── scheduledTaskKeys ──
describe('scheduledTaskKeys', () => {
  it('returns empty array when no tasks for block', () => {
    expect(scheduledTaskKeys({}, 0)).toEqual([])
  })
  it('returns task keys for block', () => {
    expect(scheduledTaskKeys({ '1': ['abc-1-0', 'abc-1-1'] }, '1')).toEqual(['abc-1-0', 'abc-1-1'])
  })
})

// ── extractAtCommand ──
describe('extractAtCommand', () => {
  it('extracts @chatsearch with query', () => {
    const result = extractAtCommand('@chatsearch how to fix bug')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('@chatsearch')
    expect(result!.rest).toBe(' how to fix bug')
  })

  it('extracts @task with description', () => {
    const result = extractAtCommand('@task run daily backup')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('@task')
    expect(result!.rest).toBe(' run daily backup')
  })

  it('extracts @chatsearch with trailing space only', () => {
    const result = extractAtCommand('@chatsearch ')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('@chatsearch')
    expect(result!.rest).toBe(' ')
  })

  it('returns null for plain text', () => {
    expect(extractAtCommand('hello world')).toBeNull()
  })

  it('returns null for text not starting with known command', () => {
    expect(extractAtCommand('@other command')).toBeNull()
  })

  it('returns null for empty string', () => {
    expect(extractAtCommand('')).toBeNull()
  })

  it('extracts command without rest text', () => {
    const result = extractAtCommand('@task')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('@task')
    expect(result!.rest).toBe('')
  })
})

// ── extractSlashCommand ──
describe('extractSlashCommand', () => {
  it('detects /commit with rest text', () => {
    const result = extractSlashCommand('/commit fix auth bug')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('/commit')
    expect(result!.rest).toBe(' fix auth bug')
  })

  it('detects /commit without rest text', () => {
    const result = extractSlashCommand('/commit')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('/commit')
    expect(result!.rest).toBe('')
  })

  it('detects /superpowers:brainstorm with colon', () => {
    const result = extractSlashCommand('/superpowers:brainstorm design')
    expect(result).not.toBeNull()
    expect(result!.command).toBe('/superpowers:brainstorm')
    expect(result!.rest).toBe(' design')
  })

  it('returns null for plain text', () => {
    expect(extractSlashCommand('hello world')).toBeNull()
  })

  it('returns null for @ command', () => {
    expect(extractSlashCommand('@chatsearch test')).toBeNull()
  })

  it('returns null for / without command name', () => {
    expect(extractSlashCommand('/ ')).toBeNull()
  })

  it('returns null for empty string', () => {
    expect(extractSlashCommand('')).toBeNull()
  })
})
