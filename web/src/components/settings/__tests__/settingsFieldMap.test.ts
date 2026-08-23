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
  })

  it('includes previously missing rag.search_pool_size', () => {
    const map = getServerFieldToLabelKey()
    expect(map['rag.search_pool_size']).toBeTruthy()
  })

  it('includes tls.cert_dir as a server text field with restart', () => {
    const map = getServerFieldToLabelKey()
    expect(map['tls.cert_dir']).toBeTruthy()

    const securityEntries = categoryItems['security']
    const entry = securityEntries.find(e => e.type === 'item' && e.spec.key === 'tls.cert_dir')
    expect(entry).toBeDefined()
    if (entry!.type === 'item') {
      expect(entry!.spec.source).toBe('server')
      expect(entry!.spec.type).toBe('text')
      expect(entry!.spec.needsRestart).toBe(true)
    }
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
      'terminal', 'tts', 'tts_engine', 'aiSummary', 'rag', 'portForward', 'frp',
    ]
    for (const cat of expectedCategories) {
      expect(categoryItems[cat]).toBeDefined()
    }
    // dingtalk category was merged into notification
    expect(categoryItems['dingtalk']).toBeUndefined()
  })

  it('chat category has a local messageDisplayMode select with summary/original options', () => {
    const chatEntries = categoryItems['chat']
    const entry = chatEntries.find(e => e.type === 'item' && e.spec.key === 'messageDisplayMode')
    expect(entry).toBeDefined()
    if (entry!.type !== 'item') throw new Error('expected item')
    expect(entry.spec.source).toBe('local')
    expect(entry.spec.type).toBe('select')
    expect(entry.spec.sectionHeader).toBe('settings.items.chatMessageSection')
    const values = (entry.spec.options ?? []).map(o => o.value)
    expect(values).toEqual(['summary', 'original'])
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
    expect(categoryHasPanels('aiSummary')).toBe(true)
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
    expect(isPanelOnlyCategory('aiSummary')).toBe(true)
    expect(isPanelOnlyCategory('rag')).toBe(true)
    expect(isPanelOnlyCategory('portForward')).toBe(true)
    expect(isPanelOnlyCategory('frp')).toBe(true)
    // notification has both an item (notificationSound) and a panel (push)
    expect(isPanelOnlyCategory('notification')).toBe(false)
    expect(isPanelOnlyCategory('appearance')).toBe(false)
  })

  // ── Terminal panel ──

  it('terminal panel has enableKey and commonFields', () => {
    const panels = getCategoryPanels('terminal')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.enableKey).toBe('terminal.enabled')
    expect(cfg.enableLabelKey).toBe('settings.items.terminalEnabled')
    expect(cfg.commonFields.length).toBe(5)
    expect(cfg.commonFields[0].key).toBe('terminalTheme')
    expect(cfg.commonFields[0].source).toBe('local')
    expect(cfg.commonFields[1].key).toBe('terminalFontSize')
    expect(cfg.commonFields[1].source).toBe('local')
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

  // ── Summarization (语音摘要) ──

  it('tts category exposes voice summary type as an immediate item', () => {
    const items = categoryItems.tts
    const ttsBackend = items.find(e => e.type === 'item' && e.spec.key === 'summarize.tts_backend')
    expect(ttsBackend).toBeDefined()
    expect(ttsBackend!.type).toBe('item')
    const spec = (ttsBackend as { type: 'item'; spec: ItemSpec }).spec
    expect(spec.type).toBe('select')
    expect(spec.source).toBe('server')
    // Only two options: simple (extract conclusion) and api (LLM). No "off".
    const values = (spec.options ?? []).map(o => o.value)
    expect(values).toEqual(['simple', 'api'])

    // Jump link to the top-level aiSummary panel
    const jump = items.find(e => e.type === 'item' && e.spec.key === 'navigateAiSummary')
    expect(jump).toBeDefined()
    const jumpSpec = (jump as { type: 'item'; spec: ItemSpec }).spec
    expect(jumpSpec.navigateTo).toBe('aiSummary')
  })

  it('ai_summary panel has shared model fields', () => {
    const panels = getCategoryPanels('aiSummary')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.panelId).toBe('ai_summary')

    const baseUrl = cfg.commonFields.find(f => f.key === 'ai_summary.api.base_url')
    expect(baseUrl).toBeDefined()
    expect(baseUrl!.type).toBe('text')

    const model = cfg.commonFields.find(f => f.key === 'ai_summary.model')
    expect(model).toBeDefined()

    const format = cfg.commonFields.find(f => f.key === 'ai_summary.format')
    expect(format).toBeDefined()
    expect(format!.type).toBe('select')

    const apiKey = cfg.commonFields.find(f => f.key === 'ai_summary.api.key')
    expect(apiKey).toBeDefined()
    expect(apiKey!.type).toBe('password')

    expect(typeof cfg.hasConnectivityTest === 'function').toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'ai_summary.api.base_url': 'https://x' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({})).toBe(false)
  })

  // ── RAG panel ──

  it('rag panel has 13 commonFields and requiredFields', () => {
    const panels = getCategoryPanels('rag')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(cfg.commonFields.length).toBe(13)
    expect(cfg.commonFields[0].key).toBe('rag.status.mode')
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
    expect(cfg.entrySelector!.options!.length).toBe(4)
    expect(cfg.entrySelector!.options!.map(o => o.value)).toEqual(['native', 'dingtalk', 'feishu', 'disabled'])
    expect(cfg.commonFields.length).toBe(0)

    const dingtalkSub = cfg.optionSubFields!.find(osf => osf.when === 'dingtalk')
    expect(dingtalkSub).toBeDefined()
    expect(dingtalkSub!.fields.length).toBe(3)
    expect(dingtalkSub!.fields.map(f => f.key)).toEqual(['dingtalk.app_key', 'dingtalk.app_secret', 'dingtalk.agent_id'])

    const feishuSub = cfg.optionSubFields!.find(osf => osf.when === 'feishu')
    expect(feishuSub).toBeDefined()
    expect(feishuSub!.fields.length).toBe(2)
    expect(feishuSub!.fields.map(f => f.key)).toEqual(['feishu.app_id', 'feishu.app_secret'])

    expect(cfg.requiredFields).toEqual(['dingtalk.app_key', 'dingtalk.app_secret', 'dingtalk.agent_id', 'feishu.app_id', 'feishu.app_secret'])
    expect(typeof cfg.hasConnectivityTest).toBe('function')
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'dingtalk' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'feishu' })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'native' })).toBe(false)
    expect((cfg.hasConnectivityTest as Function)({ push_mode: 'disabled' })).toBe(false)
    expect(cfg.getTestCategories).toBeDefined()
    expect(cfg.afterSave).toBeDefined()
  })

  // ── Sub-page route helpers (data-driven) ──

  it('isSubPageRoute identifies colon-separated IDs except agents', () => {
    expect(isSubPageRoute('tts:tts_engine')).toBe(true)
    expect(isSubPageRoute('agents:codebuddy')).toBe(false)
    expect(isSubPageRoute('agents')).toBe(false)
    expect(isSubPageRoute('terminal')).toBe(false)
    expect(isSubPageRoute('chat')).toBe(false)
  })

  it('getSubPagePanel returns panel config for valid sub-routes', () => {
    const ttsPanel = getSubPagePanel('tts:tts_engine')
    expect(ttsPanel).toBeDefined()
    expect(ttsPanel!.panelId).toBe('tts')
  })

  it('getSubPagePanel returns undefined for unknown sub-routes', () => {
    expect(getSubPagePanel('chat:unknown')).toBeUndefined()
    expect(getSubPagePanel('nonexistent:panel')).toBeUndefined()
  })

  it('getSubPageTitleKey returns title i18n key for valid sub-routes', () => {
    expect(getSubPageTitleKey('tts:tts_engine')).toBe('settings.items.ttsEngine')
  })

  it('subPagePanelMap has entry for every navigateTo action item', () => {
    // Verify that all colon-separated navigateTo action items have a
    // corresponding subPagePanelMap entry. Top-level category jumps (no colon)
    // target categoryItems directly and are excluded here.
    for (const entries of Object.values(categoryItems)) {
      for (const entry of entries) {
        if (entry.type === 'item' && entry.spec.navigateTo) {
          const nav = entry.spec.navigateTo
          if (!nav.includes(':')) continue
          expect(subPagePanelMap[nav]).toBeDefined()
          expect(subPagePanelMap[nav].panelConfig).toBeDefined()
          expect(subPagePanelMap[nav].titleKey).toBeTruthy()
        }
      }
    }
  })

  it('RAG panel hasConnectivityTest is conditional', () => {
    const panels = getCategoryPanels('rag')
    expect(panels.length).toBe(1)
    const cfg = panels[0]
    expect(typeof cfg.hasConnectivityTest === 'function').toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'rag.base_url': 'http://localhost:11434', 'rag.vector_enabled': true })).toBe(true)
    expect((cfg.hasConnectivityTest as Function)({ 'rag.base_url': 'http://localhost:11434', 'rag.vector_enabled': false })).toBe(false)
    expect((cfg.hasConnectivityTest as Function)({ 'rag.base_url': '', 'rag.vector_enabled': true })).toBe(false)
    expect((cfg.hasConnectivityTest as Function)({})).toBe(false)
  })
})
