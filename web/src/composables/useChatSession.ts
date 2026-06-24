import { ref, computed, type Ref } from 'vue'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'ChatSession'
import { clearModeState, updateAvailableModes, clearCommandState, updateCommandState, updateAvailableThinkingEfforts, clearThinkingEffortState, clearUsageState, updateUsageState, currentAgentId as _currentAgentId } from '@/composables/useSessionIdentity.ts'
import { clearPlanState, updatePlanEntries } from '@/composables/usePlanProgress'
import { useAgents, restoreOriginalModels, getAgentThinkingEffortLevels } from '@/composables/useAgents'
import { store } from '@/stores/app.ts'
import { buildMessageSnapshot, parseMessages } from '@/utils/chatSessionUtils.ts'
import { warmWorktreeCache } from '@/composables/useWorktreeAnnotation.ts'

// Module-level one-time session list load (replaces continuous polling)
// Accessible from App.vue without instantiating useChatSession
let _sessionsLoadPromise: Promise<void> | null = null

export async function loadSessionsOnce(): Promise<void> {
  // Dedup: if a load is already in-flight, reuse its promise instead of
  // firing a duplicate request (e.g. App.vue + ChatPanelContent.vue
  // mounting in quick succession).
  if (_sessionsLoadPromise) return _sessionsLoadPromise
  _sessionsLoadPromise = (async () => {
    try {
      const identity = useSessionIdentity()
      const res = await fetch('/api/ai/sessions')
      if (res.ok) {
        const data = await res.json()
        const sessions = data.sessions || []
        const hasRunning = sessions.some((s: any) => s.running)
        const unreadCount = sessions.filter((s: any) =>
          (s.unreadCount > 0 || s.pendingApproval) && s.id !== identity.currentSessionId.value
        ).length
        store.state.chatRunning = hasRunning
        store.state.chatUnreadCount = unreadCount
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
    finally {
      _sessionsLoadPromise = null
    }
  })()
  return _sessionsLoadPromise
}

/** Reset internal dedup state — called during SPA hot project switch. */
export function resetChatSessionState(): void {
  _sessionsLoadPromise = null
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
  } = options

  const toast = useToast()

  // ── Identity refs from singleton ──
  const identity = useSessionIdentity()
  const { currentSessionTitle, currentBackend, currentAgentId, currentModelId, currentModelName, currentThinkingEffort, runningSessions, runningSessionsVersion, availableCommands, autoApprove, availableThinkingEfforts } = identity

  // ── Agents from singleton ──
  const { agents, loadAgents, getAgentIcon, getAgentName, getAgent, syncModelFromAgent, getAgentModel, agentHeaderTitle: makeAgentTitle } = useAgents()

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
      // Resolve name from available levels (may be empty if levels haven't loaded yet;
      // updateAvailableThinkingEfforts will resolve it when levels arrive)
      const levels = availableThinkingEfforts.value
      if (levels.length > 0) {
        const level = levels.find(l => l.id === thinkingEffortFromServer)
        identity.currentThinkingEffortName.value = level?.name || thinkingEffortFromServer
      }
    } else {
      currentThinkingEffort.value = identity.loadThinkingPref(currentAgentId.value) || ''
    }
  }

  function syncModeFromData(modeIdFromServer?: string, availableModes?: Array<{id: string; name: string}>) {
    if (modeIdFromServer) {
      identity.currentModeId.value = modeIdFromServer
      const mode = availableModes?.find(m => m.id === modeIdFromServer)
      identity.currentModeName.value = mode?.name || modeIdFromServer
    } else if (!identity.currentModeId.value) {
      // No server-persisted mode AND no agent-set mode via SSE — clear stale value.
      // Agent-initiated mode changes (via SSE mode_update/config_update) take priority
      // and should not be overwritten by an empty DB value (e.g. after stream completion).
      identity.currentModeName.value = ''
    }
  }

  // Helper: sync transport from server data
  // Falls back to agent's configured transport, defaulting to 'cli'.
  function syncTransportFromData(transportFromServer?: string) {
    if (transportFromServer) {
      identity.currentTransport.value = transportFromServer
    } else {
      const agent = getAgent(currentAgentId.value)
      identity.currentTransport.value = agent?.transport || 'cli'
    }
  }

  // Helper: sync usage state from server data
  function syncUsageFromData(usageStateData?: { used?: number; size?: number; cost?: number; currency?: string }) {
    if (usageStateData && (usageStateData.size ?? 0) > 0) {
      updateUsageState(usageStateData.used ?? 0, usageStateData.size ?? 0, usageStateData.cost, usageStateData.currency)
    }
  }

  // Switching state — true while a session switch is in progress (distinct from
  // "loading" which means "AI is generating"). Used to show a fade/placeholder
  // transition so the user sees immediate feedback instead of a frozen UI.
  const switching = ref(false)
  const deletingSessionIds = ref(new Set<string>())

  const lastMsgCount = ref(0)
  let msgCountInterval: ReturnType<typeof setInterval> | null = null

  // Pagination state
  const totalMessages = ref(0)
  const loadingMore = ref(false)
  const hasMore = computed(() => messages.value.length < totalMessages.value)

  const agentHeaderTitle = computed(() => makeAgentTitle(currentAgentId.value))

  // Guard against concurrent switchSession calls — only the last one wins
  let switchSessionSeq = 0

  // Guard against concurrent loadHistory calls — only the last one wins.
  // Without this, stale responses (e.g. from a loadHistory triggered before
  // visibility change) can overwrite currentSessionId with a wrong value.
  let loadHistorySeq = 0

  // ── Change detection for polling ──
  // Tracks a lightweight fingerprint of the last loaded messages.
  // When polling-triggered reloads find no change, the UI is not refreshed,
  // preventing expandedTools collapse, scroll reset, and unnecessary re-renders.
  let lastMessageSnapshot = ''

  // Pending reload: when loadHistory is called while a load is already in-flight,
  // we record the requested parameters and execute one more load after the current
  // one completes. This prevents redundant concurrent fetches while ensuring the
  // final state is always fresh.
  let loadHistoryInProgress = false
  let pendingReload: { forceScrollBottom: boolean; showOverlay: boolean; skipIfUnchanged: boolean } | null = null
  let loadHistoryDeferred: { promise: Promise<void> } | null = null

  // forceScrollBottom: true = always scroll to bottom (switch session, first load)
  //                   false = only scroll if already near bottom (re-open panel, polling)
  // showOverlay: true = show the switching overlay (session switch, first open)
  //            false = silent reload (stream done, polling)
  // skipIfUnchanged: true = when data matches last snapshot, skip UI refresh entirely
  //                (used by polling to avoid collapsing expandedTools / resetting scroll)
  async function loadHistory(forceScrollBottom = true, showOverlay = false, skipIfUnchanged = false) {
    // If a load is already in-flight, record the requested params and return
    // a promise that resolves when all queued loads complete. This coalesces
    // rapid calls while ensuring callers can await + .finally() and that the
    // final state is always fresh.
    if (loadHistoryInProgress) {
      pendingReload = { forceScrollBottom, showOverlay, skipIfUnchanged }
      // Return the in-flight load's promise so callers can await/finally it.
      // The pendingReload will be executed after the in-flight load completes.
      return loadHistoryDeferred!.promise
    }
    loadHistoryInProgress = true
    let resolveDeferred: () => void
    loadHistoryDeferred = { promise: new Promise<void>((r) => { resolveDeferred = r }) }

    const mySeq = ++loadHistorySeq
    if (showOverlay) switching.value = true
    try {
      // Warm worktree cache so annotateWorktreePaths has data when rendering messages
      warmWorktreeCache(store.state.projectRoot)
      // Use max of initialMessages and current loaded count to avoid truncating lazy-loaded messages
      const limit = Math.max(store.state.chatInitialMessages, messages.value.length)
      // CRITICAL: When currentSessionId is empty, use the cookie-aware recovery
      // endpoint WITHOUT session_id — but with the FULL limit so we get the session
      // identity AND messages in a single request (no double-fetch).
      // The backend falls back to GetLatestSessionID (ORDER BY updated_at DESC) which
      // returns the cookie-remembered session for this project.
      if (!currentSessionId.value) {
        // Recover session from backend — use full limit to get both identity and
        // messages in one request, avoiding the previous double-fetch pattern.
        // AbortController timeout is a safety net only; the backend itself has
        // ACP RPC timeouts (60s) so 60s gives ample room even for slow remote
        // connections. On abort, we catch and bail gracefully (no toast error).
        const recoverCtrl = new AbortController()
        const recoverTimer = setTimeout(() => recoverCtrl.abort(), 60000)
        let recoverResp: Response
        // Load agents in parallel with recovery fetch
        const agentsPromise = agents.value.length === 0 ? loadAgents() : Promise.resolve()
        try {
          recoverResp = await fetch(`/api/ai/chat?limit=${limit}`, { signal: recoverCtrl.signal })
        } catch (e) {
          clearTimeout(recoverTimer)
          if (recoverCtrl.signal.aborted) {
            // Timeout — bail without error toast, let retry handle it
            return
          }
          throw e
        }
        clearTimeout(recoverTimer)
        await agentsPromise
        if (loadHistorySeq !== mySeq) { return }
        if (recoverResp.ok) {
          const recoverData = await recoverResp.json()
          if (loadHistorySeq !== mySeq) { return }
          if (recoverData.sessionId) {
            currentSessionId.value = recoverData.sessionId
            currentSessionTitle.value = recoverData.sessionTitle || ''
            currentBackend.value = recoverData.backend || ''
            currentAgentId.value = recoverData.agentId || ''
            syncModelFromData(currentAgentId.value, recoverData.modelId)
            syncThinkingEffortFromData(recoverData.thinkingEffortState?.currentId || '')
            syncModeFromData(recoverData.modeState?.currentModeId || '', recoverData.modeState?.availableModes)
            syncTransportFromData(recoverData.transport)
            syncUsageFromData(recoverData.usageState)
            if (recoverData.autoApprove !== undefined) {
              autoApprove.value = recoverData.autoApprove
            }
            // If the recovery response already contains messages, use them directly
            // instead of making a second fetch below. This eliminates the double-fetch
            // that previously used limit=1 then re-fetched with full limit.
            const recoverMsgs = recoverData.messages || []
            if (recoverMsgs.length > 0) {
              // Change detection
              const newSnapshot = buildMessageSnapshot(recoverMsgs)
              if (skipIfUnchanged && newSnapshot === lastMessageSnapshot && !recoverData.running) {
                return
              }
              lastMessageSnapshot = newSnapshot
              const prevCount = messages.value.length
              const newCount = recoverMsgs.length
              const sameCore = prevCount === newCount && prevCount > 0 && recoverMsgs.slice(0, -1).every((m: any, i: number) => m.id === messages.value[i]?.id)
              if (!sameCore) {
                expandedTools.value = {}
              }
              Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
              Object.keys(blockRagResults).forEach(k => delete blockRagResults[k])
              // Replace messages — pending messages are in pendingStore, not messages.value
              messages.value = parseMessages(recoverMsgs, onParseAssistantContent, messages.value)
              totalMessages.value = recoverData.total || messages.value.length
              // Sync remaining session metadata from recovery response
              if (recoverData.modeState && recoverData.modeState?.availableModes?.length > 0) {
                updateAvailableModes(recoverData.modeState.availableModes)
              }
              if (recoverData.thinkingEffortState && recoverData.thinkingEffortState.availableLevels?.length > 0) {
                updateAvailableThinkingEfforts(recoverData.thinkingEffortState.availableLevels)
              }
              if (Array.isArray(recoverData.commands) && recoverData.commands.length > 0 && availableCommands.value.length === 0) {
                updateCommandState(recoverData.commands)
              }
              if (recoverData.planState && recoverData.planState.entries?.length > 0) {
                updatePlanEntries(recoverData.planState.entries)
              }
              onExtractScheduledTasks(messages.value)
              onRenderUpdate(forceScrollBottom)
              onScrollBottom(forceScrollBottom)
              if (recoverData.running) {
                loading.value = true
                stopMsgCountPolling()
                onConnectStream(currentSessionId.value)
              } else {
                loading.value = false
                startMsgCountPolling()
              }
              // Skip the second fetch — we already have the data
              return
            }
          }
        } else {
          // Recovery request failed (e.g. 403 NoProjectSelected when
          // clawbench_project cookie is missing). Don't silently bail —
          // log the error so it's visible in devtools. If initSessionFromAPI
          // sets currentSessionId later, the normal path below will fetch messages.
          appLog.w(TAG, 'loadHistory recovery failed:', recoverResp.status, recoverResp.statusText)
        }
        // If recovery still yields no session, bail — createSession will handle it
        if (!currentSessionId.value) {
          return
        }
      }
      // Load agents in parallel with the main fetch when not in recovery path
      const agentsPromise = agents.value.length === 0 ? loadAgents() : Promise.resolve()
      const url = `/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${limit}`
      const fetchCtrl = new AbortController()
      const fetchTimer = setTimeout(() => fetchCtrl.abort(), 60000)
      let resp: Response
      try {
        // Fire agents and chat fetch in parallel
        const [, fetchResp] = await Promise.all([
          agentsPromise,
          fetch(url, { signal: fetchCtrl.signal }),
        ])
        resp = fetchResp
      } catch (e) {
        clearTimeout(fetchTimer)
        if (fetchCtrl.signal.aborted) {
          // Timeout — bail without error toast
          return
        }
        throw e
      }
      clearTimeout(fetchTimer)
      // If another loadHistory or switchSession started while we were fetching, discard our results
      if (loadHistorySeq !== mySeq) { return }
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}))
        throw new Error(errData.error || gt('chat.session.requestFailed', { status: resp.status }))
      }
      const data = await resp.json()
      // Re-check after JSON parse (another async boundary)
      if (loadHistorySeq !== mySeq) { return }
      const rawMsgs = data.messages || []

      // Change detection: if skipIfUnchanged and data matches last snapshot, do nothing.
      // Always refresh when session is running (SSE events may have been dropped).
      const newSnapshot = buildMessageSnapshot(rawMsgs)
      if (skipIfUnchanged && newSnapshot === lastMessageSnapshot && !data.running) {
        return
      }
      lastMessageSnapshot = newSnapshot

      // Data has changed (or this is a full load) — apply new data.
      // Preserve expandedTools when only the last message changed (SSE done reload),
      // to avoid collapsing user-expanded tool details and triggering full re-render.
      // Only reset when message count or non-last message identities differ.
      const prevCount = messages.value.length
      const newCount = rawMsgs.length
      const sameCore = prevCount === newCount && prevCount > 0 && rawMsgs.slice(0, -1).every((m: any, i: number) => m.id === messages.value[i]?.id)
      if (!sameCore) {
        expandedTools.value = {}
      }
      // Clear stale blockAskQuestions — after backend converts <ask-question> text blocks
      // to tool_use blocks, old entries keyed by text-block indices would cause duplicate
      // rendering. extractScheduledTasks below will re-populate from current DB state.
      Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
      Object.keys(blockRagResults).forEach(k => delete blockRagResults[k])

      // Replace messages with server data. Pending messages are NOT in
      // messages.value — they live in a separate per-session pendingStore.
      // No need to preserve/re-append pending messages here.
      messages.value = parseMessages(rawMsgs, onParseAssistantContent, messages.value)

      totalMessages.value = data.total || messages.value.length
      // Sanity check: if the backend returned a different sessionId than what we
      // requested, log a warning — this indicates a potential issue (e.g. session
      // was deleted, project mismatch). The sequence guard already prevents stale
      // responses from winning a race, so we still apply the data but log for
      // debugging.
      const requestedId = currentSessionId.value
      const returnedId = data.sessionId || ''
      if (returnedId && requestedId && returnedId !== requestedId) {
        appLog.w(TAG, `loadHistory: session ID mismatch (requested=${requestedId}, returned=${returnedId})`)
      }
      currentSessionId.value = returnedId
      currentSessionTitle.value = data.sessionTitle || ''
      currentBackend.value = data.backend || ''
      currentAgentId.value = data.agentId || ''
      syncModelFromData(currentAgentId.value, data.modelId)
      syncThinkingEffortFromData(data.thinkingEffortState?.currentId || '')
      syncModeFromData(data.modeState?.currentModeId || '', data.modeState?.availableModes)
      syncTransportFromData(data.transport)
      syncUsageFromData(data.usageState)
      // Restore autoApprove from server state (per-session, not global)
      if (data.autoApprove !== undefined) {
        autoApprove.value = data.autoApprove
      }
      // Populate ACP mode available modes from REST response.
      if (data.modeState && data.modeState.availableModes?.length > 0) {
        updateAvailableModes(data.modeState.availableModes)
      }
      // Update available thinking effort levels from ACP state
      if (data.thinkingEffortState && data.thinkingEffortState.availableLevels?.length > 0) {
        updateAvailableThinkingEfforts(data.thinkingEffortState.availableLevels)
      } else if (data.agentId) {
        // Fallback: agent config (e.g. OpenCode/Kimi ACP don't expose thought_level)
        const agentLevels = getAgentThinkingEffortLevels(data.agentId)
        if (agentLevels.length > 0) {
          updateAvailableThinkingEfforts(agentLevels.map((id: string) => ({ id, name: id })))
        }
      }
      // Populate slash commands from REST response (cached ACP state)
      if (Array.isArray(data.commands) && data.commands.length > 0 && availableCommands.value.length === 0) {
        updateCommandState(data.commands)
      }
      // Populate plan state from REST response (cached ACP state)
      // Only set if entries are non-empty to avoid clearing active SSE streaming plan
      if (data.planState && data.planState.entries?.length > 0) {
        updatePlanEntries(data.planState.entries)
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
      // Check if another loadHistory was requested while we were in-flight
      loadHistoryInProgress = false
      if (pendingReload) {
        const next = pendingReload
        pendingReload = null
        // Execute pending load — its completion will resolve the deferred
        setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged), 0)
      } else {
        // No pending load — resolve the deferred so all awaiting callers proceed
        resolveDeferred!()
        loadHistoryDeferred = null
      }
    } catch (err) {
      appLog.e(TAG, 'Failed to load chat history:', err)
      const _msg = err instanceof Error ? err.message : ''
      toast.show(_msg ? gt('chat.session.loadHistoryFailedDetail', { error: _msg }) : gt('chat.session.loadHistoryFailed'), { icon: '⚠️', type: 'error' })
      loadHistoryInProgress = false
      if (pendingReload) {
        const next = pendingReload
        pendingReload = null
        setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged), 0)
      } else {
        resolveDeferred!()
        loadHistoryDeferred = null
      }
    } finally {
      // Safety net: always reset in-flight state and resolve deferred on any
      // exit path (early returns via loadHistorySeq guard, etc.) so callers
      // aren't stuck awaiting and future loadHistory calls aren't blocked.
      loadHistoryInProgress = false
      switching.value = false
      if (loadHistoryDeferred) {
        resolveDeferred!()
        loadHistoryDeferred = null
      }
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
      appLog.e(TAG, 'Failed to load more messages:', err)
    } finally {
      loadingMore.value = false
    }
  }

  async function switchSession(sessionId: string) {
    // Increment sequence counter — if another switch starts before we finish,
    // our results will be discarded (last writer wins)
    const mySeq = ++switchSessionSeq
    // Also bump loadHistorySeq so any in-flight loadHistory results are discarded
    // (switchSession takes priority over stale loadHistory responses)
    ++loadHistorySeq

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
    Object.keys(blockTasks).forEach(k => delete blockTasks[k])
    Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
    Object.keys(blockRagResults).forEach(k => delete blockRagResults[k])
    // Clear ACP state from previous session — will be repopulated by REST response
    // or SSE events only if the new session's agent actually supports ACP.
    clearModeState()
    clearCommandState()
    clearThinkingEffortState()
    clearUsageState()
    autoApprove.value = false
    // Restore original CLI model list in case ACP had overridden it
    const prevAgentId = _currentAgentId.value
    if (prevAgentId) restoreOriginalModels(prevAgentId)
    // Clear plan progress from previous session — will be repopulated by SSE plan_update
    clearPlanState()
    try {
      // Load agents and fetch chat data in parallel — agents are only needed
      // for model name resolution which can happen after the initial render.
      const limit = store.state.chatInitialMessages
      const chatUrl = `/api/ai/chat?session_id=${encodeURIComponent(sessionId)}&limit=${limit}`
      const agentsPromise = agents.value.length === 0 ? loadAgents() : Promise.resolve()
      const [_, resp] = await Promise.all([
        agentsPromise,
        fetch(chatUrl),
      ])
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
      syncThinkingEffortFromData(data.thinkingEffortState?.currentId || '')
      syncModeFromData(data.modeState?.currentModeId || '', data.modeState?.availableModes)
      syncTransportFromData(data.transport)
      syncUsageFromData(data.usageState)
      // Restore autoApprove from server state (per-session, not global)
      if (data.autoApprove !== undefined) {
        autoApprove.value = data.autoApprove
      }
      // Populate ACP mode available modes from REST response.
      if (data.modeState && data.modeState?.availableModes?.length > 0) {
        updateAvailableModes(data.modeState.availableModes)
      }
      // Update available thinking effort levels from ACP state
      if (data.thinkingEffortState && data.thinkingEffortState.availableLevels?.length > 0) {
        updateAvailableThinkingEfforts(data.thinkingEffortState.availableLevels)
      }
      // Populate slash commands from REST response (cached ACP state)
      if (Array.isArray(data.commands) && data.commands.length > 0 && availableCommands.value.length === 0) {
        updateCommandState(data.commands)
      }
      // Populate plan state from REST response (cached ACP state)
      // Only set if entries are non-empty to avoid clearing active SSE streaming plan
      if (data.planState && data.planState.entries?.length > 0) {
        updatePlanEntries(data.planState.entries)
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
      // Fire-and-forget: don't block the switching overlay on this secondary call.
      loadSessionsOnce()
    } catch (err) {
      // If another switch happened, don't touch state
      if (switchSessionSeq !== mySeq) return
      appLog.e(TAG, 'Failed to switch session:', err)
      toast.show(gt('chat.session.switchFailed'), { icon: '⚠️', type: 'error' })
    } finally {
      // Always restore input — switchSession is the only place that locks it,
      // so it must always unlock regardless of success/failure/race.
      // If a newer switch started, it will set inputDisabled=true again immediately.
      inputDisabled.value = false
      switching.value = false
    }
  }

  async function createSession(agentId: string) {
    // Stop msg count polling for the previous session to prevent race
    // conditions — if the polling fires during creation, loadHistory could
    // overwrite the new sessionId and revert to the old session.
    stopMsgCountPolling()
    try {
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
      // Delegate full state transition to switchSession which properly:
      // - Increments loadHistorySeq to invalidate in-flight loadHistory calls
      // - Stops all polling (msg count + HTTP)
      // - Disconnects the SSE stream
      // - Loads history from the backend
      // - Starts appropriate polling for the new session
      // - Calls loadSessionsOnce() to update global state
      await switchSession(data.sessionId)
      // Update session count from creation response and show toast
      const maxCount = store.state.sessionMaxCount
      if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
      toast.show(gt('chat.session.created', { count: data.sessionCount ?? '', max: maxCount }), { icon: '✨', type: 'success', duration: 1500 })
    } catch (err) {
      appLog.e(TAG, 'Failed to create session:', err)
      const _msg = err instanceof Error ? err.message : ''
      toast.show(_msg ? gt('chat.session.createSessionFailedDetail', { error: _msg }) : gt('chat.session.createSessionFailed'), { icon: '⚠️', type: 'error' })
    }
  }

  async function deleteSession(sessionId: string, backend: string) {
    // Prevent concurrent deletes for the same session
    if (deletingSessionIds.value.has(sessionId)) return
    deletingSessionIds.value.add(sessionId)
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
            await switchSession(sessionsData.sessions[0].id)
          } else {
            // No sessions left, create a default one
            await createSession('')
          }
        } else {
          // Deleted a non-current session — refresh global state (chatUnread, chatRunning, runningSessions)
          await loadSessionsOnce()
        }
        const maxCount = store.state.sessionMaxCount
        if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
        toast.show(gt('chat.session.deleted', { count: data.sessionCount ?? '', max: maxCount }), { icon: '🗑️', type: 'success', duration: 2000 })
      } else {
        toast.show(gt('chat.session.deleteFailed'), { icon: '⚠️', type: 'error' })
      }
    } catch (err) {
      appLog.e(TAG, 'Failed to delete session:', err)
      toast.show(gt('chat.session.deleteFailed'), { icon: '⚠️', type: 'error' })
    } finally {
      deletingSessionIds.value.delete(sessionId)
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
    } else if (data.status === 'permission_pending' || data.status === 'permission_resolved') {
      // Permission approval state changed — reload sessions to update dot indicators
      if (sessionEventDebounce) clearTimeout(sessionEventDebounce)
      sessionEventDebounce = setTimeout(() => {
        sessionEventDebounce = null
        loadSessionsOnce()
      }, 300)
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
      appLog.e(TAG, 'Failed to continue from execution:', err)
      toast.show(gt('chat.session.continueFailed'), { icon: '⚠️', type: 'error' })
      return false
    }
  }

  /** Fork the current session — create a new session with copied messages. */
  async function forkSession(sessionId: string): Promise<boolean> {
    try {
      const resp = await fetch('/api/ai/session/fork', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId }),
      })
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}))
        const msgKey = errData.msgKey || ''
        if (resp.status === 409 || msgKey === 'SessionLimitReached') {
          toast.show(gt('chat.session.sessionLimitReached'), { icon: '⚠️', type: 'error' })
        } else {
          toast.show(errData.error || gt('chat.session.forkFailed'), { icon: '⚠️', type: 'error' })
        }
        return false
      }
      const data = await resp.json()
      if (!data.ok || !data.sessionId) {
        toast.show(gt('chat.session.forkFailed'), { icon: '⚠️', type: 'error' })
        return false
      }
      const maxCount = store.state.sessionMaxCount
      if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
      toast.show(gt('chat.session.forked', { count: data.sessionCount ?? '', max: maxCount }), { icon: '🔀', type: 'success', duration: 1500 })
      await switchSession(data.sessionId)
      return true
    } catch (err) {
      appLog.e(TAG, 'Failed to fork session:', err)
      toast.show(gt('chat.session.forkFailed'), { icon: '⚠️', type: 'error' })
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
    forkSession,
    checkContinueSession,
    // Agent helpers — delegate to singleton
    getAgentIcon,
    getAgentName,
  }
}
