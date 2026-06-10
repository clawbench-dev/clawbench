<template>
  <ModalDialog :open="show" :zIndex="2500" :title="t('chat.metadata.title')" @close="$emit('close')">
    <div class="metadata-content">
      <div v-if="messageId" class="metadata-item metadata-copyable" @click="copyValue(String(messageId), $event)">
        <span class="metadata-label">{{ t('chat.metadata.messageId') }}</span>
        <div class="metadata-value-wrap">
          <span class="metadata-value metadata-session-id metadata-value-copyable">{{ messageId }}</span>
          <button class="metadata-copy-btn" @click.stop="copyValue(String(messageId), $event)" :title="t('chat.metadata.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
      <div v-if="createdAt" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.time') }}</span>
        <span class="metadata-value">{{ formatDetailTime(createdAt) }} <span class="metadata-relative-time">{{ formatRelativeTime(createdAt) }}</span></span>
      </div>
      <div v-if="relatedFile" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.relatedFile') }}</span>
        <span class="metadata-value metadata-value-copyable" @click="copyValue(relatedFile, $event)">{{ relatedFile }}</span>
      </div>
      <div v-if="backend" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.backend') }}</span>
        <span class="metadata-value">{{ backend }}</span>
      </div>
      <div v-if="data.transport" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.transport') }}</span>
        <span class="metadata-value">{{ data.transport === 'cli' ? 'CLI' : 'ACP' }}</span>
      </div>
      <div v-if="data.mode" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.mode') }}</span>
        <span class="metadata-value">{{ data.mode }}</span>
      </div>
      <div v-if="data.thinkingEffort" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.thinkingEffort') }}</span>
        <span class="metadata-value">{{ data.thinkingEffort }}</span>
      </div>
      <div v-if="data.model" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.model') }}</span>
        <span class="metadata-value">{{ data.model }}</span>
      </div>
      <div v-if="data.inputTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.inputTokens') }}</span>
        <span class="metadata-value">{{ data.inputTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.outputTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.outputTokens') }}</span>
        <span class="metadata-value">{{ data.outputTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.wallMs" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.wallDuration') }}</span>
        <span class="metadata-value">{{ formatDuration(data.wallMs) }}</span>
      </div>
      <div v-if="data.durationMs" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.duration') }}</span>
        <span class="metadata-value">{{ (data.durationMs / 1000).toFixed(2) }}s</span>
      </div>
      <div v-if="data.costUsd" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cost') }}</span>
        <span class="metadata-value">${{ data.costUsd.toFixed(6) }}</span>
      </div>
      <div v-if="sessionId" class="metadata-item metadata-copyable" @click="copyValue(sessionId, $event)">
        <span class="metadata-label">{{ t('chat.metadata.sessionId') }}</span>
        <div class="metadata-value-wrap">
          <span class="metadata-value metadata-session-id metadata-value-copyable">{{ sessionId }}</span>
          <button class="metadata-copy-btn" @click.stop="copyValue(sessionId, $event)" :title="t('chat.metadata.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
      <div class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.ragIndexed') }}</span>
        <span class="metadata-value" :class="indexed ? 'metadata-indexed-yes' : 'metadata-indexed-no'">{{ indexed ? t('chat.metadata.indexedYes') : t('chat.metadata.indexedNo') }}</span>
      </div>
      <div v-if="data.stopReason" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.stopReason') }}</span>
        <span class="metadata-value">{{ data.stopReason }}</span>
      </div>
      <div v-if="data.isError" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.error') }}</span>
        <span class="metadata-value metadata-error">{{ data.errorMessage || t('chat.metadata.unknownError') }}</span>
      </div>
    </div>
  </ModalDialog>
</template>

<script setup>
import { Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useToast } from '@/composables/useToast.ts'
import { formatDuration, formatRelativeTime } from '@/utils/format.ts'

const { t } = useI18n()

const props = defineProps({
  show: Boolean,
  data: { type: Object, default: () => ({}) },
  backend: String,
  createdAt: String,
  relatedFile: String,
  messageId: Number,
  sessionId: String,
  indexed: Boolean,
  formatDetailTime: Function,
})

const emit = defineEmits(['close'])

const toast = useToast()

function copyValue(value, event) {
  const wrap = event.currentTarget.closest('.metadata-value-wrap') || event.currentTarget
  const btn = wrap.querySelector?.('.metadata-copy-btn')
  const txt = wrap.querySelector?.('.metadata-session-id')
  const doCopy = () => {
    if (btn) { btn.classList.add('copied'); setTimeout(() => btn.classList.remove('copied'), 800) }
    if (txt) { txt.classList.add('copied'); setTimeout(() => txt.classList.remove('copied'), 800) }
    toast.show(t('chat.metadata.copied'), { icon: '📋', type: 'success', duration: 1500 })
  }
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(value).then(doCopy).catch(() => {
      const ta = document.createElement('textarea')
      ta.value = value
      ta.style.cssText = 'position:fixed;opacity:0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
      doCopy()
    })
  } else {
    const ta = document.createElement('textarea')
    ta.value = value
    ta.style.cssText = 'position:fixed;opacity:0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    doCopy()
  }
}
</script>

<style scoped>
.metadata-content {
    padding: 12px 14px;
    overflow-y: auto;
    flex: 1;
}

.metadata-item {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--border-color);
}

.metadata-item:last-child {
    border-bottom: none;
}

.metadata-label {
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
    min-width: 90px;
    flex-shrink: 0;
}

.metadata-value {
    font-size: 13px;
    color: var(--text-primary);
    word-break: break-all;
}

.metadata-relative-time {
    font-size: 12px;
    color: var(--text-muted, #9ca3af);
    margin-left: 6px;
}

.metadata-session-id {
    font-family: monospace;
    font-size: 12px;
    background: var(--bg-tertiary);
    padding: 2px 6px;
    border-radius: 3px;
}

.metadata-value-wrap {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.metadata-value-copyable {
    cursor: pointer;
}

.metadata-copyable {
    user-select: none;
}

.metadata-copyable:hover {
    background: var(--bg-tertiary, #f5f5f5);
}

.metadata-value-copyable:hover {
    color: var(--accent-color, #4a90d9);
}

.metadata-value-copyable.copied {
    color: #22c55e;
}

.metadata-error {
    color: #ef4444;
    word-break: break-all;
}

.metadata-copy-btn {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    background: none;
    border: none;
    cursor: pointer;
    color: var(--text-muted, #999);
    padding: 2px;
    border-radius: 3px;
    transition: color 0.15s, background 0.15s;
}

.metadata-copy-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-tertiary, #f0f0f0);
}

.metadata-copy-btn.copied {
    color: #22c55e;
}

.metadata-indexed-yes {
    color: #22c55e;
}

.metadata-indexed-no {
    color: var(--text-muted, #999);
}
</style>
