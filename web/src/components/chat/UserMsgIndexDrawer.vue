<template>
  <BottomSheet :open="open" auto @close="$emit('close')">
    <template #header>
      <span class="bs-header-icon"><MessagesSquare :size="16" /></span>
      <span class="bs-header-title">{{ t('chat.messageList.conversationIndexTitle') }}</span>
      <span class="panel-count count-badge count-badge--md">{{ isSearching ? `${filteredMessages.length}/${messages.length}` : messages.length }}</span>
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
          <!-- Row rendering + styles live in MessageIndexRow so the share TOC
               and this drawer can never drift apart. -->
          <MessageIndexRow
            v-for="(msg, idx) in filteredMessages"
            :key="msg.id || idx"
            :msg="msg"
            :index="msgIndex(msg)"
            :active="msg.id === activeId"
            :nav-active="listNav.activeIndex.value === idx"
            :search-query="searchQuery"
            @select="$emit('select', $event)"
          />
        </div>
      </template>
    </div>
  </BottomSheet>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { MessagesSquare } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import MessageIndexRow from '@/components/chat/MessageIndexRow.vue'
import { matchIndexMsg } from '@/utils/userMsgIndexUtils.ts'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'

const { t } = useI18n()

const props = defineProps({
  open: Boolean,
  messages: { type: Array, default: () => [] },
  activeId: { type: [Number, String], default: null, required: false },
  loading: Boolean,
  jumping: Boolean,
})

const emit = defineEmits(['close', 'select'])

const listRef = ref(null)
const searchInputRef = ref(null)
let focusTimer = null

const searchQuery = ref('')
const isSearching = computed(() => searchQuery.value.trim().length > 0)

/** Visible row labels (attachment-only rows, and assistant rows with no text). */
const rowLabels = computed(() => ({
  attachment: t('chat.messageList.userMsgIndexAttachment'),
  noText: t('chat.messageList.conversationIndexNoText'),
}))

const filteredMessages = computed(() => {
  const q = searchQuery.value.trim()
  if (!q) return props.messages
  return props.messages.filter(m => matchIndexMsg(m, q, rowLabels.value))
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
  font-weight: var(--font-weight-semibold);
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color) 22%, transparent);
}

/* ── Search row (below header, above the list) ── */
.msg-index-search-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4) 14px var(--space-2);
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
  padding: var(--space-5) 0 14px 0;
  flex: 1;
  min-height: 0;
}

.panel-list::-webkit-scrollbar {
  width: 6px;
}
.panel-list::-webkit-scrollbar-thumb {
  background: var(--scrollbar-thumb, #c1c1c1);
  border-radius: var(--radius-xs);
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
  gap: var(--space-3);
  min-height: 36vh;
  padding: 24px 28px;
  text-align: center;
}

.panel-empty-icon-wrap {
  width: 52px;
  height: 52px;
  border-radius: var(--radius-lg);
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--text-muted) 10%, transparent);
  margin-bottom: var(--space-2);
}

.panel-empty-icon {
  color: var(--text-muted);
  opacity: var(--opacity-hover);
}

.panel-empty-text {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #495057);
}

.panel-empty-hint {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
  max-width: 260px;
}
</style>
