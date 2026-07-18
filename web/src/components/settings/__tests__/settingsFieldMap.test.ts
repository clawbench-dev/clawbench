import { describe, expect, it } from 'vitest'
import { getServerFieldToLabelKey, categoryItems, categoryHasPanels, isPanelOnlyCategory, getCategoryPanels, isSubPageRoute, getSubPagePanel, getSubPageTitleKey, subPagePanelMap } from '@/components/settings/settingsFieldMap'

describe('settingsFieldMap', () => {
  it('maps all server-side dot-path keys to i18n label keys', () => {
    const map = getServerFieldToLabelKey()

    // Key server fields that can appear in changed_cold_fields
    expect(map['terminal.enabled']).toBeTruthy()
    expect(map['tts.engine']).toBeTruthy()
    expect(map['rag.base_url']).toBeTruthy()
    expect(map['port_forward.enabled']).toBeTruthy()

    // Hot-reload fields
    expect(map['chat.page_size']).toBeTruthy()
    expect(map['upload.max_size_mb']).toBeTruthy()

    // All mapped values should be i18n keys (settings.items.* or settings.categories.* for headers)
    for (const labelKey of Object.values(map)) {
      expect(labelKey).toMatch(/^settings\.(items|categories)\./)
    }
  })

  it('does not map local-only settings', () => {
    const map = getServerFieldToLabelKey()

    expect(map['theme']).toBeUndefined()
    expect(map['locale']).toBeUndefined()
    expect(map['autoSpeech']).toBeUndefined()
    expect(map['swipeSession']).toBeUndefined()
    expect(map['pushPersistentNotification']).toBeUndefined()
  })

  it('includes TTS sub-config keys', () => {
    const map = getServerFieldToLabelKey()

    expect(map['tts.piper.model_path']).toBeTruthy()
    expect(map['tts.kokoro.model_path']).toBeTruthy()
    expect(map['tts.moss_nano.model_dir']).toBeTruthy()
    expect(map['summarize.api.base_url']).toBeTruthy()
  })

  it('includes previously missing rag.search_pool_size', () => {
    const map = getServerFieldToLabelKey()
    expect(map['rag.search_pool_size']).toBeTruthy()
  })

  it('includes recent_projects.max_count', () => {
    const map = getServerFieldToLabelKey()
    expect(map['recent_projects.max_count']).toBeTruthy()
  })

  it('recent_projects.max_count is in projectFiles category items', () => {
    const projectFilesEntries = categoryItems['projectFiles']
    const rpEntry = projectFilesEntries.find(e => e.type === 'item' && e.spec.key === 'recent_projects.max_count')
    expect(rpEntry).toBeDefined()
    expect(rpEntry!.type).toBe('item')
    if (rpEntry!.type === 'item') {
      expect(rpEntry!.spec.source).toBe('server')
      expect(rpEntry!.spec.type).toBe('number')
      expect(rpEntry!.spec.min).toBe(1)
    }
  })

  it('does not map orphaned ssh.* keys (renamed to port_forward)', () => {
    const map = getServerFieldToLabelKey()
    expect(map['ssh.enabled']).toBeUndefined()
    expect(map['ssh.port']).toBeUndefined()
  })

  it('categoryItems covers all expected categories', () => {
    const expectedCategories = [
      'appearance', 'agents', 'projectFiles', 'chat', 'debug', 'security', 'about',
      'notification',
      'terminal', 'tts', 'tts_engine', 'summarization_text', 'summarization_voice', 'rag', 'portForward', 'frp',
    ]
    for (const cat of expectedCategories) {
      expect(categoryItems[cat]).toBeDefined()
    }
    // dingtalk category was merged into notification
    expect(categoryItems['dingtalk']).toBeUndefined()
  })

  it('every server item in categoryItems has a corresponding field map entry', () => {
    const map = getServerFieldToLabelKey()
    for (const entries of Object.values(categoryItems)) {
      for (const entry of entries) {
        if (entry.type === 'item') {
          if (entry.spec.source === 'server' && entry.spec.key !== 'serverVersion' && entry.spec.key !== 'restart') {
            expect(map[entry.spec.key]).toBeDefined()
          }
        }
      }
    }
  })

  // ── Panel categories ──

  it('categoryHasPanels identifies panel categories', () => {
    expect(categoryHasPanels('terminal')).toBe(true)
    expect(categoryHasPanels('tts')).toBe(false)
    expect(categoryHasPanels('summarization_text')).toBe(true)
    expect(categoryHasPanels('summarization_voice')).toBe(true)
    expect(categoryHasPanels('rag')).toBe(true)
    expect(categoryHasPanels('portForward')).toBe(true)
    expect(categoryHasPanels('frp')).toBe(true)
    expect(categoryHasPanels('notification')).toBe(true)
    expect(categoryHasPanels('appearance')).toBe(false)
    expect(categoryHasPanels('chat')).toBe(false)
    expect(categoryHasPanels('about')).toBe(false)
  })

  it('isPanelOnlyCategory identifies panel-only categories', () => {
    expect(isPanelOnlyCategory('terminal')).toBe(true)
    expect(isPanelOnlyCategory('tts')).toBe(false)
    expect(isPanelOnlyCategory('summarization_text')).toBe(true)
    expect(isPanelOnlyCategory('summarization_voice')).toBe(true)
    expect(isPanelOnlyCategory('rag')).toBe(true)
    expect(isPanelOnlyCategory('portForward')).toBe(true)
    expect(isPanelOnlyCategory('frp')).toBe(true)
    expect(isPanelOnlyCategory('notification')).toBe(true)
    expect(isPanelOnlyCategory('appearance')).toBe(false)
  })

  // ── Terminal panel ──

  it('terminal panel has enableKey and commonFields', () => {
    const panels = getCategoryPanels('terminal')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.enableKey).toBe('terminal.enabled')
    expect(cfg.enableLabelKey).toBe('settings.items.terminalEnabled')
    expect(cfg.commonFields.length).toBe(4)
    expect(cfg.commonFields[0].key).toBe('terminalFontSize')
    expect(cfg.commonFields[0].source).toBe('local')
  })

  // ── TTS panel ──

  it('tts_engine panel has entrySelector and optionSubFields', () => {
    const panels = getCategoryPanels('tts_engine')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.entrySelector).toBeDefined()
    expect(cfg.entrySelector!.key).toBe('tts.engine')
    expect(cfg.entrySelector!.type).toBe('select')
    expect(cfg.entrySelector!.options!.length).toBe(4)
    expect(cfg.commonFields.length).toBe(3)
    expect(cfg.optionSubFields!.length).toBe(3)

    const piperSub = cfg.optionSubFields!.find(osf => osf.when === 'piper')
    expect(piperSub).toBeDefined()
    expect(piperSub!.fields.length).toBe(4)
    expect(piperSub!.fields[0].key).toBe('tts.piper.model_path')

    const kokoroSub = cfg.optionSubFields!.find(osf => osf.when === 'kokoro')
    expect(kokoroSub).toBeDefined()
    expect(kokoroSub!.fields.length).toBe(3)

    const mossNanoSub = cfg.optionSubFields!.find(osf => osf.when === 'moss-nano')
    expect(mossNanoSub).toBeDefined()
    expect(mossNanoSub!.fields.length).toBe(2)

    expect(cfg.requiredFields).toEqual(['tts.piper.model_path', 'tts.kokoro.model_path', 'tts.kokoro.voices_path'])
    expect(cfg.needsVoiceReset).toBe(true)
    expect(cfg.hasConnectivityTest).toBe(true)
  })

  // ── Summarization panels ──

  it('summarization_text panel has text summary fields', () => {
    const panels = getCategoryPanels('summarization_text')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.panelId).toBe('summarization_text')
    expect(cfg.entrySelector).toBeUndefined()
    expect(cfg.requiredFields).toEqual(['summarize.api.base_url'])
    expect(typeof cfg.hasConnectivityTest === 'function').toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'summarize.backend': 'api' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'summarize.backend': 'simple' })).toBe(false)
    expect((cfg.hasConnectivityTest as Function)({ 'summarize.backend': '' })).toBe(false)

    const textBackend = cfg.commonFields.find(f => f.key === 'summarize.backend')
    expect(textBackend).toBeDefined()
    expect(textBackend!.type).toBe('select')

    const apiBaseURL = cfg.commonFields.find(f => f.key === 'summarize.api.base_url')
    expect(apiBaseURL).toBeDefined()
    expect(apiBaseURL!.sectionHeader).toBe('settings.items.apiHeader')

    const model = cfg.commonFields.find(f => f.key === 'summarize.model')
    expect(model).toBeDefined()

    const apiKey = cfg.commonFields.find(f => f.key === 'summarize.api.key')
    expect(apiKey).toBeDefined()
  })

  it('summarization_voice panel has voice summary fields', () => {
    const panels = getCategoryPanels('summarization_voice')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.panelId).toBe('summarization_voice')
    expect(cfg.entrySelector).toBeUndefined()
    expect(cfg.requiredFields).toEqual(['summarize.tts_api.base_url'])
    expect(typeof cfg.hasConnectivityTest === 'function').toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'summarize.tts_backend': 'api' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'summarize.tts_backend': 'simple' })).toBe(false)

    const ttsBackend = cfg.commonFields.find(f => f.key === 'summarize.tts_backend')
    expect(ttsBackend).toBeDefined()
    expect(ttsBackend!.type).toBe('select')

    const ttsApiBaseURL = cfg.commonFields.find(f => f.key === 'summarize.tts_api.base_url')
    expect(ttsApiBaseURL).toBeDefined()
    expect(ttsApiBaseURL!.sectionHeader).toBe('settings.items.summarizeTtsApiHeader')

    const ttsModel = cfg.commonFields.find(f => f.key === 'summarize.tts_model')
    expect(ttsModel).toBeDefined()

    const ttsApiKey = cfg.commonFields.find(f => f.key === 'summarize.tts_api.key')
    expect(ttsApiKey).toBeDefined()
  })

  // ── RAG panel ──

  it('rag panel has 7 commonFields and requiredFields', () => {
    const panels = getCategoryPanels('rag')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.commonFields.length).toBe(7)
    expect(cfg.commonFields[0].key).toBe('rag.base_url')
    expect(cfg.requiredFields).toEqual(['rag.base_url'])
  })

  // ── Port Forward panel ──

  it('portForward panel has enableKey and commonFields (hot-reload, no needsRestart)', () => {
    const panels = getCategoryPanels('portForward')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.enableKey).toBe('port_forward.enabled')
    expect(cfg.enableLabelKey).toBe('settings.items.portForwardEnabled')
    expect(cfg.commonFields.length).toBe(1)
    expect(cfg.commonFields[0].key).toBe('port_forward.port')
    expect(cfg.commonFields[0].needsRestart).toBeFalsy()
  })

  // ── FRP panel ──

  it('frp panel has enableKey, optionSubFields, optionSubFieldsKey, and requiredFields', () => {
    const panels = getCategoryPanels('frp')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.enableKey).toBe('frp.enabled')
    expect(cfg.enableLabelKey).toBe('settings.items.frpEnabled')
    expect(cfg.commonFields.length).toBe(4)

    const autoPortFalseSub = cfg.optionSubFields!.find(osf => osf.when === false)
    expect(autoPortFalseSub).toBeDefined()
    expect(autoPortFalseSub!.fields.length).toBe(2)
    expect(autoPortFalseSub!.fields[0].key).toBe('frp.remote_port')
    expect(autoPortFalseSub!.fields[1].key).toBe('frp.ssh_remote_port')

    expect(cfg.requiredFields).toEqual(['frp.server_addr'])
    expect(cfg.optionSubFieldsKey).toBe('frp.auto_port')
    expect(cfg.hasConnectivityTest).toBe(true)
    expect(cfg.afterSave).toBeDefined()
    expect(cfg.onInit).toBeDefined()
  })

  // ── Panel server fields in field map ──

  it('panel server fields appear in serverFieldToLabelKey', () => {
    const map = getServerFieldToLabelKey()
    expect(map['terminal.enabled']).toBe('settings.items.terminalEnabled')
    expect(map['tts.engine']).toBe('settings.items.ttsEngine')
    expect(map['rag.base_url']).toBe('settings.items.ragBaseUrl')
    expect(map['port_forward.enabled']).toBe('settings.items.portForwardEnabled')
    expect(map['frp.enabled']).toBe('settings.items.frpEnabled')
    expect(map['frp.server_addr']).toBe('settings.items.frpServerAddr')
    expect(map['frp.remote_port']).toBe('settings.items.frpRemotePort')
  })

  // ── Notification (push) panel ──

  it('notification panel has entrySelector with push_mode, dingtalk optionSubFields, and connectivityTest', () => {
    const panels = getCategoryPanels('notification')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.entrySelector).toBeDefined()
    expect(cfg.entrySelector!.key).toBe('push_mode')
    expect(cfg.entrySelector!.type).toBe('select')
    expect(cfg.entrySelector!.options!.length).toBe(3)
    expect(cfg.entrySelector!.options!.map(o => o.value)).toEqual(['native', 'dingtalk', 'disabled'])
    expect(cfg.commonFields.length).toBe(0)

    const dingtalkSub = cfg.optionSubFields!.find(osf => osf.when === 'dingtalk')
    expect(dingtalkSub).toBeDefined()
    expect(dingtalkSub!.fields.length).toBe(3)
    expect(dingtalkSub!.fields.map(f => f.key)).toEqual(['dingtalk.app_key', 'dingtalk.app_secret', 'dingtalk.agent_id'])

    expect(cfg.requiredFields).toEqual(['dingtalk.app_key', 'dingtalk.app_secret', 'dingtalk.agent_id'])
    expect(typeof cfg.hasConnectivityTest).toBe('function')
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'dingtalk' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'native' })).toBe(false)
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'disabled' })).toBe(false)
    expect(cfg.getTestCategories).toBeDefined()
    expect(cfg.afterSave).toBeDefined()
  })

  // ── Sub-page route helpers (data-driven) ──

  it('isSubPageRoute identifies colon-separated IDs except agents', () => {
    expect(isSubPageRoute('chat:summarization_text')).toBe(true)
    expect(isSubPageRoute('tts:summarization_voice')).toBe(true)
    expect(isSubPageRoute('tts:tts_engine')).toBe(true)
    expect(isSubPageRoute('agents:codebuddy')).toBe(false)
    expect(isSubPageRoute('agents')).toBe(false)
    expect(isSubPageRoute('terminal')).toBe(false)
    expect(isSubPageRoute('chat')).toBe(false)
  })

  it('getSubPagePanel returns panel config for valid sub-routes', () => {
    const textPanel = getSubPagePanel('chat:summarization_text')
    expect(textPanel).toBeDefined()
    expect(textPanel!.panelId).toBe('summarization_text')

    const voicePanel = getSubPagePanel('tts:summarization_voice')
    expect(voicePanel).toBeDefined()
    expect(voicePanel!.panelId).toBe('summarization_voice')

    const ttsPanel = getSubPagePanel('tts:tts_engine')
    expect(ttsPanel).toBeDefined()
    expect(ttsPanel!.panelId).toBe('tts')
  })

  it('getSubPagePanel returns undefined for unknown sub-routes', () => {
    expect(getSubPagePanel('chat:unknown')).toBeUndefined()
    expect(getSubPagePanel('nonexistent:panel')).toBeUndefined()
  })

  it('getSubPageTitleKey returns title i18n key for valid sub-routes', () => {
    expect(getSubPageTitleKey('chat:summarization_text')).toBe('settings.items.summarizeTextSection')
    expect(getSubPageTitleKey('tts:summarization_voice')).toBe('settings.items.summarizeTtsSection')
    expect(getSubPageTitleKey('tts:tts_engine')).toBe('settings.items.ttsEngine')
  })

  it('subPagePanelMap has entry for every navigateTo action item', () => {
    // Verify that all action items with navigateTo have a corresponding subPagePanelMap entry
    for (const entries of Object.values(categoryItems)) {
      for (const entry of entries) {
        if (entry.type === 'item' && entry.spec.navigateTo) {
          expect(subPagePanelMap[entry.spec.navigateTo]).toBeDefined()
          expect(subPagePanelMap[entry.spec.navigateTo].panelConfig).toBeDefined()
          expect(subPagePanelMap[entry.spec.navigateTo].titleKey).toBeTruthy()
        }
      }
    }
  })

  it('RAG panel hasConnectivityTest is conditional', () => {
    const panels = getCategoryPanels('rag')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(typeof cfg.hasConnectivityTest === 'function').toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'rag.base_url': 'http://localhost:11434' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'rag.base_url': '' })).toBe(false)
    expect((cfg.hasConnectivityTest as Function)({})).toBe(false)
  })
})
