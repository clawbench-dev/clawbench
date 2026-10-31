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
import { getNative } from '@/utils/clawbenchNative'
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
  const chunkMs = () =>
    Number((settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.chunk_ms'] ?? 1000)
  const language = () =>
    (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.language'] as string | undefined

  async function toggle(prefix = '') {
    if (state.value === 'recording' || state.value === 'transcribing') {
      await stop()
    } else {
      await start(prefix)
    }
  }

  /**
   * Logs the environment a failed getUserMedia ran in. Without this the only
   * trace is `getUserMedia failed {}` — a DOMException has no enumerable own
   * properties, so the error name (NotAllowedError vs NotFoundError vs
   * NotReadableError) and the installed app version are both lost.
   */
  async function logVoiceEnv(): Promise<void> {
    let appVersion = 'n/a'
    const native = getNative()
    if (native?.getAppVersion) {
      try {
        appVersion = (await native.getAppVersion()) ?? 'n/a'
      } catch { /* bridge unavailable */ }
    }

    // Permissions API state tells "the browser thinks it may capture" apart
    // from "the device refuses to start" — the two failures need completely
    // different fixes.
    let permState: string
    try {
      const status = await navigator.permissions?.query({ name: 'microphone' as PermissionName })
      permState = status?.state ?? 'unknown'
    } catch {
      permState = 'error'
    }

    // Android only: record natively to find out whether the OS lets this app
    // use the mic at all. A WebView-side failure with a working native probe
    // means the fault is inside Chromium's audio stack, not the permission.
    let nativeMic = 'n/a'
    if (native?.probeMicrophone) {
      try {
        nativeMic = (await native.probeMicrophone()) ?? 'n/a'
      } catch {
        nativeMic = 'error'
      }
    }

    // Enumerate inputs: 0 devices means the OS exposes no microphone to the
    // WebView; missing labels means capture permission is not really in effect
    // even though the settings page shows it granted.
    let inputs: string
    try {
      const devices = await navigator.mediaDevices.enumerateDevices()
      const audioInputs = devices.filter((d) => d.kind === 'audioinput')
      inputs = `${audioInputs.length}[${audioInputs.map((d) => d.label || '<no-label>').join('|')}]`
    } catch {
      inputs = 'error'
    }

    appLog.i(
      'VoiceInput',
      `env secure=${window.isSecureContext} proto=${window.location.protocol} host=${window.location.host} ` +
        `mediaDevices=${typeof navigator.mediaDevices} permission=${permState} inputs=${inputs} ` +
        `nativeMic=${nativeMic} app=${appVersion}`,
    )
  }

  /**
   * Opens the microphone.
   *
   * NotReadableError means the permission gate passed but the audio device
   * refused to start (busy, or the HAL was not ready yet). One short retry
   * covers the transient case; a persistent failure is a system-level block
   * and must not be retried forever.
   */
  async function acquireMic(): Promise<MediaStream> {
    try {
      return await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (e) {
      if ((e as { name?: string } | null)?.name !== 'NotReadableError') throw e
      await new Promise((resolve) => setTimeout(resolve, 400))
      return navigator.mediaDevices.getUserMedia({ audio: true })
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
      void logVoiceEnv()
      void dialog.alert(error.value, {
        title: i18n.global.t('chat.voice.unavailableTitle'),
        confirmText: i18n.global.t('chat.voice.gotIt'),
      })
      return
    }
    try {
      mediaStream = await acquireMic()
    } catch (e) {
      const err = e as { name?: string; message?: string } | null
      // NotAllowedError = permission refused. Anything else (NotReadableError,
      // NotFoundError) means the OS let us through but the device did not
      // start, so telling the user to "grant permission" sends them the wrong way.
      fail(i18n.global.t(err?.name === 'NotAllowedError' ? 'chat.voice.micDenied' : 'chat.voice.micUnavailable'))
      appLog.e('VoiceInput', `getUserMedia failed name=${err?.name ?? 'unknown'} message=${err?.message ?? String(e)}`)
      void logVoiceEnv()
      return
    }

    // A new recording starts fresh — never echo transcriptions from earlier
    // recordings (appendText would otherwise pile them up with '\n' separators).
    // `prefix` carries whatever the user already typed, so dictation appends to
    // it instead of wiping the input box.
    inputText.value = prefix

    if (streaming()) {
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

  function startStreaming() {
    state.value = 'recording'
    isRecording.value = true
    const mimeType = pickMimeType()
    mediaRecorder = new MediaRecorder(mediaStream!, mimeType ? { mimeType } : undefined)
    mediaRecorder.ondataavailable = (e) => {
      if (e.data.size > 0 && ws && ws.readyState === WebSocket.OPEN) {
        ws.send(e.data)
      }
    }
    ws = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data as string) as { type?: string; text?: string; final?: string }
        if (msg.type === 'text' && msg.text) appendText(msg.text)
        if (msg.type === 'done') {
          if (msg.final) inputText.value = msg.final
          finalize()
        }
      } catch { /* ignore malformed */ }
    }
    ws.onopen = () => mediaRecorder!.start(chunkMs())
    ws.onerror = () => {
      fail(i18n.global.t('chat.voice.connectFailed'))
      cancel()
    }
    ws.onclose = () => {
      if (state.value === 'recording') {
        cancel()
      }
    }
    mediaRecorder.onstop = () => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'end' }))
      }
    }
  }

  function finalize() {
    if (ws) { ws.close(); ws = null }
    stopMediaStream()
    state.value = 'done'
    isRecording.value = false
    // Complete the documented 'done' → 'idle' transition so a new recording can start.
    state.value = 'idle'
  }

  async function stop() {
    if (mediaRecorder && mediaRecorder.state === 'recording') {
      mediaRecorder.stop()
    }
  }

  function cancel() {
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
    // Concatenate as-is: the transcript continues whatever is already in the
    // box, and inserting a separator (or trimming the existing text) would
    // rewrite input the user typed. Recognized text carries its own punctuation.
    inputText.value += text
  }

  function setState(s: VoiceInputState) { state.value = s }
  function setInputText(s: string) { inputText.value = s }

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
    shortcutKey,
  }
}
