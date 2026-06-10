import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Mock useAgents before importing useSessionIdentity
const mockGetAgentModel = vi.fn()
const mockSyncModelFromAgent = vi.fn()

// Track agent preferences in-memory so getAgent returns them
// preferredModel is set exclusively via the settings panel (PATCH /api/agents)
const agentPrefs: Record<string, { preferredModel?: string; preferredThinkingEffort?: string }> = {}

const mockGetAgent = vi.fn((agentId: string) => {
  const pref = agentPrefs[agentId]
  if (!pref) return null
  return { id: agentId, preferredModel: pref.preferredModel || '', preferredThinkingEffort: pref.preferredThinkingEffort || '' }
})

const mockGetEffectiveThinkingEffort = vi.fn((agentId: string) => {
  const pref = agentPrefs[agentId]
  if (!pref) return ''
  return pref.preferredThinkingEffort || ''
})

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: { value: [{ id: 'codebuddy' }, { id: 'claude' }] },
    loadAgents: vi.fn().mockResolvedValue(undefined),
    getAgentModel: mockGetAgentModel,
    getAgentName: vi.fn().mockReturnValue('Test'),
    syncModelFromAgent: mockSyncModelFromAgent,
    getAgentIcon: vi.fn().mockReturnValue('🤖'),
    agentHeaderTitle: vi.fn().mockReturnValue('🤖 Test'),
    getAgent: mockGetAgent,
    getEffectiveThinkingEffort: mockGetEffectiveThinkingEffort,
  }),
}))

// Mock useSettingsConfig — saveModelPref is now a no-op, but patchAgentPref
// is still used by the settings panel. Keep the mock for saveThinkingPref.
vi.mock('@/composables/useSettingsConfig', () => ({
  patchAgentPref: (agentId: string, field: 'preferred_model' | 'preferred_thinking_effort', value: string) => {
    if (!agentPrefs[agentId]) agentPrefs[agentId] = {}
    if (field === 'preferred_thinking_effort') agentPrefs[agentId].preferredThinkingEffort = value
    // Note: preferred_model is no longer written by saveModelPref —
    // only the settings panel updates it, which bypasses this mock.
    return Promise.resolve()
  },
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Import after mocks are set up — use the composable to access helper functions
import { useSessionIdentity } from '@/composables/useSessionIdentity'

describe('useSessionIdentity - model preference', () => {
  let saveModelPref: (agentId: string, modelId: string) => void
  let loadModelPref: (agentId: string) => string | null
  let saveThinkingPref: (agentId: string, level: string) => void
  let loadThinkingPref: (agentId: string) => string | null

  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    // Reset in-memory agent preferences
    for (const key of Object.keys(agentPrefs)) {
      delete agentPrefs[key]
    }
    const identity = useSessionIdentity()
    saveModelPref = identity.saveModelPref
    loadModelPref = identity.loadModelPref
    saveThinkingPref = identity.saveThinkingPref
    loadThinkingPref = identity.loadThinkingPref
  })

  afterEach(() => {
    localStorage.clear()
  })

  describe('saveModelPref', () => {
    it('is a no-op — does not persist model preference', async () => {
      await saveModelPref('codebuddy', 'glm-5.1')
      // saveModelPref no longer writes to agent preferredModel;
      // loadModelPref reads from agent.preferredModel which is set
      // exclusively via the settings panel.
      expect(loadModelPref('codebuddy')).toBeNull()
    })

    it('handles empty agentId gracefully', async () => {
      await saveModelPref('', 'glm-5.1')
      // Should not throw
    })

    it('handles empty modelId gracefully', async () => {
      await saveModelPref('codebuddy', '')
      // Should not throw
    })
  })

  describe('loadModelPref', () => {
    it('reads preferredModel set via settings panel', () => {
      // Simulate settings panel setting preferredModel
      agentPrefs['codebuddy'] = { preferredModel: 'glm-5.1' }
      expect(loadModelPref('codebuddy')).toBe('glm-5.1')
    })

    it('returns null when no preference set', () => {
      expect(loadModelPref('claude')).toBeNull()
    })

    it('returns null for empty agentId', () => {
      expect(loadModelPref('')).toBeNull()
    })
  })

  describe('saveThinkingPref / loadThinkingPref', () => {
    it('saveThinkingPref is a no-op — does not persist thinking preference', async () => {
      await saveThinkingPref('codebuddy', 'high')
      // saveThinkingPref no longer writes to agent preferredThinkingEffort;
      // loadThinkingPref reads from agent.preferredThinkingEffort which is set
      // exclusively via the settings panel or SessionSettingModal star button.
      expect(loadThinkingPref('codebuddy')).toBeNull()
    })

    it('returns null when no preference saved', () => {
      expect(loadThinkingPref('claude')).toBeNull()
    })
  })

  describe('model resolution priority (syncModelFromData behavior)', () => {
    // Priority: server modelId > preferredModel (settings panel) > agent default

    it('when server modelId is empty, preferredModel from settings panel is used', () => {
      // Simulate settings panel setting preferredModel
      agentPrefs['codebuddy'] = { preferredModel: 'claude-sonnet-4-6' }

      const serverModelId = ''
      const agentId = 'codebuddy'

      let resolvedModelId = ''
      if (serverModelId) {
        resolvedModelId = serverModelId
      } else {
        const saved = loadModelPref(agentId)
        if (saved) {
          resolvedModelId = saved
        }
      }

      expect(resolvedModelId).toBe('claude-sonnet-4-6')
    })

    it('when server modelId is set, it takes priority over preferredModel', () => {
      // Settings panel preference
      agentPrefs['codebuddy'] = { preferredModel: 'glm-5.1' }

      const serverModelId = 'claude-sonnet-4-6'
      const agentId = 'codebuddy'

      let resolvedModelId = ''
      if (serverModelId) {
        resolvedModelId = serverModelId
      } else {
        const saved = loadModelPref(agentId)
        if (saved) {
          resolvedModelId = saved
        }
      }

      expect(resolvedModelId).toBe('claude-sonnet-4-6')
    })

    it('chat model switch does not affect preferredModel for new sessions', async () => {
      // Settings panel sets preferredModel
      agentPrefs['codebuddy'] = { preferredModel: 'glm-5.1' }

      // User switches model in a chat session — saveModelPref is now a no-op
      await saveModelPref('codebuddy', 'claude-sonnet-4-6')

      // preferredModel should remain unchanged (set by settings panel only)
      expect(loadModelPref('codebuddy')).toBe('glm-5.1')
    })

    it('new session uses preferredModel from settings panel', () => {
      agentPrefs['codebuddy'] = { preferredModel: 'glm-5.1' }

      const serverModelId = ''
      const agentId = 'codebuddy'

      let resolvedModelId = ''
      if (serverModelId) {
        resolvedModelId = serverModelId
      } else {
        const saved = loadModelPref(agentId)
        if (saved) {
          resolvedModelId = saved
        }
      }

      expect(resolvedModelId).toBe('glm-5.1')
    })
  })
})
