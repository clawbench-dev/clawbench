<template>
  <div v-if="messages.length > 0" class="queued-bar" :class="{ expanded }">
    <button
      class="queued-bar-header"
      type="button"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      <LoadingIndicator class="queued-bar-spinner" size="sm" inline />
      <span class="queued-bar-title">{{ t('chat.pending.barTitle', { count: messages.length }) }}</span>
      <ChevronDown class="queued-bar-chevron" :size="14" />
    </button>

    <ul v-if="expanded" class="queued-bar-list">
      <li v-for="msg in messages" :key="msg.queueId" class="queued-bar-item">
        <div class="queued-bar-text">{{ msg.text || t('chat.pending.attachment') }}</div>
        <div v-if="msg.files.length > 0" class="queued-bar-files">
          <span v-for="(f, i) in msg.files" :key="i" class="queued-bar-file">{{ fileLabel(f) }}</span>
        </div>
        <div class="queued-bar-actions">
          <button
            class="queued-bar-action"
            :class="{ 'queued-bar-action-interrupt': !midTurnSupported }"
            type="button"
            :disabled="busy === msg.queueId"
            :title="midTurnSupported ? t('chat.pending.insertHint') : t('chat.pending.interruptHint')"
            @click="$emit('action', msg.queueId, midTurnSupported ? 'insert' : 'interrupt')"
          >
            <Zap v-if="midTurnSupported" :size="11" />
            <Square v-else :size="11" fill="currentColor" />
            {{ midTurnSupported ? t('chat.pending.insert') : t('chat.pending.interrupt') }}
          </button>
          <button
            class="queued-bar-remove"
            type="button"
            :disabled="busy === msg.queueId"
            :title="t('chat.pending.remove')"
            @click="$emit('remove', msg.queueId)"
          >×</button>
        </div>
      </li>
    </ul>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, Zap, Square } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

defineProps({
  /** Queued messages for the active session (from useMessageQueue). */
  messages: { type: Array, required: true },
  /** Whether the current backend can join the running turn (insert vs interrupt). */
  midTurnSupported: { type: Boolean, default: false },
  /** queueId of the entry whose action request is in flight. */
  busy: { type: String, default: '' },
})

defineEmits(['remove', 'action'])

const { t } = useI18n()
const expanded = ref(false)

function fileLabel(f) {
  if (!f) return ''
  if (f.kind === 'quote') return t('chat.pending.fileReference')
  if (f.kind === 'url') return f.path || f.url || ''
  const p = f.path || ''
  const parts = p.split('/')
  return parts[parts.length - 1] || p
}
</script>

<style scoped>
.queued-bar {
  flex-shrink: 0;
  margin: 0 var(--space-3) var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--surface-color, var(--bg-secondary));
  overflow: hidden;
}

.queued-bar-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-1) var(--space-2);
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-secondary);
  font-size: var(--font-size-2xs);
}

.queued-bar-spinner {
  --li-color: var(--accent-color, currentColor);
}

.queued-bar-title {
  flex: 1;
  text-align: left;
}

.queued-bar-chevron {
  transition: transform var(--duration-base);
  flex-shrink: 0;
}

.queued-bar.expanded .queued-bar-chevron {
  transform: rotate(180deg);
}

.queued-bar-list {
  list-style: none;
  margin: 0;
  padding: 0 var(--space-2) var(--space-2);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  max-height: 40vh;
  overflow-y: auto;
}

.queued-bar-item {
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary, rgba(127, 127, 127, 0.08));
}

.queued-bar-text {
  font-size: var(--font-size-2xs);
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.queued-bar-files {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
  margin-top: 2px;
}

.queued-bar-file {
  font-size: var(--font-size-2xs);
  color: var(--text-tertiary, var(--text-secondary));
  background: rgba(127, 127, 127, 0.14);
  border-radius: var(--radius-full);
  padding: 0 var(--space-2);
  max-width: 40vw;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.queued-bar-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-1);
}

.queued-bar-action {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: rgba(127, 127, 127, 0.16);
  border: none;
  border-radius: var(--radius-full);
  cursor: pointer;
  color: var(--text-secondary);
  padding: 1px 7px;
  font-size: var(--font-size-2xs);
  line-height: var(--line-height-relaxed);
  transition: background var(--duration-base), color var(--duration-base);
}

.queued-bar-action:disabled,
.queued-bar-remove:disabled {
  opacity: var(--opacity-muted);
  cursor: default;
}

.queued-bar-action-interrupt {
  color: #e08a8a;
}

@media (hover: hover) {
  .queued-bar-action:not(:disabled):hover {
    background: rgba(127, 127, 127, 0.28);
  }
}

.queued-bar-remove {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-tertiary, var(--text-secondary));
  padding: 0 var(--space-1);
  font-size: var(--font-size-md);
  line-height: 1;
  transition: color var(--duration-base);
}

@media (hover: hover) {
  .queued-bar-remove:not(:disabled):hover {
    color: var(--text-primary);
  }
}
</style>
