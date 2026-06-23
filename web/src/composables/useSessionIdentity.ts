import { ref, computed } from 'vue'
import { useAgents, registerIdentityUpdaters } from '@/composables/useAgents'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'

const TAG = 'SessionIdentity'

// ───────────────────────────────────────────────────────────
// Module-level singleton state — shared across the whole app.
// Session identity is globally needed (App.vue, QuoteQuestionBar,
// ChatPanel, etc.) but session *interaction* (messages, stream,
// polling) belongs to ChatPanel. This singleton holds only the
// identity layer.
// ───────────────────────────────────────────────────────────

const currentSessionId = ref('')
const currentSessionTitle = ref('')
const currentBackend = ref('')
export const currentAgentId = ref('')
const currentModelId = ref('')
const currentModelName = ref('')
const currentThinkingEffort = ref('')
const currentThinkingEffortName = ref('')
const currentModeId = ref('')
const currentModeName = ref('')
const currentTransport = ref('') // 'acp-stdio' or 'cli'
const autoApprove = ref(false)
const availableModes = ref<Array<{ id: string; name: string }>>([])
const availableCommands = ref<Array<{ name: string; description: string; inputHint?: string }>>([])
const availableThinkingEfforts = ref<Array<{ id: string; name: string }>>([])
const contextUsed = ref(0)
const contextSize = ref(0)
const contextCost = ref(0)
const contextCurrency = ref('')
export const runningSessions = ref(new Set<string>())
// Bumped on every mutation to runningSessions so computed properties
// that depend on the set's contents re-evaluate correctly.
const runningSessionsVersion = ref(0)

// Whether the global session drawer is open. Lifted from ChatPanelContent
// to useSessionIdentity so App.vue can render a single SessionDrawer
// instance that's accessible from any tab (chat, viewer, QuoteQuestionBar).
const sessionDrawerOpen = ref(false)

// Register identity updaters in useAgents to break the circular dependency.
// This must run at module evaluation time so that useAgents can call the
// updaters during loadAgents() without importing useSessionIdentity.
registerIdentityUpdaters({
  updateAvailableModes,
  updateAvailableThinkingEfforts,
  updateCommandState,
  currentAgentId,
})

/** Reset all module-level singleton refs — used by SPA hot project switch. */
/** Read-only accessor for the current session ID (no composable setup needed). */
export function getSessionId(): string {
  return currentSessionId.value
}

export function resetIdentity(): void {
  currentSessionId.value = ''
  currentSessionTitle.value = ''
  currentBackend.value = ''
  currentAgentId.value = ''
  currentModelId.value = ''
  currentModelName.value = ''
  currentThinkingEffort.value = ''
  currentThinkingEffortName.value = ''
  currentModeId.value = ''
  currentModeName.value = ''
  currentTransport.value = ''
  autoApprove.value = false
  availableModes.value = []
  availableCommands.value = []
  availableThinkingEfforts.value = []
  contextUsed.value = 0
  contextSize.value = 0
  contextCost.value = 0
  contextCurrency.value = ''
  runningSessions.value = new Set()
  runningSessionsVersion.value = 0
  sessionDrawerOpen.value = false
  _switchSession = null
  _createSession = null
  _deleteSession = null
  _sendMessage = null
  _openChatPanel = null
  _continueFromExecution = null
  _checkContinueSession = null
  _sessionDrawerRef = null
  // Clean up E2E test bridge
  if (typeof window !== 'undefined') {
    const bridge = (window as any).__clawbench
    if (bridge) {
      bridge.createSession = null
      bridge.switchSession = null
      bridge.deleteSession = null
    }
  }
}

// ───────────────────────────────────────────────────────────
// Agent preference persistence — stored in agent YAML files via PATCH /api/agents.
// preferredModel / preferredThinkingEffort are the source of truth for
// interactive sessions. Scheduled tasks use BaseModelID() and ThinkingEffort
// (the agent's original defaults) instead.
// ───────────────────────────────────────────────────────────

async function saveModelPref(agentId: string, modelId: string) {
  if (!agentId || !modelId) return
  // No-op: model selection in chat is session-scoped and does NOT update the agent's
  // default model. The agent's preferredModel is configured exclusively via the
  // settings panel (which calls patchAgentPref directly).
}

function loadModelPref(agentId: string): string | null {
  if (!agentId) return null
  // Read from agent's server-side preference (preferredModel)
  const { getAgent } = useAgents()
  const agent = getAgent(agentId)
  return agent?.preferredModel || null
}

async function saveThinkingPref(agentId: string, _level: string) {
  if (!agentId) return
  // No-op: thinking effort selection in chat is session-scoped and does NOT update
  // the agent's default. The agent's preferredThinkingEffort is configured exclusively
  // via the settings panel or SessionSettingModal star button (which calls patchAgentPref directly).
}

function loadThinkingPref(agentId: string): string | null {
  if (!agentId) return null
  // Read from agent's server-side preference (preferredThinkingEffort > thinkingEffort)
  const { getEffectiveThinkingEffort } = useAgents()
  return getEffectiveThinkingEffort(agentId) || null
}

// ───────────────────────────────────────────────────────────
// Mode state — ACP session mode (ask/architect/code)
// currentModeId is set by agent SSE events (takes priority),
// user action (local ref update), or DB restore (initSessionFromAPI).
// Agent-initiated mode changes override user selection.
// ───────────────────────────────────────────────────────────

/** Update mode state from REST API or user action (full state). */
export function updateModeState(modeId: string, modes: Array<{ id: string; name: string }>) {
  if (modeId) {
    currentModeId.value = modeId
    const mode = modes.find(m => m.id === modeId)
    currentModeName.value = mode?.name || modeId
  }
  if (modes.length > 0) {
    availableModes.value = modes
  }
}

/** Update available modes list without changing current selection.
 * Used by acpStates cache population (useAgents) — currentModeId
 * is managed by agent SSE events or user action, not by cache restore. */
export function updateAvailableModes(modes: Array<{ id: string; name: string }>) {
  if (modes.length > 0) {
    availableModes.value = modes
  }
}

/** Clear mode state (called on session switch or when leaving ACP session). */
export function clearModeState() {
  currentModeId.value = ''
  currentModeName.value = ''
  availableModes.value = []
}

/** Update available slash commands from ACP commands_update event. */
export function updateCommandState(commands: Array<{ name: string; description: string; inputHint?: string }>) {
  availableCommands.value = commands
}

/** Clear command state (called on session switch). */
export function clearCommandState() {
  availableCommands.value = []
}

/**
 * Slash commands are now populated from GET /api/agents (acpStates.commands)
 * and SSE commands_update events — no separate prefetch HTTP request needed.
 * This function is kept as a no-op for backward compatibility with call sites
 * that haven't been updated yet.
 * @deprecated Use acpStates from /api/agents instead.
 */
export async function prefetchCommands(_agentId: string) {
  // No-op: commands are now pre-populated from /api/agents acpStates
}

/** Update thinking effort state from SSE thinking_effort_update event. */
/** Update thinking effort state from REST API or user action (full state). */
export function updateThinkingEffortState(currentId: string, levels: Array<{ id: string; name: string }>) {
  if (currentId) {
    currentThinkingEffort.value = currentId
    const level = levels.find(l => l.id === currentId)
    currentThinkingEffortName.value = level?.name || currentId
  }
  if (levels.length > 0) {
    availableThinkingEfforts.value = levels
    // Resolve name if id was set before levels arrived
    if (currentThinkingEffort.value && !currentThinkingEffortName.value) {
      const level = levels.find(l => l.id === currentThinkingEffort.value)
      currentThinkingEffortName.value = level?.name || currentThinkingEffort.value
    }
  }
}

/** Update available thinking effort levels without changing current selection.
 * Used by SSE thinking_effort_update handler — currentThinkingEffort
 * is managed by user action + DB, not by agent notifications. */
export function updateAvailableThinkingEfforts(levels: Array<{ id: string; name: string }>) {
  if (levels.length > 0) {
    availableThinkingEfforts.value = levels
    // Resolve name if id was set before levels arrived
    if (currentThinkingEffort.value) {
      const level = levels.find(l => l.id === currentThinkingEffort.value)
      currentThinkingEffortName.value = level?.name || currentThinkingEffort.value
    }
  }
}

/** Clear thinking effort state (called on session switch). */
export function clearThinkingEffortState() {
  availableThinkingEfforts.value = []
  currentThinkingEffortName.value = ''
}

/** Update context usage state from SSE usage_update event. */
export function updateUsageState(used: number, size: number, cost?: number, currency?: string) {
  contextUsed.value = used
  contextSize.value = size
  contextCost.value = cost ?? 0
  contextCurrency.value = currency ?? ''
}

/** Clear usage state (called on session switch or reset). */
export function clearUsageState() {
  contextUsed.value = 0
  contextSize.value = 0
  contextCost.value = 0
  contextCurrency.value = ''
}

/** Toggle auto-approve mode and persist to server. */
export function toggleAutoApprove(enabled: boolean) {
  autoApprove.value = enabled
  const sid = currentSessionId.value
  if (sid) {
    fetch('/api/ai/session/update', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: sid, autoApprove: enabled }),
    }).catch(err => {
      appLog.e(TAG, 'Failed to update autoApprove:', err)
    })
  }
}

// ───────────────────────────────────────────────────────────
// Action callbacks — registered by ChatPanel on mount.
// Inversion of control: singleton owns the identity refs, but
// ChatPanel owns the session *operations*. Other consumers
// (App.vue, QuoteQuestionBar) trigger actions through these
// proxies, which delegate to ChatPanel's implementation.
// ───────────────────────────────────────────────────────────

let _switchSession: ((sessionId: string) => Promise<void>) | null = null
let _createSession: ((agentId?: string) => Promise<void>) | null = null
let _deleteSession: ((sessionId: string, backend?: string) => Promise<void>) | null = null
let _sendMessage: ((text: string, filePaths?: string[]) => Promise<void>) | null = null
let _openChatPanel: (() => void) | null = null
let _continueFromExecution: ((taskId: number, execId: number, switchTabFn: (tab: string) => void) => Promise<boolean>) | null = null
let _checkContinueSession: ((taskId: number, execId: number) => Promise<{ exists: boolean; sessionId: string }>) | null = null
// SessionDrawer component ref — set by App.vue. Allows any component to
// trigger openAgentSelector() on the global drawer without coupling.
let _sessionDrawerRef: any = null

export interface SessionActions {
  switchSession: (sessionId: string) => Promise<void>
  createSession: (agentId?: string) => Promise<void>
  deleteSession: (sessionId: string, backend?: string) => Promise<void>
  sendMessage: (text: string, filePaths?: string[]) => Promise<void>
  openChatPanel: () => void
  continueFromExecution: (taskId: number, execId: number, switchTabFn: (tab: string) => void) => Promise<boolean>
  checkContinueSession: (taskId: number, execId: number) => Promise<{ exists: boolean; sessionId: string }>
  forkSession: (sessionId: string) => Promise<boolean>
}

/**
 * Register session action callbacks. Called by App.vue on mount
 * (for openAgentSelector) and ChatPanel on mount (for the rest).
 *
 * Also exposes a minimal E2E test bridge on window.__clawbench
 * so Playwright can call createSession/switchSession without
 * page reload — session state is updated in-place by the Vue
 * reactivity system.
 */
export function registerSessionActions(actions: SessionActions) {
  _switchSession = actions.switchSession
  _createSession = actions.createSession
  _deleteSession = actions.deleteSession
  _sendMessage = actions.sendMessage
  _openChatPanel = actions.openChatPanel
  _continueFromExecution = actions.continueFromExecution
  _checkContinueSession = actions.checkContinueSession

  // Expose E2E test bridge on window for Playwright access.
  // These allow tests to create/switch sessions without page reload,
  // which is essential for ACP tests that need to switch agents mid-test.
  if (typeof window !== 'undefined') {
    const bridge = (window as any).__clawbench || ((window as any).__clawbench = {})
    bridge.createSession = actions.createSession
    bridge.switchSession = actions.switchSession
    bridge.deleteSession = actions.deleteSession
  }
}

/** Register the SessionDrawer component ref so openAgentSelector() works. */
export function registerSessionDrawerRef(drawerRef: any) {
  _sessionDrawerRef = drawerRef
}

/**
 * Pre-fill session identity from the API. Called by App.vue on mount
 * so that QuoteQuestionBar can display correct session info even
 * before ChatPanel is opened.
 */
export async function initSessionFromAPI() {
  const agentsApi = useAgents()
  try {
    const initCtrl = new AbortController()
    const initTimer = setTimeout(() => initCtrl.abort(), 60000)
    const [chatResp] = await Promise.all([
      fetch('/api/ai/chat?limit=1', { signal: initCtrl.signal }).catch((e) => {
        if (initCtrl.signal.aborted) return null as any
        throw e
      }),
      agentsApi.loadAgents(),
    ])
    clearTimeout(initTimer)
    if (chatResp && chatResp.ok) {
      const data = await chatResp.json()
      if (data.sessionId) {
        currentSessionId.value = data.sessionId
        currentSessionTitle.value = data.sessionTitle || ''
        currentBackend.value = data.backend || ''
        currentAgentId.value = data.agentId || ''
        // Slash commands are now populated from /api/agents acpStates (via loadAgents)
        // and from the chat response below — no separate prefetch request needed.
        // Initialize model: prefer server-persisted modelId, then localStorage pref, then agent default
        if (data.modelId) {
          currentModelId.value = data.modelId
          const model = agentsApi.getAgentModel(data.agentId || '', data.modelId)
          currentModelName.value = model?.name || data.modelId
        } else {
          const savedModelId = loadModelPref(data.agentId || '')
          if (savedModelId) {
            const model = agentsApi.getAgentModel(data.agentId || '', savedModelId)
            if (model) {
              currentModelId.value = savedModelId
              currentModelName.value = model.name
            } else {
              // Saved model no longer available — fall back to agent default & clear stale pref
              const { modelId, modelName } = agentsApi.syncModelFromAgent(data.agentId || '')
              currentModelId.value = modelId
              currentModelName.value = modelName
            }
          } else {
            const { modelId, modelName } = agentsApi.syncModelFromAgent(data.agentId || '')
            currentModelId.value = modelId
            currentModelName.value = modelName
          }
        }
        // Initialize thinking effort: from ACP state or localStorage pref
        if (data.thinkingEffortState?.currentId) {
          currentThinkingEffort.value = data.thinkingEffortState.currentId
          const level = data.thinkingEffortState.availableLevels?.find((l: {id: string; name: string}) => l.id === data.thinkingEffortState.currentId)
          currentThinkingEffortName.value = level?.name || data.thinkingEffortState.currentId
        } else {
          currentThinkingEffort.value = loadThinkingPref(data.agentId || '') || ''
        }
        // Initialize transport from agent's transport field
        if (data.transport) {
          currentTransport.value = data.transport
        } else {
          // Fall back to agent's stored transport
          const agent = agentsApi.getAgent(data.agentId || '')
          currentTransport.value = agent?.transport || 'cli'
        }
        // Initialize autoApprove from server state
        if (data.autoApprove !== undefined) {
          autoApprove.value = data.autoApprove
        }
        // Populate slash commands from chat response (cached ACP state)
        if (Array.isArray(data.commands) && data.commands.length > 0 && availableCommands.value.length === 0) {
          availableCommands.value = data.commands
        }
        // Initialize mode: from ACP state currentModeId
        // Only populate mode state for ACP-capable agents — CLI backends don't support modes
        if (agentsApi.supportsDualTransport(data.agentId || '') && data.modeState?.currentModeId) {
          currentModeId.value = data.modeState.currentModeId
        }
        // Populate mode state from chat response — update available modes.
        // Only for ACP-capable agents — CLI backends don't support modes
        if (agentsApi.supportsDualTransport(data.agentId || '') && data.modeState && data.modeState.availableModes?.length > 0) {
          updateAvailableModes(data.modeState.availableModes)
          // Set mode name from available modes now that the list is populated
          if (currentModeId.value) {
            const mode = data.modeState.availableModes.find((m: any) => m.id === currentModeId.value)
            currentModeName.value = mode?.name || currentModeId.value
          }
        }
        // Populate thinking effort state — update available levels.
        // currentThinkingEffort was already set above from ACP state.
        // Only for ACP-capable agents — CLI backends don't support thinking effort
        if (agentsApi.supportsDualTransport(data.agentId || '') && data.thinkingEffortState && data.thinkingEffortState.availableLevels?.length > 0) {
          updateAvailableThinkingEfforts(data.thinkingEffortState.availableLevels)
        } else if (agentsApi.supportsDualTransport(data.agentId || '') && data.agentId) {
          // Fallback: agent config (e.g. OpenCode/Kimi ACP don't expose thought_level)
          const agentLevels = agentsApi.getAgentThinkingEffortLevels(data.agentId)
          if (agentLevels.length > 0) {
            const levels = agentLevels.map((id: string) => ({ id, name: id }))
            updateAvailableThinkingEfforts(levels)
            // Resolve name if currentThinkingEffort was set from localStorage pref
            if (currentThinkingEffort.value && !currentThinkingEffortName.value) {
              const level = levels.find(l => l.id === currentThinkingEffort.value)
              currentThinkingEffortName.value = level?.name || currentThinkingEffort.value
            }
          }
        }
        // Initialize usage state from server cached data
        if (data.usageState && data.usageState.size > 0) {
          updateUsageState(data.usageState.used ?? 0, data.usageState.size, data.usageState.cost, data.usageState.currency)
        }
      }
    }
  } catch (_) {
    // Silently ignore — ChatPanel will load properly when opened
  }
}

// ───────────────────────────────────────────────────────────
// Computed helpers
// ───────────────────────────────────────────────────────────

const agentHeaderTitle = computed(() => {
  const { agentHeaderTitle: makeTitle } = useAgents()
  if (currentAgentId.value) return makeTitle(currentAgentId.value)
  return gt('chat.session.aiDialog')
})

// ───────────────────────────────────────────────────────────
// Public composable
// ───────────────────────────────────────────────────────────

export function useSessionIdentity() {
  /**
   * Switch to a different session. Delegates to ChatPanel's
   * implementation if registered, otherwise falls back to a
   * simple API call (pre-ChatPanel mount scenario).
   */
  async function switchSession(sessionId: string) {
    if (_switchSession) {
      await _switchSession(sessionId)
    }
  }

  /**
   * Create a new session. Delegates to ChatPanel if available,
   * otherwise makes a direct API call and updates identity refs.
   */
  async function createSession(agentId?: string) {
    if (_createSession) {
      await _createSession(agentId)
      return
    }
    // Fallback: direct API call (ChatPanel not yet mounted)
    try {
      const body = agentId ? { agentId } : {}
      const resp = await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await resp.json()
      if (data.ok && data.sessionId) {
        currentSessionId.value = data.sessionId
        currentSessionTitle.value = data.title || ''
        currentBackend.value = data.backend || ''
        currentAgentId.value = data.agentId || agentId || ''
        // Initialize model: prefer localStorage pref, then agent default
        const agentsApi = useAgents()
        const savedModelId = loadModelPref(currentAgentId.value)
        if (savedModelId) {
          const model = agentsApi.getAgentModel(currentAgentId.value, savedModelId)
          if (model) {
            currentModelId.value = savedModelId
            currentModelName.value = model.name
          } else {
            const { modelId, modelName } = agentsApi.syncModelFromAgent(currentAgentId.value)
            currentModelId.value = modelId
            currentModelName.value = modelName
          }
        } else {
          const { modelId, modelName } = agentsApi.syncModelFromAgent(currentAgentId.value)
          currentModelId.value = modelId
          currentModelName.value = modelName
        }
        // Initialize thinking effort from localStorage pref
        currentThinkingEffort.value = loadThinkingPref(currentAgentId.value) || ''
      }
    } catch (err) {
      appLog.e(TAG, 'Failed to create session:', err)
    }
  }

  /**
   * Delete a session. Delegates to ChatPanel if available.
   */
  async function deleteSession(sessionId: string, backend?: string) {
    if (_deleteSession) {
      await _deleteSession(sessionId, backend)
    }
  }

  /**
   * Send a message to the current session. Delegates to ChatPanel
   * if available, otherwise makes a direct API call.
   */
  async function sendMessage(text: string, filePaths?: string[]) {
    if (_sendMessage) {
      await _sendMessage(text, filePaths)
      return
    }
    // Fallback: direct API call (ChatPanel not yet mounted)
    try {
      let sid = currentSessionId.value
      if (!sid) {
        const createResp = await fetch('/api/ai/sessions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({}),
        })
        const createData = await createResp.json()
        if (createData.ok && createData.sessionId) {
          sid = createData.sessionId
          currentSessionId.value = sid
        }
      }
      const url = sid
        ? `/api/ai/chat?session_id=${encodeURIComponent(sid)}`
        : null
      if (!url) {
        appLog.e(TAG, 'sendMessage: no session ID available, cannot send')
        return
      }
      await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: text, filePaths: filePaths || [], modelId: currentModelId.value || undefined, thinkingEffort: currentThinkingEffort.value || undefined, transport: currentTransport.value || undefined }),
      })
    } catch (err) {
      appLog.e(TAG, 'Failed to send message:', err)
    }
  }

  /**
   * Open the chat panel. Delegates to App.vue's openDrawer logic
   * via the registered callback.
   */
  function openChatPanel() {
    if (_openChatPanel) {
      _openChatPanel()
    }
  }

  /**
   * Continue a task execution as a new chat session.
   * Delegates to ChatPanel's implementation if registered.
   */
  async function continueFromExecution(taskId: number, execId: number, switchTabFn: (tab: string) => void): Promise<boolean> {
    if (_continueFromExecution) {
      return await _continueFromExecution(taskId, execId, switchTabFn)
    }
    return false
  }

  /**
   * Check whether a continued session already exists for a task execution.
   * Delegates to ChatPanel's implementation if registered.
   */
  async function checkContinueSession(taskId: number, execId: number): Promise<{ exists: boolean; sessionId: string }> {
    if (_checkContinueSession) {
      return await _checkContinueSession(taskId, execId)
    }
    return { exists: false, sessionId: '' }
  }

  /** Open the global session drawer (sets sessionDrawerOpen = true). */
  function openSessionTab() {
    sessionDrawerOpen.value = true
  }

  /** Open the agent selector inside the session drawer. */
  function openAgentSelector() {
    _sessionDrawerRef?.openAgentSelector()
  }

  return {
    // Identity refs (read-only for consumers; ChatPanel writes to them)
    currentSessionId,
    currentSessionTitle,
    currentBackend,
    currentAgentId,
    currentModelId,
    currentModelName,
    currentThinkingEffort,
    currentThinkingEffortName,
    currentModeId,
    currentModeName,
    currentTransport,
    autoApprove,
    availableModes,
    availableCommands,
    availableThinkingEfforts,
    runningSessions,
    runningSessionsVersion,
    contextUsed,
    contextSize,
    contextCost,
    contextCurrency,
    agentHeaderTitle,
    // Global session drawer state
    sessionDrawerOpen,
    // Action proxies
    switchSession,
    createSession,
    deleteSession,
    sendMessage,
    openChatPanel,
    openSessionTab,
    openAgentSelector,
    continueFromExecution,
    checkContinueSession,
    // Registration (for ChatPanel)
    registerSessionActions,
    // Init (for App.vue)
    initSessionFromAPI,
    // LocalStorage persistence helpers
    saveModelPref,
    saveThinkingPref,
    loadModelPref,
    loadThinkingPref,
    toggleAutoApprove,
    // Mode state helpers
    updateModeState,
    updateAvailableModes,
    clearModeState,
    updateCommandState,
    clearCommandState,
    updateAvailableThinkingEfforts,
    clearThinkingEffortState,
    updateUsageState,
    clearUsageState,
  }
}
