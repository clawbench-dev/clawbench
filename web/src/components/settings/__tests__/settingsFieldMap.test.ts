import { describe, expect, it } from 'vitest'
import { getServerFieldToLabelKey, categoryItems, drillDownCategories, isDrillDownCategory } from '@/components/settings/settingsFieldMap'

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

  it('recent_projects.max_count is in project category items', () => {
    const projectItems = categoryItems['project']
    const rpItem = projectItems.find(item => item.key === 'recent_projects.max_count')
    expect(rpItem).toBeDefined()
    expect(rpItem!.source).toBe('server')
    expect(rpItem!.type).toBe('number')
    expect(rpItem!.min).toBe(1)
  })

  it('does not map orphaned ssh.* keys (renamed to port_forward)', () => {
    const map = getServerFieldToLabelKey()
    expect(map['ssh.enabled']).toBeUndefined()
    expect(map['ssh.port']).toBeUndefined()
  })

  it('categoryItems covers flat (non-drill-down) categories', () => {
    const expectedCategories = ['appearance', 'agents', 'project', 'chat', 'files', 'android', 'security', 'about']
    for (const cat of expectedCategories) {
      expect(categoryItems[cat]).toBeDefined()
    }
  })

  it('drill-down categories are not in categoryItems', () => {
    expect(categoryItems['terminal']).toBeUndefined()
    expect(categoryItems['tts']).toBeUndefined()
    expect(categoryItems['summarization']).toBeUndefined()
    expect(categoryItems['rag']).toBeUndefined()
    expect(categoryItems['portForward']).toBeUndefined()
    expect(categoryItems['frp']).toBeUndefined()
  })

  it('every server item in categoryItems has a corresponding field map entry', () => {
    const map = getServerFieldToLabelKey()
    for (const items of Object.values(categoryItems)) {
      for (const item of items) {
        if (item.source === 'server' && item.key !== 'serverVersion' && item.key !== 'restart') {
          expect(map[item.key]).toBeDefined()
        }
      }
    }
  })

  // ── Drill-down categories ──

  it('drillDownCategories covers all 6 drill-down categories', () => {
    const expectedIds = ['terminal', 'tts', 'summarization', 'rag', 'portForward', 'frp']
    for (const id of expectedIds) {
      expect(drillDownCategories[id]).toBeDefined()
      expect(drillDownCategories[id].categoryId).toBe(id)
    }
  })

  it('isDrillDownCategory identifies drill-down categories', () => {
    expect(isDrillDownCategory('terminal')).toBe(true)
    expect(isDrillDownCategory('tts')).toBe(true)
    expect(isDrillDownCategory('summarization')).toBe(true)
    expect(isDrillDownCategory('rag')).toBe(true)
    expect(isDrillDownCategory('portForward')).toBe(true)
    expect(isDrillDownCategory('frp')).toBe(true)
    expect(isDrillDownCategory('appearance')).toBe(false)
    expect(isDrillDownCategory('chat')).toBe(false)
    expect(isDrillDownCategory('about')).toBe(false)
  })

  // ── Terminal drill-down ──

  it('terminal drill-down has enableKey and commonFields', () => {
    const dd = drillDownCategories['terminal']
    expect(dd.enableKey).toBe('terminal.enabled')
    expect(dd.enableLabelKey).toBe('settings.items.terminalEnabled')
    expect(dd.commonFields.length).toBe(4)
    expect(dd.commonFields[0].key).toBe('terminalFontSize')
    expect(dd.commonFields[0].source).toBe('local')
  })

  // ── TTS drill-down ──

  it('tts drill-down has entrySelector and optionSubFields', () => {
    const dd = drillDownCategories['tts']
    expect(dd.entrySelector).toBeDefined()
    expect(dd.entrySelector!.key).toBe('tts.engine')
    expect(dd.entrySelector!.type).toBe('select')
    expect(dd.entrySelector!.options!.length).toBe(4)
    expect(dd.commonFields.length).toBe(3)
    expect(dd.optionSubFields!.length).toBe(3)

    const piperSub = dd.optionSubFields!.find(osf => osf.when === 'piper')
    expect(piperSub).toBeDefined()
    expect(piperSub!.fields.length).toBe(4)
    expect(piperSub!.fields[0].key).toBe('tts.piper.model_path')

    const kokoroSub = dd.optionSubFields!.find(osf => osf.when === 'kokoro')
    expect(kokoroSub).toBeDefined()
    expect(kokoroSub!.fields.length).toBe(3)

    const mossNanoSub = dd.optionSubFields!.find(osf => osf.when === 'moss-nano')
    expect(mossNanoSub).toBeDefined()
    expect(mossNanoSub!.fields.length).toBe(2)

    expect(dd.requiredFields).toEqual(['tts.piper.model_path', 'tts.kokoro.model_path', 'tts.kokoro.voices_path'])
  })

  // ── Summarization drill-down ──

  it('summarization drill-down has entrySelector and optionSubFields', () => {
    const dd = drillDownCategories['summarization']
    expect(dd.entrySelector).toBeDefined()
    expect(dd.entrySelector!.key).toBe('summarize.backend')
    expect(dd.entrySelector!.type).toBe('select')
    expect(dd.requiredFields).toEqual(['summarize.api.base_url'])

    const apiSub = dd.optionSubFields!.find(osf => osf.when === 'api')
    expect(apiSub).toBeDefined()
    expect(apiSub!.fields.length).toBe(4)
    expect(apiSub!.fields[1].key).toBe('summarize.api.base_url')

    // CLI backend sub-fields
    const claudeSub = dd.optionSubFields!.find(osf => osf.when === 'claude')
    expect(claudeSub).toBeDefined()
    expect(claudeSub!.fields.length).toBe(1)
    expect(claudeSub!.fields[0].key).toBe('summarize.model')
  })

  // ── RAG drill-down ──

  it('rag drill-down has 7 commonFields and requiredFields', () => {
    const dd = drillDownCategories['rag']
    expect(dd.commonFields.length).toBe(7)
    expect(dd.commonFields[0].key).toBe('rag.base_url')
    expect(dd.requiredFields).toEqual(['rag.base_url'])
  })

  // ── Port Forward drill-down ──

  it('portForward drill-down has enableKey and commonFields (hot-reload, no needsRestart)', () => {
    const dd = drillDownCategories['portForward']
    expect(dd.enableKey).toBe('port_forward.enabled')
    expect(dd.enableLabelKey).toBe('settings.items.portForwardEnabled')
    expect(dd.commonFields.length).toBe(1)
    expect(dd.commonFields[0].key).toBe('port_forward.port')
    // port_forward.enabled and port are hot-reload fields — no needsRestart flag
    expect(dd.commonFields[0].needsRestart).toBeFalsy()
  })

  // ── FRP drill-down ──

  it('frp drill-down has enableKey, optionSubFields, and requiredFields', () => {
    const dd = drillDownCategories['frp']
    expect(dd.enableKey).toBe('frp.enabled')
    expect(dd.enableLabelKey).toBe('settings.items.frpEnabled')
    expect(dd.commonFields.length).toBe(4)

    const autoPortFalseSub = dd.optionSubFields!.find(osf => osf.when === false)
    expect(autoPortFalseSub).toBeDefined()
    expect(autoPortFalseSub!.fields.length).toBe(2)
    expect(autoPortFalseSub!.fields[0].key).toBe('frp.remote_port')
    expect(autoPortFalseSub!.fields[1].key).toBe('frp.ssh_remote_port')

    expect(dd.requiredFields).toEqual(['frp.server_addr'])
  })

  // ── Drill-down fields are included in serverFieldToLabelKey ──

  it('drill-down server fields appear in serverFieldToLabelKey', () => {
    const map = getServerFieldToLabelKey()
    // Terminal enable key
    expect(map['terminal.enabled']).toBe('settings.items.terminalEnabled')
    // TTS engine
    expect(map['tts.engine']).toBe('settings.items.ttsEngine')
    // RAG base_url
    expect(map['rag.base_url']).toBe('settings.items.ragBaseUrl')
    // Port forward enable
    expect(map['port_forward.enabled']).toBe('settings.items.portForwardEnabled')
    // FRP enable
    expect(map['frp.enabled']).toBe('settings.items.frpEnabled')
    // FRP sub-fields
    expect(map['frp.server_addr']).toBe('settings.items.frpServerAddr')
    expect(map['frp.remote_port']).toBe('settings.items.frpRemotePort')
  })
})
