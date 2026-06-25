import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

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
    expect('androidLogCapture' in localConfig).toBe(true)
    expect('swipeSession' in localConfig).toBe(true)
    expect('pushPersistentNotification' in localConfig).toBe(true)
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
        chat: { page_size: 20, initial_messages: 20, system_prompt_interval: 10 },
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

  describe('deepAssign', () => {
    it('deep-merges nested objects without losing siblings', async () => {
      const { patchConfig, loadConfig, serverConfig } = useSettingsConfig()

      mockedApiGet.mockResolvedValue({
        chat: { page_size: 20, initial_messages: 20 },
      })
      await loadConfig()

      mockedApiPatch.mockResolvedValue({ needs_restart: false, changed_cold_fields: [] })
      await patchConfig({ chat: { page_size: 50 } })

      // page_size should be updated, initial_messages should be preserved
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
})
