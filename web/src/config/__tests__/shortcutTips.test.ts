import { describe, expect, it } from 'vitest'
import { SHORTCUT_TIPS, SHORTCUT_CONTEXT_ORDER, getShortcutTipsForContext, getAllShortcutTips } from '@/config/shortcutTips'
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

  it('getShortcutTipsForContext always includes common and chat tips', () => {
    for (const ctx of SHORTCUT_CONTEXT_ORDER) {
      const result = getShortcutTipsForContext(ctx)
      expect(result.some(t => t.context === 'common')).toBe(true)
      expect(result.some(t => t.context === 'chat')).toBe(true)
    }
  })

  it('getShortcutTipsForContext includes the context tips and nothing else', () => {
    const result = getShortcutTipsForContext('browse')
    const contexts = new Set(result.map(t => t.context))
    expect(contexts.has('browse')).toBe(true)
    for (const ctx of contexts) {
      expect(['common', 'chat', 'browse']).toContain(ctx)
    }
  })

  it('getShortcutTipsForContext chat does not duplicate chat tips', () => {
    const result = getShortcutTipsForContext('chat')
    const contexts = new Set(result.map(t => t.context))
    expect(contexts.has('chat')).toBe(true)
    for (const ctx of contexts) {
      expect(['common', 'chat']).toContain(ctx)
    }
  })

  it('getAllShortcutTips is ordered by SHORTCUT_CONTEXT_ORDER and has no duplicates', () => {
    const all = getAllShortcutTips()
    const seen = new Set<string>()
    const orderIndex = new Map(SHORTCUT_CONTEXT_ORDER.map((c, i) => [c, i]))
    let prevIdx = -1
    for (const tip of all) {
      expect(seen.has(tip.contextKey)).toBe(false)
      seen.add(tip.contextKey)
      const idx = orderIndex.get(tip.context) ?? -1
      expect(idx).toBeGreaterThanOrEqual(prevIdx)
      prevIdx = idx
    }
  })
})
