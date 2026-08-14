/**
 * Shortcut tips shown in the PC AppHeader middle area.
 *
 * Data-driven: adding a new tip only requires appending an entry here (and
 * adding its context/action text to the i18n locales). No component changes.
 */

export type ShortcutContext =
  | 'common'   // 公共/全局，任何 tab 都显示
  | 'chat'     // 常驻聊天（PC 模式聊天面板常驻，任何 tab 都显示）
  | 'browse'   // 文件管理器
  | 'view'     // 文件查看/编辑
  | 'terminal' // 终端
  | 'history'  // Git 历史
  | 'tasks'    // 任务（当前无内容）
  | 'settings' // 设置
  | 'proxy'    // 端口转发

export interface ShortcutTipDef {
  /** 所属分组（决定该提示在哪些上下文显示）。 */
  context: ShortcutContext
  /** i18n key → panel + precondition (e.g. "聊天页 · 输入框内"). */
  contextKey: string
  /** Highlighted key names (language-neutral). Optional. */
  keys?: string[]
  /** i18n key → action description / how to enable. */
  actionKey: string
}

/** 分组展示顺序（对话框表格顺序）。 */
export const SHORTCUT_CONTEXT_ORDER: ShortcutContext[] = [
  'common', 'chat', 'browse', 'view', 'terminal', 'history', 'settings', 'proxy', 'tasks',
]

export const SHORTCUT_TIPS: ShortcutTipDef[] = [
  // ── common（任何 tab） ──
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextSearch', keys: ['Ctrl+F'], actionKey: 'appHeader.shortcutTip.actionSearch' },
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextCloseOverlay', keys: ['Esc'], actionKey: 'appHeader.shortcutTip.actionCloseOverlay' },
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextConfirmDialog', keys: ['Enter'], actionKey: 'appHeader.shortcutTip.actionConfirmDialog' },
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextListNav', keys: ['↑', '↓', 'Enter'], actionKey: 'appHeader.shortcutTip.actionListNav' },

  // ── chat（常驻，任何 tab 都显示） ──
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextSend', keys: ['Enter', 'Shift+Enter'], actionKey: 'appHeader.shortcutTip.actionSend' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextSwitchSession', keys: ['Ctrl+←', 'Ctrl+→'], actionKey: 'appHeader.shortcutTip.actionSwitchSession' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextJumpUnread', keys: ['Ctrl+U'], actionKey: 'appHeader.shortcutTip.actionJumpUnread' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextOpenSessionList', keys: ['Ctrl+K'], actionKey: 'appHeader.shortcutTip.actionOpenSessionList' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextArchiveSession', keys: ['Ctrl+Delete'], actionKey: 'appHeader.shortcutTip.actionArchiveSession' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextJumpMessage', keys: ['Ctrl+↑', 'Ctrl+↓'], actionKey: 'appHeader.shortcutTip.actionJumpMessage' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextVoice', keys: ['F9'], actionKey: 'appHeader.shortcutTip.actionVoice' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextRecommend', actionKey: 'appHeader.shortcutTip.actionRecommend' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextRecommendEnable', actionKey: 'appHeader.shortcutTip.actionRecommendEnable' },

  // ── browse（文件管理器） ──
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseClipboard', keys: ['Ctrl+C', 'Ctrl+X', 'Ctrl+V'], actionKey: 'appHeader.shortcutTip.actionBrowseClipboard' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseDelete', keys: ['Delete', 'Shift+Delete'], actionKey: 'appHeader.shortcutTip.actionBrowseDelete' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseNew', keys: ['Ctrl+N', 'Ctrl+Shift+N'], actionKey: 'appHeader.shortcutTip.actionBrowseNew' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseRename', keys: ['F2'], actionKey: 'appHeader.shortcutTip.actionBrowseRename' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseParent', keys: ['Alt+↑', 'Backspace'], actionKey: 'appHeader.shortcutTip.actionBrowseParent' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseRefresh', keys: ['Ctrl+R', 'F5'], actionKey: 'appHeader.shortcutTip.actionBrowseRefresh' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseHidden', keys: ['Ctrl+Shift+H'], actionKey: 'appHeader.shortcutTip.actionBrowseHidden' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseMulti', keys: ['Ctrl+Shift+M', 'Ctrl+A'], actionKey: 'appHeader.shortcutTip.actionBrowseMulti' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseView', keys: ['Ctrl+1', 'Ctrl+2'], actionKey: 'appHeader.shortcutTip.actionBrowseView' },

  // ── view（文件查看/编辑） ──
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewSave', keys: ['Ctrl+S'], actionKey: 'appHeader.shortcutTip.actionViewSave' },
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewUndo', keys: ['Ctrl+Z', 'Ctrl+Y'], actionKey: 'appHeader.shortcutTip.actionViewUndo' },
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewImage', keys: ['←', '→'], actionKey: 'appHeader.shortcutTip.actionViewImage' },
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewZoom', keys: ['Ctrl+Wheel'], actionKey: 'appHeader.shortcutTip.actionViewZoom' },

  // ── terminal ──
  { context: 'terminal', contextKey: 'appHeader.shortcutTip.contextTermInterrupt', keys: ['Ctrl+C'], actionKey: 'appHeader.shortcutTip.actionTermInterrupt' },
  { context: 'terminal', contextKey: 'appHeader.shortcutTip.contextTermEof', keys: ['Ctrl+D'], actionKey: 'appHeader.shortcutTip.actionTermEof' },
  { context: 'terminal', contextKey: 'appHeader.shortcutTip.contextTermClear', keys: ['Ctrl+L'], actionKey: 'appHeader.shortcutTip.actionTermClear' },

  // ── history（Git 历史） ──
  { context: 'history', contextKey: 'appHeader.shortcutTip.contextHistoryNav', keys: ['↑', '↓', 'Enter'], actionKey: 'appHeader.shortcutTip.actionHistoryNav' },

  // ── settings ──
  { context: 'settings', contextKey: 'appHeader.shortcutTip.contextSettingsEdit', keys: ['Enter'], actionKey: 'appHeader.shortcutTip.actionSettingsEdit' },

  // ── proxy（端口转发） ──
  { context: 'proxy', contextKey: 'appHeader.shortcutTip.contextProxySave', keys: ['Enter'], actionKey: 'appHeader.shortcutTip.actionProxySave' },
]

/** 轮播列表：common 恒包含 + chat 常驻恒包含 + 指定 context 自身的提示。 */
export function getShortcutTipsForContext(ctx: ShortcutContext): ShortcutTipDef[] {
  return SHORTCUT_TIPS.filter(tip => tip.context === ctx || tip.context === 'common' || tip.context === 'chat')
}

/** 对话框全部提示，按 SHORTCUT_CONTEXT_ORDER 排序。 */
export function getAllShortcutTips(): ShortcutTipDef[] {
  return SHORTCUT_CONTEXT_ORDER.flatMap(ctx => SHORTCUT_TIPS.filter(tip => tip.context === ctx))
}
