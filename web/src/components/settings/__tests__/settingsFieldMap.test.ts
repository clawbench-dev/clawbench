import { describe, expect, it } from 'vitest'
import { getServerFieldToLabelKey, categoryItems, categoryGroups, getGroupById, getCategoryForGroup, type ItemSpec } from '@/components/settings/settingsFieldMap'

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
    for (const [key, labelKey] of Object.entries(map)) {
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

  it('categoryItems covers all expected categories', () => {
    const expectedCategories = ['appearance', 'agents', 'project', 'chat', 'files', 'terminal', 'tts', 'summarization', 'rag', 'portForward', 'frp', 'push', 'android', 'about']
    for (const cat of expectedCategories) {
      expect(categoryItems[cat]).toBeDefined()
    }
  })

  it('every server item in categoryItems has a corresponding field map entry', () => {
    const map = getServerFieldToLabelKey()
    for (const [category, items] of Object.entries(categoryItems)) {
      for (const item of items) {
        if (item.source === 'server' && item.key !== 'serverVersion' && item.key !== 'restart') {
          expect(map[item.key]).toBeDefined()
        }
      }
    }
  })

  it('pushPersistentNotification is a local switch without dependsOn', () => {
    const pushItems = categoryItems['push']
    const item = pushItems.find(i => i.key === 'pushPersistentNotification')
    expect(item).toBeDefined()
    expect(item!.type).toBe('switch')
    expect(item!.source).toBe('local')
    expect(item!.dependsOn).toBeUndefined()
  })

  // ── All groups have been flattened to categoryItems ──

  it('categoryGroups is empty (all groups flattened)', () => {
    expect(Object.keys(categoryGroups)).toHaveLength(0)
  })

  it('getGroupById returns undefined for any groupId', () => {
    expect(getGroupById('tts-group')).toBeUndefined()
    expect(getGroupById('nonexistent-group')).toBeUndefined()
  })

  it('getCategoryForGroup returns undefined for any groupId', () => {
    expect(getCategoryForGroup('tts-group')).toBeUndefined()
    expect(getCategoryForGroup('nonexistent-group')).toBeUndefined()
  })

  // ── TTS flattened items ──

  it('frp categoryItems has enabled switch and fields with dependsOn', () => {
    const frpItems = categoryItems['frp']
    const enabledItem = frpItems.find(i => i.key === 'frp.enabled')
    expect(enabledItem).toBeDefined()
    expect(enabledItem!.type).toBe('switch')
    expect(enabledItem!.needsRestart).toBe(true)

    const addrItem = frpItems.find(i => i.key === 'frp.server_addr')
    expect(addrItem).toBeDefined()
    expect(addrItem!.dependsOn).toEqual({ key: 'frp.enabled', value: true })

    const portItem = frpItems.find(i => i.key === 'frp.server_port')
    expect(portItem).toBeDefined()
    expect(portItem!.dependsOn).toEqual({ key: 'frp.enabled', value: true })

    const tokenItem = frpItems.find(i => i.key === 'frp.token')
    expect(tokenItem).toBeDefined()
    expect(tokenItem!.type).toBe('password')
    expect(tokenItem!.dependsOn).toEqual({ key: 'frp.enabled', value: true })

    const remotePortItem = frpItems.find(i => i.key === 'frp.remote_port')
    expect(remotePortItem).toBeDefined()
    expect(remotePortItem!.dependsOn).toEqual({ key: 'frp.enabled', value: true })
  })

  it('tts categoryItems has engine selector and per-engine fields with dependsOn', () => {
    const ttsItems = categoryItems['tts']
    const engineItem = ttsItems.find(i => i.key === 'tts.engine')
    expect(engineItem).toBeDefined()
    expect(engineItem!.type).toBe('select')

    const voiceItem = ttsItems.find(i => i.key === 'tts.voice')
    expect(voiceItem).toBeDefined()

    const piperItem = ttsItems.find(i => i.key === 'tts.piper.model_path')
    expect(piperItem).toBeDefined()
    expect(piperItem!.dependsOn).toEqual({ key: 'tts.engine', value: 'piper' })

    const kokoroItem = ttsItems.find(i => i.key === 'tts.kokoro.model_path')
    expect(kokoroItem).toBeDefined()
    expect(kokoroItem!.dependsOn).toEqual({ key: 'tts.engine', value: 'kokoro' })

    const mossNanoItem = ttsItems.find(i => i.key === 'tts.moss_nano.model_dir')
    expect(mossNanoItem).toBeDefined()
    expect(mossNanoItem!.dependsOn).toEqual({ key: 'tts.engine', value: 'moss-nano' })
  })

  // ── groupConfig tests ──

  it('frp.enabled has groupConfig with triggerValue=true', () => {
    const frpItems = categoryItems['frp']
    const enabledItem = frpItems.find(i => i.key === 'frp.enabled') as ItemSpec
    expect(enabledItem).toBeDefined()
    expect(enabledItem.groupConfig).toBeDefined()
    expect(enabledItem.groupConfig!.length).toBe(1)
    expect(enabledItem.groupConfig![0].triggerValue).toBe(true)
    expect(enabledItem.groupConfig![0].requiredFields).toEqual(['frp.server_addr'])
    expect(enabledItem.groupConfig![0].fields).toContain('frp.server_addr')
    expect(enabledItem.groupConfig![0].fields).toContain('frp.server_port')
    expect(enabledItem.groupConfig![0].fields).toContain('frp.token')
    expect(enabledItem.groupConfig![0].fields).toContain('frp.remote_port')
  })

  it('summarize.backend has groupConfig with triggerValue="api"', () => {
    const summarizeItems = categoryItems['summarization']
    const backendItem = summarizeItems.find(i => i.key === 'summarize.backend') as ItemSpec
    expect(backendItem).toBeDefined()
    expect(backendItem.groupConfig).toBeDefined()
    expect(backendItem.groupConfig!.length).toBe(1)
    expect(backendItem.groupConfig![0].triggerValue).toBe('api')
    expect(backendItem.groupConfig![0].requiredFields).toEqual(['summarize.api.base_url'])
  })

  it('tts.engine has groupConfig for piper and kokoro', () => {
    const ttsItems = categoryItems['tts']
    const engineItem = ttsItems.find(i => i.key === 'tts.engine') as ItemSpec
    expect(engineItem).toBeDefined()
    expect(engineItem.groupConfig).toBeDefined()
    expect(engineItem.groupConfig!.length).toBe(2)

    const piperTrigger = engineItem.groupConfig!.find(gc => gc.triggerValue === 'piper')
    expect(piperTrigger).toBeDefined()
    expect(piperTrigger!.requiredFields).toEqual(['tts.piper.model_path'])

    const kokoroTrigger = engineItem.groupConfig!.find(gc => gc.triggerValue === 'kokoro')
    expect(kokoroTrigger).toBeDefined()
    expect(kokoroTrigger!.requiredFields).toEqual(['tts.kokoro.model_path', 'tts.kokoro.voices_path'])
  })
})
