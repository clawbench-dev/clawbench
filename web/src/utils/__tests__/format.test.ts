import { describe, expect, it, vi } from 'vitest'
import { formatDuration, stripMarkdownPreview, formatBadgeCount } from '@/utils/format.ts'

// Mock i18n module — factory must not reference external variables (hoisted)
vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string, params?: any) => {
    if (key === 'cron.weekdayNames') return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
    return key + (params ? JSON.stringify(params) : '')
  }, locale: { value: 'zh' } } }
}))

// Import i18n-dependent functions AFTER mock
import { formatRelativeTime, formatDateTime, humanizeCron, repeatLabel, statusLabel } from '@/utils/format.ts'

describe('formatDuration', () => {
  it('formats milliseconds', () => {
    expect(formatDuration(500)).toBe('500ms')
  })

  it('formats seconds', () => {
    expect(formatDuration(3000)).toBe('3.0s')
  })

  it('formats seconds with decimal', () => {
    expect(formatDuration(1234)).toBe('1.2s')
  })

  it('formats minutes and seconds', () => {
    expect(formatDuration(90000)).toBe('1m30s')
  })

  it('formats large minutes', () => {
    expect(formatDuration(3661000)).toBe('61m1s')
  })
})

describe('stripMarkdownPreview', () => {
  it('strips bold', () => {
    expect(stripMarkdownPreview('**hello**')).toBe('hello')
  })

  it('strips italic', () => {
    expect(stripMarkdownPreview('*hello*')).toBe('hello')
  })

  it('strips inline code', () => {
    expect(stripMarkdownPreview('use `foo`')).toBe('use foo')
  })

  it('strips code blocks', () => {
    expect(stripMarkdownPreview('before```js\ncode\n```after')).toBe('beforeafter')
  })

  it('strips headings', () => {
    expect(stripMarkdownPreview('## Title')).toBe('Title')
  })

  it('strips links', () => {
    expect(stripMarkdownPreview('[click](http://example.com)')).toBe('click')
  })

  it('strips strikethrough', () => {
    expect(stripMarkdownPreview('~~deleted~~')).toBe('deleted')
  })

  it('truncates long text', () => {
    const long = 'a'.repeat(200)
    const result = stripMarkdownPreview(long, 100)
    expect(result.length).toBeLessThanOrEqual(103) // 100 + '...'
    expect(result).toContain('...')
  })

  it('returns empty for empty input', () => {
    expect(stripMarkdownPreview('')).toBe('')
  })

  it('handles newlines', () => {
    expect(stripMarkdownPreview('line1\nline2')).toBe('line1 line2')
  })
})

describe('formatRelativeTime', () => {
  it('returns justNow for very recent dates', () => {
    const now = new Date()
    const result = formatRelativeTime(now.toISOString())
    expect(result).toContain('justNow')
  })

  it('returns minutesAgo for dates within the hour', () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60000)
    const result = formatRelativeTime(fiveMinAgo.toISOString())
    expect(result).toContain('minutesAgo')
  })

  it('returns empty for empty input', () => {
    expect(formatRelativeTime('')).toBe('')
  })

  it('returns hoursAgo for dates within the day', () => {
    const threeHoursAgo = new Date(Date.now() - 3 * 3600000)
    const result = formatRelativeTime(threeHoursAgo.toISOString())
    expect(result).toContain('hoursAgo')
  })

  it('returns daysAgo for dates within the week', () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 86400000)
    const result = formatRelativeTime(threeDaysAgo.toISOString())
    expect(result).toContain('daysAgo')
  })
})

describe('formatDateTime', () => {
  it('returns empty for empty input', () => {
    expect(formatDateTime('')).toBe('')
  })

  it('formats a valid date', () => {
    const result = formatDateTime('2026-01-15T10:30:00Z')
    expect(result).toBeTruthy()
  })
})

describe('humanizeCron', () => {
  it('returns everyMinutes for */N * * * *', () => {
    expect(humanizeCron('*/5 * * * *')).toContain('everyMinutes')
  })

  it('returns everyHours for 0 */N * * *', () => {
    expect(humanizeCron('0 */2 * * *')).toContain('everyHours')
  })

  it('returns hourly for M * * * *', () => {
    expect(humanizeCron('30 * * * *')).toContain('hourly')
  })

  it('returns daily for M H * * *', () => {
    expect(humanizeCron('30 9 * * *')).toContain('daily')
  })

  it('returns expr for invalid cron', () => {
    expect(humanizeCron('invalid')).toBe('invalid')
  })
})

describe('repeatLabel', () => {
  it('returns once for once mode', () => {
    expect(repeatLabel('once', 0)).toContain('once')
  })

  it('returns times for limited mode', () => {
    expect(repeatLabel('limited', 5)).toContain('times')
  })

  it('returns unlimited for other mode', () => {
    expect(repeatLabel('unlimited', 0)).toContain('unlimited')
  })
})

describe('statusLabel', () => {
  it('returns active for active status', () => {
    expect(statusLabel('active')).toContain('active')
  })

  it('returns paused for paused status', () => {
    expect(statusLabel('paused')).toContain('paused')
  })

  it('returns status as-is for unknown status', () => {
    expect(statusLabel('unknown')).toBe('unknown')
  })
})

describe('formatBadgeCount', () => {
  it('returns number as-is when <= 99', () => {
    expect(formatBadgeCount(0)).toBe(0)
    expect(formatBadgeCount(1)).toBe(1)
    expect(formatBadgeCount(50)).toBe(50)
    expect(formatBadgeCount(99)).toBe(99)
  })

  it('returns "99+" when > 99', () => {
    expect(formatBadgeCount(100)).toBe('99+')
    expect(formatBadgeCount(999)).toBe('99+')
    expect(formatBadgeCount(10000)).toBe('99+')
  })
})
