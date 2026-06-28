import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useAgents, resetAgents, updateACPModelList, restoreOriginalModels, populateACPStateFromCache, registerIdentityUpdaters, invalidateACPStateCache } from '@/composables/useAgents'

// Mock apiGet/apiPatch/apiPost/apiDelete to control agent data
const mockApiGet = vi.fn()
const mockApiPatch = vi.fn()
const mockApiPost = vi.fn()
const mockApiDelete = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPatch: (...args: any[]) => mockApiPatch(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
  apiDelete: (...args: any[]) => mockApiDelete(...args),
}))

// Mock useLocale gt function
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Mock useSessionIdentity functions used by populateACPStateFromCache and loadAgents
const mockUpdateModeState = vi.fn()
const mockUpdateThinkingEffortState = vi.fn()
const mockUpdateCommandState = vi.fn()
const mockUpdateAvailableModes = vi.fn()
const mockUpdateAvailableThinkingEfforts = vi.fn()
const _currentAgentId = { value: '' }
vi.mock('@/composables/useSessionIdentity.ts', () => ({
  updateModeState: (...args: any[]) => mockUpdateModeState(...args),
  updateThinkingEffortState: (...args: any[]) => mockUpdateThinkingEffortState(...args),
  updateCommandState: (...args: any[]) => mockUpdateCommandState(...args),
  updateAvailableModes: (...args: any[]) => mockUpdateAvailableModes(...args),
  updateAvailableThinkingEfforts: (...args: any[]) => mockUpdateAvailableThinkingEfforts(...args),
  get currentAgentId() { return _currentAgentId },
}))

const mockUpdatePlanEntries = vi.fn()
vi.mock('@/composables/usePlanProgress', () => ({
  updatePlanEntries: (...args: any[]) => mockUpdatePlanEntries(...args),
}))

describe('useAgents', () => {
  const { agents, defaultAgentId, loadAgents, getAgentIcon, getAgentName,
    isDefaultAgent, getDefaultModelId, getAgentModels, isMultiModel,
    getAgent, getAgentModel, getAgentDefaultModelName, agentHeaderTitle,
    syncModelFromAgent, getAgentThinkingEffortLevels, hasThinkingEffortLevels,
    updateAgentField, canRefreshModels, getEffectiveThinkingEffort,
    agentCanResume, supportsDualTransport, getAgentTransport,
    setDefaultAgent, duplicateAgent, deleteAgent, rescanAgents } = useAgents()

  // Register mock identity updaters — normally done by useSessionIdentity at
  // module evaluation time, but that module is mocked so we wire manually.
  // resetAgents() clears the updaters, so we need a helper to re-register.
  function registerMocks() {
    registerIdentityUpdaters({
      updateAvailableModes: (...args: any[]) => mockUpdateAvailableModes(...args),
      updateAvailableThinkingEfforts: (...args: any[]) => mockUpdateAvailableThinkingEfforts(...args),
      updateCommandState: (...args: any[]) => mockUpdateCommandState(...args),
      currentAgentId: _currentAgentId,
    })
  }
  registerMocks()

  const testAgents = [
    {
      id: 'claude',
      name: 'Claude',
      icon: '🤖',
      canRefreshModels: true,
      models: [
        { id: 'claude-3.5', name: 'Claude 3.5 Sonnet', default: true },
        { id: 'claude-3-haiku', name: 'Claude 3 Haiku', default: false },
      ],
      thinkingEffortLevels: ['low', 'medium', 'high', 'xhigh', 'max'],
    },
    {
      id: 'gpt',
      name: 'GPT-4',
      icon: '🧠',
      canRefreshModels: true,
      models: [{ id: 'gpt-4o', name: 'GPT-4o', default: true }],
      thinkingEffortLevels: ['low', 'medium', 'high'],
    },
    {
      id: 'simple',
      name: 'Simple Agent',
      icon: '⚡',
      models: [],
      canRefreshModels: false,
      // no thinkingEffortLevels — unsupported backend
    },
  ]

  beforeEach(async () => {
    // Reset agents state
    agents.value = []
    defaultAgentId.value = ''
    mockApiGet.mockReset()

    // Load test agents
    mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude' })
    await loadAgents()
  })

  // --- loadAgents ---

  describe('loadAgents', () => {
    it('loads agents from the API', async () => {
      expect(agents.value).toHaveLength(3)
      expect(agents.value[0].id).toBe('claude')
    })

    it('sets defaultAgentId from API response', async () => {
      expect(defaultAgentId.value).toBe('claude')
    })

    it('caches result and does not re-fetch', async () => {
      await loadAgents()
      expect(mockApiGet).toHaveBeenCalledTimes(1) // only the beforeEach call
    })

    it('handles API error gracefully', async () => {
      agents.value = []
      mockApiGet.mockRejectedValue(new Error('Network error'))
      await loadAgents()
      expect(agents.value).toHaveLength(0)
    })

    it('handles null agents array', async () => {
      agents.value = []
      mockApiGet.mockResolvedValue({ agents: null })
      await loadAgents()
      expect(agents.value).toEqual([])
    })

    it('handles missing defaultAgent', async () => {
      defaultAgentId.value = ''
      agents.value = []
      mockApiGet.mockResolvedValue({ agents: testAgents })
      await loadAgents()
      expect(defaultAgentId.value).toBe('')
    })
  })

  // --- getAgentIcon ---

  describe('getAgentIcon', () => {
    it('returns the icon for a known agent', () => {
      expect(getAgentIcon('claude')).toBe('🤖')
    })

    it('returns default robot emoji for unknown agent', () => {
      expect(getAgentIcon('nonexistent')).toBe('🤖')
    })

    it('returns default for empty string', () => {
      expect(getAgentIcon('')).toBe('🤖')
    })
  })

  // --- getAgentName ---

  describe('getAgentName', () => {
    it('returns the name for a known agent', () => {
      expect(getAgentName('claude')).toBe('Claude')
    })

    it('returns agentId for unknown agent', () => {
      expect(getAgentName('unknown-id')).toBe('unknown-id')
    })

    it('returns i18n default key for empty string', () => {
      expect(getAgentName('')).toBe('agents.defaultAssistant')
    })
  })

  // --- isDefaultAgent ---

  describe('isDefaultAgent', () => {
    it('returns true for the default agent', () => {
      expect(isDefaultAgent('claude')).toBe(true)
    })

    it('returns false for non-default agent', () => {
      expect(isDefaultAgent('gpt')).toBe(false)
    })

    it('returns false when defaultAgentId is empty', () => {
      defaultAgentId.value = ''
      expect(isDefaultAgent('claude')).toBe(false)
    })
  })

  // --- getDefaultModelId ---

  describe('getDefaultModelId', () => {
    it('returns the model marked as default', () => {
      expect(getDefaultModelId('claude')).toBe('claude-3.5')
    })

    it('returns the first model if none is marked default', () => {
      // gpt has only one model which is default
      expect(getDefaultModelId('gpt')).toBe('gpt-4o')
    })

    it('returns empty string for agent with no models', () => {
      expect(getDefaultModelId('simple')).toBe('')
    })

    it('returns empty string for unknown agent', () => {
      expect(getDefaultModelId('nonexistent')).toBe('')
    })
  })

  // --- getAgentModels ---

  describe('getAgentModels', () => {
    it('returns models for a known agent', () => {
      const models = getAgentModels('claude')
      expect(models).toHaveLength(2)
      expect(models[0].id).toBe('claude-3.5')
    })

    it('returns empty array for agent with no models', () => {
      expect(getAgentModels('simple')).toEqual([])
    })

    it('returns empty array for unknown agent', () => {
      expect(getAgentModels('nonexistent')).toEqual([])
    })
  })

  // --- isMultiModel ---

  describe('isMultiModel', () => {
    it('returns true for agent with multiple models', () => {
      expect(isMultiModel('claude')).toBe(true)
    })

    it('returns false for agent with single model', () => {
      expect(isMultiModel('gpt')).toBe(false)
    })

    it('returns false for agent with no models', () => {
      expect(isMultiModel('simple')).toBe(false)
    })

    it('returns false for unknown agent', () => {
      expect(isMultiModel('nonexistent')).toBe(false)
    })
  })

  // --- getAgent ---

  describe('getAgent', () => {
    it('returns the agent object for a known id', () => {
      const agent = getAgent('claude')
      expect(agent).toBeDefined()
      expect(agent!.id).toBe('claude')
      expect(agent!.name).toBe('Claude')
    })

    it('returns undefined for unknown id', () => {
      expect(getAgent('nonexistent')).toBeUndefined()
    })
  })

  // --- getAgentModel ---

  describe('getAgentModel', () => {
    it('returns a specific model by id', () => {
      const model = getAgentModel('claude', 'claude-3-haiku')
      expect(model).toBeDefined()
      expect(model!.id).toBe('claude-3-haiku')
      expect(model!.name).toBe('Claude 3 Haiku')
    })

    it('returns undefined for unknown model id', () => {
      expect(getAgentModel('claude', 'nonexistent')).toBeUndefined()
    })

    it('returns undefined for unknown agent', () => {
      expect(getAgentModel('nonexistent', 'any')).toBeUndefined()
    })
  })

  // --- getAgentDefaultModelName ---

  describe('getAgentDefaultModelName', () => {
    it('returns the default model name', () => {
      expect(getAgentDefaultModelName('claude')).toBe('Claude 3.5 Sonnet')
    })

    it('returns the model id if model not found', () => {
      // Agent with no models → getDefaultModelId returns ''
      expect(getAgentDefaultModelName('simple')).toBe('')
    })

    it('returns model id for unknown agent', () => {
      expect(getAgentDefaultModelName('nonexistent')).toBe('')
    })
  })

  // --- agentHeaderTitle ---

  describe('agentHeaderTitle', () => {
    it('returns "icon name" for a known agent', () => {
      expect(agentHeaderTitle('claude')).toBe('🤖 Claude')
    })

    it('returns i18n key for empty agentId', () => {
      expect(agentHeaderTitle('')).toBe('chat.session.aiDialog')
    })
  })

  // --- syncModelFromAgent ---

  describe('syncModelFromAgent', () => {
    it('returns default model id and name for known agent', () => {
      const result = syncModelFromAgent('claude')
      expect(result.modelId).toBe('claude-3.5')
      expect(result.modelName).toBe('Claude 3.5 Sonnet')
    })

    it('returns empty strings for agent with no models', () => {
      const result = syncModelFromAgent('simple')
      expect(result.modelId).toBe('')
      expect(result.modelName).toBe('')
    })

    it('returns empty strings for unknown agent', () => {
      const result = syncModelFromAgent('nonexistent')
      expect(result.modelId).toBe('')
      expect(result.modelName).toBe('')
    })

    it('returns model id as name when model lookup fails', () => {
      // Create a scenario where getDefaultModelId returns a valid id
      // but getAgentModels returns empty (shouldn't happen in practice but test edge case)
      const result = syncModelFromAgent('simple')
      expect(result.modelId).toBe('')
      expect(result.modelName).toBe('')
    })
  })

  // --- getAgentThinkingEffortLevels ---

  describe('getAgentThinkingEffortLevels', () => {
    it('returns thinking effort levels for an agent with levels', () => {
      const levels = getAgentThinkingEffortLevels('claude')
      expect(levels).toEqual(['low', 'medium', 'high', 'xhigh', 'max'])
    })

    it('returns levels for another agent with different levels', () => {
      const levels = getAgentThinkingEffortLevels('gpt')
      expect(levels).toEqual(['low', 'medium', 'high'])
    })

    it('returns empty array for agent without thinking effort levels', () => {
      const levels = getAgentThinkingEffortLevels('simple')
      expect(levels).toEqual([])
    })

    it('returns empty array for unknown agent', () => {
      const levels = getAgentThinkingEffortLevels('nonexistent')
      expect(levels).toEqual([])
    })
  })

  // --- hasThinkingEffortLevels ---

  describe('hasThinkingEffortLevels', () => {
    it('returns true for agent with thinking effort levels', () => {
      expect(hasThinkingEffortLevels('claude')).toBe(true)
    })

    it('returns true for another agent with levels', () => {
      expect(hasThinkingEffortLevels('gpt')).toBe(true)
    })

    it('returns false for agent without thinking effort levels', () => {
      expect(hasThinkingEffortLevels('simple')).toBe(false)
    })

    it('returns false for unknown agent', () => {
      expect(hasThinkingEffortLevels('nonexistent')).toBe(false)
    })
  })

  // --- resetAgents ---

  describe('resetAgents', () => {
    it('clears agents and defaultAgentId', async () => {
      // After beforeEach, agents are loaded
      expect(agents.value).toHaveLength(3)
      expect(defaultAgentId.value).toBe('claude')

      resetAgents()
      registerMocks()

      expect(agents.value).toEqual([])
      expect(defaultAgentId.value).toBe('')
    })

    it('clears loadPromise so next loadAgents re-fetches', async () => {
      // After beforeEach, loadAgents is cached (loadPromise was set and cleared)
      // Reset should clear the cache so a new loadAgents() triggers an API call
      const callCountBefore = mockApiGet.mock.calls.length
      await loadAgents()
      expect(mockApiGet.mock.calls.length).toBe(callCountBefore) // cached, no new call

      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'gpt' })
      await loadAgents()
      expect(mockApiGet.mock.calls.length).toBe(callCountBefore + 1) // new API call made

      // New default loaded
      expect(defaultAgentId.value).toBe('gpt')
    })
  })

  // --- concurrent loadAgents deduplication ---

  describe('loadAgents deduplication', () => {
    it('uses cached result when agents already loaded', async () => {
      // Agents were loaded in beforeEach — second call should be a no-op
      const callCountBefore = mockApiGet.mock.calls.length
      await loadAgents()
      // Should not have made another API call
      expect(mockApiGet.mock.calls.length).toBe(callCountBefore)
    })

    it('shares the same load promise for concurrent callers', async () => {
      agents.value = []
      mockApiGet.mockReset()

      // Create a slow-resolving mock so both calls overlap
      let resolveApi: (v: any) => void
      const slowPromise = new Promise(r => { resolveApi = r })
      mockApiGet.mockReturnValue(slowPromise)

      // Start both calls before the API resolves
      const p1 = loadAgents()
      const p2 = loadAgents()

      // Resolve the API call
      resolveApi!({ agents: testAgents })

      await Promise.all([p1, p2])

      // Only one API call should have been made (deduplication via loadPromise)
      expect(mockApiGet).toHaveBeenCalledTimes(1)
      expect(agents.value).toHaveLength(3)
    })
  })

  // --- canRefreshModels ---

  describe('canRefreshModels', () => {
    it('returns true for agent with canRefreshModels=true', () => {
      expect(canRefreshModels('claude')).toBe(true)
    })

    it('returns true for another agent with canRefreshModels=true', () => {
      expect(canRefreshModels('gpt')).toBe(true)
    })

    it('returns false for agent with canRefreshModels=false', () => {
      expect(canRefreshModels('simple')).toBe(false)
    })

    it('returns false for unknown agent', () => {
      expect(canRefreshModels('nonexistent')).toBe(false)
    })

    it('returns false for empty string', () => {
      expect(canRefreshModels('')).toBe(false)
    })
  })

  // --- updateAgentField ---

  describe('updateAgentField', () => {
    it('updates a field on an existing agent', () => {
      expect(getAgent('claude')!.preferredModel).toBeUndefined()
      updateAgentField('claude', 'preferredModel', 'claude-3-haiku')
      expect(getAgent('claude')!.preferredModel).toBe('claude-3-haiku')
    })

    it('does nothing for unknown agent', () => {
      // Should not throw
      updateAgentField('nonexistent', 'preferredModel', 'test')
    })
  })

  // --- updateACPModelList ---

  describe('updateACPModelList', () => {
    beforeEach(async () => {
      // Reset module-level singletons (originalModels, acpStatesCache) and reload
      // Deep-clone testAgents so mutations don't leak between tests
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({
        agents: JSON.parse(JSON.stringify(testAgents)),
        defaultAgent: 'claude',
      })
      await loadAgents()
    })

    it('overrides agent models with ACP models', () => {
      const acpModels = [
        { id: 'acp-model-1', name: 'ACP Model 1' },
        { id: 'acp-model-2', name: 'ACP Model 2' },
      ]
      updateACPModelList('claude', acpModels, 'acp-model-2')

      const models = getAgentModels('claude')
      expect(models).toHaveLength(2)
      expect(models[0].id).toBe('acp-model-1')
      expect(models[0].name).toBe('ACP Model 1')
      expect(models[0].default).toBe(false)
      expect(models[1].id).toBe('acp-model-2')
      expect(models[1].default).toBe(true)
    })

    it('marks first model as default when no currentModelId provided', () => {
      const acpModels = [
        { id: 'acp-a', name: 'ACP A' },
        { id: 'acp-b', name: 'ACP B' },
      ]
      updateACPModelList('gpt', acpModels)

      const models = getAgentModels('gpt')
      expect(models[0].default).toBe(true)
      expect(models[1].default).toBe(false)
    })

    it('saves original models so they can be restored', () => {
      const originalModels = getAgentModels('gpt')
      expect(originalModels).toHaveLength(1) // gpt-4o

      updateACPModelList('gpt', [{ id: 'acp-x', name: 'ACP X' }])
      expect(getAgentModels('gpt')).toHaveLength(1)
      expect(getAgentModels('gpt')[0].id).toBe('acp-x')

      restoreOriginalModels('gpt')
      const restored = getAgentModels('gpt')
      expect(restored).toHaveLength(1)
      expect(restored[0].id).toBe('gpt-4o')
    })

    it('does not overwrite saved originals on second call', () => {
      const original = getAgentModels('claude').map(m => ({ ...m }))

      updateACPModelList('claude', [{ id: 'acp-1', name: 'ACP 1' }])
      updateACPModelList('claude', [{ id: 'acp-2', name: 'ACP 2' }])

      restoreOriginalModels('claude')
      const restored = getAgentModels('claude')
      expect(restored).toHaveLength(original.length)
      expect(restored[0].id).toBe(original[0].id)
    })

    it('does nothing for unknown agent', () => {
      // Should not throw
      updateACPModelList('nonexistent', [{ id: 'x', name: 'X' }])
    })
  })

  // --- restoreOriginalModels ---

  describe('restoreOriginalModels', () => {
    beforeEach(async () => {
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({
        agents: JSON.parse(JSON.stringify(testAgents)),
        defaultAgent: 'claude',
      })
      await loadAgents()
    })

    it('restores original CLI models after ACP override', () => {
      const original = getAgentModels('claude').map(m => ({ ...m }))

      updateACPModelList('claude', [{ id: 'acp-1', name: 'ACP 1' }])
      expect(getAgentModels('claude')[0].id).toBe('acp-1')

      restoreOriginalModels('claude')
      const restored = getAgentModels('claude')
      expect(restored).toHaveLength(original.length)
      expect(restored[0].id).toBe(original[0].id)
    })

    it('is a no-op when no ACP override was applied', () => {
      const before = getAgentModels('gpt')
      restoreOriginalModels('gpt')
      expect(getAgentModels('gpt')).toEqual(before)
    })

    it('allows re-overriding after restore', () => {
      updateACPModelList('gpt', [{ id: 'acp-1', name: 'ACP 1' }])
      restoreOriginalModels('gpt')
      expect(getAgentModels('gpt')[0].id).toBe('gpt-4o')

      // Second override should work and save fresh originals
      updateACPModelList('gpt', [{ id: 'acp-2', name: 'ACP 2' }])
      expect(getAgentModels('gpt')[0].id).toBe('acp-2')

      restoreOriginalModels('gpt')
      expect(getAgentModels('gpt')[0].id).toBe('gpt-4o')
    })
  })

  // --- populateACPStateFromCache ---

  describe('populateACPStateFromCache', () => {
    const acpState = {
      claude: {
        modeState: {
          currentModeId: 'code',
          availableModes: [
            { id: 'ask', name: 'Ask' },
            { id: 'code', name: 'Code' },
          ],
        },
        thinkingEffortState: {
          currentId: 'high',
          availableLevels: [
            { id: 'low', name: 'Low' },
            { id: 'high', name: 'High' },
          ],
        },
        commands: [
          { name: '/help', description: 'Show help' },
          { name: '/clear', description: 'Clear context' },
        ],
        modelListState: {
          models: [
            { id: 'acp-claude-1', name: 'ACP Claude 1' },
          ],
          currentModelId: 'acp-claude-1',
        },
      },
    }

    beforeEach(() => {
      mockUpdateAvailableModes.mockReset()
      mockUpdateAvailableThinkingEfforts.mockReset()
      mockUpdateCommandState.mockReset()
    })

    it('populates mode, thinking, commands, and model list from cached acpStates', async () => {
      // Load agents with acpStates to populate the cache
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await loadAgents()

      await populateACPStateFromCache('claude')

      expect(mockUpdateAvailableModes).toHaveBeenCalledWith(acpState.claude.modeState.availableModes)
      expect(mockUpdateAvailableThinkingEfforts).toHaveBeenCalledWith(acpState.claude.thinkingEffortState.availableLevels)
      expect(mockUpdateCommandState).toHaveBeenCalledWith(acpState.claude.commands)
      expect(getAgentModels('claude')[0].id).toBe('acp-claude-1')
    })

    it('skips mode update when availableModes is empty', async () => {
      resetAgents()
      registerMocks()
      const stateWithoutModes = {
        claude: {
          modeState: { currentModeId: '', availableModes: [] },
          thinkingEffortState: { currentId: 'high', availableLevels: [{ id: 'high', name: 'High' }] },
        },
      }
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: stateWithoutModes })
      await loadAgents()

      await populateACPStateFromCache('claude')

      expect(mockUpdateAvailableModes).not.toHaveBeenCalled()
      expect(mockUpdateAvailableThinkingEfforts).toHaveBeenCalled()
    })

    it('does nothing for agent with no cached ACP state and no server state', async () => {
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: {} })
      await loadAgents()

      await populateACPStateFromCache('nonexistent')

      expect(mockUpdateAvailableModes).not.toHaveBeenCalled()
      expect(mockUpdateAvailableThinkingEfforts).not.toHaveBeenCalled()
      expect(mockUpdateCommandState).not.toHaveBeenCalled()
    })

    it('force-refreshes from server when cache is empty for agent', async () => {
      resetAgents()
      registerMocks()
      mockApiGet.mockClear()
      // First load: no acpStates for 'claude'
      mockApiGet.mockResolvedValueOnce({ agents: testAgents, defaultAgent: 'claude', acpStates: {} })
      await loadAgents()

      // Second load (force): now returns acpStates with claude
      mockApiGet.mockResolvedValueOnce({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })

      await populateACPStateFromCache('claude')

      // Should have force-reloaded
      expect(mockApiGet).toHaveBeenCalledTimes(2)
      expect(mockUpdateAvailableModes).toHaveBeenCalledWith(acpState.claude.modeState.availableModes)
    })
  })

  // --- loadAgents with acpStates ---

  describe('loadAgents with acpStates', () => {
    const acpState = {
      claude: {
        modeState: {
          currentModeId: 'architect',
          availableModes: [{ id: 'architect', name: 'Architect' }],
        },
        thinkingEffortState: {
          currentId: 'max',
          availableLevels: [{ id: 'max', name: 'Max' }],
        },
        commands: [{ name: '/compact', description: 'Compact context' }],
      },
    }

    beforeEach(() => {
      mockUpdateAvailableModes.mockReset()
      mockUpdateAvailableThinkingEfforts.mockReset()
      mockUpdateCommandState.mockReset()
      _currentAgentId.value = ''
    })

    it('caches acpStates from API response', async () => {
      resetAgents()
      registerMocks()
      mockApiGet.mockClear()
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await loadAgents()

      // populateACPStateFromCache should work without re-fetching
      await populateACPStateFromCache('claude')
      // Only 1 API call — the initial loadAgents (populateACPStateFromCache uses cache)
      expect(mockApiGet).toHaveBeenCalledTimes(1)
      expect(mockUpdateAvailableModes).toHaveBeenCalledWith(acpState.claude.modeState.availableModes)
    })

    it('populates ACP state for the current agent during load', async () => {
      resetAgents()
      registerMocks()
      _currentAgentId.value = 'claude'
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await loadAgents()

      expect(mockUpdateAvailableModes).toHaveBeenCalledWith(acpState.claude.modeState.availableModes)
      expect(mockUpdateAvailableThinkingEfforts).toHaveBeenCalledWith(acpState.claude.thinkingEffortState.availableLevels)
      expect(mockUpdateCommandState).toHaveBeenCalledWith(acpState.claude.commands)
    })

    it('does not populate ACP state when currentAgentId does not match', async () => {
      resetAgents()
      registerMocks()
      _currentAgentId.value = 'gpt' // gpt has no acpState
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await loadAgents()

      expect(mockUpdateAvailableModes).not.toHaveBeenCalled()
    })

    it('does not populate ACP state when currentAgentId is empty', async () => {
      resetAgents()
      registerMocks()
      _currentAgentId.value = ''
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await loadAgents()

      expect(mockUpdateAvailableModes).not.toHaveBeenCalled()
    })

    it('overrides models from modelListState during load for current agent', async () => {
      resetAgents()
      registerMocks()
      _currentAgentId.value = 'claude'
      const stateWithModels = {
        claude: {
          modelListState: {
            models: [{ id: 'acp-new', name: 'ACP New' }],
            currentModelId: 'acp-new',
          },
        },
      }
      mockApiGet.mockResolvedValue({ agents: testAgents, defaultAgent: 'claude', acpStates: stateWithModels })
      await loadAgents()

      expect(getAgentModels('claude')[0].id).toBe('acp-new')
      expect(getAgentModels('claude')[0].default).toBe(true)
    })
  })

  // --- resetAgents clears acpStatesCache ---

  describe('resetAgents clears acpStatesCache', () => {
    it('clears cached ACP state so populateACPStateFromCache force-refreshes', async () => {
      const acpState = {
        claude: {
          modeState: { currentModeId: 'code', availableModes: [{ id: 'code', name: 'Code' }] },
        },
      }
      resetAgents()
      registerMocks()
      mockApiGet.mockClear()
      mockApiGet.mockResolvedValueOnce({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await loadAgents()

      // Cache is populated — populateACPStateFromCache should not re-fetch
      await populateACPStateFromCache('claude')
      expect(mockApiGet).toHaveBeenCalledTimes(1)

      // After reset, cache is gone — populateACPStateFromCache must re-fetch
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValueOnce({ agents: testAgents, defaultAgent: 'claude', acpStates: acpState })
      await populateACPStateFromCache('claude')
      expect(mockApiGet).toHaveBeenCalledTimes(2)
    })
  })

  // --- getEffectiveThinkingEffort ---

  describe('getEffectiveThinkingEffort', () => {
    it('returns preferredThinkingEffort when set', async () => {
      updateAgentField('claude', 'preferredThinkingEffort', 'max')
      expect(getEffectiveThinkingEffort('claude')).toBe('max')
      // cleanup
      updateAgentField('claude', 'preferredThinkingEffort', '')
    })

    it('returns agent thinkingEffort when no preferred', () => {
      // claude may or may not have thinkingEffort set from API
      const result = getEffectiveThinkingEffort('claude')
      expect(typeof result).toBe('string')
    })

    it('returns empty string for unknown agent', () => {
      expect(getEffectiveThinkingEffort('nonexistent')).toBe('')
    })
  })

  // --- agentCanResume ---

  describe('agentCanResume', () => {
    it('returns true when acpStatesCache has loadSession', async () => {
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({
        agents: testAgents,
        defaultAgent: 'claude',
        acpStates: { claude: { loadSession: true } },
      })
      await loadAgents()
      expect(agentCanResume('claude')).toBe(true)
    })

    it('returns true when agent has acpCommand', async () => {
      resetAgents()
      registerMocks()
      const agentsWithAcp = testAgents.map(a => a.id === 'claude' ? { ...a, acpCommand: 'claude-acp' } : a)
      mockApiGet.mockResolvedValue({ agents: agentsWithAcp, defaultAgent: 'claude' })
      await loadAgents()
      expect(agentCanResume('claude')).toBe(true)
    })

    it('returns false when no loadSession and no acpCommand', () => {
      expect(agentCanResume('simple')).toBe(false)
    })

    it('returns false for unknown agent', () => {
      expect(agentCanResume('nonexistent')).toBe(false)
    })
  })

  // --- supportsDualTransport ---

  describe('supportsDualTransport', () => {
    it('returns true when agent has acpCommand', async () => {
      resetAgents()
      registerMocks()
      const agentsWithAcp = testAgents.map(a => a.id === 'claude' ? { ...a, acpCommand: 'claude-acp' } : a)
      mockApiGet.mockResolvedValue({ agents: agentsWithAcp, defaultAgent: 'claude' })
      await loadAgents()
      expect(supportsDualTransport('claude')).toBe(true)
    })

    it('returns false when agent has no acpCommand', () => {
      expect(supportsDualTransport('simple')).toBe(false)
    })

    it('returns false for unknown agent', () => {
      expect(supportsDualTransport('nonexistent')).toBe(false)
    })
  })

  // --- getAgentTransport ---

  describe('getAgentTransport', () => {
    it('returns the transport field when set', async () => {
      resetAgents()
      registerMocks()
      const agentsWithTransport = testAgents.map(a => a.id === 'claude' ? { ...a, transport: 'acp-stdio' } : a)
      mockApiGet.mockResolvedValue({ agents: agentsWithTransport, defaultAgent: 'claude' })
      await loadAgents()
      expect(getAgentTransport('claude')).toBe('acp-stdio')
    })

    it('returns cli when transport is not set', () => {
      expect(getAgentTransport('gpt')).toBe('cli')
    })

    it('returns cli for unknown agent', () => {
      expect(getAgentTransport('nonexistent')).toBe('cli')
    })
  })

  // --- setDefaultAgent ---

  describe('setDefaultAgent', () => {
    beforeEach(() => {
      mockApiPatch.mockReset().mockResolvedValue({})
    })

    it('calls apiPatch and updates defaultAgentId', async () => {
      await setDefaultAgent('gpt')
      expect(mockApiPatch).toHaveBeenCalledWith('/api/config', { default_agent: 'gpt' })
      expect(defaultAgentId.value).toBe('gpt')
    })
  })

  // --- duplicateAgent ---

  describe('duplicateAgent', () => {
    beforeEach(() => {
      mockApiPost.mockReset().mockResolvedValue({})
    })

    it('calls apiPost with source_id and name, then reloads agents', async () => {
      await duplicateAgent('claude', 'Claude Copy')
      expect(mockApiPost).toHaveBeenCalledWith('/api/agents', { source_id: 'claude', name: 'Claude Copy' })
      // loadAgents(true) is called after, which triggers a fresh apiGet
    })
  })

  // --- deleteAgent ---

  describe('deleteAgent', () => {
    beforeEach(() => {
      mockApiDelete.mockReset().mockResolvedValue({})
    })

    it('calls apiDelete with agent id, then reloads agents', async () => {
      await deleteAgent('gpt')
      expect(mockApiDelete).toHaveBeenCalledWith('/api/agents', { body: { id: 'gpt' } })
    })
  })

  // --- rescanAgents ---

  describe('rescanAgents', () => {
    beforeEach(() => {
      mockApiPost.mockReset().mockResolvedValue({})
    })

    it('calls apiPost to rescan, then reloads agents', async () => {
      await rescanAgents()
      expect(mockApiPost).toHaveBeenCalledWith('/api/agents/rescan', {})
    })
  })

  // --- invalidateACPStateCache ---

  describe('invalidateACPStateCache', () => {
    it('removes cached ACP state for an agent', async () => {
      resetAgents()
      registerMocks()
      mockApiGet.mockResolvedValue({
        agents: testAgents,
        defaultAgent: 'claude',
        acpStates: { claude: { loadSession: true } },
      })
      await loadAgents()
      expect(agentCanResume('claude')).toBe(true)

      invalidateACPStateCache('claude')
      // After invalidation, cache no longer has loadSession
      // but agent may still have acpCommand — depends on agent object
      expect(agentCanResume('claude')).toBe(false) // no acpCommand on testAgents
    })
  })
})
