import { ref, computed } from 'vue'
import { useAgents } from '@/composables/useAgents'
import { gt } from '@/composables/useLocale'

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
const currentModeId = ref('')
const currentModeName = ref('')
const availableModes = ref<Array<{ id: string; name: string }>>([])
const availableCommands = ref<Array<{ name: string; description: string; inputHint?: string }>>([])
const availableThinkingEfforts = ref<Array<{ id: string; name: string }>>([])
const runningSessions = ref(new Set<string>())
// Bumped on every mutation to runningSessions so computed properties
// that depend on the set's contents re-evaluate correctly.
const runningSessionsVersion = ref(0)

// Whether the global session drawer is open. Lifted from ChatPanelContent
// to useSessionIdentity so App.vue can render a single SessionDrawer
// instance that's accessible from any tab (chat, viewer, QuoteQuestionBar).
const sessionDrawerOpen = ref(false)

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
  currentModeId.value = ''
  currentModeName.value = ''
  availableModes.value = []
  availableCommands.value = []
  availableThinkingEfforts.value = []
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

async function saveThinkingPref(agentId: string, level: string) {
  if (!agentId) return
  // Save to server (agent YAML) via PATCH /api/agents
  const { patchAgentPref } = await import('@/composables/useSettingsConfig')
  patchAgentPref(agentId, 'preferred_thinking_effort', level).catch(() => {})
}

function loadThinkingPref(agentId: string): string | null {
  if (!agentId) return null
  // Read from agent's server-side preference (preferredThinkingEffort > thinkingEffort)
  const { getEffectiveThinkingEffort } = useAgents()
  return getEffectiveThinkingEffort(agentId) || null
}

// ───────────────────────────────────────────────────────────
// Mode state — ACP session mode (ask/architect/code)
// Updated from SSE mode_update/config_update events.
// Only populated for ACP-backed sessions that support modes.
// ───────────────────────────────────────────────────────────

/** Update mode state from SSE mode_update or config_update event. */
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
export function updateThinkingEffortState(currentId: string, levels: Array<{ id: string; name: string }>) {
  if (currentId) {
    currentThinkingEffort.value = currentId
  }
  if (levels.length > 0) {
    availableThinkingEfforts.value = levels
  }
}

/** Clear thinking effort state (called on session switch). */
export function clearThinkingEffortState() {
  availableThinkingEfforts.value = []
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
    const [chatResp] = await Promise.all([
      fetch('/api/ai/chat?limit=1'),
      agentsApi.loadAgents(),
    ])
    if (chatResp.ok) {
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
        // Initialize thinking effort: prefer server data, then localStorage pref
        if (data.thinkingEffort) {
          currentThinkingEffort.value = data.thinkingEffort
        } else {
          currentThinkingEffort.value = loadThinkingPref(data.agentId || '') || ''
        }
        // Populate slash commands from chat response (cached ACP state)
        if (Array.isArray(data.commands) && data.commands.length > 0 && availableCommands.value.length === 0) {
          availableCommands.value = data.commands
        }
        // Populate mode state from chat response (DB-persisted ACP state)
        if (data.modeState && data.modeState.availableModes?.length > 0) {
          updateModeState(data.modeId || data.modeState.currentModeId || '', data.modeState.availableModes)
        }
        // Populate thinking effort state from chat response (DB-persisted ACP state)
        if (data.thinkingEffortState && data.thinkingEffortState.availableLevels?.length > 0) {
          updateThinkingEffortState(data.thinkingEffort || data.thinkingEffortState.currentLevelId || '', data.thinkingEffortState.availableLevels)
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
      console.error('Failed to create session:', err)
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
        : '/api/ai/chat'
      await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: text, filePaths: filePaths || [], modelId: currentModelId.value || undefined, thinkingEffort: currentThinkingEffort.value || undefined }),
      })
    } catch (err) {
      console.error('Failed to send message:', err)
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
    currentModeId,
    currentModeName,
    availableModes,
    availableCommands,
    availableThinkingEfforts,
    runningSessions,
    runningSessionsVersion,
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
  }
}
