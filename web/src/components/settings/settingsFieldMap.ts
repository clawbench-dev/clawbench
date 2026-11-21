/**
 * Centralized settings item definitions — the single source of truth.
 *
 * Used by:
 * - SettingsCategory.vue (renders the UI)
 * - SettingsRestartDialog.vue (translates changed_cold_fields via serverFieldToLabelKey)
 *
 * Adding a new setting? Add it here.
 * Both the category page and the restart dialog will pick it up automatically.
 */

import { getNative } from '@/utils/clawbenchNative'
import { UI_SCALE_STEP, UI_SCALE_MANUAL_MIN, UI_SCALE_MANUAL_MAX } from '@/utils/uiScale'
import { buildFontStack, DEFAULT_MONO_STACK, DEFAULT_UI_STACK, MONO_FONT_CHOICES, UI_FONT_CHOICES, MONO_FALLBACK_CHOICES, type FontChoice } from '@/utils/fontConfig'

/** Raw (untranslated) font option descriptor used by the settings pickers. */
export interface FontOptionRaw {
  labelKey: string
  value: string
  groupKey: string
  badgeKey?: string
  previewFont?: string
}

/** Shared core: map a candidate list to raw options with groups/badges/previews. */
function toFontOptions(candidates: FontChoice[], defaultStack: string): FontOptionRaw[] {
  return candidates.map(f => {
    const opt: FontOptionRaw = {
      labelKey: `settings.items.fonts.${f.id}`,
      value: f.id,
      groupKey: `settings.items.fontsGroup.${f.kind}`,
      previewFont: f.id === 'default' ? undefined : buildFontStack(f.id, defaultStack),
    }
    return f.kind === 'bundled' ? { ...opt, badgeKey: 'settings.items.fontsBadge.bundled' } : opt
  })
}

/**
 * Build the raw select options for the primary mono/ui font picker, carrying
 * group + bundled badge + a previewFont CSS stack so the option label renders
 * in its own font. 'default' has no previewFont (uses the ambient UI font).
 */
export function buildFontFamilyOptions(isMono: boolean): FontOptionRaw[] {
  const candidates = isMono ? MONO_FONT_CHOICES : UI_FONT_CHOICES
  const stack = isMono ? DEFAULT_MONO_STACK : DEFAULT_UI_STACK
  return toFontOptions(candidates, stack)
}

/** Build options for the CODE-FONT fallback picker (adds CJK system fonts so
 *  Chinese comments remain readable when the primary code font is Latin-only). */
export function buildMonoFallbackOptions(): FontOptionRaw[] {
  return toFontOptions(MONO_FALLBACK_CHOICES, DEFAULT_MONO_STACK)
}

export interface DependsOn {
  key: string
  value?: unknown
  values?: unknown[]
}

/** Check a single DependsOn condition against a value resolver */
export function isSingleDependsOnMet(
  dep: DependsOn,
  getValue: (key: string) => unknown,
): boolean {
  const currentValue = getValue(dep.key)
  if ('value' in dep) return currentValue === dep.value
  return dep.values!.includes(currentValue as unknown)
}

/** Check dependsOn (single or array, OR logic) against a value resolver */
export function isDependsOnMet(
  dependsOn: DependsOn | DependsOn[] | undefined,
  getValue: (key: string) => unknown,
): boolean {
  if (!dependsOn) return true
  if (Array.isArray(dependsOn)) return dependsOn.some(d => isSingleDependsOnMet(d, getValue))
  return isSingleDependsOnMet(dependsOn, getValue)
}

/**
 * Render a stored usage-meter anchor for display.
 *
 * The canonical value is "YYYY-MM-DD" or "YYYY-MM-DDTHH:MM:SS". The raw form
 * leaks the ISO "T" separator into a settings row, which reads as machine
 * output; a space reads as a timestamp. Midnight is already stored date-only,
 * so no special case is needed here.
 */
export function formatAnchorForDisplay(value: unknown): string {
  const raw = String(value ?? '').trim()
  if (!raw) return ''
  const sep = raw.indexOf('T')
  if (sep < 0) return raw
  return `${raw.slice(0, sep)} ${raw.slice(sep + 1)}`
}

/** Sentinel option value for "type it yourself" in a model select. */
export const CUSTOM_OPTION_VALUE = '__custom__'

export interface ItemSpec {
  labelKey: string
  descriptionKey?: string
  key: string
  type: 'switch' | 'select' | 'multi-select' | 'number' | 'text' | 'date' | 'datetime' | 'slider' | 'action' | 'info' | 'header' | 'password' | 'textarea' | 'summaryModel'
  source: 'server' | 'local'
  needsRestart?: boolean
  options?: { labelKey: string; value: unknown; modelName?: string; groupKey?: string; badgeKey?: string }[]
  /** multi-select only: a "select all" row at the top of the picker, also used
   *  as the row's value when nothing is selected. Required when an empty
   *  selection is a meaningful value (e.g. "every agent") rather than "none". */
  allOptionKey?: string
  min?: number
  max?: number
  step?: number
  /** Hide this field when the condition is NOT met. */
  dependsOn?: DependsOn | DependsOn[]
  /** Gray out (disable) this field when the condition is NOT met, but keep it visible. */
  disableUnless?: DependsOn | DependsOn[]
  sectionHeader?: string
  /** Hard input limit for text/textarea editors (mirrors the server-side cap). */
  maxLength?: number
  /** Transform raw value for display (e.g., 0 → 'auto' for port_forward.port) */
  displayTransform?: (value: unknown) => unknown
  defaultValue?: unknown
  displayFormat?: 'percent' | 'raw'
  /** Only show this item when running inside the Android app */
  appOnly?: boolean
  /**
   * Hide this item everywhere EXCEPT the Android WebView shell (i.e. show it
   * only on Android).
   *
   * Distinct from `appOnly`, which cannot express this: `appOnly` is filtered
   * as `!isAppMode`, and BOTH native hosts report isAppMode() === true, so an
   * `appOnly` row renders in the Electron desktop shell too. Use `androidOnly`
   * for rows whose feature is genuinely Android-only (its native bridge method
   * exists solely on Android, or its copy names Android-only permissions) —
   * otherwise desktop users get a switch that silently does nothing.
   */
  androidOnly?: boolean
  /**
   * Hide this item inside the Android WebView shell (but keep it in the plain
   * browser and the Electron desktop shell).
   *
   * Distinct from `appOnly`, which cannot express this: BOTH native hosts
   * report isAppMode() === true, so "app only" would hide the row on desktop
   * too. Used by features whose semantics do not apply to the mobile layout.
   */
  hideInAndroidApp?: boolean
  /** For action items: navigate to this category sub-route ID on click */
  navigateTo?: string
  /**
   * Show the shared AI-summary-model config status pill on this row
   * (已配置 / 未配置). Used by the summary-model rows, so
   * the user can tell at a glance whether the model those features call is
   * actually configured — the backend silently skips when it is not.
   */
  showSummaryModelStatus?: boolean
  /** Progress bar for info-type items: { value, max }. `active: false` shows the
   *  bar without the moving stripes (backlog that cannot progress right now,
   *  e.g. the embedder is unreachable). */
  progress?: { value: number; max: number; active?: boolean }
  /**
   * Render this row as a read-only feature status (已启用 / 已停用) with a
   * 「去功能编排」 link, instead of a control. The value is the server enable
   * key (e.g. `forge.enabled`) — the switch itself lives in the Dock feature
   * manager, because one toggle there has to control BOTH the tab and the
   * background feature. A second switch here would be a second source of truth
   * for the same flag.
   */
  statusFeature?: string
}

// ── Group panel config types ─────────────────────────────

export interface GroupPanelConfig {
  /** Unique panel ID within the category (e.g., 'terminal', 'dingtalk') */
  panelId: string
  /** i18n key for the panel title (shown as separator). Skipped for single-panel categories with no flat items. */
  titleKey?: string
  enableKey?: string
  enableLabelKey?: string
  entrySelector?: ItemSpec
  commonFields: ItemSpec[]
  optionSubFields?: { when: unknown; fields: ItemSpec[] }[]
  requiredFields?: string[]
  /** Whether this panel has a connectivity test button. Can be a static boolean or a function that checks current values. */
  hasConnectivityTest?: boolean | ((values: Record<string, unknown>) => boolean)
  /** Function to map panel values to backend test categories.
   *  Receives current localValues, returns array of { category, values }.
   *  If hasConnectivityTest is true but this is undefined, defaults to
   *  [{ category: panelId, values }]. */
  getTestCategories?: (values: Record<string, unknown>) => Array<{ category: string; values: Record<string, unknown> }>
  /** Side effect: called after successful save with changed keys and current panel values */
  afterSave?: (changedKeys: string[], values?: Record<string, unknown>) => void
  /** Side effect: called on panel mount */
  onInit?: () => void
}

/** A category entry is either a flat item or a group panel. */
export type CategoryEntry =
  | { type: 'item'; spec: ItemSpec }
  | { type: 'panel'; config: GroupPanelConfig }

// ── Category items (unified: flat items + panels) ────────────

/**
 * Complete category → entries mapping.
 * Each entry is either a flat instant-save item or a group panel with batch save.
 */
export const categoryItems: Record<string, CategoryEntry[]> = {
  // Custom page (DockPluginSetting.vue): presets + visibility + order. The
  // empty entry keeps the "every SettingsIndex category exists here" contract
  // without pretending it is a flat item list.
  dockPlugins: [],
  // The human user's identity. A first-class category rather than a row under
  // 群聊: the nickname is also the meeting display name, and the profile is
  // injected into every agent's system prompt — neither is group-chat-specific.
  identity: [
    { type: 'item', spec: { labelKey: 'settings.items.userNickname', descriptionKey: 'settings.items.userNicknameDesc', key: 'user.nickname', type: 'text', source: 'server', sectionHeader: 'settings.items.identitySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.userProfile', descriptionKey: 'settings.items.userProfileDesc', key: 'user.profile', type: 'textarea', source: 'server', maxLength: 4000, sectionHeader: 'settings.items.identitySection' } },
  ],
  appearance: [
    { type: 'item', spec: { labelKey: 'settings.items.theme', descriptionKey: 'settings.items.themeDesc', key: 'theme', type: 'select', source: 'local', sectionHeader: 'settings.items.themeSection', options: [
      { labelKey: 'settings.items.themeAuto', value: 'auto' },
      // 按背景亮度从浅到深排列
      { labelKey: 'settings.items.themeOneLight', value: 'one-light' },
      { labelKey: 'settings.items.themeAyuLight', value: 'ayu-light' },
      { labelKey: 'settings.items.themeGithubLight', value: 'github-light' },
      { labelKey: 'settings.items.themeLightModern', value: 'light-modern' },
      { labelKey: 'settings.items.themeLightPlus', value: 'light-plus' },
      { labelKey: 'settings.items.themeQuietLight', value: 'quiet-light' },
      { labelKey: 'settings.items.themeVitesseLight', value: 'vitesse-light' },
      { labelKey: 'settings.items.themeBlulocoLight', value: 'bluloco-light' },
      { labelKey: 'settings.items.themeMaterialLighter', value: 'material-lighter' },
      { labelKey: 'settings.items.themeAlabaster', value: 'alabaster' },
      { labelKey: 'settings.items.themeEverforestLight', value: 'everforest-light' },
      { labelKey: 'settings.items.themeHighContrastLight', value: 'high-contrast-light' },
      { labelKey: 'settings.items.themeNordLight', value: 'nord-light' },
      { labelKey: 'settings.items.themeCatppuccinLatte', value: 'catppuccin-latte' },
      { labelKey: 'settings.items.themeSolarizedLight', value: 'solarized-light' },
      { labelKey: 'settings.items.themeGruvboxLight', value: 'gruvbox-light' },
      { labelKey: 'settings.items.themeSolarizedDark', value: 'solarized-dark' },
      { labelKey: 'settings.items.themeMonokai', value: 'monokai' },
      { labelKey: 'settings.items.themeMaterialDarker', value: 'material-darker' },
      { labelKey: 'settings.items.themeDarkPlus', value: 'dark-plus' },
      { labelKey: 'settings.items.themeBlulocoDark', value: 'bluloco-dark' },
      { labelKey: 'settings.items.themeNord', value: 'nord' },
      { labelKey: 'settings.items.themeEverforestDark', value: 'everforest-dark' },
      { labelKey: 'settings.items.themeOneDarkPro', value: 'one-dark-pro' },
      { labelKey: 'settings.items.themeDracula', value: 'dracula' },
      { labelKey: 'settings.items.themeRosePine', value: 'rose-pine' },
      { labelKey: 'settings.items.themeGruvboxDark', value: 'gruvbox-dark' },
      { labelKey: 'settings.items.themeSolarizedDeep', value: 'solarized-deep' },
      { labelKey: 'settings.items.themeGithubDark', value: 'github-dark' },
      { labelKey: 'settings.items.themeCatppuccinMocha', value: 'catppuccin-mocha' },
      { labelKey: 'settings.items.themeVitesseDark', value: 'vitesse-dark' },
      { labelKey: 'settings.items.themeTokyoNight', value: 'tokyo-night' },
      { labelKey: 'settings.items.themeKanagawa', value: 'kanagawa' },
      { labelKey: 'settings.items.themeAyuDark', value: 'ayu-dark' },
      { labelKey: 'settings.items.themeNightOwl', value: 'night-owl' },
      { labelKey: 'settings.items.themeHighContrastDark', value: 'high-contrast-dark' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.locale', descriptionKey: 'settings.items.localeDesc', key: 'locale', type: 'select', source: 'local', sectionHeader: 'settings.items.appearanceDisplaySection', options: [
      { labelKey: 'settings.items.localeZh', value: 'zh' },
      { labelKey: 'settings.items.localeEn', value: 'en' },
    ]}},
    // One-shot auto fit, deliberately NOT a switch: the scale is a stored value
    // now, and the button only computes it when clicked. An always-on switch
    // re-derived the factor on every applier call, which made the UI jump.
    // Hidden on Android, whose layout is already density-adapted by the WebView.
    { type: 'item', spec: { labelKey: 'settings.items.uiScaleAutoFit', descriptionKey: 'settings.items.uiScaleAutoFitDesc', key: 'uiScaleAutoFit', type: 'action', source: 'local', hideInAndroidApp: true, sectionHeader: 'settings.items.appearanceDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.uiScale', descriptionKey: 'settings.items.uiScaleDesc', key: 'uiScale', type: 'slider', source: 'local', min: UI_SCALE_MANUAL_MIN, max: UI_SCALE_MANUAL_MAX, step: UI_SCALE_STEP, defaultValue: 1, displayFormat: 'percent', sectionHeader: 'settings.items.appearanceDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.headerShortcutTips', descriptionKey: 'settings.items.headerShortcutTipsDesc', key: 'headerShortcutTips', type: 'switch', source: 'local', sectionHeader: 'settings.items.appearanceDisplaySection' } },
    // How panel geometry is remembered: one record for every project, or one
    // per project. Placed in Display rather than a new card because it is the
    // same kind of per-device preference as UI scale — and it must live next to
    // the things it governs, or nobody finds it.
    { type: 'item', spec: { labelKey: 'settings.items.layoutMemory', descriptionKey: 'settings.items.layoutMemoryDesc', key: 'layoutMemory', type: 'select', source: 'local', defaultValue: 'global', sectionHeader: 'settings.items.appearanceDisplaySection', options: [
      { labelKey: 'settings.items.layoutMemoryGlobal', value: 'global' },
      { labelKey: 'settings.items.layoutMemoryProject', value: 'project' },
    ]}},
    // Starting wrap mode for every Markdown table (chat, file preview, task
    // detail, row viewer). A display preference, so it lives next to the other
    // per-device display tweaks rather than under a content category.
    { type: 'item', spec: { labelKey: 'settings.items.tableWrapDefault', descriptionKey: 'settings.items.tableWrapDefaultDesc', key: 'tableWrap', type: 'switch', source: 'local', sectionHeader: 'settings.items.appearanceDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.fontMono', descriptionKey: 'settings.items.fontMonoDesc', key: 'fontMono', type: 'select', source: 'local', defaultValue: 'default', sectionHeader: 'settings.items.fontSection', options: buildFontFamilyOptions(true) }},
    { type: 'item', spec: { labelKey: 'settings.items.fontMonoFallback', descriptionKey: 'settings.items.fontMonoFallbackDesc', key: 'fontMonoFallback', type: 'select', source: 'local', defaultValue: 'default', sectionHeader: 'settings.items.fontSection', options: buildMonoFallbackOptions() }},
    { type: 'item', spec: { labelKey: 'settings.items.fontUi', descriptionKey: 'settings.items.fontUiDesc', key: 'fontUi', type: 'select', source: 'local', defaultValue: 'default', sectionHeader: 'settings.items.fontSection', options: buildFontFamilyOptions(false) }},
    { type: 'item', spec: { labelKey: 'settings.items.fontUiFallback', descriptionKey: 'settings.items.fontUiFallbackDesc', key: 'fontUiFallback', type: 'select', source: 'local', defaultValue: 'default', sectionHeader: 'settings.items.fontSection', options: buildFontFamilyOptions(false) }},
    { type: 'item', spec: { labelKey: 'settings.items.fontDir', descriptionKey: 'settings.items.fontDirDesc', key: 'fonts.dir', type: 'text', source: 'server', sectionHeader: 'settings.items.fontSection' }},
    // Custom wallpaper: the wallpaper-file control is a dedicated component
    // (WallpaperSetting.vue) rendered by SettingsCategory at the top of this
    // section; only the panel-opacity slider is a real data item here. The
    // slider item is NOT rendered through the generic row (its UI lives inside
    // WallpaperSetting) — it exists so the section card is created and the
    // value flows through the same local-config pipeline as the other display
    // tweaks. Panel opacity is a per-device preference (localStorage), like
    // blur / edge fade / wave speed.
    { type: 'item', spec: { labelKey: 'settings.items.wallpaperPanelOpacity', key: 'panelOpacity', type: 'slider', source: 'local', min: 0, max: 1, step: 0.01, defaultValue: 0.7, displayFormat: 'percent', sectionHeader: 'settings.items.wallpaperSection' }},
  ],
  agents: [],
  // 聊天 owns what a turn looks like and how it behaves: reading aloud, the
  // transcript, the context window, recommended replies and group discussion.
  // The session LIST and the session lifecycle moved to `sessions` — they are
  // about sessions as objects, not about the conversation itself, and mixing
  // the two made one page answer two unrelated questions.
  chat: [
    { type: 'item', spec: { labelKey: 'settings.items.autoSpeech', descriptionKey: 'settings.items.autoSpeechDesc', key: 'autoSpeech', type: 'switch', source: 'local', sectionHeader: 'settings.items.chatInteractionSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.preventScreenLock', descriptionKey: 'settings.items.preventScreenLockDesc', key: 'preventScreenLock', type: 'switch', source: 'local', sectionHeader: 'settings.items.chatInteractionSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.messageDisplayMode', descriptionKey: 'settings.items.messageDisplayModeDesc', key: 'messageDisplayMode', type: 'select', source: 'local', sectionHeader: 'settings.items.chatMessageSection', options: [
      { labelKey: 'settings.items.messageDisplayModeMixed', value: 'mixed' },
      { labelKey: 'settings.items.messageDisplayModeSummary', value: 'summary' },
      { labelKey: 'settings.items.messageDisplayModeOriginal', value: 'original' },
    ]}},
    // Fold a run of tool calls into one "ran N operations" row. On by default:
    // a long turn otherwise becomes a wall of full-width pills. The per-tool
    // detail is one click away (the row unfolds in place).
    { type: 'item', spec: { labelKey: 'settings.items.compactToolCalls', descriptionKey: 'settings.items.compactToolCallsDesc', key: 'compactToolCalls', type: 'switch', source: 'local', sectionHeader: 'settings.items.chatMessageSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatInitialMessages', descriptionKey: 'settings.items.chatInitialMessagesDesc', key: 'chat.initial_messages', type: 'number', source: 'server', sectionHeader: 'settings.items.chatMessageSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatPageSize', descriptionKey: 'settings.items.chatPageSizeDesc', key: 'chat.page_size', type: 'number', source: 'server', sectionHeader: 'settings.items.chatMessageSection' } },
    // Its own card: how often the system prompt is re-injected and how much
    // history a fork/branch may carry are the two knobs that decide what the
    // model actually sees, which is a different subject from how the transcript
    // is drawn.
    { type: 'item', spec: { labelKey: 'settings.items.chatSystemPromptInterval', descriptionKey: 'settings.items.chatSystemPromptIntervalDesc', key: 'chat.system_prompt_interval', type: 'number', source: 'server', sectionHeader: 'settings.items.chatContextSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatForkContextBudget', descriptionKey: 'settings.items.chatForkContextBudgetDesc', key: 'chat.fork_context_budget', type: 'number', source: 'server', min: 1000, sectionHeader: 'settings.items.chatContextSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatRecommendEnabled', descriptionKey: 'settings.items.chatRecommendEnabledDesc', key: 'chat.recommend_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.recommendSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatRecommendContextMessages', descriptionKey: 'settings.items.chatRecommendContextMessagesDesc', key: 'chat.recommend_context_messages', type: 'number', source: 'server', min: 0, max: 20, disableUnless: { key: 'chat.recommend_enabled', value: true }, sectionHeader: 'settings.items.recommendSectionHeader' } },
    // The AI-summary jump belongs to the 推荐回复 card. It must stay contiguous
    // with the rows above: the renderer groups cards by *runs* of the same
    // sectionHeader, so any row with a different header inserted here splits
    // this row into a second, one-row 推荐回复 card.
    { type: 'item', spec: { labelKey: 'settings.items.summaryModelRow', descriptionKey: 'settings.items.summaryModelRowDesc', key: 'summaryModel', type: 'summaryModel', source: 'local', disableUnless: { key: 'chat.recommend_enabled', value: true }, showSummaryModelStatus: true, sectionHeader: 'settings.items.recommendSectionHeader' } },
    // Group chat: the cap on how many times the agents may speak to each other
    // before the floor returns to the user. A global server setting (same value
    // on every device). The user nickname now lives in the 身份 category — it
    // is not group-chat-specific.
    { type: 'item', spec: { labelKey: 'settings.items.groupMaxSpeeches', descriptionKey: 'settings.items.groupMaxSpeechesDesc', key: 'chat.group_max_speeches', type: 'number', source: 'server', min: 1, sectionHeader: 'settings.items.groupChatSectionHeader' } },
  ],
  // 会话管理: the session list and the session lifecycle. Split out of 聊天 so
  // that "how many sessions, how they are ordered, when they are cleaned up"
  // is not interleaved with "how a message is drawn".
  sessions: [
    // ── 会话列表 ──
    { type: 'item', spec: { labelKey: 'settings.items.sessionMaxCount', descriptionKey: 'settings.items.sessionMaxCountDesc', key: 'session.max_count', type: 'number', source: 'server', sectionHeader: 'settings.items.sessionListSection' } },
    // Session list order. Off by default: ordering by activity reshuffles the
    // sidebar while the user reads it, which was disruptive enough to be made
    // opt-in. Manual drag order still wins in both modes.
    { type: 'item', spec: { labelKey: 'settings.items.sessionSortRecent', descriptionKey: 'settings.items.sessionSortRecentDesc', key: 'chat.session_sort_recent', type: 'switch', source: 'server', sectionHeader: 'settings.items.sessionListSection' } },
    // How the list is navigated on touch screens. Local: a desktop user who
    // swipes with a trackpad and a phone user may want different answers.
    { type: 'item', spec: { labelKey: 'settings.items.swipeSession', descriptionKey: 'settings.items.swipeSessionDesc', key: 'swipeSession', type: 'switch', source: 'local', sectionHeader: 'settings.items.sessionListSection' } },
    // 「其他项目卡片」自成一组：这两个值是同一件事的两个维度（几张卡、多久内算活跃），
    // 放在「会话列表」里会跟会话条数、排序混在一起。渲染器按 sectionHeader 的
    // **连续段**分组，所以这两行必须紧挨着，中间不能插别的 header。
    { type: 'item', spec: { labelKey: 'settings.items.recentProjectCardsMaxCount', descriptionKey: 'settings.items.recentProjectCardsMaxCountDesc', key: 'chat.recent_project_cards_max_count', type: 'number', source: 'server', min: 1, sectionHeader: 'settings.items.recentProjectCardsSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.recentProjectCardsWindowHours', descriptionKey: 'settings.items.recentProjectCardsWindowHoursDesc', key: 'chat.recent_project_cards_window_hours', type: 'number', source: 'server', min: 1, sectionHeader: 'settings.items.recentProjectCardsSectionHeader' } },
    // How those cards are laid out. Local (per device): the same person may
    // want the always-visible strip on a desktop and separate tabs on a phone.
    // Kept inside the same section as the two rows above — the renderer groups
    // by consecutive runs of the same sectionHeader.
    { type: 'item', spec: { labelKey: 'settings.items.sessionOtherProjectMode', descriptionKey: 'settings.items.sessionOtherProjectModeDesc', key: 'sessionOtherProjectMode', type: 'select', source: 'local', sectionHeader: 'settings.items.recentProjectCardsSectionHeader', options: [
      { labelKey: 'settings.items.sessionOtherProjectModeStrip', value: 'strip' },
      { labelKey: 'settings.items.sessionOtherProjectModeTabs', value: 'tabs' },
    ]}},
    // ── 会话生命周期 ──
    { type: 'item', spec: { labelKey: 'settings.items.chatAutoContinueEnabled', descriptionKey: 'settings.items.chatAutoContinueEnabledDesc', key: 'chat.auto_continue_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.autoContinueSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatAutoContinueMaxRetries', descriptionKey: 'settings.items.chatAutoContinueMaxRetriesDesc', key: 'chat.auto_continue_max_retries', type: 'number', source: 'server', min: -1, disableUnless: { key: 'chat.auto_continue_enabled', value: true }, sectionHeader: 'settings.items.autoContinueSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatRestartResumeWindow', descriptionKey: 'settings.items.chatRestartResumeWindowDesc', key: 'chat.restart_resume_window_minutes', type: 'number', source: 'server', min: 0, disableUnless: { key: 'chat.auto_continue_enabled', value: true }, sectionHeader: 'settings.items.autoContinueSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatAutoRenameEnabled', descriptionKey: 'settings.items.chatAutoRenameEnabledDesc', key: 'chat.auto_rename_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.autoRenameSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.summaryModelRow', descriptionKey: 'settings.items.summaryModelRowDesc', key: 'summaryModel', type: 'summaryModel', source: 'local', disableUnless: { key: 'chat.auto_rename_enabled', value: true }, showSummaryModelStatus: true, sectionHeader: 'settings.items.autoRenameSectionHeader' } },
    // 删除会话：永久删除时是否连智能体端会话一起删。和「归档留存」分开一组——归档可逆、
    // 删除不可逆，两件事不该挤在同一个标题下。
    { type: 'item', spec: { labelKey: 'settings.items.destroyAcpSession', descriptionKey: 'settings.items.destroyAcpSessionDesc', key: 'session.delete_acp_on_destroy', type: 'switch', source: 'server', sectionHeader: 'settings.items.destroySessionSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.archiveRetentionEnabled', descriptionKey: 'settings.items.archiveRetentionEnabledDesc', key: 'session.archive_retention_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.archiveRetentionSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.archiveRetentionDays', descriptionKey: 'settings.items.archiveRetentionDaysDesc', key: 'session.archive_retention_days', type: 'number', source: 'server', min: 0, disableUnless: { key: 'session.archive_retention_enabled', value: true }, sectionHeader: 'settings.items.archiveRetentionSectionHeader' } },
    // ── 会话搜索与索引 ──
    // These four shape how sessions are indexed and how many hits a search
    // returns — session-retrieval behaviour, which is why they sit here rather
    // than on the RAG page. The embedding service they call is configured on
    // the RAG page and shared with the knowledge base.
    { type: 'item', spec: { labelKey: 'settings.items.ragRetentionDays', descriptionKey: 'settings.items.ragRetentionDaysDesc', key: 'rag.retention_days', type: 'number', source: 'server', sectionHeader: 'settings.items.sessionSearchSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.ragSearchLimit', descriptionKey: 'settings.items.ragSearchLimitDesc', key: 'rag.search_limit', type: 'number', source: 'server', sectionHeader: 'settings.items.sessionSearchSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.ragSearchPoolSize', descriptionKey: 'settings.items.ragSearchPoolSizeDesc', key: 'rag.search_pool_size', type: 'number', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true }, sectionHeader: 'settings.items.sessionSearchSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.ragBatchSize', descriptionKey: 'settings.items.ragBatchSizeDesc', key: 'rag.batch_size', type: 'number', source: 'server', sectionHeader: 'settings.items.sessionSearchSection' } },
  ],
  // Task scheduler. The master switch (`tasks.enabled`) is a Dock feature switch
  // and lives in the feature manager — turning it off stops the scheduler and
  // cancels running/queued runs, so this page only reports its state. Everything
  // below shapes HOW the scheduler behaves once it is on.
  // Fields live in `plugins/tasks/settings.ts` — see PLUGIN_SETTINGS below.
  tasks: [],
  project: [
    { type: 'item', spec: { labelKey: 'settings.items.recentProjectsMaxCount', descriptionKey: 'settings.items.recentProjectsMaxCountDesc', key: 'recent_projects.max_count', type: 'number', source: 'server', min: 1, sectionHeader: 'settings.items.projectSectionHeader' } },
    // Opens the "all projects" card, which ProjectsSetting fills with the live
    // registry listing. The placeholder carries only the section header, so it
    // must not also render as a generic row (see PLACEHOLDER_ITEMS).
    { type: 'item', spec: { labelKey: 'settings.items.allProjectsSection', key: 'allProjectsList', type: 'info', source: 'local', sectionHeader: 'settings.items.allProjectsSection' } },
  ],
  // Fields live in `plugins/browse/settings.ts` — see PLUGIN_SETTINGS below.
  files: [],
  // File preview is its own tab, so it gets its own settings page rather than
  // sharing the file manager's. The file manager keeps list/grid, sorting,
  // hidden files and upload; preview keeps the reading surface.
  // Fields live in `plugins/view/settings.ts` — see PLUGIN_SETTINGS below.
  fileView: [],
  // Git history (the 历史 dock tab). Before this page the list length was a
  // hardcoded 30 in the server, the time column was a fixed "relative under a
  // week, absolute after" mix, and merges/the graph could not be turned off.
  // Fields live in `plugins/history/settings.ts` — see PLUGIN_SETTINGS below.
  gitHistory: [],
  // Statistics panel defaults. All local: they only seed what the panel opens
  // with, and the same person may want a different default on a phone.
  // Fields live in `plugins/stats/settings.ts` — see PLUGIN_SETTINGS below.
  stats: [],
  debug: [
    // Opt-in: agent processes inherit what the user's shell startup files
    // export. Off by default because it runs those files and hands the agent
    // their whole environment (see issue #619).
    { type: 'item', spec: { labelKey: 'settings.items.agentEnvInherit', descriptionKey: 'settings.items.agentEnvInheritDesc', key: 'shell_env.inherit', type: 'switch', source: 'server', sectionHeader: 'settings.items.agentEnvSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.logCapture', descriptionKey: 'settings.items.logCaptureDesc', key: 'logCapture', type: 'switch', source: 'local', sectionHeader: 'settings.items.debugSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.reconfigureServer', descriptionKey: 'settings.items.reconfigureServerDesc', key: 'reconfigureServer', type: 'action', source: 'local', appOnly: true, sectionHeader: 'settings.items.debugSection' } },
  ],
  security: [
    // HTTPS certificate: its own card, opened by a placeholder item that the
    // dedicated component fills (TlsCertPanel). The directory row stays below
    // the panel in the same card, because it is the one thing the panel needs
    // from the generic form.
    { type: 'item', spec: { labelKey: 'settings.items.tlsSection', descriptionKey: 'settings.items.tlsPanelDesc', key: 'tlsCert', type: 'info', source: 'server', sectionHeader: 'settings.items.tlsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.tlsCertDir', descriptionKey: 'settings.items.tlsCertDirDesc', key: 'tls.cert_dir', type: 'text', source: 'server', needsRestart: true, sectionHeader: 'settings.items.tlsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.changePassword', descriptionKey: 'settings.items.changePasswordDesc', key: 'changePassword', type: 'action', source: 'local', sectionHeader: 'settings.items.securitySection' } },
    // Desktop-only: the Electron shell remembers accepted self-signed
    // certificates (origin -> fingerprint), so it needs an explicit undo.
    // `appOnly` keeps it out of the browser, `hideInAndroidApp` out of Android,
    // which never persists the answer (it re-asks on every connection).
    { type: 'item', spec: { labelKey: 'settings.items.trustedCerts', descriptionKey: 'settings.items.trustedCertsDesc', key: 'trustedCerts', type: 'action', source: 'local', appOnly: true, hideInAndroidApp: true, sectionHeader: 'settings.items.securitySection' } },
  ],
  skills: [
    // Cross-agent skill discovery, split into one card per concern. Each entry
    // below carries no rendered row — it exists only to open a section (and
    // therefore a card), which SettingsCategory fills with a dedicated
    // component. They must be listed in PLACEHOLDER_ITEMS there.
    //   skillsCard     → master switch (SkillsSetting)
    //   skillsDirs     → local directories (SkillsDirsSetting)
    //   skillsRepos    → git repositories + sync (SkillsReposSetting)
    //   skillsDiscovered → the read-only result listing (SkillsDiscoveredSetting)
    { type: 'item', spec: { labelKey: 'settings.items.skillsCard', descriptionKey: 'settings.items.skillsCardDesc', key: 'skillsCard', type: 'info', source: 'server', sectionHeader: 'settings.items.skillsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.skillsDirs', key: 'skillsDirs', type: 'info', source: 'server', sectionHeader: 'settings.items.skillsDirs' } },
    { type: 'item', spec: { labelKey: 'settings.items.skillsRepos', key: 'skillsRepos', type: 'info', source: 'server', sectionHeader: 'settings.items.skillsRepos' } },
    { type: 'item', spec: { labelKey: 'settings.items.skillsDiscoveredSection', key: 'skillsDiscovered', type: 'info', source: 'server', sectionHeader: 'settings.items.skillsDiscoveredSection' } },
  ],
  mcp: [
    // Global MCP server list. The card body is a dedicated component
    // (MCPSetting); this entry only opens the section, so it must also be
    // listed in PLACEHOLDER_ITEMS in SettingsCategory.vue.
    { type: 'item', spec: { labelKey: 'settings.items.mcpSection', descriptionKey: 'settings.items.mcpDesc', key: 'mcpServers', type: 'info', source: 'server', sectionHeader: 'settings.items.mcpSection' } },
  ],
  // Fields live in `plugins/mail/settings.ts` — see PLUGIN_SETTINGS below.
  mail: [],
  providers: [
    // Model-provider list (endpoint + credential). Same shape as the MCP
    // section: the card body is ProviderSetting, so this placeholder item only
    // opens the section and must be listed in PLACEHOLDER_ITEMS.
    { type: 'item', spec: { labelKey: 'settings.items.providerSection', descriptionKey: 'settings.items.providerDesc', key: 'providersCard', type: 'info', source: 'server', sectionHeader: 'settings.items.providerSection' } },
  ],
  // Share center. The global switch (`share.enabled`) is a Dock feature switch:
  // turning it off revokes every link already handed out, which is far more
  // than a display preference, so it belongs in the feature manager. This page
  // holds the finer sharing RULES.
  // Fields live in `plugins/share/settings.ts` — see PLUGIN_SETTINGS below.
  share: [],
  // Fields live in `plugins/forge/settings.ts` — see PLUGIN_SETTINGS below.
  forgeIntegration: [],
  notification: [
    // In-app surfaces: the completion card and its alert sound. Both are local
    // settings and take effect immediately (no save button).
    { type: 'item', spec: { labelKey: 'settings.items.inAppNotification', descriptionKey: 'settings.items.inAppNotificationDesc', key: 'inAppNotification', type: 'switch', source: 'local', sectionHeader: 'settings.items.inAppNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.notificationSound', descriptionKey: 'settings.items.notificationSoundDesc', key: 'notificationSound', type: 'switch', source: 'local', sectionHeader: 'settings.items.inAppNotifySection' } },
    // System notifications. Deliberately NOT appOnly: the "桌面与系统" card below
    // is app-only and disappears in browser mode, which is precisely where this
    // switch matters most. It gates the Notification API path and is independent
    // of the server-side push_mode (that one selects the mobile/IM channel).
    { type: 'item', spec: { labelKey: 'settings.items.desktopNotification', descriptionKey: 'settings.items.desktopNotificationDesc', key: 'desktopNotification', type: 'switch', source: 'local', sectionHeader: 'settings.items.desktopNotifySection' } },
    // Out-of-app desktop/system surfaces.
    //
    // The Live Updates chip is `androidOnly` (NOT `appOnly`): `setLiveUpdateEnabled`
    // / `canPostPromotedNotifications` exist only on the Android bridge, and the
    // copy names Android-only permissions. `appOnly` is `!isAppMode`, which the
    // Electron shell also satisfies, so it rendered there as a dead switch.
    //
    // The floating status window is `appOnly` for the same reason but the
    // opposite conclusion: BOTH hosts implement it — Android draws the bubble
    // over the launcher, the Electron shell draws an always-on-top capsule plus
    // a tray icon (`desktop/src/main/floating.ts`). Marking it `androidOnly`
    // left the desktop feature with no way to be switched on at all, and the
    // tray's "turn off" then had no way back.
    { type: 'item', spec: { labelKey: 'settings.items.floatingStatusWindow', descriptionKey: 'settings.items.floatingStatusWindowDesc', key: 'floatingStatusWindow', type: 'switch', source: 'local', appOnly: true, sectionHeader: 'settings.items.desktopSystemSection' } },
    // What the frameless shell's close button does. Desktop-only for the same
    // reason as `trustedCerts` below: the dialog, the tray icon and the
    // `getCloseAction`/`setCloseAction` bridge exist only in the Electron shell.
    // macOS is excluded at runtime (`windowChrome.ts` gives it a native frame
    // and its red button already means "close the window, keep the app").
    { type: 'item', spec: { labelKey: 'settings.items.closeAction', descriptionKey: 'settings.items.closeActionDesc', key: 'closeAction', type: 'select', source: 'local', appOnly: true, hideInAndroidApp: true, sectionHeader: 'settings.items.desktopSystemSection', options: [
      { labelKey: 'settings.items.closeActionAsk', value: 'ask' },
      { labelKey: 'settings.items.closeActionTray', value: 'tray' },
      { labelKey: 'settings.items.closeActionExit', value: 'exit' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveUpdate', descriptionKey: 'settings.items.liveUpdateDesc', key: 'liveUpdate', type: 'switch', source: 'local', androidOnly: true, sectionHeader: 'settings.items.desktopSystemSection' } },
    { type: 'panel', config: {
      panelId: 'push',
      titleKey: 'settings.items.mobileNotifySection',
      entrySelector: {
        labelKey: 'settings.items.pushMode',
        descriptionKey: 'settings.items.pushModeDesc',
        key: 'push_mode',
        type: 'select',
        source: 'server',
        options: [
          { labelKey: 'settings.items.pushModeNative', value: 'native' },
          { labelKey: 'settings.items.pushModeDingtalk', value: 'dingtalk' },
          { labelKey: 'settings.items.pushModeFeishu', value: 'feishu' },
          // WeChat has no editable credentials: selecting it renders the QR
          // login block instead (see WeChatLoginPanel), so it has no
          // optionSubFields entry and is excluded from requiredFields.
          { labelKey: 'settings.items.pushModeWechat', value: 'wechat' },
          { labelKey: 'settings.items.pushModeDisabled', value: 'disabled' },
        ],
        defaultValue: 'native',
      },
      commonFields: [],
      optionSubFields: [
        { when: 'dingtalk', fields: [
          { labelKey: 'settings.items.dingtalkAppKey', descriptionKey: 'settings.items.dingtalkAppKeyDesc', key: 'dingtalk.app_key', type: 'text', source: 'server' },
          { labelKey: 'settings.items.dingtalkAppSecret', descriptionKey: 'settings.items.dingtalkAppSecretDesc', key: 'dingtalk.app_secret', type: 'password', source: 'server' },
          { labelKey: 'settings.items.dingtalkAgentId', descriptionKey: 'settings.items.dingtalkAgentIdDesc', key: 'dingtalk.agent_id', type: 'number', source: 'server' },
        ]},
        { when: 'feishu', fields: [
          { labelKey: 'settings.items.feishuAppId', descriptionKey: 'settings.items.feishuAppIdDesc', key: 'feishu.app_id', type: 'text', source: 'server' },
          { labelKey: 'settings.items.feishuAppSecret', descriptionKey: 'settings.items.feishuAppSecretDesc', key: 'feishu.app_secret', type: 'password', source: 'server' },
        ]},
      ],
      requiredFields: ['dingtalk.app_key', 'dingtalk.app_secret', 'dingtalk.agent_id', 'feishu.app_id', 'feishu.app_secret'],
      hasConnectivityTest: (values) => values.push_mode === 'dingtalk' || values.push_mode === 'feishu',
      getTestCategories: (values) => {
        if (values.push_mode === 'dingtalk') return [{ category: 'dingtalk', values }]
        if (values.push_mode === 'feishu') return [{ category: 'feishu', values }]
        return []
      },
      afterSave(changedKeys, values) {
        if (changedKeys.includes('push_mode')) {
          // Sync Android native push (backend derives dingtalk/feishu.enabled from push_mode automatically)
          try {
            getNative()?.setNativePushEnabled?.(values?.push_mode === 'native')
          } catch { /* not in app mode */ }
        }
      },
    }},
  ],
  // Usage meter: its own category, side by side with 推送通知 rather than
  // nested inside it. It is a notification FEATURE, but its configuration
  // (cycle, metric, budget) is a distinct subject — burying it as a card
  // under 推送通知 made it read as one more push channel.
  usageMeter: [
    // A single global rule. Every field is optional: the meter shows the largest
    // readout the configuration supports, from "total for all time" up to
    // "day N of M against a budget". The enable switch is the only on/off.
  { type: 'panel', config: {
    panelId: 'usageMeter',
    titleKey: 'settings.items.usageMeter',
    // The switch is its own row at the top of the panel rather than a field in
    // the list: it decides whether ANY of the rows below take effect. While it
    // is off the rows are disabled (not hidden), so the stored rule stays
    // visible — switching it back on does not require retyping anything.
    enableKey: 'usage_meter.enabled',
    enableLabelKey: 'settings.items.usageMeterEnabled',
    entrySelector: {
      // "Usage type", not "Usage": the row selects WHAT is counted (tokens /
      // credit / cost). Reusing the category title made it read as a repeat of
      // the page heading with no hint about what the value means.
      labelKey: 'settings.items.usageMeterMetric',
      // The metric row is where the unit changes, so the "switching clears
      // the quota" warning belongs here — it was previously written into
      // i18n but never referenced, so users lost their quota with no notice.
      descriptionKey: 'settings.items.usageMeterMetricDesc',
      key: 'usage_meter.metric',
      type: 'select',
      source: 'server',
      options: [
        { labelKey: 'settings.items.usageMeterMetricTokens', value: 'tokens' },
        { labelKey: 'settings.items.usageMeterMetricCredit', value: 'credit' },
        { labelKey: 'settings.items.usageMeterMetricCost', value: 'cost' },
      ],
      defaultValue: 'cost',
    },
    commonFields: [
      // Cycle type first: it decides whether the "period (days)" row below is
      // shown at all, so reading the two in the other order would present a
      // number whose meaning is not yet known.
      { labelKey: 'settings.items.usageMeterCycleMode', descriptionKey: 'settings.items.usageMeterCycleModeDesc', key: 'usage_meter.cycle_mode', type: 'select', source: 'server', options: [
        { labelKey: 'settings.items.usageMeterCycleModeMonthly', value: 'monthly' },
        { labelKey: 'settings.items.usageMeterCycleModeFixed', value: 'fixed' },
      ], defaultValue: 'fixed' },
      { labelKey: 'settings.items.usageMeterAnchor', descriptionKey: 'settings.items.usageMeterAnchorDesc', key: 'usage_meter.anchor_date', type: 'datetime', source: 'server', displayTransform: formatAnchorForDisplay },
      // Hidden in monthly mode: the cycle length comes from the calendar, so an
      // editable day count would be a value the user can change that has no
      // effect.
      //
      // 0 is meaningful ("no cycle: the total never resets"), so it is not
      // replaced with a default — inventing 30 would silently roll the user's
      // running total over every month.
      { labelKey: 'settings.items.usageMeterPeriod', descriptionKey: 'settings.items.usageMeterPeriodDesc', key: 'usage_meter.period_days', type: 'number', source: 'server', min: 0, max: 366, dependsOn: { key: 'usage_meter.cycle_mode', value: 'fixed' } },
      // 0 is meaningful here too ("no budget"), which is why the row is not
      // required and the panel never blocks a save on it.
      { labelKey: 'settings.items.usageMeterQuota', descriptionKey: 'settings.items.usageMeterQuotaDesc', key: 'usage_meter.quota', type: 'number', source: 'server', min: 0 },
      // What the quota is measured against. Empty = every agent, which is why
      // the row is not required: an empty selection IS the default.
      { labelKey: 'settings.items.usageMeterAgents', descriptionKey: 'settings.items.usageMeterAgentsDesc', key: 'usage_meter.agent_ids', type: 'multi-select', source: 'server', allOptionKey: 'settings.items.usageMeterAgentsAll' },
    ],
    requiredFields: [],
    // Switching the metric changes the unit, so a stored quota would silently
    // change meaning ($20 becoming 20 tokens). The SERVER clears it in the
    // same patch (see applyConfigPatch) — a second client-side request would
    // open a window where the new metric runs against the old number, and the
    // worker wakes within seconds of a PATCH. So no afterSave here.
  }},
  ],
  about: [
    { type: 'item', spec: { labelKey: 'settings.items.aboutServerVersion', descriptionKey: 'settings.items.aboutServerVersionDesc', key: 'serverVersion', type: 'info', source: 'server', sectionHeader: 'settings.items.aboutVersionSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.aboutAppVersion', descriptionKey: 'settings.items.aboutAppVersionDesc', key: 'appVersion', type: 'info', source: 'local', sectionHeader: 'settings.items.aboutVersionSection' } },
    // Project identity links — open the project site / GitHub repository in the
    // system browser. Rendered as their own section so they are not buried among
    // the maintenance actions below.
    { type: 'item', spec: { labelKey: 'settings.items.aboutHomepage', descriptionKey: 'settings.items.aboutHomepageDesc', key: 'openProjectHomepage', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutProjectSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.aboutGithub', descriptionKey: 'settings.items.aboutGithubDesc', key: 'openProjectGithub', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutProjectSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.aboutFeedback', descriptionKey: 'settings.items.aboutFeedbackDesc', key: 'openProjectFeedback', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutProjectSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.addToHomeScreen', descriptionKey: 'settings.items.addToHomeScreenDesc', key: 'addToHomeScreen', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutActionsSection' } },
    // NOT appOnly: that flag means "Android app only" and would hide this row
    // in the desktop browser, which is exactly where it is needed. Visibility is
    // gated in SettingsCategory.vue instead — the row renders only when a
    // download URL for the current platform actually exists.
    { type: 'item', spec: { labelKey: 'settings.items.downloadDesktopApp', descriptionKey: 'settings.items.downloadDesktopAppDesc', key: 'downloadDesktopApp', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutActionsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.downloadAndroidApp', descriptionKey: 'settings.items.downloadAndroidAppDesc', key: 'downloadAndroidApp', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutActionsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.showWelcome', descriptionKey: 'settings.items.showWelcomeDesc', key: 'showWelcome', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutActionsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.restartServer', descriptionKey: 'settings.items.restartServerDesc', key: 'restartServer', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutActionsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.checkUpgrade', descriptionKey: 'settings.items.checkUpgradeDesc', key: 'checkUpgrade', type: 'action', source: 'local', sectionHeader: 'settings.items.aboutActionsSection' } },
  ],

  // ── Panel-only categories (formerly drill-down) ────────────

  // Fields live in `plugins/terminal/settings.ts` — see PLUGIN_SETTINGS below.
  terminal: [],
  tts: [
    { type: 'item', spec: { labelKey: 'settings.items.ttsEngine', descriptionKey: 'settings.items.ttsEngineDesc', key: 'navigateTtsEngine', type: 'action', source: 'local', navigateTo: 'tts:tts_engine', sectionHeader: 'settings.items.ttsEngineSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.summarizeTtsSection', descriptionKey: 'settings.items.summarizeTtsBackendDesc', key: 'summarize.tts_backend', type: 'select', source: 'server', sectionHeader: 'settings.items.voiceSummarySection', options: [
      { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
      { labelKey: 'settings.items.summarizeApi', value: 'api' },
      { labelKey: 'settings.items.summarizeAuto', value: 'auto' },
    ]} },
    { type: 'item', spec: { labelKey: 'settings.items.summaryModelRow', descriptionKey: 'settings.items.summaryModelRowDesc', key: 'summaryModel', type: 'summaryModel', source: 'local', showSummaryModelStatus: true, sectionHeader: 'settings.items.voiceSummarySection' } },
    // Same header as the two rows above: the cards are grouped by consecutive
    // sectionHeader, so leaving this one header-less would drop it into the
    // trailing "其他" card and split the summary settings in two.
    { type: 'item', spec: { labelKey: 'settings.items.summarizeAutoJunkRatio', descriptionKey: 'settings.items.summarizeAutoJunkRatioDesc', key: 'summarize.auto_junk_ratio', type: 'slider', source: 'server', min: 0.1, max: 0.9, step: 0.05, displayFormat: 'percent', dependsOn: { key: 'summarize.tts_backend', value: 'auto' }, sectionHeader: 'settings.items.voiceSummarySection' } },
  ],
  tts_engine: [
    { type: 'panel', config: {
      panelId: 'tts',
      entrySelector: { labelKey: 'settings.items.ttsEngine', descriptionKey: 'settings.items.ttsEngineDesc', key: 'tts.engine', type: 'select', source: 'server', options: [
        { labelKey: 'settings.items.ttsEngineEdge', value: 'edge' },
        { labelKey: 'settings.items.ttsEngineOpenAI', value: 'openai' },
        { labelKey: 'settings.items.ttsEngineAlibaba', value: 'alibaba' },
        { labelKey: 'settings.items.ttsEngineTencent', value: 'tencent' },
        { labelKey: 'settings.items.ttsEngineVolcano', value: 'volcano' },
        { labelKey: 'settings.items.ttsEngineMinimax', value: 'minimax' },
        // Per-device, but deliberately in the same list: to the user this is
        // just another voice. Selecting it stores a local flag and leaves
        // tts.engine (the server-wide choice) untouched — see
        // SettingsGroupPanel's effectiveEngine.
        { labelKey: 'settings.items.ttsEngineSystem', value: 'system' },
      ]},
      commonFields: [
        // tts.voice belongs to the Edge engine. It is hidden for "openai" (that
        // engine has its own tts.openai.voice row below) and is NOT reset when
        // the engine changes — resetting it overwrote the Edge voice the moment
        // the user tried the other engine.
        { labelKey: 'settings.items.ttsVoice', descriptionKey: 'settings.items.ttsVoiceDesc', key: 'tts.voice', type: 'select', source: 'server', dependsOn: { key: 'tts.engine', value: 'edge' } },
        { labelKey: 'settings.items.ttsSpeed', descriptionKey: 'settings.items.ttsSpeedDesc', key: 'tts.speed', type: 'slider', source: 'server', min: 0.5, max: 3, step: 0.1 },
        { labelKey: 'settings.items.ttsMaxCacheFiles', descriptionKey: 'settings.items.ttsMaxCacheFilesDesc', key: 'tts.max_cache_files', type: 'number', source: 'server' },
      ],
      optionSubFields: [
        {
          when: 'openai',
          fields: [
            { labelKey: 'settings.items.openaiTTSBaseUrl', descriptionKey: 'settings.items.openaiTTSBaseUrlDesc', key: 'tts.openai.base_url', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsOpenAIHeader' },
            { labelKey: 'settings.items.openaiTTSApiKey', descriptionKey: 'settings.items.openaiTTSApiKeyDesc', key: 'tts.openai.api_key', type: 'password', source: 'server' },
            // Model and voice are free text on purpose: the set of models and
            // voices a compatible server accepts is not bounded by OpenAI's
            // documented list (custom voices are referenced by id).
            { labelKey: 'settings.items.openaiTTSModel', descriptionKey: 'settings.items.openaiTTSModelDesc', key: 'tts.openai.model', type: 'text', source: 'server' },
            { labelKey: 'settings.items.openaiTTSVoice', descriptionKey: 'settings.items.openaiTTSVoiceDesc', key: 'tts.openai.voice', type: 'text', source: 'server' },
            // Stream mode: "auto" follows the per-model default; "sse" / "audio"
            // force a transport for servers whose model name does not reveal
            // whether SSE is supported.
            { labelKey: 'settings.items.openaiTTSStreamMode', descriptionKey: 'settings.items.openaiTTSStreamModeDesc', key: 'tts.openai.stream_mode', type: 'select', source: 'server', options: [
              { labelKey: 'settings.items.openaiTTSStreamModeAuto', value: 'auto' },
              { labelKey: 'settings.items.openaiTTSStreamModeSSE', value: 'sse' },
              { labelKey: 'settings.items.openaiTTSStreamModeAudio', value: 'audio' },
            ]},
          ],
        },
        {
          // Alibaba Cloud Model Studio (百炼 / DashScope). The endpoint, model
          // and voice all have working defaults, so only the key is required.
          when: 'alibaba',
          fields: [
            { labelKey: 'settings.items.alibabaTTSApiKey', descriptionKey: 'settings.items.alibabaTTSApiKeyDesc', key: 'tts.alibaba.api_key', type: 'password', source: 'server', sectionHeader: 'settings.items.ttsAlibabaHeader' },
            { labelKey: 'settings.items.alibabaTTSBaseUrl', descriptionKey: 'settings.items.alibabaTTSBaseUrlDesc', key: 'tts.alibaba.base_url', type: 'text', source: 'server' },
            { labelKey: 'settings.items.alibabaTTSModel', descriptionKey: 'settings.items.alibabaTTSModelDesc', key: 'tts.alibaba.model', type: 'text', source: 'server' },
            { labelKey: 'settings.items.alibabaTTSVoice', descriptionKey: 'settings.items.alibabaTTSVoiceDesc', key: 'tts.alibaba.voice', type: 'text', source: 'server' },
          ],
        },
        {
          // Tencent Cloud. Auth is a signed request, so there is a SecretId /
          // SecretKey pair instead of a single token; the voice is a numeric
          // catalog id.
          when: 'tencent',
          fields: [
            { labelKey: 'settings.items.tencentTTSSecretId', descriptionKey: 'settings.items.tencentTTSSecretIdDesc', key: 'tts.tencent.secret_id', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsTencentHeader' },
            { labelKey: 'settings.items.tencentTTSSecretKey', descriptionKey: 'settings.items.tencentTTSSecretKeyDesc', key: 'tts.tencent.secret_key', type: 'password', source: 'server' },
            { labelKey: 'settings.items.tencentTTSVoiceType', descriptionKey: 'settings.items.tencentTTSVoiceTypeDesc', key: 'tts.tencent.voice_type', type: 'number', source: 'server' },
          ],
        },
        {
          // Volcano Engine (火山引擎 / 豆包). One API key from the speech
          // console, plus the resource id that matches the voice type.
          when: 'volcano',
          fields: [
            { labelKey: 'settings.items.volcanoTTSApiKey', descriptionKey: 'settings.items.volcanoTTSApiKeyDesc', key: 'tts.volcano.api_key', type: 'password', source: 'server', sectionHeader: 'settings.items.ttsVolcanoHeader' },
            { labelKey: 'settings.items.volcanoTTSResourceId', descriptionKey: 'settings.items.volcanoTTSResourceIdDesc', key: 'tts.volcano.resource_id', type: 'text', source: 'server' },
            { labelKey: 'settings.items.volcanoTTSVoice', descriptionKey: 'settings.items.volcanoTTSVoiceDesc', key: 'tts.volcano.voice', type: 'text', source: 'server' },
          ],
        },
        {
          // MiniMax (T2A v2). GroupId is optional — only older accounts need
          // it — and the base URL selects the China / international site.
          when: 'minimax',
          fields: [
            { labelKey: 'settings.items.minimaxTTSApiKey', descriptionKey: 'settings.items.minimaxTTSApiKeyDesc', key: 'tts.minimax.api_key', type: 'password', source: 'server', sectionHeader: 'settings.items.ttsMinimaxHeader' },
            { labelKey: 'settings.items.minimaxTTSGroupId', descriptionKey: 'settings.items.minimaxTTSGroupIdDesc', key: 'tts.minimax.group_id', type: 'text', source: 'server' },
            { labelKey: 'settings.items.minimaxTTSBaseUrl', descriptionKey: 'settings.items.minimaxTTSBaseUrlDesc', key: 'tts.minimax.base_url', type: 'text', source: 'server' },
            { labelKey: 'settings.items.minimaxTTSModel', descriptionKey: 'settings.items.minimaxTTSModelDesc', key: 'tts.minimax.model', type: 'text', source: 'server' },
            { labelKey: 'settings.items.minimaxTTSVoice', descriptionKey: 'settings.items.minimaxTTSVoiceDesc', key: 'tts.minimax.voice', type: 'text', source: 'server' },
          ],
        },
      ],
      requiredFields: [
        'tts.openai.base_url',
        'tts.openai.model',
        'tts.openai.voice',
        'tts.alibaba.api_key',
        'tts.tencent.secret_id',
        'tts.tencent.secret_key',
        'tts.volcano.api_key',
        'tts.minimax.api_key',
      ],
      hasConnectivityTest: true,
      getTestCategories: (values) => [{ category: 'tts', values }],
    }},
  ],
  stt: [
    { type: 'panel', config: {
      panelId: 'stt_engine',
      commonFields: [
        { labelKey: 'settings.items.sttProvider', descriptionKey: 'settings.items.sttProviderDesc', key: 'stt.provider', type: 'select', source: 'server', sectionHeader: 'settings.items.sttHeader', options: [
          // The empty value is "not configured": no engine, no rows, and voice
          // input tells the user to come back here instead of failing an upload.
          { labelKey: 'settings.items.sttProviderNone', value: '' },
          { labelKey: 'settings.items.sttProviderVllm', value: 'vllm' },
          { labelKey: 'settings.items.sttProviderDoubao', value: 'doubao' },
          { labelKey: 'settings.items.sttProviderAlibaba', value: 'alibaba' },
          { labelKey: 'settings.items.sttProviderTencent', value: 'tencent' },
        ] },
        // Each engine's credentials and options live in its own section. They
        // are NOT shared: a single set of fields meant switching the engine
        // overwrote the previous one's key and endpoint, so the engine you were
        // not using lost its configuration.
        { labelKey: 'settings.items.sttBaseUrl', descriptionKey: 'settings.items.sttBaseUrlDesc', key: 'stt.vllm.base_url', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'vllm' } },
        { labelKey: 'settings.items.sttModel', descriptionKey: 'settings.items.sttModelDesc', key: 'stt.vllm.model', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'vllm' } },
        { labelKey: 'settings.items.sttApiKey', descriptionKey: 'settings.items.sttApiKeyDesc', key: 'stt.vllm.api_key', type: 'password', source: 'server', dependsOn: { key: 'stt.provider', value: 'vllm' } },
        { labelKey: 'settings.items.sttLanguage', descriptionKey: 'settings.items.sttLanguageDesc', key: 'stt.vllm.language', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'vllm' } },
        // No 流式识别 switch here: this engine cannot decode live PCM, so the
        // dictation path always uploads one finished recording. The row used to
        // be rendered anyway and toggled a value nothing read.
        { labelKey: 'settings.items.sttChunkMs', descriptionKey: 'settings.items.sttChunkMsDesc', key: 'stt.vllm.chunk_ms', type: 'number', source: 'server', min: 200, max: 10000, step: 100, dependsOn: { key: 'stt.provider', value: 'vllm' } },
        { labelKey: 'settings.items.sttFinalRecheck', descriptionKey: 'settings.items.sttFinalRecheckDesc', key: 'stt.vllm.final_recheck', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'vllm' } },

        { labelKey: 'settings.items.sttDoubaoApiKey', descriptionKey: 'settings.items.sttDoubaoApiKeyDesc', key: 'stt.doubao.api_key', type: 'password', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' } },
        { labelKey: 'settings.items.sttResourceId', descriptionKey: 'settings.items.sttResourceIdDesc', key: 'stt.doubao.resource_id', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' } },
        { labelKey: 'settings.items.sttStreaming', descriptionKey: 'settings.items.sttStreamingDesc', key: 'stt.doubao.streaming', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' } },
        { labelKey: 'settings.items.sttEnableItn', descriptionKey: 'settings.items.sttEnableItnDesc', key: 'stt.doubao.enable_itn', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' } },
        { labelKey: 'settings.items.sttEnablePunc', descriptionKey: 'settings.items.sttEnablePuncDesc', key: 'stt.doubao.enable_punc', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' } },
        { labelKey: 'settings.items.sttEnableDdc', descriptionKey: 'settings.items.sttEnableDdcDesc', key: 'stt.doubao.enable_ddc', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' } },
        { labelKey: 'settings.items.sttZhVariant', descriptionKey: 'settings.items.sttZhVariantDesc', key: 'stt.doubao.output_zh_variant', type: 'select', source: 'server', dependsOn: { key: 'stt.provider', value: 'doubao' }, options: [
          { labelKey: 'settings.items.sttZhVariantNone', value: '' },
          { labelKey: 'settings.items.sttZhVariantTraditional', value: 'traditional' },
          { labelKey: 'settings.items.sttZhVariantTw', value: 'tw' },
          { labelKey: 'settings.items.sttZhVariantHk', value: 'hk' },
        ] },
        { labelKey: 'settings.items.sttEndWindowSize', descriptionKey: 'settings.items.sttEndWindowSizeDesc', key: 'stt.doubao.end_window_size', type: 'number', source: 'server', min: 300, max: 5000, step: 100, dependsOn: { key: 'stt.provider', value: 'doubao' } },

        { labelKey: 'settings.items.sttAlibabaApiKey', descriptionKey: 'settings.items.sttAlibabaApiKeyDesc', key: 'stt.alibaba.api_key', type: 'password', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttAlibabaBaseUrl', descriptionKey: 'settings.items.sttAlibabaBaseUrlDesc', key: 'stt.alibaba.base_url', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttAlibabaModel', descriptionKey: 'settings.items.sttAlibabaModelDesc', key: 'stt.alibaba.model', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttAlibabaFileModel', descriptionKey: 'settings.items.sttAlibabaFileModelDesc', key: 'stt.alibaba.file_model', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttAlibabaLanguage', descriptionKey: 'settings.items.sttAlibabaLanguageDesc', key: 'stt.alibaba.language', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttStreaming', descriptionKey: 'settings.items.sttStreamingDesc', key: 'stt.alibaba.streaming', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttEnableItn', descriptionKey: 'settings.items.sttEnableItnDesc', key: 'stt.alibaba.enable_itn', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttEnablePunc', descriptionKey: 'settings.items.sttEnablePuncDesc', key: 'stt.alibaba.enable_punc', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttEnableDdc', descriptionKey: 'settings.items.sttEnableDdcDesc', key: 'stt.alibaba.enable_ddc', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'alibaba' } },
        { labelKey: 'settings.items.sttEndWindowSize', descriptionKey: 'settings.items.sttEndWindowSizeDesc', key: 'stt.alibaba.end_window_size', type: 'number', source: 'server', min: 300, max: 5000, step: 100, dependsOn: { key: 'stt.provider', value: 'alibaba' } },

        // Tencent Cloud signs every request with a SecretId/SecretKey pair; the
        // realtime API additionally needs the numeric AppID in its URL path.
        // The engine model name carries the language (16k_zh, 16k_en, …), so
        // there is no separate language row.
        { labelKey: 'settings.items.sttTencentSecretId', descriptionKey: 'settings.items.sttTencentSecretIdDesc', key: 'stt.tencent.secret_id', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttTencentSecretKey', descriptionKey: 'settings.items.sttTencentSecretKeyDesc', key: 'stt.tencent.secret_key', type: 'password', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttTencentAppId', descriptionKey: 'settings.items.sttTencentAppIdDesc', key: 'stt.tencent.app_id', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttTencentModel', descriptionKey: 'settings.items.sttTencentModelDesc', key: 'stt.tencent.model', type: 'text', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttStreaming', descriptionKey: 'settings.items.sttTencentStreamingDesc', key: 'stt.tencent.streaming', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttEnableItn', descriptionKey: 'settings.items.sttEnableItnDesc', key: 'stt.tencent.enable_itn', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttEnableDdc', descriptionKey: 'settings.items.sttEnableDdcDesc', key: 'stt.tencent.enable_ddc', type: 'switch', source: 'server', dependsOn: { key: 'stt.provider', value: 'tencent' } },
        { labelKey: 'settings.items.sttTencentEndWindowSize', descriptionKey: 'settings.items.sttTencentEndWindowSizeDesc', key: 'stt.tencent.end_window_size', type: 'number', source: 'server', min: 500, max: 2000, step: 100, dependsOn: { key: 'stt.provider', value: 'tencent' } },

        { labelKey: 'settings.items.sttShortcutKey', descriptionKey: 'settings.items.sttShortcutKeyDesc', key: 'stt.shortcut_key', type: 'text', source: 'server' },

        // Microphone processing. Shared with meetings (the echo/noise/gain trio
        // is applied when the device is opened, so a meeting audience hears the
        // result too); the band filter only shapes what the recogniser gets.
        { labelKey: 'settings.items.sttMicEchoCancel', descriptionKey: 'settings.items.sttMicEchoCancelDesc', key: 'stt.mic_echo_cancel', type: 'switch', source: 'server', sectionHeader: 'settings.items.sttMicHeader' },
        { labelKey: 'settings.items.sttMicNoiseSuppress', descriptionKey: 'settings.items.sttMicNoiseSuppressDesc', key: 'stt.mic_noise_suppress', type: 'switch', source: 'server' },
        { labelKey: 'settings.items.sttMicAutoGain', descriptionKey: 'settings.items.sttMicAutoGainDesc', key: 'stt.mic_auto_gain', type: 'switch', source: 'server' },
        { labelKey: 'settings.items.sttMicBandFilter', descriptionKey: 'settings.items.sttMicBandFilterDesc', key: 'stt.mic_band_filter', type: 'switch', source: 'server' },

        // Voice gate. Shared with 听记, which uses the same threshold to decide
        // whether a segment has any speech worth uploading.
        { labelKey: 'settings.items.sttVoiceGate', descriptionKey: 'settings.items.sttVoiceGateDesc', key: 'stt.voice_gate', type: 'switch', source: 'server', sectionHeader: 'settings.items.sttVoiceGateHeader' },
        { labelKey: 'settings.items.sttVoiceGateSensitivity', descriptionKey: 'settings.items.sttVoiceGateSensitivityDesc', key: 'stt.voice_gate_sensitivity', type: 'select', source: 'server', options: [
          { labelKey: 'settings.items.sttVoiceGateConservative', value: 'conservative' },
          { labelKey: 'settings.items.sttVoiceGateStandard', value: 'standard' },
          { labelKey: 'settings.items.sttVoiceGateAggressive', value: 'aggressive' },
        ] },

        // Free-dialog mode. These only matter when the mode is switched on from
        // the chat action bar.
        { labelKey: 'settings.items.sttTriggerWords', descriptionKey: 'settings.items.sttTriggerWordsDesc', key: 'stt.trigger_words', type: 'text', source: 'server', sectionHeader: 'settings.items.sttFreeDialogHeader' },
        { labelKey: 'settings.items.sttSilenceSendSeconds', descriptionKey: 'settings.items.sttSilenceSendSecondsDesc', key: 'stt.silence_send_seconds', type: 'number', source: 'server', min: 0, max: 30, step: 1 },
        { labelKey: 'settings.items.sttIdleExitSeconds', descriptionKey: 'settings.items.sttIdleExitSecondsDesc', key: 'stt.idle_exit_seconds', type: 'number', source: 'server', min: 0, max: 600, step: 10 },
        { labelKey: 'settings.items.sttBargeIn', descriptionKey: 'settings.items.sttBargeInDesc', key: 'stt.barge_in', type: 'select', source: 'server', options: [
          { labelKey: 'settings.items.sttBargeInOff', value: 'off' },
          { labelKey: 'settings.items.sttBargeInAny', value: 'any' },
          { labelKey: 'settings.items.sttBargeInKeyword', value: 'keyword' },
        ] },
        { labelKey: 'settings.items.sttBargeInWords', descriptionKey: 'settings.items.sttBargeInWordsDesc', key: 'stt.barge_in_words', type: 'text', source: 'server', dependsOn: { key: 'stt.barge_in', value: 'keyword' } },
      ],
      requiredFields: ['stt.vllm.base_url', 'stt.vllm.model'],
      hasConnectivityTest: true,
      getTestCategories: (values) => [{ category: 'stt', values }],
    }},
  ],
  // Fields live in `plugins/live/settings.ts` — see PLUGIN_SETTINGS below.
  meeting: [],
  // 文本嵌入: the shared embedding service, its own page.
  //
  // Deliberately NOT a block inside 会话索引. The knowledge base calls the same
  // endpoint, and a shared service buried inside one consumer's page reads as
  // that consumer's private setting — which is exactly how the old
  // 「向量嵌入（知识库共用）」section was being read. One page owns the
  // endpoint, model, key and chunking; both consumers point here.
  embedding: [
    { type: 'panel', config: {
      panelId: 'embedding',
      commonFields: [
        // ── Service ──
        { labelKey: 'settings.items.embeddingEnabled', descriptionKey: 'settings.items.embeddingEnabledDesc', key: 'rag.vector_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.embeddingServiceSection' },
        { labelKey: 'settings.items.ragEmbedderStatus', descriptionKey: 'settings.items.ragEmbedderStatusDesc', key: 'rag.status.embedder_healthy', type: 'info', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragBaseUrl', descriptionKey: 'settings.items.ragBaseUrlDesc', key: 'rag.base_url', type: 'text', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragModel', descriptionKey: 'settings.items.ragModelDesc', key: 'rag.model', type: 'text', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragApiKey', descriptionKey: 'settings.items.ragApiKeyDesc', key: 'rag.api_key', type: 'password', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        // ── Chunking ──
        // Chunk size and overlap decide what gets embedded, and the knowledge
        // base chunks with the same numbers, so they belong with the service
        // rather than with either consumer.
        { labelKey: 'settings.items.ragChunkSize', descriptionKey: 'settings.items.ragChunkSizeDesc', key: 'rag.chunk_size', type: 'number', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true }, sectionHeader: 'settings.items.embeddingChunkSection' },
        { labelKey: 'settings.items.ragChunkOverlap', descriptionKey: 'settings.items.ragChunkOverlapDesc', key: 'rag.chunk_overlap', type: 'number', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
      ],
      requiredFields: ['rag.base_url'],
      hasConnectivityTest: (v) => !!v['rag.base_url'] && !!v['rag.vector_enabled'],
      getTestCategories: (values) => [{ category: 'rag', values }],
    }},
  ],
  // 会话索引: what the CHAT index is doing right now.
  //
  // Only chat counters live here. The knowledge base has its own queue, its own
  // failures and its own progress (shown on the 知识库 page and in its panel),
  // and the embedding endpoint is configured on 文本嵌入. Keeping the three
  // apart is what stops this page from reading as "the whole RAG status".
  rag: [
    { type: 'panel', config: {
      panelId: 'rag',
      commonFields: [
        { labelKey: 'settings.items.ragSearchMode', descriptionKey: 'settings.items.ragSearchModeDesc', key: 'rag.status.mode', type: 'info', source: 'server', sectionHeader: 'settings.items.ragStatusSectionHeader' },
        { labelKey: 'settings.items.ragIndexProgress', descriptionKey: 'settings.items.ragIndexProgressDesc', key: 'rag.status.index_progress', type: 'info', source: 'server' },
        { labelKey: 'settings.items.ragFtsSize', descriptionKey: 'settings.items.ragFtsSizeDesc', key: 'rag.status.fts_size', type: 'info', source: 'server' },
        { labelKey: 'settings.items.ragEmbedProgress', descriptionKey: 'settings.items.ragEmbedProgressDesc', key: 'rag.status.embed_progress', type: 'info', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragVecSize', descriptionKey: 'settings.items.ragVecSizeDesc', key: 'rag.status.vec_size', type: 'info', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
      ],
    }},
  ],
  // Knowledge base. `kb.enabled` is deliberately NOT here: it is a Dock feature
  // switch (it stops the indexer and closes the database), so it lives in the
  // feature manager and this page only reports its state. Everything else that
  // is `kb.*` belongs here — those rows used to have no UI at all.
  // Fields live in `plugins/kb/settings.ts` — see PLUGIN_SETTINGS below.
  kb: [],
  notifications: [],
  // Fields live in `plugins/proxy/settings.ts` — see PLUGIN_SETTINGS below.
  portForward: [],
}

// ── Sub-page panel map (data-driven third-level navigation) ────────

/**
 * Maps colon-separated sub-route IDs to their panel config and title i18n key.
 * Used by SettingsCategory.vue and SettingsPage.vue for data-driven sub-page rendering.
 *
 * Convention: sub-route ID = `{parentCategory}:{panelId}`
 * - Parent category contains a navigation action item that emits `navigate` with this ID
 * - SettingsCategory detects the colon and renders a standalone panel (no title)
 * - SettingsPage uses the titleKey for the header bar
 */
export const subPagePanelMap: Record<string, { panelConfig: GroupPanelConfig; titleKey: string }> = {
  'tts:tts_engine': {
    panelConfig: getCategoryPanels('tts_engine')[0],
    titleKey: 'settings.items.ttsEngine',
  },
}

/** Check if a category ID is a sub-page route (present in subPagePanelMap) */
export function isSubPageRoute(categoryId: string): boolean {
  return categoryId in subPagePanelMap
}

/**
 * Field lists contributed by the plugins themselves.
 *
 * A dock plugin owns a settings category (`settingsCategory`) and, until now,
 * its field list lived only in this file — so adding a setting meant editing a
 * 1300-line table in a different directory from the feature. Each plugin may now
 * ship `plugins/<id>/settings.ts` (a default export of `CategoryEntry[]`) and the
 * merge below puts it under that plugin's category.
 *
 * Why a glob here instead of a loader in the plugin runtime: this module is
 * imported by `SettingsCategory.vue`, `SettingsIndex.vue` and the restart dialog,
 * none of which otherwise care about plugins, and the plugin runtime would drag
 * the whole plugin graph (lucide + every panel) into them. `import.meta.glob` is
 * resolved at build time and only records module ids — the modules are the
 * plugins' own field tables, which import nothing but types.
 *
 * A category listed in BOTH places is a mistake, not a merge: two sources would
 * silently concatenate and the row order would depend on it. The guard below
 * throws at module load so it fails in tests instead of in front of a user.
 */
/**
 * Plugin directory → the settings category its `settings.ts` fills.
 *
 * Needed because the two names differ for five plugins. This is the one place
 * that pairing is written down for the field table; the plugin's own
 * `settingsCategory` is the runtime's copy (used for the gear button and the
 * index row), and `dockPluginManifest.test.ts` pins the two together.
 */
const PLUGIN_SETTINGS_CATEGORY: Record<string, string> = {
  browse: 'files',
  forge: 'forgeIntegration',
  history: 'gitHistory',
  kb: 'kb',
  live: 'meeting',
  mail: 'mail',
  notifications: 'notifications',
  proxy: 'portForward',
  share: 'share',
  stats: 'stats',
  tasks: 'tasks',
  terminal: 'terminal',
  view: 'fileView',
}

const PLUGIN_SETTINGS_MODULES = import.meta.glob<{ default?: CategoryEntry[] }>(
  '@/plugins/*/settings.ts',
  { eager: true },
)

/** Category id a plugin's `settings.ts` belongs to: its directory name. */
function pluginSettingsCategoryId(path: string): string | null {
  return path.match(/\/plugins\/([^/]+)\/settings\.ts$/)?.[1] ?? null
}

for (const [path, mod] of Object.entries(PLUGIN_SETTINGS_MODULES)) {
  const pluginId = pluginSettingsCategoryId(path)
  const entries = mod.default
  if (!pluginId || !entries) continue
  // The plugin's directory name is NOT always its settings category (browse →
  // files, view → fileView, forge → forgeIntegration, proxy → portForward,
  // live → meeting). The mapping is declared by the plugin as
  // `settingsCategory`; reading it from the plugin module would mean importing
  // the plugin graph, so it is stated once, here, next to the merge.
  const categoryId = PLUGIN_SETTINGS_CATEGORY[pluginId] ?? pluginId
  const existing = categoryItems[categoryId]
  if (existing && existing.length > 0) {
    throw new Error(
      `settingsFieldMap: category "${categoryId}" declares fields twice — once in this file and once in plugins/${pluginId}/settings.ts. Keep exactly one source.`,
    )
  }
  categoryItems[categoryId] = entries
}

/** Get sub-page panel config for a colon-separated sub-route ID */
export function getSubPagePanel(categoryId: string): GroupPanelConfig | undefined {
  return subPagePanelMap[categoryId]?.panelConfig
}

/** Get sub-page title i18n key for a colon-separated sub-route ID */
export function getSubPageTitleKey(categoryId: string): string | undefined {
  return subPagePanelMap[categoryId]?.titleKey
}

// ── Helpers ─────────────────────────────────────────────────

/** Build and return the mapping from server config dot-path keys to i18n label keys. */
export function getServerFieldToLabelKey(): Record<string, string> {
  const map: Record<string, string> = {}
  for (const entries of Object.values(categoryItems)) {
    for (const entry of entries) {
      if (entry.type === 'item') {
        if (entry.spec.source === 'server') map[entry.spec.key] = entry.spec.labelKey
      } else {
        const cfg = entry.config
        if (cfg.enableKey && cfg.enableLabelKey) map[cfg.enableKey] = cfg.enableLabelKey
        if (cfg.entrySelector?.source === 'server') map[cfg.entrySelector.key] = cfg.entrySelector.labelKey
        for (const f of cfg.commonFields) {
          if (f.source === 'server') map[f.key] = f.labelKey
        }
        for (const osf of cfg.optionSubFields ?? []) {
          for (const f of osf.fields) {
            if (f.source === 'server') map[f.key] = f.labelKey
          }
        }
      }
    }
  }
  // Switches that moved to the Dock feature manager are no longer settings rows,
  // but they are still server config keys: if one ever lands in a restart
  // dialog's changed_cold_fields, the dialog must name it instead of printing
  // the raw dot-path.
  map['terminal.enabled'] = 'settings.items.terminalEnabled'
  map['mail.enabled'] = 'settings.items.mailEnabled'
  map['forge.enabled'] = 'settings.items.forgeEnabled'
  return map
}

/** Pre-computed singleton — used by SettingsRestartDialog to translate field paths. */
export const serverFieldToLabelKey: Record<string, string> = getServerFieldToLabelKey()

/** Check if a category has any panel entries. */
export function categoryHasPanels(categoryId: string): boolean {
  const entries = categoryItems[categoryId]
  return entries?.some(e => e.type === 'panel') ?? false
}

/** Check if a category consists entirely of panel entries (no flat items). */
export function isPanelOnlyCategory(categoryId: string): boolean {
  const entries = categoryItems[categoryId]
  if (!entries || entries.length === 0) return false
  return entries.every(e => e.type === 'panel')
}

/** Get all panel configs for a category. */
export function getCategoryPanels(categoryId: string): GroupPanelConfig[] {
  const entries = categoryItems[categoryId] ?? []
  return entries.filter((e): e is CategoryEntry & { type: 'panel' } => e.type === 'panel').map(e => e.config)
}

/**
 * Voice options per TTS engine.
 * Used by SettingsGroupPanel.vue to dynamically resolve tts.voice select options
 * based on the currently selected tts.engine value (local preview inside panel).
 *
 * Labels are i18n keys — resolved at render time for locale support.
 */
export const engineVoiceOptions: Record<string, { labelKey: string; value: string }[]> = {
  edge: [
    { labelKey: 'settings.items.voiceEdgeXiaoxiao', value: 'zh-CN-XiaoxiaoNeural' },
    { labelKey: 'settings.items.voiceEdgeYunxi', value: 'zh-CN-YunxiNeural' },
    { labelKey: 'settings.items.voiceEdgeYunjian', value: 'zh-CN-YunjianNeural' },
    { labelKey: 'settings.items.voiceEdgeXiaoyi', value: 'zh-CN-XiaoyiNeural' },
    { labelKey: 'settings.items.voiceEdgeXiaochen', value: 'zh-CN-XiaochenNeural' },
    { labelKey: 'settings.items.voiceEdgeXiaohan', value: 'zh-CN-XiaohanNeural' },
    { labelKey: 'settings.items.voiceEdgeXiaomo', value: 'zh-CN-XiaomoNeural' },
    { labelKey: 'settings.items.voiceEdgeXiaorui', value: 'zh-CN-XiaoruiNeural' },
    { labelKey: 'settings.items.voiceEdgeYunyang', value: 'zh-CN-YunyangNeural' },
    { labelKey: 'settings.items.voiceEdgeYunhao', value: 'zh-CN-YunhaoNeural' },
    { labelKey: 'settings.items.voiceEdgeJenny', value: 'en-US-JennyNeural' },
    { labelKey: 'settings.items.voiceEdgeGuy', value: 'en-US-GuyNeural' },
    { labelKey: 'settings.items.voiceEdgeAria', value: 'en-US-AriaNeural' },
    { labelKey: 'settings.items.voiceEdgeDavis', value: 'en-US-DavisNeural' },
  ],
}
