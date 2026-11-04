import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import MailListItem from '../MailListItem.vue'
import type { MailMessage } from '@/utils/mailTypes'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function message(sentAt: number): MailMessage {
  return {
    id: 1,
    accountId: 1,
    folderId: 1,
    uid: 1,
    messageId: 'x@y',
    subject: '周报',
    fromName: '老板',
    fromAddr: 'boss@corp.com',
    to: ['me@corp.com'],
    cc: [],
    replyTo: [],
    inReplyTo: '',
    references: [],
    sentAt,
    size: 1024,
    seen: false,
    flagged: false,
    answered: false,
    hasAttachments: false,
    snippet: '本周进展',
    bodyFetched: true,
  } as MailMessage
}

function mountRow(sentAt: number) {
  return mount(MailListItem, {
    props: { msg: message(sentAt) },
    global: { plugins: [i18n] },
  })
}

describe('MailListItem time', () => {
  it('shows the friendly day and the exact clock side by side', () => {
    const now = Math.floor(Date.now() / 1000)
    const wrapper = mountRow(now)

    const day = wrapper.find('.mail-row__day')
    const clock = wrapper.find('.mail-row__clock')
    expect(day.exists()).toBe(true)
    expect(clock.exists()).toBe(true)
    // Both are rendered at once — showing only one answers half the question.
    expect(day.text().trim().length).toBeGreaterThan(0)
    expect(clock.text()).toMatch(/^\d{2}:\d{2}$/)
  })

  it('keeps an older message readable: date label plus its clock time', () => {
    const threeDaysAgo = Math.floor(Date.now() / 1000) - 3 * 86400
    const wrapper = mountRow(threeDaysAgo)

    expect(wrapper.find('.mail-row__day').text().trim().length).toBeGreaterThan(0)
    expect(wrapper.find('.mail-row__clock').text()).toMatch(/^\d{2}:\d{2}$/)
  })

  it('carries the full timestamp as a tooltip', () => {
    const now = Math.floor(Date.now() / 1000)
    const wrapper = mountRow(now)
    const title = wrapper.find('.mail-row__time').attributes('title') || ''
    expect(title).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
  })
})
