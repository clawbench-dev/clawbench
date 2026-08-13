/**
 * Shortcut tips shown in the PC AppHeader middle area.
 *
 * Data-driven: adding a new tip only requires appending an entry here (and
 * adding its context/action text to the i18n locales). No component changes.
 */

export interface ShortcutTipDef {
  /** i18n key → panel + precondition (e.g. "聊天页 · 输入框内"). */
  contextKey: string
  /** Highlighted key names (language-neutral). Optional. */
  keys?: string[]
  /** i18n key → action description / how to enable. */
  actionKey: string
}

export const SHORTCUT_TIPS: ShortcutTipDef[] = [
  {
    contextKey: 'appHeader.shortcutTip.contextSend',
    keys: ['Enter', 'Shift+Enter'],
    actionKey: 'appHeader.shortcutTip.actionSend',
  },
  {
    contextKey: 'appHeader.shortcutTip.contextSearch',
    keys: ['Ctrl+F'],
    actionKey: 'appHeader.shortcutTip.actionSearch',
  },
  {
    contextKey: 'appHeader.shortcutTip.contextSwitchSession',
    keys: ['Ctrl+←', 'Ctrl+→'],
    actionKey: 'appHeader.shortcutTip.actionSwitchSession',
  },
  {
    contextKey: 'appHeader.shortcutTip.contextJumpUnread',
    keys: ['Ctrl+U'],
    actionKey: 'appHeader.shortcutTip.actionJumpUnread',
  },
  {
    contextKey: 'appHeader.shortcutTip.contextOpenSessionList',
    keys: ['Ctrl+K'],
    actionKey: 'appHeader.shortcutTip.actionOpenSessionList',
  },
  {
    contextKey: 'appHeader.shortcutTip.contextRecommend',
    actionKey: 'appHeader.shortcutTip.actionRecommend',
  },
  {
    contextKey: 'appHeader.shortcutTip.contextRecommendEnable',
    actionKey: 'appHeader.shortcutTip.actionRecommendEnable',
  },
]
