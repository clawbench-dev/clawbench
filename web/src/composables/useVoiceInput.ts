/**
 * useVoiceInput: speech-to-text (ASR) for the chat input box.
 *
 * State machine: 'idle' → 'recording' → 'transcribing' → 'done' → 'idle'
 *
 * Streaming (cfg.STT.streaming=true): audio chunks are sent over a WebSocket
 * and incremental text is appended live. Non-streaming: the full recording is
 * POSTed on release and the result appears once.
 *
 * Recognized text is appended to `inputText` (the chat input box); it is never
 * auto-sent.
 */
import { ref } from 'vue'
import { useSettingsConfig } from './useSettingsConfig'
import { useToast } from './useToast'
import { useDialog } from './useDialog'
import { appLog } from '@/utils/appLog'
import { acquireMic, classifyMicError, describeMicError, logMicEnv } from '@/utils/micCapture'
import {
  VOICE_TUNING_DEFAULTS,
  buildMicConstraints,
  readSttField,
  readVoiceTuning,
  rmsFromByteTimeDomain,
  rmsThreshold,
  type VoiceTuning,
} from '@/utils/audioTuning'
import { PCM_SAMPLE_RATE, PcmStreamer } from '@/utils/pcmCapture'
import i18n from '@/i18n'

export type VoiceInputState = 'idle' | 'recording' | 'transcribing' | 'done'

// Module-level singleton refs (mirrors useAutoSpeech pattern).
const state = ref<VoiceInputState>('idle')
const inputText = ref('')
const error = ref('')
const isRecording = ref(false)
let mediaRecorder: MediaRecorder | null = null
let ws: WebSocket | null = null
let mediaStream: MediaStream | null = null
// Text already in the input box when recording started; transcripts are
// composed as prefix + recognized text.
let voicePrefix = ''
// Set by stop() so a socket that is still connecting does not start capturing
// after the user has already released the button. Reset on every start().
let stopRequested = false
// PCM capture for the streaming engine (band filter + worklet + voice gate).
let pcm: PcmStreamer | null = null
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
  const provider = () => (sttField('provider') as string | undefined) ?? 'vllm'

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
   * (Doubao, Alibaba Cloud Paraformer) are reached that way — every other
   * provider receives the bytes as a `recording.webm` upload, so the flag alone
   * must never select this path.
   */
  const usePcmStreaming = () => streaming() && (provider() === 'doubao' || provider() === 'alibaba')

  async function toggle(prefix = '') {
    if (state.value === 'recording' || state.value === 'transcribing') {
      await stop()
    } else {
      await start(prefix)
    }
  }

  async function start(prefix = '') {
    if (state.value !== 'idle') return
    error.value = ''
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

    // A new recording starts fresh — never echo transcriptions from earlier
    // recordings (appendText would otherwise pile them up). `prefix` carries
    // whatever the user already typed, so dictation appends to it instead of
    // wiping the input box.
    voicePrefix = prefix
    inputText.value = prefix
    stopRequested = false

    if (usePcmStreaming()) {
      startStreaming()
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
      state.value = 'transcribing'
      // The recording is complete once this fires, so the capture indicator can
      // go off while the upload runs — without cutting the final chunk.
      stopMediaStream()
      const blob = new Blob(chunks, { type: 'audio/webm' })
      // The buffered path cannot skip silence frame-by-frame (there is no frame
      // boundary to decide on) — the whole recording is one upload. A clip that
      // never rose above the speech threshold is pure cost and returns an empty
      // transcript anyway, so it is dropped here instead of being uploaded.
      if (tuning.voiceGate && !sawSpeech) {
        finishRecording('')
        return
      }
      try {
        const form = new FormData()
        form.append('file', blob, 'recording.webm')
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
    mediaRecorder.start()
    startBufferedGate()
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

  /** Replaces the transcript while keeping whatever the user had typed. */
  function setPartial(text: string) {
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
  }

  async function stop() {
    stopRequested = true
    if (ws) {
      // Streaming: stop capture first so no frame lands after the end marker.
      stopPcmCapture()
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'end' }))
      } else if (ws.readyState === WebSocket.CONNECTING) {
        // Nothing was captured yet; abort the handshake instead of waiting for
        // it to open and then starting capture on a released button.
        ws.close()
      }
      // Release the microphone now — waiting for the engine's final result
      // would leave the capture indicator on after the user let go.
      stopMediaStream()
      state.value = 'transcribing'
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
    stopBufferedGate()
    if (mediaRecorder && mediaRecorder.state === 'recording') {
      try { mediaRecorder.stop() } catch { /* noop */ }
    }
    stopMediaStream()
    if (ws) { ws.close(); ws = null }
    state.value = 'idle'
    isRecording.value = false
  }

  function appendText(text: string) {
    if (!text) return
    // Concatenate without rewriting either side: the transcript continues
    // whatever is already in the box, so nothing is trimmed. A separator is
    // added only where the junction needs one — joining verbatim glued English
    // words together ("Please review" + "the diff" → "Please reviewthe diff"),
    // because the buffered engine emits one transcript per audio segment and
    // each starts a new utterance.
    const prev = inputText.value
    inputText.value = prev + (needsJunctionSpace(prev, text) ? ' ' : '') + text
  }

  function setState(s: VoiceInputState) { state.value = s }
  function setInputText(s: string) { inputText.value = s }

  /**
   * Replace the text this dictation started from.
   *
   * `start(prefix)` snapshots the input box in TWO places: `voicePrefix` (the
   * PCM path rebuilds `voicePrefix + text` on every partial) and `inputText`
   * (the buffered path appends to it). If the box is emptied while a recording
   * is still in flight — the message was just sent — that snapshot is stale:
   * the transcript that lands afterwards would put the sent text back into the
   * box, and the next tap would send it a second time.
   *
   * Callers therefore re-point it at the box's new content (`''` after a send,
   * the restored text after a failed one). Deliberately not part of `cancel()`:
   * here the recording finishes normally and its transcript must still arrive —
   * just without the old text in front of it.
   */
  function setStartText(text: string) {
    voicePrefix = text
    inputText.value = text
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
    toggle,
    start,
    stop,
    cancel,
    reset,
    appendText,
    setState,
    setInputText,
    setStartText,
    shortcutKey,
    currentTuning,
  }
}
