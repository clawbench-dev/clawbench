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

export interface ItemSpec {
  labelKey: string
  descriptionKey?: string
  key: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action' | 'info' | 'header' | 'password' | 'textarea'
  source: 'server' | 'local'
  needsRestart?: boolean
  options?: { labelKey: string; value: unknown }[]
  min?: number
  max?: number
  step?: number
  /** Hide this field when the condition is NOT met. */
  dependsOn?: DependsOn | DependsOn[]
  /** Gray out (disable) this field when the condition is NOT met, but keep it visible. */
  disableUnless?: DependsOn | DependsOn[]
  sectionHeader?: string
  /** Transform raw value for display (e.g., 0 → 'auto' for port_forward.port) */
  displayTransform?: (value: unknown) => unknown
  defaultValue?: unknown
  displayFormat?: 'percent' | 'raw'
  /** Only show this item when running inside the Android app */
  appOnly?: boolean
  /** For action items: navigate to this category sub-route ID on click */
  navigateTo?: string
  /** Progress bar for info-type items: { value, max }. Bar hidden when value >= max. */
  progress?: { value: number; max: number }
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
  /** Override which field drives optionSubFields matching. Defaults to entrySelector key.
   *  Used by FRP where sub-fields are keyed by frp.auto_port, not the entry selector. */
  optionSubFieldsKey?: string
  /** Whether this panel has a connectivity test button. Can be a static boolean or a function that checks current values. */
  hasConnectivityTest?: boolean | ((values: Record<string, unknown>) => boolean)
  /** Function to map panel values to backend test categories.
   *  Receives current localValues, returns array of { category, values }.
   *  If hasConnectivityTest is true but this is undefined, defaults to
   *  [{ category: panelId, values }]. */
  getTestCategories?: (values: Record<string, unknown>) => Array<{ category: string; values: Record<string, unknown> }>
  /** Side effect: called after successful save with changed keys and current panel values */
  afterSave?: (changedKeys: string[], values?: Record<string, unknown>) => void
  /** Side effect: called on panel mount (e.g., fetch FRP info) */
  onInit?: () => void
  /** Whether this panel needs voice reset on engine change (TTS only) */
  needsVoiceReset?: boolean
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
  appearance: [
    { type: 'item', spec: { labelKey: 'settings.items.theme', descriptionKey: 'settings.items.themeDesc', key: 'theme', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.themeAuto', value: 'auto' },
      { labelKey: 'settings.items.themeLight', value: 'light' },
      { labelKey: 'settings.items.themeDark', value: 'dark' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.locale', descriptionKey: 'settings.items.localeDesc', key: 'locale', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.localeZh', value: 'zh' },
      { labelKey: 'settings.items.localeEn', value: 'en' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.uiScale', descriptionKey: 'settings.items.uiScaleDesc', key: 'uiScale', type: 'slider', source: 'local', min: 0.8, max: 1.5, step: 0.05, defaultValue: 1, displayFormat: 'percent' } },
  ],
  agents: [],
  chat: [
    { type: 'item', spec: { labelKey: 'settings.items.autoSpeech', descriptionKey: 'settings.items.autoSpeechDesc', key: 'autoSpeech', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.preventScreenLock', descriptionKey: 'settings.items.preventScreenLockDesc', key: 'preventScreenLock', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.swipeSession', descriptionKey: 'settings.items.swipeSessionDesc', key: 'swipeSession', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatInitialMessages', descriptionKey: 'settings.items.chatInitialMessagesDesc', key: 'chat.initial_messages', type: 'number', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatPageSize', descriptionKey: 'settings.items.chatPageSizeDesc', key: 'chat.page_size', type: 'number', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.chatSystemPromptInterval', descriptionKey: 'settings.items.chatSystemPromptIntervalDesc', key: 'chat.system_prompt_interval', type: 'number', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.sessionMaxCount', descriptionKey: 'settings.items.sessionMaxCountDesc', key: 'session.max_count', type: 'number', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.summarizeTextSection', descriptionKey: 'settings.items.summarizeTextBackendDesc', key: 'navigateSummarizeText', type: 'action', source: 'local', navigateTo: 'chat:summarization_text' } },
  ],
  projectFiles: [
    { type: 'item', spec: { labelKey: 'settings.items.recentProjectsMaxCount', descriptionKey: 'settings.items.recentProjectsMaxCountDesc', key: 'recent_projects.max_count', type: 'number', source: 'server', min: 1, sectionHeader: 'settings.items.projectSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.fileSearchDisplayLimit', descriptionKey: 'settings.items.fileSearchDisplayLimitDesc', key: 'file_search.display_limit', type: 'number', source: 'server', min: 10, max: 500, sectionHeader: 'settings.items.searchSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.showHidden', descriptionKey: 'settings.items.showHiddenDesc', key: 'showHidden', type: 'switch', source: 'local', sectionHeader: 'settings.items.filesSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.recentFilesCount', descriptionKey: 'settings.items.recentFilesCountDesc', key: 'recentFilesCount', type: 'number', source: 'local', min: 1, max: 50 } },
    { type: 'item', spec: { labelKey: 'settings.items.wordWrap', descriptionKey: 'settings.items.wordWrapDesc', key: 'wordWrap', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.lineNumbers', descriptionKey: 'settings.items.lineNumbersDesc', key: 'lineNumbers', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.stickyScroll', descriptionKey: 'settings.items.stickyScrollDesc', key: 'stickyScroll', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.fileView', descriptionKey: 'settings.items.fileViewDesc', key: 'fileView', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.fileViewList', value: 'list' },
      { labelKey: 'settings.items.fileViewGrid', value: 'grid' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.sortField', descriptionKey: 'settings.items.sortFieldDesc', key: 'sortField', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.sortFieldDefault', value: null },
      { labelKey: 'settings.items.sortFieldName', value: 'name' },
      { labelKey: 'settings.items.sortFieldTime', value: 'time' },
      { labelKey: 'settings.items.sortFieldType', value: 'type' },
      { labelKey: 'settings.items.sortFieldSize', value: 'size' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.sortDir', descriptionKey: 'settings.items.sortDirHint', key: 'sortDir', type: 'select', source: 'local', dependsOn: { key: 'sortField', values: ['name', 'time', 'type', 'size'] }, options: [
      { labelKey: 'settings.items.sortDirAsc', value: 'asc' },
      { labelKey: 'settings.items.sortDirDesc', value: 'desc' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.uploadMaxSize', descriptionKey: 'settings.items.uploadMaxSizeDesc', key: 'upload.max_size_mb', type: 'number', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.uploadMaxFiles', descriptionKey: 'settings.items.uploadMaxFilesDesc', key: 'upload.max_files', type: 'number', source: 'server' } },
  ],
  debug: [
    { type: 'item', spec: { labelKey: 'settings.items.logCapture', descriptionKey: 'settings.items.logCaptureDesc', key: 'logCapture', type: 'switch', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.reconfigureServer', descriptionKey: 'settings.items.reconfigureServerDesc', key: 'reconfigureServer', type: 'action', source: 'local', appOnly: true } },
  ],
  security: [
    { type: 'item', spec: { labelKey: 'settings.items.localhostAuthExempt', descriptionKey: 'settings.items.localhostAuthExemptDesc', key: 'localhost_auth_exempt', type: 'switch', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.changePassword', descriptionKey: 'settings.items.changePasswordDesc', key: 'changePassword', type: 'action', source: 'local' } },
  ],
  notification: [
    { type: 'panel', config: {
      panelId: 'push',
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
            const native = (window as unknown as { AndroidNative?: { setNativePushEnabled?: (v: boolean) => void } }).AndroidNative
            native?.setNativePushEnabled?.(values?.push_mode === 'native')
          } catch { /* not in app mode */ }
        }
      },
    }},
  ],
  about: [
    { type: 'item', spec: { labelKey: 'settings.items.aboutServerVersion', descriptionKey: 'settings.items.aboutServerVersionDesc', key: 'serverVersion', type: 'info', source: 'server' } },
    { type: 'item', spec: { labelKey: 'settings.items.aboutAppVersion', descriptionKey: 'settings.items.aboutAppVersionDesc', key: 'appVersion', type: 'info', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.addToHomeScreen', descriptionKey: 'settings.items.addToHomeScreenDesc', key: 'addToHomeScreen', type: 'action', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.downloadAndroidApp', descriptionKey: 'settings.items.downloadAndroidAppDesc', key: 'downloadAndroidApp', type: 'action', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.showWelcome', descriptionKey: 'settings.items.showWelcomeDesc', key: 'showWelcome', type: 'action', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.restartServer', descriptionKey: 'settings.items.restartServerDesc', key: 'restartServer', type: 'action', source: 'local' } },
    { type: 'item', spec: { labelKey: 'settings.items.checkUpgrade', descriptionKey: 'settings.items.checkUpgradeDesc', key: 'checkUpgrade', type: 'action', source: 'local' } },
  ],

  // ── Panel-only categories (formerly drill-down) ────────────

  terminal: [
    { type: 'panel', config: {
      panelId: 'terminal',
      enableKey: 'terminal.enabled',
      enableLabelKey: 'settings.items.terminalEnabled',
      commonFields: [
        { labelKey: 'settings.items.terminalFontSize', descriptionKey: 'settings.items.terminalFontSizeDesc', key: 'terminalFontSize', type: 'slider', source: 'local', min: 10, max: 24, step: 1, defaultValue: 12 },
        { labelKey: 'settings.items.terminalIdleTimeout', descriptionKey: 'settings.items.terminalIdleTimeoutDesc', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
        { labelKey: 'settings.items.terminalMaxSessions', descriptionKey: 'settings.items.terminalMaxSessionsDesc', key: 'terminal.max_sessions', type: 'number', source: 'server' },
        { labelKey: 'settings.items.terminalBufferLines', descriptionKey: 'settings.items.terminalBufferLinesDesc', key: 'terminal.buffer_lines', type: 'number', source: 'server' },
      ],
      afterSave(changedKeys) {
        if (changedKeys.includes('terminal.enabled')) {
          import('@/composables/useTerminalStatus').then(m => m.useTerminalStatus().loadTerminalStatus()).catch(() => {})
        }
      },
    }},
  ],
  tts: [
    { type: 'item', spec: { labelKey: 'settings.items.ttsEngine', descriptionKey: 'settings.items.ttsEngineDesc', key: 'navigateTtsEngine', type: 'action', source: 'local', navigateTo: 'tts:tts_engine' } },
    { type: 'item', spec: { labelKey: 'settings.items.summarizeTtsSection', descriptionKey: 'settings.items.summarizeTtsBackendDesc', key: 'navigateSummarizeVoice', type: 'action', source: 'local', navigateTo: 'tts:summarization_voice' } },
  ],
  tts_engine: [
    { type: 'panel', config: {
      panelId: 'tts',
      entrySelector: { labelKey: 'settings.items.ttsEngine', descriptionKey: 'settings.items.ttsEngineDesc', key: 'tts.engine', type: 'select', source: 'server', options: [
        { labelKey: 'settings.items.ttsEngineEdge', value: 'edge' },
        { labelKey: 'settings.items.ttsEnginePiper', value: 'piper' },
        { labelKey: 'settings.items.ttsEngineKokoro', value: 'kokoro' },
        { labelKey: 'settings.items.ttsEngineMossNano', value: 'moss-nano' },
      ]},
      commonFields: [
        { labelKey: 'settings.items.ttsVoice', descriptionKey: 'settings.items.ttsVoiceDesc', key: 'tts.voice', type: 'select', source: 'server' },
        { labelKey: 'settings.items.ttsSpeed', descriptionKey: 'settings.items.ttsSpeedDesc', key: 'tts.speed', type: 'slider', source: 'server', min: 0.5, max: 3, step: 0.1 },
        { labelKey: 'settings.items.ttsMaxCacheFiles', descriptionKey: 'settings.items.ttsMaxCacheFilesDesc', key: 'tts.max_cache_files', type: 'number', source: 'server' },
      ],
      optionSubFields: [
        {
          when: 'piper',
          fields: [
            { labelKey: 'settings.items.piperModelPath', descriptionKey: 'settings.items.piperModelPathDesc', key: 'tts.piper.model_path', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsPiperHeader' },
            { labelKey: 'settings.items.piperNoiseScale', descriptionKey: 'settings.items.piperNoiseScaleDesc', key: 'tts.piper.noise_scale', type: 'number', source: 'server', min: 0, max: 1, step: 0.001 },
            { labelKey: 'settings.items.piperLengthScale', descriptionKey: 'settings.items.piperLengthScaleDesc', key: 'tts.piper.length_scale', type: 'number', source: 'server', min: 0.1, max: 5, step: 0.1 },
            { labelKey: 'settings.items.piperSentenceSilence', descriptionKey: 'settings.items.piperSentenceSilenceDesc', key: 'tts.piper.sentence_silence', type: 'number', source: 'server', min: 0, max: 5, step: 0.1 },
          ],
        },
        {
          when: 'kokoro',
          fields: [
            { labelKey: 'settings.items.kokoroModelPath', descriptionKey: 'settings.items.kokoroModelPathDesc', key: 'tts.kokoro.model_path', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsKokoroHeader' },
            { labelKey: 'settings.items.kokoroVoicesPath', descriptionKey: 'settings.items.kokoroVoicesPathDesc', key: 'tts.kokoro.voices_path', type: 'text', source: 'server' },
            { labelKey: 'settings.items.kokoroLang', descriptionKey: 'settings.items.kokoroLangDesc', key: 'tts.kokoro.lang', type: 'text', source: 'server' },
          ],
        },
        {
          when: 'moss-nano',
          fields: [
            { labelKey: 'settings.items.mossNanoModelDir', descriptionKey: 'settings.items.mossNanoModelDirDesc', key: 'tts.moss_nano.model_dir', type: 'text', source: 'server', sectionHeader: 'settings.items.ttsMossNanoHeader' },
            { labelKey: 'settings.items.mossNanoBackend', descriptionKey: 'settings.items.mossNanoBackendDesc', key: 'tts.moss_nano.backend', type: 'select', source: 'server', options: [
              { labelKey: 'settings.items.mossNanoBackendOnnx', value: 'onnx' },
              { labelKey: 'settings.items.mossNanoBackendPytorch', value: 'pytorch' },
            ]},
          ],
        },
      ],
      requiredFields: ['tts.piper.model_path', 'tts.kokoro.model_path', 'tts.kokoro.voices_path'],
      hasConnectivityTest: true,
      getTestCategories: (values) => [{ category: 'tts', values }],
      needsVoiceReset: true,
    }},
  ],
  summarization_text: [
    { type: 'panel', config: {
      panelId: 'summarization_text',
      commonFields: [
        { labelKey: 'settings.items.summarizeTextBackend', descriptionKey: 'settings.items.summarizeTextBackendDesc', key: 'summarize.backend', type: 'select', source: 'server', options: [
          { labelKey: 'settings.items.summarizeDisabled', value: '' },
          { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
          { labelKey: 'settings.items.summarizeApi', value: 'api' },
        ]},
        { labelKey: 'settings.items.apiBaseUrl', descriptionKey: 'settings.items.apiBaseUrlDesc', key: 'summarize.api.base_url', type: 'text', source: 'server', sectionHeader: 'settings.items.apiHeader', dependsOn: { key: 'summarize.backend', values: ['api'] } },
        { labelKey: 'settings.items.summarizeModel', descriptionKey: 'settings.items.summarizeModelDesc', key: 'summarize.model', type: 'text', source: 'server', dependsOn: { key: 'summarize.backend', values: ['api'] } },
        { labelKey: 'settings.items.apiKey', descriptionKey: 'settings.items.apiKeyDesc', key: 'summarize.api.key', type: 'password', source: 'server', dependsOn: { key: 'summarize.backend', values: ['api'] } },
      ],
      requiredFields: ['summarize.api.base_url'],
      hasConnectivityTest: (v) => v['summarize.backend'] === 'api',
      getTestCategories(values) {
        return [{ category: 'summarize_text', values }]
      },
    }},
  ],
  summarization_voice: [
    { type: 'panel', config: {
      panelId: 'summarization_voice',
      commonFields: [
        { labelKey: 'settings.items.summarizeTtsBackend', descriptionKey: 'settings.items.summarizeTtsBackendDesc', key: 'summarize.tts_backend', type: 'select', source: 'server', options: [
          { labelKey: 'settings.items.summarizeDisabled', value: '' },
          { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
          { labelKey: 'settings.items.summarizeApi', value: 'api' },
        ]},
        { labelKey: 'settings.items.ttsApiBaseUrl', descriptionKey: 'settings.items.ttsApiBaseUrlDesc', key: 'summarize.tts_api.base_url', type: 'text', source: 'server', sectionHeader: 'settings.items.summarizeTtsApiHeader', dependsOn: { key: 'summarize.tts_backend', values: ['api'] } },
        { labelKey: 'settings.items.summarizeTtsModel', descriptionKey: 'settings.items.summarizeTtsModelDesc', key: 'summarize.tts_model', type: 'text', source: 'server', dependsOn: { key: 'summarize.tts_backend', values: ['api'] } },
        { labelKey: 'settings.items.ttsApiKey', descriptionKey: 'settings.items.ttsApiKeyDesc', key: 'summarize.tts_api.key', type: 'password', source: 'server', dependsOn: { key: 'summarize.tts_backend', values: ['api'] } },
      ],
      requiredFields: ['summarize.tts_api.base_url'],
      hasConnectivityTest: (v) => v['summarize.tts_backend'] === 'api',
      getTestCategories(values) {
        return [{ category: 'summarize_voice', values }]
      },
    }},
  ],
  rag: [
    { type: 'panel', config: {
      panelId: 'rag',
      commonFields: [
        // ── Vector embedding toggle ──
        { labelKey: 'settings.items.ragEnabled', key: 'rag.vector_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.ragVectorSectionHeader' },
        { labelKey: 'settings.items.ragBaseUrl', descriptionKey: 'settings.items.ragBaseUrlDesc', key: 'rag.base_url', type: 'text', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragModel', descriptionKey: 'settings.items.ragModelDesc', key: 'rag.model', type: 'text', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragApiKey', descriptionKey: 'settings.items.ragApiKeyDesc', key: 'rag.api_key', type: 'password', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        { labelKey: 'settings.items.ragEmbedderStatus', descriptionKey: 'settings.items.ragEmbedderStatusDesc', key: 'rag.status.embedder_healthy', type: 'info', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
        // ── FTS settings (always visible) ──
        { labelKey: 'settings.items.ragChunkSize', descriptionKey: 'settings.items.ragChunkSizeDesc', key: 'rag.chunk_size', type: 'number', source: 'server' },
        { labelKey: 'settings.items.ragSearchLimit', descriptionKey: 'settings.items.ragSearchLimitDesc', key: 'rag.search_limit', type: 'number', source: 'server' },
        { labelKey: 'settings.items.ragSearchPoolSize', descriptionKey: 'settings.items.ragSearchPoolSizeDesc', key: 'rag.search_pool_size', type: 'number', source: 'server' },
        { labelKey: 'settings.items.ragRetentionDays', descriptionKey: 'settings.items.ragRetentionDaysDesc', key: 'rag.retention_days', type: 'number', source: 'server' },
        // ── Status indicators ──
        { labelKey: 'settings.items.ragSearchMode', descriptionKey: 'settings.items.ragSearchModeDesc', key: 'rag.status.mode', type: 'info', source: 'server' },
        { labelKey: 'settings.items.ragIndexProgress', descriptionKey: 'settings.items.ragIndexProgressDesc', key: 'rag.status.index_progress', type: 'info', source: 'server' },
        { labelKey: 'settings.items.ragEmbedProgress', descriptionKey: 'settings.items.ragEmbedProgressDesc', key: 'rag.status.embed_progress', type: 'info', source: 'server', disableUnless: { key: 'rag.vector_enabled', value: true } },
      ],
      requiredFields: ['rag.base_url'],
      hasConnectivityTest: (v) => !!v['rag.base_url'] && !!v['rag.vector_enabled'],
      getTestCategories: (values) => [{ category: 'rag', values }],
    }},
  ],
  portForward: [
    { type: 'panel', config: {
      panelId: 'portForward',
      enableKey: 'port_forward.enabled',
      enableLabelKey: 'settings.items.portForwardEnabled',
      commonFields: [
        { labelKey: 'settings.items.portForwardPort', descriptionKey: 'settings.items.portForwardPortDesc', key: 'port_forward.port', type: 'number', source: 'server', displayTransform: (v: unknown) => v === 0 ? '__auto__' : v },
      ],
      hasConnectivityTest: true,
      getTestCategories: (values) => [{ category: 'port_forward', values }],
      afterSave(changedKeys) {
        if (changedKeys.includes('port_forward.enabled')) {
          import('@/composables/usePortForward').then(m => m.usePortForward().loadSSHInfo()).catch(() => {})
        }
      },
    }},
  ],
  frp: [
    { type: 'panel', config: {
      panelId: 'frp',
      enableKey: 'frp.enabled',
      enableLabelKey: 'settings.items.frpEnabled',
      commonFields: [
        { labelKey: 'settings.items.frpServerAddr', descriptionKey: 'settings.items.frpServerAddrDesc', key: 'frp.server_addr', type: 'text', source: 'server' },
        { labelKey: 'settings.items.frpServerPort', descriptionKey: 'settings.items.frpServerPortDesc', key: 'frp.server_port', type: 'number', source: 'server' },
        { labelKey: 'settings.items.frpToken', descriptionKey: 'settings.items.frpTokenDesc', key: 'frp.token', type: 'password', source: 'server' },
        { labelKey: 'settings.items.frpAutoPort', descriptionKey: 'settings.items.frpAutoPortDesc', key: 'frp.auto_port', type: 'switch', source: 'server' },
      ],
      optionSubFields: [
        {
          when: false,
          fields: [
            { labelKey: 'settings.items.frpRemotePort', descriptionKey: 'settings.items.frpRemotePortDesc', key: 'frp.remote_port', type: 'number', source: 'server' },
            { labelKey: 'settings.items.frpSSHRemotePort', descriptionKey: 'settings.items.frpSSHRemotePortDesc', key: 'frp.ssh_remote_port', type: 'number', source: 'server' },
          ],
        },
      ],
      requiredFields: ['frp.server_addr'],
      optionSubFieldsKey: 'frp.auto_port',
      hasConnectivityTest: true,
      getTestCategories: (values) => [{ category: 'frp', values }],
      afterSave(changedKeys) {
        if (changedKeys.includes('frp.enabled')) {
          import('@/composables/useFrp').then(m => m.useFrp().fetchFrpInfo()).catch(() => {})
        }
      },
      onInit() {
        import('@/composables/useFrp').then(m => m.useFrp().fetchFrpInfo()).catch(() => {})
      },

    }},
  ],
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
  'chat:summarization_text': {
    panelConfig: getCategoryPanels('summarization_text')[0],
    titleKey: 'settings.items.summarizeTextSection',
  },
  'tts:summarization_voice': {
    panelConfig: getCategoryPanels('summarization_voice')[0],
    titleKey: 'settings.items.summarizeTtsSection',
  },
  'tts:tts_engine': {
    panelConfig: getCategoryPanels('tts_engine')[0],
    titleKey: 'settings.items.ttsEngine',
  },
}

/** Check if a category ID is a sub-page route (present in subPagePanelMap) */
export function isSubPageRoute(categoryId: string): boolean {
  return categoryId in subPagePanelMap
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
  piper: [
    { labelKey: 'settings.items.voicePiperHuayanMedium', value: 'zh_CN-huayan-medium' },
    { labelKey: 'settings.items.voicePiperHuayanXLow', value: 'zh_CN-huayan-x_low' },
    { labelKey: 'settings.items.voicePiperChaowenMedium', value: 'zh_CN-chaowen-medium' },
    { labelKey: 'settings.items.voicePiperLessacMedium', value: 'en_US-lessac-medium' },
    { labelKey: 'settings.items.voicePiperLibrittsHigh', value: 'en_US-libritts-high' },
  ],
  kokoro: [
    { labelKey: 'settings.items.voiceKokoroZf001', value: 'zf_001' },
    { labelKey: 'settings.items.voiceKokoroZf002', value: 'zf_002' },
    { labelKey: 'settings.items.voiceKokoroZf003', value: 'zf_003' },
    { labelKey: 'settings.items.voiceKokoroZf004', value: 'zf_004' },
    { labelKey: 'settings.items.voiceKokoroZf005', value: 'zf_005' },
    { labelKey: 'settings.items.voiceKokoroZf006', value: 'zf_006' },
    { labelKey: 'settings.items.voiceKokoroZf007', value: 'zf_007' },
    { labelKey: 'settings.items.voiceKokoroZf008', value: 'zf_008' },
    { labelKey: 'settings.items.voiceKokoroZm009', value: 'zm_009' },
    { labelKey: 'settings.items.voiceKokoroZm010', value: 'zm_010' },
    { labelKey: 'settings.items.voiceKokoroZm011', value: 'zm_011' },
    { labelKey: 'settings.items.voiceKokoroZfXiaobei', value: 'zf_xiaobei' },
    { labelKey: 'settings.items.voiceKokoroZfShanshan', value: 'zf_shanshan' },
    { labelKey: 'settings.items.voiceKokoroZfXiaoyi', value: 'zf_xiaoyi' },
    { labelKey: 'settings.items.voiceKokoroZmYunxi', value: 'zm_yunxi' },
    { labelKey: 'settings.items.voiceKokoroZmYunjian', value: 'zm_yunjian' },
  ],
  'moss-nano': [
    { labelKey: 'settings.items.voiceMossJunhao', value: 'Junhao' },
    { labelKey: 'settings.items.voiceMossZhiming', value: 'Zhiming' },
    { labelKey: 'settings.items.voiceMossWeiguo', value: 'Weiguo' },
    { labelKey: 'settings.items.voiceMossXiaoyu', value: 'Xiaoyu' },
    { labelKey: 'settings.items.voiceMossYuewen', value: 'Yuewen' },
    { labelKey: 'settings.items.voiceMossLingyu', value: 'Lingyu' },
    { labelKey: 'settings.items.voiceMossTrump', value: 'Trump' },
    { labelKey: 'settings.items.voiceMossAva', value: 'Ava' },
    { labelKey: 'settings.items.voiceMossBella', value: 'Bella' },
    { labelKey: 'settings.items.voiceMossAdam', value: 'Adam' },
    { labelKey: 'settings.items.voiceMossNathan', value: 'Nathan' },
    { labelKey: 'settings.items.voiceMossSakura', value: 'Sakura' },
    { labelKey: 'settings.items.voiceMossYui', value: 'Yui' },
    { labelKey: 'settings.items.voiceMossAoi', value: 'Aoi' },
    { labelKey: 'settings.items.voiceMossHina', value: 'Hina' },
    { labelKey: 'settings.items.voiceMossMei', value: 'Mei' },
  ],
}
