import { describe, expect, it } from 'vitest'
import { getServerFieldToLabelKey, categoryItems, categoryHasPanels, isPanelOnlyCategory, getCategoryPanels } from '@/components/settings/settingsFieldMap'

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
      'terminal', 'tts', 'summarization', 'rag', 'portForward', 'frp',
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
    expect(categoryHasPanels('tts')).toBe(true)
    expect(categoryHasPanels('summarization')).toBe(true)
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
    expect(isPanelOnlyCategory('tts')).toBe(true)
    expect(isPanelOnlyCategory('summarization')).toBe(true)
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

  it('tts panel has entrySelector and optionSubFields', () => {
    const panels = getCategoryPanels('tts')
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

  // ── Summarization panel ──

  it('summarization panel has separate text and voice backends', () => {
    const panels = getCategoryPanels('summarization')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.entrySelector).toBeUndefined()
    expect(cfg.requiredFields).toEqual(['summarize.api.base_url', 'summarize.tts_api.base_url'])

    const textBackend = cfg.commonFields.find(f => f.key === 'summarize.backend')
    expect(textBackend).toBeDefined()
    expect(textBackend!.type).toBe('select')
    expect(textBackend!.sectionHeader).toBe('settings.items.summarizeTextSection')

    const ttsBackend = cfg.commonFields.find(f => f.key === 'summarize.tts_backend')
    expect(ttsBackend).toBeDefined()
    expect(ttsBackend!.type).toBe('select')
    expect(ttsBackend!.sectionHeader).toBe('settings.items.summarizeTtsSection')

    const apiBaseURL = cfg.commonFields.find(f => f.key === 'summarize.api.base_url')
    expect(apiBaseURL).toBeDefined()
    expect(apiBaseURL!.sectionHeader).toBe('settings.items.apiHeader')

    const ttsApiBaseURL = cfg.commonFields.find(f => f.key === 'summarize.tts_api.base_url')
    expect(ttsApiBaseURL).toBeDefined()
    expect(ttsApiBaseURL!.sectionHeader).toBe('settings.items.summarizeTtsApiHeader')

    expect(cfg.hasConnectivityTest).toBe(true)
    expect(cfg.getTestCategories).toBeDefined()
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
    expect(cfg.hasConnectivityTest).toBe(true)
    expect(cfg.getTestCategories).toBeDefined()
    expect(cfg.afterSave).toBeDefined()
  })
})
