<template>
  <ModalDialog :open="show" :zIndex="2500" @close="$emit('close')">
    <template #header>
      <Info :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('chat.metadata.title') }}</span>
    </template>
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
      <div v-if="data.requestModelName" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.requestModelName') }}</span>
        <span class="metadata-value">{{ data.requestModelName }}</span>
      </div>
      <div v-if="data.messageRequestId" class="metadata-item metadata-copyable" @click="copyValue(data.messageRequestId, $event)">
        <span class="metadata-label">{{ t('chat.metadata.messageRequestId') }}</span>
        <div class="metadata-value-wrap">
          <span class="metadata-value metadata-session-id metadata-value-copyable">{{ data.messageRequestId }}</span>
          <button class="metadata-copy-btn" @click.stop="copyValue(data.messageRequestId, $event)" :title="t('chat.metadata.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
      <div v-if="data.inputTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.inputTokens') }}</span>
        <span class="metadata-value">{{ data.inputTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.outputTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.outputTokens') }}</span>
        <span class="metadata-value">{{ data.outputTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.totalTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.totalTokens') }}</span>
        <span class="metadata-value">{{ data.totalTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.cachedReadTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cachedReadTokens') }}</span>
        <span class="metadata-value">{{ data.cachedReadTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.cacheHitTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cacheHitTokens') }}</span>
        <span class="metadata-value">{{ data.cacheHitTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.cachedWriteTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cachedWriteTokens') }}</span>
        <span class="metadata-value">{{ data.cachedWriteTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.cacheCreationTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cacheCreationTokens') }}</span>
        <span class="metadata-value">{{ data.cacheCreationTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.cacheMissTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cacheMissTokens') }}</span>
        <span class="metadata-value">{{ data.cacheMissTokens.toLocaleString() }}</span>
      </div>
      <div v-if="metadataCacheHitRate !== null" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.cacheHitRate') }}</span>
        <span class="metadata-value">{{ metadataCacheHitRate.toFixed(1) }}%</span>
      </div>
      <div v-if="data.credit" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.credit') }}</span>
        <span class="metadata-value">{{ Number(data.credit).toFixed(4) }}</span>
      </div>
      <div v-if="data.thoughtTokens" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.thoughtTokens') }}</span>
        <span class="metadata-value">{{ data.thoughtTokens.toLocaleString() }}</span>
      </div>
      <div v-if="data.usageByCategory && Object.keys(data.usageByCategory).length" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.usageByCategory') }}</span>
        <span class="metadata-value">
          <span v-for="(val, key) in data.usageByCategory" :key="key" class="metadata-cat-chip">
            {{ catLabel(key) }}: {{ val.toLocaleString() }}
          </span>
        </span>
      </div>
      <div v-if="data.requestId" class="metadata-item metadata-copyable" @click="copyValue(data.requestId, $event)">
        <span class="metadata-label">{{ t('chat.metadata.requestId') }}</span>
        <div class="metadata-value-wrap">
          <span class="metadata-value metadata-session-id metadata-value-copyable">{{ data.requestId }}</span>
          <button class="metadata-copy-btn" @click.stop="copyValue(data.requestId, $event)" :title="t('chat.metadata.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
      <div v-if="data.traceId" class="metadata-item metadata-copyable" @click="copyValue(data.traceId, $event)">
        <span class="metadata-label">{{ t('chat.metadata.traceId') }}</span>
        <div class="metadata-value-wrap">
          <span class="metadata-value metadata-session-id metadata-value-copyable">{{ data.traceId }}</span>
          <button class="metadata-copy-btn" @click.stop="copyValue(data.traceId, $event)" :title="t('chat.metadata.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
      <div v-if="data.responseModelId" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.responseModelId') }}</span>
        <span class="metadata-value">{{ data.responseModelId }}</span>
      </div>
      <div v-if="data.finishReason" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.finishReason') }}</span>
        <span class="metadata-value">{{ data.finishReason }}</span>
      </div>
      <div v-if="data.outcome" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.outcome') }}</span>
        <span class="metadata-value">{{ data.outcome }}</span>
      </div>
      <div v-if="data.agentPhase" class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.agentPhase') }}</span>
        <span class="metadata-value">{{ data.agentPhase }}</span>
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
        <span class="metadata-value">${{ data.costUsd.toFixed(2) }}</span>
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
      <div v-if="data.sessionId && data.sessionId !== sessionId" class="metadata-item metadata-copyable" @click="copyValue(data.sessionId, $event)">
        <span class="metadata-label">{{ t('chat.metadata.externalSessionId') }}</span>
        <div class="metadata-value-wrap">
          <span class="metadata-value metadata-session-id metadata-value-copyable">{{ data.sessionId }}</span>
          <button class="metadata-copy-btn" @click.stop="copyValue(data.sessionId, $event)" :title="t('chat.metadata.copy')">
            <Copy :size="13" />
          </button>
        </div>
      </div>
      <div class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.ftsIndexed') }}</span>
        <span class="metadata-value" :class="ftsIndexed ? 'metadata-indexed-yes' : 'metadata-indexed-no'">{{ ftsIndexed ? t('chat.metadata.indexedYes') : t('chat.metadata.indexedNo') }}</span>
      </div>
      <div class="metadata-item">
        <span class="metadata-label">{{ t('chat.metadata.vecIndexed') }}</span>
        <span class="metadata-value" :class="vecIndexed ? 'metadata-indexed-yes' : 'metadata-indexed-no'">{{ vecIndexed ? t('chat.metadata.indexedYes') : t('chat.metadata.indexedNo') }}</span>
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
import { computed } from 'vue'
import { Copy, Info } from 'lucide-vue-next'
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
  ftsIndexed: Boolean,
  vecIndexed: Boolean,
  formatDetailTime: Function,
})

defineEmits(['close'])

const toast = useToast()

// Map a usageByCategory key (CodeBuddy codebuddy.ai/usageByCategory) to a
// localized label. Unknown future keys fall back to the raw key.
function catLabel(key) {
  const map = {
    conversation: 'chat.sessionInfo.catConversation',
    tools: 'chat.sessionInfo.catTools',
    systemPrompt: 'chat.sessionInfo.catSystemPrompt',
    skills: 'chat.sessionInfo.catSkills',
    mcp: 'chat.sessionInfo.catMCP',
  }
  const i18nKey = map[key]
  return i18nKey ? t(i18nKey) : key
}

// Cache hit rate = hit / (hit + miss). Null when neither side is reported.
const metadataCacheHitRate = computed(() => {
  const hit = Number(props.data?.cacheHitTokens) || 0
  const miss = Number(props.data?.cacheMissTokens) || 0
  if (hit <= 0 && miss <= 0) return null
  const total = hit + miss
  if (total <= 0) return 0
  return (hit / total) * 100
})

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
    font-family: var(--font-mono, monospace);
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

@media (hover: hover) {
  .metadata-copyable:hover {
    background: var(--bg-tertiary, #f5f5f5);
  }

  .metadata-value-copyable:hover {
    color: var(--accent-color, #4a90d9);
  }
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

@media (hover: hover) {
  .metadata-copy-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-tertiary, #f0f0f0);
  }
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

.metadata-cat-chip {
    display: inline-block;
    background: var(--bg-tertiary, #f0f0f0);
    border-radius: 3px;
    padding: 1px 6px;
    margin-right: 6px;
    margin-bottom: 2px;
    font-size: 12px;
}
</style>
