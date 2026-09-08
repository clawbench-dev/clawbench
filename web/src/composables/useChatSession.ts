import { ref, computed, type Ref } from 'vue'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useAppForeground, onAppForeground } from '@/composables/useAppForeground'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { appLog } from '@/utils/appLog'

const TAG = 'ChatSession'
import { updateAvailableModes, updateCommandState, updateAvailableThinkingEfforts, clearUsageStateById, updateUsageState, currentAgentId as _currentAgentId, clearSessionIdentity, reconcileRunningSessions } from '@/composables/useSessionIdentity.ts'
import { getRecentSession, clearRecentSession } from '@/composables/useRecentSession'
import { clearPlanState, updatePlanEntries } from '@/composables/usePlanProgress'
import { useAgents, restoreOriginalModels, getAgentThinkingEffortLevels, populateACPStateFromCache } from '@/composables/useAgents'
import { store } from '@/stores/app.ts'
import { buildMessageSnapshot, parseMessages } from '@/utils/chatSessionUtils.ts'
import { forceCleanupStreamingState, type ChatMessage, type ChatMessageAction } from '@/utils/chatStreamUtils.ts'
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
  /** Single write channel for the messages array (chatMessageReducer). */
  dispatch: (action: ChatMessageAction) => void
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
  onDisconnectStream: () => void
  onOpen: () => void
  onStreamDone?: () => void
  /** Defensive fallback: create the streaming placeholder when a running event
   *  arrives while loading is false but the stream_start event hasn't created
   *  one yet (delayed/lost). The placeholder is normally event-driven. */
  onEnsureStreamingPlaceholder?: () => void
}

export function useChatSession(options: UseChatSessionOptions) {
  const {
    currentSessionId,
    messages,
    dispatch,
    loading,
    inputDisabled,
    blockTasks,
    blockAskQuestions,
    expandedTools,
    onParseAssistantContent,
    onExtractScheduledTasks,
    onRenderUpdate,
    onScrollBottom,
    onDisconnectStream,
    onEnsureStreamingPlaceholder,
  } = options

  const toast = useToast()

  // Reliable app foreground/background signal. Completion events must only
  // auto-mark a session read while the user is actually looking at the app —
  // otherwise a session finishing in the background (e.g. Android app paused,
  // floating window active) would clear its unread badge before the floating
  // window ever showed it.
  const { appInForeground } = useAppForeground()
  // True while the global WS is replaying events missed while disconnected.
  // Replayed completions (session finished while the app was backgrounded and
  // the WS was down) must NOT be auto-marked read — the user never saw them.
  const { isReplayingEvents } = useGlobalEvents()

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
    immediate: boolean,
  ): { synced: boolean; keepInputDisabled: boolean } {
    const rawMsgs = (sessionData.messages as Array<Record<string, unknown>> | undefined) || []
    const isRunning = !!sessionData.running
    const isReplayPending = !!sessionData.replayPending

    // ── Session identity guard ──
    // A stale loadHistory response (e.g. for a session the user just switched
    // away from) must NEVER leak into the current session's message list. This
    // is the root cause of the "new session shows old session's messages" bug:
    // the db_load dispatch below previously ran BEFORE this check, so a
    // mismatched response already contaminated messages (and overwrote
    // currentSessionId) by the time the mismatch was merely logged.
    //
    // - Recovery path: currentSessionId is set to the response's sessionId
    //   right before this call, so requestedId === returnedId (no false reject).
    // - Main path: currentSessionId equals the requested session_id, so a
    //   mismatch here means the response is stale — discard it entirely without
    //   touching messages, identity, or the change-detection snapshot.
    const returnedId = (sessionData.sessionId as string) || ''
    const requestedId = currentSessionId.value
    if (returnedId && requestedId && returnedId !== requestedId) {
      appLog.w(TAG, `syncSessionState: rejecting stale response for ${returnedId} (current is ${requestedId})`)
      return { synced: false, keepInputDisabled: false }
    }

    // ── Change detection ──
    const newSnapshot = buildMessageSnapshot(rawMsgs)
    if (skipIfUnchanged && newSnapshot === lastMessageSnapshot && !isRunning) {
      return { synced: false, keepInputDisabled: false } // no change, skip UI refresh
    }
    lastMessageSnapshot = newSnapshot

    // ── Message replacement ──
    const prevCount = messages.value.length
    const newCount = rawMsgs.length
    const sameCore = prevCount === newCount && prevCount > 0 && rawMsgs.slice(0, -1).every((m: Record<string, unknown>, i: number) => m.id === messages.value[i]?.id)
    if (!sameCore) {
      expandedTools.value = {}
    }
    Object.keys(blockAskQuestions).forEach(k => delete blockAskQuestions[k])
    const parsed = parseMessages(rawMsgs, onParseAssistantContent, messages.value, isRunning)

    // Merge DB rows into the current array via the reducer (single write
    // channel). rebuildFromDb rebuilds the array from the authoritative DB
    // snapshot, preserving only the live streaming placeholder, pending queued
    // bubbles and adopted _remote rows — so every loadHistory converges to what
    // an app restart would show (the ActionBar refresh behaves like a restart).
    dispatch({ type: 'db_load', dbMessages: parsed as ChatMessage[] })
    // The loaded-window cursor follows the authoritative DB snapshot: after a
    // db_load the oldest loaded row IS the snapshot's oldest DB row. This
    // update is idempotent for repeated loads of the same window, and a new
    // window (different session / grew) always lands on the correct oldest id.
    // While the snapshot holds no DB rows, the cursor is cleared (nothing
    // loaded to paginate from).
    oldestLoadedId.value = oldestDbId(parsed)
    const prevTotal = totalMessages.value
    totalMessages.value = (sessionData.total as number) || messages.value.length
    queuedCount.value = (sessionData.queuedCount as number) || 0
    // Re-evaluate history existence only when the session actually grew new
    // messages (total increased) or this is a different session. A routine
    // refresh of an already-exhausted history must NOT clear noMoreHistory —
    // otherwise the top scroll would re-fire an empty loadMore after every
    // polling/refresh loadHistory.
    if (totalMessages.value > prevTotal) {
      noMoreHistory.value = false
    }

    // ── Identity sync ──
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
    // The streaming placeholder is data-driven now: rebuildFromDb restores it
    // from the DB streaming=1 row (opening a mid-stream session) and the
    // backend's per-prompt stream_start event creates it for live streams —
    // so no onConnectStream call is needed here.
    let keepInputDisabled = false
    if (isRunning) {
      loading.value = true
      onScrollBottom(forceScrollBottom)
    } else if (isReplayPending) {
      loading.value = true
      if (immediate) keepInputDisabled = true
      else inputDisabled.value = true
      onScrollBottom(forceScrollBottom)
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
  function syncUsageFromData(usageStateData?: { used?: number; size?: number; cost?: number; currency?: string; inputTokens?: number; outputTokens?: number; totalTokens?: number; cachedReadTokens?: number; cachedWriteTokens?: number; thoughtTokens?: number; cacheCreationTokens?: number; cacheHitTokens?: number; cacheMissTokens?: number; credit?: number; usageByCategory?: Record<string, number> }, sessionId?: string) {
    if (usageStateData && (usageStateData.size ?? 0) > 0) {
      updateUsageState(usageStateData.used ?? 0, usageStateData.size ?? 0, usageStateData.cost, usageStateData.currency, sessionId, usageStateData.inputTokens, usageStateData.outputTokens, usageStateData.totalTokens, usageStateData.cachedReadTokens, usageStateData.cachedWriteTokens, usageStateData.thoughtTokens, usageStateData.cacheCreationTokens, usageStateData.cacheHitTokens, usageStateData.cacheMissTokens, usageStateData.credit, usageStateData.usageByCategory)
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
  // Server confirmed there are no older messages (a loadMore returned empty).
  // Once set, hasMore is forced to false so scrolling to the top never fires
  // another loadMore for this session. Reset on switchSession (new session)
  // or when a loadHistory reports the session grew (total increased) — a
  // routine refresh of an already-exhausted history keeps the flag set.
  const noMoreHistory = ref(false)
  // ── Loaded-window state (authoritative pagination cursor) ──
  // The numeric DB id of the OLDEST message currently loaded in the messages
  // array. This is the single source of truth for "how far back have we
  // loaded" — it is explicitly maintained by the loading actions and does NOT
  // derive from the messages array. The array is a view; it can be cleared
  // (session switch), rebuilt (db_load), or mutated by stream events without
  // ever corrupting the pagination cursor.
  //
  // null means no DB history is loaded yet (the array is empty or holds only
  // transient pending/streaming bubbles). While null, loadMore must not fire —
  // an empty array has nothing to paginate from, and sending an empty
  // before_id would make the backend return the most-recent window (a full
  // copy of the history), producing the reported "every message doubled"
  // (AABBCC) bug.
  const oldestLoadedId = ref<number | null>(null)
  // ── Window helpers ──
  // Extract the oldest numeric DB id from a parsed messages snapshot.
  // Ignores transient string-id bubbles (pending/_remote/streaming placeholders)
  // and returns null when there is no DB-backed row.
  function oldestDbId(msgs: Array<{ id?: unknown }>): number | null {
    let oldest: number | null = null
    for (const m of msgs) {
      if (typeof m.id !== 'number') continue
      if (oldest === null || m.id < oldest) oldest = m.id
    }
    return oldest
  }
  // Plan C: compare non-queued loaded messages against non-queued total.
  // The queued messages in the messages array are pending bubbles, not loaded
  // history. Filtering by (pending || queued) — NOT by queueId — keeps the
  // loaded count accurate: every user row now carries a queueId (the backend
  // persists it for direct-sent messages too), so a queueId filter would
  // exclude ALL user messages and hasMore would stay true forever.
  //
  // Root-cause fix: hasMore is gated on oldestLoadedId != null. While no DB
  // history is loaded (session switch just cleared the array, or a brand-new
  // session still holding only transient bubbles), hasMore is false — the top
  // scroll can never fire a loadMore against an empty window.
  const hasMore = computed(() => {
    if (noMoreHistory.value) return false
    if (oldestLoadedId.value === null) return false
    const loaded = messages.value.filter((m) => !m.pending && !m.queued).length
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
  let pendingReload: { forceScrollBottom: boolean; showOverlay: boolean; skipIfUnchanged: boolean; immediate?: boolean } | null = null
  let loadHistoryDeferred: { promise: Promise<void> } | null = null

  // forceScrollBottom: true = always scroll to bottom (switch session, first load)
  //                   false = only scroll if already near bottom (re-open panel, polling)
  // showOverlay: true = show the switching overlay (session switch, first open)
  //            false = silent reload (stream done, polling)
  // skipIfUnchanged: true = when data matches last snapshot, skip UI refresh entirely
  //                (used by polling to avoid collapsing expandedTools / resetting scroll)
  // immediate: true = skip the loadHistoryInProgress queue and execute immediately.
  //             Used by switchSession which must not wait for a stale polling request
  //             to finish. When immediate=true, switching/inputDisabled are set and
  //             restored in loadHistory's finally block (same as switchSession did).
  async function loadHistory(forceScrollBottom = true, showOverlay = false, skipIfUnchanged = false, immediate = false) {
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
        pendingReload = { forceScrollBottom, showOverlay, skipIfUnchanged, immediate }
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
        //
        // Prefer the last opened session for this project (per-project
        // localStorage). If that session no longer exists (404) or belongs to
        // another project (403), drop the stale entry and fall back to the
        // default recovery (GetLatestSessionID by updated_at).
        const storedSessionId = getRecentSession()
        const recoverCtrl = new AbortController()
        const recoverTimer = setTimeout(() => recoverCtrl.abort(), 60000)
        // Load agents in parallel with recovery fetch
        const agentsPromise = agents.value.length === 0 ? loadAgents() : Promise.resolve()
        const doRecover = async (withStored: boolean): Promise<Response | null> => {
          const url = withStored && storedSessionId
            ? `/api/ai/chat?limit=${limit}&view=summary&session_id=${encodeURIComponent(storedSessionId)}`
            : `/api/ai/chat?limit=${limit}&view=summary`
          try {
            return await fetch(url, { signal: recoverCtrl.signal })
          } catch (e) {
            if (recoverCtrl.signal.aborted) {
              // Timeout — bail without error toast, let retry handle it
              return null
            }
            throw e
          }
        }
        let recoverResp = await doRecover(true)
        if (storedSessionId && recoverResp && !recoverResp.ok) {
          // Stored session is gone (404) or no longer in this project (403) —
          // remove it and retry with default logic.
          clearRecentSession()
          recoverResp = await doRecover(false)
        }
        clearTimeout(recoverTimer)
        await agentsPromise
        if (loadHistorySeq !== mySeq) { return }
        if (!recoverResp) {
          // Aborted / timeout — bail, let retry handle it
          return
        }
        if (recoverResp.ok) {
          const recoverData = await recoverResp.json()
          if (loadHistorySeq !== mySeq) { return }
          if (recoverData.sessionId) {
            // Recovery path sets currentSessionId BEFORE calling syncSessionState
            // so the helper can use it for usage cache and stream connection.
            currentSessionId.value = recoverData.sessionId
            const rawMsgs = (recoverData.messages || []) as Array<Record<string, unknown>>
            if (rawMsgs.length > 0) {
              const result = syncSessionState(recoverData, forceScrollBottom, skipIfUnchanged, immediate)
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
        // Cross-project race: a loadHistory for a session that belongs to
        // another project (e.g. a stale request in flight while the project
        // cookie switched back) gets 403 AccessDenied. This is a normal
        // consequence of switching projects — clear the stale sessionId and
        // recover silently instead of showing an error toast.
        if (resp.status === 403 && errData.msgKey === 'AccessDenied' && currentSessionId.value) {
          appLog.w(TAG, 'loadHistory: session belongs to another project, clearing stale sessionId and recovering')
          currentSessionId.value = ''
          loadHistoryInProgress = false
          resolveDeferred!()
          loadHistoryDeferred = null
          const next = pendingReload || { forceScrollBottom, showOverlay, skipIfUnchanged, immediate }
          pendingReload = null
          setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.immediate), 0)
          return
        }
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
          const next = pendingReload || { forceScrollBottom, showOverlay, skipIfUnchanged, immediate }
          pendingReload = null
          setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.immediate), 0)
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
      const result = syncSessionState(data, forceScrollBottom, skipIfUnchanged, immediate)
      keepInputDisabled = result.keepInputDisabled
      if (!result.synced) return // skipIfUnchanged detected no change

      switching.value = false
      // Check if another loadHistory was requested while we were in-flight
      loadHistoryInProgress = false
      if (pendingReload) {
        const next = pendingReload
        pendingReload = null
        // Execute pending load — its completion will resolve the deferred
        setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.immediate || false), 0)
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
        setTimeout(() => loadHistory(next.forceScrollBottom, next.showOverlay, next.skipIfUnchanged, next.immediate || false), 0)
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
      // Cursor-based pagination: the before_id is the oldest loaded DB row id,
      // tracked independently of the messages array (oldestLoadedId). The
      // cursor is never derived from the array, so a cleared/rebuilt array can
      // never yield an empty/string before_id — the exact cause of the
      // reported "every message doubled" (AABBCC) bug (empty before_id made
      // the backend return the most-recent window again).
      const beforeId = oldestLoadedId.value
      if (beforeId === null) {
        return
      }
      const resp = await fetch(`/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${pageSize}&before_id=${beforeId}&view=summary`)
      if (!resp.ok) return
      const data = await resp.json()
      const olderMsgs = parseMessages(data.messages || [], onParseAssistantContent, undefined, data.running)
      if (olderMsgs.length > 0) {
        dispatch({ type: 'prepend_older', olderMsgs: olderMsgs as ChatMessage[] })
        // Advance the loaded-window cursor to the oldest newly-loaded row.
        // After a successful loadMore the window's bottom edge is the oldest
        // DB id among the prepended rows — again independent of the array.
        const newestOldest = oldestDbId(olderMsgs)
        if (newestOldest !== null) {
          oldestLoadedId.value = newestOldest
        }
        totalMessages.value = data.total || totalMessages.value
        // Refresh queuedCount from the latest response (plan C) — it may have
        // changed since the initial load (e.g. messages drained meanwhile).
        if (typeof data.queuedCount === 'number') {
          queuedCount.value = data.queuedCount
        }
        onExtractScheduledTasks(olderMsgs)
        onRenderUpdate(true)
      } else {
        // No older messages returned — the server has confirmed we reached the
        // beginning of history. Record it so hasMore flips to false and future
        // scrolls to the top stop firing empty loadMore requests (previously
        // hasMore stayed true and every top scroll re-triggered the fetch).
        noMoreHistory.value = true
        // Sync the snapshot anyway so queuedCount stays fresh for plan C.
        if (typeof data.total === 'number' && data.total >= 0) {
          totalMessages.value = data.total
        }
        if (typeof data.queuedCount === 'number') {
          queuedCount.value = data.queuedCount
        }
      }
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to load more messages:', err)
    } finally {
      loadingMore.value = false
    }
  }

  /**
   * Explicitly mark a session as read via POST /api/ai/chat/read.
   * Loading history (GET /api/ai/chat) does NOT mark read anymore — that
   * happens only when the user actively opens the session (switchSession),
   * so automatic reloads never clear an unread badge the user hasn't seen.
   */
  async function markSessionRead(sessionId: string, projectPath?: string): Promise<void> {
    if (!sessionId) return
    const params = new URLSearchParams({ session_id: sessionId })
    // When the session's owning project is known (e.g. from the WS
    // session_update event's project_path), pass it explicitly so the backend
    // can verify ownership even when the current cookie project differs —
    // otherwise cross-project sessions opened via notification deep links or
    // the completion popover never get marked read. Omit it when unknown:
    // MarkChatRead falls back to the cookie project, which is correct for
    // same-project switches.
    if (projectPath) {
      params.set('project_path', projectPath)
    }
    const resp = await fetch(`/api/ai/chat/read?${params.toString()}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    })
    if (!resp.ok) {
      appLog.w(TAG, `markSessionRead failed for ${sessionId.slice(0, 12)}: ${resp.status}`)
    }
  }

  // Returning to foreground: if the currently-active session has unread
  // messages, mark it read immediately — the user is looking at it now. This
  // covers the case where the session completed while the app was backgrounded
  // (the completion paths skip mark-read there so the floating window can show
  // the unread badge) and the user returns to it.
  // markSessionRead is idempotent: the backend anchors last_read_at to the
  // newest finalized assistant message, so a session with nothing new just
  // refreshes the anchor harmlessly. loadSessionsOnce (deduped) re-reads the
  // unread state so the session list badge clears without waiting for a WS
  // event round-trip.
  //
  // handleManualRefresh() resyncs the current session's messages on foreground
  // return. This is the belt-and-suspenders path for Android: document.
  // visibilityState is unreliable in the WebView (onPause doesn't reliably
  // flip it to 'hidden'), so the WS may NOT have been disconnected while
  // backgrounded and no clawbench-reconnect event fires on return. Messages
  // produced in the background would then never appear. The native
  // __setAppForeground bridge (authoritative on Android) drives this callback
  // regardless, so the session is always re-synced here.
  //
  // It deliberately uses handleManualRefresh (forceReload=true, the same
  // semantics as the chat refresh button / a cold restart) instead of the
  // lightweight handleWsReconnect (forceReload=false): when the WS stayed
  // connected through the background period the lightweight path can skip the
  // reload (skipIfUnchanged) or race the reconnect, leaving DB-flushed
  // streaming content missing from the UI until the user manually refreshes or
  // cold-restarts the app. A forced authoritative loadHistory always converges
  // the streaming placeholder (rebuildFromDb) to what the server has.
  const removeForegroundReadListener = onAppForeground((fg) => {
    if (!fg) return
    const sid = currentSessionId.value
    if (!sid) return
    markSessionRead(sid).catch(() => {})
    loadSessionsOnce()
    handleManualRefresh().catch(() => {})
  })

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
    dispatch({ type: 'clear' })
    // New session — history existence must be re-evaluated on load.
    noMoreHistory.value = false
    // The loaded-window cursor is part of session identity: clearing the
    // message array must also clear "how far back we loaded". While null,
    // hasMore is false and loadMore cannot fire against the empty window.
    oldestLoadedId.value = null
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
    // - Placeholder restoration for running sessions (rebuildFromDb)
    // immediate=true skips the loadHistoryInProgress queue and
    // handles switching/inputDisabled in its finally block.
    // Session switches always land at the bottom: forceScrollBottom=true.
    await loadHistory(true, true, false, true)

    // Mark the session as read. Loading history (loadHistory → GET /api/ai/chat)
    // no longer marks a session read on the backend — only an explicit user
    // action of opening the session should clear its unread badge. Automatic
    // reloads (WS reconnect refresh, completion-event refresh) hit loadHistory
    // too, so marking read must happen HERE, at the user-intent switch point,
    // and not inside loadHistory itself.
    // Await before loadSessionsOnce so the session list reflects the cleared
    // unread state (chatUnread) — the backend's UpdateLastRead must complete
    // first or loadSessionsOnce reads a stale unread badge.
    await markSessionRead(sessionId).catch(() => {})

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
    dispatch({ type: 'clear' })
    // The loaded-window cursor belongs to the cleared session; reset it so
    // hasMore/loadMore can't act on the stale window during the async gap.
    noMoreHistory.value = false
    oldestLoadedId.value = null
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
  function onSessionEvent(data: { session_id?: string; status?: string; has_new_messages?: boolean; project_path?: string } | undefined) {
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
        // Defensive: create the streaming placeholder now in case the backend's
        // stream_start event is delayed/lost. Normally stream_start follows the
        // running event and would create it; this covers the gap.
        onEnsureStreamingPlaceholder?.()
      }
    } else if (data.status === 'permission_pending' || data.status === 'permission_resolved') {
      // Permission approval state changed — reload sessions to update dot indicators
      if (permissionDebounce) clearTimeout(permissionDebounce)
      permissionDebounce = setTimeout(() => {
        permissionDebounce = null
        loadSessionsOnce()
      }, 300)
    } else if (data.status === 'read') {
      // The session was marked read from another client. This is a pure unread
      // count refresh — it must NOT remove the session from runningSessions
      // (a running session can be read from elsewhere while still streaming).
      loadSessionsOnce()
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
        // Reload from DB to get the final message state. The backend clears
        // in-memory running state before emitting terminal WS events, so a
        // plain reload sees the final state.
        loadHistory(false, false, true).then(() => {
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
        // The user is actively viewing this session, so its execution finishing
        // must clear its unread badge. This is the reliable completion signal
        // even when the chat_stream 'done' event was missed (e.g. WS reconnect
        // delivers a fresh session_update). Loading history (GET /api/ai/chat)
        // no longer marks read — only an explicit mark-as-read call does.
        // Guarded on app foreground + not a replayed event: a completion
        // delivered while the app is paused (Android) must NOT clear the badge
        // — the floating window shows unread sessions only in the background,
        // and a session finishing there is exactly what should surface as
        // unread. Same for events replayed after a reconnect: the user was not
        // watching when it finished, so the badge must survive until opened.
        if ((data.status === 'completed' || data.status === 'cancelled')
            && appInForeground.value && !isReplayingEvents.value) {
          // Pass the session's owning project (from the WS event) so a
          // cross-project session that finished while viewed still gets marked
          // read; without it the backend falls back to the cookie project and
          // rejects ownership for a different project.
          markSessionRead(sid, data.project_path).catch(() => {})
        }
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
   * force:false (WS reconnect) — lightweight, silent:
   * - still running: reload history (the placeholder is restored from the DB
   *   streaming=1 row via rebuildFromDb, or created by the live stream_start
   *   event); no explicit stream connect needed.
   * - finished while disconnected: clean up the stuck loading state, then reload
   *   history with skipIfUnchanged=true.
   * - idle: reload history with skipIfUnchanged=true (no UI churn if unchanged).
   * No switching overlay, no input lock, queued behind any in-flight loadHistory.
   *
   * force:true (manual refresh / foreground return) — always authoritative:
   * - still running: force a history reload (isRunning keeps loading=true, the
   *   placeholder is restored from the DB streaming=1 row).
   * - finished while disconnected: same cleanup, then force reload history.
   * - idle: force reload history with skipIfUnchanged=false so the UI always
   *   re-renders against the latest server state.
   * Shows the switching overlay, locks input, and runs immediately (bypasses
   * the loadHistory in-flight queue) so the user sees the full resync.
   *
   * The loadHistory parameters are derived from force in one place instead of
   * being re-derived inside each branch: force → scrollBottom/showOverlay/
   * immediate=true + skipIfUnchanged=false (authoritative); !force → the
   * opposite (silent). The still-running branches differ only in the log
   * message, so they share a single call site here.
   */
  async function runReload(opts: { force: boolean }) {
    if (!currentSessionId.value) return
    const { force } = opts
    // Refresh runningSessions from the backend so the current-session decision
    // below reflects any change that happened on the server side.
    await loadSessionsOnceInner()
    const source = force ? 'Manual refresh' : 'WS reconnect'
    const scrollBottom = force
    const showOverlay = force
    const skipIfUnchanged = !force
    const immediate = force
    if (loading.value) {
      if (runningSessions.value.has(currentSessionId.value)) {
        // Still running — force a history reload. The isRunning branch keeps
        // loading=true, and the streaming placeholder is restored from the
        // authoritative DB streaming=1 row (rebuildFromDb) — or, for a WS
        // reconnect, re-subscribed via the live stream_start event / the
        // watch(connected) re-subscribe in useChatStream — so the live stream
        // resumes from the full message list. skipIfUnchanged keeps the
        // reconnect path silent (no UI churn when nothing changed).
        appLog.i(TAG, `${source}: session ${currentSessionId.value} still running — reload history`)
        try {
          await loadHistory(scrollBottom, showOverlay, skipIfUnchanged, immediate)
          onRenderUpdate(true)
        } catch {
          loading.value = false
        }
        return
      }
      // AI finished while the user was away — clean up the stuck loading state
      // and reload history. The backend clears in-memory running state before
      // emitting terminal events, so the plain reload sees the final state.
      appLog.w(TAG, `${source}: session ${currentSessionId.value} no longer running — cleaning up stuck loading state`)
      onDisconnectStream()
      forceCleanupStreamingState(messages.value as ChatMessage[], { onRenderNeeded: (f) => onRenderUpdate(f ?? true), onExtractScheduledTasks })
      loading.value = false
      try {
        await loadHistory(scrollBottom, showOverlay, skipIfUnchanged, immediate)
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
        await loadHistory(scrollBottom, showOverlay, skipIfUnchanged, immediate)
        onRenderUpdate(true)
      } catch {
        // Non-critical — keep current view on failure.
      }
    }
  }

  /**
   * Semantic single entry point for "resync the current session against the
   * backend". Both public handlers below just pick the authority level:
   * force=false for silent WS-reconnect resyncs, force=true for user-initiated
   * authoritative refreshes.
   */
  async function syncCurrentSessionOnReconnect(force: boolean) {
    await runReload({ force })
  }

  /**
   * Handle WS reconnection: resync the current session to reflect changes that
   * occurred while disconnected. Lightweight variant — skips UI refresh when
   * the message snapshot is unchanged, and re-subscribes a still-running stream
   * in place.
   */
  const handleWsReconnect = () => syncCurrentSessionOnReconnect(false)

  /**
   * Manual refresh from the chat ActionBar refresh button (and on foreground
   * return). Mirrors the WS reconnect resync flow but ALWAYS forces a
   * loadHistory so every refresh re-renders against the authoritative server
   * state — messages, stream subscription, mode/usage/commands all stay
   * consistent with the backend.
   */
  const handleManualRefresh = () => syncCurrentSessionOnReconnect(true)

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

  /** Rewind/回溯 the current session IN PLACE — truncate its history after the
   *  anchor assistant message, reset the AI-side session (so the next send
   *  starts a fresh ACP session whose first prompt receives the retained
   *  history as injected context), and return the plain text of the first
   *  removed user message for input prefill ('' when none). Nothing is sent.
   *  Unlike forkSession the same session row is kept and no session switch
   *  happens — the message list is reloaded in place. */
  async function rewindSession(sessionId: string, beforeMessageId: number): Promise<string> {
    try {
      const resp = await fetch('/api/ai/session/rewind', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, beforeMessageId }),
      })
      if (!resp.ok) {
        const errData = await resp.json().catch(() => ({}))
        const msgKey = errData.msgKey || ''
        if (resp.status === 400 && msgKey === 'NothingToRewind') {
          toast.show(gt('chat.session.nothingToRewind'), { icon: 'ℹ️', type: 'info' })
        } else {
          toast.show(errData.error || gt('chat.session.rewindFailed'), { icon: '⚠️', type: 'error' })
        }
        return ''
      }
      const data = await resp.json()
      if (!data.ok) {
        toast.show(gt('chat.session.rewindFailed'), { icon: '⚠️', type: 'error' })
        return ''
      }
      // Reload the message list in place (skipIfUnchanged=false forces an
      // authoritative refresh that rebuilds from the truncated DB snapshot).
      // Unlike switchSession this keeps the identity, cookie, WS subscription
      // and input bar — the rewind operates on the current session.
      await loadHistory(false, false, false)
      // The truncating edit clears unread like an explicit open would.
      markSessionRead(sessionId).catch(() => {})
      loadSessionsOnce()
      toast.show(gt('chat.session.rewinded'), { icon: '⏪', type: 'success', duration: 1500 })
      return data.restoredText || ''
    } catch (err: unknown) {
      appLog.e(TAG, 'Failed to rewind session:', err)
      toast.show(gt('chat.session.rewindFailed'), { icon: '⚠️', type: 'error' })
      return ''
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
    oldestLoadedId,
    switching,
    // Operations
    loadHistory,
    loadMoreMessages,
    switchSession,
    markSessionRead,
    createSession,
    archiveSession,
    destroySession,
    onSessionEvent,
    loadSessionsOnce: loadSessionsOnceInner,
    handleWsReconnect,
    handleManualRefresh,
    continueFromExecution,
    forkSession,
    rewindSession,
    checkContinueSession,
    // Unsubscribes the foreground-transition mark-read listener. Must be
    // called when the hosting component unmounts (SPA project switch) so the
    // module-level foregroundListeners array does not grow stale closures.
    removeForegroundReadListener,
    // Agent helpers — delegate to singleton
    getAgentBackend,
    getAgentName,
  }
}
