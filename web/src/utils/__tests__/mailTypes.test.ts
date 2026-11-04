import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import {
    formatMailSize,
    mailClockTime,
    mailDayLabel,
    mailErrorMessage,
    mailFullTimestamp,
    mailSenderLabel,
    parseAddressList,
} from '../mailTypes'

describe('mailErrorMessage', () => {
    const t = (key: string) => `t:${key}`

    it('translates a known server classification', () => {
        expect(mailErrorMessage({ msgKey: 'MailAuthFailed' }, t)).toBe('t:mail.err.auth')
        expect(mailErrorMessage({ msgKey: 'MailSyncBusy' }, t)).toBe('t:mail.err.busy')
    })

    it('falls back to the server message for an unknown key', () => {
        // A key this build does not know about must not render "t:undefined".
        expect(mailErrorMessage({ msgKey: 'MailBrandNew', message: 'boom' }, t)).toBe('boom')
    })

    it('falls back to the generic text when nothing usable is attached', () => {
        expect(mailErrorMessage({}, t)).toBe('t:mail.err.generic')
        expect(mailErrorMessage(null, t)).toBe('t:mail.err.generic')
        expect(mailErrorMessage(new Error(''), t)).toBe('t:mail.err.generic')
    })

    it('prefers a translation over a raw message when the key is known', () => {
        expect(mailErrorMessage({ msgKey: 'MailTimeout', message: 'i/o timeout' }, t)).toBe('t:mail.err.timeout')
    })
})

describe('mailSenderLabel', () => {
    it('uses the display name when there is one', () => {
        expect(mailSenderLabel({ fromName: '张三', fromAddr: 'z@example.com' })).toBe('张三')
    })

    it('falls back to the bare address', () => {
        expect(mailSenderLabel({ fromName: '', fromAddr: 'z@example.com' })).toBe('z@example.com')
    })

    it('uses an em dash when both are empty', () => {
        expect(mailSenderLabel({ fromName: '', fromAddr: '' })).toBe('—')
    })
})

describe('parseAddressList', () => {
    it('splits on commas and semicolons and trims each entry', () => {
        expect(parseAddressList(' a@x.com , b@x.com;c@x.com ')).toEqual(['a@x.com', 'b@x.com', 'c@x.com'])
    })

    it('drops empty entries', () => {
        expect(parseAddressList('a@x.com,,  ,')).toEqual(['a@x.com'])
        expect(parseAddressList('')).toEqual([])
    })
})

describe('formatMailSize', () => {
    it('reports zero and sub-kilobyte sizes in bytes', () => {
        expect(formatMailSize(0)).toBe('0 B')
        expect(formatMailSize(512)).toBe('512 B')
    })

    it('keeps one decimal below 10 units and rounds above', () => {
        expect(formatMailSize(1536)).toBe('1.5 KB')
        expect(formatMailSize(20 * 1024)).toBe('20 KB')
    })

    it('climbs to the largest fitting unit', () => {
        expect(formatMailSize(5 * 1024 * 1024)).toBe('5.0 MB')
        expect(formatMailSize(3 * 1024 * 1024 * 1024)).toBe('3.0 GB')
    })
})

describe('mail timestamp helpers', () => {
    beforeEach(() => {
        // Fixed clock so "today" is stable regardless of when the suite runs.
        vi.useFakeTimers()
        vi.setSystemTime(new Date(2026, 10, 4, 15, 30, 0))
    })

    afterEach(() => {
        vi.useRealTimers()
    })

    const unix = (d: Date) => Math.floor(d.getTime() / 1000)

    it('returns empty strings for a missing timestamp', () => {
        expect(mailDayLabel(0)).toBe('')
        expect(mailClockTime(0)).toBe('')
        expect(mailFullTimestamp(0)).toBe('')
    })

    it('labels today and yesterday relatively', () => {
        // Compares against the shared timeline labels: the mail list must not
        // grow a second, drifting copy of "today".
        const today = i18n.global.t('time.today')
        const yesterday = i18n.global.t('time.yesterday')
        expect(mailDayLabel(unix(new Date(2026, 10, 4, 9, 0, 0)))).toBe(today)
        expect(mailDayLabel(unix(new Date(2026, 10, 3, 23, 0, 0)))).toBe(yesterday)
    })

    it('falls back to a month/day label beyond a week', () => {
        expect(mailDayLabel(unix(new Date(2026, 9, 12, 8, 0, 0)))).toBe(
            i18n.global.t('time.monthDay', { month: 10, day: 12 }),
        )
    })

    it('shows the exact clock time beside the day label', () => {
        // The whole point of splitting them: the day says which, the clock says when.
        expect(mailClockTime(unix(new Date(2026, 10, 4, 14, 21, 5)))).toBe('14:21')
    })

    it('formats a full timestamp for the tooltip', () => {
        expect(mailFullTimestamp(unix(new Date(2026, 10, 4, 14, 21, 5)))).toBe('2026-11-04 14:21:05')
    })
})
