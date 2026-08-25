import { ref, computed, type Ref } from 'vue'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'ChatSession'
import { updateAvailableModes, updateCommandState, updateAvailableThinkingEfforts, clearUsageStateById, updateUsageState, currentAgentId as _currentAgentId, clearSessionIdentity, reconcileRunningSessions } from '@/composables/useSessionIdentity.ts'
import { clearPlanState, updatePlanEntries } from '@/composables/usePlanProgress'
import { useAgents, restoreOriginalModels, getAgentThinkingEffortLevels, populateACPStateFromCache } from '@/composables/useAgents'
import { store } from '@/stores/app.ts'
import { buildMessageSnapshot, parseMessages } from '@/utils/chatSessionUtils.ts'
import { forceCleanupStreamingState, sortMessages, type ChatMessage } from '@/utils/chatStreamUtils.ts'
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
        const sessions: Array<{ running?: boolean; unreadCount?: number; pendingApproval?: boolean; id: string }> = data.sessions || []
        const unreadCount = sessions.filter(s =>
          (s.unreadCount! > 0 || s.pendingApproval) && s.id !== identity.currentSessionId.value
        ).length
        store.state.chatUnreadCount = unreadCount
        // Update session count for header indicator
        if (typeof data.totalCount === 'number') {
          store.state.sessionCount = data.totalCount
        }
        // Populate runningSessions set from API data (full authoritative list)
        reconcileRunningSessions(sessions, true)
        // Signal any mounted session list (drawer/sidebar) to refresh in real time.
        // loadSessionsOnce is the single funnel for read/complete/archive/delete
        // state refreshes, so bumping the version here keeps the list in sync
        // even for changes that don't emit a WS event (e.g. mark-as-read).
        store.state.sessionListVersion++
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
  messages: Ref<Array<Record<string, unknown>>>
  loading: Ref<boolean>
  inputDisabled: Ref<boolean>
  blockTasks: Record<string, unknown>
  blockAskQuestions: Record<string, unknown>
  expandedTools: Ref<Record<string, boolean>>
  switching?: Ref<boolean>
  onParseAssistantContent: (content: string) => Record<string, unknown>
  onExtractScheduledTasks: (msgs: Array<Record<string, unknown>>) => void
  onRenderUpdate: (forceFull: boolean) => void
  onScrollBottom: (force?: boolean) => void
  onConnectStream: (sessionId: string, options?: { subscribeOnly?: boolean; reuseExistingStreaming?: boolean }) => void
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
    expandedTools,
    onParseAssistantContent,
    onExtractScheduledTasks,
    onRenderUpdate,
    onScrollBottom,
    onConnectStream,
    onDisconnectStream,
  } = options

  const toast = useToast()

  // ── Session state sync helper ──
  // Shared logic for syncing session identity, available modes/commands/plan,
  // change detection, message replacement, and running/replay stream connection.
  // Used by both the recovery path and the main path in loadHistory.
  // Returns { synced, keepInputDisabled }:
  //   synced: true if state was applied (false when skipIfUnchanged detected no change)
  //   keepInputDisabled: true when replayPending requires input to remain locked
  function syncSessionState(
    sessionData: Record<string, unknown>,
    forceScrollBottom: boolean,
    skipIfUnchanged: boolean,
    forceNotRunning: boolean,
    immediate: boolean,
  ): { synced: boolean; keepInputDisabled: boolean } {
    const rawMsgs = (sessionData.messages as Array<Record<string, unknown>> | undefined) || []
    const isRunning = forceNotRunning ? false : !!sessionData.running
    const isReplayPending = !!sessionData.replayPending && !forceNotRunning

    // ── Change detection ──
    const newSnapshot = buildMessageSnapshot(rawMsgs)
    if (skipIfUnchanged && newSnapshot === lastMessageSnapshot && !isRunning) {
      return { synced: false, keepInputDisabled: false } // no change, skip UI refresh
    }
    lastMessageSnapshot = newSnapshot

    // ── Message replacement ──
    const prevCount = messages.value.length
    const newCount = rawMsgs.length
    // Diagnostic: log when messages are being set for a session (helps debug
    // stale-messages-in-new-session issues). Only logs when the message count
    // changes or when a non-empty list replaces an empty one.
    if (newCount > 0 || prevCount > 0) {
      const sid = (sessionData.sessionId as string) || '?'
      const curSid = currentSessionId.value || '?'
      appLog.d(TAG, `syncSessionState: ${prevCount}→${newCount} msgs, curSid=${curSid.slice(0,12)}, respSid=${sid.slice(0,12)}, skip=${skipIfUnchanged}`)
    }
    const sameCore = prevCount === newCount && prevCount > 0 && rawMsgs.slice(0, -1).every((m: Record<string, unknown>, i: number) => m.id === messages.value[i]?.id)
    if (!sameCore) {
      expandedTools.value = {}
    }
    Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
    const prevMessages = messages.value
    const parsed = parseMessages(rawMsgs, onParseAssistantContent, messages.value, isRunning)

    // Adopt DB data into recently-finalized streaming messages (drain-* IDs).
    // When a stream ends, _forceCleanupStreamingState removes the streaming flag
    // but the message still has its ephemeral drain-* id. loadHistory returns the
    // same message with a numeric DB id. Replacing the object changes the v-for
    // key from 'db-drain-*' to 'db-{number}', causing Vue to destroy and
    // recreate the entire ChatMessageItem component tree — a costly DOM rebuild
    // that creates the multi-second lag the user sees between content appearing
    // and the meta bar / file-changes banner showing up.
    //
    // Instead, merge the DB fields into the existing object and reuse it.
    // This keeps the v-for key stable and avoids the DOM rebuild.
    // Match by: same role + createdAt within 5s + the drain-* id pattern.
    // Only match non-streaming assistant messages (streaming=true means the
    // session is still running and loadHistory is reconnecting, not finalizing).
    const drainPrefix = 'drain-'
    const recentlyFinalized = new Map<number, Record<string, unknown>>()
    for (let i = prevMessages.length - 1; i >= 0; i--) {
      const prev = prevMessages[i]
      if (prev.role !== 'assistant') continue
      if (prev.streaming) continue  // still streaming — don't adopt
      const prevId = prev.id
      if (typeof prevId !== 'string' || !prevId.startsWith(drainPrefix)) continue
      recentlyFinalized.set(i, prev)
    }

    // Race guard (adf6c9e6): 'done' now unlocks the UI and runs loadHistory in
    // the background. If the user sends a new message before that loadHistory
    // arrives, a NEW streaming assistant message (drain-*) coexists with the just-
    // finalized one. Adopting DB fields while a stream is live is unsafe: the
    // new message's createdAt can fall inside the 5s tolerance of a DB row and get
    // merged into (clobbered by) the wrong object. Skip the whole merge while any
    // assistant message is currently streaming — the DB identity is adopted by the
    // next loadHistory that runs after the stream ends.
    if (!messages.value.some((m) => m.role === 'assistant' && m.streaming)) {
      for (let pi = 0; pi < parsed.length; pi++) {
        const newMsg = parsed[pi]
        if (newMsg.role !== 'assistant') continue
        if (typeof newMsg.id !== 'number') continue
        const newCreatedAt = newMsg.createdAt as string | undefined
        if (!newCreatedAt) continue
        // Find a matching recently-finalized message
        for (const [prevIdx, prevMsg] of recentlyFinalized) {
          const prevCreatedAt = prevMsg.createdAt as string | undefined
          if (!prevCreatedAt) continue
          // Match by createdAt within 5 seconds tolerance
          const prevTime = new Date(prevCreatedAt).getTime()
          const newTime = new Date(newCreatedAt).getTime()
          if (Math.abs(prevTime - newTime) > 5000) continue
          // Merge DB fields into the existing object to preserve Vue component identity
          prevMsg.id = newMsg.id
          if (newMsg.summary) prevMsg.summary = newMsg.summary
          if (newMsg.summaryCards) prevMsg.summaryCards = newMsg.summaryCards
          if (newMsg.metadata && !prevMsg.metadata) prevMsg.metadata = newMsg.metadata
          // Replace the parsed message with the reused existing object
          parsed[pi] = prevMsg
          recentlyFinalized.delete(prevIdx)
          break
        }
      }
    }

    messages.value = parsed
    // Queued messages are real chat_history rows now (queued=1, queue_id set).
    // Mark them pending so the UI shows a waiting bubble, and preserve the
    // queueId for drain/cancel matching. parseMessages already rebuilt the
    // array from the authoritative DB response, so no appendQueueItems /
    // ghost-pending reconciliation is needed — the rows are the truth.
    //
    // Only queued=true rows are pending. A drained row keeps its queue_id
    // (needed for queue_cancel matching) but has queued=false — it is a normal
    // conversation message and must NOT show a waiting bubble (regression: the
    // old `|| m.queueId` condition marked every completed user message pending).
    for (const m of messages.value as ChatMessage[]) {
      if (m.role === 'user' && m.queued === true) {
        m.pending = true
      }
    }
    sortMessages(messages.value as ChatMessage[])
    totalMessages.value = (sessionData.total as number) || messages.value.length
    queuedCount.value = (sessionData.queuedCount as number) || 0

    // ── Identity sync ──
    const returnedId = (sessionData.sessionId as string) || ''
    const requestedId = currentSessionId.value
    if (returnedId && requestedId && returnedId !== requestedId) {
      appLog.w(TAG, `loadHistory: session ID mismatch (requested=${requestedId}, returned=${returnedId})`)
    }
    currentSessionId.value = returnedId
    currentSessionTitle.value = (sessionData.sessionTitle as string) || ''
    currentBackend.value = (sessionData.backend as string) || ''
    currentAgentId.value = (sessionData.agentId as string) || ''
    syncModelFromData(currentAgentId.value, sessionData.modelId as string)
    syncThinkingEffortFromData((sessionData.thinkingEffortState as Record<string, unknown>)?.currentId as string || '')
    syncModeFromData(
      (sessionData.modeState as Record<string, unknown>)?.currentModeId as string || '',
      (sessionData.modeState as Record<string, unknown>)?.availableModes as Array<{id: string; name: string}> || [],
    )
    syncTransportFromData(sessionData.transport as string)
    syncUsageFromData(sessionData.usageState as { used?: number; size?: number; cost?: number; currency?: string; inputTokens?: number; outputTokens?: number }, returnedId)
    if (sessionData.autoApprove !== undefined) {
      autoApprove.value = sessionData.autoApprove as boolean
    }

    // ── Available modes / thinking / commands / plan ──
    const modeState = sessionData.modeState as Record<string, unknown> | undefined
    if (modeState && (modeState.availableModes as Array<unknown>)?.length > 0) {
      updateAvailableModes(modeState.availableModes as Array<{id: string; name: string}>)
    }
    const thinkingState = sessionData.thinkingEffortState as Record<string, unknown> | undefined
    if (thinkingState && (thinkingState.availableLevels as Array<unknown>)?.length > 0) {
      updateAvailableThinkingEfforts(thinkingState.availableLevels as Array<{id: string; name: string}>)
    } else if (sessionData.agentId) {
      // Fallback: agent config (e.g. OpenCode/Kimi ACP don't expose thought_level)
      const agentLevels = getAgentThinkingEffortLevels(sessionData.agentId as string)
      if (agentLevels.length > 0) {
        updateAvailableThinkingEfforts(agentLevels.map((id: string) => ({ id, name: id })))
      }
    }
    if (Array.isArray(sessionData.commands) && (sessionData.commands as Array<unknown>).length > 0 && availableCommands.value.length === 0) {
      updateCommandState(sessionData.commands as Array<{ name: string; description: string; inputHint?: string }>)
    }
    const planState = sessionData.planState as Record<string, unknown> | undefined
    if (planState && (planState.entries as Array<unknown>)?.length > 0) {
      updatePlanEntries(planState.entries as Array<{ content: string; priority: 'high' | 'medium' | 'low'; status: 'pending' | 'in_progress' | 'completed' }>)
    }

    // ── Scheduled tasks + render ──
    onExtractScheduledTasks(messages.value)
    onRenderUpdate(forceScrollBottom)

    // ── Running / replay / idle ──
    let keepInputDisabled = false
    if (isRunning) {
      loading.value = true
      onScrollBottom(forceScrollBottom)
      // This loadHistory is reconnecting to the SAME live stream (e.g. after a
      // WS reconnect, tab-visibility change, or stream timeout) — NOT starting a
      // new turn. Reuse the existing streaming message so connectStream doesn't
      // finalize it and open a duplicate empty "outputting" segment.
      onConnectStream(currentSessionId.value, { reuseExistingStreaming: true })
    } else if (isReplayPending) {
      loading.value = true
      if (immediate) keepInputDisabled = true
      else inputDisabled.value = true
      onScrollBottom(forceScrollBottom)
      onConnectStream(currentSessionId.value, immediate ? { subscribeOnly: true } : undefined)
    } else {
      loading.value = false
      onScrollBottom(forceScrollBottom)
    }

    return { synced: true, keepInputDisabled }
  }

  // ── Identity refs from singleton ──
  const identity = useSessionIdentity()
  const { currentSessionTitle, currentBackend, currentAgentId, currentModelId, currentModelName, runningSessions, runningSessionsVersion, availableCommands, autoApprove, thinkingEffortState, modeState } = identity

  // ── Agents from singleton ──
  const { agents, loadAgents, getAgentBackend, getAgentName, getAgent, syncModelFromAgent, getAgentModel, agentHeaderTitle: makeAgentTitle, supportsACP } = useAgents()

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
    thinkingEffortState.syncAndFallback(thinkingEffortFromServer, [], currentAgentId.value)
  }

  function syncModeFromData(modeIdFromServer?: string, availableModes?: Array<{id: string; name: string}>) {
    modeState.syncAndFallback(modeIdFromServer || '', availableModes || [], currentAgentId.value)
  }

  // Helper: sync transport from server data
  // Falls back to agent's configured transport, defaulting to 'cli'.
  function syncTransportFromData(transportFromServer?: string) {
    if (transportFromServer) {
      identity.currentTransport.value = transportFromServer
    } else {
      const agent = getAgent(currentAgentId.value)
      identity.currentTransport.value = agent?.transport || (agent?.acpCommand ? 'acp-stdio' : 'cli')
    }
  }

  // Helper: sync usage state from server data.
  // When usageStateData is present and size > 0, update the per-session cache.
  // When missing or size=0, do NOT clear the existing cache entry — it may
  // have been populated by SSE usage_update events for a running session.
  // The backend REST API returns usageState only when an ACP connection exists
  // with cached data; CLI sessions and reaped ACP connections return nil.
  // Clearing on missing data would discard valid SSE-cached values, causing
  // the context progress bar to disappear when switching back to a running session.
  function syncUsageFromData(usageStateData?: { used?: number; size?: number; cost?: number; currency?: string; inputTokens?: number; outputTokens?: number }, sessionId?: string) {
    if (usageStateData && (usageStateData.size ?? 0) > 0) {
      updateUsageState(usageStateData.used ?? 0, usageStateData.size ?? 0, usageStateData.cost, usageStateData.currency, sessionId, usageStateData.inputTokens, usageStateData.outputTokens)
    }
  }

  // Switching state — true while a session switch is in progress (distinct from
  // "loading" which means "AI is generating"). Used to show a fade/placeholder
  // transition so the user sees immediate feedback instead of a frozen UI.
  const switching = ref(false)
  const pendingSessionOps = ref(new Set<string>())

  // Fallback polling timer for WS disconnect

  // Pagination state
  const totalMessages = ref(0)
  // Number of queued (still waiting for the drain loop) messages in this
  // session. They are real DB rows counted in totalMessages, so hasMore must
  // exclude them — a pending bubble is not "loaded history" (plan C).
  const queuedCount = ref(0)
  const loadingMore = ref(false)
  // Plan C: compare non-queued loaded messages against non-queued total.
  // The queued messages in the messages array are pending bubbles, not loaded
  // history. Using filter(!m.queueId) instead of `length - queuedCount` keeps
  // the loaded count accurate even when queuedCount (a server snapshot) drifts
  // from the rows actually present in the messages array.
  const hasMore = computed(() => {
    const loaded = messages.value.filter((m) => !(m as ChatMessage).queueId).length
    return loaded < totalMessages.value - queuedCount.value
  })

  const agentHeaderTitle = computed(() => makeAgentTitle(currentAgentId.value))

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
  let pendingReload: { forceScrollBottom: boolean; showOverlay: boolean; skipIfUnchanged: boolean; forceNotRunning: boolean; immediate?: boolean } | null = null
  let loadHistoryDeferred: { promise: Promise<void> } | null = null

  // forceScrollBottom: true = always scroll to bottom (switch session, first load)
  //                   false = only scroll if already near bottom (re-open panel, polling)
  // showOverlay: true = show the switching overlay (session switch, first open)
  //            false = silent reload (stream done, polling)
  // skipIfUnchanged: true = when data matches last snapshot, skip UI refresh entirely
  //                (used by polling to avoid collapsing expandedTools / resetting scroll)
  // forceNotRunning: true = treat the session as not running even if the server
  //                  says it is (used after session_update completed/cancelled to
  //                  prevent a race where the server's in-memory running state
  //                  hasn't been updated yet, causing loadHistory to re-connect
  //                  the stream and set loading=true again)
  // immediate: true = skip the loadHistoryInProgress queue and execute immediately.
  //             Used by switchSession which must not wait for a stale polling request
  //             to finish. When immediate=true, switching/inputDisabled are set and
  //             restored in loadHistory's finally block (same as switchSession did).
  async function loadHistory(forceScrollBottom = true, showOverlay = false, skipIfUnchanged = false, forceNotRunning = false, immediate = false) {
    // Track whether input should remain disabled after load (replayPending case).
    // Only relevant when immediate=true (switchSession path).
    let keepInputDisabled = false

    // immediate mode: skip the queue, execute directly. Used by switchSession
    // which must not wait for a stale polling loadHistory to finish.
    if (!immediate) {
      // If a load is already in-flight, record the requested params and return
      // a promise that resolves when all queued loads complete. This coalesces
      // rapid calls while ensuring callers can await + .finally() and that the
      // final state is always fresh.
      if (loadHistoryInProgress) {
        pendingReload = { forceScrollBottom, showOverlay, skipIfUnchanged, forceNotRunning, immediate }
        // Return the in-flight load's promise so callers can await/finally it.
        // The pendingReload will be executed after the in-flight load completes.
        return loadHistoryDeferred!.promise
      }
    }
    loadHistoryInProgress = true
    let resolveDeferred: () => void
    loadHistoryDeferred = { promise: new Promise<void>((r) => { resolveDeferred = r }) }

    const mySeq = ++loadHistorySeq
    if (showOverlay || immediate) switching.value = true
    // immediate mode (switchSession): lock input to prevent stale messages
    if (immediate) inputDisabled.value = true
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
          recoverResp = await fetch(`/api/ai/chat?limit=${limit}&view=summary`, { signal: recoverCtrl.signal })
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
            // Recovery path sets currentSessionId BEFORE calling syncSessionState
            // so the helper can use it for usage cache and stream connection.
            currentSessionId.value = recoverData.sessionId
            const rawMsgs = (recoverData.messages || []) as Array<Record<string, unknown>>
            if (rawMsgs.length > 0) {
              const result = syncSessionState(recoverData, forceScrollBottom, skipIfUnchanged, forceNotRunning, immediate)
              keepInputDisabled = result.keepInputDisabled
              if (result.synced) {
                // Skip the second fetch — we already have the data
                return
              }
            }
            // Recovery returned sessionId but no messages — identity is set,
            // fall through to the main fetch to load messages.
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
      const url = `/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${limit}&view=summary`
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
        // If the session was deleted (404 + SessionNotFound), clear stale
        // currentSessionId and recover by re-triggering loadHistory (which
        // will use the recovery path to auto-select the latest available
        // session or create a new one).
        if (resp.status === 404 && errData.msgKey === 'SessionNotFound' && currentSessionId.value) {
          appLog.w(TAG, 'loadHistory: session not found, clearing stale sessionId and recovering')
          currentSessionId.value = ''
          // Resolve deferred and clean up in-flight state before re-invoking
          // so the finally block doesn't double-resolve or flicker switching.
          loadHistoryInProgress = false
          resolveDeferred!()
          loadHistoryDeferred = null
          const next = pendingReload || { forceScrollBottom, showOverlay, skipIfUnchanged, forceNotRunning, immediate }
          pendingReload = null
          setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.forceNotRunning, next.immediate), 0)
          return
        }
        throw new Error(errData.error || gt('chat.session.requestFailed', { status: resp.status }))
      }
      const data = await resp.json()
      // Re-check after JSON parse (another async boundary)
      if (loadHistorySeq !== mySeq) { return }

      // Delegate all state sync to the shared helper.
      // Main path does NOT set currentSessionId before calling — the helper
      // sets it from the response data (returnedId).
      const result = syncSessionState(data, forceScrollBottom, skipIfUnchanged, forceNotRunning, immediate)
      keepInputDisabled = result.keepInputDisabled
      if (!result.synced) return // skipIfUnchanged detected no change

      switching.value = false
      // Check if another loadHistory was requested while we were in-flight
      loadHistoryInProgress = false
      if (pendingReload) {
        const next = pendingReload
        pendingReload = null
        // Execute pending load — its completion will resolve the deferred
        setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.forceNotRunning, next.immediate || false), 0)
      } else {
        // No pending load — resolve the deferred so all awaiting callers proceed
        resolveDeferred!()
        loadHistoryDeferred = null
      }
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to load chat history:', err)
      const _msg = err instanceof Error ? err.message : ''
      toast.show(_msg ? gt('chat.session.loadHistoryFailedDetail', { error: _msg }) : gt('chat.session.loadHistoryFailed'), { icon: '⚠️', type: 'error' })
      loadHistoryInProgress = false
      if (pendingReload) {
        const next = pendingReload
        pendingReload = null
        setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.forceNotRunning, next.immediate || false), 0)
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
      // immediate mode (switchSession path): restore inputDisabled.
      // Same logic as switchSession's old finally block:
      // - If not keepInputDisabled, unlock input
      // - If keepInputDisabled (replayPending), leave input locked — replay_done
      //   WS event will re-enable it later
      // - If a newer switch started, it will set inputDisabled=true again immediately
      if (immediate && !keepInputDisabled) {
        inputDisabled.value = false
      }
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
      const beforeId = (oldestMsg?.id as string) || ''
      const resp = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${pageSize}&before_id=${encodeURIComponent(beforeId)}&view=summary`)
      if (!resp.ok) return
      const data = await resp.json()
      const olderMsgs = parseMessages(data.messages || [], onParseAssistantContent, undefined, data.running)
      if (olderMsgs.length > 0) {
        messages.value = [...olderMsgs, ...messages.value]
        totalMessages.value = data.total || totalMessages.value
        // Refresh queuedCount from the latest response (plan C) — it may have
        // changed since the initial load (e.g. messages drained meanwhile).
        if (typeof data.queuedCount === 'number') {
          queuedCount.value = data.queuedCount
        }
        onExtractScheduledTasks(olderMsgs)
        onRenderUpdate(true)
      }
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to load more messages:', err)
    } finally {
      loadingMore.value = false
    }
  }

  async function switchSession(sessionId: string) {
    // Bump loadHistorySeq so any in-flight loadHistory results are discarded
    // (switchSession takes priority over stale loadHistory responses).
    // loadHistory's own mySeq check handles the actual guard.
    ++loadHistorySeq

    // Disconnect stream and invalidate snapshot before switching identity.
    onDisconnectStream()
    lastMessageSnapshot = ''  // Invalidate snapshot — new session may have different data
    expandedTools.value = {}
    // Start the new session's message list fresh. In-flight (queued/streaming)
    // messages belong to the PREVIOUS session and must not be carried over by
    // syncSessionState's in-flight merge into the new session.
    messages.value = []
    // Clear stale blockAskQuestions from previous session
    Object.keys(blockTasks).forEach(k => delete blockTasks[k])
    Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
    // Restore original CLI model list in case ACP had overridden it
    // Must run BEFORE clearing currentAgentId so the old agent's models
    // can be properly restored.
    const prevAgentId = _currentAgentId.value
    if (prevAgentId) restoreOriginalModels(prevAgentId)
    // Clear all identity refs and set currentSessionId to the target — avoids
    // flashing stale info during the async fetch. Will be repopulated from
    // the REST response. This also clears ACP state (mode/commands/thinking).
    clearSessionIdentity(sessionId)
    // Clear plan progress from previous session — will be repopulated by SSE plan_update
    clearPlanState()

    // Delegate to loadHistory which handles:
    // - Fetch + parseMessages + queue restore (single path, no duplication)
    // - Switching overlay / inputDisabled control
    // - Stream connection for running sessions
    // immediate=true skips the loadHistoryInProgress queue and
    // handles switching/inputDisabled in its finally block.
    await loadHistory(true, true, false, false, true)

    // Recalculate global chatUnread after switching — the backend has already
    // marked this session as read (UpdateLastRead), so the session list will
    // reflect the correct unread state. Without this, chatUnread stays true
    // when the user is already on the chat tab (switchTab early-returns).
    // Fire-and-forget: don't block the switching overlay on this secondary call.
    loadSessionsOnce()
  }

  async function createSession(agentId: string) {
    // Pre-check session limit before clearing identity or making any request.
    // This avoids wiping currentSessionId (which disables the delete button)
    // when we already know creation will fail.
    const maxCount = store.state.sessionMaxCount
    if (maxCount > 0 && store.state.sessionCount >= maxCount) {
      toast.show(gt('chat.session.sessionLimitReached'), { icon: '⚠️', type: 'error' })
      return
    }
    // Immediately clear identity and show switching overlay so the user
    // doesn't see stale info from the previous session during the network
    // round-trip to create the new session.
    switching.value = true
    inputDisabled.value = true
    // Save currentSessionId before clearing — if the POST fails (e.g. TOCTOU
    // race where another client created a session between pre-check and POST),
    // we need to restore it to avoid disabling the delete button.
    const prevSessionId = currentSessionId.value
    // Mirror switchSession's synchronous pre-flight cleanup so the async POST
    // gap doesn't leak stale state (stream events, snapshots, blocks).
    onDisconnectStream()
    lastMessageSnapshot = ''
    expandedTools.value = {}
    Object.keys(blockTasks).forEach(k => delete blockTasks[k])
    Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
    clearSessionIdentity()
    // Bump loadHistorySeq to invalidate any in-flight loadHistory (e.g.
    // polling) so its recovery path cannot re-populate stale messages while
    // currentSessionId is empty. This mirrors switchSession's ++loadHistorySeq.
    ++loadHistorySeq
    // Clear messages immediately so the recovery path in any concurrent
    // loadHistory (e.g. from the active watcher) cannot re-populate stale
    // messages from the previous session via the cookie-based fallback.
    // switchSession also clears messages, but it runs after the async POST —
    // the gap between clearSessionIdentity('') and switchSession is the
    // window where the recovery path can load old messages.
    messages.value = []
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
      // Restore ACP state (mode, thinking effort, commands) from the agent
      // cache so mode/thinking chips appear immediately on new sessions.
      // switchSession clears identity state and the REST response for a
      // brand-new session may have no ACP state yet.
      // This runs after switchSession's finally block, so the UI is already
      // interactive — a failure here is non-critical (SSE will populate later).
      const effectiveAgentId = currentAgentId.value || data.agentId || agentId
      if (effectiveAgentId && supportsACP(effectiveAgentId)) {
        try {
          await populateACPStateFromCache(effectiveAgentId)
        } catch {
          // Non-critical: mode/thinking chips will populate from the first SSE event
          appLog.w(TAG, 'populateACPStateFromCache failed for new session, will rely on SSE')
        }
      }
      // Update session count from creation response and show toast
      if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
      toast.show(gt('chat.session.created', { count: data.sessionCount ?? '', max: maxCount }), { icon: '✨', type: 'success', duration: 1500 })
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to create session:', err)
      const _msg = err instanceof Error ? err.message : ''
      toast.show(_msg ? gt('chat.session.createSessionFailedDetail', { error: _msg }) : gt('chat.session.createSessionFailed'), { icon: '⚠️', type: 'error' })
      // Reset switching/input state on failure — switchSession won't run so
      // its finally block won't fire. On success, switchSession's finally
      // handles the reset.
      switching.value = false
      inputDisabled.value = false
      // Restore sessionId to prevent delete button from being stuck disabled
      // after a rare TOCTOU race (pre-check passed but backend still 409'd).
      if (prevSessionId && !currentSessionId.value) {
        currentSessionId.value = prevSessionId
      }
      // Reload messages for the restored session — messages were cleared
      // above before the async POST, so we need to re-fetch them.
      // showOverlay=false: switching overlay was already reset above.
      if (currentSessionId.value) {
        loadHistory(false, false, false).catch((e) => {
          appLog.w(TAG, 'Failed to reload messages after createSession error:', e)
        })
      }
    }
  }

  async function archiveSession(sessionId: string, backend: string) {
    // Prevent concurrent deletes for the same session
    if (pendingSessionOps.value.has(sessionId)) return
    pendingSessionOps.value.add(sessionId)
    try {
      const resp = await fetch(`/api/ai/session/archive?session_id=${encodeURIComponent(sessionId)}&backend=${encodeURIComponent(backend || '')}`, {
        method: 'DELETE',
      })
      const data = await resp.json()
      if (data.ok) {
        // Evict usage cache for the deleted session
        clearUsageStateById(sessionId)
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
          // Deleted a non-current session — refresh global state (chatUnread, runningSessions)
          await loadSessionsOnce()
        }
        const maxCount = store.state.sessionMaxCount
        if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
        if (data.destroyed) {
          toast.show(gt('chat.session.destroyed'), { icon: '🗑️', type: 'success', duration: 2000 })
        } else {
          toast.show(gt('chat.session.archived', { count: data.sessionCount ?? '', max: maxCount }), { icon: '📦', type: 'success', duration: 2000 })
        }
      } else {
        toast.show(gt('chat.session.archiveFailed'), { icon: '⚠️', type: 'error' })
      }
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to archive session:', err)
      toast.show(gt('chat.session.archiveFailed'), { icon: '⚠️', type: 'error' })
    } finally {
      pendingSessionOps.value.delete(sessionId)
    }
  }

  // Hard-delete (physically destroy) a session and all its associated data.
  // Unlike ArchiveSession, this is irreversible.
  async function destroySession(sessionId: string) {
    if (pendingSessionOps.value.has(sessionId)) return
    pendingSessionOps.value.add(sessionId)
    try {
      const resp = await fetch(`/api/ai/session/destroy?session_id=${encodeURIComponent(sessionId)}`, {
        method: 'DELETE',
      })
      const data = await resp.json()
      if (data.ok) {
        clearUsageStateById(sessionId)
        // After destroying current session, switch to another or create new
        if (sessionId === currentSessionId.value) {
          const sessionsResp = await fetch('/api/ai/sessions')
          const sessionsData = await sessionsResp.json()
          if (sessionsData.sessions && sessionsData.sessions.length > 0) {
            await switchSession(sessionsData.sessions[0].id)
          } else {
            await createSession('')
          }
        } else {
          await loadSessionsOnce()
        }
        if (typeof data.sessionCount === 'number') store.state.sessionCount = data.sessionCount
        toast.show(gt('chat.session.destroyed'), { icon: '🗑️', type: 'success', duration: 2000 })
      } else {
        toast.show(gt('chat.session.destroyFailed'), { icon: '⚠️', type: 'error' })
      }
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to destroy session:', err)
      toast.show(gt('chat.session.destroyFailed'), { icon: '⚠️', type: 'error' })
    } finally {
      pendingSessionOps.value.delete(sessionId)
    }
  }

  // Debounce timers for loadSessionsOnce after session events.
  // Separate timers for permission and completion events to prevent them from
  // cancelling each other (permission needs faster 300ms, completion needs 500ms).
  let permissionDebounce: ReturnType<typeof setTimeout> | null = null
  let completionDebounce: ReturnType<typeof setTimeout> | null = null

  // Called from WS session_update event
  function onSessionEvent(data: { session_id?: string; status?: string; has_new_messages?: boolean } | undefined) {
    if (!data) return
    const sid = data.session_id

    if (data.status === 'running') {
      if (sid) { runningSessions.value.add(sid); runningSessionsVersion.value++ }
      // Recovery: if the current session started running but loading is false,
      // it means we missed the transition (e.g. WS reconnect delivered a stale
      // "completed" before the fresh "running", or drain loop continued). Re-enter
      // streaming state so the UI shows the correct "executing" indicator.
      if (sid === currentSessionId.value && !loading.value) {
        appLog.w(TAG, `session_update running received but loading is false — recovering streaming state`)
        loading.value = true
        onConnectStream(currentSessionId.value, { reuseExistingStreaming: true })
      }
    } else if (data.status === 'permission_pending' || data.status === 'permission_resolved') {
      // Permission approval state changed — reload sessions to update dot indicators
      if (permissionDebounce) clearTimeout(permissionDebounce)
      permissionDebounce = setTimeout(() => {
        permissionDebounce = null
        loadSessionsOnce()
      }, 300)
    } else {
      if (sid) { runningSessions.value.delete(sid); runningSessionsVersion.value++ }
      // Safety net: if the session completed/cancelled but loading is still true,
      // it means the chat_stream 'done'/'cancelled' event was missed or its
      // handler failed (e.g., sessionChanged() guard returned early, or the WS
      // disconnected right as 'done' was sent). Without this, loading.value stays
      // true forever — the input bar shows the stop button and the loading
      // indicator never clears, until the user manually switches sessions.
      // This is the root cause of the "stuck in progress" bug.
      if (sid === currentSessionId.value && loading.value && (data.status === 'completed' || data.status === 'cancelled')) {
        appLog.w(TAG, `session_update ${data.status} received but loading still true — cleaning up stuck loading state`)
        onDisconnectStream()
        forceCleanupStreamingState(messages.value as ChatMessage[], { onRenderNeeded: (f) => onRenderUpdate(f ?? true), onExtractScheduledTasks })
        loading.value = false
        // Reload from DB to get the final message state.
        // forceNotRunning=true prevents a race where the server's in-memory
        // running state hasn't been updated yet, which would cause loadHistory
        // to re-connect the stream and set loading=true again.
        loadHistory(false, false, true, true).then(() => {
          // Re-render Mermaid on the final DOM — loadHistory replaced messages
          // and Vue rebuilt the DOM, destroying any Mermaid SVGs rendered by the
          // earlier forceCleanupStreamingState onRenderUpdate(true) call.
          onRenderUpdate(true)
        })
      }
      // Completed/cancelled current session OR has_new_messages — reload messages.
      // This replaces the old 15s msgCountPolling. skipIfUnchanged=true prevents
      // no-op refreshes when data is already current. loadHistory has built-in
      // dedup (loadHistoryInProgress) so has_new_messages + completed don't double-call.
      if (sid === currentSessionId.value && !loading.value && (data.has_new_messages || data.status === 'completed' || data.status === 'cancelled')) {
        loadHistory(false, false, true)
      }
      // Recalculate chatUnread from backend instead of optimistically setting true.
      // The old code unconditionally set chatUnread=true here, which caused phantom
      // flashing: a session that was already read (last_read_at set) would trigger
      // the flash, and the button kept blinking until loadSessionsOnce() corrected it.
      // Now we debounce-load the real unread state from the server.
      // Both current and non-current session completions need this — the current session
      // completing may clear a stale chatUnreadCount that was set by a prior event,
      // and onStreamEnd may not fire if the stream was disconnected.
      // Note: for the current session, onStreamEnd('done') also calls loadSessionsOnce()
      // immediately — the dedup (_sessionsLoadPromise) ensures no duplicate API call.
      if (sid) {
        if (completionDebounce) clearTimeout(completionDebounce)
        completionDebounce = setTimeout(() => {
          completionDebounce = null
          loadSessionsOnce()
        }, 500)
      }
      // Refresh git state — completed/cancelled session may have modified files
      // or switched branches; needed when user is on a different tab
      store.loadGitBranch().catch(() => {})
    }
  }

  // One-time session list load — delegates to module-level function
  async function loadSessionsOnceInner() {
    await loadSessionsOnce()
  }

  /**
   * Shared resync flow for both the WS reconnect and the manual refresh button.
   * Refreshes runningSessions from the backend, then branches on session state.
   *
   * forceReload:false (WS reconnect) — lightweight, silent:
   * - still running: re-subscribe the stream in place (subscribeOnly), preserving
   *   the existing streaming message; no history fetch.
   * - finished while disconnected: clean up the stuck loading state, then reload
   *   history with skipIfUnchanged=true + forceNotRunning=true.
   * - idle: reload history with skipIfUnchanged=true (no UI churn if unchanged).
   * No switching overlay, no input lock, queued behind any in-flight loadHistory.
   *
   * forceReload:true (manual refresh) — always authoritative:
   * - still running: force a history reload whose syncSessionState isRunning
   *   branch re-subscribes the stream (reuseExistingStreaming).
   * - finished while disconnected: same cleanup, then force reload history.
   * - idle: force reload history with skipIfUnchanged=false so the UI always
   *   re-renders against the latest server state.
   * Shows the switching overlay, locks input, and runs immediately (bypasses
   * the loadHistory in-flight queue) so the user sees the full resync.
   */
  async function syncSessionOnReconnect(forceReload: boolean) {
    if (!currentSessionId.value) return
    // Refresh runningSessions from the backend so the current-session decision
    // below reflects any change that happened on the server side.
    await loadSessionsOnceInner()
    const source = forceReload ? 'Manual refresh' : 'WS reconnect'
    // A manual refresh is always authoritative: it shows the switching overlay,
    // scrolls to bottom, skips the snapshot check (skipIfUnchanged=false), and
    // runs immediately (bypassing the loadHistory in-flight queue). The WS
    // reconnect path stays silent — overlay off, skipIfUnchanged=true, queued.
    const scrollBottom = forceReload
    const showOverlay = forceReload
    const skipIfUnchanged = !forceReload
    const immediate = forceReload
    if (loading.value) {
      if (runningSessions.value.has(currentSessionId.value)) {
        if (forceReload) {
          // Still running — force a history reload. loadHistory's isRunning
          // branch re-subscribes the stream (reuseExistingStreaming) and keeps
          // loading=true, so the live stream is resumed from the authoritative
          // DB state rather than merely re-subscribed in place.
          appLog.i(TAG, `${source}: session ${currentSessionId.value} still running — force reload history + resubscribe stream`)
          try {
            await loadHistory(scrollBottom, showOverlay, skipIfUnchanged, false, immediate)
            onRenderUpdate(true)
          } catch {
            loading.value = false
          }
          return
        }
        // WS reconnect: the live stream re-subscribes on reconnect. The
        // useChatStream watch on `connected` only re-subscribes when its
        // internal isStreaming is still true. If a stream watchdog timeout (or
        // an explicit disconnectStream()) already set isStreaming=false while
        // loading stayed true, that watch never fires and the session is left
        // stuck on the loading spinner with no live stream and no history
        // reload. Explicitly re-subscribe (subscribeOnly, so the existing
        // streaming message is preserved) to guarantee the stream is resumed
        // regardless of isStreaming.
        onConnectStream(currentSessionId.value, { subscribeOnly: true })
        return
      }
      // AI finished while the user was away — clean up the stuck loading state
      // and reload history. forceNotRunning=true prevents loadHistory from
      // re-connecting the stream if the server's in-memory running state
      // hasn't been updated yet.
      appLog.w(TAG, `${source}: session ${currentSessionId.value} no longer running — cleaning up stuck loading state`)
      onDisconnectStream()
      forceCleanupStreamingState(messages.value as ChatMessage[], { onRenderNeeded: (f) => onRenderUpdate(f ?? true), onExtractScheduledTasks })
      loading.value = false
      try {
        await loadHistory(scrollBottom, showOverlay, skipIfUnchanged, true, immediate)
        onRenderUpdate(true)
      } catch {
        loading.value = false
      }
    } else {
      // Session idle — reload history to reflect changes that occurred while
      // disconnected. skipIfUnchanged avoids UI churn when nothing changed;
      // a manual refresh forces the reload (skipIfUnchanged=false) so the UI
      // always re-renders against the latest server state.
      try {
        await loadHistory(scrollBottom, showOverlay, skipIfUnchanged, false, immediate)
        onRenderUpdate(true)
      } catch {
        // Non-critical — keep current view on failure.
      }
    }
  }

  /**
   * Handle WS reconnection: resync the current session to reflect changes that
   * occurred while disconnected. Lightweight variant — skips UI refresh when
   * the message snapshot is unchanged, and re-subscribes a still-running stream
   * in place.
   */
  async function handleWsReconnect() {
    await syncSessionOnReconnect(false)
  }

  /**
   * Manual refresh from the chat ActionBar refresh button. Mirrors the WS
   * reconnect resync flow but ALWAYS forces a loadHistory so every refresh
   * re-renders against the authoritative server state — messages, stream
   * subscription, mode/usage/commands all stay consistent with the backend.
   */
  async function handleManualRefresh() {
    await syncSessionOnReconnect(true)
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
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to continue from execution:', err)
      toast.show(gt('chat.session.continueFailed'), { icon: '⚠️', type: 'error' })
      return false
    }
  }

  /** Fork the current session — create a new session with copied messages.
   *  If beforeMessageId is provided, only messages up to and including that message are copied. */
  async function forkSession(sessionId: string, beforeMessageId?: number, agentId?: string): Promise<boolean> {
    try {
      const body: Record<string, unknown> = { sessionId }
      if (beforeMessageId && beforeMessageId > 0) {
        body.beforeMessageId = beforeMessageId
      }
      if (agentId) {
        body.agentId = agentId
      }
      const resp = await fetch('/api/ai/session/fork', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
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
    } catch (err: unknown) {
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
    queuedCount,
    hasMore,
    loadingMore,
    switching,
    // Operations
    loadHistory,
    loadMoreMessages,
    switchSession,
    createSession,
    archiveSession,
    destroySession,
    onSessionEvent,
    loadSessionsOnce: loadSessionsOnceInner,
    handleWsReconnect,
    handleManualRefresh,
    continueFromExecution,
    forkSession,
    checkContinueSession,
    // Agent helpers — delegate to singleton
    getAgentBackend,
    getAgentName,
  }
}
