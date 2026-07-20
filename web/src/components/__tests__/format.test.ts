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
        if (key === 'time.hoursAgo') return `${params?.count}h ago`
        if (key === 'time.daysAgo') return `${params?.count}d ago`
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
  stripMarkdownPreview,
} from '@/utils/format.ts'

describe('formatDuration', () => {
  it('formats milliseconds', () => {
    expect(formatDuration(500)).toBe('500ms')
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

  it('formats minutes and seconds', () => {
    expect(formatDuration(90000)).toBe('1m30s')
  })

  it('formats large duration', () => {
    expect(formatDuration(3723000)).toBe('62m3s')
  })

  it('formats exactly 60 seconds as 1m0s', () => {
    expect(formatDuration(60000)).toBe('1m0s')
  })

  it('formats 1ms', () => {
    expect(formatDuration(1)).toBe('1ms')
  })

  it('formats 999ms', () => {
    expect(formatDuration(999)).toBe('999ms')
  })

  it('formats 999.9s as 16m40s', () => {
    expect(formatDuration(999900)).toBe('16m40s')
  })

  it('formats 30.5s with one decimal place', () => {
    // toFixed(1) rounds 30.5 to "30.5"
    expect(formatDuration(30500)).toBe('30.5s')
  })

  it('formats 1 second exactly', () => {
    expect(formatDuration(1000)).toBe('1.0s')
  })

  it('formats 59.9 seconds', () => {
    expect(formatDuration(59900)).toBe('59.9s')
  })

  it('formats boundary at 1 minute', () => {
    expect(formatDuration(60500)).toBe('1m1s')
  })

  it('formats 100 minutes', () => {
    expect(formatDuration(6000000)).toBe('100m0s')
  })
})

describe('formatRelativeTime', () => {
  it('returns empty string for empty input', () => {
    expect(formatRelativeTime('')).toBe('')
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

  it('returns empty for null-like input', () => {
    expect(formatRelativeTime(null as any)).toBe('')
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
    expect(result.length).toBe(103) // 100 + '...'
    expect(result.endsWith('...')).toBe(true)
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
    // 'Hello world' is 11 chars, maxLen=7 -> 'Hello w' + '...' = 'Hello w...'
    expect(stripMarkdownPreview('Hello world', 7)).toBe('Hello w...')
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
