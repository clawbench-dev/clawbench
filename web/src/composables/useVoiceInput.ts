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
import { appLog } from '@/utils/appLog'

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

  const shortcutKey = () =>
    (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.shortcut_key'] as string | undefined ?? 'F9'
  const streaming = () =>
    Boolean((settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.streaming'] ?? false)
  const chunkMs = () =>
    Number((settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.chunk_ms'] ?? 1000)
  const language = () =>
    (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.language'] as string | undefined

  async function toggle() {
    if (state.value === 'recording' || state.value === 'transcribing') {
      await stop()
    } else {
      await start()
    }
  }

  async function start() {
    if (state.value !== 'idle') return
    error.value = ''
    try {
      mediaStream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (e) {
      error.value = '麦克风权限被拒绝或不可用'
      appLog.e('VoiceInput', 'getUserMedia failed', e)
      return
    }

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
        error.value = '语音识别失败'
        appLog.e('VoiceInput', 'non-streaming transcribe failed', e)
      } finally {
        state.value = 'done'
        isRecording.value = false
        stopMediaStream()
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
      error.value = '语音识别连接失败'
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
    inputText.value = (inputText.value.trim() ? inputText.value.trim() + '\n' : '') + text
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
