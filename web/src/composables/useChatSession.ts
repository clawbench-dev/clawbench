import { ref, computed, type Ref } from 'vue'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { useNotification } from '@/composables/useNotification.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { clearModeState, updateModeState, clearCommandState, updateCommandState, updateThinkingEffortState, clearThinkingEffortState, currentAgentId as _currentAgentId } from '@/composables/useSessionIdentity.ts'
import { clearPlanState } from '@/composables/usePlanProgress'
import { useAgents, restoreOriginalModels, populateACPStateFromCache } from '@/composables/useAgents'
import { store } from '@/stores/app.ts'
import { buildMessageSnapshot, parseMessages } from '@/utils/chatSessionUtils.ts'
import { warmWorktreeCache } from '@/composables/useWorktreeAnnotation.ts'

// Module-level one-time session list load (replaces continuous polling)
// Accessible from App.vue without instantiating useChatSession
export async function loadSessionsOnce() {
  try {
    const identity = useSessionIdentity()
    const res = await fetch('/api/ai/sessions')
    if (res.ok) {
      const data = await res.json()
      const sessions = data.sessions || []
      const hasRunning = sessions.some((s: any) => s.running)
      const hasUnread = sessions.some((s: any) => s.unreadCount > 0 && s.id !== identity.currentSessionId.value)
      store.state.chatRunning = hasRunning
      store.state.chatUnread = hasUnread
      // Update session count for header indicator
      if (typeof data.totalCount === 'number') {
        store.state.sessionCount = data.totalCount
      }
      // Populate runningSessions set from API data
      identity.runningSessions.value.clear()
      for (const s of sessions) {
        if (s.running) identity.runningSessions.value.add(s.id)
      }
      identity.runningSessionsVersion.value++
    }
  } catch { /* ignore */ }
}

export interface UseChatSessionOptions {
  currentSessionId: Ref<string>
  messages: Ref<any[]>
  loading: Ref<boolean>
  inputDisabled: Ref<boolean>
  blockTasks: Record<string, any>
  blockAskQuestions: Record<string, any>
  blockRagResults: Record<string, any>
  expandedTools: Ref<Record<string, boolean>>
  switching?: Ref<boolean>
  onParseAssistantContent: (content: string) => any
  onExtractScheduledTasks: (msgs: any[]) => void
  onRenderUpdate: (forceFull: boolean) => void
  onScrollBottom: (force?: boolean) => void
  onConnectStream: (sessionId: string) => void
  onStopPolling: () => void
  onDisconnectStream: () => void
  onOpen: () => void
  onStreamDone?: () => void
}

export function useChatSession(options: UseChatSessionOptions) {
  const {
    currentSessionId,
    messages,
    loading,
    inputDisabled,
    blockTasks,
    blockAskQuestions,
    blockRagResults,
    expandedTools,
    onParseAssistantContent,
    onExtractScheduledTasks,
    onRenderUpdate,
    onScrollBottom,
    onConnectStream,
    onStopPolling,
    onDisconnectStream,
    onOpen,
    onStreamDone,
  } = options

  const toast = useToast()
  const notification = useNotification()

  // ── Identity refs from singleton ──
  const identity = useSessionIdentity()
  const { currentSessionTitle, currentBackend, currentAgentId, currentModelId, currentModelName, currentThinkingEffort, runningSessions, runningSessionsVersion, availableCommands } = identity

  // ── Agents from singleton ──
  const { agents, loadAgents, getAgentIcon, getAgentName, syncModelFromAgent, getAgentModel, agentHeaderTitle: makeAgentTitle } = useAgents()

  // Helper: sync model state from agent config when agent changes
  function syncModelFromAgentLocal(agentId: string) {
    const { modelId, modelName } = syncModelFromAgent(agentId)
    currentModelId.value = modelId
    currentModelName.value = modelName
  }

  // Helper: sync model state from server data, preferring persisted modelId
  // over the agent default. Falls back to agent default when server has no model.
  // Also checks localStorage for a previously saved preference.
  function syncModelFromData(agentId: string, modelIdFromServer: string) {
    if (modelIdFromServer) {
      // Server has a model — use it (it was explicitly chosen for this session)
      currentModelId.value = modelIdFromServer
      const model = getAgentModel(agentId, modelIdFromServer)
      currentModelName.value = model?.name || modelIdFromServer
    } else {
      // No server model — check localStorage for saved preference
      const savedModelId = identity.loadModelPref(agentId)
      if (savedModelId) {
        const model = getAgentModel(agentId, savedModelId)
        if (model) {
          currentModelId.value = savedModelId
          currentModelName.value = model.name
        } else {
          // Saved model no longer available — clear stale pref and use default
          syncModelFromAgentLocal(agentId)
        }
      } else {
        syncModelFromAgentLocal(agentId)
      }
    }
  }

  // Helper: sync thinking effort from server data
  // Falls back to localStorage for a previously saved preference.
  function syncThinkingEffortFromData(thinkingEffortFromServer: string) {
    if (thinkingEffortFromServer) {
      currentThinkingEffort.value = thinkingEffortFromServer
    } else {
      currentThinkingEffort.value = identity.loadThinkingPref(currentAgentId.value) || ''
    }
  }

  // Switching state — true while a session switch is in progress (distinct from
  // "loading" which means "AI is generating"). Used to show a fade/placeholder
  // transition so the user sees immediate feedback instead of a frozen UI.
  const switching = ref(false)

  const lastMsgCount = ref(0)
  let msgCountInterval: ReturnType<typeof setInterval> | null = null

  // Pagination state
  const totalMessages = ref(0)
  const loadingMore = ref(false)
  const hasMore = computed(() => messages.value.length < totalMessages.value)

  const agentHeaderTitle = computed(() => makeAgentTitle(currentAgentId.value))

  // Guard against concurrent switchSession calls — only the last one wins
  let switchSessionSeq = 0

  // ── Change detection for polling ──
  // Tracks a lightweight fingerprint of the last loaded messages.
  // When polling-triggered reloads find no change, the UI is not refreshed,
  // preventing expandedTools collapse, scroll reset, and unnecessary re-renders.
  let lastMessageSnapshot = ''

  // forceScrollBottom: true = always scroll to bottom (switch session, first load)
  //                   false = only scroll if already near bottom (re-open panel, polling)
  // showOverlay: true = show the switching overlay (session switch, first open)
  //            false = silent reload (stream done, polling)
  // skipIfUnchanged: true = when data matches last snapshot, skip UI refresh entirely
  //                (used by polling to avoid collapsing expandedTools / resetting scroll)
  async function loadHistory(forceScrollBottom = true, showOverlay = false, skipIfUnchanged = false) {
    if (showOverlay) switching.value = true
    try {
      // Load agents first so we can resolve agent names
      if (agents.value.length === 0) await loadAgents()
      // Warm worktree cache so annotateWorktreePaths has data when rendering messages
      warmWorktreeCache(store.state.projectRoot)
      // Use max of initialMessages and current loaded count to avoid truncating lazy-loaded messages
      const limit = Math.max(store.state.chatInitialMessages, messages.value.length)
      const url = currentSessionId.value
        ? `/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${limit}`
        : `/api/ai/chat?limit=${limit}`
      const resp = await fetch(url)
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}))
        throw new Error(errData.error || gt('chat.session.requestFailed', { status: resp.status }))
      }
      const data = await resp.json()
      const rawMsgs = data.messages || []

      // Change detection: if skipIfUnchanged and data matches last snapshot, do nothing.
      // Always refresh when session is running (SSE events may have been dropped).
      const newSnapshot = buildMessageSnapshot(rawMsgs)
      if (skipIfUnchanged && newSnapshot === lastMessageSnapshot && !data.running) {
        switching.value = false
        return
      }
      lastMessageSnapshot = newSnapshot

      // Data has changed (or this is a full load) — apply new data.
      // Preserve expandedTools when only the last message changed (SSE done reload),
      // to avoid collapsing user-expanded tool details and triggering full re-render.
      // Only reset when message count or non-last message identities differ.
      const prevCount = messages.value.length
      const newCount = rawMsgs.length
      const sameCore = prevCount === newCount && prevCount > 0 && rawMsgs.slice(0, -1).every((m, i) => m.id === messages.value[i]?.id)
      if (!sameCore) {
        expandedTools.value = {}
      }
      // Clear stale blockAskQuestions — after backend converts <ask-question> text blocks
      // to tool_use blocks, old entries keyed by text-block indices would cause duplicate
      // rendering. extractScheduledTasks below will re-populate from current DB state.
      Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
      Object.keys(blockRagResults).forEach(k => delete blockRagResults[k])
      messages.value = parseMessages(rawMsgs, onParseAssistantContent, messages.value)
      totalMessages.value = data.total || messages.value.length
      currentSessionId.value = data.sessionId || ''
      currentSessionTitle.value = data.sessionTitle || ''
      currentBackend.value = data.backend || ''
      currentAgentId.value = data.agentId || ''
      syncModelFromData(currentAgentId.value, data.modelId)
      syncThinkingEffortFromData(data.thinkingEffort)
      // Populate ACP mode state from REST response (avoids waiting for SSE events
      // which may have already been consumed by a previous SSE handler).
      if (data.modeState && data.modeState.availableModes?.length > 0) {
        updateModeState(data.modeState.currentModeId || '', data.modeState.availableModes)
      }
      if (data.thinkingEffortState && data.thinkingEffortState.availableLevels?.length > 0) {
        updateThinkingEffortState(data.thinkingEffortState.currentId || '', data.thinkingEffortState.availableLevels)
      }
      // Populate slash commands from REST response (cached ACP state)
      if (Array.isArray(data.commands) && data.commands.length > 0 && availableCommands.value.length === 0) {
        updateCommandState(data.commands)
      }
      onExtractScheduledTasks(messages.value)
      onRenderUpdate(true)
      if (data.running) {
        loading.value = true
        stopMsgCountPolling()
        onScrollBottom(true)
        onConnectStream(currentSessionId.value)
      } else {
        loading.value = false
        startMsgCountPolling()
        onScrollBottom(forceScrollBottom)
      }
      switching.value = false
    } catch (err) {
      console.error('Failed to load chat history:', err)
      const _msg = err instanceof Error ? err.message : ''
      toast.show(_msg ? gt('chat.session.loadHistoryFailedDetail', { error: _msg }) : gt('chat.session.loadHistoryFailed'), { icon: '⚠️', type: 'error' })
      switching.value = false
    }
  }

  async function loadMoreMessages() {
    if (loadingMore.value || !hasMore.value || !currentSessionId.value) return
    loadingMore.value = true
    try {
      const pageSize = store.state.chatPageSize
      // Use cursor-based pagination: pass the id of the oldest loaded message
      const oldestMsg = messages.value[0]
      const beforeId = oldestMsg?.id || ''
      const resp = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${pageSize}&before_id=${encodeURIComponent(beforeId)}`)
      if (!resp.ok) return
      const data = await resp.json()
      const olderMsgs = parseMessages(data.messages || [], onParseAssistantContent)
      if (olderMsgs.length > 0) {
        messages.value = [...olderMsgs, ...messages.value]
        totalMessages.value = data.total || totalMessages.value
        onExtractScheduledTasks(olderMsgs)
        onRenderUpdate(true)
      }
    } catch (err) {
      console.error('Failed to load more messages:', err)
    } finally {
      loadingMore.value = false
    }
  }

  async function switchSession(sessionId) {
    // Increment sequence counter — if another switch starts before we finish,
    // our results will be discarded (last writer wins)
    const mySeq = ++switchSessionSeq

    // Mark switching state immediately so UI can show a fade/placeholder
    switching.value = true
    // Briefly lock input to prevent sending messages with stale sessionId.
    // This is the ONLY place inputDisabled is set to true — it defaults to false
    // and is restored as soon as the switch completes (even if the session is running).
    inputDisabled.value = true

    onDisconnectStream()
    onStopPolling()
    stopMsgCountPolling()
    lastMessageSnapshot = ''  // Invalidate snapshot — new session may have different data
    expandedTools.value = {}
    // Clear stale blockAskQuestions from previous session
    Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
    Object.keys(blockRagResults).forEach(k => delete blockRagResults[k])
    // Clear ACP state from previous session — will be repopulated by REST response
    // or SSE events only if the new session's agent actually supports ACP.
    clearModeState()
    clearCommandState()
    clearThinkingEffortState()
    // Restore original CLI model list in case ACP had overridden it
    const prevAgentId = _currentAgentId.value
    if (prevAgentId) restoreOriginalModels(prevAgentId)
    // Clear plan progress from previous session — will be repopulated by SSE plan_update
    clearPlanState()
    try {
      // Load agents first so we can resolve agent names
      if (agents.value.length === 0) await loadAgents()
      const limit = store.state.chatInitialMessages
      const resp = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(sessionId)}&limit=${limit}`)
      if (!resp.ok) {
        toast.show(gt('chat.session.switchFailed'), { icon: '⚠️', type: 'error' })
        return
      }
      const data = await resp.json()

      // If another switch happened while we were fetching, discard our results
      // (the newer switch will set switching=false when it completes)
      if (switchSessionSeq !== mySeq) return

      messages.value = parseMessages(data.messages || [], onParseAssistantContent)
      totalMessages.value = data.total || messages.value.length
      currentSessionId.value = data.sessionId || sessionId
      currentSessionTitle.value = data.sessionTitle || ''
      currentBackend.value = data.backend || ''
      currentAgentId.value = data.agentId || ''
      syncModelFromData(currentAgentId.value, data.modelId)
      syncThinkingEffortFromData(data.thinkingEffort)
      // Populate ACP mode state from REST response (avoids waiting for SSE events
      // which may have already been consumed by a previous SSE handler).
      if (data.modeState && data.modeState.availableModes?.length > 0) {
        updateModeState(data.modeState.currentModeId || '', data.modeState.availableModes)
      }
      if (data.thinkingEffortState && data.thinkingEffortState.availableLevels?.length > 0) {
        updateThinkingEffortState(data.thinkingEffortState.currentId || '', data.thinkingEffortState.availableLevels)
      }
      // Populate slash commands from REST response (cached ACP state)
      if (Array.isArray(data.commands) && data.commands.length > 0 && availableCommands.value.length === 0) {
        updateCommandState(data.commands)
      }
      onExtractScheduledTasks(messages.value)
      onRenderUpdate(true)
      onScrollBottom(true)
      if (data.running) {
        loading.value = true
        stopMsgCountPolling()
        onConnectStream(sessionId)
      } else {
        loading.value = false
        startMsgCountPolling()
      }
      // Recalculate global chatUnread after switching — the backend has already
      // marked this session as read (UpdateLastRead), so the session list will
      // reflect the correct unread state. Without this, chatUnread stays true
      // when the user is already on the chat tab (switchTab early-returns).
      await loadSessionsOnce()
    } catch (err) {
      // If another switch happened, don't touch state
      if (switchSessionSeq !== mySeq) return
      console.error('Failed to switch session:', err)
      toast.show(gt('chat.session.switchFailed'), { icon: '⚠️', type: 'error' })
    } finally {
      // Always restore input — switchSession is the only place that locks it,
      // so it must always unlock regardless of success/failure/race.
      // If a newer switch started, it will set inputDisabled=true again immediately.
      inputDisabled.value = false
      switching.value = false
    }
  }

  async function createSession(agentId) {
    try {
      // Load agents first so UI can resolve agent names
      if (agents.value.length === 0) await loadAgents()
      const body = agentId ? { agentId } : {}
      const resp = await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await resp.json()
      if (!resp.ok || !data.ok) {
        throw new Error(data.error || gt('chat.session.createFailed', { status: resp.status }))
      }
      currentSessionId.value = data.sessionId
      currentSessionTitle.value = data.title || ''
      currentBackend.value = data.backend || ''
      currentAgentId.value = data.agentId || agentId || ''
      syncModelFromData(currentAgentId.value, '')
      currentThinkingEffort.value = identity.loadThinkingPref(currentAgentId.value) || ''
      // Clear ACP state for new session — then immediately restore from
      // cached acpStates so mode/thinking/command chips appear without
      // needing to send the first message first.
      clearModeState()
      clearCommandState()
      clearThinkingEffortState()
      // Clear plan progress from previous session — plan is session-bound,
      // will be repopulated by SSE plan_update for the new session.
      clearPlanState()
      // Restore original CLI model list in case ACP had overridden it
      restoreOriginalModels(currentAgentId.value)
      // Re-populate ACP state from the agents cache (pool + DB persisted).
      // This makes mode chips visible immediately on new sessions.
      // May trigger a force-refresh of /api/agents if the cache is stale.
      await populateACPStateFromCache(currentAgentId.value)
      messages.value = []
      totalMessages.value = 0
      lastMessageSnapshot = ''  // New session — no messages yet
      Object.keys(blockTasks).forEach(k => delete blockTasks[k])
      Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
      Object.keys(blockRagResults).forEach(k => delete blockRagResults[k])
      loading.value = false
      const maxCount = store.state.sessionMaxCount
      if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
      toast.show(gt('chat.session.created', { count: data.sessionCount ?? '', max: maxCount }), { icon: '✨', type: 'success', duration: 1500 })
    } catch (err) {
      console.error('Failed to create session:', err)
      const _msg = err instanceof Error ? err.message : ''
      toast.show(_msg ? gt('chat.session.createSessionFailedDetail', { error: _msg }) : gt('chat.session.createSessionFailed'), { icon: '⚠️', type: 'error' })
    }
  }

  async function deleteSession(sessionId, backend) {
    try {
      const resp = await fetch(`/api/ai/session/delete?session_id=${encodeURIComponent(sessionId)}&backend=${encodeURIComponent(backend || '')}`, {
        method: 'DELETE',
      })
      const data = await resp.json()
      if (data.ok) {
        // If deleted current session, switch to another
        if (sessionId === currentSessionId.value) {
          const sessionsResp = await fetch('/api/ai/sessions')
          const sessionsData = await sessionsResp.json()
          if (sessionsData.sessions && sessionsData.sessions.length > 0) {
            await switchSession(sessionsData.sessions[0].id, sessionsData.sessions[0].backend)
          } else {
            // No sessions left, create a default one
            await createSession()
          }
        } else {
          // Deleted a non-current session — refresh global state (chatUnread, chatRunning, runningSessions)
          await loadSessionsOnce()
        }
        const maxCount = store.state.sessionMaxCount
        if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
        toast.show(gt('chat.session.deleted', { count: data.sessionCount ?? '', max: maxCount }), { icon: '🗑️', type: 'success', duration: 2000 })
      }
    } catch (err) {
      console.error('Failed to delete session:', err)
      toast.show(gt('chat.session.deleteFailed'), { icon: '⚠️', type: 'error' })
    }
  }

  function startMsgCountPolling() {
    stopMsgCountPolling()
    if (!currentSessionId.value) return
    lastMsgCount.value = messages.value.length
    msgCountInterval = setInterval(async () => {
      if (!currentSessionId.value || loading.value) return
      try {
        const resp = await fetch(`/api/ai/chat/count?session_id=${encodeURIComponent(currentSessionId.value)}`)
        if (!resp.ok) return
        const data = await resp.json()
        if (data.count > lastMsgCount.value) {
          lastMsgCount.value = data.count
          // Reload history to pick up new messages (don't force scroll, skip if unchanged)
          await loadHistory(false, false, true)
        }
      } catch (err) {
        // Silently ignore polling errors
      }
    }, 15000)
  }

  function stopMsgCountPolling() {
    if (msgCountInterval) { clearInterval(msgCountInterval); msgCountInterval = null }
  }

  // Debounce timer for loadSessionsOnce after session events.
  // When multiple sessions complete in quick succession, we coalesce
  // the recalculations into a single API call after a short delay.
  let sessionEventDebounce: ReturnType<typeof setTimeout> | null = null

  // Called from WS session_update event
  function onSessionEvent(data: { session_id?: string; status?: string; has_new_messages?: boolean } | undefined) {
    if (!data) return
    const sid = data.session_id
    if (data.status === 'running') {
      store.state.chatRunning = true
      if (sid) { runningSessions.value.add(sid); runningSessionsVersion.value++ }
    } else {
      if (sid) { runningSessions.value.delete(sid); runningSessionsVersion.value++ }
      // Update global boolean from remaining set
      store.state.chatRunning = runningSessions.value.size > 0
      // Recalculate chatUnread from backend instead of optimistically setting true.
      // The old code unconditionally set chatUnread=true here, which caused phantom
      // flashing: a session that was already read (last_read_at set) would trigger
      // the flash, and the button kept blinking until loadSessionsOnce() corrected it.
      // Now we debounce-load the real unread state from the server.
      if (sid && sid !== currentSessionId.value) {
        if (sessionEventDebounce) clearTimeout(sessionEventDebounce)
        sessionEventDebounce = setTimeout(() => {
          sessionEventDebounce = null
          loadSessionsOnce()
        }, 500)
      }
    }
  }

  // One-time session list load — delegates to module-level function
  async function loadSessionsOnceInner() {
    await loadSessionsOnce()
  }

  // Track which sessions have already had their completion notification fired.
  // Prevents repeated sound/notification if an exception in the callback
  // prevents runningSessions from being updated.
  const notifiedSessions = new Set<string>()

  function handleVisibilityChange() {
    if (document.visibilityState === 'visible' && loading.value) {
      // Page became visible while streaming - reconnect
      onDisconnectStream()
      onStopPolling()
      loadHistory(true, false, true).catch(() => {
        // loadHistory failed — reset loading state so user isn't stuck
        loading.value = false
      })
    }
  }

  /**
   * Check whether a continued session already exists for a task execution.
   * Returns { exists, sessionId } — does not create anything.
   */
  async function checkContinueSession(taskId: number, execId: number): Promise<{ exists: boolean; sessionId: string }> {
    try {
      const resp = await fetch(`/api/tasks/${taskId}/executions/${execId}/continue`)
      if (!resp.ok) return { exists: false, sessionId: '' }
      const data = await resp.json()
      return { exists: !!data.exists, sessionId: data.sessionId || '' }
    } catch {
      return { exists: false, sessionId: '' }
    }
  }

  /**
   * Continue a task execution as a new chat session.
   * 1. GET check — if already continued, navigate to existing session
   * 2. POST create — create new session with copied history
   * 3. Navigate to chat tab and switch to the new/existing session
   * Returns true on success, false on error.
   */
  async function continueFromExecution(taskId: number, execId: number, switchTabFn: (tab: string) => void): Promise<boolean> {
    try {
      // Step 1: Pre-check
      const check = await checkContinueSession(taskId, execId)
      let sessionId = ''
      let isNewlyCreated = false

      if (check.exists && check.sessionId) {
        // Already continued — navigate to existing session (no toast)
        sessionId = check.sessionId
      } else {
        // Step 2: POST create
        const resp = await fetch(`/api/tasks/${taskId}/executions/${execId}/continue`, { method: 'POST' })
        if (!resp.ok) {
          const errData = await resp.json().catch(() => ({}))
          const msgKey = errData.msgKey || ''
          if (resp.status === 409 || msgKey === 'SessionLimitReached') {
            toast.show(gt('chat.session.sessionLimitReached'), { icon: '⚠️', type: 'error' })
          } else {
            toast.show(errData.error || gt('chat.session.continueFailed'), { icon: '⚠️', type: 'error' })
          }
          return false
        }
        const data = await resp.json()
        if (!data.ok || !data.sessionId) {
          toast.show(gt('chat.session.continueFailed'), { icon: '⚠️', type: 'error' })
          return false
        }
        sessionId = data.sessionId
        isNewlyCreated = !data.alreadyExists
        // Toast: only when a new session is actually created (not when restoring a deleted one)
        if (isNewlyCreated) {
          const maxCount = store.state.sessionMaxCount
          if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
          toast.show(gt('chat.session.continued', { count: data.sessionCount ?? '', max: maxCount }), { icon: '💬', type: 'success', duration: 1500 })
        }
      }

      // Step 3: Navigate — switchSession first (which sets currentSessionId and loads history),
      // then switchTab to make the chat panel visible.
      // Order matters: if we switchTab first, the chat panel re-renders and may call
      // loadHistory() with the OLD sessionId from cookie, overwriting our switchSession.
      // By switching the session first, the cookie and state are already correct when
      // the chat panel becomes visible.
      await switchSession(sessionId)
      switchTabFn('chat')
      return true
    } catch (err) {
      console.error('Failed to continue from execution:', err)
      toast.show(gt('chat.session.continueFailed'), { icon: '⚠️', type: 'error' })
      return false
    }
  }

  return {
    // Exposed refs (consumed by ChatPanelContent etc.)
    currentSessionId,
    currentSessionTitle,
    currentBackend,
    currentAgentId,
    runningSessions,
    // UI state — local to this instance
    agentHeaderTitle,
    totalMessages,
    hasMore,
    loadingMore,
    switching,
    // Operations
    loadHistory,
    loadMoreMessages,
    switchSession,
    createSession,
    deleteSession,
    onSessionEvent,
    loadSessionsOnce: loadSessionsOnceInner,
    startMsgCountPolling,
    stopMsgCountPolling,
    handleVisibilityChange,
    continueFromExecution,
    checkContinueSession,
    // Agent helpers — delegate to singleton
    getAgentIcon,
    getAgentName,
  }
}
