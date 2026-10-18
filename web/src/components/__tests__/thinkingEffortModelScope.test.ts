import { describe, expect, it, beforeEach } from 'vitest'
import {
  useSessionIdentity,
  reconcileThinkingEffort,
  updateThinkingEffortState,
  clearThinkingEffortState,
  currentAgentId,
  currentModelId,
} from '@/composables/useSessionIdentity.ts'
import { resetAgents, useAgents } from '@/composables/useAgents.ts'

/**
 * Issue #552 — the effort shown in the chat must be what the next turn will
 * actually run, not whatever the agent reported for itself.
 *
 * Three inputs compete:
 *   1. the user's pick for THIS model (remembered on the agent),
 *   2. the agent's own per-model default (OpenCode says "low" for most),
 *   3. the user's explicit pick for this session (userPicked=true).
 *
 * The rule under test: (3) wins when present; otherwise (1) beats (2). Before
 * the fix (2) was treated as (3), so a session could never leave the agent's
 * low default and the configured preference never applied.
 */

function installAgent(models: Record<string, string>, preferred?: string) {
  const agents = useAgents()
  resetAgents()
  // The agent list is populated from the server in production; here we inject
  // the exact fields the lookup reads.
  ;(agents.agents as unknown as { value: Array<Record<string, unknown>> }).value = [{
    id: 'opencode',
    name: 'OpenCode',
    backend: 'opencode',
    models: [],
    thinkingEffort: '',
    preferredThinkingEffort: preferred || '',
    thinkingEffortByModel: models,
  }]
}

beforeEach(() => {
  clearThinkingEffortState()
  currentAgentId.value = 'opencode'
  currentModelId.value = 'glm-5.3'
})

describe('reconcileThinkingEffort', () => {
  it('adopts the agent-reported level when there is no user preference anywhere', () => {
    installAgent({})
    reconcileThinkingEffort('low', [{ id: 'low', name: 'Low' }, { id: 'max', name: 'Max' }], 'opencode', 'glm-5.3', false)
    const { currentThinkingEffort } = useSessionIdentity()
    expect(currentThinkingEffort.value).toBe('low')
  })

  it('prefers the remembered pick for THIS model over the agent default', () => {
    installAgent({ 'glm-5.3': 'max' })
    reconcileThinkingEffort('low', [{ id: 'low', name: 'Low' }, { id: 'max', name: 'Max' }], 'opencode', 'glm-5.3', false)
    const { currentThinkingEffort, currentThinkingEffortName } = useSessionIdentity()
    expect(currentThinkingEffort.value).toBe('max')
    expect(currentThinkingEffortName.value).toBe('Max')
  })

  it('does not apply another model\'s pick', () => {
    installAgent({ 'deepseek-v4-pro': 'high' })
    reconcileThinkingEffort('low', [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }], 'opencode', 'glm-5.3', false)
    const { currentThinkingEffort } = useSessionIdentity()
    expect(currentThinkingEffort.value).toBe('low', 'the other model\'s pick must not leak')
  })

  it('falls back to the model-independent default when this model has no pick', () => {
    installAgent({}, 'high')
    reconcileThinkingEffort('low', [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }], 'opencode', 'glm-5.3', false)
    const { currentThinkingEffort } = useSessionIdentity()
    expect(currentThinkingEffort.value).toBe('high')
  })

  it('lets an explicit session pick win over everything', () => {
    installAgent({ 'glm-5.3': 'max' })
    reconcileThinkingEffort('medium', [{ id: 'medium', name: 'Medium' }, { id: 'max', name: 'Max' }], 'opencode', 'glm-5.3', true)
    const { currentThinkingEffort } = useSessionIdentity()
    expect(currentThinkingEffort.value).toBe('medium',
      'a user pick recorded for this session is authoritative')
  })

  it('still resolves the display name for an explicit pick with no level list', () => {
    installAgent({})
    updateThinkingEffortState('high', [])
    reconcileThinkingEffort('high', [], 'opencode', 'glm-5.3', true)
    const { currentThinkingEffortName } = useSessionIdentity()
    expect(currentThinkingEffortName.value).toBe('high')
  })
})
