import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateThinkingEffortState, updateCommandState, currentAgentId } from '@/composables/useSessionIdentity.ts'

// Singleton state — shared across the whole app
const agents = ref<any[]>([])
const defaultAgentId = ref('')
let loadPromise: Promise<void> | null = null

/** Reset all module-level singleton refs — used by SPA hot project switch. */
export function resetAgents(): void {
    agents.value = []
    defaultAgentId.value = ''
    loadPromise = null
}

async function loadAgents(force = false): Promise<void> {
    if (!force && agents.value.length > 0) return // already loaded
    if (!force && loadPromise) return loadPromise  // load in progress

    loadPromise = (async () => {
        try {
            const data = await apiGet<{ agents: any[]; defaultAgent?: string; acpStates?: Record<string, any> }>('/api/agents')
            agents.value = data.agents || []
            if (data.defaultAgent) {
                defaultAgentId.value = data.defaultAgent
            }
            // Populate ACP mode/thinking/commands state from the agents response.
            // Only populate for the CURRENT session's agent to avoid showing
            // ACP features (mode chips, slash commands) when the user switches
            // to a non-ACP backend. Other agents' ACP state will be loaded
            // when the user switches sessions (via /api/ai/chat REST response
            // or SSE events).
            if (data.acpStates) {
                const activeAgentId = currentAgentId.value
                const activeState = activeAgentId ? data.acpStates[activeAgentId] : null
                if (activeState) {
                    if (activeState.modeState?.availableModes?.length > 0) {
                        updateModeState(activeState.modeState.currentModeId || '', activeState.modeState.availableModes)
                    }
                    if (activeState.thinkingEffortState?.availableLevels?.length > 0) {
                        updateThinkingEffortState(activeState.thinkingEffortState.currentId || '', activeState.thinkingEffortState.availableLevels)
                    }
                    if (Array.isArray(activeState.commands) && activeState.commands.length > 0) {
                        updateCommandState(activeState.commands)
                    }
                }
            }
        } catch (err) {
            console.error('Failed to load agents:', err)
        } finally {
            loadPromise = null
        }
    })()
    return loadPromise
}

function getAgentIcon(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent ? agent.icon : '🤖'
}

function getAgentName(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent ? agent.name : (agentId || gt('agents.defaultAssistant'))
}

function isDefaultAgent(agentId: string): boolean {
    return agentId === defaultAgentId.value
}

/** Get the default model ID for an agent. Priority: preferredModel > first model with default:true > first in list. */
function getDefaultModelId(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    if (agent?.preferredModel) return agent.preferredModel
    if (!agent?.models?.length) return ''
    const defaultModel = agent.models.find(m => m.default)
    return defaultModel ? defaultModel.id : agent.models[0].id
}

/** Get the models list for an agent. */
function getAgentModels(agentId: string): { id: string; name: string; default: boolean }[] {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.models || []
}

/** Check if an agent has multiple models (show model switcher chip). */
function isMultiModel(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return (agent?.models?.length || 0) > 1
}

/** Get the raw agent object by id. Returns undefined if not found. */
function getAgent(agentId: string) {
    return agents.value.find(a => a.id === agentId)
}

/**
 * Get the display name of an agent's default model.
 * Returns the model name, or the modelId itself if the model is not found.
 */
function getAgentDefaultModelName(agentId: string): string {
    const modelId = getDefaultModelId(agentId)
    const models = getAgentModels(agentId)
    const model = models.find(m => m.id === modelId)
    return model?.name || modelId
}

/** Build the "icon name" header string for an agent. */
function agentHeaderTitle(agentId: string): string {
    const agent = getAgent(agentId)
    if (agent) return `${agent.icon} ${agent.name}`
    return agentId ? getAgentName(agentId) : gt('chat.session.aiDialog')
}

/**
 * Sync modelId and modelName from an agent's default model.
 * Returns { modelId, modelName } so callers can assign to their refs.
 */
function syncModelFromAgent(agentId: string): { modelId: string; modelName: string } {
    const modelId = getDefaultModelId(agentId)
    const models = getAgentModels(agentId)
    const model = models.find(m => m.id === modelId)
    return { modelId, modelName: model?.name || modelId }
}

/** Get a specific model by id for an agent. Returns undefined if not found. */
function getAgentModel(agentId: string, modelId: string) {
    const models = getAgentModels(agentId)
    return models.find(m => m.id === modelId)
}

/** Get the thinking effort levels for an agent. Returns [] for unsupported backends. */
function getAgentThinkingEffortLevels(agentId: string): string[] {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.thinkingEffortLevels || []
}

/** Check if an agent supports thinking effort selection (has levels defined). */
function hasThinkingEffortLevels(agentId: string): boolean {
    return getAgentThinkingEffortLevels(agentId).length > 0
}

/** Get the effective thinking effort for interactive sessions (preferred > agent default). */
function getEffectiveThinkingEffort(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.preferredThinkingEffort || agent?.thinkingEffort || ''
}

/** Update a single field on an agent in the reactive store (for immediate UI feedback after PATCH). */
function updateAgentField(agentId: string, field: string, value: any): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (agent) {
        (agent as any)[field] = value
    }
}

/** Check if an agent supports model refresh (has canRefreshModels from backend). */
function canRefreshModels(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return !!agent?.canRefreshModels
}

export function useAgents() {
    return {
        agents,
        defaultAgentId,
        loadAgents,
        getAgentIcon,
        getAgentName,
        isDefaultAgent,
        getDefaultModelId,
        getAgentModels,
        isMultiModel,
        getAgent,
        getAgentModel,
        getAgentDefaultModelName,
        agentHeaderTitle,
        syncModelFromAgent,
        getAgentThinkingEffortLevels,
        hasThinkingEffortLevels,
        getEffectiveThinkingEffort,
        updateAgentField,
        canRefreshModels,
    }
}
