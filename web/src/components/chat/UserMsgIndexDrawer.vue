<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <span class="bs-header-icon"><MessagesSquare :size="16" /></span>
      <span class="bs-header-title">{{ t('chat.messageList.conversationIndexTitle') }}</span>
      <span class="panel-count">{{ isSearching ? `${filteredMessages.length}/${messages.length}` : messages.length }}</span>
    </template>
    <LoadingIndicator v-if="loading" size="md" :label="t('chat.messageList.loadingMore')" />
    <LoadingIndicator v-else-if="jumping" size="md" :label="t('chat.messageList.loadingMore')" />
    <div v-else-if="messages.length === 0" class="panel-empty">
      <span class="panel-empty-icon-wrap">
        <MessagesSquare :size="26" class="panel-empty-icon" />
      </span>
      <span class="panel-empty-text">{{ t('chat.messageList.noUserMessages') }}</span>
      <span class="panel-empty-hint">{{ t('chat.messageList.noUserMessagesHint') }}</span>
    </div>
    <div v-else class="panel-content">
      <div class="msg-index-search-row">
        <SearchInput
          ref="searchInputRef"
          v-model="searchQuery"
          :placeholder="t('chat.messageList.conversationIndexSearch')"
          @enter="listNav.confirm"
          @down="listNav.down"
          @up="listNav.up"
        />
      </div>
      <div v-if="isSearching && filteredMessages.length === 0" class="panel-empty">
        <span class="panel-empty-icon-wrap">
          <MessagesSquare :size="26" class="panel-empty-icon" />
        </span>
        <span class="panel-empty-text">{{ t('chat.messageList.conversationIndexNoResults') }}</span>
      </div>
      <template v-else>
        <div class="panel-list" ref="listRef">
          <div
            v-for="(msg, idx) in filteredMessages"
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
              <span class="msg-index">{{ msgIndex(msg) }}</span>
            </span>
            <div class="msg-body">
              <span class="msg-text" v-html="rowHighlight(msg)"></span>
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
      </template>
    </div>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { MessagesSquare, Split, MousePointerClick } from 'lucide-vue-next'
import { formatUserMsg, matchUserMsg } from '@/utils/userMsgIndexUtils.ts'
import { highlightText } from '@/utils/searchUtils'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { formatRelativeTime } from '@/utils/format.ts'
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'

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
const searchInputRef = ref(null)
let focusTimer = null

const searchQuery = ref('')
const isSearching = computed(() => searchQuery.value.trim().length > 0)

const filteredMessages = computed(() => {
  const q = searchQuery.value.trim()
  if (!q) return props.messages
  const attachmentLabel = t('chat.messageList.userMsgIndexAttachment')
  return props.messages.filter(m => matchUserMsg(m, q, attachmentLabel))
})

// Full-list ordinal per message object, so the index badge keeps the message's
// original conversation number while the list is filtered.
const msgOrdinal = computed(() => {
  const map = new Map()
  props.messages.forEach((m, i) => map.set(m, i))
  return map
})

function msgIndex(msg) {
  return (msgOrdinal.value.get(msg) ?? 0) + 1
}

function truncateText(msg) {
  return formatUserMsg(msg, t('chat.messageList.userMsgIndexAttachment'))
}

/** Row display text with the active query's matches wrapped in <mark>. */
function rowHighlight(msg) {
  return highlightText(truncateText(msg), searchQuery.value)
}

// ── Keyboard ↑/↓ + Enter navigation over the message index ──
const listNav = useListNav({
  getCount: () => filteredMessages.value.length,
  onConfirm: (idx) => emit('select', filteredMessages.value[idx]),
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

watch(filteredMessages, () => {
  listNav.reset()
  // A filter change can shorten the list dramatically; snap back to the top so
  // the first result (and any highlight) is visible instead of clamped below.
  if (listRef.value) listRef.value.scrollTop = 0
})

// Reset the search query when the underlying messages change (e.g. switching
// sessions swaps the whole list) or when the drawer reopens, so a stale query
// never hides the freshly shown list.
watch(() => props.messages, () => {
  searchQuery.value = ''
  listNav.reset()
})
watch(() => props.open, (val) => {
  searchQuery.value = ''
  listNav.reset()
  clearTimeout(focusTimer)
  if (val) {
    // Wait for the BottomSheet slide-up animation before focusing, so the
    // search box is ready for immediate typing (matches SessionSearchDrawer).
    focusTimer = setTimeout(() => searchInputRef.value?.focus(), 300)
  }
})

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

onUnmounted(() => {
  clearTimeout(focusTimer)
  focusTimer = null
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

/* ── Search row (below header, above the list) ── */
.msg-index-search-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px 4px;
  flex-shrink: 0;
}

.msg-index-search-row :deep(.search-pill) {
  flex: 1;
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

.msg-text :deep(mark) {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
  color: inherit;
  border-radius: 2px;
  padding: 0 1px;
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

<style>
/* Dark theme override — non-scoped for the [data-theme] selector. Softer mark
   fill keeps highlighted query text readable on dark backgrounds. */
[data-theme-base="dark"] .msg-text mark {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 28%, transparent);
  color: inherit;
}
</style>
