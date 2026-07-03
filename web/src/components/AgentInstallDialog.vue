<template>
  <Teleport to="body">
    <Transition name="dlg">
      <div class="install-overlay" @click.self="handleClose">
        <div class="install-box">
          <div class="install-title">
            {{ status === 'error' ? t('welcomeInfo.installFailed') : t('welcomeInfo.installing') }}
            {{ backendName }}...
          </div>
          <div class="install-log" ref="logContainer">
            <div v-for="(line, i) in logLines" :key="i" class="log-line">{{ line }}</div>
            <div v-if="logLines.length === 0 && status === 'running'" class="log-waiting">...</div>
          </div>
          <div v-if="status === 'error'" class="install-error-section">
            <div class="install-hint">{{ t('welcomeInfo.manualInstallHint') }}</div>
            <code class="install-cmd">{{ installCmd }}</code>
          </div>
          <div class="install-actions">
            <button class="dlg-btn dlg-cancel" @click="handleClose">
              {{ status === 'error' ? t('common.close') : t('common.cancel') }}
            </button>
            <button v-if="status === 'error'" class="dlg-btn dlg-ok" @click="retry">
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
}>()

const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const logLines = ref<string[]>([])
const status = ref<'running' | 'success' | 'error'>('running')
const logContainer = ref<HTMLElement | null>(null)
let abortController: AbortController | null = null

onMounted(() => {
  startInstall()
})

onUnmounted(() => {
  abortController?.abort()
})

async function startInstall() {
  logLines.value = []
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
      status.value = 'error'
      logLines.value.push(`HTTP ${response.status}: ${errText || response.statusText}`)
      return
    }

    // Parse SSE stream from response body
    const reader = response.body!.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    while (true) {
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
            } else if (currentEvent === 'install_success') {
              status.value = 'success'
              emit('success')
            } else if (currentEvent === 'install_error') {
              status.value = 'error'
              logLines.value.push(data.error || 'Unknown error')
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
  } catch (e: unknown) {
    if (e instanceof DOMException && e.name === 'AbortError') return
    status.value = 'error'
    const msg = e instanceof Error ? e.message : String(e)
    logLines.value.push(msg)
    appLog.w('AgentInstallDialog', 'install failed', e)
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
  startInstall()
}

function handleClose() {
  abortController?.abort()
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
  color: var(--text-muted, #666);
}

.install-error-section {
  margin-bottom: 12px;
  padding: 8px 10px;
  background: color-mix(in srgb, #d32f2f 8%, var(--bg-primary, #fff));
  border: 1px solid color-mix(in srgb, #d32f2f 20%, var(--border-color, #ddd));
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
