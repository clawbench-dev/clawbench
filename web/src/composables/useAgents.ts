import { ref } from 'vue'
import { apiGet, apiPatch, apiPost, apiDelete } from '@/utils/api'
import { gt } from '@/composables/useLocale'
import { updatePlanEntries } from '@/composables/usePlanProgress'
import { appLog } from '@/utils/appLog'

const TAG = 'Agents'

// ───────────────────────────────────────────────────────────
// Break circular dependency with useSessionIdentity:
// useSessionIdentity imports useAgents, so useAgents cannot
// import useSessionIdentity directly. Instead, useSessionIdentity
// registers its updater functions here at init time.
// ───────────────────────────────────────────────────────────
let _updateAvailableModes: ((modes: Array<{ id: string; name: string }>) => void) | null = null
let _updateAvailableThinkingEfforts: ((levels: Array<{ id: string; name: string }>) => void) | null = null
let _updateCommandState: ((commands: Array<{ name: string; description: string; inputHint?: string }>) => void) | null = null
let _currentAgentId: { value: string } | null = null

export function registerIdentityUpdaters(opts: {
  updateAvailableModes: (modes: Array<{ id: string; name: string }>) => void
  updateAvailableThinkingEfforts: (levels: Array<{ id: string; name: string }>) => void
  updateCommandState: (commands: Array<{ name: string; description: string; inputHint?: string }>) => void
  currentAgentId: { value: string }
}) {
  _updateAvailableModes = opts.updateAvailableModes
  _updateAvailableThinkingEfforts = opts.updateAvailableThinkingEfforts
  _updateCommandState = opts.updateCommandState
  _currentAgentId = opts.currentAgentId
}

// Singleton state — shared across the whole app
const agents = ref<any[]>([])
const defaultAgentId = ref('')
let loadPromise: Promise<void> | null = null

// Cached ACP states from /api/agents — used by createSession to restore
// mode/thinking/command chips after clearing state for a new session.
// Without this cache, chips only reappear after the first message triggers
// an SSE mode_update event, even though the data is already in memory.
let acpStatesCache: Record<string, any> = {}

// originalModels stores CLI-discovered model lists for each agent.
// When ACP provides a model list, we override agent.models but keep
// the original here so we can restore it when switching away from ACP.
const originalModels = new Map<string, any[]>()

/** Reset all module-level singleton refs — used by SPA hot project switch. */
export function resetAgents(): void {
    agents.value = []
    defaultAgentId.value = ''
    acpStatesCache = {}
    loadPromise = null
    _updateAvailableModes = null
    _updateAvailableThinkingEfforts = null
    _updateCommandState = null
    _currentAgentId = null
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
            // Cache acpStates for later use (e.g. createSession needs to
            // restore mode chips without re-fetching /api/agents).
            if (data.acpStates) {
                acpStatesCache = data.acpStates
            }
            // Populate ACP mode/thinking/commands state from the agents response.
            // Only populate for the CURRENT session's agent to avoid showing
            // ACP features (mode chips, slash commands) when the user switches
            // to a non-ACP backend. Other agents' ACP state will be loaded
            // when the user switches sessions (via /api/ai/chat REST response
            // or SSE events).
            if (data.acpStates) {
                const activeAgentId = _currentAgentId?.value || ''
                const activeState = activeAgentId ? data.acpStates[activeAgentId] : null
                if (activeState) {
                    // Only update available modes/levels — currentModeId and
                    // currentThinkingEffort are managed by user action + DB,
                    // not by agent cache (which reflects the agent's runtime
                    // state, not the user's selection).
                    if (activeState.modeState?.availableModes?.length > 0) {
                        _updateAvailableModes?.(activeState.modeState.availableModes)
                    }
                    if (activeState.thinkingEffortState?.availableLevels?.length > 0) {
                        _updateAvailableThinkingEfforts?.(activeState.thinkingEffortState.availableLevels)
                    } else {
                        // Fallback: agent config (e.g. OpenCode/Kimi ACP don't expose thought_level)
                        const agentLevels = getAgentThinkingEffortLevels(activeAgentId)
                        if (agentLevels.length > 0) {
                            _updateAvailableThinkingEfforts?.(agentLevels.map((id: string) => ({ id, name: id })))
                        }
                    }
                    if (Array.isArray(activeState.commands) && activeState.commands.length > 0) {
                        _updateCommandState?.(activeState.commands)
                    }
                    // When ACP provides a model list, override agent.models
                    // so the frontend SessionSettingModal shows ACP models.
                    if (activeState.modelListState?.models?.length > 0) {
                        updateACPModelList(activeAgentId, activeState.modelListState.models, activeState.modelListState.currentModelId)
                    }
                    if (activeState.planState?.entries?.length > 0) {
                        updatePlanEntries(activeState.planState.entries)
                    }
                }
            }
        } catch (err) {
            appLog.e(TAG, 'Failed to load agents:', err)
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
    const defaultModel = agent.models.find((m: any) => m.default)
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
export function getAgentThinkingEffortLevels(agentId: string): string[] {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.thinkingEffortLevels || []
}

/** Check if an agent supports @resume (LoadSession + ListSessions capabilities). */
export function agentCanResume(agentId: string): boolean {
    const state = acpStatesCache[agentId]
    // If cache has explicit loadSession, use it
    if (state?.loadSession) return true
    // If agent supports ACP (has acpCommand) but pool hasn't been initialized yet,
    // assume it may support resume — the AcpSessionDrawer will handle 501 gracefully
    // if the agent doesn't actually support it.
    const agent = agents.value.find(a => a.id === agentId)
    return !!agent?.acpCommand
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

/** Set the global default agent via PATCH /api/config. Takes effect immediately (hot-reload). */
async function setDefaultAgent(agentId: string): Promise<void> {
    await apiPatch('/api/config', { default_agent: agentId })
    defaultAgentId.value = agentId
}

/** Update agent's model list from ACP. Saves original CLI models first so they can be restored. */
export function updateACPModelList(agentId: string, models: Array<{ id: string; name: string }>, currentModelId?: string): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return
    // Save original CLI models if not already saved
    if (!originalModels.has(agentId)) {
        originalModels.set(agentId, [...agent.models])
    }
    const mapped = models.map((m, i) => ({
        id: m.id,
        name: m.name,
        default: currentModelId ? m.id === currentModelId : i === 0,
    }))
    agent.models = mapped
}

/** Restore agent's model list to the original CLI-discovered models (clears ACP override). */
export function restoreOriginalModels(agentId: string): void {
    const saved = originalModels.get(agentId)
    if (!saved) return
    const agent = agents.value.find(a => a.id === agentId)
    if (agent) {
        agent.models = [...saved]
    }
    originalModels.delete(agentId)
}

/** Check if an agent supports model refresh (has canRefreshModels from backend). */
function canRefreshModels(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return !!agent?.canRefreshModels
}

/** Check if an agent supports both ACP and CLI transport modes (has acpCommand set). */
function supportsDualTransport(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return !!agent?.acpCommand
}

/** Get the current transport mode for an agent. Returns 'acp-stdio' or 'cli'. */
function getAgentTransport(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.transport || 'cli'
}

/** Invalidate the ACP state cache for an agent so next access force-refreshes. */
export function invalidateACPStateCache(agentId: string): void {
    delete acpStatesCache[agentId]
}

/**
 * Handle acp_state_update WS event from backend prefetch.
 * Updates the cache and, if the event is for the current agent,
 * immediately refreshes the UI (mode/thinking/command chips).
 */
/**
 * Populate ACP state (mode, thinking, commands, model list) for the given
 * agent from the cached acpStates. Used by createSession after clearing
 * session-level state so that mode chips appear immediately on new sessions.
 */
export async function populateACPStateFromCache(agentId: string): Promise<void> {
    // If the cache doesn't have this agent's ACP state yet (e.g. pool was
    // empty on first loadAgents, but an ACP session has since been created),
    // force-refresh from the server before giving up.
    if (!acpStatesCache[agentId]) {
        await loadAgents(true)
    }
    const state = acpStatesCache[agentId]
    if (!state) return
    // Only update available modes/levels — currentModeId and currentThinkingEffort
    // are managed by user action + DB, not by agent cache (which reflects the
    // agent's runtime state, not the user's selection).
    if (state.modeState?.availableModes?.length > 0) {
        _updateAvailableModes?.(state.modeState.availableModes)
    }
    if (state.thinkingEffortState?.availableLevels?.length > 0) {
        _updateAvailableThinkingEfforts?.(state.thinkingEffortState.availableLevels)
    } else {
        // Fallback: agent config (e.g. OpenCode/Kimi ACP don't expose thought_level)
        const agentLevels = getAgentThinkingEffortLevels(agentId)
        if (agentLevels.length > 0) {
            _updateAvailableThinkingEfforts?.(agentLevels.map((id: string) => ({ id, name: id })))
        }
    }
    if (Array.isArray(state.commands) && state.commands.length > 0) {
        _updateCommandState?.(state.commands)
    }
    if (state.modelListState?.models?.length > 0) {
        updateACPModelList(agentId, state.modelListState.models, state.modelListState.currentModelId)
    }
    if (state.planState?.entries?.length > 0) {
        updatePlanEntries(state.planState.entries)
    }
}

/** Duplicate an agent by cloning its configuration with a new name. */
async function duplicateAgent(sourceId: string, newName: string): Promise<void> {
    await apiPost('/api/agents', { source_id: sourceId, name: newName })
    await loadAgents(true)
}

/** Delete an agent by ID. */
async function deleteAgent(agentId: string): Promise<void> {
    await apiDelete('/api/agents', { body: { id: agentId } })
    await loadAgents(true)
}

/** Rescan all agents (re-run CLI discovery pipeline). */
async function rescanAgents(): Promise<void> {
    await apiPost('/api/agents/rescan', {})
    await loadAgents(true)
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
        setDefaultAgent,
        canRefreshModels,
        agentCanResume,
        supportsDualTransport,
        getAgentTransport,
        invalidateACPStateCache,
        updateACPModelList,
        restoreOriginalModels,
        populateACPStateFromCache,
        duplicateAgent,
        deleteAgent,
        rescanAgents,
    }
}
