import { describe, expect, it } from 'vitest'
import {
  MAIL_HUES,
  mailAttachmentKind,
  mailHue,
  mailInitial,
} from '../mailColors'

describe('mailHue', () => {
  it('keeps the same colour for the same address across calls and casing', () => {
    // The whole point of hashing instead of cycling by row index: the same
    // person must not change colour when the list is re-sorted or reloaded.
    const first = mailHue('Boss@Example.com')
    expect(mailHue('boss@example.com')).toBe(first)
    expect(mailHue('  BOSS@example.com  ')).toBe(first)
  })

  it('only ever returns one of the theme tokens', () => {
    for (const key of ['a@b.c', 'x@y.z', '942980741@qq.com', 'noreply@npmjs.com']) {
      expect(MAIL_HUES).toContain(mailHue(key) as (typeof MAIL_HUES)[number])
    }
  })

  it('spreads a realistic inbox over more than one hue', () => {
    // A palette that collapses to a single colour would look exactly like the
    // monochrome list this replaced. The low-bit clustering of a bare djb2
    // (`boss@` / `support@` / `noreply@` all landing together) is why the hash
    // carries a finalizer; this asserts the result, not the algorithm.
    const senders = [
      'noreply@npmjs.com',
      'boss@example.com',
      'github-actions@github.com',
      'support@qq.com',
      'team@vercel.com',
      'me@xulongzhe.com',
      'news@medium.com',
      'billing@stripe.com',
    ]
    expect(new Set(senders.map(mailHue)).size).toBeGreaterThanOrEqual(3)
  })

  it('falls back to the accent for an empty sender', () => {
    expect(mailHue('')).toBe(MAIL_HUES[MAIL_HUES.length - 1])
    expect(mailHue('   ')).toBe(MAIL_HUES[MAIL_HUES.length - 1])
  })
})

describe('mailInitial', () => {
  it('prefers the display name and upper-cases it', () => {
    expect(mailInitial('npm', 'support@npmjs.com')).toBe('N')
  })

  it('falls back to the address when there is no name', () => {
    expect(mailInitial('', 'boss@example.com')).toBe('B')
  })

  it('survives a name that starts with a surrogate pair', () => {
    expect(mailInitial('🙂 Team', 'x@y.z')).toBe('🙂')
  })

  it('answers "unknown" instead of an empty circle', () => {
    expect(mailInitial('', '')).toBe('?')
  })
})

describe('mailAttachmentKind', () => {
  it('classifies by extension', () => {
    expect(mailAttachmentKind('photo.JPG')).toBe('image')
    expect(mailAttachmentKind('report.pdf')).toBe('pdf')
    expect(mailAttachmentKind('bundle.tar.gz')).toBe('archive')
    expect(mailAttachmentKind('budget.xlsx')).toBe('sheet')
    expect(mailAttachmentKind('notes.md')).toBe('doc')
    expect(mailAttachmentKind('weird.bin')).toBe('other')
  })

  it('falls back to the content type when the name has no usable extension', () => {
    expect(mailAttachmentKind('inline-image', 'image/png')).toBe('image')
    expect(mailAttachmentKind('scan', 'application/pdf')).toBe('pdf')
    expect(mailAttachmentKind('readme', 'text/plain')).toBe('doc')
  })

  it('does not let octet-stream hide a known extension', () => {
    // Outlook and QQ mail declare almost everything as octet-stream; trusting
    // the content type first would paint every attachment the same grey.
    expect(mailAttachmentKind('report.pdf', 'application/octet-stream')).toBe('pdf')
  })

  it('treats a dotless name as other', () => {
    expect(mailAttachmentKind('Makefile')).toBe('other')
    expect(mailAttachmentKind('')).toBe('other')
  })
})
