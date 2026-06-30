/**
 * Centralized settings item definitions — the single source of truth.
 *
 * Used by:
 * - SettingsCategory.vue (renders the UI)
 * - SettingsRestartDialog.vue (translates changed_cold_fields via serverFieldToLabelKey)
 *
 * Adding a new setting? Add it here. Both the category page and the restart
 * dialog will pick it up automatically — no manual sync required.
 */

export interface DependsOn {
  key: string
  value?: any
  values?: any[]
}

export interface ItemSpec {
  labelKey: string
  descriptionKey?: string
  key: string
  type: 'switch' | 'select' | 'number' | 'text' | 'slider' | 'action' | 'info' | 'header' | 'password'
  source: 'server' | 'local'
  needsRestart?: boolean
  options?: { labelKey: string; value: any }[]
  min?: number
  max?: number
  step?: number
  dependsOn?: DependsOn | DependsOn[]
  sectionHeader?: string
}

/**
 * Complete category → items mapping.
 * The `agents` category has a static `default_agent` entry here;
 * the agent list is rendered dynamically by SettingsAgentsIndex.vue.
 */
export const categoryItems: Record<string, ItemSpec[]> = {
  appearance: [
    { labelKey: 'settings.items.theme', descriptionKey: 'settings.items.themeDesc', key: 'theme', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.themeAuto', value: 'auto' },
      { labelKey: 'settings.items.themeLight', value: 'light' },
      { labelKey: 'settings.items.themeDark', value: 'dark' },
    ]},
    { labelKey: 'settings.items.locale', descriptionKey: 'settings.items.localeDesc', key: 'locale', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.localeZh', value: 'zh' },
      { labelKey: 'settings.items.localeEn', value: 'en' },
    ]},
  ],
  agents: [
    { labelKey: 'settings.items.defaultAgent', descriptionKey: 'settings.items.defaultAgentDesc', key: 'default_agent', type: 'select', source: 'server', needsRestart: false },
  ],
  chat: [
    { labelKey: 'settings.items.autoSpeech', descriptionKey: 'settings.items.autoSpeechDesc', key: 'autoSpeech', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.preventScreenLock', descriptionKey: 'settings.items.preventScreenLockDesc', key: 'preventScreenLock', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.swipeSession', descriptionKey: 'settings.items.swipeSessionDesc', key: 'swipeSession', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.chatInitialMessages', descriptionKey: 'settings.items.chatInitialMessagesDesc', key: 'chat.initial_messages', type: 'number', source: 'server' },
    { labelKey: 'settings.items.chatPageSize', descriptionKey: 'settings.items.chatPageSizeDesc', key: 'chat.page_size', type: 'number', source: 'server' },
    { labelKey: 'settings.items.chatSystemPromptInterval', descriptionKey: 'settings.items.chatSystemPromptIntervalDesc', key: 'chat.system_prompt_interval', type: 'number', source: 'server' },
    { labelKey: 'settings.items.sessionMaxCount', descriptionKey: 'settings.items.sessionMaxCountDesc', key: 'session.max_count', type: 'number', source: 'server' },
  ],
  project: [
    { labelKey: 'settings.items.recentProjectsMaxCount', descriptionKey: 'settings.items.recentProjectsMaxCountDesc', key: 'recent_projects.max_count', type: 'number', source: 'server', min: 1 },
  ],
  files: [
    { labelKey: 'settings.items.showHidden', descriptionKey: 'settings.items.showHiddenDesc', key: 'showHidden', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.wordWrap', descriptionKey: 'settings.items.wordWrapDesc', key: 'wordWrap', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.lineNumbers', descriptionKey: 'settings.items.lineNumbersDesc', key: 'lineNumbers', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.stickyScroll', descriptionKey: 'settings.items.stickyScrollDesc', key: 'stickyScroll', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.fileView', descriptionKey: 'settings.items.fileViewDesc', key: 'fileView', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.fileViewList', value: 'list' },
      { labelKey: 'settings.items.fileViewGrid', value: 'grid' },
    ]},
    { labelKey: 'settings.items.sortField', descriptionKey: 'settings.items.sortFieldDesc', key: 'sortField', type: 'select', source: 'local', options: [
      { labelKey: 'settings.items.sortFieldDefault', value: null },
      { labelKey: 'settings.items.sortFieldName', value: 'name' },
      { labelKey: 'settings.items.sortFieldTime', value: 'time' },
      { labelKey: 'settings.items.sortFieldType', value: 'type' },
      { labelKey: 'settings.items.sortFieldSize', value: 'size' },
    ]},
    { labelKey: 'settings.items.sortDir', descriptionKey: 'settings.items.sortDirHint', key: 'sortDir', type: 'select', source: 'local', dependsOn: { key: 'sortField', values: ['name', 'time', 'type', 'size'] }, options: [
      { labelKey: 'settings.items.sortDirAsc', value: 'asc' },
      { labelKey: 'settings.items.sortDirDesc', value: 'desc' },
    ]},
    { labelKey: 'settings.items.uploadMaxSize', descriptionKey: 'settings.items.uploadMaxSizeDesc', key: 'upload.max_size_mb', type: 'number', source: 'server' },
    { labelKey: 'settings.items.uploadMaxFiles', descriptionKey: 'settings.items.uploadMaxFilesDesc', key: 'upload.max_files', type: 'number', source: 'server' },
  ],
  terminal: [
    { labelKey: 'settings.items.terminalFontSize', descriptionKey: 'settings.items.terminalFontSizeDesc', key: 'terminalFontSize', type: 'slider', source: 'local', min: 10, max: 24, step: 1 },
    { labelKey: 'settings.items.terminalEnabled', descriptionKey: 'settings.items.terminalEnabledDesc', key: 'terminal.enabled', type: 'switch', source: 'server' },
    { labelKey: 'settings.items.terminalIdleTimeout', descriptionKey: 'settings.items.terminalIdleTimeoutDesc', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
    { labelKey: 'settings.items.terminalMaxSessions', descriptionKey: 'settings.items.terminalMaxSessionsDesc', key: 'terminal.max_sessions', type: 'number', source: 'server' },
    { labelKey: 'settings.items.terminalBufferLines', descriptionKey: 'settings.items.terminalBufferLinesDesc', key: 'terminal.buffer_lines', type: 'number', source: 'server' },
  ],
  tts: [
    // Engine selection (always shown)
    { labelKey: 'settings.items.ttsEngine', descriptionKey: 'settings.items.ttsEngineDesc', key: 'tts.engine', type: 'select', source: 'server', options: [
      { labelKey: 'settings.items.ttsEngineEdge', value: 'edge' },
      { labelKey: 'settings.items.ttsEnginePiper', value: 'piper' },
      { labelKey: 'settings.items.ttsEngineKokoro', value: 'kokoro' },
      { labelKey: 'settings.items.ttsEngineMossNano', value: 'moss-nano' },
    ]},
    // Common fields (always shown)
    { labelKey: 'settings.items.ttsVoice', descriptionKey: 'settings.items.ttsVoiceDesc', key: 'tts.voice', type: 'select', source: 'server' },
    { labelKey: 'settings.items.ttsSpeed', descriptionKey: 'settings.items.ttsSpeedDesc', key: 'tts.speed', type: 'slider', source: 'server', min: 0.5, max: 3, step: 0.1 },
    // Minimax-specific
    { labelKey: 'settings.items.ttsModel', descriptionKey: 'settings.items.ttsModelDesc', key: 'tts.tts_model', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'minimax' } },
    { labelKey: 'settings.items.ttsFormat', descriptionKey: 'settings.items.ttsFormatDesc', key: 'tts.format', type: 'select', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'minimax' }, options: [
      { labelKey: 'settings.items.ttsFormatDefault', value: '' },
      { labelKey: 'settings.items.ttsFormatMp3', value: 'mp3' },
      { labelKey: 'settings.items.ttsFormatWav', value: 'wav' },
      { labelKey: 'settings.items.ttsFormatPcm', value: 'pcm' },
    ]},
    // Piper sub-config
    { labelKey: 'settings.items.piperModelPath', descriptionKey: 'settings.items.piperModelPathDesc', key: 'tts.piper.model_path', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'piper' }, sectionHeader: 'settings.items.ttsPiperHeader' },
    { labelKey: 'settings.items.piperNoiseScale', descriptionKey: 'settings.items.piperNoiseScaleDesc', key: 'tts.piper.noise_scale', type: 'number', source: 'server', min: 0, max: 1, step: 0.001,
      dependsOn: { key: 'tts.engine', value: 'piper' } },
    { labelKey: 'settings.items.piperLengthScale', descriptionKey: 'settings.items.piperLengthScaleDesc', key: 'tts.piper.length_scale', type: 'number', source: 'server', min: 0.1, max: 5, step: 0.1,
      dependsOn: { key: 'tts.engine', value: 'piper' } },
    { labelKey: 'settings.items.piperSentenceSilence', descriptionKey: 'settings.items.piperSentenceSilenceDesc', key: 'tts.piper.sentence_silence', type: 'number', source: 'server', min: 0, max: 5, step: 0.1,
      dependsOn: { key: 'tts.engine', value: 'piper' } },
    // Kokoro sub-config
    { labelKey: 'settings.items.kokoroModelPath', descriptionKey: 'settings.items.kokoroModelPathDesc', key: 'tts.kokoro.model_path', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'kokoro' }, sectionHeader: 'settings.items.ttsKokoroHeader' },
    { labelKey: 'settings.items.kokoroVoicesPath', descriptionKey: 'settings.items.kokoroVoicesPathDesc', key: 'tts.kokoro.voices_path', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'kokoro' } },
    { labelKey: 'settings.items.kokoroLang', descriptionKey: 'settings.items.kokoroLangDesc', key: 'tts.kokoro.lang', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'kokoro' } },
    // MossNano sub-config
    { labelKey: 'settings.items.mossNanoModelDir', descriptionKey: 'settings.items.mossNanoModelDirDesc', key: 'tts.moss_nano.model_dir', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'moss-nano' }, sectionHeader: 'settings.items.ttsMossNanoHeader' },
    { labelKey: 'settings.items.mossNanoPromptSpeech', descriptionKey: 'settings.items.mossNanoPromptSpeechDesc', key: 'tts.moss_nano.prompt_speech', type: 'text', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'moss-nano' } },
    { labelKey: 'settings.items.mossNanoVoice', descriptionKey: 'settings.items.mossNanoVoiceDesc', key: 'tts.moss_nano.voice', type: 'select', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'moss-nano' }, options: [
      { labelKey: 'settings.items.voiceMossJunhao', value: 'Junhao' },
    ]},
    { labelKey: 'settings.items.mossNanoBackend', descriptionKey: 'settings.items.mossNanoBackendDesc', key: 'tts.moss_nano.backend', type: 'select', source: 'server',
      dependsOn: { key: 'tts.engine', value: 'moss-nano' }, options: [
      { labelKey: 'settings.items.mossNanoBackendOnnx', value: 'onnx' },
      { labelKey: 'settings.items.mossNanoBackendPytorch', value: 'pytorch' },
    ]},
    // Cache
    { labelKey: 'settings.items.ttsMaxCacheFiles', descriptionKey: 'settings.items.ttsMaxCacheFilesDesc', key: 'tts.max_cache_files', type: 'number', source: 'server',
      sectionHeader: 'settings.items.ttsCacheHeader' },
  ],
  summarization: [
    // Summarization backend (shared by TTS and tasks)
    { labelKey: 'settings.items.summarizeBackend', descriptionKey: 'settings.items.summarizeBackendDesc', key: 'summarize.backend', type: 'select', source: 'server', options: [
      { labelKey: 'settings.items.summarizeDisabled', value: '' },
      { labelKey: 'settings.items.summarizeSimple', value: 'simple' },
      { labelKey: 'settings.items.summarizeApi', value: 'api' },
      { labelKey: 'settings.items.summarizeClaude', value: 'claude' },
      { labelKey: 'settings.items.summarizeCodebuddy', value: 'codebuddy' },
      { labelKey: 'settings.items.summarizeOpencode', value: 'opencode' },
      { labelKey: 'settings.items.summarizeCodex', value: 'codex' },
      { labelKey: 'settings.items.summarizeQoder', value: 'qoder' },
      { labelKey: 'settings.items.summarizeVecli', value: 'vecli' },
      { labelKey: 'settings.items.summarizeDeepseek', value: 'deepseek' },
      { labelKey: 'settings.items.summarizePi', value: 'pi' },
    ]},
    { labelKey: 'settings.items.summarizeModel', descriptionKey: 'settings.items.summarizeModelDesc', key: 'summarize.model', type: 'text', source: 'server',
      dependsOn: { key: 'summarize.backend', values: ['api', 'claude', 'codebuddy', 'opencode', 'codex', 'qoder', 'vecli', 'deepseek', 'pi'] } },
    // API sub-config (shown when backend is "api")
    { labelKey: 'settings.items.apiBaseUrl', descriptionKey: 'settings.items.apiBaseUrlDesc', key: 'summarize.api.base_url', type: 'text', source: 'server',
      dependsOn: { key: 'summarize.backend', value: 'api' }, sectionHeader: 'settings.items.apiHeader' },
    { labelKey: 'settings.items.apiKey', descriptionKey: 'settings.items.apiKeyDesc', key: 'summarize.api.key', type: 'password', source: 'server',
      dependsOn: { key: 'summarize.backend', value: 'api' } },
    { labelKey: 'settings.items.apiFormat', descriptionKey: 'settings.items.apiFormatDesc', key: 'summarize.api.format', type: 'select', source: 'server',
      dependsOn: { key: 'summarize.backend', value: 'api' }, options: [
      { labelKey: 'settings.items.apiFormatOpenai', value: 'openai' },
      { labelKey: 'settings.items.apiFormatAnthropic', value: 'anthropic' },
    ]},
  ],
  rag: [
    { labelKey: 'settings.items.ragOllamaUrl', descriptionKey: 'settings.items.ragOllamaUrlDesc', key: 'rag.ollama_base_url', type: 'text', source: 'server' },
    { labelKey: 'settings.items.ragOllamaModel', descriptionKey: 'settings.items.ragOllamaModelDesc', key: 'rag.ollama_model', type: 'text', source: 'server' },
    { labelKey: 'settings.items.ragChunkSize', descriptionKey: 'settings.items.ragChunkSizeDesc', key: 'rag.chunk_size', type: 'number', source: 'server' },
    { labelKey: 'settings.items.ragSearchLimit', descriptionKey: 'settings.items.ragSearchLimitDesc', key: 'rag.search_limit', type: 'number', source: 'server' },
    { labelKey: 'settings.items.ragSearchPoolSize', descriptionKey: 'settings.items.ragSearchPoolSizeDesc', key: 'rag.search_pool_size', type: 'number', source: 'server' },
    { labelKey: 'settings.items.ragRetentionDays', descriptionKey: 'settings.items.ragRetentionDaysDesc', key: 'rag.retention_days', type: 'number', source: 'server' },
  ],
  portForward: [
    { labelKey: 'settings.items.portForwardEnabled', descriptionKey: 'settings.items.portForwardEnabledDesc', key: 'port_forward.enabled', type: 'switch', source: 'server', needsRestart: true },
    { labelKey: 'settings.items.portForwardPort', descriptionKey: 'settings.items.portForwardPortDesc', key: 'port_forward.port', type: 'number', source: 'server', needsRestart: true },
  ],
  push: [
    { labelKey: 'settings.items.pushEnabled', descriptionKey: 'settings.items.pushEnabledDesc', key: 'push.jpush.enabled', type: 'switch', source: 'server' },
    { labelKey: 'settings.items.pushPersistentNotification', descriptionKey: 'settings.items.pushPersistentNotificationDesc', key: 'pushPersistentNotification', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.pushAppKey', descriptionKey: 'settings.items.pushAppKeyDesc', key: 'push.jpush.app_key', type: 'text', source: 'server' },
    { labelKey: 'settings.items.pushMasterSecret', descriptionKey: 'settings.items.pushMasterSecretDesc', key: 'push.jpush.master_secret', type: 'password', source: 'server' },
  ],
  android: [
    { labelKey: 'settings.items.androidLogCapture', descriptionKey: 'settings.items.androidLogCaptureDesc', key: 'androidLogCapture', type: 'switch', source: 'local' },
    { labelKey: 'settings.items.reconfigureServer', descriptionKey: 'settings.items.reconfigureServerDesc', key: 'reconfigureServer', type: 'action', source: 'local' },
  ],
  security: [
    { labelKey: 'settings.items.localhostAuthExempt', descriptionKey: 'settings.items.localhostAuthExemptDesc', key: 'localhost_auth_exempt', type: 'switch', source: 'server' },
    { labelKey: 'settings.items.changePassword', descriptionKey: 'settings.items.changePasswordDesc', key: 'changePassword', type: 'action', source: 'local' },
  ],
  about: [
    { labelKey: 'settings.items.aboutServerVersion', descriptionKey: 'settings.items.aboutServerVersionDesc', key: 'serverVersion', type: 'info', source: 'server' },
    { labelKey: 'settings.items.aboutAppVersion', descriptionKey: 'settings.items.aboutAppVersionDesc', key: 'appVersion', type: 'info', source: 'local' },
    { labelKey: 'settings.items.addToHomeScreen', descriptionKey: 'settings.items.addToHomeScreenDesc', key: 'addToHomeScreen', type: 'action', source: 'local' },
    { labelKey: 'settings.items.downloadAndroidApp', descriptionKey: 'settings.items.downloadAndroidAppDesc', key: 'downloadAndroidApp', type: 'action', source: 'local' },
    { labelKey: 'settings.items.restartServer', descriptionKey: 'settings.items.restartServerDesc', key: 'restartServer', type: 'action', source: 'local' },
  ],
}

/** Build and return the mapping from server config dot-path keys to i18n label keys. */
export function getServerFieldToLabelKey(): Record<string, string> {
  const map: Record<string, string> = {}
  for (const items of Object.values(categoryItems)) {
    for (const item of items) {
      if (item.source === 'server') {
        map[item.key] = item.labelKey
      }
    }
  }
  return map
}

/** Pre-computed singleton — used by SettingsRestartDialog to translate field paths. */
export const serverFieldToLabelKey: Record<string, string> = getServerFieldToLabelKey()

/**
 * Voice options per TTS engine.
 * Used by SettingsCategory.vue to dynamically resolve tts.voice select options
 * based on the currently selected tts.engine value.
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
  ],
}
