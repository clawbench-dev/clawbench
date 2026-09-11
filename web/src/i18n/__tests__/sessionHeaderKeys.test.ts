import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Keys used by the session-list header action buttons (search / create /
 * refresh / pin). A missing key renders the raw key string as the tooltip
 * (e.g. "session.refresh"), so lock the set down for both locales.
 */
const HEADER_ACTION_KEYS = ['title', 'newSession', 'refresh', 'pinToSidebar', 'unpinToSidebar', 'closeSidebar'] as const

describe('i18n session header action keys', () => {
  it('en defines every session header action key', () => {
    for (const key of HEADER_ACTION_KEYS) {
      expect((en.session as Record<string, string>)[key], `en.session.${key}`).toBeTruthy()
    }
  })

  it('zh defines every session header action key', () => {
    for (const key of HEADER_ACTION_KEYS) {
      expect((zh.session as Record<string, string>)[key], `zh.session.${key}`).toBeTruthy()
    }
  })
})
