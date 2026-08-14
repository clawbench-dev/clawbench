<template>
  <div ref="rootRef" class="session-sidebar" :class="{ overlay: !isWideScreen }" :style="{ width: `${width}px` }">
    <SplitDivider @dragmove="onDragMove" />
    <div class="sidebar-inner">
      <div class="bs-header session-sidebar-header">
        <SessionListHeader
          :session-count="sessionCount"
          :session-max-count="sessionMaxCount"
          :pinned="true"
          @refresh="handleRefresh"
          @open-search="$emit('open-session-search')"
          @create="handleCreateClick"
        >
          <template #actions>
            <button class="header-action-btn sidebar-close-btn" @click.stop="$emit('close')" :title="t('session.closeSidebar')">
              <PanelLeftClose :size="16" />
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
import { PanelLeftClose } from 'lucide-vue-next'
import SplitDivider from '@/components/common/SplitDivider.vue'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import { useAgents } from '@/composables/useAgents'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
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
const { isWideScreen } = useWideScreenLayout()

const listRef = ref(null)
const rootRef = ref(null)

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
function handleRefresh() {
  listRef.value?.loadSessions()
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
/* On non-wide screens the pinned sidebar floats as an overlay over the chat
   (instead of taking layout space and crushing the chat column). */
.session-sidebar.overlay {
  position: absolute;
  right: 0;
  top: 0;
  bottom: 0;
  z-index: 30;
  box-shadow: -4px 0 12px rgba(0, 0, 0, 0.15);
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
