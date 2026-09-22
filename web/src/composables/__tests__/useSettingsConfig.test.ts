import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useSettingsConfig, applyUIScale, getUIScale, toFixedCSS, getZoomedViewport, applyFirstRunThemeDefaults, localConfig, applyStoredTheme, syncThemeFromSystem, startSystemThemeWatcher } from '@/composables/useSettingsConfig'

// Mock api.ts
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
  apiPatch: vi.fn(),
  apiPost: vi.fn(),
}))

// Mock useAgents
const mockGetAgent = vi.fn().mockReturnValue(null)
const mockUpdateAgentField = vi.fn()
const mockLoadAgents = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    getAgent: mockGetAgent,
    updateAgentField: mockUpdateAgentField,
    loadAgents: mockLoadAgents,
  }),
}))

import { apiGet, apiPatch, apiPost } from '@/utils/api'
import { store } from '@/stores/app.ts'
import { applyThemeAttributes } from '@/utils/themeMeta'

const mockedApiGet = vi.mocked(apiGet)
const mockedApiPatch = vi.mocked(apiPatch)
const mockedApiPost = vi.mocked(apiPost)

describe('useSettingsConfig', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('loads config from API', async () => {
    const mockConfig = {
      server: { port: 20000, log_level: 'info' },
      ssh: { enabled: true, port: 2222 },
    }
    mockedApiGet.mockResolvedValue(mockConfig)

    const { loadConfig, serverConfig } = useSettingsConfig()
    await loadConfig()

    expect(mockedApiGet).toHaveBeenCalledWith('/api/config')
    expect(serverConfig.value).toEqual(mockConfig)
  })

  it('patchConfig calls API and returns restart info', async () => {
    const mockResult = { needs_restart: true, changed_cold_fields: ['ssh.enabled'] }
    mockedApiPatch.mockResolvedValue(mockResult)

    const { patchConfig } = useSettingsConfig()
    const result = await patchConfig({ ssh: { enabled: false } })

    expect(mockedApiPatch).toHaveBeenCalledWith('/api/config', { ssh: { enabled: false } })
    // Log the actual result for CI debugging
    if (result.needsRestart !== true) {
      console.log('DEBUG patchConfig result:', JSON.stringify(result))
      // Try reading the raw API response
      const rawCall = mockedApiPatch.mock.results[0]
      console.log('DEBUG raw mock result:', JSON.stringify(rawCall?.value))
    }
    expect(result.needsRestart).toBe(true)
    expect(result.changedColdFields).toEqual(['ssh.enabled'])
  })

  it('restartServer calls API', async () => {
    mockedApiPost.mockResolvedValue({})

    const { restartServer } = useSettingsConfig()
    await restartServer()

    expect(mockedApiPost).toHaveBeenCalledWith('/api/config/restart', {})
  })

  it('setLocalConfig writes to localStorage and updates reactive', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('theme', 'dark')

    expect(localConfig.theme).toBe('dark')
    expect(localStorage.getItem('clawbench-settings-theme')).toBe('"dark"')

    // Clean up
    localStorage.removeItem('clawbench-settings-theme')
  })

  it('getServerValue reads by dot-path', async () => {
    mockedApiGet.mockResolvedValue({ server: { port: 20000 } })

    const { loadConfig, getServerValue } = useSettingsConfig()
    await loadConfig()

    expect(getServerValue('server.port')).toBe(20000)
    expect(getServerValue('server.log_level')).toBeUndefined()
    expect(getServerValue('nonexistent')).toBeUndefined()
  })

  it('getServerValueWithDefault returns server value when present', async () => {
    mockedApiGet.mockResolvedValue({ port_forward: { allowed_ports: '3000-4000' } })

    const { loadConfig, getServerValueWithDefault } = useSettingsConfig()
    await loadConfig()

    expect(getServerValueWithDefault('port_forward.allowed_ports')).toBe('3000-4000')
  })

  it('getServerValueWithDefault falls back to serverDefaults when not present', async () => {
    mockedApiGet.mockResolvedValue({ server: { port: 20000 } })

    const { loadConfig, getServerValueWithDefault } = useSettingsConfig()
    await loadConfig()

    expect(getServerValueWithDefault('port_forward.allowed_ports')).toBe('1024-65535')
  })

  it('localConfig has default keys', () => {
    const { localConfig } = useSettingsConfig()

    // Verify keys exist (values may be overridden by localStorage from other tests)
    expect('theme' in localConfig).toBe(true)
    expect('locale' in localConfig).toBe(true)
    expect('autoSpeech' in localConfig).toBe(true)
    expect('wordWrap' in localConfig).toBe(true)
    expect('lineNumbers' in localConfig).toBe(true)
    expect('showHidden' in localConfig).toBe(true)
    expect('fileView' in localConfig).toBe(true)
    expect('terminalFontSize' in localConfig).toBe(true)
    expect('terminalCopyOnSelect' in localConfig).toBe(true)
    expect('logCapture' in localConfig).toBe(true)
    expect('swipeSession' in localConfig).toBe(true)
  })

  it('serverDefaults exposes the wallpaper source keys', () => {
    // These back the wallpaper mode/Bing controls before /api/config resolves,
    // so the settings panel renders meaningful values on first paint.
    const { getServerValueWithDefault } = useSettingsConfig()

    expect(getServerValueWithDefault('appearance.wallpaper_mode')).toBe('')
    expect(getServerValueWithDefault('appearance.wallpaper_enabled')).toBe(false)
    expect(getServerValueWithDefault('appearance.bing.enabled')).toBe(false)
    expect(getServerValueWithDefault('appearance.bing.mkt')).toBe('zh-CN')
  })

  it('serverDefaults mirrors the backend default for chat.system_prompt_interval', () => {
    // serverDefaults claims to mirror ApplyDefaults() in internal/model/defaults.go.
    // The backend default is 0 ("never re-inject"), not 10: defaults.go keeps 0
    // expressible on purpose so a user who disables periodic re-injection is not
    // silently switched back to every-10-turns. A stale 10 here would make the
    // settings panel show 10 until /api/config resolves, contradicting the server.
    const { getServerValueWithDefault } = useSettingsConfig()

    expect(getServerValueWithDefault('chat.system_prompt_interval')).toBe(0)
  })

  it('serverDefaults mirrors the backend default for chat.recommend_context_messages', () => {
    // Same contract as above. The backend default is 10 (defaults.go rewrites any
    // <= 0 to 10), not 3 — a stale 3 makes the settings panel understate how much
    // conversation the recommender reads until /api/config resolves.
    const { getServerValueWithDefault } = useSettingsConfig()

    expect(getServerValueWithDefault('chat.recommend_context_messages')).toBe(10)
  })

  it('localConfig has markdownCodeLinkPreview defaulting to true', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-markdownCodeLinkPreview')
    expect('markdownCodeLinkPreview' in localConfig).toBe(true)
    expect(localConfig.markdownCodeLinkPreview).toBe(true)
  })

  it('localConfig has filePreviewMode defaulting to false', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-filePreviewMode')
    expect('filePreviewMode' in localConfig).toBe(true)
    expect(localConfig.filePreviewMode).toBe(false)
  })

  it('setLocalConfig persists filePreviewMode to localStorage', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('filePreviewMode', true)
    expect(localConfig.filePreviewMode).toBe(true)
    expect(localStorage.getItem('clawbench-settings-filePreviewMode')).toBe('true')

    localStorage.removeItem('clawbench-settings-filePreviewMode')
  })

  it('localConfig has notificationSound defaulting to true', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-notificationSound')
    expect('notificationSound' in localConfig).toBe(true)
    expect(localConfig.notificationSound).toBe(true)
  })

  it('setLocalConfig persists notificationSound to localStorage', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('notificationSound', false)
    expect(localConfig.notificationSound).toBe(false)
    expect(localStorage.getItem('clawbench-settings-notificationSound')).toBe('false')

    localStorage.removeItem('clawbench-settings-notificationSound')
  })

  // The in-app completion card defaults ON so upgrading users keep seeing it;
  // only an explicit opt-out hides it.
  it('localConfig has inAppNotification defaulting to true', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-inAppNotification')
    expect('inAppNotification' in localConfig).toBe(true)
    expect(localConfig.inAppNotification).toBe(true)
  })

  it('setLocalConfig persists inAppNotification to localStorage', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('inAppNotification', false)
    expect(localConfig.inAppNotification).toBe(false)
    expect(localStorage.getItem('clawbench-settings-inAppNotification')).toBe('false')

    setLocalConfig('inAppNotification', true)
    expect(localConfig.inAppNotification).toBe(true)
    expect(localStorage.getItem('clawbench-settings-inAppNotification')).toBe('true')

    localStorage.removeItem('clawbench-settings-inAppNotification')
  })

  // Browser/system notifications default ON: they are a separate channel from
  // the server-side push_mode, so an upgrading user who never touched this
  // switch keeps getting desktop alerts.
  it('localConfig has desktopNotification defaulting to true', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-desktopNotification')
    expect('desktopNotification' in localConfig).toBe(true)
    expect(localConfig.desktopNotification).toBe(true)
  })

  it('setLocalConfig persists desktopNotification to localStorage', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('desktopNotification', false)
    expect(localConfig.desktopNotification).toBe(false)
    expect(localStorage.getItem('clawbench-settings-desktopNotification')).toBe('false')

    setLocalConfig('desktopNotification', true)
    expect(localConfig.desktopNotification).toBe(true)
    expect(localStorage.getItem('clawbench-settings-desktopNotification')).toBe('true')

    localStorage.removeItem('clawbench-settings-desktopNotification')
  })

  // The key was renamed from browserNotification. An upgrading user who had
  // turned the switch OFF must stay off — a silent reset to the default would
  // start showing notifications they had deliberately disabled.
  it('migrates a stored browserNotification value to desktopNotification', async () => {
    localStorage.setItem('clawbench-settings-browserNotification', 'false')
    localStorage.removeItem('clawbench-settings-desktopNotification')

    // migrateLegacyKeys() runs at module load, so re-import to trigger it.
    vi.resetModules()
    const { useSettingsConfig: fresh } = await import('@/composables/useSettingsConfig')
    expect(fresh().localConfig.desktopNotification).toBe(false)
    expect(localStorage.getItem('clawbench-settings-desktopNotification')).toBe('false')

    localStorage.removeItem('clawbench-settings-browserNotification')
    localStorage.removeItem('clawbench-settings-desktopNotification')
  })

  it('does not let the legacy key override an explicit new value', async () => {
    localStorage.setItem('clawbench-settings-browserNotification', 'false')
    localStorage.setItem('clawbench-settings-desktopNotification', 'true')

    vi.resetModules()
    const { useSettingsConfig: fresh } = await import('@/composables/useSettingsConfig')
    expect(fresh().localConfig.desktopNotification).toBe(true)

    localStorage.removeItem('clawbench-settings-browserNotification')
    localStorage.removeItem('clawbench-settings-desktopNotification')
  })

  it('localConfig has messageDisplayMode defaulting to mixed', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-messageDisplayMode')
    expect('messageDisplayMode' in localConfig).toBe(true)
    expect(localConfig.messageDisplayMode).toBe('mixed')
  })

  it('setLocalConfig persists messageDisplayMode to localStorage', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('messageDisplayMode', 'original')
    expect(localConfig.messageDisplayMode).toBe('original')
    expect(localStorage.getItem('clawbench-settings-messageDisplayMode')).toBe('"original"')

    localStorage.removeItem('clawbench-settings-messageDisplayMode')
  })

  it('sanitizes an invalid persisted messageDisplayMode back to mixed', async () => {
    // Seed an invalid value before the module loads so the sanitizer runs.
    localStorage.setItem('clawbench-settings-messageDisplayMode', '"bogus"')
    vi.resetModules()
    const fresh = await import('@/composables/useSettingsConfig')
    expect(fresh.localConfig.messageDisplayMode).toBe('mixed')
    // The sanitized default is written back to localStorage.
    expect(localStorage.getItem('clawbench-settings-messageDisplayMode')).toBe('"mixed"')
    localStorage.removeItem('clawbench-settings-messageDisplayMode')
  })

  it('reads persisted localStorage value via setLocalConfig', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('showHidden', true)
    expect(localConfig.showHidden).toBe(true)
    expect(localStorage.getItem('clawbench-settings-showHidden')).toBe('true')

    // Clean up
    localStorage.removeItem('clawbench-settings-showHidden')
  })

  describe('agent preference helpers', () => {
    it('reads agent model preference from agent data', () => {
      const { getAgentModelPref } = useSettingsConfig()

      // No agent data → null
      mockGetAgent.mockReturnValue(null)
      expect(getAgentModelPref('test-agent')).toBeNull()

      // Agent with preferredModel set
      mockGetAgent.mockReturnValue({ id: 'test-agent', preferredModel: 'model-1' })
      expect(getAgentModelPref('test-agent')).toBe('model-1')

      // Agent without preferredModel
      mockGetAgent.mockReturnValue({ id: 'test-agent', preferredModel: '' })
      expect(getAgentModelPref('test-agent')).toBeNull()
    })

    it('reads agent thinking preference from agent data', () => {
      const { getAgentThinkingPref } = useSettingsConfig()

      // No agent data → null
      mockGetAgent.mockReturnValue(null)
      expect(getAgentThinkingPref('test-agent')).toBeNull()

      // Agent with preferredThinkingEffort set
      mockGetAgent.mockReturnValue({ id: 'test-agent', preferredThinkingEffort: 'high' })
      expect(getAgentThinkingPref('test-agent')).toBe('high')

      // Agent without preferredThinkingEffort
      mockGetAgent.mockReturnValue({ id: 'test-agent', preferredThinkingEffort: '' })
      expect(getAgentThinkingPref('test-agent')).toBeNull()
    })

    it('patchAgentPref calls PATCH /api/agents and updates local agent data', async () => {
      const { patchAgentPref } = useSettingsConfig()
      mockedApiPatch.mockResolvedValue({})

      await patchAgentPref('test-agent', 'preferred_model', 'model-1')

      expect(mockedApiPatch).toHaveBeenCalledWith('/api/agents', { id: 'test-agent', preferred_model: 'model-1' })
      expect(mockUpdateAgentField).toHaveBeenCalledWith('test-agent', 'preferredModel', 'model-1')

      // Test preferred_thinking_effort too
      await patchAgentPref('test-agent', 'preferred_thinking_effort', 'high')

      expect(mockedApiPatch).toHaveBeenCalledWith('/api/agents', { id: 'test-agent', preferred_thinking_effort: 'high' })
      expect(mockUpdateAgentField).toHaveBeenCalledWith('test-agent', 'preferredThinkingEffort', 'high')
    })
  })

  describe('concurrent setServerValue', () => {
    it('preserves first optimistic update when second call resolves', async () => {
      // Simulate: user changes chat.page_size then chat.initial_messages quickly
      // Both are under the "chat" key in serverConfig
      const { serverConfig, setServerValue, loadConfig } = useSettingsConfig()

      // Initialize serverConfig with chat sub-object
      mockedApiGet.mockResolvedValue({
        chat: { page_size: 20, initial_messages: 20, system_prompt_interval: 0 },
      })
      await loadConfig()

      // First PATCH resolves immediately
      // Second PATCH resolves after a tick
      let resolveFirst: (v: any) => void
      let resolveSecond: (v: any) => void
      const firstPromise = new Promise(r => { resolveFirst = r })
      const secondPromise = new Promise(r => { resolveSecond = r })

      mockedApiPatch.mockImplementationOnce(() => firstPromise)
      mockedApiPatch.mockImplementationOnce(() => secondPromise)

      // Fire both updates concurrently
      const p1 = setServerValue('chat.page_size', 50)
      const p2 = setServerValue('chat.initial_messages', 30)

      // Before API resolves, both should be optimistically applied
      expect(serverConfig.value.chat.page_size).toBe(50)
      expect(serverConfig.value.chat.initial_messages).toBe(30)

      // Resolve second call first (out of order)
      resolveSecond!({ needs_restart: false, changed_cold_fields: [] })
      await p2

      // page_size should still be 50, initial_messages should still be 30
      expect(serverConfig.value.chat.page_size).toBe(50)
      expect(serverConfig.value.chat.initial_messages).toBe(30)

      // Now resolve first call
      resolveFirst!({ needs_restart: false, changed_cold_fields: [] })
      await p1

      // Both values should be preserved
      expect(serverConfig.value.chat.page_size).toBe(50)
      expect(serverConfig.value.chat.initial_messages).toBe(30)
    })
  })

  describe('sortField and sortDir side effects', () => {
    it('setLocalConfig for sortField dispatches clawbench-sort-change event', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-sort-change', listener)

      setLocalConfig('sortField', 'name')

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({
        detail: { field: 'name' },
      }))

      window.removeEventListener('clawbench-sort-change', listener)
      // Clean up
      localStorage.removeItem('clawbench-settings-sortField')
    })

    it('setLocalConfig for sortField=null resets sortDir to asc', () => {
      const { localConfig, setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-sort-change', listener)

      // Set sortDir to something other than asc first
      setLocalConfig('sortDir', 'desc')

      // Now clear sortField → sortDir should reset to 'asc'
      setLocalConfig('sortField', null)

      expect(localConfig.sortDir).toBe('asc')

      window.removeEventListener('clawbench-sort-change', listener)
      localStorage.removeItem('clawbench-settings-sortField')
      localStorage.removeItem('clawbench-settings-sortDir')
    })

    it('setLocalConfig for sortDir dispatches sort-change event with dir', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-sort-change', listener)

      setLocalConfig('sortDir', 'desc')

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({
        detail: { dir: 'desc' },
      }))

      window.removeEventListener('clawbench-sort-change', listener)
      localStorage.removeItem('clawbench-settings-sortDir')
    })
  })

  describe('font config side effects', () => {
    it('setLocalConfig for fontMono dispatches clawbench-font-change', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-font-change', listener)

      setLocalConfig('fontMono', 'Fira Code')

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({
        detail: { fontMono: 'Fira Code' },
      }))

      window.removeEventListener('clawbench-font-change', listener)
      localStorage.removeItem('clawbench-settings-fontMono')
    })

    it('setLocalConfig for fontUi dispatches clawbench-font-change', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-font-change', listener)

      setLocalConfig('fontUi', 'Inter')

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({
        detail: { fontUi: 'Inter' },
      }))

      window.removeEventListener('clawbench-font-change', listener)
      localStorage.removeItem('clawbench-settings-fontUi')
    })

    it('setLocalConfig for fontMonoFallback dispatches clawbench-font-change', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-font-change', listener)

      setLocalConfig('fontMonoFallback', 'Sarasa Mono SC')

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({
        detail: { fontMonoFallback: 'Sarasa Mono SC' },
      }))

      window.removeEventListener('clawbench-font-change', listener)
      localStorage.removeItem('clawbench-settings-fontMonoFallback')
    })

    it('setLocalConfig for fontUiFallback dispatches clawbench-font-change', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-font-change', listener)

      setLocalConfig('fontUiFallback', 'SimSun')

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({
        detail: { fontUiFallback: 'SimSun' },
      }))

      window.removeEventListener('clawbench-font-change', listener)
      localStorage.removeItem('clawbench-settings-fontUiFallback')
    })
  })

  describe('patchAgentField', () => {
    it('patches agent field and updates local agent data', async () => {
      const { patchAgentField } = useSettingsConfig()
      mockedApiPatch.mockResolvedValue({})

      await patchAgentField('test-agent', 'name', 'New Name')

      expect(mockedApiPatch).toHaveBeenCalledWith('/api/agents', { id: 'test-agent', name: 'New Name' })
      expect(mockUpdateAgentField).toHaveBeenCalledWith('test-agent', 'name', 'New Name')
    })

    it('maps custom_system_prompt to customSystemPrompt', async () => {
      const { patchAgentField } = useSettingsConfig()
      mockedApiPatch.mockResolvedValue({})

      await patchAgentField('test-agent', 'custom_system_prompt', 'My custom prompt')

      expect(mockUpdateAgentField).toHaveBeenCalledWith('test-agent', 'customSystemPrompt', 'My custom prompt')
    })

    it('maps sort_order to sortOrder', async () => {
      const { patchAgentField } = useSettingsConfig()
      mockedApiPatch.mockResolvedValue({})

      await patchAgentField('test-agent', 'sort_order', 5)

      expect(mockUpdateAgentField).toHaveBeenCalledWith('test-agent', 'sortOrder', 5)
    })

    it('maps transport field', async () => {
      const { patchAgentField } = useSettingsConfig()
      mockedApiPatch.mockResolvedValue({})

      await patchAgentField('test-agent', 'transport', 'acp-stdio')

      expect(mockUpdateAgentField).toHaveBeenCalledWith('test-agent', 'transport', 'acp-stdio')
    })
  })

  describe('setServerValue — rollback', () => {
    it('rolls back local cache on patch failure', async () => {
      const { serverConfig, setServerValue, loadConfig } = useSettingsConfig()

      mockedApiGet.mockResolvedValue({ server: { port: 20000 } })
      await loadConfig()

      mockedApiPatch.mockRejectedValue(new Error('Network error'))

      await expect(setServerValue('server.port', 30000)).rejects.toThrow('Network error')

      // Value should be rolled back to original
      expect(serverConfig.value.server.port).toBe(20000)
    })
  })

  describe('patchConfig reloads serverConfig', () => {
    it('reloads config from server after patch (password fields get masked)', async () => {
      const { patchConfig, loadConfig, serverConfig } = useSettingsConfig()

      mockedApiGet.mockResolvedValue({
        chat: { page_size: 20, initial_messages: 20 },
      })
      await loadConfig()

      // After patch, loadConfig is called again — simulate server returning updated values
      mockedApiPatch.mockResolvedValue({ needs_restart: false, changed_cold_fields: [] })
      mockedApiGet.mockResolvedValue({
        chat: { page_size: 50, initial_messages: 20 },
      })
      await patchConfig({ chat: { page_size: 50 } })

      // serverConfig should reflect the reloaded values
      expect(serverConfig.value.chat.page_size).toBe(50)
      expect(serverConfig.value.chat.initial_messages).toBe(20)
    })
  })

  describe('loadConfig — error handling', () => {
    it('keeps existing cached values when API fails', async () => {
      const { loadConfig, serverConfig } = useSettingsConfig()

      // First load succeeds
      mockedApiGet.mockResolvedValue({ server: { port: 20000 } })
      await loadConfig()
      expect(serverConfig.value.server.port).toBe(20000)

      // Second load fails
      mockedApiGet.mockRejectedValue(new Error('Server unreachable'))
      await loadConfig()

      // Existing cached values should still be there
      expect(serverConfig.value.server.port).toBe(20000)
    })
  })

  describe('loadConfig — server limits sync', () => {
    it('mirrors session.max_count into the store immediately (no page reload)', async () => {
      const { loadConfig } = useSettingsConfig()
      mockedApiGet.mockResolvedValue({ session: { max_count: 15 }, chat: { page_size: 20 } })
      await loadConfig()
      expect(store.state.sessionMaxCount).toBe(15)
    })

    it('applies sessionMaxCount=0 (unlimited) instead of keeping a stale non-zero value', async () => {
      const { loadConfig } = useSettingsConfig()
      store.state.sessionMaxCount = 10
      mockedApiGet.mockResolvedValue({ session: { max_count: 0 } })
      await loadConfig()
      expect(store.state.sessionMaxCount).toBe(0)
    })

    it('syncs the other flat limits from their nested sections', async () => {
      const { loadConfig } = useSettingsConfig()
      mockedApiGet.mockResolvedValue({
        session: { max_count: 15 },
        recent_projects: { max_count: 12 },
        chat: { initial_messages: 30, page_size: 40 },
        upload: { max_size_mb: 200, max_files: 25 },
      })
      await loadConfig()
      expect(store.state.sessionMaxCount).toBe(15)
      expect(store.state.recentProjectsMaxCount).toBe(12)
      expect(store.state.chatInitialMessages).toBe(30)
      expect(store.state.chatPageSize).toBe(40)
      expect(store.state.uploadMaxSizeMB).toBe(200)
      expect(store.state.uploadMaxFiles).toBe(25)
    })
  })

  describe('theme side effect', () => {
    it('setLocalConfig for theme dispatches clawbench-theme-change', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-theme-change', listener)

      setLocalConfig('theme', 'dark')

      expect(listener).toHaveBeenCalled()
      const detail = listener.mock.calls[0][0].detail
      expect(detail).toBe('dark')

      window.removeEventListener('clawbench-theme-change', listener)
      localStorage.removeItem('clawbench-settings-theme')
    })
  })

  describe('autoSpeech side effect', () => {
    it('setLocalConfig for autoSpeech dispatches clawbench-autospeech-change', () => {
      const { setLocalConfig } = useSettingsConfig()
      const listener = vi.fn()
      window.addEventListener('clawbench-autospeech-change', listener)

      setLocalConfig('autoSpeech', true)

      expect(listener).toHaveBeenCalledWith(expect.objectContaining({ detail: true }))

      window.removeEventListener('clawbench-autospeech-change', listener)
      localStorage.removeItem('clawbench-settings-autoSpeech')
    })
  })

  describe('push_mode sync', () => {
    it('calls ClawBenchNative.setNativePushEnabled on loadConfig', async () => {
      const mockSetNativePushEnabled = vi.fn()
      const original = (window as any).ClawBenchNative
      ;(window as any).ClawBenchNative = { setNativePushEnabled: mockSetNativePushEnabled }

      // Mock API to return push_mode
      mockedApiGet.mockResolvedValueOnce({ push_mode: 'native' })

      const { loadConfig } = useSettingsConfig()
      await loadConfig()
      // Default push_mode is "native", so setNativePushEnabled should be called with true
      expect(mockSetNativePushEnabled).toHaveBeenCalledWith(true)

      // Restore
      if (original) {
        ;(window as any).ClawBenchNative = original
      } else {
        delete (window as any).ClawBenchNative
      }
    })

    it('calls ClawBenchNative.setNativePushEnabled(false) when push_mode is dingtalk', async () => {
      const mockSetNativePushEnabled = vi.fn()
      const original = (window as any).ClawBenchNative
      ;(window as any).ClawBenchNative = { setNativePushEnabled: mockSetNativePushEnabled }

      mockedApiGet.mockResolvedValueOnce({ push_mode: 'dingtalk' })

      const { loadConfig } = useSettingsConfig()
      await loadConfig()
      expect(mockSetNativePushEnabled).toHaveBeenCalledWith(false)

      if (original) {
        ;(window as any).ClawBenchNative = original
      } else {
        delete (window as any).ClawBenchNative
      }
    })
  })

  describe('floatingStatusWindow sync', () => {
    function installNativeMock() {
      const mockSetFloatingWindowEnabled = vi.fn()
      const original = (window as any).ClawBenchNative
      ;(window as any).ClawBenchNative = { setFloatingWindowEnabled: mockSetFloatingWindowEnabled }
      return { mockSetFloatingWindowEnabled, original }
    }
    function restoreNativeMock(original: unknown) {
      if (original) {
        ;(window as any).ClawBenchNative = original
      } else {
        delete (window as any).ClawBenchNative
      }
    }

    it('calls ClawBenchNative.setFloatingWindowEnabled(true) when setLocalConfig enables it', () => {
      const { mockSetFloatingWindowEnabled, original } = installNativeMock()
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('floatingStatusWindow', true)

      expect(mockSetFloatingWindowEnabled).toHaveBeenCalledWith(true)
      localStorage.removeItem('clawbench-settings-floatingStatusWindow')
      restoreNativeMock(original)
    })

    it('calls ClawBenchNative.setFloatingWindowEnabled(false) when setLocalConfig disables it', () => {
      const { mockSetFloatingWindowEnabled, original } = installNativeMock()
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('floatingStatusWindow', false)

      expect(mockSetFloatingWindowEnabled).toHaveBeenCalledWith(false)
      localStorage.removeItem('clawbench-settings-floatingStatusWindow')
      restoreNativeMock(original)
    })

    it('syncs the local value to ClawBenchNative on loadConfig', async () => {
      const { mockSetFloatingWindowEnabled, original } = installNativeMock()
      // Seed localStorage before module load so localConfig picks it up
      localStorage.setItem('clawbench-settings-floatingStatusWindow', 'true')
      vi.resetModules()
      // Re-import a fresh module instance (fresh mocks after resetModules)
      const { apiGet: freshApiGet } = await import('@/utils/api')
      vi.mocked(freshApiGet).mockResolvedValueOnce({})
      const { useSettingsConfig: freshUseSettingsConfig } = await import('@/composables/useSettingsConfig')

      const { loadConfig } = freshUseSettingsConfig()
      await loadConfig()

      expect(mockSetFloatingWindowEnabled).toHaveBeenCalledWith(true)

      localStorage.removeItem('clawbench-settings-floatingStatusWindow')
      restoreNativeMock(original)
    })
  })

  describe('liveUpdate sync', () => {
    function installNativeMock() {
      const mockSetLiveUpdateEnabled = vi.fn()
      const original = (window as any).ClawBenchNative
      ;(window as any).ClawBenchNative = { setLiveUpdateEnabled: mockSetLiveUpdateEnabled }
      return { mockSetLiveUpdateEnabled, original }
    }
    function restoreNativeMock(original: unknown) {
      if (original) {
        ;(window as any).ClawBenchNative = original
      } else {
        delete (window as any).ClawBenchNative
      }
    }

    it('calls ClawBenchNative.setLiveUpdateEnabled(true) when setLocalConfig enables it', () => {
      const { mockSetLiveUpdateEnabled, original } = installNativeMock()
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('liveUpdate', true)

      expect(mockSetLiveUpdateEnabled).toHaveBeenCalledWith(true)
      localStorage.removeItem('clawbench-settings-liveUpdate')
      restoreNativeMock(original)
    })

    it('calls ClawBenchNative.setLiveUpdateEnabled(false) when setLocalConfig disables it', () => {
      const { mockSetLiveUpdateEnabled, original } = installNativeMock()
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('liveUpdate', false)

      expect(mockSetLiveUpdateEnabled).toHaveBeenCalledWith(false)
      localStorage.removeItem('clawbench-settings-liveUpdate')
      restoreNativeMock(original)
    })

    it('syncs the local value to ClawBenchNative on loadConfig', async () => {
      const { mockSetLiveUpdateEnabled, original } = installNativeMock()
      localStorage.setItem('clawbench-settings-liveUpdate', 'true')
      vi.resetModules()
      const { apiGet: freshApiGet } = await import('@/utils/api')
      vi.mocked(freshApiGet).mockResolvedValueOnce({})
      const { useSettingsConfig: freshUseSettingsConfig } = await import('@/composables/useSettingsConfig')

      const { loadConfig } = freshUseSettingsConfig()
      await loadConfig()

      expect(mockSetLiveUpdateEnabled).toHaveBeenCalledWith(true)

      localStorage.removeItem('clawbench-settings-liveUpdate')
      restoreNativeMock(original)
    })

    it('opens the Live Updates settings when enabling without promotion permission', () => {
      const mockOpenSettings = vi.fn()
      const original = (window as any).ClawBenchNative
      ;(window as any).ClawBenchNative = {
        setLiveUpdateEnabled: vi.fn(),
        canPostPromotedNotifications: () => false,
        openLiveUpdateSettings: mockOpenSettings,
      }
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('liveUpdate', true)

      expect(mockOpenSettings).toHaveBeenCalledTimes(1)
      localStorage.removeItem('clawbench-settings-liveUpdate')
      if (original) {
        ;(window as any).ClawBenchNative = original
      } else {
        delete (window as any).ClawBenchNative
      }
    })

    it('does not open settings when promotion permission is already available', () => {
      const mockOpenSettings = vi.fn()
      const original = (window as any).ClawBenchNative
      ;(window as any).ClawBenchNative = {
        setLiveUpdateEnabled: vi.fn(),
        canPostPromotedNotifications: () => true,
        openLiveUpdateSettings: mockOpenSettings,
      }
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('liveUpdate', true)

      expect(mockOpenSettings).not.toHaveBeenCalled()
      localStorage.removeItem('clawbench-settings-liveUpdate')
      if (original) {
        ;(window as any).ClawBenchNative = original
      } else {
        delete (window as any).ClawBenchNative
      }
    })
  })

  // ── applyUIScale / getUIScale ──

  describe('applyUIScale', () => {
    it('applies zoom style for scale > 1', () => {
      applyUIScale(1.5)
      expect(document.documentElement.style.zoom).toBe('1.5')
    })

    it('applies zoom style for scale < 1', () => {
      applyUIScale(0.75)
      expect(document.documentElement.style.zoom).toBe('0.75')
    })

    it('clears zoom style when scale is 1', () => {
      applyUIScale(1)
      expect(document.documentElement.style.zoom).toBe('')
    })

    it('clamps scale to minimum 0.5', () => {
      applyUIScale(0.1)
      expect(document.documentElement.style.zoom).toBe('0.5')
    })

    it('clamps scale to maximum 2', () => {
      applyUIScale(3)
      expect(document.documentElement.style.zoom).toBe('2')
    })
  })

  describe('getUIScale', () => {
    it('returns 1 when no zoom is set', () => {
      document.documentElement.style.zoom = ''
      expect(getUIScale()).toBe(1)
    })

    it('returns current zoom value', () => {
      document.documentElement.style.zoom = '1.5'
      expect(getUIScale()).toBe(1.5)
      document.documentElement.style.zoom = ''
    })

    it('returns 1 for non-numeric zoom value', () => {
      document.documentElement.style.zoom = 'invalid'
      expect(getUIScale()).toBe(1)
      document.documentElement.style.zoom = ''
    })
  })

  describe('toFixedCSS', () => {
    it('divides viewportCoord by zoom factor', () => {
      document.documentElement.style.zoom = '2'
      expect(toFixedCSS(200)).toBe(100)
      document.documentElement.style.zoom = ''
    })

    it('returns same value when zoom is 1', () => {
      document.documentElement.style.zoom = ''
      expect(toFixedCSS(150)).toBe(150)
    })
  })

  describe('getZoomedViewport', () => {
    it('returns window inner dimensions', () => {
      const vp = getZoomedViewport()
      expect(vp.width).toBe(window.innerWidth)
      expect(vp.height).toBe(window.innerHeight)
    })
  })

  // ── uiScale side effect ──

  describe('uiScale side effect', () => {
    it('setLocalConfig for uiScale applies CSS zoom', () => {
      const { setLocalConfig } = useSettingsConfig()

      setLocalConfig('uiScale', 1.5)

      expect(document.documentElement.style.zoom).toBe('1.5')

      // Clean up
      document.documentElement.style.zoom = ''
      localStorage.removeItem('clawbench-settings-uiScale')
    })
  })

  // ── first-run appearance defaults ──

  describe('applyFirstRunThemeDefaults', () => {
    it('applies the out-of-box theme on a fresh install with no stored theme', () => {
      localStorage.removeItem('clawbench-settings-theme')
      localStorage.removeItem('theme')

      applyFirstRunThemeDefaults({ first_run: true })

      expect(localConfig.theme).toBe('gruvbox-dark')
      expect(localStorage.getItem('clawbench-settings-theme')).toBe(JSON.stringify('gruvbox-dark'))
      expect(document.documentElement.getAttribute('data-theme')).toBe('gruvbox-dark')
    })

    it('does not override a theme the user already picked', () => {
      localStorage.setItem('clawbench-settings-theme', JSON.stringify('dracula'))
      localConfig.theme = 'dracula'

      applyFirstRunThemeDefaults({ first_run: true })

      expect(localConfig.theme).toBe('dracula')
      expect(localStorage.getItem('clawbench-settings-theme')).toBe(JSON.stringify('dracula'))

      localStorage.removeItem('clawbench-settings-theme')
      localConfig.theme = 'auto'
    })

    it('respects a legacy stored theme key', () => {
      localStorage.removeItem('clawbench-settings-theme')
      localStorage.setItem('theme', 'nord')
      localConfig.theme = 'nord'

      applyFirstRunThemeDefaults({ first_run: true })

      expect(localConfig.theme).toBe('nord')
      expect(localStorage.getItem('clawbench-settings-theme')).toBeNull()

      localStorage.removeItem('theme')
      localConfig.theme = 'auto'
    })

    it('is a no-op on an existing install', () => {
      localStorage.removeItem('clawbench-settings-theme')
      localStorage.removeItem('theme')
      localStorage.removeItem('clawbench-fresh-theme-guess')
      localConfig.theme = 'auto'

      applyFirstRunThemeDefaults({ first_run: false })
      applyFirstRunThemeDefaults({})

      expect(localConfig.theme).toBe('auto')
      expect(localStorage.getItem('clawbench-settings-theme')).toBeNull()
    })

    it('reverts an optimistic first-visit guess when the server is not fresh', () => {
      // index.html guesses gruvbox-dark on a first-ever visit because it cannot
      // know first_run yet; a pre-existing server must undo that guess.
      // jsdom lacks matchMedia — resolveThemeId('auto') needs it.
      vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
      localStorage.removeItem('clawbench-settings-theme')
      localStorage.removeItem('theme')
      localStorage.setItem('clawbench-fresh-theme-guess', '1')
      localConfig.theme = 'gruvbox-dark'

      try {
        applyFirstRunThemeDefaults({ first_run: false })

        expect(localConfig.theme).toBe('auto')
        expect(localStorage.getItem('clawbench-settings-theme')).toBe(JSON.stringify('auto'))
        expect(localStorage.getItem('clawbench-fresh-theme-guess')).toBeNull()
      } finally {
        vi.unstubAllGlobals()
        localStorage.removeItem('clawbench-settings-theme')
      }
    })

    it('clears the guess marker once a fresh install is confirmed', () => {
      localStorage.removeItem('clawbench-settings-theme')
      localStorage.removeItem('theme')
      localStorage.setItem('clawbench-fresh-theme-guess', '1')
      localConfig.theme = 'gruvbox-dark'

      applyFirstRunThemeDefaults({ first_run: true })

      expect(localConfig.theme).toBe('gruvbox-dark')
      expect(localStorage.getItem('clawbench-fresh-theme-guess')).toBeNull()

      localStorage.removeItem('clawbench-settings-theme')
      localConfig.theme = 'auto'
    })
  })

  // ── system color-scheme following (issue #458) ──

  describe('applyStoredTheme', () => {
    it("applies the resolved auto theme but leaves the stored value as 'auto'", () => {
      vi.stubGlobal('matchMedia', vi.fn(() => ({
        matches: false,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })))
      try {
        localConfig.theme = 'auto'
        localStorage.setItem('clawbench-settings-theme', JSON.stringify('auto'))

        const resolved = applyStoredTheme()

        // Resolved to a concrete theme for the DOM …
        expect(resolved).toBe('github-light')
        expect(document.documentElement.getAttribute('data-theme')).toBe('github-light')
        // … but the *stored* setting must survive, or the settings page shows
        // a concrete theme and the app stops following the system forever.
        expect(localStorage.getItem('clawbench-settings-theme')).toBe(JSON.stringify('auto'))
        expect(localConfig.theme).toBe('auto')
      } finally {
        vi.unstubAllGlobals()
        document.documentElement.removeAttribute('data-theme')
        document.documentElement.removeAttribute('data-theme-base')
        localStorage.removeItem('clawbench-settings-theme')
        localConfig.theme = 'auto'
      }
    })

    it('applies a concrete stored theme as-is', () => {
      try {
        localConfig.theme = 'dracula'
        expect(applyStoredTheme()).toBe('dracula')
        expect(document.documentElement.getAttribute('data-theme')).toBe('dracula')
      } finally {
        document.documentElement.removeAttribute('data-theme')
        document.documentElement.removeAttribute('data-theme-base')
        localConfig.theme = 'auto'
      }
    })
  })

  describe('syncThemeFromSystem', () => {
    /**
     * Stub matchMedia with a fixed scheme and reset the document/localConfig
     * afterwards. The callback is driven by calling syncThemeFromSystem
     * directly — the listener wiring is covered by onSystemColorSchemeChange
     * and startSystemThemeWatcher specs.
     */
    function withScheme(dark: boolean, fn: () => void) {
      vi.stubGlobal('matchMedia', vi.fn(() => ({
        matches: dark,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })))
      try {
        fn()
      } finally {
        vi.unstubAllGlobals()
        document.documentElement.removeAttribute('data-theme')
        document.documentElement.removeAttribute('data-theme-base')
        localStorage.removeItem('clawbench-settings-theme')
        localConfig.theme = 'auto'
      }
    }

    it('re-resolves and applies when the stored value is auto', () => {
      withScheme(true, () => {
        localConfig.theme = 'auto'
        applyThemeAttributes('github-light')

        syncThemeFromSystem()

        expect(document.documentElement.getAttribute('data-theme')).toBe('github-dark')
        expect(document.documentElement.getAttribute('data-theme-base')).toBe('dark')
      })
    })

    it('notifies listeners via clawbench-theme-change', () => {
      withScheme(true, () => {
        localConfig.theme = 'auto'
        applyThemeAttributes('github-light')
        const listener = vi.fn()
        window.addEventListener('clawbench-theme-change', listener)

        try {
          syncThemeFromSystem()
          expect(listener).toHaveBeenCalledTimes(1)
          expect(listener.mock.calls[0][0].detail).toBe('github-dark')
        } finally {
          window.removeEventListener('clawbench-theme-change', listener)
        }
      })
    })

    it('does not touch the DOM when the resolved theme is already applied', () => {
      withScheme(true, () => {
        localConfig.theme = 'auto'
        applyThemeAttributes('github-dark')
        const listener = vi.fn()
        window.addEventListener('clawbench-theme-change', listener)

        try {
          // Resume events (visibilitychange/pageshow) fire even when nothing
          // changed — they must not re-render mermaid or ping native.
          syncThemeFromSystem()
          expect(listener).not.toHaveBeenCalled()
          expect(document.documentElement.getAttribute('data-theme')).toBe('github-dark')
        } finally {
          window.removeEventListener('clawbench-theme-change', listener)
        }
      })
    })

    it('ignores the system when the user picked a concrete theme', () => {
      withScheme(true, () => {
        localConfig.theme = 'dracula'
        applyThemeAttributes('dracula')
        const listener = vi.fn()
        window.addEventListener('clawbench-theme-change', listener)

        try {
          syncThemeFromSystem()
          expect(listener).not.toHaveBeenCalled()
          expect(document.documentElement.getAttribute('data-theme')).toBe('dracula')
        } finally {
          window.removeEventListener('clawbench-theme-change', listener)
        }
      })
    })
  })

  describe('startSystemThemeWatcher', () => {
    it('subscribes to the scheme change once and re-applies through syncThemeFromSystem', () => {
      const listeners = new Set<() => void>()
      vi.stubGlobal('matchMedia', vi.fn(() => ({
        matches: true,
        media: '(prefers-color-scheme: dark)',
        onchange: null,
        addEventListener: (_: string, cb: () => void) => { listeners.add(cb) },
        removeEventListener: (_: string, cb: () => void) => { listeners.delete(cb) },
        dispatchEvent: vi.fn(),
      })))
      const listener = vi.fn()
      try {
        localConfig.theme = 'auto'
        applyThemeAttributes('github-light')
        window.addEventListener('clawbench-theme-change', listener)

        // App.vue's registration runs on both cold start and post-login; a
        // second subscription would apply every scheme change twice.
        startSystemThemeWatcher()
        startSystemThemeWatcher()
        expect(listeners.size).toBe(1)

        for (const cb of [...listeners]) cb()
        expect(document.documentElement.getAttribute('data-theme')).toBe('github-dark')
        expect(listener).toHaveBeenCalledTimes(1)
      } finally {
        window.removeEventListener('clawbench-theme-change', listener)
        vi.unstubAllGlobals()
        document.documentElement.removeAttribute('data-theme')
        document.documentElement.removeAttribute('data-theme-base')
        localConfig.theme = 'auto'
      }
    })
  })
})
