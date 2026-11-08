/**
 * Free-dialog mode (shown as 连续对话 in the UI): the microphone stays open,
 * the user talks, and a turn is sent to the assistant when they say a trigger
 * word or simply stop talking. Once the reply has been read out, listening
 * resumes — and if the user starts speaking while it is still talking, the
 * playback is cut short.
 *
 * State machine:
 *
 *   idle ──start()──▶ listening ⇄ thinking ⇄ speaking ──stop()──▶ idle
 *
 *   listening  microphone open, streaming to the recogniser
 *   thinking   a turn was sent, waiting for the reply to finish
 *   speaking   the reply is being read aloud (auto-speech)
 *
 * ONE microphone for the whole session: the capture graph is opened when the
 * mode starts and released when it stops. A phase change only swaps who reads
 * its frames — the dictation recogniser while listening, the interrupt
 * recogniser while speaking, nobody in between.
 *
 * Two reasons, both observed on a phone:
 *
 *   1. Closing the dictation capture and acquiring the interrupt one at the
 *      exact instant playback begins makes Android reconfigure the audio
 *      route (WebRTC switches the audio mode when a capture starts). The first
 *      few characters of the reply were swallowed on every read-aloud.
 *   2. Re-acquiring the device — getUserMedia, a fresh AudioContext, the
 *      worklet module — is what made "listen again once it stops talking" feel
 *      like a second of dead air. With the graph already running, resuming is
 *      one WebSocket handshake.
 *
 * Frames are still only handed to a consumer that is prepared to act on them,
 * which is what keeps the assistant from hearing itself: the leak is filtered
 * by never forwarding, not by closing the device.
 *
 * Why barge-in keeps the mic open instead of filtering the live signal:
 *   Echo cancellation removes most of the speaker's voice, but a Bluetooth
 *   headset with drifting latency or a loud speaker leaks fragments, and the
 *   leaked fragments are indistinguishable from the user (same voice band, same
 *   language). Reading the microphone only when we are prepared to act on what
 *   it hears — plus the `looksLikeOwnSpeech` comparison against the text being
 *   spoken — makes a false trigger harmless instead of destructive.
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
import { useWakeLock } from '@/composables/useWakeLock'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'

const TAG = 'FreeDialog'

export type FreeDialogPhase = 'idle' | 'listening' | 'thinking' | 'speaking'

/**
 * How long to wait after playback ends before the microphone is handed back to
 * dictation.
 *
 * The speaker keeps ringing for a moment after the last sample, and a
 * Bluetooth output adds its own latency on top. Arming immediately captures
 * the tail of the assistant's last syllable and — because it is genuinely the
 * user's microphone hearing a voice — feeds it straight back to the recogniser.
 *
 * This is the ONLY wait now: the capture itself stays open across the whole
 * session, so resuming costs one socket handshake instead of re-acquiring the
 * device. A few hundred milliseconds is enough for the tail; the old value was
 * tuned against a much slower path and made the mode feel deaf.
 */
const REOPEN_DELAY_MS = 250

// ── Module-level singleton ───────────────────────────────────────────────
// One loop per app instance, like useAutoSpeech: the mode is a property of the
// session being talked to, not of a component that may unmount.
const phase = ref<FreeDialogPhase>('idle')
/** Live transcript of what the microphone currently hears (shown to the user). */
const liveTranscript = ref('')
/** Set when the mode stopped itself because of a failure, so the UI can say why. */
const lastError = ref('')

let tuning: VoiceTuning = { ...VOICE_TUNING_DEFAULTS }
/** The session's capture graph — see "ONE microphone" in the header. */
let micStream: MediaStream | null = null
let streamer: PcmStreamer | null = null
/**
 * Where the current frames go, or null when nobody is reading them.
 *
 * This is the whole of the phase switch: listening points it at the dictation
 * socket, speaking at the interrupt one, and the gaps in between at nothing.
 */
let frameSink: ((frame: ArrayBuffer) => void) | null = null
/** An acquisition in flight, so two phase changes cannot open two devices. */
let capturePending: Promise<boolean> | null = null
/** Invalidates an acquisition that was still pending when the session moved on. */
let captureGen = 0
/** Recogniser socket of the dictation (listening) phase. */
let ws: WebSocket | null = null
/** Text confirmed by the recogniser for the CURRENT utterance. */
let utterance = ''
/** The session the loop belongs to; a switch cancels the loop. */
let sessionId = ''
/** Called with the final text when a turn should be sent. */
let onSend: ((text: string) => void) | null = null
/** Cancels a turn that is still running when the user interrupts it. */
let onInterrupt: (() => void) | null = null
/** Whether a turn is currently in flight (set by the owner via `setThinking`). */
let turnInFlight = false
let silenceTimer: ReturnType<typeof setTimeout> | null = null
/** Fires when the microphone has produced no recognised words for too long. */
let idleExitTimer: ReturnType<typeof setTimeout> | null = null
let reopenTimer: ReturnType<typeof setTimeout> | null = null
/** Recogniser socket of the interrupt (speaking) phase. */
let bargeWs: WebSocket | null = null
let bargeTriggered = false
/**
 * Invalidation counter for the interrupt listener.
 *
 * `openBargeListener` awaits the capture, so two callers can be inside it at
 * once (the playback watcher and "opened while a turn was already running").
 * Clearing the socket refs before the await only stops the PREVIOUS listener;
 * without this counter the slower call would install its socket over the
 * faster one's and leak a recogniser session nothing can reach any more.
 */
let bargeGen = 0
/**
 * Invalidation counter for the dictation socket.
 *
 * `openListening` awaits the capture as well. A barge-in or a turn send that
 * lands during that await must win: without the counter the slower call would
 * install a dictation socket over a phase that has already moved on, and the
 * assistant's own reply would be transcribed as the user's next message.
 */
let listenGen = 0
/**
 * Grace window after a turn ends, while we wait to find out whether the reply
 * is going to be read aloud.
 *
 * `loading` turns false BEFORE playback starts. Arming dictation on that edge
 * would transcribe the first sentence of the reply; waiting forever would leave
 * the loop stuck whenever there is nothing to say (an empty reply, a TTS
 * failure, `auto-speech` on but the message having no speakable text). A short
 * window resolves both: playback starting cancels it, and if it fires with
 * nothing playing, listening resumes.
 *
 * `speakMessage` marks auto-speech busy synchronously, so this window is only
 * ever spent when nothing will be read out — it does not delay a normal reply.
 */
let settleTimer: ReturnType<typeof setTimeout> | null = null
const SETTLE_GRACE_MS = 600
/** Generation guard: every stop() bumps this so late async work bails out. */
let gen = 0

const autoSpeech = useAutoSpeech()

/**
 * Keeps the display awake for the whole time the mode is on.
 *
 * This is not a comfort setting: the loop lives in the page, so the moment the
 * screen turns itself off the app goes to the background and Android takes the
 * microphone away — the dialog silently stops listening. A user who is
 * thinking out loud does not touch the screen, so without this the mode dies
 * after the system's screen timeout, which is exactly when they are relying on
 * it. Held unconditionally (not behind `preventScreenLock`) for that reason,
 * and released in `stop()` so leaving the mode restores the normal timeout.
 */
const wakeLock = useWakeLock()
const WAKE_LOCK_REASON = 'free-dialog'

/**
 * How long to wait before telling the user the screen will not stay awake.
 *
 * Long enough not to overwrite the "mode on" toast (the toast slot holds one
 * message), short enough to land before the system screen timeout.
 */
const WAKE_LOCK_WARNING_DELAY_MS = 2600
let wakeLockWarningTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Says out loud that this host cannot hold the screen on.
 *
 * `navigator.wakeLock` is secure-context only and the native bridge exists only
 * in the app shells, so a phone opening the server over plain http has neither.
 * Without a word the user would believe the screen is pinned on and watch the
 * mode die at the system timeout.
 */
function warnIfScreenCannotStayAwake(): void {
  if (wakeLockWarningTimer !== null) return
  const myGen = gen
  wakeLockWarningTimer = setTimeout(() => {
    wakeLockWarningTimer = null
    if (myGen !== gen || phase.value === 'idle') return
    if (wakeLock.held.value) return
    useToast().show(gt('chat.voice.freeDialogNoWakeLock'), { icon: 'warning', type: 'info', duration: 6000 })
  }, WAKE_LOCK_WARNING_DELAY_MS)
}

function clearWakeLockWarningTimer() {
  if (wakeLockWarningTimer !== null) {
    clearTimeout(wakeLockWarningTimer)
    wakeLockWarningTimer = null
  }
}

function clearSilenceTimer() {
  if (silenceTimer !== null) {
    clearTimeout(silenceTimer)
    silenceTimer = null
  }
}

function clearIdleExitTimer() {
  if (idleExitTimer !== null) {
    clearTimeout(idleExitTimer)
    idleExitTimer = null
  }
}

function clearReopenTimer() {
  if (reopenTimer !== null) {
    clearTimeout(reopenTimer)
    reopenTimer = null
  }
}

function clearSettleTimer() {
  if (settleTimer !== null) {
    clearTimeout(settleTimer)
    settleTimer = null
  }
}

/** Closes the dictation recogniser, leaving the microphone alone. */
function closeDictationSocket() {
  listenGen += 1
  if (ws) {
    ws.onmessage = null
    ws.onopen = null
    ws.onerror = null
    ws.onclose = null
    try { ws.close() } catch { /* already closed */ }
    ws = null
  }
  frameSink = null
}

/** Closes the interrupt recogniser, leaving the microphone alone. */
function closeBargeSocket() {
  bargeGen += 1
  if (bargeWs) {
    bargeWs.onmessage = null
    bargeWs.onopen = null
    bargeWs.onerror = null
    bargeWs.onclose = null
    try { bargeWs.close() } catch { /* already closed */ }
    bargeWs = null
  }
}

/** Releases the microphone and its capture graph. */
function closeCapture() {
  captureGen += 1
  capturePending = null
  frameSink = null
  streamer?.stop()
  streamer = null
  if (micStream) {
    micStream.getTracks().forEach((t) => t.stop())
    micStream = null
  }
}

/** Ends the whole capture side: both recognisers and the device. */
function stopCapture() {
  clearIdleExitTimer()
  closeDictationSocket()
  closeBargeSocket()
  closeCapture()
}

/**
 * Whether this mode is the one that turned read-aloud on.
 *
 * Recorded so `stop()` can put the switch back: the user asked for a
 * conversation, not for a permanent change to a setting they had deliberately
 * left off. Cleared on every stop, so a later start re-reads the real state.
 */
let autoSpeechTurnedOnByUs = false

/**
 * Ends the mode and releases every resource. Safe to call from anywhere.
 *
 * `notify: false` suppresses the "dialog closed" toast. The failure paths have
 * already shown a more specific reason, and `startFreeDialog`'s own reset would
 * otherwise announce "closed" a millisecond before "opened".
 */
export function stop(options: { notify?: boolean } = {}): void {
  const wasActive = phase.value !== 'idle'
  // Restore only if we are the ones who turned it on AND the user has not
  // turned it off in the meantime — re-enabling a switch they just disabled
  // would be worse than leaving the mode's change behind.
  const restoreSpeech = autoSpeechTurnedOnByUs && autoSpeech.enabled.value
  gen += 1
  clearSilenceTimer()
  clearIdleExitTimer()
  clearReopenTimer()
  clearSettleTimer()
  clearWakeLockWarningTimer()
  stopCapture()
  wakeLock.release(WAKE_LOCK_REASON)
  utterance = ''
  liveTranscript.value = ''
  turnInFlight = false
  phase.value = 'idle'
  autoSpeechTurnedOnByUs = false
  onInterrupt = null
  if (restoreSpeech) autoSpeech.setEnabled(false, { silent: true })
  if (options.notify !== false && wasActive) {
    useToast().show(
      gt(restoreSpeech ? 'chat.voice.freeDialogOffWithSpeech' : 'chat.voice.freeDialogOff'),
      { icon: 'mute', type: 'info', duration: 2000 },
    )
  }
}

/**
 * Arms the "a pause also sends" timer, restarting it whenever new words arrive.
 *
 * Driven by the TRANSCRIPT, not by the microphone level. A microphone in a
 * normal room never goes quiet — fan hum, a keyboard, a fridge two rooms away
 * all keep the RMS above zero — so a level-driven timer restarts on every
 * frame and the pause never fires. Recognition output only moves when something
 * was actually understood, which is the signal the setting is really about.
 */
function restartSilenceTimer() {
  clearSilenceTimer()
  if (tuning.silenceSendMs <= 0) return
  // Nothing to send yet: arming here would only burn a timer that expires into
  // an empty utterance.
  if (!utterance.trim()) return
  silenceTimer = setTimeout(() => {
    silenceTimer = null
    if (phase.value !== 'listening') return
    const text = utterance.trim()
    if (text) sendTurn(text)
  }, tuning.silenceSendMs)
}

/**
 * (Re)arms the "nothing heard for too long" timer.
 *
 * Unlike the pause timer this one does not send anything — it ends the session.
 * Armed when listening starts, pushed back by every recognised word, so it only
 * fires when the microphone has produced nothing at all for the configured span
 * (the user walked away, or left the mode on by accident).
 */
function restartIdleExitTimer() {
  clearIdleExitTimer()
  if (tuning.idleExitMs <= 0) return
  idleExitTimer = setTimeout(() => {
    idleExitTimer = null
    if (phase.value !== 'listening') return
    appLog.i(TAG, `no words for ${tuning.idleExitMs}ms, leaving continuous conversation`)
    stop({ notify: false })
    useToast().show(gt('chat.voice.freeDialogIdleExit'), { icon: 'mute', type: 'info', duration: 4000 })
  }, tuning.idleExitMs)
}

/**
 * Applies a new transcript, and re-arms the pause timer if it actually changed.
 *
 * Engines repeat the same cumulative partial while they chew on audio, so an
 * unconditional re-arm would push the deadline out on every repeat.
 */
function applyTranscript(next: string): void {
  if (next === utterance) return
  utterance = next
  liveTranscript.value = next
  restartSilenceTimer()
  restartIdleExitTimer()
}

/** Sends the current utterance as a turn and stops feeding the recogniser. */
function sendTurn(text: string) {
  const clean = text.trim()
  if (!clean) return
  clearSilenceTimer()
  utterance = ''
  liveTranscript.value = ''
  // Stop feeding dictation before the turn starts: the reply will be read
  // aloud, and frames left flowing into the recogniser would turn it into the
  // user's next message. The microphone itself stays open — closing it here is
  // what used to make the next playback start with a swallowed syllable.
  closeDictationSocket()
  phase.value = 'thinking'
  turnInFlight = true
  onSend?.(clean)
}

function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}${path}`
}

/**
 * Ensures the session's capture graph is running.
 *
 * Resolves true when frames are being produced (now or already). The device is
 * opened at most once per session; every later call is a no-op. Two concurrent
 * callers share one acquisition — a second `getUserMedia` here would leave a
 * second microphone track that nothing can reach.
 */
function ensureCapture(): Promise<boolean> {
  if (micStream && streamer) return Promise.resolve(true)
  if (capturePending) return capturePending
  const myGen = gen
  const myCaptureGen = ++captureGen
  const toast = useToast()
  const pending = (async () => {
    let stream: MediaStream
    try {
      stream = await acquireMic({ audio: { echoCancellation: tuning.echoCancellation, noiseSuppression: tuning.noiseSuppression, autoGainControl: tuning.autoGainControl } })
    } catch (e) {
      lastError.value = gt(classifyMicError(e) === 'denied' ? 'chat.voice.micDenied' : 'chat.voice.micUnavailable')
      toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
      appLog.e(TAG, `getUserMedia failed ${describeMicError(e)}`)
      // The reason is already on screen; a second "dialog closed" toast would
      // overwrite it (the toast slot holds one message).
      stop({ notify: false })
      return false
    }
    if (myGen !== gen || myCaptureGen !== captureGen) {
      // The session moved on while the device was being acquired: closing it
      // here is the only chance, since the release ran before it existed.
      stream.getTracks().forEach((t) => t.stop())
      return false
    }
    micStream = stream

    // A track can end without us stopping it — another app grabs the microphone
    // (a call, a recorder) or a Bluetooth headset disconnects. The sockets stay
    // open and the phase stays 'listening' while nothing is captured, so the
    // mode looks alive but hears nothing. `stop()` on our side does not fire
    // this event, so it only ever means the device went away.
    const audioTrack = stream.getAudioTracks?.()[0]
    audioTrack?.addEventListener('ended', () => {
      if (myGen !== gen || phase.value === 'idle') return
      lastError.value = gt('chat.voice.micLost')
      useToast().show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
      // The reason is already on screen; "dialog closed" would replace it.
      stop({ notify: false })
    })

    const s = new PcmStreamer({
      stream,
      tuning,
      onFrame: (frame) => frameSink?.(frame),
    })
    streamer = s
    void s.start().catch((err) => {
      appLog.e(TAG, 'pcm capture failed', err)
      lastError.value = gt('chat.voice.micUnavailable')
      toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
      stop({ notify: false })
    })
    return true
  })()
  capturePending = pending
  void pending.finally(() => {
    if (capturePending === pending) capturePending = null
  })
  return pending
}

/** Points the capture at the dictation recogniser. */
async function openListening(): Promise<void> {
  const myGen = gen
  const toast = useToast()
  // Defensive: a dictation socket from an earlier round must not survive (two
  // of them would each apply a transcript). The capture is deliberately NOT
  // touched — see "ONE microphone" in the header.
  closeDictationSocket()
  const myListenGen = listenGen
  closeBargeSocket()
  if (!(await ensureCapture())) return
  // A barge-in or a sent turn may have moved the phase on during the await.
  if (myGen !== gen || myListenGen !== listenGen) return
  const sock = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
  ws = sock
  sock.onmessage = (e) => {
    if (ws !== sock || myGen !== gen) return
    try {
      const msg = JSON.parse(e.data as string) as { type?: string; text?: string; final?: string; message?: string }
      if (msg.type === 'partial') {
        // Cumulative transcript so far: replace, never append.
        applyTranscript(msg.text ?? '')
        checkTrigger()
      } else if (msg.type === 'text' && msg.text) {
        applyTranscript(utterance + msg.text)
        checkTrigger()
      } else if (msg.type === 'done') {
        const final = (msg.final ?? '').trim()
        if (final) applyTranscript(final)
        checkTrigger()
      } else if (msg.type === 'error') {
        lastError.value = msg.message || gt('chat.voice.transcribeFailed')
        toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
        stop({ notify: false })
      }
    } catch { /* ignore malformed */ }
  }
  sock.onopen = () => {
    if (ws !== sock || myGen !== gen) return
    sock.send(JSON.stringify({ type: 'start', sampleRate: 16000, channels: 1, bits: 16, context: '' }))
    // Audio withheld while nothing was reading it belongs to the previous
    // phase; flushing it here would hand the assistant's tail to the
    // recogniser as if the user had said it.
    streamer?.resetGate()
    frameSink = (frame) => {
      if (ws === sock && sock.readyState === WebSocket.OPEN) sock.send(frame)
    }
  }
  sock.onerror = () => {
    if (ws !== sock || myGen !== gen) return
    lastError.value = gt('chat.voice.connectFailed')
    toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
    stop({ notify: false })
  }
  sock.onclose = () => {
    if (ws !== sock || myGen !== gen) return
    if (phase.value === 'listening') stop({ notify: false })
  }
  phase.value = 'listening'
  // No pause timer here: the pause is measured from the first recognised word
  // (see restartSilenceTimer), and the utterance is empty until one arrives.
  // The idle-exit timer does start now — "nothing was ever heard" is exactly
  // the case it exists for.
  restartIdleExitTimer()
}

/**
 * Looks for a trigger word at the end of what has been recognised so far.
 *
 * Runs on EVERY recognition update, not on "done": the streaming relay only
 * emits `done` after the client sends `end`, and free-dialog mode never sends
 * `end` while it is listening (that would close the session). Waiting for
 * `done` means the trigger path never fires at all and the mode degenerates to
 * "send after a 3 second pause".
 *
 * Running on partials is safe because the match is anchored to the END of the
 * transcript: a revision that adds words after the trigger simply stops
 * matching until the user says it again.
 */
function checkTrigger() {
  if (phase.value !== 'listening') return
  const text = utterance.trim()
  if (!text) return
  const { matched, text: rest } = extractTrigger(text, tuning.triggerWords)
  if (!matched) return
  if (rest) {
    sendTurn(rest)
    return
  }
  // A bare "发送" sends nothing — there was no content, and sending an empty
  // message would start a turn the user did not ask for. Clear it so the next
  // utterance does not carry the trigger word along.
  applyTranscript('')
}

/**
 * Points the capture at a recogniser that watches for an interruption.
 *
 * Recognition runs on the same socket protocol, but only the trigger decision
 * matters here — the transcript is discarded.
 */
async function openBargeListener(): Promise<void> {
  if (tuning.bargeIn === 'off') return
  // Idempotent: both the playback watcher and the "entered while a turn was
  // already running" path can land here, and two sockets would leak a
  // recogniser session nobody reads.
  closeBargeSocket()
  // A dictation socket that is still being opened must not land after this:
  // it would transcribe the reply as the user's next message.
  listenGen += 1
  const myGen = gen
  const myBargeGen = bargeGen
  bargeTriggered = false
  frameSink = null
  if (!(await ensureCapture())) return
  if (myGen !== gen || myBargeGen !== bargeGen || phase.value !== 'speaking') return
  const sock = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
  // Tracked at module scope: a local would leave the socket (and the engine
  // session behind it) open forever once playback ends.
  bargeWs = sock
  sock.onopen = () => {
    if (myGen !== gen || bargeWs !== sock) return
    sock.send(JSON.stringify({ type: 'start', sampleRate: 16000, channels: 1, bits: 16, context: '' }))
    streamer?.resetGate()
    frameSink = (frame) => {
      if (bargeWs === sock && sock.readyState === WebSocket.OPEN) sock.send(frame)
    }
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
      if (autoSpeech.isBusy()) {
        // Something is being generated or read out: muting it IS the interrupt.
        autoSpeech.stopAudio()
      } else {
        // The turn is still running and has produced no speech yet, so there is
        // nothing to mute — the turn itself has to be cancelled.
        onInterrupt?.()
      }
    } catch { /* ignore malformed */ }
  }
  sock.onclose = () => { /* the playback watcher owns the lifecycle */ }
}

/** Applies the server settings and starts listening. */
export async function startFreeDialog(opts: {
  sessionId: string
  onSend: (text: string) => void
  /** A turn is already running in this session. */
  busy?: boolean
  /** Cancels that running turn when the user interrupts it. */
  onInterrupt?: () => void
}): Promise<void> {
  // Reset without announcing: "closed" followed immediately by "opened" is
  // noise, and the toast slot only holds one message anyway.
  stop({ notify: false })
  // stop() released the lock, so take it again for this run.
  void wakeLock.acquire(WAKE_LOCK_REASON)
  warnIfScreenCannotStayAwake()
  sessionId = opts.sessionId
  onSend = opts.onSend
  onInterrupt = opts.onInterrupt ?? null
  lastError.value = ''
  tuning = readVoiceTuning(serverConfig.value as Record<string, unknown>)
  // The loop is built around the reply being read out: it waits for playback
  // before reopening the microphone, and interrupting only means anything while
  // something is being spoken. Starting with read-aloud off leaves a mode that
  // sends and listens with no voice in between — not what "对话" means.
  //
  // Opened SILENTLY: the toast slot is a single one, so the "read-aloud on"
  // message and the "dialog on" message would overwrite each other and the user
  // would only ever see the second. One combined message is shown below.
  autoSpeechTurnedOnByUs = !autoSpeech.enabled.value
  if (autoSpeechTurnedOnByUs) autoSpeech.setEnabled(true, { silent: true })

  if (opts.busy) {
    // A turn is already running. Opening the dictation path now would treat
    // whatever the user says as the NEXT message — but they are still waiting
    // for this answer, so what they want is to be able to cut in.
    //
    // `turnInFlight` covers the other half: a PREVIOUS reply may still be
    // playing. When it ends, settleAfterPlayback() must see a turn in flight
    // and wait, otherwise the microphone reopens while this turn is still
    // running and the user's words get queued behind the answer they are
    // waiting for.
    turnInFlight = true
    if (tuning.bargeIn === 'off') {
      // They asked not to be interruptible: wait the turn out, then listen.
      phase.value = 'thinking'
    } else {
      phase.value = 'speaking'
      void openBargeListener()
    }
    useToast().show(
      gt(tuning.bargeIn === 'off' ? 'chat.voice.freeDialogOnWaitingQuiet' : 'chat.voice.freeDialogOnWaiting'),
      { icon: 'chat', type: 'info', duration: 2500 },
    )
    return
  }

  await openListening()
  // A failed start has already shown a more specific reason and stopped itself;
  // only announce a mode that actually reached the microphone.
  if (phase.value !== 'listening') return
  useToast().show(
    gt(autoSpeechTurnedOnByUs ? 'chat.voice.freeDialogOnWithSpeech' : 'chat.voice.freeDialogOn'),
    { icon: 'chat', type: 'info', duration: 2500 },
  )
}

/** Called by the owner when a turn it started has finished (or failed). */
export function setFreeDialogThinking(thinking: boolean): void {
  turnInFlight = thinking
  if (thinking) {
    if (phase.value === 'listening') {
      closeDictationSocket()
      phase.value = 'thinking'
    }
    return
  }
  // Turn finished. Two ways out, and BOTH are needed:
  //   - playback starts (the watcher below flips the phase to 'speaking' and
  //     cancels this window), or
  //   - nothing is ever played (auto-speech off, the reply has no speakable
  //     text, TTS failed) and this window expires.
  // Without the window, "auto-speech on but nothing to say" leaves the phase in
  // 'thinking' forever: the button spins and the microphone never reopens.
  // Two phases can be waiting for a turn to end: 'thinking' (the usual one) and
  // 'speaking' (the mode was opened while a turn was already running, so there
  // was no 'thinking' edge to catch). Both need the SAME grace window: the TTS
  // POST is asynchronous, so reopening on the turn-end edge would transcribe
  // the first sentence of the reply as if the user had said it.
  if (phase.value !== 'thinking' && phase.value !== 'speaking') return
  // Playback already owns the microphone; its watcher resumes listening once it
  // ends, and cancels this window when it starts.
  if (autoSpeech.isBusy()) return
  clearSettleTimer()
  const myGen = gen
  settleTimer = setTimeout(() => {
    settleTimer = null
    if (myGen !== gen) return
    if (phase.value !== 'thinking' && phase.value !== 'speaking') return
    if (autoSpeech.isBusy()) return
    resumeListening()
  }, SETTLE_GRACE_MS)
}

/** Hands the microphone back to dictation after the reply has been dealt with. */
function resumeListening(): void {
  clearReopenTimer()
  const myGen = gen
  reopenTimer = setTimeout(() => {
    reopenTimer = null
    if (myGen !== gen || phase.value === 'idle' || phase.value === 'listening') return
    // Re-check: a queued speaker may have started another read-aloud inside the
    // settling window. Opening the recogniser then would transcribe it.
    if (autoSpeech.isBusy()) return
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
      // Playback started. Dictation must stop reading the microphone: it is
      // listening to the speakers, and the reply would come back as the user's
      // next message. The device itself stays open — reopening it here is what
      // used to swallow the first syllables of the reply on Android, and the
      // barge-in listener is about to read the same frames anyway.
      clearSettleTimer()
      if (phase.value === 'listening') closeDictationSocket()
      phase.value = 'speaking'
      void openBargeListener()
    } else if (!playing && wasPlaying) {
      closeBargeSocket()
      if (phase.value === 'speaking') settleAfterPlayback()
    }
  },
)

watch(
  () => autoSpeech.isBusy(),
  (busy, wasBusy) => {
    // Playback is coming: cancel the "nothing to say" window opened when the
    // turn ended, otherwise it fires mid-read-aloud and reopens the mic.
    if (busy) {
      clearSettleTimer()
      return
    }
    if (!wasBusy) return
    closeBargeSocket()
    if (phase.value === 'speaking' || phase.value === 'thinking') settleAfterPlayback()
  },
)

/**
 * Decides what follows a finished read-aloud: another turn is in flight, so
 * wait; another SPEAKER is queued, so stay in 'speaking'; otherwise reopen the
 * microphone.
 *
 * The middle case is not theoretical: a group chat, or any run where several
 * replies are read in sequence, hands off between items with `state` going
 * playing → synthesizing. Without the `isBusy()` check the mic opens in the
 * middle of the second item and transcribes it.
 */
function settleAfterPlayback(): void {
  if (turnInFlight) {
    phase.value = 'thinking'
    return
  }
  if (autoSpeech.isBusy()) {
    phase.value = 'speaking'
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
