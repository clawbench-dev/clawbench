import { describe, expect, it } from 'vitest'
import { SHORTCUT_TIPS, SHORTCUT_CONTEXT_ORDER, getShortcutTipsForContext, getAllShortcutTips, resolveShortcutContext } from '@/config/shortcutTips'
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

  // Regression guard: these file-manager / terminal shortcuts were implemented
  // in the components (FileManagerContent.handleKeydown, TerminalHelpDrawer)
  // long before they were listed here, so a rename or deletion of any of these
  // entries silently re-introduces the "implemented but undiscoverable" gap.
  it('lists every implemented file-manager keyboard shortcut', () => {
    const byLeaf = (leaf: string) => SHORTCUT_TIPS.find(t => t.contextKey.endsWith(`.${leaf}`))
    expect(byLeaf('contextBrowseNavigate')?.keys).toEqual(['↑', '↓', 'Home', 'End'])
    expect(byLeaf('contextBrowseOpen')?.keys).toEqual(['Enter'])
    expect(byLeaf('contextBrowseToggleSelect')?.keys).toEqual(['Space'])
    expect(byLeaf('contextBrowseEscape')?.keys).toEqual(['Esc'])
  })

  it('lists the terminal Ctrl+Z suspend shortcut alongside Ctrl+C/D/L', () => {
    const suspend = SHORTCUT_TIPS.find(t => t.contextKey.endsWith('.contextTermSuspend'))
    expect(suspend).toBeDefined()
    expect(suspend?.keys).toEqual(['Ctrl+Z'])
    const terminalKeys = SHORTCUT_TIPS
      .filter(t => t.context === 'terminal')
      .flatMap(t => t.keys ?? [])
    expect(terminalKeys).toEqual(expect.arrayContaining(['Ctrl+C', 'Ctrl+D', 'Ctrl+L', 'Ctrl+Z']))
  })

  // Regression guard: copy-on-select and the copy chords were implemented in
  // TerminalPanelContent.vue / TerminalHelpDrawer.vue before they were listed
  // here, so a rename or deletion silently re-introduces the "implemented but
  // undiscoverable" gap this dialog exists to close.
  it('lists every terminal copy path', () => {
    const byLeaf = (leaf: string) => SHORTCUT_TIPS.find(t => t.contextKey.endsWith(`.${leaf}`))
    expect(byLeaf('contextTermCopy')?.keys).toEqual(['Ctrl+C', 'Ctrl+Shift+C'])
    expect(byLeaf('contextTermCopyInsert')?.keys).toEqual(['Ctrl+Insert'])
    expect(byLeaf('contextTermZoom')?.keys).toEqual(['Ctrl+Wheel'])
    // Copy-on-select and right-click have no key chord — they are gestures, so
    // they carry a description instead. Assert the ROW exists (not just that
    // its keys are undefined, which a missing row would also satisfy): these
    // are the two paths users cannot discover by reading a key table, so a
    // silently dropped row is exactly the regression to catch.
    for (const leaf of ['contextTermCopyOnSelect', 'contextTermRightClick']) {
      const tip = byLeaf(leaf)
      expect(tip, `${leaf} row missing`).toBeDefined()
      expect(tip?.keys).toBeUndefined()
      expect(tip?.actionKey.endsWith(leaf.replace('context', 'action'))).toBe(true)
    }
  })

  // Regression guard for the desktop page-zoom chords: they are claimed in the
  // Electron main process (desktop/src/main/shortcuts.ts), so nothing in the
  // web layer would fail if this row were dropped — the feature would just
  // become undiscoverable.
  it('lists the desktop page-zoom chords', () => {
    const zoom = SHORTCUT_TIPS.find(t => t.contextKey.endsWith('.contextPageZoom'))
    expect(zoom, 'contextPageZoom row missing').toBeDefined()
    expect(zoom?.context).toBe('common')
    expect(zoom?.keys).toEqual(['Ctrl+=', 'Ctrl+-', 'Ctrl+0'])
    expect(zoom?.actionKey.endsWith('.actionPageZoom')).toBe(true)
  })

  it('keeps the interrupt tip scoped to "no selection" now that Ctrl+C also copies', () => {
    // Ctrl+C is overloaded: it interrupts without a selection and copies with
    // one. Both rows exist, so each must state its precondition or the table
    // reads as a contradiction.
    const interrupt = SHORTCUT_TIPS.find(t => t.contextKey.endsWith('.contextTermInterrupt'))
    const copy = SHORTCUT_TIPS.find(t => t.contextKey.endsWith('.contextTermCopy'))
    expect(interrupt?.contextKey).not.toBe(copy?.contextKey)
    expect(interrupt?.actionKey).not.toBe(copy?.actionKey)
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

  it('resolveShortcutContext: narrow uses activeTab', () => {
    expect(resolveShortcutContext({ isWideScreen: false, activePane: 'left', leftTab: 'browse', activeTab: 'terminal' })).toBe('terminal')
  })

  it('resolveShortcutContext: wide + right pane is chat', () => {
    expect(resolveShortcutContext({ isWideScreen: true, activePane: 'right', leftTab: 'browse', activeTab: 'chat' })).toBe('chat')
  })

  it('resolveShortcutContext: wide + left pane uses leftTab', () => {
    expect(resolveShortcutContext({ isWideScreen: true, activePane: 'left', leftTab: 'history', activeTab: 'chat' })).toBe('history')
  })
})
