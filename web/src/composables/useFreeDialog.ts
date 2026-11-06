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
import { useWakeLock } from '@/composables/useWakeLock'
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
/** Cancels a turn that is still running when the user interrupts it. */
let onInterrupt: (() => void) | null = null
/** Whether a turn is currently in flight (set by the owner via `setThinking`). */
let turnInFlight = false
let silenceTimer: ReturnType<typeof setTimeout> | null = null
/** Fires when the microphone has produced no recognised words for too long. */
let idleExitTimer: ReturnType<typeof setTimeout> | null = null
let reopenTimer: ReturnType<typeof setTimeout> | null = null
/** Barge-in listener state: the mic is open only while the assistant talks. */
let bargeStreamer: PcmStreamer | null = null
let bargeStream: MediaStream | null = null
let bargeWs: WebSocket | null = null
let bargeTriggered = false
/**
 * Invalidation counter for the interrupt listener.
 *
 * `openBargeListener` awaits `getUserMedia`, so two callers can be inside it at
 * once (the playback watcher and "opened while a turn was already running").
 * Clearing the module-level refs before the await only stops the PREVIOUS
 * listener; without this counter the slower call would overwrite the faster
 * one's refs and leak a microphone track plus a recogniser socket that nothing
 * can reach any more.
 */
let bargeGen = 0
/**
 * Grace window after a turn ends, while we wait to find out whether the reply
 * is going to be read aloud.
 *
 * The TTS POST is asynchronous, so `loading` turns false BEFORE playback
 * starts. Reopening the microphone on that edge would transcribe the first
 * sentence of the reply; waiting forever would leave the loop stuck whenever
 * there is nothing to say (an empty reply, a TTS failure, `auto-speech` on but
 * the message having no speakable text). A short window resolves both: playback
 * starting cancels it, and if it fires with nothing playing, listening resumes.
 */
let settleTimer: ReturnType<typeof setTimeout> | null = null
const SETTLE_GRACE_MS = 1500
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
  clearIdleExitTimer()
  streamer?.stop()
  streamer = null
  if (micStream) {
    micStream.getTracks().forEach((t) => t.stop())
    micStream = null
  }
  closeSocket()
}

function stopBargeListener() {
  bargeGen += 1
  bargeStreamer?.stop()
  bargeStreamer = null
  if (bargeWs) {
    bargeWs.onmessage = null
    bargeWs.onopen = null
    bargeWs.onerror = null
    bargeWs.onclose = null
    try { bargeWs.close() } catch { /* already closed */ }
    bargeWs = null
  }
  if (bargeStream) {
    bargeStream.getTracks().forEach((t) => t.stop())
    bargeStream = null
  }
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
  stopBargeListener()
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
  // Defensive: every caller already stops the capture, but a leaked second
  // capture means a microphone indicator that never goes out and frames being
  // written into a socket nobody reads. Idempotent, so it costs nothing.
  // The interrupt listener is included on purpose: entering from "waiting to
  // interrupt" reaches here without passing through the playback watcher, which
  // is the other place that closes it.
  stopCapture()
  stopBargeListener()
  // Held locally until the generation check passes: assigning straight to
  // `micStream` would let a slow call clobber a newer one's stream.
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
    return
  }
  if (myGen !== gen) {
    // The mode was stopped while getUserMedia was pending: stopCapture() ran
    // before this stream existed, so closing it here is the only chance.
    stream.getTracks().forEach((t) => t.stop())
    return
  }
  micStream = stream

  // A track can end without us stopping it — another app grabs the microphone
  // (a call, a recorder) or a Bluetooth headset disconnects. The socket stays
  // open and the phase stays 'listening' while nothing is captured, so the mode
  // looks alive but hears nothing. `stop()` on our side does not fire this
  // event, so it only ever means the device went away.
  const audioTrack = stream.getAudioTracks?.()[0]
  audioTrack?.addEventListener('ended', () => {
    if (myGen !== gen || phase.value === 'idle') return
    lastError.value = gt('chat.voice.micLost')
    useToast().show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
    // The reason is already on screen; "dialog closed" would replace it.
    stop({ notify: false })
  })

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
    const s = new PcmStreamer({
      stream: micStream!,
      tuning,
      onFrame: (frame) => {
        if (ws && ws.readyState === WebSocket.OPEN) ws.send(frame)
      },
    })
    streamer = s
    void s.start().catch((err) => {
      appLog.e(TAG, 'pcm capture failed', err)
      lastError.value = gt('chat.voice.micUnavailable')
      toast.show(lastError.value, { icon: 'warning', type: 'error', duration: 6000 })
      stop({ notify: false })
    })
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
 * Opens a short-lived microphone during playback so the user can cut the
 * assistant off.
 *
 * Recognition runs on the same socket protocol, but only the trigger decision
 * matters here — the transcript is discarded.
 */
async function openBargeListener(): Promise<void> {
  if (tuning.bargeIn === 'off') return
  // Idempotent: both the playback watcher and the "entered while a turn was
  // already running" path can land here, and a second capture would leak a
  // microphone track (the indicator stays on with nobody reading it).
  stopBargeListener()
  const myGen = gen
  const myBargeGen = bargeGen
  bargeTriggered = false
  // Local until the generation check passes — a slow call must not overwrite
  // the stream a newer listener already installed.
  let stream: MediaStream
  try {
    stream = await acquireMic({ audio: { echoCancellation: tuning.echoCancellation, noiseSuppression: tuning.noiseSuppression, autoGainControl: tuning.autoGainControl } })
  } catch {
    // Barge-in is an optimisation; failing to open the microphone must not
    // break the read-aloud itself.
    return
  }
  if (myGen !== gen || myBargeGen !== bargeGen || phase.value !== 'speaking') {
    stream.getTracks().forEach((t) => t.stop())
    return
  }
  bargeStream = stream
  const sock = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
  // Tracked at module scope: a local would leave the socket (and the engine
  // session behind it) open forever once playback ends.
  bargeWs = sock
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
      stopCapture()
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

/** Reopens the microphone after the reply has been dealt with. */
function resumeListening(): void {
  clearReopenTimer()
  const myGen = gen
  reopenTimer = setTimeout(() => {
    reopenTimer = null
    if (myGen !== gen || phase.value === 'idle' || phase.value === 'listening') return
    // Re-check: a queued speaker may have started another read-aloud inside the
    // 500 ms window. Opening the microphone then would transcribe the assistant.
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
      // Playback started. Whatever the phase was, the dictation capture must go
      // first: it is listening to the speakers, and leaving it running while a
      // SECOND capture opens would leak a microphone track (the indicator stays
      // on) and feed the reply back into the recogniser. This happens whenever
      // the user taps read-aloud on some message while the mode is listening.
      clearSettleTimer()
      if (phase.value === 'listening') stopCapture()
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
    // Playback is coming: cancel the "nothing to say" window opened when the
    // turn ended, otherwise it fires mid-read-aloud and reopens the mic.
    if (busy) {
      clearSettleTimer()
      return
    }
    if (!wasBusy) return
    stopBargeListener()
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
