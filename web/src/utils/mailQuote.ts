/**
 * Turning an email into a quote — the ONE builder for both ways an email gets
 * quoted: the reader's "quote in chat" button, and dropping a row on the chat
 * column.
 *
 * Kept out of the components because the two live far apart (MailReader and
 * App.vue's drop handler) and a second copy is how one of them ends up without
 * the locator or with a different label.
 */
import { quoteItemFromTarget, type QuoteItem } from '@/utils/quoteItem'
import { mailSenderLabel, type MailMessageDetail } from '@/utils/mailTypes'

/** Longest body inlined into a quote. A mail body can be an entire thread. */
export const MAIL_QUOTE_TEXT_MAX = 6000

/**
 * The card label: `主题 · 发件人`.
 *
 * Subject alone does not identify a mail (threads repeat it), and the sender is
 * what the user recognises in a list. `noSubject` is passed in rather than read
 * from i18n here so this module stays a pure utility.
 */
export function mailQuoteLabel(
  detail: Pick<MailMessageDetail, 'subject' | 'fromName' | 'fromAddr'>,
  noSubject: string,
): string {
  const subject = (detail.subject || '').trim() || noSubject
  const sender = mailSenderLabel(detail).trim()
  return sender ? `${subject} · ${sender}` : subject
}

/**
 * Build the quote for one email.
 *
 * The BODY is inlined: `mailId` is a locator for the client's "jump to source",
 * and the AI has no tool that can read a mailbox — a label alone tells it
 * nothing it can act on. Falls back to the list snippet when the message has no
 * plain-text body (HTML-only mail), and clips very long threads.
 */
export function buildMailQuote(detail: MailMessageDetail, noSubject: string): QuoteItem {
  const body = (detail.bodyText || detail.snippet || '').trim()
  const text = body.length > MAIL_QUOTE_TEXT_MAX ? `${body.slice(0, MAIL_QUOTE_TEXT_MAX)}…` : body
  return quoteItemFromTarget({
    filePath: mailQuoteLabel(detail, noSubject),
    language: 'mail',
    sourceKind: 'mail',
    text,
    mailId: detail.id,
  })
}
