/**
 * Wire types for the built-in mail client.
 *
 * These mirror `internal/service/mail_store.go` field by field; the JSON tags
 * there are the contract. Keep them in sync when a field is added.
 */
import { formatClockTime, formatDayLabel, formatFullTimestamp } from '@/utils/format'

export type MailSecurity = 'tls' | 'starttls' | 'none'

export interface MailAccount {
  id: number
  name: string
  email: string
  username: string
  imapHost: string
  imapPort: number
  imapSecurity: MailSecurity
  smtpHost: string
  smtpPort: number
  smtpSecurity: MailSecurity
  insecureTls: boolean
  enabled: boolean
  sortOrder: number
  lastSyncAt: number
  lastError: string
  hasPassword: boolean
  createdAt: number
}

/** What the account form submits. `password` empty on edit means "keep it". */
export interface MailAccountInput {
  name: string
  email: string
  username: string
  password: string
  imapHost: string
  imapPort: number
  imapSecurity: MailSecurity
  smtpHost: string
  smtpPort: number
  smtpSecurity: MailSecurity
  insecureTls: boolean
  enabled: boolean
  sortOrder?: number
}

export interface MailPreset {
  id: string
  label: string
  domains?: string[]
  imapHost: string
  imapPort: number
  imapSecurity: MailSecurity
  smtpHost: string
  smtpPort: number
  smtpSecurity: MailSecurity
  hintKey?: string
}

export interface MailFolder {
  id: number
  accountId: number
  path: string
  name: string
  delimiter: string
  specialUse: string
  unread: number
  total: number
}

export interface MailAddress {
  name: string
  addr: string
}

export interface MailMessage {
  id: number
  accountId: number
  folderId: number
  folderPath?: string
  uid: number
  messageId: string
  subject: string
  fromName: string
  fromAddr: string
  to: MailAddress[]
  cc: MailAddress[]
  replyTo: string
  inReplyTo: string
  references: string
  sentAt: number
  size: number
  seen: boolean
  flagged: boolean
  answered: boolean
  hasAttachments: boolean
  snippet: string
  bodyFetched: boolean
}

export interface MailAttachment {
  id: number
  messageId: number
  filename: string
  contentType: string
  size: number
  contentId: string
  inline: boolean
}

export interface MailMessageDetail extends MailMessage {
  bodyText: string
  bodyHtml: string
  attachments: MailAttachment[]
}

export interface MailReplyContext {
  inReplyTo: string
  references: string
  to: string
  cc: string
  subject: string
  quote: string
}

export interface MailDraftFile {
  filename: string
  path: string
  size: number
}

export interface MailDraft {
  id: number
  accountId: number
  to: string
  cc: string
  bcc: string
  subject: string
  bodyText: string
  bodyHtml: string
  inReplyTo: string
  references: string
  attachments: MailDraftFile[]
  updatedAt: number
}

export interface MailSendRequest {
  accountId: number
  to: string[]
  cc: string[]
  bcc: string[]
  subject: string
  text: string
  html?: string
  inReplyTo?: string
  references?: string
  attachments: MailDraftFile[]
  draftId?: number
}

export interface MailUnreadSummary {
  accountId: number
  name: string
  email: string
  unread: number
}

/** The special-use values the folder bar knows how to label. */
export const MAIL_SPECIAL_FOLDERS = ['inbox', 'sent', 'drafts', 'trash', 'junk', 'archive'] as const

/**
 * mailErrorMessage turns an API failure into something a person can read.
 *
 * The server classifies failures (auth / connect / TLS / timeout / busy) and
 * returns a `msgKey`; showing the raw IMAP line instead would be noise. Unknown
 * keys fall back to the server's own message.
 */
export function mailErrorMessage(err: unknown, t: (key: string) => string): string {
  const keyMap: Record<string, string> = {
    MailAuthFailed: 'mail.err.auth',
    MailConnectFailed: 'mail.err.connect',
    MailTLSFailed: 'mail.err.tls',
    MailTimeout: 'mail.err.timeout',
    MailServerError: 'mail.err.server',
    MailSyncBusy: 'mail.err.busy',
    MailNoRecipients: 'mail.err.noRecipients',
  }
  const e = err as { msgKey?: string; message?: string } | null
  if (e?.msgKey && keyMap[e.msgKey]) return t(keyMap[e.msgKey])
  if (e?.message) return e.message
  return t('mail.err.generic')
}

/** Formats a message timestamp the way the list shows it. */
/**
 * Friendly day label for the message list: 今天 / 昨天 / 周三 / 11月3日.
 *
 * Shares the timeline's labels (`time.today` …) so the two places cannot drift,
 * and keeps the exact clock time beside it — a list that shows only "11月3日"
 * answers "which day" but not "when", which is what you need when hunting for
 * a specific message.
 */
export function mailDayLabel(unix: number): string {
  if (!unix) return ''
  return formatDayLabel(new Date(unix * 1000))
}

/** "14:21" — the exact clock time shown under the day label. */
export function mailClockTime(unix: number): string {
  if (!unix) return ''
  return formatClockTime(new Date(unix * 1000))
}

/** "2026-11-04 14:21:05" — the tooltip / reading-pane timestamp. */
export function mailFullTimestamp(unix: number): string {
  if (!unix) return ''
  return formatFullTimestamp(new Date(unix * 1000))
}

/** "张三" for a named sender, otherwise the bare address. */
export function mailSenderLabel(msg: Pick<MailMessage, 'fromName' | 'fromAddr'>): string {
  return msg.fromName || msg.fromAddr || '—'
}

/** Splits a comma/semicolon separated address field into clean addresses. */
export function parseAddressList(raw: string): string[] {
  return raw
    .split(/[,;]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

/** Human-readable size for attachments. */
export function formatMailSize(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}
