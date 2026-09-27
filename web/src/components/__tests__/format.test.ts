import { describe, expect, it, vi } from 'vitest'

// ────────────────────────────────────────────────────────────
// formatDuration and statusLabel are pure functions we can test
// directly. formatRelativeTime, formatDateTime, humanizeCron,
// and repeatLabel depend on i18n which we need to mock.
// ────────────────────────────────────────────────────────────

// Mock i18n module
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string, params?: any) => {
        // Simple mock: return key with params for verification
        if (key === 'cron.everyMinutes') return `Every ${params?.count} min`
        if (key === 'cron.everyHours') return `Every ${params?.count} hours`
        if (key === 'cron.daily') return `Daily at ${params?.time}`
        if (key === 'cron.weekdays') return `Weekdays at ${params?.time}`
        if (key === 'cron.weekly') return `${params?.day} at ${params?.time}`
        if (key === 'cron.monthly') return `Monthly on day ${params?.day} at ${params?.time}`
        if (key === 'cron.hourly') return `Hourly at :${params?.minute}`
        if (key === 'task.repeat.once') return 'Once'
        if (key === 'task.repeat.times') return `${params?.count} times`
        if (key === 'task.repeat.unlimited') return 'Unlimited'
        if (key === 'task.status.active') return 'Enabled'
        if (key === 'task.status.paused') return 'Disabled'
        if (key === 'task.status.completed') return 'Completed'
        if (key === 'time.justNow') return 'Just now'
        if (key === 'time.minutesAgo') return `${params?.count} min ago`
        if (key === 'time.minutesFromNow') return `${params?.count} min from now`
        if (key === 'time.hoursAgo') return `${params?.count}h ago`
        if (key === 'time.hoursFromNow') return `${params?.count}h from now`
        if (key === 'time.daysAgo') return `${params?.count}d ago`
        if (key === 'time.daysFromNow') return `${params?.count}d from now`
        if (key === 'cron.weekdayNames') return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
        return key
      },
      locale: { value: 'en' },
    },
  },
}))

// Import after mocks
import {
  formatDuration,
  statusLabel,
  humanizeCron,
  repeatLabel,
  formatRelativeTime,
  formatDateTime,
  formatDateTimeWithYear,
  stripMarkdownPreview,
} from '@/utils/format.ts'

describe('formatDuration', () => {
  it('formats sub-second values as whole milliseconds', () => {
    expect(formatDuration(500)).toBe('500ms')
    expect(formatDuration(1)).toBe('1ms')
    expect(formatDuration(999)).toBe('999ms')
  })

  it('formats zero', () => {
    expect(formatDuration(0)).toBe('0ms')
  })

  it('formats seconds with one decimal', () => {
    expect(formatDuration(1500)).toBe('1.5s')
  })

  it('formats exact seconds', () => {
    expect(formatDuration(3000)).toBe('3.0s')
  })

  it('formats 1 second exactly', () => {
    expect(formatDuration(1000)).toBe('1.0s')
  })

  it('formats 59.9 seconds still as seconds', () => {
    expect(formatDuration(59900)).toBe('59.9s')
  })

  // ── Minutes: largest unit, no seconds tail ──
  it('formats exactly 60 seconds as minutes', () => {
    expect(formatDuration(60000)).toBe('1.0m')
  })

  it('formats 90 seconds as 1.5m', () => {
    expect(formatDuration(90000)).toBe('1.5m')
  })

  it('formats 999.9s as 16.7m (no seconds tail)', () => {
    expect(formatDuration(999900)).toBe('16.7m')
  })

  it('promotes 100 minutes to hours', () => {
    // 100 min ≥ 60 min, so it renders as 1.7h rather than the old "100m0s".
    expect(formatDuration(6000000)).toBe('1.7h')
  })

  // ── Hours: the old formatter never promoted, so "62m3s" and "100m0s" leaked ──
  it('promotes to hours past 60 minutes', () => {
    expect(formatDuration(3723000)).toBe('1.0h')
  })

  it('formats 1.5 hours', () => {
    expect(formatDuration(5400000)).toBe('1.5h')
  })

  it('formats a long multi-hour duration compactly', () => {
    // The exact bug this rewrite fixes: 2h10m used to render as "130m0s".
    expect(formatDuration(7800000)).toBe('2.2h')
  })

  it('returns empty for negative or non-finite input', () => {
    expect(formatDuration(-1)).toBe('')
    expect(formatDuration(NaN)).toBe('')
    expect(formatDuration(Infinity)).toBe('')
  })
})

describe('formatRelativeTime', () => {
  it('returns empty string for empty input', () => {
    expect(formatRelativeTime('')).toBe('')
  })

  it('returns empty for null-like input', () => {
    expect(formatRelativeTime(null)).toBe('')
    expect(formatRelativeTime(undefined)).toBe('')
  })

  it('returns empty for an invalid date string', () => {
    expect(formatRelativeTime('not-a-date')).toBe('')
  })

  it('returns "Just now" for dates less than 1 minute ago', () => {
    const now = new Date()
    const thirtySecondsAgo = new Date(now.getTime() - 30000)
    expect(formatRelativeTime(thirtySecondsAgo)).toBe('Just now')
  })

  it('returns minutes ago for dates within the hour', () => {
    const now = new Date()
    const fiveMinutesAgo = new Date(now.getTime() - 5 * 60000)
    expect(formatRelativeTime(fiveMinutesAgo)).toBe('5 min ago')
  })

  it('returns hours ago for dates within the day', () => {
    const now = new Date()
    const threeHoursAgo = new Date(now.getTime() - 3 * 3600000)
    expect(formatRelativeTime(threeHoursAgo)).toBe('3h ago')
  })

  it('returns days ago for dates within the week', () => {
    const now = new Date()
    const threeDaysAgo = new Date(now.getTime() - 3 * 86400000)
    expect(formatRelativeTime(threeDaysAgo)).toBe('3d ago')
  })

  // ── Future direction (task "next run") ──
  it('uses "from now" wording for future timestamps', () => {
    // A second of margin: the formatter reads its own Date.now(), so an exact
    // offset would land just under the unit boundary and floor one lower.
    const base = Date.now() + 1000
    expect(formatRelativeTime(new Date(base + 5 * 60000))).toBe('5 min from now')
    expect(formatRelativeTime(new Date(base + 3 * 3600000))).toBe('3h from now')
    expect(formatRelativeTime(new Date(base + 3 * 86400000))).toBe('3d from now')
  })

  it('returns locale date string for dates older than a week', () => {
    const now = new Date()
    const tenDaysAgo = new Date(now.getTime() - 10 * 86400000)
    const result = formatRelativeTime(tenDaysAgo)
    // Should be a date string containing slashes or month/day info
    expect(result).not.toBe('Just now')
    expect(result).not.toContain('min ago')
    expect(result).not.toContain('h ago')
    expect(result).not.toContain('d ago')
    // Should contain at least a digit (year, month, or day)
    expect(result).toMatch(/\d/)
  })

  it('handles ISO string input', () => {
    const now = new Date()
    const twoMinutesAgo = new Date(now.getTime() - 2 * 60000).toISOString()
    expect(formatRelativeTime(twoMinutesAgo)).toBe('2 min ago')
  })
})

describe('formatDateTime', () => {
  it('returns empty string for empty input', () => {
    expect(formatDateTime('')).toBe('')
  })

  it('returns formatted date string for valid date', () => {
    const result = formatDateTime('2025-01-15T10:30:00Z')
    // Should contain date and time components (digits)
    expect(result).toMatch(/\d/)
    expect(result.length).toBeGreaterThan(4)
  })

  it('handles Date object input', () => {
    const date = new Date('2025-06-15T14:30:00')
    const result = formatDateTime(date)
    expect(result).toMatch(/\d/)
    expect(result.length).toBeGreaterThan(4)
  })

  it('returns a string with time info', () => {
    const result = formatDateTime('2025-03-20T09:15:00')
    // toLocaleString output should contain colon-separated time
    expect(result).toMatch(/\d+:\d+/)
  })
})

describe('formatDateTimeWithYear', () => {
  it('returns empty string for falsy input', () => {
    expect(formatDateTimeWithYear('')).toBe('')
    expect(formatDateTimeWithYear(null as any)).toBe('')
  })

  it('omits the year for a current-year datetime (matches formatDateTime)', () => {
    const now = new Date()
    const d = new Date(now.getFullYear(), 0, 15, 8, 30) // Jan 15 this year
    expect(formatDateTimeWithYear(d)).toBe(formatDateTime(d))
  })

  it('includes the year for a cross-year (future) datetime', () => {
    const nextYear = new Date().getFullYear() + 1
    const d = new Date(nextYear, 11, 31, 8, 0) // Dec 31 next year
    const out = formatDateTimeWithYear(d)
    // A year-less format is ambiguous for a Dec 31 across the year boundary —
    // the 2-digit year must appear somewhere in the output.
    expect(out).toContain(String(nextYear).slice(-2))
    expect(out.length).toBeGreaterThan(formatDateTime(d).length)
  })

  it('handles string input', () => {
    const nextYear = new Date().getFullYear() + 1
    const out = formatDateTimeWithYear(`${nextYear}-12-31T08:00:00`)
    expect(out).toContain(String(nextYear).slice(-2))
  })
})

describe('stripMarkdownPreview', () => {
  it('returns empty string for empty input', () => {
    expect(stripMarkdownPreview('')).toBe('')
  })

  it('strips bold markdown', () => {
    expect(stripMarkdownPreview('This is **bold** text')).toBe('This is bold text')
  })

  it('strips italic markdown', () => {
    expect(stripMarkdownPreview('This is *italic* text')).toBe('This is italic text')
  })

  it('strips inline code', () => {
    expect(stripMarkdownPreview('Use `console.log` here')).toBe('Use console.log here')
  })

  it('strips code blocks', () => {
    expect(stripMarkdownPreview('Before\n```js\nconst x = 1\n```\nAfter')).toBe('Before After')
  })

  it('strips headings', () => {
    expect(stripMarkdownPreview('## Title')).toBe('Title')
  })

  it('strips links keeping link text', () => {
    expect(stripMarkdownPreview('[Click here](https://example.com)')).toBe('Click here')
  })

  it('strips strikethrough', () => {
    expect(stripMarkdownPreview('This is ~~deleted~~ text')).toBe('This is deleted text')
  })

  it('truncates long text with ellipsis', () => {
    const longText = 'a'.repeat(200)
    const result = stripMarkdownPreview(longText, 100)
    expect(result.length).toBe(101) // 100 + '…'
    expect(result.endsWith('…')).toBe(true)
  })

  it('preserves short text without truncation', () => {
    expect(stripMarkdownPreview('Short text')).toBe('Short text')
  })

  it('replaces newlines with spaces', () => {
    expect(stripMarkdownPreview('Line 1\nLine 2\nLine 3')).toBe('Line 1 Line 2 Line 3')
  })

  it('handles mixed markdown', () => {
    const result = stripMarkdownPreview('**Bold** and *italic* and `code`')
    expect(result).toBe('Bold and italic and code')
  })

  it('respects custom maxLen', () => {
    // 'Hello world' is 11 chars, maxLen=7 -> 'Hello w' + '…' = 'Hello w…'
    expect(stripMarkdownPreview('Hello world', 7)).toBe('Hello w…')
  })
})

describe('humanizeCron', () => {
  it('returns raw expression for invalid length', () => {
    expect(humanizeCron('invalid')).toBe('invalid')
  })

  it('returns raw expression for 4-part cron', () => {
    expect(humanizeCron('* * * *')).toBe('* * * *')
  })

  it('parses every-N-minutes', () => {
    expect(humanizeCron('*/5 * * * *')).toBe('Every 5 min')
  })

  it('parses every-N-hours', () => {
    expect(humanizeCron('0 */2 * * *')).toBe('Every 2 hours')
  })

  it('parses daily schedule', () => {
    expect(humanizeCron('0 9 * * *')).toBe('Daily at 9:00')
  })

  it('parses weekday schedule', () => {
    expect(humanizeCron('0 9 * * 1-5')).toBe('Weekdays at 9:00')
  })

  it('parses hourly schedule', () => {
    expect(humanizeCron('30 * * * *')).toBe('Hourly at :30')
  })

  it('parses weekly schedule with specific weekday', () => {
    expect(humanizeCron('0 9 * * 3')).toBe('Wed at 9:00')
  })

  it('parses monthly schedule', () => {
    expect(humanizeCron('0 9 15 * *')).toBe('Monthly on day 15 at 9:00')
  })

  it('returns raw expression for unrecognized pattern', () => {
    expect(humanizeCron('30 4 1 1 *')).toBe('30 4 1 1 *')
  })

  it('parses every-1-minute', () => {
    expect(humanizeCron('*/1 * * * *')).toBe('Every 1 min')
  })

  it('handles minute with padding (single-digit minute in daily)', () => {
    expect(humanizeCron('5 9 * * *')).toBe('Daily at 9:05')
  })
})

describe('repeatLabel', () => {
  it('returns "Once" for once mode', () => {
    expect(repeatLabel('once', 0)).toBe('Once')
  })

  it('returns count for limited mode', () => {
    expect(repeatLabel('limited', 5)).toBe('5 times')
  })

  it('returns "Unlimited" for unlimited mode', () => {
    expect(repeatLabel('unlimited', 0)).toBe('Unlimited')
  })

  it('returns "Unlimited" for any other mode', () => {
    expect(repeatLabel('other', 0)).toBe('Unlimited')
  })
})

describe('statusLabel', () => {
  it('returns "Enabled" for active status', () => {
    expect(statusLabel('active')).toBe('Enabled')
  })

  it('returns "Disabled" for paused status', () => {
    expect(statusLabel('paused')).toBe('Disabled')
  })

  it('returns "Completed" for completed status', () => {
    expect(statusLabel('completed')).toBe('Completed')
  })

  it('returns raw status for unknown status', () => {
    expect(statusLabel('unknown')).toBe('unknown')
  })

  it('returns empty string for empty status', () => {
    expect(statusLabel('')).toBe('')
  })
})
