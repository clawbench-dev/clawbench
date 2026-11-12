/**
 * useVoiceInput: speech-to-text (ASR), usable from anywhere.
 *
 * State machine: 'idle' → 'recording' → 'transcribing' → 'done' → 'idle'
 *
 * Streaming (cfg.STT.streaming=true): audio chunks are sent over a WebSocket
 * and incremental text is appended live. Non-streaming: the full recording is
 * POSTed on release and the result appears once.
 *
 * Where the text goes depends on the caret at the moment recording starts:
 *
 *   - a writable input/textarea owns focus → `startAt` / `startGlobal` writes
 *     into it at the caret (replacing the selection). This is the normal path
 *     for every field in the app, not just the chat box.
 *   - nothing writable is focused (password box, read-only view, terminal,
 *     plain page) → the text collects in `inputText` and the capsule banner
 *     shows it. The user then copies it, drops it into the chat box, or
 *     discards it; it is never silently lost.
 *
 * Recognized text is never auto-sent.
 */
import { computed, ref, shallowRef } from 'vue'
import { useSettingsConfig } from './useSettingsConfig'
import { useToast } from './useToast'
import { useDialog } from './useDialog'
import { appLog } from '@/utils/appLog'
import { acquireMic, classifyMicError, describeMicError, logMicEnv } from '@/utils/micCapture'
import {
  createVoiceTarget,
  findVoiceTarget,
  removeVoiceText,
  textBeforeTarget,
  writeVoiceText,
  type VoiceTarget,
} from '@/utils/voiceTarget'
import {
  VOICE_TUNING_DEFAULTS,
  buildMicConstraints,
  readSttField,
  readVoiceTuning,
  rmsFromByteTimeDomain,
  rmsThreshold,
  type VoiceTuning,
} from '@/utils/audioTuning'
import { PCM_SAMPLE_RATE, PcmStreamer, encodeWav } from '@/utils/pcmCapture'
import i18n from '@/i18n'

export type VoiceInputState = 'idle' | 'recording' | 'transcribing' | 'done'

/**
 * Where this recording's text is going.
 *
 *   'field'  — into a live input/textarea at the caret
 *   'buffer' — nowhere to type; the capsule owns it
 */
export type VoiceInputMode = 'idle' | 'field' | 'buffer'

// Module-level singleton refs (mirrors useAutoSpeech pattern).
const state = ref<VoiceInputState>('idle')
const inputText = ref('')
const error = ref('')
const isRecording = ref(false)
const mode = ref<VoiceInputMode>('idle')
const targetEl = shallowRef<HTMLElement | null>(null)
/** True once a target-less transcript is finished and waiting in the capsule. */
const draftPending = ref(false)

let mediaRecorder: MediaRecorder | null = null
let ws: WebSocket | null = null
let mediaStream: MediaStream | null = null
// The field this recording writes into, or null for the capsule path.
let target: VoiceTarget | null = null
// Text already in the input box when recording started; transcripts are
// composed as prefix + recognized text.
let voicePrefix = ''
// Set by stop() so a socket that is still connecting does not start capturing
// after the user has already released the button. Reset on every start().
let stopRequested = false
// Keeps the "已写入「XX」" pill on screen briefly after the field path finishes.
let fieldDoneTimer: ReturnType<typeof setTimeout> | null = null
// A finished capsule transcript that was set aside because the user started
// dictating into a field. It goes back to the capsule when that run ends —
// a pending draft is never thrown away by an unrelated recording.
let heldDraft = ''
// PCM capture for the streaming engine (band filter + worklet + voice gate).
let pcm: PcmStreamer | null = null
// PCM capture for the whole-file engines that cannot read WebM: frames are
// collected and encoded as WAV when the user stops.
let wavStreamer: PcmStreamer | null = null
let wavFrames: ArrayBuffer[] = []
// Level metering for the buffered (whole-file) path: whether any window of the
// recording rose above the speech threshold.
let sawSpeech = false
let bufferedCtx: AudioContext | null = null
let bufferedGateTimer: ReturnType<typeof setInterval> | null = null

/**
 * Tuning in effect for the NEXT capture.
 *
 * Read from the server config at `start()` rather than held as reactive state:
 * the microphone constraints and the gate threshold are baked into the audio
 * graph when it is built, so a change mid-recording could not take effect
 * anyway. A module-level value keeps every entry point (button, F9, free-dialog
 * mode) using the same numbers.
 */
let tuning: VoiceTuning = { ...VOICE_TUNING_DEFAULTS }

/**
 * CJK ideographs, kana, hangul and CJK/fullwidth punctuation.
 *
 * Used to decide whether a word separator belongs at a junction: Chinese,
 * Japanese and Korean do not separate words with spaces, so gluing two CJK
 * fragments must stay space-free.
 */
const CJK_CHAR_RE = /[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uf900-\ufaff\uff00-\uffef]/

/** Leading characters that already separate themselves from what precedes. */
const SELF_SEPARATING_LEAD_RE = /[,.!?;:%)\]}，。！？；：、）》」』…—]/

/**
 * Whether a space must be inserted where a new transcript meets the text
 * already in the box.
 *
 * The two sides are independent utterances (the buffered engine emits one
 * transcript per audio segment), so joining them verbatim runs English words
 * together. Nothing is inserted when the junction already carries whitespace,
 * when the transcript opens with punctuation, or between two CJK fragments.
 */
function needsJunctionSpace(prev: string, next: string): boolean {
  if (!prev || !next) return false
  const last = prev.slice(-1)
  const first = next.slice(0, 1)
  if (/\s/.test(last) || /\s/.test(first)) return false
  if (SELF_SEPARATING_LEAD_RE.test(first)) return false
  if (CJK_CHAR_RE.test(last) && CJK_CHAR_RE.test(first)) return false
  return true
}

function stopMediaStream() {
  if (mediaStream) {
    mediaStream.getTracks().forEach((t) => t.stop())
    mediaStream = null
  }
}

function pickMimeType(): string {
  if (typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported && MediaRecorder.isTypeSupported('audio/webm;codecs=opus')) {
    return 'audio/webm;codecs=opus'
  }
  return ''
}

function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}${path}`
}

export function useVoiceInput() {
  const settings = useSettingsConfig()
  const toast = useToast()
  const dialog = useDialog()

  // fail records the error and surfaces it to the user via a toast.
  function fail(msg: string): void {
    error.value = msg
    toast.show(msg, { icon: 'warning', type: 'error', duration: 6000 })
  }

  // `/api/config` is NESTED (`{"stt": {...}}`), not a flat map of dotted keys —
  // see `readSttField`. These used to index `['stt.streaming']`, which is
  // always undefined, so every engine setting silently kept its default: the
  // 流式识别 switch never turned streaming on and 识别语言 was never sent.
  const sttField = (key: string) =>
    readSttField(settings.serverConfig.value as Record<string, unknown> | undefined, key)

  const shortcutKey = () => (sttField('shortcut_key') as string | undefined) ?? 'F9'
  const provider = () => (sttField('provider') as string | undefined) ?? ''

  /**
   * Whether a recognition engine is configured.
   *
   * The touch mic button is hidden while this is false: an entry point whose
   * only possible outcome is "go configure something" is worse than no entry
   * point. The F9 shortcut still answers with the same hint, so the feature is
   * discoverable without a dead button sitting on screen.
   */
  const configured = computed(() => Boolean(provider()))

  // Each engine keeps its own streaming switch and language: they used to be
  // shared, so turning streaming on for Doubao also switched it on for the
  // self-hosted engine, and its language was overwritten by the other engine's.
  const engineField = (key: string): unknown => {
    const engine = sttField(provider())
    return engine && typeof engine === 'object' ? (engine as Record<string, unknown>)[key] : undefined
  }
  const streaming = () => Boolean(engineField('streaming') ?? false)
  const language = () => engineField('language') as string | undefined

  /** The tuning currently in effect (read by free-dialog mode as well). */
  const currentTuning = () => tuning

  /**
   * The streaming WebSocket only carries raw 16 kHz PCM. The server picks the
   * protocol by provider capability, and only the engines that decode PCM
   * (Doubao, Alibaba Cloud Paraformer, Tencent Cloud) are reached that way —
   * every other provider receives the bytes as a `recording.webm` upload, so
   * the flag alone must never select this path. Tencent also rejects WebM on
   * its whole-file API, which is why its streaming switch defaults to on.
   */
  const usePcmStreaming = () =>
    streaming() && (provider() === 'doubao' || provider() === 'alibaba' || provider() === 'tencent')

  /**
   * Whole-file engines that reject the WebM container the MediaRecorder
   * produces. Tencent's 一句话识别 decodes containers (wav/pcm/mp3/m4a/…), not
   * bare Opus frames, so its upload is built from the PCM capture instead.
   * Every other buffered engine takes the recorder's WebM as-is.
   */
  const needsWavUpload = () => provider() === 'tencent'

  async function toggle(prefix = '') {
    if (state.value === 'recording' || state.value === 'transcribing') {
      await stop()
    } else {
      await start(prefix)
    }
  }

  /** Toggle for a known field (the chat mic button, its long-press gesture). */
  async function toggleAt(el: Element | null) {
    if (state.value === 'recording' || state.value === 'transcribing') {
      await stop()
    } else {
      await startAt(el)
    }
  }

  /**
   * Starts dictation with no field: text collects in the capsule buffer.
   *
   * `prefix` is kept for callers that already hold text for that buffer (and
   * for the unit tests that drive the recogniser without a DOM field).
   */
  async function start(prefix = '') {
    // Low-level buffer entry (used by callers that already hold the text and
    // by the recogniser tests): a new recording starts from `prefix` alone.
    // The global shortcut goes through `startGlobal`, which continues a
    // pending capsule draft instead of replacing it.
    heldDraft = ''
    draftPending.value = false
    await begin(null, prefix)
  }

  /** Starts dictation into a specific field (chat mic button / long press). */
  async function startAt(el: Element | null) {
    const t = createVoiceTarget(el)
    await begin(t, t ? textBeforeTarget(t) : '')
  }

  /**
   * Starts dictation wherever the caret is.
   *
   * This is the global shortcut's entry point: a writable field takes the
   * text at its caret; anything else falls back to the capsule.
   */
  async function startGlobal() {
    const t = findVoiceTarget()
    if (t) {
      await begin(t, textBeforeTarget(t))
      return
    }
    // Nothing to type into. A transcript already waiting in the capsule is
    // continued rather than overwritten — losing dictated text because the
    // shortcut was pressed twice would be the worst possible outcome.
    await begin(null, draftPending.value ? inputText.value : '')
  }

  async function begin(t: VoiceTarget | null, prefix: string) {
    if (state.value !== 'idle') return
    // Cleared before the microphone opens: `stop()` may arrive while we are
    // still waiting for permission, and that release must not be forgotten.
    stopRequested = false
    // A draft set aside by an earlier field run comes back first, so it can be
    // continued (buffer) or set aside again (field) instead of being dropped.
    restoreHeldDraft()
    if (!t && !prefix && draftPending.value) prefix = inputText.value
    error.value = ''
    // Nothing configured means there is nowhere to send the audio. Say so before
    // asking for the microphone: recording a clip that can only fail is worse
    // than one clear sentence pointing at the setting.
    if (!provider()) {
      fail(i18n.global.t('chat.voice.notConfigured'))
      return
    }
    // navigator.mediaDevices only exists in a secure context (HTTPS or
    // localhost). If it's missing the page was served over plain HTTP on a
    // remote host, so the microphone is unreachable regardless of permission.
    if (!navigator.mediaDevices || typeof navigator.mediaDevices.getUserMedia !== 'function') {
      error.value = i18n.global.t('chat.voice.insecureContext')
      appLog.w('VoiceInput', 'voice input requires secure context (HTTPS or localhost)')
      void logMicEnv('VoiceInput')
      void dialog.alert(error.value, {
        title: i18n.global.t('chat.voice.unavailableTitle'),
        confirmText: i18n.global.t('chat.voice.gotIt'),
      })
      return
    }
    try {
      // Re-read the tuning on every start: the microphone constraints are baked
      // into the stream at open time, so a setting changed while nothing was
      // recording must be picked up here rather than at page load.
      tuning = readVoiceTuning(settings.serverConfig.value as Record<string, unknown> | undefined)
      mediaStream = await acquireMic({ audio: buildMicConstraints(tuning) })
    } catch (e) {
      // "Permission refused" and "the device did not start" need different
      // advice, so they get different messages.
      fail(i18n.global.t(classifyMicError(e) === 'denied' ? 'chat.voice.micDenied' : 'chat.voice.micUnavailable'))
      appLog.e('VoiceInput', `getUserMedia failed ${describeMicError(e)}`)
      void logMicEnv('VoiceInput')
      return
    }

    // Released while the microphone was still starting (a quick tap): start
    // nothing, or the recorder would run forever with no one holding the key.
    if (stopRequested) {
      stopRequested = false
      stopMediaStream()
      return
    }

    // A new recording starts fresh — never echo transcriptions from earlier
    // recordings (appendText would otherwise pile them up). `prefix` carries
    // whatever the user already typed, so dictation appends to it instead of
    // wiping the input box.
    target = t
    mode.value = t ? 'field' : 'buffer'
    targetEl.value = t?.el ?? null
    heldDraft = t && draftPending.value ? inputText.value : ''
    draftPending.value = false
    if (fieldDoneTimer) { clearTimeout(fieldDoneTimer); fieldDoneTimer = null }
    voicePrefix = prefix
    if (!t) inputText.value = prefix

    if (usePcmStreaming()) {
      startStreaming()
    } else if (needsWavUpload()) {
      startWavRecording()
    } else {
      startNonStreaming()
    }
  }

  function startNonStreaming() {
    state.value = 'recording'
    isRecording.value = true
    const chunks: Blob[] = []
    const mimeType = pickMimeType()
    mediaRecorder = new MediaRecorder(mediaStream!, mimeType ? { mimeType } : undefined)
    mediaRecorder.ondataavailable = (e) => { if (e.data.size > 0) chunks.push(e.data) }
    mediaRecorder.onstop = async () => {
      // The recording is complete once this fires, so the capture indicator can
      // go off while the upload runs — without cutting the final chunk.
      stopMediaStream()
      await uploadRecording(new Blob(chunks, { type: 'audio/webm' }), 'recording.webm')
    }
    mediaRecorder.start()
    startBufferedGate()
  }

  /**
   * Buffered recording for engines that cannot read WebM.
   *
   * The same PCM capture the streaming path uses, with the gate disabled: a
   * whole-file upload has no keepalive requirement, and dropping silence would
   * splice the speech together and shift every timestamp. The level callback
   * still drives `sawSpeech`, so a clip that never rose above the threshold is
   * skipped exactly like the recorder path.
   */
  function startWavRecording() {
    state.value = 'recording'
    isRecording.value = true
    wavFrames = []
    sawSpeech = !tuning.voiceGate
    const threshold = rmsThreshold(tuning)
    const streamer = new PcmStreamer({
      stream: mediaStream!,
      tuning: { ...tuning, voiceGate: false },
      onFrame: (frame) => { wavFrames.push(frame) },
      onLevel: (rms) => { if (!sawSpeech && rms >= threshold) sawSpeech = true },
    })
    wavStreamer = streamer
    streamer.start().catch((e) => {
      appLog.e('VoiceInput', 'pcm capture failed', e)
      fail(i18n.global.t('chat.voice.micUnavailable'))
      cancel()
    })
  }

  /**
   * Uploads one finished recording and appends the transcript.
   *
   * Shared by the recorder path and the WAV path so the silence skip, the error
   * handling and the state transition cannot drift between them.
   */
  async function uploadRecording(blob: Blob, filename: string) {
    state.value = 'transcribing'
    // A clip that never rose above the speech threshold is pure cost and
    // returns an empty transcript anyway, so it is dropped instead of uploaded.
    if (tuning.voiceGate && !sawSpeech) {
      finishRecording('')
      return
    }
    try {
      const form = new FormData()
      form.append('file', blob, filename)
      form.append('language', language() ?? 'zh')
      const resp = await fetch('/api/stt/transcribe', { method: 'POST', body: form })
      const data = await resp.json().catch(() => ({}))
      if (!resp.ok) throw new Error((data as Record<string, unknown>).error as string ?? 'transcribe failed')
      appendText((data as Record<string, unknown>).text as string ?? '')
    } catch (e) {
      fail(i18n.global.t('chat.voice.transcribeFailed'))
      appLog.e('VoiceInput', 'non-streaming transcribe failed', e)
    } finally {
      finishRecording()
    }
  }

  /** Completes the non-streaming state machine (shared by the silent-skip path). */
  function finishRecording(transcript?: string) {
    if (transcript) appendText(transcript)
    stopBufferedGate()
    state.value = 'done'
    isRecording.value = false
    stopMediaStream()
    // Complete the documented 'done' → 'idle' transition so a new recording can start.
    state.value = 'idle'
    completeTarget()
  }

  /**
   * Wraps up whichever destination this recording used.
   *
   * Field path: the text is already in the field, so the only thing left is to
   * keep `targetEl` around long enough for the "已写入" pill, then let it go.
   * Buffer path: hand the transcript to the capsule and keep it there — a
   * finished dictation must never evaporate because nothing was focused.
   */
  function completeTarget() {
    if (mode.value === 'field') {
      target = null
      if (fieldDoneTimer) clearTimeout(fieldDoneTimer)
      fieldDoneTimer = setTimeout(() => {
        fieldDoneTimer = null
        targetEl.value = null
        if (mode.value === 'field') mode.value = 'idle'
        restoreHeldDraft()
      }, 1600)
      return
    }
    mode.value = 'buffer'
    draftPending.value = inputText.value.trim() !== ''
    if (!draftPending.value) mode.value = 'idle'
  }

  /** Puts a set-aside capsule transcript back in front of the user. */
  function restoreHeldDraft() {
    if (!heldDraft) return
    inputText.value = heldDraft
    heldDraft = ''
    mode.value = 'buffer'
    draftPending.value = true
  }

  /**
   * Level metering for the buffered (whole-file) capture path.
   *
   * `sawSpeech` is what lets a recording with no speech skip the upload. The
   * meter is best-effort: without an analyser the flag starts true, because
   * "cannot tell" must not mean "silent".
   */
  function startBufferedGate() {
    sawSpeech = !tuning.voiceGate || !mediaStream || typeof AudioContext === 'undefined'
    if (sawSpeech) return
    try {
      bufferedCtx = new AudioContext()
      const source = bufferedCtx.createMediaStreamSource(mediaStream!)
      const analyser = bufferedCtx.createAnalyser()
      analyser.fftSize = 512
      source.connect(analyser)
      const buf = new Uint8Array(analyser.fftSize)
      const threshold = rmsThreshold(tuning)
      bufferedGateTimer = setInterval(() => {
        if (sawSpeech) return
        analyser.getByteTimeDomainData(buf)
        if (rmsFromByteTimeDomain(buf) >= threshold) sawSpeech = true
      }, 100)
    } catch (e) {
      appLog.w('VoiceInput', 'level meter unavailable, keeping the recording', e)
      sawSpeech = true
    }
  }

  function stopBufferedGate() {
    if (bufferedGateTimer !== null) {
      clearInterval(bufferedGateTimer)
      bufferedGateTimer = null
    }
    if (bufferedCtx) {
      void bufferedCtx.close().catch(() => { /* already closed */ })
      bufferedCtx = null
    }
  }

  async function startPcmCapture(): Promise<void> {
    if (!mediaStream) throw new Error('no media stream')
    const streamer = new PcmStreamer({
      stream: mediaStream,
      tuning,
      onFrame: (frame) => {
        if (ws && ws.readyState === WebSocket.OPEN) ws.send(frame)
      },
    })
    pcm = streamer
    await streamer.start()
  }

  function stopPcmCapture(): void {
    pcm?.stop()
    pcm = null
  }

  function startStreaming() {
    state.value = 'recording'
    isRecording.value = true
    const sock = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
    ws = sock
    // Every callback ignores a socket that is no longer the active one: stop()
    // aborts a still-connecting socket, and its late close must not tear down a
    // recording the user has started in the meantime.
    sock.onmessage = (e) => {
      if (ws !== sock) return
      try {
        const msg = JSON.parse(e.data as string) as {
          type?: string
          text?: string
          final?: string
          message?: string
        }
        if (msg.type === 'partial') {
          // Whole transcript so far: replace, never append — the engine revises
          // earlier words as it hears more audio.
          setPartial(msg.text ?? '')
        } else if (msg.type === 'text' && msg.text) {
          // Buffered-engine fallback emits deltas only.
          appendText(msg.text)
        } else if (msg.type === 'done') {
          // A final is authoritative when present. An empty one means the
          // server did not re-check the whole recording (stt.final_recheck is
          // off), so the accumulated text stands — clearing here would throw
          // the user's dictation away.
          if (msg.final) setPartial(msg.final)
          finalize()
        } else if (msg.type === 'error') {
          fail(msg.message || i18n.global.t('chat.voice.transcribeFailed'))
          cancel()
        }
      } catch { /* ignore malformed */ }
    }
    sock.onopen = () => {
      if (ws !== sock) return
      if (stopRequested) {
        // The button was released while the socket was still connecting: never
        // start capturing, and drop the socket so the engine has nothing to
        // transcribe.
        finalize()
        return
      }
      sock.send(JSON.stringify({
        type: 'start',
        sampleRate: PCM_SAMPLE_RATE,
        channels: 1,
        bits: 16,
        context: voicePrefix,
      }))
      startPcmCapture().catch((e) => {
        appLog.e('VoiceInput', 'pcm capture failed', e)
        fail(i18n.global.t('chat.voice.micUnavailable'))
        cancel()
      })
    }
    sock.onerror = () => {
      if (ws !== sock) return
      fail(i18n.global.t('chat.voice.connectFailed'))
      cancel()
    }
    sock.onclose = () => {
      if (ws !== sock) return
      if (state.value === 'recording' || state.value === 'transcribing') {
        cancel()
      }
    }
  }

  /**
   * Replaces the transcript while keeping whatever the user had typed.
   *
   * In a field this rewrites the exact range the previous partial occupied, so
   * the recogniser can revise earlier words without disturbing anything the
   * user typed around it.
   */
  function setPartial(text: string) {
    dropDeadTarget()
    if (target) {
      writeVoiceText(target, text)
      return
    }
    inputText.value = voicePrefix + text
  }

  function finalize() {
    stopPcmCapture()
    if (ws) { ws.close(); ws = null }
    stopMediaStream()
    state.value = 'done'
    isRecording.value = false
    // Complete the documented 'done' → 'idle' transition so a new recording can start.
    state.value = 'idle'
    completeTarget()
  }

  /**
   * How long the capture keeps running after the user lets go.
   *
   * The microphone pipeline sits between the air and `onFrame`: the device's own
   * buffer, echo cancellation / noise suppression, the resampler into the
   * worklet, the worklet's partial 100 ms frame and the message queue. Tearing
   * the graph down on pointer-up throws all of that away, so the recogniser
   * hears the last syllable cut short — reported as "the last couple of
   * characters are never recognized". The state flips to `transcribing` first,
   * so this reads as the normal wait for the final text rather than as extra
   * recording.
   */
  const RELEASE_TAIL_MS = 300

  async function stop() {
    stopRequested = true
    if (ws) {
      const sock = ws
      // Stop the capture indicator immediately; the microphone itself stays
      // open for the release tail below.
      state.value = 'transcribing'
      isRecording.value = false
      await new Promise((resolve) => window.setTimeout(resolve, RELEASE_TAIL_MS))
      // A cancel (or a finalize triggered by the engine) may have torn this
      // recording down while the tail was running.
      if (ws !== sock) return
      // Stop capture first so no frame lands after the end marker.
      stopPcmCapture()
      if (sock.readyState === WebSocket.OPEN) {
        sock.send(JSON.stringify({ type: 'end' }))
      } else if (sock.readyState === WebSocket.CONNECTING) {
        // Nothing was captured yet; abort the handshake instead of waiting for
        // it to open and then starting capture on a released button.
        sock.close()
      }
      stopMediaStream()
      return
    }
    if (wavStreamer) {
      // Capture first, then release the microphone: the last frame is already
      // in `wavFrames`, so there is nothing left to flush.
      const frames = wavFrames
      wavFrames = []
      wavStreamer.stop()
      wavStreamer = null
      stopMediaStream()
      // A tap that released before the worklet produced anything has no audio
      // to send; a header-only WAV would come back as an engine error.
      if (frames.length === 0) {
        finishRecording('')
        return
      }
      await uploadRecording(encodeWav(frames), 'recording.wav')
      return
    }
    if (mediaRecorder && mediaRecorder.state === 'recording') {
      // The recorder flushes its final chunk as it stops, so the microphone is
      // released in `onstop` rather than here: stopping the tracks first can
      // truncate the last utterance.
      mediaRecorder.stop()
    }
  }

  function cancel() {
    stopPcmCapture()
    if (wavStreamer) {
      wavStreamer.stop()
      wavStreamer = null
      wavFrames = []
    }
    stopBufferedGate()
    if (mediaRecorder && mediaRecorder.state === 'recording') {
      try { mediaRecorder.stop() } catch { /* noop */ }
    }
    stopMediaStream()
    if (ws) { ws.close(); ws = null }
    // Take back whatever this recording already wrote into a field, and drop
    // the pending capsule draft. Cancelling is an explicit "forget this".
    if (target) {
      removeVoiceText(target)
      target = null
    }
    if (fieldDoneTimer) { clearTimeout(fieldDoneTimer); fieldDoneTimer = null }
    targetEl.value = null
    draftPending.value = false
    mode.value = 'idle'
    state.value = 'idle'
    isRecording.value = false
    // Drop the live buffer transcript; a draft that predates this recording is
    // not part of what was cancelled.
    if (!heldDraft) inputText.value = ''
    restoreHeldDraft()
  }

  /**
   * A field can disappear mid-recording (the chat box is re-keyed when the
   * composer is rebuilt). Writing into a detached node would swallow the
   * transcript, so the text moves to the capsule instead of being lost.
   */
  function dropDeadTarget(): void {
    if (!target || target.el.isConnected) return
    const carried = target.applied
    target = null
    targetEl.value = null
    mode.value = 'buffer'
    voicePrefix = carried
    inputText.value = carried
  }

  function appendText(text: string) {
    if (!text) return
    dropDeadTarget()
    // Concatenate without rewriting either side: the transcript continues
    // whatever is already in the box, so nothing is trimmed. A separator is
    // added only where the junction needs one — joining verbatim glued English
    // words together ("Please review" + "the diff" → "Please reviewthe diff"),
    // because the buffered engine emits one transcript per audio segment and
    // each starts a new utterance.
    if (target) {
      const written = target.applied
      const prev = written || textBeforeTarget(target)
      const next = written + (needsJunctionSpace(prev, text) ? ' ' : '') + text
      writeVoiceText(target, next)
      return
    }
    const prev = inputText.value
    inputText.value = prev + (needsJunctionSpace(prev, text) ? ' ' : '') + text
  }

  function setState(s: VoiceInputState) { state.value = s }
  function setInputText(s: string) { inputText.value = s }

  /** Drops the finished capsule transcript (the × button). */
  function discardDraft() {
    inputText.value = ''
    draftPending.value = false
    if (mode.value === 'buffer') mode.value = 'idle'
  }

  function reset() {
    cancel()
    inputText.value = ''
    error.value = ''
  }

  return {
    state,
    inputText,
    error,
    isRecording,
    mode,
    targetEl,
    draftPending,
    configured,
    toggle,
    toggleAt,
    start,
    startAt,
    startGlobal,
    stop,
    cancel,
    reset,
    appendText,
    discardDraft,
    setState,
    setInputText,
    shortcutKey,
    currentTuning,
  }
}
