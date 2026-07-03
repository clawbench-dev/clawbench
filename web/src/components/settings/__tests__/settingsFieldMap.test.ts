import { describe, expect, it } from 'vitest'
import { getServerFieldToLabelKey, categoryItems, categoryGroups, getAllGroupFields, getGroupById, getCategoryForGroup } from '@/components/settings/settingsFieldMap'

describe('settingsFieldMap', () => {
  it('maps all server-side dot-path keys to i18n label keys', () => {
    const map = getServerFieldToLabelKey()

    // Key server fields that can appear in changed_cold_fields
    expect(map['terminal.enabled']).toBeTruthy()
    expect(map['tts.engine']).toBeTruthy()
    expect(map['rag.base_url']).toBeTruthy()
    expect(map['port_forward.enabled']).toBeTruthy()
    expect(map['push.jpush.enabled']).toBeTruthy()

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
    const expectedCategories = ['appearance', 'agents', 'project', 'chat', 'files', 'terminal', 'tts', 'summarization', 'rag', 'portForward', 'push', 'android', 'about']
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

  // ── Config Group tests ──

  it('categoryGroups only has tts (others flattened to categoryItems)', () => {
    expect(categoryGroups['tts']).toBeDefined()
    expect(categoryGroups['summarization']).toEqual([])
    expect(categoryGroups['rag']).toEqual([])
    expect(categoryGroups['portForward']).toEqual([])
    expect(categoryGroups['push']).toEqual([])
  })

  it('each group has a unique groupId', () => {
    const ids = new Set<string>()
    for (const groups of Object.values(categoryGroups)) {
      for (const g of groups) {
        expect(ids.has(g.groupId)).toBe(false)
        ids.add(g.groupId)
      }
    }
  })

  it('group field keys are all in serverFieldToLabelKey', () => {
    const map = getServerFieldToLabelKey()
    for (const groups of Object.values(categoryGroups)) {
      for (const group of groups) {
        for (const field of getAllGroupFields(group)) {
          if (field.source === 'server' && !field.key.startsWith('_')) {
            expect(map[field.key]).toBeDefined()
          }
        }
      }
    }
  })

  it('no overlap between group field keys and standalone categoryItems keys', () => {
    for (const [category, groups] of Object.entries(categoryGroups)) {
      const groupKeys = new Set<string>()
      for (const g of groups) {
        for (const f of getAllGroupFields(g)) groupKeys.add(f.key)
      }
      const standaloneItems = categoryItems[category] ?? []
      for (const item of standaloneItems) {
        expect(groupKeys.has(item.key)).toBe(false)
      }
    }
  })

  it('tts group has no nonExpandValues (all engines expand)', () => {
    const ttsGroup = categoryGroups['tts']?.[0]
    expect(ttsGroup).toBeDefined()
    expect(ttsGroup!.nonExpandValues ?? []).toHaveLength(0)
  })

  // ── Lookup helpers ──

  it('getGroupById finds tts-group', () => {
    expect(getGroupById('tts-group')).toBeDefined()
    expect(getGroupById('tts-group')!.entryType).toBe('select')
  })

  it('getGroupById returns undefined for unknown groupId', () => {
    expect(getGroupById('nonexistent-group')).toBeUndefined()
  })

  it('getCategoryForGroup returns correct category for tts', () => {
    expect(getCategoryForGroup('tts-group')).toBe('tts')
  })

  it('getCategoryForGroup returns undefined for unknown groupId', () => {
    expect(getCategoryForGroup('nonexistent-group')).toBeUndefined()
  })
})
