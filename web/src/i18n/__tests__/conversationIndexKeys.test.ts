import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Ensures the conversation-index (user message index drawer) keys exist in
 * both en and zh locales, so the drawer's search box / empty states always
 * render translated text rather than silently falling back to raw keys.
 */
describe('i18n conversation-index keys completeness', () => {
  const keys = [
    'conversationIndexTitle',
    'conversationIndexDesc',
    'conversationIndexSearch',
    'conversationIndexNoResults',
    'noUserMessages',
    'noUserMessagesHint',
    'userMsgIndexAttachment',
  ]

  it('en and zh have all conversation-index keys', () => {
    const enMissing = keys.filter(k => !(en.chat?.messageList as Record<string, string>)?.[k])
    const zhMissing = keys.filter(k => !(zh.chat?.messageList as Record<string, string>)?.[k])
    expect(enMissing, 'keys missing in en').toEqual([])
    expect(zhMissing, 'keys missing in zh').toEqual([])
  })

  it('conversation-index values are non-empty', () => {
    const messageLists: Array<[string, Record<string, string>]> = [
      ['en', en.chat?.messageList as Record<string, string>],
      ['zh', zh.chat?.messageList as Record<string, string>],
    ]
    for (const [locale, list] of messageLists) {
      for (const key of keys) {
        expect(list?.[key], `${locale}.chat.messageList.${key} should not be empty`).not.toBe('')
      }
    }
  })
})
