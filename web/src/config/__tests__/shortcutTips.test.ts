import { describe, expect, it } from 'vitest'
import { SHORTCUT_TIPS } from '@/config/shortcutTips'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

function resolvePath(obj: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, part) => {
    if (acc && typeof acc === 'object' && part in (acc as Record<string, unknown>)) {
      return (acc as Record<string, unknown>)[part]
    }
    return undefined
  }, obj)
}

describe('SHORTCUT_TIPS', () => {
  it('every tip has contextKey and actionKey translations in both locales', () => {
    for (const tip of SHORTCUT_TIPS) {
      expect(resolvePath(zh, tip.contextKey), `zh contextKey ${tip.contextKey}`).toBeTruthy()
      expect(resolvePath(zh, tip.actionKey), `zh actionKey ${tip.actionKey}`).toBeTruthy()
      expect(resolvePath(en, tip.contextKey), `en contextKey ${tip.contextKey}`).toBeTruthy()
      expect(resolvePath(en, tip.actionKey), `en actionKey ${tip.actionKey}`).toBeTruthy()
    }
  })

  it('includes the jump-to-unread shortcut with the Ctrl+U key', () => {
    const jumpUnread = SHORTCUT_TIPS.find(tip => tip.contextKey.endsWith('.contextJumpUnread'))
    expect(jumpUnread).toBeDefined()
    expect(jumpUnread?.keys).toContain('Ctrl+U')
  })

  it('includes the open-session-list shortcut with the Ctrl+K key', () => {
    const openList = SHORTCUT_TIPS.find(tip => tip.contextKey.endsWith('.contextOpenSessionList'))
    expect(openList).toBeDefined()
    expect(openList?.keys).toContain('Ctrl+K')
  })
})
