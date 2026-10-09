/**
 * useAutoSpeech
 *
 * Manages the auto-speech toggle state and audio playback for AI messages.
 * When enabled, AI replies are automatically summarized and read aloud via TTS.
 * Toggle state is persisted in localStorage.
 *
 * Uses module-level singleton state so all consumers share the same toggle/audio state.
 * Should only be instantiated once (in ChatPanel.vue) and provided via inject to children.
 *
 * State machine: idle → summarizing → synthesizing → playing → idle
 *   - Phase transitions are driven by EventSource SSE events from the backend.
 *   - Cache hits skip SSE entirely and play audio immediately.
 */

import { ref } from 'vue'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import i18n from '@/i18n'
import { localConfig, setLocalConfig } from '@/composables/useSettingsConfig'
import { buildLocalFileUrl } from '@/utils/download.ts'
import { useWakeLock } from '@/composables/useWakeLock'
import { MseAudioPlayer } from '@/composables/useMseAudio'
import { appLog } from '@/utils/appLog'
import { extractSpeakableText } from '@/utils/speakableText'

// Re-exported so existing `@/composables/useAutoSpeech` importers keep working;
// the implementation lives in the leaf module to avoid dragging this file's
// module-level side effects (useToast/i18n) into text-only consumers.
export { extractSpeakableText }

/**
 * Extract speakable text from chat message blocks.
 * Includes both text blocks and AskUserQuestion tool_use blocks
 * (structured questions) so TTS can read the question and options.
 *
 * Implemented in `@/utils/speakableText` (a leaf module) and re-exported above
 * so text-only consumers do not pull in this file's module-level side effects.
 */

/** TTS lifecycle states — the single source of truth for UI rendering */
type SpeechState = 'idle' | 'summarizing' | 'synthesizing' | 'playing'

// --- Singleton state (shared across all instances) ---
const enabled = ref(false)
const state = ref<SpeechState>('idle')
const activeId = ref<string>('')
const playingSummary = ref<string>('')
const lastError = ref<string>('')
let abortController: AbortController | null = null
let currentEventSource: EventSource | null = null
let currentAudioEl: HTMLAudioElement | null = null
let currentWs: WebSocket | null = null
let mseAudio: MseAudioPlayer | null = null
let playbackEndTimer: ReturnType<typeof setInterval> | null = null
let playbackTimeupdateHandler: (() => void) | null = null
let playbackEndAudio: HTMLAudioElement | null = null
/** Set to true after MSE endOfStream() — signals that no more chunks will arrive */
let mseStreamEnded = false

/**
 * FIFO of auto-speech items waiting for the current playback to finish.
 *
 * A group chat can have several members speak in one run (and, in free mode,
 * concurrently). Each member turn finalizes on its own, so auto-speech is
 * triggered once per speaker — without a queue every new trigger would preempt
 * the previous one (`_speak` stops the current audio first) and only the LAST
 * speaker would ever be heard. Queuing makes them play one after another
 * ("轮流播放"). Manual playback (the read-aloud button) and an explicit stop
 * clear the queue, since those are deliberate user actions.
 */
let autoQueue: Array<{ id: string; text: string; speakerName?: string }> = []

function clearPlaybackEndTimer() {
  if (playbackEndTimer) {
    clearInterval(playbackEndTimer)
    playbackEndTimer = null
  }
  if (playbackEndAudio && playbackTimeupdateHandler && typeof playbackEndAudio.removeEventListener === 'function') {
    playbackEndAudio.removeEventListener('timeupdate', playbackTimeupdateHandler)
  }
  playbackEndAudio = null
  playbackTimeupdateHandler = null
  mseStreamEnded = false
}

function startPlaybackEndTimer(audio: HTMLAudioElement, onEnd: () => void) {
  clearPlaybackEndTimer()
  playbackEndAudio = audio
  let lastCurrentTime = 0
  let stallCount = 0

  const handler = () => {
    if (state.value !== 'playing') return

    const dur = audio.duration
    const ct = audio.currentTime

    // Finite duration: standard detection — currentTime near duration
    if (dur > 0 && isFinite(dur) && ct >= dur - 0.5) {
      appLog.i(TAG, 'Fallback: detected playback end via duration check (ended event missed)')
      onEnd()
      return
    }

    // MSE with Infinity/NaN/0 duration: after endOfStream(), detect playback end
    // by checking if currentTime has stalled (not advancing for >2 consecutive checks)
    // while we've reached the end of the buffered range.
    // Also applies to non-MSE paths where duration is unavailable (0/NaN/Infinity)
    // and the audio has been playing for a while without advancing.
    if (mseStreamEnded || (dur === 0 || !isFinite(dur))) {
      const buffered = audio.buffered
      if (buffered && buffered.length > 0) {
        const bufferEnd = buffered.end(buffered.length - 1)
        // If currentTime is near or past the last buffered byte, start stall detection
        if (ct >= bufferEnd - 0.5) {
          if (ct === lastCurrentTime) {
            stallCount++
            if (stallCount >= 5) { // 5 × 500ms = 2.5s stall → playback finished
              appLog.i(TAG, 'Fallback: detected playback end via stall after MSE endOfStream (ended event missed)')
              onEnd()
              return
            }
          } else {
            stallCount = 0
          }
        }
      } else if (dur === 0 || !isFinite(dur)) {
        // No buffered info available — use pure stall detection:
        // if currentTime hasn't advanced for 2.5s and we've played something, we're done
        if (ct > 0 && ct === lastCurrentTime) {
          stallCount++
          if (stallCount >= 5) {
            appLog.i(TAG, 'Fallback: detected playback end via pure stall (ended event missed)')
            onEnd()
            return
          }
        } else {
          stallCount = 0
        }
      }
      lastCurrentTime = ct
    }
  }
  playbackTimeupdateHandler = handler
  audio.addEventListener('timeupdate', handler)
  playbackEndTimer = setInterval(handler, 500)
}

// Initialize from settings config (which handles legacy key migration)
enabled.value = !!localConfig.autoSpeech

// Module-level toast instance (shared, not per-component)
const toast = useToast()

// Wake lock singleton — acquired when output starts (if auto-speech on), released when TTS ends
const wakeLock = useWakeLock()

/** Track whether screen lock is currently suppressed for the output+speech cycle.
 *  Used to avoid releasing when we didn't acquire (e.g. auto-speech was off). */
let screenLockSuppressed = false

const TAG = 'AutoSpeech'

// Sync from Settings page changes
if (typeof window !== 'undefined') {
  window.addEventListener('clawbench-autospeech-change', (e: Event) => {
    const val = (e as CustomEvent).detail as boolean
    enabled.value = val
    toast.show(gt(val ? 'autoSpeech.enabled' : 'autoSpeech.disabled'), { icon: val ? '🔊' : '🔇', type: 'info', duration: 2000 })
  })
}

export function useAutoSpeech() {
  // --- Persistence ---
  function saveState() {
    setLocalConfig('autoSpeech', enabled.value)
  }

  function toggle() {
    enabled.value = !enabled.value
    saveState()
    if (!enabled.value) {
      stopAudio()
      toast.show(gt('autoSpeech.disabled'), { icon: '🔇', type: 'info', duration: 2000 })
    } else {
      toast.show(gt('autoSpeech.enabled'), { icon: '🔊', type: 'info', duration: 2000 })
    }
  }

  // --- Audio Playback ---

  /** True while a TTS generation/playback cycle is in flight (including the
   *  window between the POST and the first audio, when `state` is still idle). */
  function isBusy(): boolean {
    return state.value !== 'idle' || activeId.value !== ''
  }

  /** Append an auto-speech item and play it now when nothing is in flight. */
  function enqueueOrPlay(id: string, text: string, speakerName?: string) {
    if (!text) return
    if (isBusy()) {
      autoQueue.push({ id, text, speakerName })
      return
    }
    void _speak(id, text, speakerName)
  }

  /** Play the next queued item (if any). Called when a cycle ends. */
  function drainAutoQueue() {
    const next = autoQueue.shift()
    if (!next) return
    void _speak(next.id, next.text, next.speakerName)
  }

  /**
   * Finish the current cycle and hand off to the next queued speaker.
   * The screen wake lock is kept across a hand-off (there is still output to
   * play) and released only when the queue drains.
   */
  function advancePlayback() {
    const hasMore = autoQueue.length > 0
    stopPlayback(!hasMore)
    if (hasMore) drainAutoQueue()
  }

  /** Handle audio.play() rejection uniformly. */
  function handlePlayError(err: unknown) {
    const errName = (err as Error)?.name
    if (errName === 'AbortError') return
    let message = gt('autoSpeech.generateFailedGeneric')
    if (errName === 'NotAllowedError') {
      message = gt('autoSpeech.autoplayBlocked')
    }
    reportError(message)
    // A failed speaker must not stall the rest of the run — advance the queue.
    advancePlayback()
  }

  /**
   * Stop the current audio/TTS cycle (does NOT touch the queue — callers that
   * mean "stop everything" go through the exported `stopAudio`).
   * @param releaseScreenLock If true, release the screen wake lock. Pass false
   *   when handing off to the next queued speaker, so the lock stays held.
   */
  function stopPlayback(releaseScreenLock = true) {
    abortController?.abort()
    abortController = null
    if (currentEventSource) {
      currentEventSource.close()
      currentEventSource = null
    }
    if (currentWs) {
      currentWs.close()
      currentWs = null
    }
    if (mseAudio) {
      mseAudio.cleanup()
      mseAudio = null
    }
    if (currentAudioEl) {
      currentAudioEl.pause()
      currentAudioEl.currentTime = 0
      currentAudioEl.onended = null
      currentAudioEl.onerror = null
      currentAudioEl = null
    }
    clearPlaybackEndTimer()
    activeId.value = ''
    playingSummary.value = ''
    state.value = 'idle'
    // Release screen lock if suppressed for this output cycle
    if (releaseScreenLock && screenLockSuppressed) {
      wakeLock.release()
      screenLockSuppressed = false
    }
  }

  /**
   * Stop playback AND clear any queued speakers. This is the deliberate
   * user-facing stop (read-aloud button acting as stop, toggling auto-speech
   * off): it must not be followed by "the next speaker" surprising the user.
   */
  function stopAudio(releaseScreenLock = true) {
    autoQueue = []
    stopPlayback(releaseScreenLock)
  }

  function reportError(message: string) {
    lastError.value = message
    toast.show(message, { icon: '🔊', type: 'error', duration: 5000 })
  }

  // --- Internal: play audio from a path ---
  function playAudio(audioPath: string) {
    const audioUrl = buildLocalFileUrl(audioPath)
    const audio = new Audio(audioUrl)
    currentAudioEl = audio
    state.value = 'playing'

    audio.onended = () => {
      appLog.i(TAG, 'playAudio: onended fired')
      advancePlayback()
    }
    audio.onerror = () => {
      appLog.i(TAG, 'playAudio: onerror fired')
      reportError(gt('autoSpeech.playbackFailed'))
      advancePlayback()
    }

    // Fallback: detect playback end via polling if 'ended' event is missed
    startPlaybackEndTimer(audio, () => {
      appLog.i(TAG, 'playAudio: fallback timer triggered advancePlayback')
      advancePlayback()
    })

    audio.play().catch(handlePlayError)
  }

  // --- Internal: play streaming audio via WebSocket + MSE ---
  function playStreamingAudio(jobId: string) {
    // Check MSE support first — fall back to SSE if not available
    if (!MseAudioPlayer.isSupported()) {
      appLog.w(TAG, 'MSE not supported, falling back to SSE')
      connectSSE(jobId)
      return
    }

    const mse = new MseAudioPlayer()
    mseAudio = mse
    const audio = mse.init()
    currentAudioEl = audio
    state.value = 'synthesizing'

    audio.onended = () => {
      appLog.i(TAG, 'playStreamingAudio: onended fired')
      advancePlayback()
    }
    audio.onerror = () => {
      appLog.i(TAG, 'playStreamingAudio: onerror fired')
      reportError(gt('autoSpeech.playbackFailed'))
      advancePlayback()
    }

    // Fallback: some browsers (notably Android WebView) may not fire 'ended'
    // after MSE endOfStream(). Use timeupdate + polling to detect completion.
    startPlaybackEndTimer(audio, () => {
      appLog.i(TAG, 'playStreamingAudio: fallback timer triggered advancePlayback')
      advancePlayback()
    })

    // Build WebSocket URL
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${location.host}/api/tts/audio/ws`
    const ws = new WebSocket(wsUrl)
    ws.binaryType = 'arraybuffer'
    currentWs = ws

    let firstChunkReceived = false

    ws.onopen = () => {
      ws.send(JSON.stringify({ type: 'start', jobId }))
    }

    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        // Binary frame — MP3 chunk
        mse.appendChunk(event.data)
        if (!firstChunkReceived) {
          firstChunkReceived = true
          state.value = 'playing'
          audio.play().catch(handlePlayError)
        }
      } else {
        // Text frame — control message
        try {
          const msg = JSON.parse(event.data)
          if (msg.type === 'phase') {
            if (msg.phase === 'summarizing') state.value = 'summarizing'
            else if (msg.phase === 'synthesizing') state.value = 'synthesizing'
          } else if (msg.type === 'done') {
            if (msg.summary) playingSummary.value = msg.summary
            mseStreamEnded = true
            mse.endOfStream()
            ws.close()
          } else if (msg.type === 'error') {
            reportError(msg.message || gt('autoSpeech.synthesisFailed'))
            stopAudio()
            ws.close()
          }
        } catch { /* ignore malformed */ }
      }
    }

    ws.onerror = () => {
      reportError(gt('autoSpeech.generateFailedGeneric'))
      stopAudio()
    }

    ws.onclose = () => {
      if (currentWs === ws) currentWs = null
    }
  }

  // --- Internal: connect to SSE for non-streaming TTS (Piper/Kokoro/MOSS-Nano) ---
  function connectSSE(jobId: string) {
    const controller = abortController
    const es = new EventSource(`/api/tts/stream/${jobId}`)
    currentEventSource = es

    let resultData: Record<string, unknown> | null = null

    es.addEventListener('phase', (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data)
        if (event.phase === 'summarizing') {
          state.value = 'summarizing'
        } else if (event.phase === 'synthesizing') {
          state.value = 'synthesizing'
        }
      } catch { /* ignore malformed data */ }
    })

    es.addEventListener('result', (e: MessageEvent) => {
      try {
        resultData = JSON.parse(e.data)
      } catch { /* ignore malformed data */ }
      es.close()
      currentEventSource = null
      handleResult(resultData)
    })

    es.onerror = () => {
      es.close()
      if (currentEventSource === es) {
        currentEventSource = null
      }
      if (resultData) {
        handleResult(resultData)
        return
      }
      if (controller?.signal.aborted) return
      reportError(gt('autoSpeech.generateFailedGeneric'))
      advancePlayback()
    }

    function handleResult(result: Record<string, unknown> | null) {
      if (!result) {
        reportError(gt('autoSpeech.noResult'))
        advancePlayback()
        return
      }

      if (result.synthesizeFailed) {
        reportError(result.synthesizeError ? gt('autoSpeech.synthesisFailedDetail', { error: result.synthesizeError }) : gt('autoSpeech.synthesisFailed'))
        advancePlayback()
        return
      }

      if (!result.audioPath) {
        reportError(gt('autoSpeech.noAudioFile'))
        advancePlayback()
        return
      }

      if (result.summarizeFailed) {
        toast.show(gt('autoSpeech.summaryFailed'), { icon: '⚠️', type: 'error', duration: 3000 })
      }

      if (result.summary) {
        playingSummary.value = result.summary as string
      }

      playAudio(result.audioPath as string)
    }
  }

  // --- Internal: generate and play TTS for text ---
  async function _speak(id: string, text: string, speakerName?: string) {
    if (!text) {
      // Nothing to speak for this item — hand off to the next queued speaker
      // rather than silently dropping the rest of the run.
      advancePlayback()
      return
    }

    stopPlayback(false)
    lastError.value = ''

    const controller = new AbortController()
    abortController = controller
    activeId.value = id

    try {
      // Step 1: POST to create TTS job (or get cached result)
      const body: Record<string, unknown> = { text, language: i18n.global.locale.value }
      const msgId = parseInt(id, 10)
      if (!isNaN(msgId)) body.messageId = msgId
      // Group chat: the backend prepends "<name>说：" to the SPOKEN audio only
      // (never to the summary) so concurrent speakers are distinguishable.
      if (speakerName) body.speakerName = speakerName
      const resp = await fetch('/api/tts/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal: controller.signal,
      })

      if (!resp.ok) {
        let errorMsg = gt('autoSpeech.generateFailed', { status: resp.status })
        try {
          const errData = await resp.json()
          if (errData.error) errorMsg = gt('autoSpeech.generateFailedDetail', { error: errData.error })
        } catch { /* ignore parse error */ }
        throw new Error(errorMsg)
      }

      const data = await resp.json()

      // Cache hit — play audio immediately, no SSE needed
      if (data.cached && data.audioPath) {
        if (data.summary) {
          playingSummary.value = data.summary
        }
        playAudio(data.audioPath)
        return
      }

      // Cache miss — streaming path (Edge TTS) or SSE path (other engines)
      if (data.streaming && data.jobId) {
        state.value = 'summarizing'
        playStreamingAudio(data.jobId as string)
        return
      }

      // Non-streaming fallback (Piper/Kokoro/MOSS-Nano)
      state.value = 'summarizing'

      if (!data.jobId) throw new Error(gt('autoSpeech.noResult'))
      connectSSE(data.jobId as string)

    } catch (err: unknown) {
      const errName = (err as Error)?.name
      const errMsg = (err as Error)?.message
      if (errName === 'AbortError') return

      let message = gt('autoSpeech.generateFailedGeneric')
      if (errName === 'NotAllowedError') {
        message = gt('autoSpeech.autoplayBlocked')
      } else if (errMsg) {
        message = gt('autoSpeech.generateFailedDetail', { error: errMsg })
      }
      reportError(message)
      advancePlayback()
    } finally {
      if (abortController === controller) {
        abortController = null
      }
    }
  }

  /**
   * Auto-speech entry point: enqueue the item so it plays AFTER any speaker
   * already being read (a group run triggers this once per member). No-op when
   * auto-speech is disabled.
   */
  function speakMessage(id: string, text: string, speakerName?: string) {
    if (!enabled.value) return
    enqueueOrPlay(id, text, speakerName)
  }

  /** Manual playback (read-aloud button): a deliberate user action, so it
   *  preempts whatever is playing and clears any pending auto queue. */
  function speakText(id: string, text: string) {
    autoQueue = []
    void _speak(id, text)
  }

  function isActive(id: string): boolean {
    return activeId.value === id && state.value !== 'idle'
  }

  function getSummary(id: string): string {
    return activeId.value === id ? playingSummary.value : ''
  }

  function getPhaseLabel(id: string): string {
    if (activeId.value !== id) return ''
    switch (state.value) {
      case 'summarizing': return 'summarizing'
      case 'synthesizing': return 'synthesizing'
      case 'playing': return 'playing'
      default: return ''
    }
  }

  function isGeneratingText(id: string): boolean {
    return activeId.value === id
      && (state.value === 'summarizing' || state.value === 'synthesizing')
  }

  function isPlayingAudio(id: string): boolean {
    return activeId.value === id && state.value === 'playing'
  }

  // --- Screen Lock ---

  /**
   * Called when AI output starts.
   * Suppresses screen lock so the display stays on during
   * the entire output + TTS playback cycle.
   * Only activates if the preventScreenLock setting is enabled.
   */
  function onOutputStart() {
    if (!localConfig.preventScreenLock) return
    if (screenLockSuppressed) return
    wakeLock.acquire()
    screenLockSuppressed = true
    appLog.i(TAG, 'Screen lock suppressed for output')
  }

  /**
   * Called when output ends but auto-speech did not start TTS
   * (e.g. no speakable text). Releases the screen lock that was
   * suppressed at output start.
   */
  function onOutputEndNoSpeech() {
    // Keep the lock while a queued speaker is still pending: between two queued
    // items `state` is momentarily 'idle' (the hand-off in advancePlayback), and
    // releasing here would drop the lock mid-run.
    if (screenLockSuppressed && state.value === 'idle' && autoQueue.length === 0) {
      wakeLock.release()
      screenLockSuppressed = false
      appLog.i(TAG, 'Screen lock restored: output ended without TTS')
    }
  }

  return {
    enabled,
    state,
    lastError,
    toggle,
    speakMessage,
    speakText,
    stopAudio,
    isActive,
    getSummary,
    getPhaseLabel,
    isGeneratingText,
    isPlayingAudio,
    onOutputStart,
    onOutputEndNoSpeech,
  }
}
