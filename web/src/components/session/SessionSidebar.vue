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
          <template #actions>
            <button class="header-action-btn sidebar-pin-btn is-active" @click.stop="$emit('close')" :title="t('session.unpinToSidebar')">
              <Pin :size="16" :fill="'currentColor'" />
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
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pin } from 'lucide-vue-next'
import SplitDivider from '@/components/common/SplitDivider.vue'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import { useAgents } from '@/composables/useAgents'
import { SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/composables/useSessionSidebar'
import { store } from '@/stores/app.ts'

defineProps({
  width: { type: Number, default: 280 },
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
})

const emit = defineEmits(['select', 'archive', 'destroy', 'close', 'resize', 'open-session-search', 'create', 'create-agent-select'])

const { t } = useI18n()
const { agents, loadAgents } = useAgents()

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

async function handleCreateClick() {
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
  } else {
    emit('create-agent-select')
  }
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

function handleSelect(sessionId, backend) {
  emit('select', sessionId, backend)
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
  border-left: 1px solid var(--border-color, #e5e5e5);
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
.header-action-btn.sidebar-pin-btn.is-active {
  background: rgba(0, 102, 204, 0.15);
  color: var(--accent-color, #0066cc);
}
</style>
