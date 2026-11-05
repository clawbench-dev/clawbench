import { describe, expect, it } from 'vitest'
import { buildMailQuote, mailQuoteLabel, MAIL_QUOTE_TEXT_MAX } from '@/utils/mailQuote'
import { resolveQuoteType } from '@/utils/quoteItem'
import type { MailMessageDetail } from '@/utils/mailTypes'

function detail(over: Partial<MailMessageDetail> = {}): MailMessageDetail {
  return {
    id: 77,
    accountId: 1,
    folderId: 1,
    uid: 9,
    messageId: 'm77@example.com',
    subject: '季度复盘',
    fromName: '张三',
    fromAddr: 'zhang@example.com',
    to: [],
    cc: [],
    replyTo: '',
    inReplyTo: '',
    references: '',
    sentAt: 0,
    size: 1024,
    seen: true,
    flagged: false,
    answered: false,
    hasAttachments: false,
    snippet: '',
    bodyFetched: true,
    bodyText: '本季度收入同比增长 12%',
    bodyHtml: '',
    attachments: [],
    ...over,
  }
}

describe('mailQuoteLabel', () => {
  it('is subject · sender', () => {
    expect(mailQuoteLabel(detail(), '（无主题）')).toBe('季度复盘 · 张三')
  })

  it('falls back to the address when the sender has no name', () => {
    expect(mailQuoteLabel(detail({ fromName: '' }), '（无主题）')).toBe('季度复盘 · zhang@example.com')
  })

  it('falls back to the no-subject label, never rendering a blank card', () => {
    expect(mailQuoteLabel(detail({ subject: '' }), '（无主题）')).toBe('（无主题） · 张三')
  })
})

describe('buildMailQuote', () => {
  it('inlines the body and carries the mail locator', () => {
    const q = buildMailQuote(detail(), '（无主题）')

    // The AI cannot read a mailbox from an id, so the body must travel with it.
    expect(q.text).toBe('本季度收入同比增长 12%')
    expect(q.mailId).toBe(77)
    expect(q.filePath).toBe('季度复盘 · 张三')
    expect(resolveQuoteType(q)).toBe('mail')
  })

  it('falls back to the list snippet when there is no plain-text body', () => {
    // HTML-only mail: bodyText is empty, and a card with no text tells the AI
    // nothing at all.
    const q = buildMailQuote(detail({ bodyText: '', snippet: '仅 HTML 的正文…' }), '（无主题）')
    expect(q.text).toBe('仅 HTML 的正文…')
  })

  it('clips a very long thread', () => {
    const q = buildMailQuote(detail({ bodyText: 'x'.repeat(MAIL_QUOTE_TEXT_MAX + 50) }), '（无主题）')
    expect(q.text.length).toBe(MAIL_QUOTE_TEXT_MAX + 1) // + the ellipsis
    expect(q.text.endsWith('…')).toBe(true)
  })
})
