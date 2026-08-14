<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <span class="bs-header-icon"><MessagesSquare :size="16" /></span>
      <span class="bs-header-title">{{ t('chat.messageList.conversationIndexTitle') }}</span>
      <span class="panel-count">{{ messages.length }}</span>
    </template>
    <LoadingIndicator v-if="loading" size="sm" :label="t('chat.messageList.loadingMore')" />
    <LoadingIndicator v-else-if="jumping" size="sm" :label="t('chat.messageList.loadingMore')" />
    <div v-else-if="messages.length === 0" class="panel-empty">
      <span class="panel-empty-icon-wrap">
        <MessagesSquare :size="26" class="panel-empty-icon" />
      </span>
      <span class="panel-empty-text">{{ t('chat.messageList.noUserMessages') }}</span>
      <span class="panel-empty-hint">{{ t('chat.messageList.noUserMessagesHint') }}</span>
    </div>
    <div v-else class="panel-content">
      <div class="panel-list" ref="listRef">
        <div
          v-for="(msg, idx) in messages"
          :key="msg.id || idx"
          class="msg-item"
          :class="{ active: msg.id === activeId, 'msg-item-active': listNav.activeIndex.value === idx }"
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
      <div class="panel-hint">
        <MousePointerClick :size="13" />
        <span>{{ t('chat.messageList.conversationIndexDesc') }}</span>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { MessagesSquare, Split, MousePointerClick } from 'lucide-vue-next'
import { formatUserMsg } from '@/utils/userMsgIndexUtils.ts'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
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

const emit = defineEmits(['close', 'select', 'fork'])

const listRef = ref(null)

function truncateText(msg) {
  return formatUserMsg(msg, t('chat.messageList.userMsgIndexAttachment'))
}

// ── Keyboard ↑/↓ + Enter navigation over the message index ──
const listNav = useListNav({
  getCount: () => props.messages.length,
  onConfirm: (idx) => emit('select', props.messages[idx]),
  onActiveChange: scrollActiveIntoView,
})
// Document-level keys so navigation works regardless of where focus is inside the drawer
useListKeys({ isOpen: () => props.open, nav: listNav })

function scrollActiveIntoView(index) {
  const items = document.querySelectorAll('.panel-list .msg-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

watch(() => props.messages, () => listNav.reset())

// Scroll the active message into view when the drawer opens.
// Must wait for loading/jumping to finish so .panel-list is rendered (listRef is non-null).
// Also wait one nextTick after data ready for Vue to mount the DOM.
watch([() => props.open, () => props.loading, () => props.jumping], async ([isOpen, isLoading, isJumping]) => {
  if (!isOpen || isLoading || isJumping) return
  await nextTick()
  const activeEl = listRef.value?.querySelector('.msg-item.active')
  if (activeEl) {
    activeEl.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }
})
</script>

<style scoped>
/* ── Count badge ── */
.panel-count {
  margin-left: auto;
  font-size: 11px;
  font-weight: 600;
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color) 22%, transparent);
  border-radius: 10px;
  padding: 1px 8px;
  line-height: 1.5;
}

/* ── List ── */
.panel-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.panel-list {
  overflow-y: auto;
  padding: 10px 0 14px 0;
  flex: 1;
  min-height: 0;
}

.panel-list::-webkit-scrollbar {
  width: 6px;
}
.panel-list::-webkit-scrollbar-thumb {
  background: var(--scrollbar-thumb, #c1c1c1);
  border-radius: 3px;
}
.panel-list::-webkit-scrollbar-track {
  background: transparent;
}

/* ── Empty state ── */
.panel-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 36vh;
  padding: 24px 28px;
  text-align: center;
}

.panel-empty-icon-wrap {
  width: 52px;
  height: 52px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--text-muted) 10%, transparent);
  margin-bottom: 4px;
}

.panel-empty-icon {
  color: var(--text-muted);
  opacity: 0.8;
}

.panel-empty-text {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-secondary, #495057);
}

.panel-empty-hint {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
  max-width: 260px;
}

/* ── Message items ── */
.msg-item {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 9px 8px 9px 14px;
  border-radius: 12px;
  cursor: pointer;
  transition: background 0.15s ease;
  -webkit-tap-highlight-color: transparent;
}

/* Timeline connector line — accent-tinted, fades at top & bottom */
.msg-item::before {
  content: '';
  position: absolute;
  left: 26px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: linear-gradient(
    to bottom,
    transparent,
    color-mix(in srgb, var(--accent-color) 22%, transparent) 12%,
    color-mix(in srgb, var(--accent-color) 22%, transparent) 88%,
    transparent
  );
  border-radius: 1px;
  opacity: 0.6;
}

.msg-item:first-child::before {
  top: 18px;
}

.msg-item:last-child::before {
  display: none;
}

@media (hover: hover) {
  .msg-item:hover {
    border-radius: 0;
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
  .msg-item:hover .msg-node {
    background: color-mix(in srgb, var(--accent-color) 16%, transparent);
    border-color: color-mix(in srgb, var(--accent-color) 34%, transparent);
  }
}

.msg-item:active {
  opacity: 0.75;
}

.msg-item.active {
  border-radius: 0;
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  box-shadow: inset 3px 0 0 var(--accent-color);
}

.msg-item-active {
  background: color-mix(in srgb, var(--text-primary) 7%, transparent);
}

/* ── Timeline node (number badge) ── */
.msg-node {
  position: relative;
  z-index: 1;
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  margin-top: 1px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-secondary);
  border: 1.5px solid var(--border-color);
  box-shadow: 0 0 0 3px var(--bg-secondary);
  transition: background 0.15s, border-color 0.15s, color 0.15s;
}

.msg-item.active .msg-node {
  background: var(--accent-color);
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent-color) 16%, transparent);
}

.msg-index {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-secondary);
  line-height: 1;
  transition: color 0.15s;
}

.msg-item.active .msg-index {
  color: #fff;
}

.msg-item.active .msg-text {
  color: var(--accent-color, #0066cc);
}

/* ── Message body ── */
.msg-body {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
  min-width: 0;
}

.msg-text {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.5;
  word-break: break-word;
  white-space: pre-wrap;
}

.msg-time {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 10.5px;
  color: var(--text-muted, #999);
  line-height: 1;
  letter-spacing: 0.2px;
}

.msg-time::before {
  content: '';
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--border-color);
}

/* ── Fork button ── */
.msg-fork-btn {
  flex-shrink: 0;
  min-width: 24px;
  height: 24px;
  margin-top: 1px;
  padding: 0 4px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  border-radius: 6px;
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
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    color: var(--accent-color);
  }
}

.msg-fork-btn:active {
  opacity: 1;
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
}

/* ── Footer hint ── */
.panel-hint {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px 12px;
  font-size: 11px;
  color: var(--text-muted);
  border-top: 1px solid var(--border-color);
  background: color-mix(in srgb, var(--bg-tertiary) 40%, transparent);
  flex-shrink: 0;
}

.panel-hint svg {
  opacity: 0.7;
}
</style>
