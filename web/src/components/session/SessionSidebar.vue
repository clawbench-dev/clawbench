<template>
  <div ref="rootRef" class="session-sidebar" :style="{ width: `${width}px` }">
    <SplitDivider @dragmove="onDragMove" />
    <div class="sidebar-inner">
      <div class="bs-header session-sidebar-header">
        <SessionListHeader
          :session-count="sessionCount"
          :session-max-count="sessionMaxCount"
          :pinned="true"
          :refreshing="sessionRefreshing"
          @refresh="handleRefresh"
          @open-search="$emit('open-session-search')"
          @create="handleCreateClick"
        >
          <!-- Close sits in the trailing slot so it is the right-most control in
               the header, after search/create/refresh. It is a plain utility
               action like refresh — no accent tint — and uses the close (X)
               glyph so it reads as "collapse this panel". -->
          <template #actions-end>
            <button class="header-action-btn sidebar-close-btn" @click.stop="$emit('close')" :title="t('session.closeSidebar')">
              <X :size="16" />
              <span class="action-label">{{ t('session.closeSidebar') }}</span>
            </button>
          </template>
        </SessionListHeader>
      </div>
      <SessionList
        ref="listRef"
        :current-session-id="currentSessionId"
        :running-session-ids="runningSessionIds"
        :is-active="isActive"
        @select="handleSelect"
        @archive="handleArchive"
        @destroy="$emit('destroy', $event)"
      />
    </div>
    <!-- Shared-conversation management lives in the 分享 tab now, so this
         component no longer mounts a drawer. The root stays a single element:
         App.vue drives visibility with v-show (a style fallthrough), and a
         fragment root silently drops it. -->
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import SplitDivider from '@/components/common/SplitDivider.vue'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import { SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/composables/useSessionSidebar'
import { store } from '@/stores/app.ts'

defineProps({
  width: { type: Number, default: 280 },
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
})

const emit = defineEmits(['select', 'archive', 'destroy', 'close', 'resize', 'open-session-search', 'create-agent-select'])
const { t } = useI18n()

const listRef = ref(null)
const rootRef = ref(null)
// Tracks a manual refresh so the sidebar refresh button can spin for the real
// load duration (SessionList keeps its list visible during background reloads,
// so `loading` alone can't drive the button).
const sessionRefreshing = ref(false)

const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)

function clampWidth(w) {
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, w))
}

function onDragMove(clientX) {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return
  // Sidebar grows to the right; divider sits at its left edge.
  // Width = distance from sidebar's right edge to the pointer.
  const rightEdge = rect.right
  const newWidth = rightEdge - clientX
  emit('resize', clampWidth(newWidth))
}

// The create button always opens the merged create page (single / group) via
// the shared agent selector, even with a single agent. Direct-creating a
// session would be a one-tap mis-tap risk on touch, and would make the group
// tab unreachable. Mirrors the mobile drawer and ChatPanelContent.
function handleCreateClick() {
  emit('create-agent-select')
}

// Manual refresh in the pinned sidebar: reload the session list from the API.
async function handleRefresh() {
  sessionRefreshing.value = true
  try {
    await listRef.value?.loadSessions()
  } finally {
    sessionRefreshing.value = false
  }
}

function handleSelect(sessionId, backend, projectPath) {
  emit('select', sessionId, backend, projectPath)
}

function handleArchive(sessionId, backend) {
  emit('archive', sessionId, backend)
}

defineExpose({ loadSessions: () => listRef.value?.loadSessions(), addSessionLocally: (s) => listRef.value?.addSessionLocally(s) })
</script>

<style scoped>
.session-sidebar {
  position: relative;
  flex-shrink: 0;
  height: 100%;
  display: flex;
  background: var(--bg-secondary, #fff);
}
.sidebar-inner {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.session-sidebar-header {
  flex-shrink: 0;
  cursor: default;
}
</style>
