<template>
  <Teleport to="body">
    <Transition name="dlg">
      <div v-show="visible !== false" class="install-overlay" @click.self="handleClose">
      <div class="install-box">
        <div class="install-title">
          <template v-if="status === 'verifying'">{{ t('welcomeInfo.verifying') }} {{ backendName }}...</template>
          <template v-else-if="status === 'error' || status === 'notDetected'">{{ t('welcomeInfo.installFailed') }} {{ backendName }}</template>
          <template v-else-if="status === 'success'">{{ t('welcomeInfo.installSuccess') }} {{ backendName }}</template>
          <template v-else>{{ t('welcomeInfo.installing') }} {{ backendName }}...</template>
        </div>
        <div class="install-log" ref="logContainer">
          <div v-for="(line, i) in logLines" :key="i" class="log-line">{{ line }}</div>
          <div v-if="logLines.length === 0 && status === 'running'" class="log-waiting">
            <span class="waiting-spinner"></span>
            <span class="waiting-text">{{ t('welcomeInfo.preparing') }}</span>
          </div>
          <div v-if="logLines.length > 0 && status === 'running'" class="log-running-indicator">
            <span class="running-dot"></span>
            <span class="running-dot"></span>
            <span class="running-dot"></span>
          </div>
          <div v-if="status === 'verifying'" class="log-waiting">
            <span class="waiting-spinner"></span>
            <span class="waiting-text">{{ t('welcomeInfo.verifying') }}...</span>
          </div>
        </div>
        <div v-if="(effectiveInstallCmd || installCmd) && status !== 'success'" class="install-cmd-section">
          <div v-if="status === 'error' || status === 'notDetected'" class="install-hint">{{ t('welcomeInfo.manualInstallHint') }}</div>
          <code class="install-cmd">{{ effectiveInstallCmd || installCmd }}</code>
        </div>
        <div class="install-actions">
          <button class="dlg-btn dlg-cancel" @click="handleClose">
            {{ (status === 'error' || status === 'notDetected') ? t('common.close') : t('common.cancel') }}
          </button>
          <button v-if="status === 'error' || status === 'notDetected'" class="dlg-btn dlg-ok" @click="retry">
            {{ t('welcomeInfo.install') }}
          </button>
        </div>
      </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { appLog } from '@/utils/appLog'

const props = defineProps<{
  backendId: string
  backendName: string
  installCmd: string
  visible?: boolean
}>()

const emit = defineEmits<{
  close: []
  success: []
  failed: []
}>()

const { t } = useI18n()
const logLines = ref<string[]>([])
const status = ref<'running' | 'verifying' | 'success' | 'error' | 'notDetected'>('running')
const logContainer = ref<HTMLElement | null>(null)
const effectiveInstallCmd = ref(props.installCmd)
let abortController: AbortController | null = null
let retryCount409 = 0
let retryTimerId: ReturnType<typeof setTimeout> | null = null
const MAX_409_RETRIES = 10
const RETRY_409_BASE_MS = 1500

onMounted(() => {
  startInstall()
})

onUnmounted(() => {
  abortController?.abort()
  if (retryTimerId !== null) {
    clearTimeout(retryTimerId)
    retryTimerId = null
  }
})

async function startInstall() {
  // Only reset state on first call, not on 409 retries
  if (retryCount409 === 0) {
    logLines.value = []
  }
  status.value = 'running'
  abortController = new AbortController()

  try {
    const response = await fetch('/api/agents/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ backend_id: props.backendId }),
      signal: abortController.signal,
    })

    if (!response.ok) {
      const errText = await response.text().catch(() => '')
      // Auto-retry on 409 InstallInProgress — another install is finishing
      if (response.status === 409 && retryCount409 < MAX_409_RETRIES) {
        retryCount409++
        const delay = RETRY_409_BASE_MS * retryCount409
        logLines.value.push(`Another install in progress, retrying in ${delay / 1000}s...`)
        await new Promise<void>(r => { retryTimerId = setTimeout(() => { retryTimerId = null; r() }, delay) })
        return startInstall()
      }
      status.value = 'error'
      logLines.value.push(`HTTP ${response.status}: ${errText || response.statusText}`)
      emit('failed')
      return
    }

    // Parse SSE stream from response body
    const reader = response.body!.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let terminal = false

    while (!terminal) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || '' // keep incomplete line in buffer

      let currentEvent = ''
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          currentEvent = line.slice(7).trim()
        } else if (line.startsWith('data: ') && currentEvent) {
          const dataStr = line.slice(6)
          try {
            const data = JSON.parse(dataStr)
            if (currentEvent === 'install_log') {
              logLines.value.push(data.line || '')
              scrollToBottom()
              // Cap log lines to prevent unbounded memory growth
              if (logLines.value.length > 500) {
                logLines.value.splice(0, logLines.value.length - 400)
              }
            } else if (currentEvent === 'install_start') {
              if (data.command) {
                effectiveInstallCmd.value = data.command
              }
            } else if (currentEvent === 'install_success') {
              terminal = true
              break
            } else if (currentEvent === 'install_error') {
              status.value = 'error'
              logLines.value.push(data.error || 'Unknown error')
              emit('failed')
              terminal = true
              break
            }
          } catch {
            // ignore parse errors for non-JSON data lines
          }
          currentEvent = ''
        } else if (line.startsWith(': ')) {
          // heartbeat, ignore
        } else if (line.trim() === '') {
          // end of event, reset
          currentEvent = ''
        }
      }
    }
    // Release reader to allow the response body to be closed
    reader.releaseLock()
    if (!response.body!.locked) {
      response.body!.cancel().catch(() => {})
    }

    // If install_success was received, verify the agent is actually detected
    if (status.value === 'running') {
      await verifyAndFinish()
    }
  } catch (e: unknown) {
    if (e instanceof DOMException && e.name === 'AbortError') return
    status.value = 'error'
    const msg = e instanceof Error ? e.message : String(e)
    logLines.value.push(msg)
    appLog.w('AgentInstallDialog', 'install failed', e)
    emit('failed')
  }
}

/** After install_success, scan /api/agents to verify the backend is actually detected. */
async function verifyAndFinish() {
  status.value = 'verifying'
  logLines.value.push('')
  logLines.value.push('--- ' + t('welcomeInfo.verifying') + ' ---')
  scrollToBottom()

  try {
    const resp = await fetch('/api/agents')
    if (resp.ok) {
      const data = await resp.json()
      const agents: { backend?: string; id?: string }[] = data.agents || data || []
      const found = agents.some(a => (a.backend || a.id) === props.backendId)
      if (found) {
        status.value = 'success'
        logLines.value.push(t('welcomeInfo.installSuccess'))
        scrollToBottom()
        emit('success')
      } else {
        status.value = 'notDetected'
        logLines.value.push(t('welcomeInfo.notDetectedAfterInstall'))
        scrollToBottom()
        emit('failed')
      }
    } else {
      // API error — assume success since install_success was received
      status.value = 'success'
      logLines.value.push(t('welcomeInfo.installSuccess'))
      scrollToBottom()
      emit('success')
    }
  } catch {
    // Network error — assume success
    status.value = 'success'
    logLines.value.push(t('welcomeInfo.installSuccess'))
    scrollToBottom()
    emit('success')
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (logContainer.value) {
      logContainer.value.scrollTop = logContainer.value.scrollHeight
    }
  })
}

function retry() {
  retryCount409 = 0
  startInstall()
}

function handleClose() {
  // Only hide the dialog — do NOT abort the install.
  // The install keeps running so success/failed events will fire
  // and update the installing indicator on the button.
  // Abort is handled in onUnmounted when the component is actually destroyed.
  emit('close')
}
</script>

<style scoped>
.install-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 3000;
  padding: 0 20px;
}

.install-box {
  background: var(--bg-secondary, #fff);
  border-radius: 14px;
  padding: 18px 16px 14px;
  max-width: 420px;
  width: 100%;
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
  animation: dlg-in 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.install-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary, #1a1a1a);
  margin-bottom: 10px;
}

.install-log {
  flex: 1;
  min-height: 80px;
  max-height: 35vh;
  overflow-y: auto;
  background: var(--bg-primary, #1a1a2e);
  border: 1px solid var(--border-color, #333);
  border-radius: 8px;
  padding: 8px 10px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 11px;
  line-height: 1.5;
  color: var(--text-secondary, #aaa);
  margin-bottom: 12px;
}

.log-line {
  white-space: pre-wrap;
  word-break: break-all;
}

.log-waiting {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
}

.waiting-spinner {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid var(--border-color, #333);
  border-top-color: var(--accent-color, #0066cc);
  border-radius: 50%;
  animation: wait-spin 0.7s linear infinite;
  flex-shrink: 0;
}

@keyframes wait-spin {
  to { transform: rotate(360deg); }
}

.waiting-text {
  color: var(--text-muted, #666);
  font-size: 11px;
}

.log-running-indicator {
  display: inline-flex;
  gap: 3px;
  padding: 6px 0 2px;
  align-items: center;
}

.running-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--text-muted, #666);
  animation: dot-pulse 1.2s ease-in-out infinite;
}

.running-dot:nth-child(2) {
  animation-delay: 0.2s;
}

.running-dot:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes dot-pulse {
  0%, 80%, 100% { opacity: 0.3; transform: scale(0.8); }
  40% { opacity: 1; transform: scale(1.2); }
}

.install-cmd-section {
  margin-bottom: 12px;
  padding: 8px 10px;
  background: var(--bg-tertiary, #f0f0f0);
  border: 1px solid var(--border-color, #ddd);
  border-radius: 8px;
}

.install-hint {
  font-size: 12px;
  color: var(--text-secondary, #555);
  margin-bottom: 4px;
}

.install-cmd {
  display: block;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 11px;
  color: var(--text-primary, #1a1a1a);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 4px 8px;
  border-radius: 4px;
  word-break: break-all;
}

.install-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}

.dlg-btn {
  padding: 6px 16px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  border: none;
  cursor: pointer;
  transition: opacity 0.12s;
  -webkit-tap-highlight-color: transparent;
}

.dlg-btn:active { opacity: 0.7; }

.dlg-cancel {
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #555);
}

.dlg-ok {
  background: var(--accent-color, #0066cc);
  color: #fff;
}
</style>

<style>
@keyframes dlg-in {
  from { opacity: 0; transform: scale(0.9); }
  to { opacity: 1; transform: scale(1); }
}
</style>
