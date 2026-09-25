import { onUnmounted, watch, type Ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { StreamFrameScheduler } from '@/utils/streamFrameScheduler'
import { useGlobalEvents } from './useGlobalEvents'
import { gt } from '@/composables/useLocale'
import { updateModeState, updateCommandState, updateThinkingEffortState, currentAgentId, updateUsageState } from './useSessionIdentity'
import { updateACPModelList, applyResolvedModelList } from './useAgents'
import { updatePlanEntries } from './usePlanProgress'
import { FILE_MODIFYING_TOOLS, forceCleanupStreamingState as _forceCleanupStreamingState, findStreamingMsg, messageText, nextClientSeq, untrackInFlightSend, type ChatMessage, type ChatMessageAction, type ContentBlock, type ContentEventData, type ThinkingEventData, type ToolUseEventData, type QueueEventData, type ErrorEventData } from '@/utils/chatStreamUtils.ts'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import type { ChatStreamEventData } from '@/utils/chatStreamUtils.ts'
import { ToolUseWatchdog } from '@/utils/toolUseWatchdog'
import { markCancelRequested, reportCancelRoundTrip } from '@/utils/cancelRoundTrip'
import { addQueued, removeQueued, removeQueuedMany, getQueue } from '@/composables/useMessageQueue.ts'

const TAG = 'ChatStream'

export interface UseChatStreamOptions {
  messages: Ref<ChatMessage[]>
  /** Single write channel for the messages array (chatMessageReducer). */
  dispatch: (action: ChatMessageAction) => void
  currentSessionId: Ref<string>
  currentBackend: Ref<string>
  loading: Ref<boolean>
  onRenderNeeded: (forceFull?: boolean) => void
  onScrollBottom: (force?: boolean) => void
  onLoadHistory: () => Promise<void>
  onMessage: () => void
  onOpen: () => void
  isOpen: Ref<boolean>
  onParseAssistantContent: (content: string) => { blocks: ContentBlock[]; metadata?: Record<string, unknown>; cancelled?: boolean }
  onToast: (msg: string, opts?: { icon?: string; type?: string; duration?: number; onClick?: () => void }) => void
  onNotification: (title: string, opts?: { body?: string; onClick?: () => void }) => void
  onStreamEnd?: (reason: 'done' | 'cancelled' | 'error') => void
  /**
   * Fires at every queue_drain turn boundary for the current session: the
   * previous turn's reply just finalized (stamping its completed_at) and the
   * next queued message is starting. The host decides whether the user was
   * watching and should therefore clear the unread state — the backend cannot
   * know (a queued message may come from another device or an IM push).
   */
  onQueueDrainBoundary?: (sessionId: string) => void
  onFileModified?: (filePath: string) => void
  onExtractScheduledTasks?: (msgs: ChatMessage[]) => void
  onToolResult?: (toolId: string) => void
  onToolUpdate?: (toolId: string) => void
  onReplayDone?: () => void
}

export function useChatStream(options: UseChatStreamOptions) {
  const {
    messages,
    dispatch,
    currentSessionId,
    currentBackend,
    loading,
    onRenderNeeded,
    onScrollBottom,
    onLoadHistory,
    onMessage,
    onOpen,
    isOpen,
    onNotification,
    onStreamEnd,
    onQueueDrainBoundary,
    onFileModified,
    onExtractScheduledTasks,
    onToolResult,
    onToolUpdate,
    onReplayDone,
  } = options

  const renderScheduler = new StreamFrameScheduler()
  // Watchdog for tool_use blocks: a tool that goes silent for TOOL_USE_TIMEOUT_MS
  // is considered stalled and marked done. Restarted on every tool_use progress
  // event so long-running tools aren't falsely marked finished.
  const toolUseWatchdog = new ToolUseWatchdog()
  // Counter for assigning stable _key to thinking blocks during streaming
  let thinkingBlockCounter = 0
  // Whether the WS subscription to a session is live. Persistent: set on session
  // open, cleared on session switch/unmount, re-established on WS reconnect.
  let isSubscribed = false
  let subscribedSessionId: string | null = null

  const TOOL_USE_TIMEOUT_MS = 30000 // 30 seconds without 'done' event = mark as done

  // ── Stream stall watchdog ──
  //
  // A subscription can be silently lost server-side (WS reconnect replaced the
  // connection before its re-subscribe landed, App backgrounded and dropped the
  // StreamHub subscriber, the session list lagged so runReload wrongly tore the
  // subscription down). The run keeps going and the backend keeps writing to the
  // DB, but every content/done event is dropped with `reason=no_subscribers`, so
  // the UI sits on a spinner forever — until a manual refresh reads the DB and
  // reveals the reply was complete long ago.
  //
  // There is no other self-healing path: the frontend has no stream inactivity
  // timeout (an earlier 30s one was removed because a genuinely idle session is
  // normal) and the polling fallback was deleted. This watchdog closes that gap
  // without resurrecting the old "reload on any silence" behaviour:
  //
  //   * It only acts while we believe a turn is in flight (loading=true) AND the
  //     panel is visible — an idle session and a backgrounded tab are untouched.
  //   * "Progress" = ANY event delivered for our session, including housekeeping.
  //     That is deliberate and is the opposite of the backend ACP watchdog, which
  //     had to EXCLUDE housekeeping because its heartbeat kept arriving while the
  //     model was dead. Here the failure being detected is a lost SUBSCRIPTION,
  //     and housekeeping is fanned out through the same HasSubscribers gate as
  //     content — so if the subscription were gone, the heartbeat would be gone
  //     too. Receiving anything therefore proves the transport works and the
  //     silence is the backend's (a long tool/subagent), which must NOT be
  //     "recovered" as if the subscription had dropped.
  //   * The recovery is NON-destructive and idempotent — resubscribe (re-triggers
  //     the server's OnSubscribe → stream_start re-emit) + an authoritative
  //     history reload. It never finalizes the turn, so a long-running tool or
  //     subagent keeps its spinner (unlike the removed timeout, which killed it).
  //     A turn that is legitimately silent for >STREAM_STALL_MS (e.g. a Claude
  //     subagent whose inner events are not forwarded to the parent wire) will
  //     trip the bounded attempts below; that is a few harmless reloads, not a
  //     failure.
  //   * Attempts are bounded per turn so a genuinely hung backend cannot cause an
  //     endless reload loop; the log line is the diagnostic breadcrumb.
  const STREAM_STALL_MS = 120000 // no event at all for 2min => suspect a lost subscription
  const STREAM_STALL_CHECK_MS = 30000
  const MAX_STALL_RECOVERIES = 3

  let lastProgressAt = 0
  let stallRecoveries = 0
  let stallRecoveryInFlight = false
  let stallWatchTimer: ReturnType<typeof setInterval> | null = null

  /** Record that the model produced real progress (resets the stall window). */
  function noteStreamProgress() {
    lastProgressAt = Date.now()
  }

  /** Reset the stall window and the per-turn recovery budget. */
  function resetStallWatch() {
    lastProgressAt = Date.now()
    stallRecoveries = 0
  }

  function checkStreamStall() {
    // Only an in-flight turn on a visible panel can be "stuck". An idle session
    // legitimately produces nothing, and a hidden panel is recovered by the
    // foreground resync instead.
    if (!loading.value || !isOpen.value) return
    if (stallRecoveryInFlight) return
    if (!currentSessionId.value) return
    // First observation of an in-flight turn: start the window from here. Any
    // entry path (send, session opened mid-stream, App foreground) is covered,
    // so no explicit "turn started" hook is needed.
    if (!lastProgressAt) { lastProgressAt = Date.now(); return }
    const silentFor = Date.now() - lastProgressAt
    if (silentFor < STREAM_STALL_MS) return
    if (stallRecoveries >= MAX_STALL_RECOVERIES) {
      // Logged once per turn, then silent: the budget is exhausted, stop
      // hammering. The budget is per-turn and is restored by the next turn's
      // start (see resetStallWatch call sites) — a turn that ends resets it,
      // so a later turn in the same session still gets its own attempts.
      if (stallRecoveries === MAX_STALL_RECOVERIES) {
        stallRecoveries++
        appLog.w(TAG, `stream silent ${silentFor}ms — recovery budget exhausted (${MAX_STALL_RECOVERIES}), giving up until a new turn starts`)
      }
      return
    }
    stallRecoveries++
    // Reset the window BEFORE acting so the next tick cannot re-fire while the
    // reload is still in flight.
    lastProgressAt = Date.now()
    stallRecoveryInFlight = true
    const sid = currentSessionId.value
    appLog.w(TAG, `stream silent ${silentFor}ms with a turn in flight — recovering (attempt ${stallRecoveries}/${MAX_STALL_RECOVERIES}, session=${sid})`)
    // Resubscribe forces the server to re-emit stream_start/state for a run that
    // is still going; the reload converges the UI to the DB if it already ended.
    resubscribe(sid)
    Promise.resolve()
      .then(() => onLoadHistory())
      .catch(() => { /* non-critical: the subscription repair already happened */ })
      .finally(() => { stallRecoveryInFlight = false })
  }

  function startStallWatch() {
    if (stallWatchTimer) return
    stallWatchTimer = setInterval(checkStreamStall, STREAM_STALL_CHECK_MS)
  }

  function stopStallWatch() {
    if (stallWatchTimer) {
      clearInterval(stallWatchTimer)
      stallWatchTimer = null
    }
  }

  // Subagent (task/Agent) tool calls run for minutes inside a child session whose
  // inner events aren't forwarded over ACP, so the outer call legitimately exceeds
  // TOOL_USE_TIMEOUT_MS. Don't kill their spinner with the 30s fallback, otherwise a
  // long-running subagent looks like it already finished.
  const SUBAGENT_TOOL_NAMES = new Set(['task', 'agent'])
  function isSubagentToolName(name?: string): boolean {
    return !!name && SUBAGENT_TOOL_NAMES.has(name.toLowerCase())
  }

  const { onEvent, sendWsMessage, connected, isReplayingEvents } = useGlobalEvents()

  function debouncedRender() {
    // Panel not visible: drop any pending render/scroll — data still accumulates
    // and rendering catches up when the tab becomes active (loadHistory on
    // re-activate). The cancels must stay on this branch: they are what
    // discards work already queued before the panel was hidden.
    if (!isOpen.value) {
      renderScheduler.cancel('render')
      renderScheduler.cancel('scroll')
      return
    }
    // No cancel before scheduling. `schedule` already replaces a pending
    // callback of the same name, and cancelling here had a nasty side effect:
    // when the cancel emptied the queue it also called `cancelAnimationFrame`,
    // so every stream event destroyed the pending frame and requested a new
    // one. At ~180 events/s that was ~2,900 cancel+request round trips per
    // 19s window (measured: 799ms of main-thread self time in
    // `cancelAnimationFrame`) for the same 290 frames. Leaving the frame
    // pending lets the events coalesce into it as intended.
    renderScheduler.schedule('render', onRenderNeeded)
    // Streaming context: content is still arriving, so the viewport should
    // follow even if the container height hasn't grown to the bottom yet.
    renderScheduler.schedule('scroll', () => onScrollBottom(false))
  }

  // ── Subscription (decoupled from streaming state) ──
  // The WS subscription is persistent for the open session: established on
  // session open, torn down on session switch/unmount, re-established on WS
  // reconnect (the backend clears all subscriptions on disconnect). It no
  // longer tracks whether an AI stream is active.

  /** Subscribe to a session's WS events (deduped: same session → no-op). */
  function subscribe(sessionId: string | null) {
    if (!sessionId) return
    if (isSubscribed && subscribedSessionId === sessionId) return
    if (isSubscribed && subscribedSessionId !== sessionId) {
      if (subscribedSessionId) {
        sendWsMessage({ type: 'unsubscribe', session_id: subscribedSessionId })
      }
    }
    sendWsMessage({ type: 'subscribe', session_id: sessionId })
    subscribedSessionId = sessionId
    isSubscribed = true
  }

  /**
   * Re-establish the WS subscription for a session even when the frontend
   * already believes it is subscribed. subscribe() dedupes (same session →
   * no-op), so it cannot repair a subscription the SERVER side dropped — e.g.
   * the backend cleared the StreamHub subscriber when the App-mode WS was
   * disconnected on background, or a connection-replace race wiped it. A fresh
   * `subscribe` message makes the server's OnSubscribe re-emit the running
   * stream's state (stream_start + ACP state), exactly like switching away and
   * back does.
   *
   * If the WS is not OPEN yet (send drops the message silently), the
   * subscribedSessionId/isSubscribed flags are already set, so the existing
   * watch(connected) false→true handler re-sends one subscribe — no pending
   * flag needed.
   */
  function resubscribe(sessionId: string | null) {
    if (!sessionId) return
    if (isSubscribed && subscribedSessionId && subscribedSessionId !== sessionId) {
      sendWsMessage({ type: 'unsubscribe', session_id: subscribedSessionId })
    }
    sendWsMessage({ type: 'subscribe', session_id: sessionId })
    subscribedSessionId = sessionId
    isSubscribed = true
  }

  /** Unsubscribe from the current session (idempotent). */
  function unsubscribe() {
    if (isSubscribed && subscribedSessionId) {
      sendWsMessage({ type: 'unsubscribe', session_id: subscribedSessionId })
    }
    isSubscribed = false
    subscribedSessionId = null
  }

  /** Whether an assistant message carries any renderable content (text/thinking/tool blocks). */
  function messageHasContent(m: ChatMessage): boolean {
    return messageText(m) !== '' || !!m.blocks?.length
  }

  /** Ensure a streaming assistant placeholder exists for the current turn. */
  function ensureStreamingPlaceholder(options?: { reuseExistingStreaming?: boolean }) {
    const existingStreaming = findStreamingMsg(messages.value)

    // A stale streaming message left over from a previous turn (e.g. its
    // 'done' event was missed) must be finalized before we start a new one.
    // Otherwise connectStream would reuse it and keep appending content,
    // echoing all earlier replies into the current reply. Reuse is only
    // legitimate when explicitly opted in (enqueue/reconnect to a live stream).
    //
    // IMPORTANT: only finalize a stale streaming message when it actually
    // carries content. An EMPTY streaming message (no content, no blocks) may
    // be the legitimate placeholder this very turn's stream_start event just
    // created (WS can beat the sendMessage POST response) — finalizing it
    // would strip its streaming flag and then `findStreamingMsg` below fails,
    // so a SECOND placeholder with a drain-* id is created. Stream events
    // (tool_use/thinking/content) then append onto the drain-* message, and
    // clicking a tool on it sends message_id=drain-* → backend 400
    // (InvalidMessageId) → "详情暂不可用". Reusing the empty placeholder is
    // always safe: the stream_start placeholder has no stale content to leak.
    if (existingStreaming && !options?.reuseExistingStreaming) {
      const stale = messageHasContent(existingStreaming)
      if (stale) {
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
      }
    }

    // Ensure a streaming assistant message exists — create one if needed.
    // Ordering is by DB id (the question row was materialized before the reply),
    // so a transient placeholder simply sorts after all DB-backed messages.
    const streaming = findStreamingMsg(messages.value)
    if (!streaming) {
      const newStreaming: ChatMessage = {
        role: 'assistant' as const,
        id: `drain-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        content: '',
        blocks: [] as ContentBlock[],
        streaming: true,
        createdAt: new Date().toISOString(),
        backend: currentBackend.value,
        seq: nextClientSeq(),
      }
      // Single write channel: the reducer pushes + re-sorts.
      dispatch({ type: 'stream_placeholder', msg: newStreaming })
      thinkingBlockCounter = 0
      onRenderNeeded()
    } else if ((streaming as ChatMessage).fromDB) {
      delete (streaming as ChatMessage).fromDB
    }
    onScrollBottom(false)
  }

  /** Stop the active stream state (watchdog/counter) without touching the subscription. */
  function stopStreaming() {
    clearToolUseTimeouts()
    thinkingBlockCounter = 0
    // The turn is over (or being replaced). Clearing the window makes the stall
    // watchdog inert until the next turn records progress — the idle period that
    // follows a completed turn is normal and must never look like a stall. The
    // interval itself keeps running (started once at setup): toggling it per turn
    // would add a lifecycle that has to be kept in sync with every exit path.
    lastProgressAt = 0
    // Restore the recovery budget for the NEXT turn. This is what makes the
    // budget per-turn rather than per-composable: a turn discovered as running
    // via loadHistory (server-side run started while backgrounded, foreground
    // resync) never calls connectStream, so without this reset it would inherit
    // an exhausted budget from a previous turn in the same session — e.g. a
    // subagent silent for minutes burns all 3 attempts — and a genuine
    // subscription loss in the new turn would get zero attempts and never heal.
    stallRecoveries = 0
  }

  function disconnectStream() {
    stopStreaming()
    // Events buffered for a placeholder that never appeared belong to the
    // stream being torn down; keeping them would replay stale content into the
    // next stream this composable attaches to.
    clearBufferedEvents()
    unsubscribe()
  }

  function clearToolUseTimeouts() {
    toolUseWatchdog.clearAll()
  }

  /**
   * Clean up streaming state for the current assistant message.
   * Delegates to the extracted pure function, then handles composable-specific
   * cleanup (tool_use timeouts, loading state).
   */

  function connectStream(sessionId: string, options?: { reuseExistingStreaming?: boolean }) {
    // Stop any previous turn's stream state, then start a fresh one.
    stopStreaming()
    // New turn: start the stall window fresh and restore the recovery budget.
    resetStallWatch()
    // Discard events buffered for the PREVIOUS turn. They are replayed on the
    // next stream_start (the only replay trigger), so leaving them here means
    // that if the previous turn never got a placeholder — the very case the
    // buffer exists for, and one where no terminal event arrives to clear it —
    // its stale content gets replayed into THIS turn's bubble when the new
    // stream_start lands. Same-session consecutive sends are the common path,
    // so this is not a corner case. Session switches already clear the buffer
    // via the watcher; this covers the case the watcher cannot see.
    clearBufferedEvents()
    ensureStreamingPlaceholder(options)
    // Subscribe (deduped) — connectStream now guarantees the subscription
    // exists without tearing it down on stream end.
    subscribe(sessionId)
  }

  // Diagnostic for stream events that arrive but are not applied.
  //
  // These drop points are all legitimate in themselves — events for another
  // session, or for a turn whose placeholder is gone — so they must not be
  // "fixed" by applying the event. But they were silent, which made the two
  // very different causes indistinguishable in logs:
  //   (a) the backend never sent the event, or
  //   (b) it arrived and we discarded it.
  // Only the second is ours to explain, and only while `loading` is true (we
  // believe a turn is in flight) is discarding suspicious — background sessions
  // legitimately have no placeholder here. So: log only in that state.
  const noteDroppedEvent = (eventType: string, reason: string) => {
    if (!loading.value) return
    appLog.w(TAG, `dropped ${eventType} while loading (${reason}) session=${currentSessionId.value}`)
  }

  // Content events that arrived before their placeholder existed.
  //
  // `content` / `thinking` / tool events carry no message id, so the only way to
  // apply them is to find the streaming message. When `stream_start` is delayed
  // or lost, they used to be discarded — the user then saw an empty reply (or a
  // reply missing its first chunks) even though the backend produced it.
  //
  // They are buffered instead and replayed once the placeholder exists. The
  // buffer is bounded: if no placeholder ever appears (a genuinely lost
  // stream_start), the run's events must not accumulate without limit.
  type BufferedStreamEvent = { sessionId: string; eventType: string; payload: Record<string, unknown> }
  const MAX_BUFFERED_EVENTS = 200
  let pendingStreamEvents: BufferedStreamEvent[] = []
  let isReplayingBuffered = false

  const bufferEvent = (sessionId: string, eventType: string, payload: Record<string, unknown>) => {
    if (pendingStreamEvents.length >= MAX_BUFFERED_EVENTS) {
      // Drop the oldest: the newest events are what the user is about to see,
      // and an unbounded buffer would leak for a stream that never starts.
      pendingStreamEvents.shift()
      appLog.w(TAG, `stream event buffer full (${MAX_BUFFERED_EVENTS}), dropped oldest before replay`)
    }
    pendingStreamEvents.push({ sessionId, eventType, payload })
  }

  // Buffered events belong to one session's in-flight turn, so they are
  // meaningless after a switch — and replaying them into another session's
  // placeholder would corrupt it.
  const clearBufferedEvents = () => {
    pendingStreamEvents = []
  }

  // Replay buffered events, in arrival order, once a placeholder exists.
  //
  // Re-invoking the same handler keeps ONE code path for applying an event, so
  // replay cannot drift from live handling. The buffer is cleared first: an
  // event that still cannot be applied must not be re-buffered and looped.
  const replayBufferedEvents = () => {
    if (pendingStreamEvents.length === 0 || isReplayingBuffered) return
    if (!findStreamingMsg(messages.value)) return

    const queued = pendingStreamEvents
    pendingStreamEvents = []
    isReplayingBuffered = true
    try {
      appLog.i(TAG, `replaying ${queued.length} buffered stream event(s) after placeholder appeared`)
      for (const item of queued) {
        handleChatStreamEvent('chat_stream', {
          session_id: item.sessionId,
          event_type: item.eventType,
          payload: item.payload,
        })
      }
    } finally {
      isReplayingBuffered = false
    }
  }

  // ── WS event handler for chat_stream events ──
  // All 21+ event types from the backend are dispatched through this single
  // function. It is named (not an inline arrow) because replaying buffered
  // events re-enters it, so live and replayed events share one apply path.
  function handleChatStreamEvent(event: string, data: unknown) {
    if (event !== 'chat_stream') return
    const csData = data as ChatStreamEventData
    if (csData.session_id !== currentSessionId.value) {
      // Not our session: dropping is correct, but record it when this session
      // is mid-stream — that combination is what a lost/mismatched subscription
      // looks like from here.
      noteDroppedEvent(String(csData.event_type), `session mismatch (got ${csData.session_id})`)
      return
    }

    const sessionId = csData.session_id
    const payload = csData.payload as Record<string, unknown>
    const sessionChanged = () => currentSessionId.value !== sessionId

    // Any event for OUR session proves the subscription is alive — including one
    // we end up discarding (e.g. a tool event for a placeholder that is gone).
    //
    // This watchdog detects a LOST SUBSCRIPTION, not a slow model, so the
    // progress signal is deliberately the opposite of the backend ACP watchdog's:
    // there, housekeeping notifications had to be EXCLUDED because they kept
    // arriving while the model was dead, blinding the check. Here they are
    // exactly the right evidence — housekeeping is fanned out through the same
    // `HasSubscribers` gate as content, so if a subscription were lost, the
    // heartbeat would be dropped too and NOTHING would arrive. Receiving anything
    // therefore means the transport is fine and the silence is the backend's
    // doing (a long tool / subagent), which must not be "recovered".
    if (!sessionChanged()) {
      noteStreamProgress()
    }

    switch (csData.event_type) {
      case 'stream_start': {
        if (sessionChanged()) return
        const messageId = payload.message_id as number | undefined
        if (messageId) {
          // Event-driven placeholder: if no streaming assistant message exists
          // (e.g. client opened the session mid-stream, or the optimistic
          // placeholder was dropped by a loadHistory), create one. The DB row id
          // is used as the message id so subsequent content events
          // (findStreamingMsg) match it. Ordering is by DB id, so no anchor is
          // needed: the user row was materialized before this placeholder.
          if (!findStreamingMsg(messages.value)) {
            dispatch({ type: 'stream_placeholder', msg: {
              role: 'assistant',
              id: messageId,
              content: '',
              blocks: [] as ContentBlock[],
              streaming: true,
              createdAt: new Date().toISOString(),
              backend: currentBackend.value,
              seq: nextClientSeq(),
            } as ChatMessage })
            onRenderNeeded()
            onScrollBottom(false)
          }
          // ws_stream_start is idempotent: it re-sets the id on the existing
          // streaming message (a no-op when the placeholder above already
          // carries the DB id).
          dispatch({ type: 'ws_stream_start', messageId })
        }
        // A placeholder now exists, so anything that arrived before it can be
        // applied. Done after the dispatch above so the replayed events land on
        // the real placeholder rather than being dropped again.
        replayBufferedEvents()
        break
      }

      case 'stream_split': {
        if (sessionChanged()) return
        const messageId = payload.message_id as number | undefined
        if (!messageId) break
        // The assistant reply was split in two at a mid-turn injection point.
        // The reducer finalizes the current bubble and pushes the new "after"
        // bubble; the injected question sits between them by DB id (it was
        // materialized at injection time).
        dispatch({ type: 'ws_stream_split', messageId })
        onRenderNeeded()
        onScrollBottom(false)
        break
      }

      case 'content_reset': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { noteDroppedEvent('content_reset', 'no streaming placeholder'); return }
        dispatch({ type: 'ws_content_reset' })
        onRenderNeeded()
        break
      }

      case 'content': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { bufferEvent(sessionId, 'content', payload); noteDroppedEvent('content', 'buffered until placeholder'); return }
        const contentData = payload as unknown as ContentEventData
        dispatch({ type: 'ws_content', text: contentData.content ?? '', parentToolCallId: contentData.parent_tool_call_id })
        debouncedRender()
        break
      }

      case 'thinking': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { bufferEvent(sessionId, 'thinking', payload); noteDroppedEvent('thinking', 'buffered until placeholder'); return }
        const thinkingData = payload as unknown as ThinkingEventData
        dispatch({ type: 'ws_thinking', text: thinkingData.text ?? '', key: `thinking-${thinkingBlockCounter++}`, parentToolCallId: thinkingData.parent_tool_call_id })
        // debouncedRender schedules the scroll pin in the same rAF — no
        // separate onScrollBottom here (duplicate pin in the same frame).
        debouncedRender()
        break
      }

      case 'thinking_done': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { bufferEvent(sessionId, 'thinking_done', payload); noteDroppedEvent('thinking_done', 'buffered until placeholder'); return }
        dispatch({ type: 'ws_thinking_done' })
        onRenderNeeded()
        break
      }

      case 'tool_use': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { bufferEvent(sessionId, 'tool_use', payload); noteDroppedEvent('tool_use', 'buffered until placeholder'); return }
        const data = payload as unknown as ToolUseEventData
        dispatch({ type: 'ws_tool_use', data })
        // Side effects that depend on the block's updated state.
        const smAfter = findStreamingMsg(messages.value)
        const blocksAfter = smAfter?.blocks || []
        const existing = blocksAfter.find((b) => b.type === 'tool_use' && b.id === data.id)
        if (data.done) {
          toolUseWatchdog.clear(data.id!)
          if (data.name && FILE_MODIFYING_TOOLS.has(data.name) && onFileModified) {
            const filePath = data.file_path || existing?.file_path
            if (filePath) {
              onFileModified(filePath)
            }
          }
        } else if (!data.done) {
          // Progress event: reset the stall watchdog so long-running tools
          // that keep emitting updates are never falsely marked done.
          if (data.name !== 'PermissionApproval' && !isSubagentToolName(data.name)) {
            const block = existing || blocksAfter[blocksAfter.length - 1]
            toolUseWatchdog.start(data.id!, TOOL_USE_TIMEOUT_MS, () => {
              if (block && !block.done) {
                appLog.w(TAG, `tool_use block ${data.id} stalled without 'done' for ${TOOL_USE_TIMEOUT_MS}ms, marking as done`)
                block.done = true
                onRenderNeeded()
              }
            })
          }
        }
        if (onToolUpdate && data.id) {
          onToolUpdate(data.id)
        }
        if (isOpen.value) {
          onScrollBottom(false)
        }
        break
      }

      case 'tool_result': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { bufferEvent(sessionId, 'tool_result', payload); noteDroppedEvent('tool_result', 'buffered until placeholder'); return }
        const data = payload as unknown as ToolUseEventData
        dispatch({ type: 'ws_tool_result', data })
        toolUseWatchdog.clear(data.id!)
        onRenderNeeded()
        if (onToolResult && data.id) {
          onToolResult(data.id)
        }
        if (isOpen.value) {
          onScrollBottom(false)
        }
        break
      }

      case 'metadata': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { bufferEvent(sessionId, 'metadata', payload); noteDroppedEvent('metadata', 'buffered until placeholder'); return }
        dispatch({ type: 'ws_metadata', metadata: payload as Record<string, unknown> })
        break
      }

      case 'done': {
        if (sessionChanged()) return
        // The turn is over. Anything still buffered belongs to a placeholder
        // that never appeared; replaying it later (e.g. on a reconnect's stale
        // stream_start) would build a zombie streaming bubble that never
        // receives another terminal event.
        clearBufferedEvents()
        stopStreaming()

        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })

        // Unlock input bar and fire stream-end callbacks immediately so the user
        // sees the final state (meta bar, file-changes banner, summary toggle)
        // without waiting for the loadHistory REST round-trip. Previously these
        // were in the .finally() of onLoadHistory(), which meant the UI stayed
        // in a limbo state (streaming indicator gone but meta bar not yet shown)
        // for 50-500ms+ while the REST call completed and the message array was
        // replaced. Moving them here eliminates that perceived lag.
        loading.value = false
        onMessage()
        reportCancelRoundTrip('done')
        if (isOpen.value) {
          onScrollBottom(false)
        }
        onStreamEnd?.('done')
        if (!isOpen.value) {
          const lastMsg = messages.value[messages.value.length - 1]
          if (lastMsg?.role === 'assistant') {
            // In-app toast bubble removed — the completion popover now covers
            // this case (shown when the chat view is not in the foreground).
            // Keep the system notification for when the app is backgrounded.
            onNotification(gt('chat.stream.aiReplied'), {
              body: gt('chat.stream.clickToViewReply'),
              onClick: () => onOpen()
            })
          }
        }

        // Sync messages from DB in the background. loadHistory replaces the
        // entire messages array (DB IDs replace drain-* keys, summary is
        // populated, etc.) but this is a non-urgent consistency refresh — the
        // user already sees the correct final state from forceCleanupStreamingState.
        onLoadHistory().then(() => {
          // Re-render Mermaid on the final DOM — loadHistory replaced messages
          // and Vue rebuilt the DOM, destroying any Mermaid SVGs rendered by the
          // earlier forceCleanupStreamingState onRenderNeeded(true) call.
          onRenderNeeded(true)
        }).catch(() => {
          // Non-critical: loadHistory has its own error handling (toast).
          // UI already finalized by forceCleanupStreamingState above.
        })
        break
      }

      case 'replay_done': {
        if (sessionChanged()) return
        appLog.i(TAG, '[replay_done] LoadSession replay completed, reloading history from DB')
        stopStreaming()
        onReplayDone?.()
        // Unlock input immediately — don't wait for loadHistory REST round-trip.
        loading.value = false
        if (isOpen.value) {
          onScrollBottom(false)
        }
        // Sync from DB in the background.
        onLoadHistory().then(() => {
          // Force full render — loadHistory replaced DOM, Mermaid needs re-render
          onRenderNeeded(true)
        }).catch(() => {
          // Non-critical: UI already finalized.
        })
        break
      }

      case 'cancelled': {
        if (sessionChanged()) return
        // Terminal: discard anything buffered for a placeholder that never came.
        clearBufferedEvents()
        // Do NOT bail out when no streaming placeholder is found. The terminal
        // event's job is to end the turn; the placeholder is only an optional
        // artifact of it. Returning early here left loading.value = true
        // forever — the stop button stayed armed and the loading indicator
        // never cleared until the user switched sessions. forceCleanupStreamingState
        // already tolerates a missing placeholder, and 'done'/'error' have no
        // such guard, so this path must not either.
        const sm = findStreamingMsg(messages.value)
        stopStreaming()
        if (sm) sm.cancelled = true
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
        loading.value = false
        reportCancelRoundTrip('cancelled')
        onStreamEnd?.('cancelled')
        break
      }

      case 'error': {
        if (sessionChanged()) return
        // Terminal: discard anything buffered for a placeholder that never came.
        clearBufferedEvents()
        stopStreaming()
        const errorData = payload as unknown as ErrorEventData
        // Set the error block via the reducer's single write channel so the UI
        // updates immediately. The reducer attaches it to the live streaming
        // assistant, or to the last assistant when the stream already ended
        // (backend crash after done) — so the user never needs a reload to see
        // it.
        dispatch({ type: 'ws_error', text: errorData?.error || 'Unknown error', reason: errorData?.reason, errorCode: errorData?.error_code, httpStatus: errorData?.http_status, errorSource: errorData?.error_source, errorDetail: errorData?.error_detail })
        _forceCleanupStreamingState(messages.value, { onRenderNeeded, onExtractScheduledTasks })
        loading.value = false
        onStreamEnd?.('error')
        // Sync from DB in the background (same pattern as 'done' handler).
        onLoadHistory().then(() => {
          onRenderNeeded(true)
        }).catch(() => {
          // Non-critical: error block already displayed.
        })
        break
      }

      case 'warning': {
        if (sessionChanged()) return
        if (!findStreamingMsg(messages.value)) { noteDroppedEvent('warning', 'no streaming placeholder'); return }
        const warningData = payload as { text?: string; reason?: string; error_code?: number; http_status?: number; error_source?: string; error_detail?: string }
        dispatch({ type: 'ws_warning', text: warningData.text || '', reason: warningData.reason, errorCode: warningData.error_code, httpStatus: warningData.http_status, errorSource: warningData.error_source, errorDetail: warningData.error_detail })
        if (isOpen.value) {
          onRenderNeeded()
        }
        break
      }

      case 'mode_update': {
        if (sessionChanged()) return
        const modeData = payload as Record<string, unknown>
        if (modeData.currentModeId || (modeData.availableModes as unknown[])?.length > 0) {
          updateModeState(modeData.currentModeId as string || '', (modeData.availableModes || []) as { id: string; name: string }[])
        }
        break
      }

      case 'config_update': {
        if (sessionChanged()) return
        const configData = payload as Record<string, unknown>
        for (const opt of (configData.options as Record<string, unknown>[] || [])) {
          if ((opt.category as string) === 'mode' || (opt.id as string) === 'mode') {
            const modes = ((opt.values as Record<string, string>[]) || []).map((v) => ({ id: v.id, name: v.name || v.id }))
            const currentModeId = (configData.currentValueId as string) || ''
            if (currentModeId || modes.length > 0) {
              updateModeState(currentModeId, modes)
            }
          }
          if ((opt.category as string) === 'thought_level' || (opt.id as string) === 'thought_level') {
            const levels = ((opt.values as Record<string, string>[]) || []).map((v) => ({ id: v.id, name: v.name || v.id }))
            const currentId = (configData.currentValueId as string) || ''
            if (currentId || levels.length > 0) {
              updateThinkingEffortState(currentId, levels)
            }
          }
        }
        break
      }

      case 'thinking_effort_update': {
        if (sessionChanged()) return
        const effortData = payload as Record<string, unknown>
        if (effortData.currentId || (effortData.availableLevels as unknown[])?.length > 0) {
          const levels = ((effortData.availableLevels as Record<string, string>[]) || []).map((l) => ({ id: l.id, name: l.name || l.id }))
          const currentId = (effortData.currentId as string) || ''
          updateThinkingEffortState(currentId, levels)
        }
        break
      }

      case 'commands_update': {
        if (sessionChanged()) return
        const cmdData = payload as { commands?: unknown[] }
        if (Array.isArray(cmdData.commands)) {
          updateCommandState(cmdData.commands as { name: string; description: string; inputHint?: string }[])
        }
        break
      }

      case 'model_list_update': {
        if (sessionChanged()) return
        const mlData = payload as {
          models?: unknown[]
          currentModelId?: string
          resolvedModels?: unknown[]
          cliModels?: unknown[]
        }
        const aid = currentAgentId.value
        if (!aid) break
        // The backend resolves the CLI and ACP lists and sends both, so prefer
        // the resolved list. Falling back to the raw ACP list keeps older
        // backends (or an emit path without a bound agent) working.
        if (Array.isArray(mlData.resolvedModels) && mlData.resolvedModels.length > 0) {
          applyResolvedModelList(
            aid,
            mlData.resolvedModels as Array<{ id: string; name: string; default: boolean }>,
            mlData.cliModels as Array<{ id: string; name: string; default: boolean }> | undefined,
          )
        } else if (Array.isArray(mlData.models) && mlData.models.length > 0) {
          updateACPModelList(aid, mlData.models as { id: string; name: string }[], mlData.currentModelId)
        }
        break
      }

      case 'plan_update': {
        if (sessionChanged()) return
        const planData = payload as { entries?: unknown[] }
        if (Array.isArray(planData.entries)) {
          updatePlanEntries(planData.entries as import('@/composables/usePlanProgress').PlanEntry[])
        }
        break
      }

      case 'usage_update': {
        if (sessionChanged()) return
        const usageData = payload as { size?: number; used?: number; cost?: number; currency?: string; inputTokens?: number; outputTokens?: number; totalTokens?: number; cachedReadTokens?: number; cachedWriteTokens?: number; thoughtTokens?: number; cacheCreationTokens?: number; cacheHitTokens?: number; cacheMissTokens?: number; credit?: number; usageByCategory?: Record<string, number> }
        if ((usageData.size ?? 0) > 0) {
          updateUsageState(usageData.used ?? 0, usageData.size!, usageData.cost, usageData.currency, sessionId, usageData.inputTokens, usageData.outputTokens, usageData.totalTokens, usageData.cachedReadTokens, usageData.cachedWriteTokens, usageData.thoughtTokens, usageData.cacheCreationTokens, usageData.cacheHitTokens, usageData.cacheMissTokens, usageData.credit, usageData.usageByCategory)
        }
        break
      }

      case 'user_message': {
        if (sessionChanged()) return
        const userData = payload as { messageId?: number; content?: string; files?: FileEntry[]; senderClientId?: string; queueId?: string }

        // A message whose queueId is still in the queue is being MATERIALIZED
        // right now: drop the queue entry and render it inline. This covers the
        // sender too — its optimistic bubble lives in the queue store, not the
        // message list, so the usual self-echo skip would hide it.
        const wasQueued = !!userData.queueId && getQueue(sessionId).some((m) => m.queueId === userData.queueId)
        if (wasQueued) {
          // removeQueued also releases the entry's in-flight guard.
          removeQueued(sessionId, userData.queueId!)
        } else {
          // Direct send: this device already has the bubble (adopted from the
          // POST response), so its echo is skipped.
          const myClientId = localStorage.getItem('clawbench_client_id')
          if (userData.senderClientId && userData.senderClientId === myClientId) break
        }

        // Strip senderClientId when we decided to render: the reducer has its
        // own self-echo guard, and a queue-originated echo from this device must
        // still be shown.
        dispatch({ type: 'ws_user_message', data: { ...userData, senderClientId: undefined, backend: currentBackend.value } })

        // debouncedRender schedules the scroll pin in the same rAF — no
        // separate onScrollBottom here (duplicate pin in the same frame).
        debouncedRender()
        break
      }

      case 'queue_added': {
        // A message was enqueued (by this device or another). It has no
        // chat_history row yet, so it goes to the queue panel, not the list.
        const addedData = payload as { queueId?: string; text?: string; files?: FileEntry[]; senderClientId?: string }
        const myClientId = localStorage.getItem('clawbench_client_id')
        if (addedData.senderClientId && addedData.senderClientId === myClientId) break
        if (addedData.queueId) {
          addQueued(sessionId, { queueId: addedData.queueId, text: addedData.text || '', files: addedData.files || [] })
        }
        break
      }

      case 'queue_drain': {
        // A queued message started its OWN turn. The real user message arrived
        // in the preceding user_message event (content lives only there), so
        // this is just the turn boundary: drop the entry from the queue panel
        // and make sure a streaming placeholder exists for the reply.
        const drainData = payload as unknown as QueueEventData
        const eventSessionId = drainData.sessionId || sessionId
        if (eventSessionId !== currentSessionId.value) break
        if (drainData.queueId) removeQueued(eventSessionId, drainData.queueId)
        // Finalize the reply that was streaming: the backend emits `done` only
        // when the whole drain loop exits, so without this the previous bubble
        // would keep its spinner while the next turn streams into it.
        dispatch({ type: 'ws_queue_drain' })
        // Turn boundary: the previous reply just finalized (its completed_at is
        // now stamped past last_read_at), so the session would count as unread
        // for the whole duration of the next turn even though the user may be
        // watching. Let the host re-anchor last_read_at when it can prove the
        // user is present. Gated on a live (non-replayed) event: a replayed
        // drain means the user was disconnected, so the badge must survive.
        if (!isReplayingEvents.value) {
          onQueueDrainBoundary?.(eventSessionId)
        }
        // The new turn's placeholder is normally created by the stream_start
        // that follows. Ensure one exists anyway so a lost stream_start still
        // renders the reply.
        ensureStreamingPlaceholder()
        onExtractScheduledTasks?.(messages.value)
        if (isOpen.value) {
          onRenderNeeded()
          onScrollBottom(false)
        }
        break
      }

      case 'queue_inject': {
        // A queued message joined the RUNNING turn. Unlike queue_drain this must
        // NOT open a new assistant placeholder — the reply in flight continues
        // (the steer boundary splits it if the backend supports that). Only the
        // queue entry goes; its user_message already put it in the list.
        const injectData = payload as unknown as QueueEventData
        const injectSessionId = injectData.sessionId || sessionId
        if (injectSessionId !== currentSessionId.value) break
        if (injectData.queueId) {
          removeQueued(injectSessionId, injectData.queueId)
          onRenderNeeded()
        }
        break
      }

      case 'queue_cancel': {
        const cancelData = payload as { sessionId?: string; queueIds?: string[] }
        const eventSessionId = cancelData.sessionId || sessionId
        if (eventSessionId !== currentSessionId.value) break
        const cancelled = cancelData.queueIds || []
        removeQueuedMany(eventSessionId, cancelled)
        for (const id of cancelled) untrackInFlightSend(id)
        onRenderNeeded()
        break
      }
    }
  }

  const unsubscribeFromWs = onEvent(handleChatStreamEvent)

  // Watch for a silently-lost stream subscription for the lifetime of the
  // composable. Cheap (one 30s interval, all checks are O(1) guard reads) and
  // inert unless a turn is in flight on a visible panel.
  startStallWatch()

  async function cancelStream() {
    if (!currentSessionId.value || !loading.value) return
    // Record the click before the send so the measured latency includes the
    // WS round-trip, not just the backend's own work.
    markCancelRequested()
    // Send cancel via WS
    sendWsMessage({ type: 'cancel', session_id: currentSessionId.value })
  }

  // Subscribe whenever the current session changes (session open / switch).
  // This makes the persistent subscription follow the currentSessionId data
  // fact rather than any connectStream call site — a client that opens (or is
  // switched to) a session is subscribed immediately, so stream_start /
  // user_message / queue_drain events for a live session are never missed.
  // subscribe() dedups: re-observing the same session is a no-op.
  const stopSessionWatch = watch(currentSessionId, (sid) => {
    // Buffered events belong to the previous session's in-flight turn; applying
    // them to the new session's placeholder would corrupt it.
    clearBufferedEvents()
    // The stall window is per-turn state: a new session starts a fresh one.
    resetStallWatch()
    if (sid) subscribe(sid)
  }, { immediate: true })

  // Re-subscribe on WS reconnect
  // NOTE: After this watch fires, App.vue's handleReconnect runs
  // loadSessionsOnce() which refreshes runningSessions. If the session
  // finished during disconnection, handleReconnect (via useChatSession)
  // will detect the stale loading state and clean it up. The re-subscribe
  // here is a fallback for the case where the session IS still running.
  // The backend clears all subscriptions on disconnect, so a subscribed
  // session must be re-subscribed exactly once on reconnect. The watch
  // fires once per false→true transition, so exactly one subscribe is sent.
  // It intentionally bypasses subscribe()'s dedup — isSubscribed is still
  // true, but the backend already dropped the subscription.
  const stopConnectedWatch = watch(connected, (isConnected) => {
    if (isConnected && isSubscribed && subscribedSessionId) {
      appLog.i(TAG, 'WS reconnected, re-subscribing to session stream')
      sendWsMessage({ type: 'subscribe', session_id: subscribedSessionId })
    }
  })

  onUnmounted(() => {
    disconnectStream()
    stopStallWatch()
    clearToolUseTimeouts()
    renderScheduler.cancelAll()
    unsubscribeFromWs()
    stopConnectedWatch()
    stopSessionWatch()
  })

  return {
    connectStream,
    disconnectStream,
    subscribe,
    resubscribe,
    unsubscribe,
    ensureStreamingPlaceholder,
    cancelStream,
  }
}
