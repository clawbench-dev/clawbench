// Time and task formatting utilities
import i18n from '@/i18n'

/**
 * BCP-47 tag for Intl. The app's locale ids are two-letter ("zh" / "en"), and
 * `new Intl.DateTimeFormat('zh')` already resolves, but spelling the region out
 * keeps weekday wording stable across engines.
 */
function localeTag(): string {
    return i18n.global.locale.value === 'zh' ? 'zh-CN' : 'en-US'
}

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

/**
 * Clock time for a timeline gutter: "14:32".
 *
 * 24-hour in BOTH locales on purpose. The gutter is ~38px wide and the stamps
 * stack vertically, so a 12-hour "2:32 PM" would either wrap or widen the
 * column, and the AM/PM suffix would break the vertical scan the gutter exists
 * for. The locale-dependent part (which day this is) lives in the day header —
 * see formatDayLabel.
 */
export function formatClockTime(date: string | Date | null | undefined): string {
    if (!date) return ''
    const d = date instanceof Date ? date : new Date(date)
    const ms = d.getTime()
    if (!Number.isFinite(ms) || d.getFullYear() < 2000) return ''
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

/** Local midnight for a date — the grouping key for a day bucket. */
export function startOfLocalDay(date: string | Date): number {
    const d = date instanceof Date ? date : new Date(date)
    return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

/**
 * Human day label for a grouped timeline: 今天 / 昨天 / 周三 / 10月12日
 * (Today / Yesterday / Wed / 10/12).
 *
 * Only the header spells the day out; every row inside the bucket shows just
 * its clock. Returns '' for missing/invalid input so callers can skip the
 * header instead of rendering "Invalid Date".
 */
export function formatDayLabel(date: string | Date | null | undefined): string {
    if (!date) return ''
    const d = date instanceof Date ? date : new Date(date)
    const ms = d.getTime()
    if (!Number.isFinite(ms) || d.getFullYear() < 2000) return ''
    // Round, not floor: a DST day is 23 or 25 hours long, so a raw division
    // would misreport yesterday as "today" twice a year.
    const dayDiff = Math.round((startOfLocalDay(new Date()) - startOfLocalDay(d)) / 86400000)
    if (dayDiff <= 0) return i18n.global.t('time.today')
    if (dayDiff === 1) return i18n.global.t('time.yesterday')
    if (dayDiff < 7) {
        // Intl, not the `cron.weekdayNames` message: those arrays go through
        // @intlify/unplugin-vue-i18n at build time and are compiled into message
        // ASTs, so `tm()` returns a node object rather than a string[] (the app
        // shipped "周{ \"t\": 0, \"b\": … }" as a day header). Intl also gets the
        // locale's own convention right — "周日"/"Sun" for zh/en, and the right
        // shape for any locale added later.
        return new Intl.DateTimeFormat(localeTag(), { weekday: 'short' }).format(d)
    }
    if (d.getFullYear() === new Date().getFullYear()) {
        return i18n.global.t('time.monthDay', { month: d.getMonth() + 1, day: d.getDate() })
    }
    return i18n.global.t('time.yearMonthDay', {
        year: d.getFullYear(),
        month: d.getMonth() + 1,
        day: d.getDate(),
    })
}

/**
 * Unabbreviated timestamp for a `title` tooltip: "2026-10-19 14:32:05".
 *
 * The visible surfaces all abbreviate (clock in the gutter, day in the
 * header), so the exact instant has to remain reachable somewhere.
 */
export function formatFullTimestamp(date: string | Date | null | undefined): string {
    if (!date) return ''
    const d = date instanceof Date ? date : new Date(date)
    if (!Number.isFinite(d.getTime()) || d.getFullYear() < 2000) return ''
    const p = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
        `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
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

/**
 * Format an ACCUMULATED span (a report total, not one turn) in the units a
 * person actually reads a duration in, always with exactly one decimal:
 *
 *   ≥ 1 day  → "1.5 天"
 *   ≥ 1 hour → "3.5 小时"
 *   ≥ 1 min  → "5.3 分钟"
 *   ≥ 1 s    → "1.5 秒"
 *   < 1 s    → "420 毫秒"  (integer; sub-second precision is the point)
 *
 * Why this is not formatDuration: that one caps at hours because it labels a
 * single reply in a compact meta bar ("3.5h" next to a timestamp). A month of
 * runtime summed over every turn is ~700 hours, and "703.2h" is a number
 * nobody can place — the same figure as "29.3 天" is immediately readable.
 * The units are localized here (and only here): an accumulated span is prose,
 * not a compact glyph.
 */
export function formatSpanDuration(ms: number): string {
    if (!Number.isFinite(ms) || ms < 0) return ''
    if (ms < 1000) return i18n.global.t('time.durationMs', { n: Math.round(ms) })
    const sec = ms / 1000
    if (sec < 60) return i18n.global.t('time.durationSec', { n: sec.toFixed(1) })
    const min = sec / 60
    if (min < 60) return i18n.global.t('time.durationMin', { n: min.toFixed(1) })
    const hour = min / 60
    if (hour < 24) return i18n.global.t('time.durationHour', { n: hour.toFixed(1) })
    return i18n.global.t('time.durationDay', { n: (hour / 24).toFixed(1) })
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
