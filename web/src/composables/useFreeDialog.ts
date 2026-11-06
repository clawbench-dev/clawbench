/**
 * Free-dialog mode (自由对话): the microphone stays open, the user talks, and a
 * turn is sent to the assistant when they say a trigger word or simply stop
 * talking. Once the reply has been read out, listening resumes — and if the
 * user starts speaking while it is still talking, the playback is cut short.
 *
 * State machine:
 *
 *   idle ──start()──▶ listening ⇄ thinking ⇄ speaking ──stop()──▶ idle
 *
 *   listening  microphone open, streaming to the recogniser
 *   thinking   a turn was sent, waiting for the reply to finish
 *   speaking   the reply is being read aloud (auto-speech)
 *
 * Only ONE of {listening, speaking} holds the microphone: during playback the
 * capture is closed. That is what keeps the assistant from hearing itself, and
 * it is why "barge-in" is implemented by briefly reopening the microphone
 * rather than by filtering the live signal.
 *
 * Why barge-in reopens the mic instead of leaving it open:
 *   Echo cancellation removes most of the speaker's voice, but a Bluetooth
 *   headset with drifting latency or a loud speaker leaks fragments, and the
 *   leaked fragments are indistinguishable from the user (same voice band, same
 *   language). Opening the mic only when we are prepared to act on what it hears
 *   — plus the `looksLikeOwnSpeech` comparison against the text being spoken —
 *   makes a false trigger harmless instead of destructive.
 *
 * The loop never sends on its own initiative: a turn is sent only when the
 * user's own words produce a trigger or a pause. Silence alone is not a message.
 */
import { computed, ref, watch } from 'vue'
import { appLog } from '@/utils/appLog'
import {
  VOICE_TUNING_DEFAULTS,
  extractTrigger,
  looksLikeOwnSpeech,
  matchesBargeInWord,
  readVoiceTuning,
  type VoiceTuning,
} from '@/utils/audioTuning'
import { acquireMic, classifyMicError, describeMicError } from '@/utils/micCapture'
import { PcmStreamer } from '@/utils/pcmCapture'
import { serverConfig } from '@/composables/useSettingsConfig'
import { useAutoSpeech } from '@/composables/useAutoSpeech'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'

const TAG = 'FreeDialog'

export type FreeDialogPhase = 'idle' | 'listening' | 'thinking' | 'speaking'

/**
 * How long to wait after playback ends before reopening the microphone.
 *
 * The speaker keeps ringing for a moment after the last sample, and a
 * Bluetooth output adds its own latency on top. Reopening immediately captures
 * the tail of the assistant's last syllable and — because it is genuinely the
 * user's microphone hearing a voice — feeds it straight back to the recogniser.
 */
const REOPEN_DELAY_MS = 500

// ── Module-level singleton ───────────────────────────────────────────────
// One loop per app instance, like useAutoSpeech: the mode is a property of the
// session being talked to, not of a component that may unmount.
const phase = ref<FreeDialogPhase>('idle')
/** Live transcript of what the microphone currently hears (shown to the user). */
const liveTranscript = ref('')
/** Set when the mode stopped itself because of a failure, so the UI can say why. */
const lastError = ref('')

let tuning: VoiceTuning = { ...VOICE_TUNING_DEFAULTS }
let micStream: MediaStream | null = null
let streamer: PcmStreamer | null = null
let ws: WebSocket | null = null
/** Text confirmed by the recogniser for the CURRENT utterance. */
let utterance = ''
/** The session the loop belongs to; a switch cancels the loop. */
let sessionId = ''
/** Called with the final text when a turn should be sent. */
let onSend: ((text: string) => void) | null = null
/** Whether a turn is currently in flight (set by the owner via `setThinking`). */
let turnInFlight = false
let silenceTimer: ReturnType<typeof setTimeout> | null = null
let reopenTimer: ReturnType<typeof setTimeout> | null = null
/** Barge-in listener state: the mic is open only while the assistant talks. */
let bargeStreamer: PcmStreamer | null = null
let bargeStream: MediaStream | null = null
let bargeTriggered = false
/** Generation guard: every stop() bumps this so late async work bails out. */
let gen = 0

const autoSpeech = useAutoSpeech()

function clearSilenceTimer() {
  if (silenceTimer !== null) {
    clearTimeout(silenceTimer)
    silenceTimer = null
  }
}

function clearReopenTimer() {
  if (reopenTimer !== null) {
    clearTimeout(reopenTimer)
    reopenTimer = null
  }
}

function closeSocket() {
  if (ws) {
    ws.onmessage = null
    ws.onopen = null
    ws.onerror = null
    ws.onclose = null
    try { ws.close() } catch { /* already closed */ }
    ws = null
  }
}

function stopCapture() {
  streamer?.stop()
  streamer = null
  if (micStream) {
    micStream.getTracks().forEach((t) => t.stop())
    micStream = null
  }
  closeSocket()
}

function stopBargeListener() {
  bargeStreamer?.stop()
  bargeStreamer = null
  if (bargeStream) {
    bargeStream.getTracks().forEach((t) => t.stop())
    bargeStream = null
  }
}

/** Ends the mode and releases every resource. Safe to call from anywhere. */
export function stop(): void {
  gen += 1
  clearSilenceTimer()
  clearReopenTimer()
  stopCapture()
  stopBargeListener()
  utterance = ''
  liveTranscript.value = ''
  turnInFlight = false
  phase.value = 'idle'
}

/** Arms the "a pause also sends" timer, restarting it on every frame. */
function restartSilenceTimer() {
  clearSilenceTimer()
  if (tuning.silenceSendMs <= 0) return
  silenceTimer = setTimeout(() => {
    silenceTimer = null
    if (phase.value !== 'listening') return
    const text = utterance.trim()
    if (text) sendTurn(text)
  }, tuning.silenceSendMs)
}

/** Sends the current utterance as a turn and leaves the microphone closed. */
function sendTurn(text: string) {
  const clean = text.trim()
  if (!clean) return
  clearSilenceTimer()
  utterance = ''
  liveTranscript.value = ''
  // Close the capture before the turn starts: the reply will be read aloud, and
  // a microphone left open would hear the assistant the moment it speaks.
  stopCapture()
  phase.value = 'thinking'
  turnInFlight = true
  onSend?.(clean)
}

function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}${path}`
}

/** Opens the microphone and the streaming recogniser. */
async function openListening(): Promise<void> {
  const myGen = gen
  const toast = useToast()
  try {
    micStream = await acquireMic({ audio: { echoCancellation: tuning.echoCancellation, noiseSuppression: tuning.noiseSuppression, autoGainControl: tuning.autoGainControl } })
  } catch (e) {
    lastError.value = gt(classifyMicError(e) === 'denied' ? 'chat.voice.micDenied' : 'chat.voice.micUnavailable')
    toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
    appLog.e(TAG, `getUserMedia failed ${describeMicError(e)}`)
    stop()
    return
  }
  if (myGen !== gen) return

  const sock = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
  ws = sock
  sock.onmessage = (e) => {
    if (ws !== sock || myGen !== gen) return
    try {
      const msg = JSON.parse(e.data as string) as { type?: string; text?: string; final?: string; message?: string }
      if (msg.type === 'partial') {
        liveTranscript.value = msg.text ?? ''
      } else if (msg.type === 'text' && msg.text) {
        utterance += msg.text
        liveTranscript.value = utterance
      } else if (msg.type === 'done') {
        const final = (msg.final ?? '').trim()
        if (final) utterance = final
        handleUtterance()
      } else if (msg.type === 'error') {
        lastError.value = msg.message || gt('chat.voice.transcribeFailed')
        toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
        stop()
      }
    } catch { /* ignore malformed */ }
  }
  sock.onopen = () => {
    if (ws !== sock || myGen !== gen) return
    sock.send(JSON.stringify({ type: 'start', sampleRate: 16000, channels: 1, bits: 16, context: '' }))
    const s = new PcmStreamer({
      stream: micStream!,
      tuning,
      onFrame: (frame) => {
        if (ws && ws.readyState === WebSocket.OPEN) ws.send(frame)
      },
      onLevel: (rms) => {
        // Any level above the gate threshold restarts the pause timer: the user
        // is still talking. Frames below it never reach this callback.
        if (phase.value === 'listening' && rms > 0) restartSilenceTimer()
      },
    })
    streamer = s
    void s.start().catch((err) => {
      appLog.e(TAG, 'pcm capture failed', err)
      lastError.value = gt('chat.voice.micUnavailable')
      toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
      stop()
    })
  }
  sock.onerror = () => {
    if (ws !== sock || myGen !== gen) return
    lastError.value = gt('chat.voice.connectFailed')
    stop()
  }
  sock.onclose = () => {
    if (ws !== sock || myGen !== gen) return
    if (phase.value === 'listening') stop()
  }
  phase.value = 'listening'
  restartSilenceTimer()
}

/**
 * Handles one finished utterance: strip a trigger word and send, or keep
 * listening.
 *
 * The recogniser's "done" only fires when the engine decides the sentence ended
 * (`stt.end_window_size`), so this is not the same thing as the pause timer —
 * they race, and whichever fires first sends.
 */
function handleUtterance() {
  if (phase.value !== 'listening') return
  const text = utterance.trim()
  if (!text) {
    restartSilenceTimer()
    return
  }
  const { matched, text: rest } = extractTrigger(text, tuning.triggerWords)
  if (matched) {
    // Everything except the trigger word goes out. A bare "发送" sends nothing —
    // there was no content, and sending an empty message would start a turn
    // the user did not ask for.
    if (rest) sendTurn(rest)
    else {
      utterance = ''
      liveTranscript.value = ''
      restartSilenceTimer()
    }
    return
  }
  restartSilenceTimer()
}

/**
 * Opens a short-lived microphone during playback so the user can cut the
 * assistant off.
 *
 * Recognition runs on the same socket protocol, but only the trigger decision
 * matters here — the transcript is discarded.
 */
async function openBargeListener(): Promise<void> {
  if (tuning.bargeIn === 'off') return
  const myGen = gen
  bargeTriggered = false
  try {
    bargeStream = await acquireMic({ audio: { echoCancellation: tuning.echoCancellation, noiseSuppression: tuning.noiseSuppression, autoGainControl: tuning.autoGainControl } })
  } catch {
    // Barge-in is an optimisation; failing to open the microphone must not
    // break the read-aloud itself.
    return
  }
  if (myGen !== gen || phase.value !== 'speaking') {
    bargeStream.getTracks().forEach((t) => t.stop())
    bargeStream = null
    return
  }
  const sock = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
  const s = new PcmStreamer({
    stream: bargeStream,
    tuning,
    onFrame: (frame) => {
      if (sock.readyState === WebSocket.OPEN) sock.send(frame)
    },
  })
  bargeStreamer = s
  sock.onopen = () => {
    if (myGen !== gen) return
    sock.send(JSON.stringify({ type: 'start', sampleRate: 16000, channels: 1, bits: 16, context: '' }))
    void s.start().catch(() => { /* barge-in degrades silently */ })
  }
  sock.onmessage = (e) => {
    if (myGen !== gen || bargeTriggered) return
    try {
      const msg = JSON.parse(e.data as string) as { type?: string; text?: string; final?: string }
      const heard = (msg.final ?? msg.text ?? '').trim()
      if (!heard) return
      // The assistant's own voice leaking through the speakers is the main
      // false positive; anything that matches what is being read out is ignored.
      if (looksLikeOwnSpeech(heard, autoSpeech.speakingText.value)) return
      if (!matchesBargeInWord(heard, tuning)) return
      bargeTriggered = true
      appLog.i(TAG, `barge-in by "${heard}"`)
      autoSpeech.stopAudio()
      // The playback state watcher reopens listening once it sees 'idle'.
    } catch { /* ignore malformed */ }
  }
  sock.onclose = () => { /* the playback watcher owns the lifecycle */ }
}

/** Applies the server settings and starts listening. */
export async function startFreeDialog(opts: {
  sessionId: string
  onSend: (text: string) => void
}): Promise<void> {
  stop()
  sessionId = opts.sessionId
  onSend = opts.onSend
  lastError.value = ''
  tuning = readVoiceTuning(serverConfig.value as Record<string, unknown>)
  await openListening()
}

/** Called by the owner when a turn it started has finished (or failed). */
export function setFreeDialogThinking(thinking: boolean): void {
  turnInFlight = thinking
  if (thinking) {
    if (phase.value === 'listening') {
      stopCapture()
      phase.value = 'thinking'
    }
    return
  }
  // Turn finished. If auto-speech is going to read the reply, the phase moves to
  // 'speaking' (see the playback watcher) and listening resumes when it ends;
  // otherwise resume right away.
  if (phase.value === 'thinking') {
    if (!autoSpeech.enabled.value) resumeListening()
  }
}

/** Reopens the microphone after the reply has been dealt with. */
function resumeListening(): void {
  clearReopenTimer()
  const myGen = gen
  reopenTimer = setTimeout(() => {
    reopenTimer = null
    if (myGen !== gen || phase.value === 'idle' || phase.value === 'listening') return
    void openListening()
  }, REOPEN_DELAY_MS)
}

/**
 * Mirrors auto-speech's playback state into the dialog phase.
 *
 * Registered once at module scope: the composable is a singleton and the watcher
 * must exist even when no component has called `useFreeDialog()` yet.
 *
 * Two edges are watched on purpose. `state` says "audio is playing right now",
 * which is what starts and ends barge-in. `isBusy()` covers the window before
 * the first audio chunk and the cases where a cycle never reaches 'playing'
 * (a reply with nothing to say, a TTS failure) — without it the phase would
 * stay 'speaking' forever and the microphone would never reopen.
 */
watch(
  () => autoSpeech.state.value,
  (now, before) => {
    if (phase.value === 'idle') return
    const playing = now === 'playing'
    const wasPlaying = before === 'playing'
    if (playing && !wasPlaying) {
      phase.value = 'speaking'
      void openBargeListener()
    } else if (!playing && wasPlaying) {
      stopBargeListener()
      if (phase.value === 'speaking') settleAfterPlayback()
    }
  },
)

watch(
  () => autoSpeech.isBusy(),
  (busy, wasBusy) => {
    if (!wasBusy || busy) return
    if (phase.value !== 'speaking') return
    stopBargeListener()
    settleAfterPlayback()
  },
)

/**
 * Decides what follows a finished read-aloud: another turn is in flight, so
 * wait; otherwise reopen the microphone.
 */
function settleAfterPlayback(): void {
  if (turnInFlight) {
    phase.value = 'thinking'
    return
  }
  resumeListening()
}

/** Reactive surface for the UI. */
export function useFreeDialog() {
  const enabled = computed(() => phase.value !== 'idle')
  return {
    phase,
    enabled,
    liveTranscript,
    lastError,
    start: startFreeDialog,
    stop,
    setThinking: setFreeDialogThinking,
  }
}

/** Session the loop is bound to ('' when idle). */
export function freeDialogSessionId(): string {
  return phase.value === 'idle' ? '' : sessionId
}
