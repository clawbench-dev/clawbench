// Time and task formatting utilities
import i18n from '@/i18n'

/** Format a date as relative time */
export function formatRelativeTime(date: string | Date): string {
    if (!date) return ''
    const d = new Date(date)
    // Guard against Go zero-value time.Time (year 0001)
    if (d.getFullYear() < 2000) return ''
    const now = new Date()
    const diff = now.getTime() - d.getTime()
    const minutes = Math.floor(diff / 60000)
    const hours = Math.floor(diff / 3600000)
    const days = Math.floor(diff / 86400000)

    if (minutes < 1) return i18n.global.t('time.justNow')
    if (minutes < 60) return i18n.global.t('time.minutesAgo', { count: minutes })
    if (hours < 24) return i18n.global.t('time.hoursAgo', { count: hours })
    if (days < 7) return i18n.global.t('time.daysAgo', { count: days })
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

/** Format milliseconds as human-readable duration (e.g. "12.3s", "1m30s") */
export function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`
    const sec = ms / 1000
    if (sec < 60) return `${sec.toFixed(1)}s`
    const min = Math.floor(sec / 60)
    const rem = Math.round(sec % 60)
    return `${min}m${rem}s`
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
        .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1') // links
        .replace(/[#*`_~[\]()>|]/g, '')   // remaining syntax chars
        .replace(/\n+/g, ' ')             // newlines → space
        .trim()
    return clean.length > maxLen ? clean.substring(0, maxLen) + '...' : clean
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
