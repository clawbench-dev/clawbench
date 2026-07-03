<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <MessagesSquare :size="16" class="panel-icon" />
      <span>{{ t('chat.messageList.conversationIndexTitle') }}</span>
      <span class="panel-count">{{ messages.length }}</span>
    </template>
    <div v-if="loading" class="panel-loading">
      <span class="chat-load-spinner"></span>
      <span>{{ t('chat.messageList.loadingMore') }}</span>
    </div>
    <div v-else-if="jumping" class="panel-loading">
      <span class="chat-load-spinner"></span>
      <span>{{ t('chat.messageList.loadingMore') }}</span>
    </div>
    <div v-else-if="messages.length === 0" class="panel-empty">
      <MessagesSquare :size="28" class="panel-empty-icon" />
      <span>{{ t('chat.messageList.noUserMessages') }}</span>
    </div>
    <div v-else class="panel-list" ref="listRef">
      <div
        v-for="(msg, idx) in messages"
        :key="msg.id || idx"
        class="msg-item"
        :class="{ active: msg.id === activeId }"
        :aria-current="msg.id === activeId || undefined"
        tabindex="0"
        role="button"
        @click="$emit('select', msg)"
        @keydown.enter="$emit('select', msg)"
      >
        <span class="msg-node">
          <span class="msg-index">{{ idx + 1 }}</span>
        </span>
        <div class="msg-body">
          <span class="msg-text">{{ truncateText(msg) }}</span>
          <span v-if="msg.createdAt" class="msg-time">{{ formatRelativeTime(msg.createdAt) }}</span>
        </div>
        <button class="msg-fork-btn" @click.stop="$emit('fork', msg)" :title="t('chat.actions.forkSession')">
          <Split :size="14" />
        </button>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { MessagesSquare, Split } from 'lucide-vue-next'
import { truncateUserMsg } from '@/utils/userMsgIndexUtils.ts'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { formatRelativeTime } from '@/utils/format.ts'
import { ref, watch, nextTick } from 'vue'

const { t } = useI18n()

const props = defineProps({
  open: Boolean,
  messages: { type: Array, default: () => [] },
  activeId: { type: [Number, String], default: null, required: false },
  loading: Boolean,
  jumping: Boolean,
})

defineEmits(['close', 'select', 'fork'])

const listRef = ref(null)

function truncateText(msg) {
  return truncateUserMsg(msg, t('chat.messageList.userMsgIndexAttachment'))
}

// Scroll the active message into view when the drawer opens
watch(() => props.open, async (val) => {
  if (val) {
    await nextTick()
    const activeEl = listRef.value?.querySelector('.msg-item.active')
    if (activeEl) {
      activeEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }
})
</script>

<style scoped>
.panel-icon {
  color: var(--accent-color);
  flex-shrink: 0;
}

.panel-count {
  margin-left: auto;
  font-size: 11px;
  font-weight: 600;
  color: var(--accent-color);
  background: var(--accent-bg, rgba(0, 102, 204, 0.08));
  border-radius: 10px;
  padding: 1px 8px;
  line-height: 1.4;
}

.panel-loading {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 18px;
  font-size: 12px;
  color: var(--text-muted);
}

.panel-list {
  overflow-y: auto;
  padding: 4px 8px 12px 4px;
}

.panel-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 30vh;
  color: var(--text-muted);
  font-size: 13px;
}

.panel-empty-icon {
  opacity: 0.35;
}

.msg-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 6px 8px 6px 4px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.15s;
  -webkit-tap-highlight-color: transparent;
  position: relative;
}

.msg-item:active {
  opacity: 0.7;
}

@media (hover: hover) {
  .msg-item:hover {
    background: var(--bg-tertiary);
  }
  .msg-item:hover .msg-node {
    background: var(--accent-bg, rgba(0, 102, 204, 0.1));
    box-shadow: 0 0 0 3px var(--bg-tertiary);
  }
}

/* Timeline connector line */
.msg-item::before {
  content: '';
  position: absolute;
  left: 16px;
  top: 0;
  bottom: 0;
  width: 1.5px;
  background: var(--border-color);
}

.msg-item:first-child::before {
  top: 16px;
}

.msg-item:last-child::before {
  display: none;
}

.msg-item.active {
  background: var(--accent-bg, rgba(0, 102, 204, 0.06));
}

/* Timeline node */
.msg-node {
  position: relative;
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-tertiary);
  transition: background 0.15s, box-shadow 0.15s;
  z-index: 1;
  box-shadow: 0 0 0 3px var(--bg-secondary);
}

.msg-item.active .msg-node {
  background: var(--accent-color);
  box-shadow: 0 0 0 3px var(--bg-secondary);
}

.msg-item.active .msg-index {
  color: var(--bg-secondary, #fff);
}

.msg-item.active .msg-text {
  color: var(--accent-color, #0066cc);
}

.msg-index {
  font-size: 11px;
  font-weight: 700;
  color: var(--accent-color);
  line-height: 1;
}

.msg-body {
  display: flex;
  flex-direction: column;
  gap: 3px;
  flex: 1;
  min-width: 0;
}

.msg-text {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.4;
  word-break: break-word;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.msg-time {
  font-size: 10.5px;
  color: var(--text-muted, #999);
  line-height: 1;
  letter-spacing: 0.2px;
}

.msg-fork-btn {
  flex-shrink: 0;
  min-width: 22px;
  height: 22px;
  padding: 0 4px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0.4;
  transition: opacity 0.2s, background 0.2s, color 0.2s;
  -webkit-tap-highlight-color: transparent;
}

@media (hover: hover) {
  .msg-item:hover .msg-fork-btn {
    opacity: 0.8;
  }
  .msg-fork-btn:hover {
    opacity: 1 !important;
    background: var(--bg-tertiary);
    color: var(--accent-color);
  }
}

.msg-fork-btn:active {
  opacity: 1;
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
}
</style>
