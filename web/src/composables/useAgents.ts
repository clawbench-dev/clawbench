import { ref, computed } from 'vue'
import { apiGet, apiPatch, apiPost, apiDelete, apiPut } from '@/utils/api'
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
interface AgentRecord {
  id: string
  name: string
  backend: string
  command?: string
  specialty?: string
  models?: Array<{ id: string; name: string; default: boolean }>
  /** Pure CLI-discovered list, supplied by the backend for the CLI view. */
  cliModels?: Array<{ id: string; name: string; default: boolean }>
  preferredModel?: string
  thinkingEffort?: string
  thinkingEffortLevels?: string[]
  preferredThinkingEffort?: string
  /**
   * Per-model effort picks (key = model id). The user's own choice for that
   * model; it wins over `preferredThinkingEffort` when the session runs that
   * model. Absent/empty means no per-model pick (issue #552).
   */
  thinkingEffortByModel?: Record<string, string>
  preferredMode?: string
  acpCommand?: string
  acpAvailableModes?: Array<{ id: string; name: string }>
  transport?: string
  customSystemPrompt?: string
  /** Raw SVG string for a user-configured avatar; empty = built-in brand icon. */
  avatar?: string
  canRefreshModels?: boolean
  /**
   * Whether this agent's backend has a registry spec, i.e. whether auto-discovery
   * owns its `acp_command` (rewritten on every rescan/startup). The settings UI
   * uses it to decide whether the command field is editable; the server applies
   * the same predicate when it refuses the PATCH.
   */
  backendHasSpec?: boolean
  supportsCLI?: boolean
  supportsMidTurn?: boolean
  autoApprove?: boolean
  /** Hidden from every new-entry picker (new session / task / group member). */
  disabled?: boolean
  /** Manual display order (lower first); the server returns the list already sorted by it. */
  sortOrder?: number
  /**
   * When set, the agent is scoped to that project id and is visible only there.
   * Absent/null means global (visible in every project). The backend already
   * filters /api/agents by the current project cookie, so the list the client
   * receives contains only agents visible here.
   */
  projectId?: number
  /** Derived usage counts from GET /api/agents (not persisted). */
  sessionCount?: number
  taskCount?: number
  membershipCount?: number
}

const agents = ref<AgentRecord[]>([])
const defaultAgentId = ref('')
let loadPromise: Promise<void> | null = null

// Agents that may be chosen at a NEW-entry point (new session / new task /
// group member add). Disabled agents are filtered out here so every picker
// (AgentSelectorDrawer) hides them from one place. Existing sessions/tasks
// keep their disabled agent — this only gates creation.
const selectableAgents = computed(() => agents.value.filter(a => !a.disabled))

// Whether the agent list has been fetched successfully. The list starts empty,
// so "id not in list" is ambiguous between "not loaded yet" and "deleted" —
// only the latter may be reported as missing (same rule as useTaskTab's
// tasksLoaded). A view that gates on an agent looked up from this list keys its
// "agent no longer exists" fallback on this flag.
const agentsLoaded = ref(false)

// Cached ACP states from /api/agents — used by createSession to restore
// mode/thinking/command chips after clearing state for a new session.
// Without this cache, chips only reappear after the first message triggers
// an SSE mode_update event, even though the data is already in memory.
let acpStatesCache: Record<string, Record<string, unknown>> = {}

// Type for nested ACP state access (used by loadAgents and populateACPStateFromCache)
interface AcpState {
  modeState?: { availableModes?: Array<{ id: string; name: string }>; currentModeId?: string }
  thinkingEffortState?: { availableLevels?: Array<{ id: string; name: string }>; currentId?: string }
  commands?: Array<{ name: string; description: string; inputHint?: string }>
  modelListState?: {
    models?: Array<{ id: string; name: string }>
    resolvedModels?: Array<{ id: string; name: string; default: boolean }>
    cliModels?: Array<{ id: string; name: string; default: boolean }>
    currentModelId?: string
  }
  planState?: { entries?: Array<Record<string, unknown>> }
  loadSession?: boolean
}

/** Reset all module-level singleton refs — used by SPA hot project switch. */
export function resetAgents(): void {
    agents.value = []
    defaultAgentId.value = ''
    acpStatesCache = {}
    loadPromise = null
    agentsLoaded.value = false
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
            const data = await apiGet<{ agents: AgentRecord[]; defaultAgent?: string; acpStates?: Record<string, Record<string, unknown>> }>('/api/agents')
            // The backend resolves the model list (CLI order/names with ACP
            // membership applied) and also reports the untouched CLI list as
            // cliModels. The client renders what it is given; it keeps no
            // baseline of its own.
            agents.value = data.agents || []
            // The list is now authoritative: an id absent from it is genuinely
            // gone, not merely unloaded. Views that gate on a looked-up agent
            // use this to tell "deleted" apart from "not fetched yet".
            agentsLoaded.value = true
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
                const activeState = activeAgentId ? data.acpStates[activeAgentId] as AcpState : null
                if (activeState) {
                    // Only update available modes/levels — currentModeId and
                    // currentThinkingEffort are managed by user action + DB,
                    // not by agent cache (which reflects the agent's runtime
                    // state, not the user's selection).
                    if (activeState.modeState?.availableModes && activeState.modeState.availableModes.length > 0) {
                        _updateAvailableModes?.(activeState.modeState.availableModes)
                    }
                    if (activeState.thinkingEffortState?.availableLevels && activeState.thinkingEffortState.availableLevels.length > 0) {
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
                    // so the frontend SessionDrawer shows ACP models.
                    // The backend resolves the CLI and ACP lists before sending
                    // them, so apply the resolved list when present.
                    const ml = activeState.modelListState
                    if (ml?.resolvedModels && ml.resolvedModels.length > 0) {
                        applyResolvedModelList(activeAgentId, ml.resolvedModels, ml.cliModels)
                    } else if (ml?.models && ml.models.length > 0) {
                        updateACPModelList(activeAgentId, ml.models, ml.currentModelId)
                    }
                    if (activeState.planState?.entries && activeState.planState.entries.length > 0) {
                        updatePlanEntries(activeState.planState.entries as unknown as import('@/composables/usePlanProgress').PlanEntry[])
                    }
                    // NOTE: Usage state is NOT restored from agent-level cache.
                    // Usage is per-session data — the agent-level cache reflects
                    // the last session that emitted a UsageUpdate, which may be
                    // a different session. Restoring it would flash stale progress
                    // on session switch. Usage comes from per-session sources only
                    // (SSE usage_update, REST chat response, createSession reset).
                }
            }
        } catch (err: unknown) {
            appLog.e(TAG, 'Failed to load agents:', err)
        } finally {
            loadPromise = null
        }
    })()
    return loadPromise
}

// 模型发现在后台异步跑，完成时服务端广播 agents_updated。此时前端可能已经
// 用空模型列表渲染过选择器，需要强制重拉一次。与 useMessageClusters 一样
// 在模块级注册，全应用生命周期只注册一次。
//
// 方向是 useAgents → useGlobalEvents：反过来会让 useGlobalEvents 依赖
// useAgents，而它已被大量模块引用，风险更高。
//
// 这条边会闭合一个既有的静态环：
//   useAgents → useGlobalEvents → useNotification → useSettingsConfig → useAgents
// 若用静态 import + 模块体里同步调用 useGlobalEvents()，则当 useGlobalEvents
// 先于 useAgents 求值（任何直接 import useGlobalEvents 的模块作为入口时都会
// 如此，例如 useMessageClusters）就会命中 TDZ：
//   ReferenceError: Cannot access 'connected' before initialization
// （queueMicrotask 也不行——模块加载在模块之间会让出微任务队列，回调仍可能
// 在 useGlobalEvents 求值到一半时触发。）
// 因此改用动态 import：它的 promise 按规范在目标模块求值完成后才 resolve，
// 天然规避该环。注册时机仍远早于任何 WS 事件（WS 在 app 挂载后才连接）。
// catch 同时兜住两种失败：动态 chunk 加载失败，以及测试里常见的
// useGlobalEvents 部分 mock（只提供 wsStatus 没有 onEvent）。
void import('@/composables/useGlobalEvents')
    .then(({ useGlobalEvents }) => {
        useGlobalEvents().onEvent((event: string) => {
            if (event !== 'agents_updated') return
            void loadAgents(true)
        })
    })
    .catch((err: unknown) => {
        appLog.e(TAG, 'Failed to register agents_updated listener:', err)
    })

// Fallback for the broadcast above: the dynamic import (or a partial
// useGlobalEvents mock) can fail, and the reload must not depend on a single
// subscription. The same signal that re-syncs other stale state — a WS
// reconnect — also re-pulls the agent list, so a missed `agents_updated` costs
// one reconnect at worst instead of a manual rescan. A plain `focus` is
// deliberately NOT used: it fires constantly and `loadAgents` has no staleness
// check of its own, so it would re-fetch on every window focus.
//
// Registered at module scope (the app-lifetime singleton pattern used by
// usePwaInstall): no teardown, and it also runs in the share.html bundle, where
// nothing dispatches the event — harmless, just not app-only.
window.addEventListener('clawbench-reconnect', () => {
    void loadAgents(true)
})

function getAgentBackend(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.backend || ''
}

/** Get an agent's custom avatar SVG string. Returns '' when unset (use built-in icon). */
export function getAgentAvatar(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.avatar || ''
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
    const defaultModel = agent.models.find((m: { default: boolean }) => m.default)
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

/**
 * Build the header title string for an agent.
 *
 * Returns '' for an unknown/absent agent rather than a placeholder ("AI 对话"):
 * the chat title bar now shows a skeleton while a session switch is in flight
 * and otherwise renders nothing, so a fallback string would only flash during
 * the switch. Callers gate on the empty result (App.vue renders the agent line
 * only when `currentAgentId` is set).
 */
function agentHeaderTitle(agentId: string): string {
    const agent = getAgent(agentId)
    if (agent) return agent.name
    return agentId ? getAgentName(agentId) : ''
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

/** Get thinking effort levels including ACP-reported levels for dual-transport agents. */
export function getAgentThinkingEffortLevelsIncludingACP(agentId: string): string[] {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return []
    // For ACP-transport agents, prefer ACP-reported levels
    if (agent.transport === 'acp-stdio') {
        const state = acpStatesCache[agentId] as AcpState | undefined
        const acpLevels = state?.thinkingEffortState?.availableLevels?.map(l => l.id)
        if (acpLevels && acpLevels.length > 0) return acpLevels
    }
    return agent.thinkingEffortLevels || []
}

/** Check if an agent supports session resume (LoadSession capability). */
export function agentCanResume(agentId: string): boolean {
    const state = acpStatesCache[agentId] as AcpState | undefined
    return !!state?.loadSession
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

/**
 * Get the effort to pre-select for one agent+model pair (issue #552).
 *
 * Priority: the user's pick for THIS model > the model-independent default
 * (preferredThinkingEffort > thinkingEffort). Mirrors the server's
 * model.Agent.ThinkingEffortForModel so the chip and the first turn agree.
 *
 * An empty modelId (no model chosen yet) skips the per-model lookup: there is
 * no key to match, and guessing one would pre-select a level for a model the
 * user has not picked.
 */
function getEffectiveThinkingEffortForModel(agentId: string, modelId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return ''
    if (modelId) {
        const perModel = agent.thinkingEffortByModel?.[modelId]
        if (perModel) return perModel
    }
    return agent.preferredThinkingEffort || agent.thinkingEffort || ''
}

/** Get the effective mode ID for an agent. Priority: preferredMode > empty. */
function getEffectiveModeId(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.preferredMode || ''
}

/** Check if an agent has a preferred mode configured. */
function hasPreferredMode(agentId: string): boolean {
    return getEffectiveModeId(agentId) !== ''
}

/**
 * Record a per-model effort pick in the local agent cache (issue #552).
 *
 * The server persists the pick when the user clicks a level, but the agent list
 * is cached (loadAgents() early-returns once loaded) and nothing pushes this
 * field over the WS. Without this optimistic write, a new chat opened in the
 * same SPA session reads a stale list, falls back to the agent's own default,
 * and then SENDS that stale level as an explicit override on the next message —
 * which beats the pick the server remembered. The bug would survive for the
 * exact flow the issue reports.
 */
function setThinkingEffortForModelLocal(agentId: string, modelId: string, level: string): void {
    if (!agentId || !modelId) return
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return
    // Replace the map instead of mutating it: the value is read by computed
    // consumers, and a nested mutation is invisible to them unless the parent
    // object identity changes.
    const next = { ...(agent.thinkingEffortByModel || {}) }
    if (level) next[modelId] = level
    else delete next[modelId]
    agent.thinkingEffortByModel = next
}

/** Update a single field on an agent in the reactive store (for immediate UI feedback after PATCH). */
function updateAgentField(agentId: string, field: string, value: unknown): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (agent) {
        (agent as unknown as Record<string, unknown>)[field] = value
    }
}

/** Set the global default agent via PATCH /api/config. Takes effect immediately (hot-reload). */
async function setDefaultAgent(agentId: string): Promise<void> {
    await apiPatch('/api/config', { default_agent: agentId })
    defaultAgentId.value = agentId
}

/**
 * Apply an ACP model list update pushed over the WebSocket.
 *
 * The backend sends the already-resolved list (`resolvedModels`) plus the pure
 * CLI list (`cliModels`), so this is a plain assignment. Previously the client
 * held a CLI baseline and merged the two lists itself, which needed a tier-alias
 * pass for claude-style agents and a manual save/restore around transport
 * switches.
 *
 * Falls back to the raw ACP list when the backend could not resolve it (no CLI
 * list available at emit time) — better to show the ACP list alone than nothing.
 */
export function updateACPModelList(agentId: string, models: Array<{ id: string; name: string }>, currentModelId?: string): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return
    if (models.length === 0) return

    agent.models = models.map(m => ({ id: m.id, name: m.name, default: m.id === currentModelId }))
    // An empty currentModelId means no session context; the backend's resolved
    // list already carries the default flag, so only override when we know it.
    if (currentModelId) {
        for (const m of agent.models) m.default = m.id === currentModelId
    } else if (!agent.models.some(m => m.default)) {
        agent.models[0].default = true
    }
}

/**
 * Apply a backend-resolved model list together with its CLI counterpart.
 *
 * This is the preferred path: the backend already applied ACP membership over the
 * CLI skeleton, so the client only assigns. `cliModels` is stored alongside so a
 * later transport switch to CLI is a plain field read.
 */
export function applyResolvedModelList(
    agentId: string,
    resolved: Array<{ id: string; name: string; default: boolean }>,
    cliModels?: Array<{ id: string; name: string; default: boolean }>,
): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return
    agent.models = resolved.map(m => ({ ...m }))
    if (cliModels && cliModels.length > 0) {
        agent.cliModels = cliModels.map(m => ({ ...m }))
    }
}

/**
 * Switch the agent's displayed model list to the CLI view.
 *
 * `cliModels` is supplied by the backend on every /api/agents response, so no
 * client-side baseline is needed.
 */
export function restoreOriginalModels(agentId: string): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent?.cliModels) return
    agent.models = agent.cliModels.map(m => ({ ...m }))
}

/**
 * Restore the agent's model list to the ACP view.
 *
 * The counterpart to restoreOriginalModels. Switching ACP → CLI replaces
 * `agent.models` with the CLI list, so switching back must NOT leave it there:
 * the CLI list is a guess until the ACP agent has reported its own models
 * (ACP has no client-side default-model concept). So this either applies the
 * ACP-reported list or clears the list entirely — never falls through to the
 * CLI baseline.
 *
 * Deliberately separate from populateACPStateFromCache, which the settings page
 * also calls and which must leave a dual-transport agent's CLI list intact.
 *
 * Returns true when an ACP-reported list was applied, false when the list was
 * cleared because the agent has not reported models yet.
 */
export function restoreACPModelView(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return false
    const state = acpStatesCache[agentId] as AcpState | undefined
    const ml = state?.modelListState
    if (ml?.resolvedModels && ml.resolvedModels.length > 0) {
        applyResolvedModelList(agentId, ml.resolvedModels, ml.cliModels)
        return true
    }
    if (ml?.models && ml.models.length > 0) {
        updateACPModelList(agentId, ml.models, ml.currentModelId)
        return true
    }
    // No ACP-reported models: an empty list is the honest answer, and it keeps
    // the send path from shipping a CLI model as the session's selection.
    agent.models = []
    return false
}

/**
 * Reset the agent's displayed model list to whichever view its CURRENT
 * transport calls for.
 *
 * Both baselines live on the same `agent.models` field, so a caller that resets
 * "to the CLI list" unconditionally is wrong for an agent running over ACP (and
 * vice versa). This picks the right one: ACP → the ACP view (see
 * restoreACPModelView, which clears the list when the agent has not reported
 * models yet); CLI → the CLI baseline.
 *
 * The server response is authoritative and self-heals on the next loadAgents,
 * so this only covers the window between fetches (a transport toggle, a session
 * switch).
 */
export function resetModelViewForTransport(agentId: string): void {
    if (getAgentTransport(agentId) === 'acp-stdio') {
        restoreACPModelView(agentId)
        return
    }
    restoreOriginalModels(agentId)
}

/**
 * Apply the result of a manual "refresh models".
 *
 * The backend persists the newly discovered CLI list and answers with the same
 * pair GET /api/agents reports: the resolved list (`models`) plus the pure CLI
 * list (`cliModels`). The CLI baseline is always replaced — a later transport
 * switch reads it — while the displayed list depends on the active transport,
 * because in ACP mode the ACP-resolved list is authoritative for membership.
 *
 * `cliModels` is optional for backwards compatibility: an older backend answers
 * with only `models`, which is then used for both lists (the previous behavior).
 */
export function applyRefreshedModelList(
    agentId: string,
    resolved: Array<{ id: string; name: string; default?: boolean }>,
    cliModels?: Array<{ id: string; name: string; default?: boolean }>,
): void {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return
    const cli = (cliModels && cliModels.length > 0 ? cliModels : resolved).map(m => ({
        id: m.id,
        name: m.name,
        default: !!m.default,
    }))
    agent.cliModels = cli
    if (getAgentTransport(agentId) === 'acp-stdio') {
        agent.models = resolved.map(m => ({ id: m.id, name: m.name, default: !!m.default }))
    } else {
        agent.models = cli.map(m => ({ ...m }))
    }
}

/** Check if an agent supports model refresh (has canRefreshModels from backend). */
function canRefreshModels(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return !!agent?.canRefreshModels
}

/**
 * Check if an agent supports ACP transport (has acpCommand set).
 * ACP-only backends (e.g. grok) support ACP but not CLI.
 */
function supportsACP(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return !!agent?.acpCommand
}

/**
 * Check if an agent supports CLI transport (has a CLI backend implementation).
 * Backends without a CLI factory (e.g. grok, ACP-only) return false.
 * Falls back to true for legacy agents where supportsCLI is not reported.
 */
function supportsCLI(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    if (!agent) return false
    return agent.supportsCLI !== false
}

/**
 * Check if an agent's backend can inject a message into a turn that is ALREADY
 * RUNNING (rather than queueing it for the next one).
 *
 * Drives the queued bubble's action label: true → "insert into the current
 * reply"; false → "interrupt and send" (stop this turn, run the message next).
 * Falls back to false for legacy agents that do not report the flag, so the
 * safer "interrupt" affordance is shown when in doubt.
 */
function supportsMidTurn(agentId: string): boolean {
    const agent = agents.value.find(a => a.id === agentId)
    return agent?.supportsMidTurn === true
}

/**
 * Check if an agent supports BOTH ACP and CLI transport modes
 * (has acpCommand AND a CLI backend implementation).
 */
function supportsDualTransport(agentId: string): boolean {
    return supportsACP(agentId) && supportsCLI(agentId)
}

/** Get the current transport mode for an agent. Returns 'acp-stdio' or 'cli'. */
function getAgentTransport(agentId: string): string {
    const agent = agents.value.find(a => a.id === agentId)
    if (agent?.transport) return agent.transport
    return agent?.acpCommand ? 'acp-stdio' : 'cli'
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
    const state = acpStatesCache[agentId] as AcpState | undefined
    if (!state) return
    // Only update available modes/levels — currentModeId and currentThinkingEffort
    // are managed by user action + DB, not by agent cache (which reflects the
    // agent's runtime state, not the user's selection).
    if (state.modeState?.availableModes && state.modeState.availableModes.length > 0) {
        _updateAvailableModes?.(state.modeState.availableModes)
    }
    if (state.thinkingEffortState?.availableLevels && state.thinkingEffortState.availableLevels.length > 0) {
        _updateAvailableThinkingEfforts?.(state.thinkingEffortState.availableLevels)
    } else {
        // Fallback: agent config (e.g. OpenCode/Kimi ACP don't expose thought_level)
        const agentLevels = getAgentThinkingEffortLevels(agentId)
        if (agentLevels.length > 0) {
            _updateAvailableThinkingEfforts?.(agentLevels.map((id: string) => ({ id, name: id })))
        }
    }
    if (Array.isArray(state.commands) && state.commands.length > 0) {
        _updateCommandState?.(state.commands as { name: string; description: string; inputHint?: string }[])
    }
    const ml = state.modelListState
    if (ml?.resolvedModels && ml.resolvedModels.length > 0) {
        applyResolvedModelList(agentId, ml.resolvedModels, ml.cliModels)
    } else if (ml?.models && ml.models.length > 0) {
        updateACPModelList(agentId, ml.models, ml.currentModelId)
    }
    if (state.planState?.entries && state.planState.entries.length > 0) {
        updatePlanEntries(state.planState.entries as unknown as import('@/composables/usePlanProgress').PlanEntry[])
    }
    // NOTE: Usage state is NOT restored from agent-level cache here.
    // Usage is per-session data — the agent-level cache reflects the last
    // session that emitted a UsageUpdate, which may be a different session.
    // Restoring it here would flash stale progress on session switch or
    // when sending a message in a new session. Usage state should only come
    // from per-session sources: SSE usage_update events, REST chat response,
    // or the explicit reset in createSession().
}

/**
 * Duplicate an agent by cloning its configuration with a new name.
 *
 * Returns the new agent's id (from the created agent the backend answers with)
 * so the caller can navigate to it — the settings panel switches to the copy's
 * config page right after duplicating.
 */
async function duplicateAgent(sourceId: string, newName: string): Promise<string> {
    const created = await apiPost<{ id?: string }>('/api/agents', { source_id: sourceId, name: newName })
    await loadAgents(true)
    return created?.id ?? ''
}

/**
 * Create a user-defined ACP agent (issue #557) from a spawn command.
 *
 * Returns the created agent's id so the settings panel can navigate to its
 * detail page, mirroring duplicateAgent. `backend` is not a parameter: the
 * server always stamps its own custom-ACP backend ID, because borrowing a
 * built-in one would let the discovery sync overwrite the command.
 */
async function createAcpAgent(name: string, acpCommand: string): Promise<string> {
    const created = await apiPost<{ id?: string }>('/api/agents', { name, acp_command: acpCommand })
    await loadAgents(true)
    return created?.id ?? ''
}

/**
 * Delete an agent by ID.
 *
 * The backend refuses (409, msgKey=AgentInUse) while the agent still has
 * sessions, tasks, or active group memberships. The error is re-thrown with
 * its msgKey/detail intact so the caller can show the authoritative counts.
 */
async function deleteAgent(agentId: string): Promise<void> {
    await apiDelete('/api/agents', { body: { id: agentId } })
    await loadAgents(true)
}

/** Rescan all agents (re-run CLI discovery pipeline). */
async function rescanAgents(): Promise<void> {
    await apiPost('/api/agents/rescan', {}, { timeoutMs: 60_000 })
    await loadAgents(true)
}

/**
 * Persist a manual agent order (settings → agents drag).
 *
 * The backend writes each agent's sort_order to its index and reorders the
 * in-memory list, so the order is global. We re-fetch rather than mirroring
 * locally: the shared `agents` array is consumed by every picker, and letting
 * the server be the single source keeps them all in step (same rationale as
 * patchAgentField's project_scope branch).
 */
async function reorderAgents(ids: string[]): Promise<void> {
    await apiPut('/api/agents/reorder', { ids })
    await loadAgents(true)
}

export function useAgents() {
    return {
        agents,
        // Agents eligible for a NEW entry point (disabled filtered out).
        selectableAgents,
        defaultAgentId,
        // True once the list has loaded successfully — callers use it to tell
        // "agent deleted" apart from "not fetched yet".
        agentsLoaded,
        loadAgents,
        getAgentBackend,
        getAgentName,
        getAgentAvatar,
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
        getAgentThinkingEffortLevelsIncludingACP,
        hasThinkingEffortLevels,
        getEffectiveThinkingEffort,
        getEffectiveThinkingEffortForModel,
        setThinkingEffortForModelLocal,
        getEffectiveModeId,
        hasPreferredMode,
        updateAgentField,
        setDefaultAgent,
        canRefreshModels,
        agentCanResume,
        supportsACP,
        supportsCLI,
        supportsMidTurn,
        supportsDualTransport,
        getAgentTransport,
        invalidateACPStateCache,
        updateACPModelList,
        restoreOriginalModels,
        restoreACPModelView,
        resetModelViewForTransport,
        applyRefreshedModelList,
        populateACPStateFromCache,
        duplicateAgent,
        createAcpAgent,
        deleteAgent,
        rescanAgents,
        reorderAgents,
    }
}
