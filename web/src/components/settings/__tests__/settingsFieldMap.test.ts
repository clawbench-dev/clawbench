import { describe, expect, it } from 'vitest'
import { getServerFieldToLabelKey, categoryItems } from '@/components/settings/settingsFieldMap'

describe('settingsFieldMap', () => {
  it('maps all server-side dot-path keys to i18n label keys', () => {
    const map = getServerFieldToLabelKey()

    // Key server fields that can appear in changed_cold_fields
    expect(map['default_agent']).toBeTruthy()
    expect(map['terminal.enabled']).toBeTruthy()
    expect(map['tts.engine']).toBeTruthy()
    expect(map['rag.ollama_base_url']).toBeTruthy()
    expect(map['port_forward.enabled']).toBeTruthy()
    expect(map['push.jpush.enabled']).toBeTruthy()

    // Hot-reload fields (shouldn't normally appear but should still be mapped)
    expect(map['chat.page_size']).toBeTruthy()
    expect(map['upload.max_size_mb']).toBeTruthy()

    // All mapped values should be i18n keys
    for (const [key, labelKey] of Object.entries(map)) {
      expect(labelKey).toMatch(/^settings\.items\./)
    }
  })

  it('does not map local-only settings', () => {
    const map = getServerFieldToLabelKey()

    // These are local settings, should not appear
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
    // SSH was renamed to port_forward — ssh.enabled/ssh.port are backend-internal only
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
          // serverVersion and restart are virtual keys, not dot-path config paths
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
    // No dependsOn — this is always visible (not conditional on push.jpush.enabled)
    // because the serverConfig may not be loaded yet when the page renders,
    // causing the dependsOn check to fail with the default value (false).
    expect(item!.dependsOn).toBeUndefined()
  })

  it('pushPersistentNotification comes after pushEnabled in push category', () => {
    const pushItems = categoryItems['push']
    const enabledIdx = pushItems.findIndex(i => i.key === 'push.jpush.enabled')
    const persistentIdx = pushItems.findIndex(i => i.key === 'pushPersistentNotification')
    expect(enabledIdx).toBeLessThan(persistentIdx)
  })
})
