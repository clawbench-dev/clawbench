// Time and task formatting utilities
import i18n from '@/i18n'

/**
 * Format a date as a friendly relative time.
 *
 * This is the ONE relative-time formatter for the app — every surface (chat meta
 * bars, session lists, task cards, ACP sessions, file mtimes) routes through it
 * so the same timestamp never reads differently in two places. It therefore
 * covers both directions: a past timestamp renders "5分钟前" and a future one
 * (e.g. a task's next run) renders "5分钟后", rather than showing a future time
 * as if it were in the past.
 *
 * Thresholds: < 1 min "just now", < 1 h minutes, < 1 day hours, < 7 days days,
 * then the localized date. Returns '' for missing/invalid input — callers rely
 * on that to hide the label and its separator.
 */
export function formatRelativeTime(date: string | Date | null | undefined): string {
    if (!date) return ''
    const d = date instanceof Date ? date : new Date(date)
    const ms = d.getTime()
    // Invalid input (NaN) and Go zero-value time.Time (year 0001) both mean
    // "no timestamp" rather than "a very long time ago".
    if (!Number.isFinite(ms) || d.getFullYear() < 2000) return ''
    const diff = Date.now() - ms
    const abs = Math.abs(diff)
    const future = diff < 0
    const minutes = Math.floor(abs / 60000)
    const hours = Math.floor(abs / 3600000)
    const days = Math.floor(abs / 86400000)

    if (minutes < 1) return i18n.global.t('time.justNow')
    if (minutes < 60) return i18n.global.t(future ? 'time.minutesFromNow' : 'time.minutesAgo', { count: minutes })
    if (hours < 24) return i18n.global.t(future ? 'time.hoursFromNow' : 'time.hoursAgo', { count: hours })
    if (days < 7) return i18n.global.t(future ? 'time.daysFromNow' : 'time.daysAgo', { count: days })
    return d.toLocaleDateString(i18n.global.locale.value === 'zh' ? 'zh-CN' : 'en-US')
}

/** Format a date as a localized datetime string */
export function formatDateTime(date: string | Date): string {
    if (!date) return ''
    const d = new Date(date)
    return d.toLocaleString(i18n.global.locale.value === 'zh' ? 'zh-CN' : 'en-US', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
    })
}

/**
 * Format a date as a localized datetime including the year.
 *
 * Used for key scheduling info (e.g. a task's next run time) that can span
 * years — `formatDateTime` omits the year, so a Dec 31 next-run would be
 * ambiguous. The year is shown only when it differs from the current year to
 * avoid clutter for same-year times.
 */
export function formatDateTimeWithYear(date: string | Date): string {
    if (!date) return ''
    const d = new Date(date)
    const locale = i18n.global.locale.value === 'zh' ? 'zh-CN' : 'en-US'
    const opts: Intl.DateTimeFormatOptions = {
        year: '2-digit',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
    }
    // Omit the year for same-year times to reduce clutter.
    if (d.getFullYear() === new Date().getFullYear()) {
        delete opts.year
    }
    return d.toLocaleString(locale, opts)
}

/** Humanize a cron expression into localized description */
export function humanizeCron(expr: string): string {
    const parts = expr.split(' ')
    if (parts.length !== 5) return expr
    const [min, hour, day, month, weekday] = parts
    const isNumeric = (s: string) => /^\d+$/.test(s)

    // Every N minutes: */N * * * *
    if (min.startsWith('*/') && hour === '*') return i18n.global.t('cron.everyMinutes', { count: min.slice(2) })
    // Every N hours: 0 */N * * *
    if (hour.startsWith('*/') && day === '*' && month === '*' && weekday === '*') return i18n.global.t('cron.everyHours', { count: hour.slice(2) })

    const timeStr = isNumeric(hour) ? `${hour}:${min.padStart(2, '0')}` : ''

    // Hourly at minute M: M * * * *
    if (isNumeric(min) && hour === '*' && day === '*' && month === '*' && weekday === '*') {
        return i18n.global.t('cron.hourly', { minute: min.padStart(2, '0') })
    }
    // Daily: M H * * *
    if (isNumeric(min) && isNumeric(hour) && day === '*' && month === '*' && weekday === '*') {
        return i18n.global.t('cron.daily', { time: timeStr })
    }
    // Weekly: M H * * DOW
    const weekdayNames = i18n.global.t('cron.weekdayNames') as unknown as string[]
    if (isNumeric(min) && isNumeric(hour) && day === '*' && month === '*') {
        if (weekday === '1-5') return i18n.global.t('cron.weekdays', { time: timeStr })
        if (isNumeric(weekday)) return i18n.global.t('cron.weekly', { day: weekdayNames[parseInt(weekday)], time: timeStr })
    }
    // Monthly: M H D * *
    if (isNumeric(min) && isNumeric(hour) && isNumeric(day) && month === '*' && weekday === '*') {
        return i18n.global.t('cron.monthly', { day, time: timeStr })
    }

    return expr
}

/**
 * Format milliseconds as a human-readable duration, using the largest unit that
 * applies and at most one decimal — the single duration formatter for the app.
 *
 *   < 1 s   → "500ms"    (integer; sub-second precision is the point)
 *   < 1 min → "1.5s"
 *   < 1 h   → "5.3m"     (no seconds: "5m20s" is needlessly long)
 *   ≥ 1 h   → "3.5h"
 *
 * The old formatter grew without bound ("62m3s", "100m0s") and never promoted
 * to hours, so a long turn read as a wall of digits. Capping at the largest
 * unit keeps the chat meta bar compact.
 */
export function formatDuration(ms: number): string {
    if (!Number.isFinite(ms) || ms < 0) return ''
    if (ms < 1000) return `${Math.round(ms)}ms`
    const sec = ms / 1000
    if (sec < 60) return `${sec.toFixed(1)}s`
    const min = sec / 60
    if (min < 60) return `${min.toFixed(1)}m`
    return `${(min / 60).toFixed(1)}h`
}

/** Strip markdown formatting for list previews */
export function stripMarkdownPreview(text: string, maxLen: number = 100): string {
    if (!text) return ''
    const clean = text
        .replace(/```[\s\S]*?```/g, '')   // code blocks
        .replace(/`([^`]+)`/g, '$1')       // inline code
        .replace(/#{1,6}\s+/g, '')         // headings
        .replace(/\*\*([^*]+)\*\*/g, '$1') // bold
        .replace(/\*([^*]+)\*/g, '$1')     // italic
        .replace(/__([^_]+)__/g, '$1')     // bold
        .replace(/_([^_]+)_/g, '$1')       // italic
        .replace(/~~([^~]+)~~/g, '$1')     // strikethrough
        .replace(/!\[[^\]]*\]\([^)]+\)/g, '')   // images (remove entirely)
        .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1') // links (keep text)
        .replace(/[#*`_~[\]()>|]/g, '')   // remaining syntax chars
        .replace(/\n+/g, ' ')             // newlines → space
        .trim()
    return [...clean].length > maxLen ? [...clean].slice(0, maxLen).join('') + '…' : clean
}

/** Get a label for task repeat mode */
export function repeatLabel(mode: string, maxRuns: number): string {
    if (mode === 'once') return i18n.global.t('task.repeat.once')
    if (mode === 'limited') return i18n.global.t('task.repeat.times', { count: maxRuns })
    return i18n.global.t('task.repeat.unlimited')
}

/** Get a label for task status */
export function statusLabel(status: string): string {
    if (status === 'active') return i18n.global.t('task.status.active')
    if (status === 'paused') return i18n.global.t('task.status.paused')
    if (status === 'completed') return i18n.global.t('task.status.completed')
    return status
}

/** Format badge count: truncate to "99+" when exceeding 99 */
export function formatBadgeCount(n: number): string | number {
    return n > 99 ? '99+' : n
}
