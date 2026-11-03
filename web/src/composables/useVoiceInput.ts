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
// PCM capture for the streaming engine (AudioWorklet).
let audioCtx: AudioContext | null = null
let pcmNode: AudioWorkletNode | null = null
let pcmSource: MediaStreamAudioSourceNode | null = null

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

/** PCM rate the streaming engine is told to expect. */
const PCM_SAMPLE_RATE = 16000

/**
 * AudioWorklet processor: mixes to mono, resamples to `targetRate` with a box
 * filter and posts 100 ms Int16 frames — the streaming ASR wants raw PCM, and
 * MediaRecorder can only produce WebM/Opus.
 *
 * Kept as source text rather than a separate module so no bundler asset wiring
 * (and no CSP surprise) is involved: it is loaded from a Blob URL.
 */
const PCM_WORKLET_SOURCE = `
class ClawbenchPcm extends AudioWorkletProcessor {
  constructor(options) {
    super()
    const target = (options.processorOptions && options.processorOptions.targetRate) || 16000
    this.ratio = sampleRate / target
    this.acc = 0
    this.count = 0
    this.pos = 0
    this.out = []
    this.frameSize = Math.round(target / 10)
  }
  process(inputs) {
    const input = inputs[0]
    if (!input || !input[0]) return true
    const ch = input[0]
    for (let i = 0; i < ch.length; i++) {
      this.acc += ch[i]
      this.count++
      this.pos += 1
      if (this.pos >= this.ratio) {
        const v = this.acc / this.count
        this.out.push(Math.max(-32768, Math.min(32767, Math.round(v < 0 ? v * 0x8000 : v * 0x7fff))))
        this.acc = 0
        this.count = 0
        this.pos -= this.ratio
      }
    }
    if (this.out.length >= this.frameSize) {
      const pcm = Int16Array.from(this.out)
      this.out = []
      this.port.postMessage(pcm.buffer, [pcm.buffer])
    }
    return true
  }
}
registerProcessor('clawbench-pcm', ClawbenchPcm)
`

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

  const shortcutKey = () =>
    (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.shortcut_key'] as string | undefined ?? 'F9'
  const streaming = () =>
    Boolean((settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.streaming'] ?? false)
  const provider = () =>
    (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.provider'] as string | undefined ?? 'vllm'
  const language = () =>
    (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.language'] as string | undefined

  /**
   * The streaming WebSocket only carries raw 16 kHz PCM. The server picks the
   * protocol by provider capability, and only Doubao decodes PCM — every other
   * provider receives the bytes as a `recording.webm` upload, so the flag alone
   * must never select this path.
   */
  const usePcmStreaming = () => streaming() && provider() === 'doubao'

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
      mediaStream = await acquireMic()
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
        state.value = 'done'
        isRecording.value = false
        stopMediaStream()
        // Complete the documented 'done' → 'idle' transition so a new recording can start.
        state.value = 'idle'
      }
    }
    mediaRecorder.start()
  }

  async function startPcmCapture(): Promise<void> {
    if (!mediaStream) throw new Error('no media stream')
    audioCtx = new AudioContext({ sampleRate: PCM_SAMPLE_RATE })
    // A context created outside a user gesture starts suspended and would feed
    // nothing to the worklet.
    if (audioCtx.state === 'suspended') await audioCtx.resume()
    const url = URL.createObjectURL(new Blob([PCM_WORKLET_SOURCE], { type: 'application/javascript' }))
    try {
      await audioCtx.audioWorklet.addModule(url)
    } finally {
      URL.revokeObjectURL(url)
    }
    pcmSource = audioCtx.createMediaStreamSource(mediaStream)
    pcmNode = new AudioWorkletNode(audioCtx, 'clawbench-pcm', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      channelCount: 1,
      processorOptions: { targetRate: PCM_SAMPLE_RATE },
    })
    pcmNode.port.onmessage = (e: MessageEvent<ArrayBuffer>) => {
      if (ws && ws.readyState === WebSocket.OPEN) ws.send(e.data)
    }
    pcmSource.connect(pcmNode)
    // The worklet emits silence, but the graph only runs while it reaches the
    // destination.
    pcmNode.connect(audioCtx.destination)
  }

  function stopPcmCapture(): void {
    if (pcmNode) {
      pcmNode.port.onmessage = null
      pcmNode.disconnect()
      pcmNode = null
    }
    if (pcmSource) {
      pcmSource.disconnect()
      pcmSource = null
    }
    if (audioCtx) {
      void audioCtx.close().catch(() => { /* already closed */ })
      audioCtx = null
    }
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
          setPartial(msg.final ?? '')
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
  }
}
