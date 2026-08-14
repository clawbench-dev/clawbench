<template>
  <div
    ref="rootRef"
    class="session-sidebar"
    :class="{ 'bottom-docked': !isWideScreen }"
    :style="isWideScreen ? { width: `${width}px` } : { height: `${height}px` }"
  >
    <SplitDivider :orientation="isWideScreen ? 'vertical' : 'horizontal'" @dragmove="onDragMove" />
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
import { SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_HEIGHT, SIDEBAR_MAX_HEIGHT } from '@/composables/useSessionSidebar'
import { store } from '@/stores/app.ts'

defineProps({
  width: { type: Number, default: 280 },
  height: { type: Number, default: 320 },
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

function clampHeight(h) {
  return Math.max(SIDEBAR_MIN_HEIGHT, Math.min(SIDEBAR_MAX_HEIGHT, h))
}

function onDragMove(value) {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return
  if (isWideScreen.value) {
    // Right-side panel: divider at left edge. Width = right edge - pointer.
    const newWidth = rect.right - value
    emit('resize', clampWidth(newWidth))
  } else {
    // Bottom-docked panel: divider at top edge. Height = bottom edge - pointer.
    const newHeight = rect.bottom - value
    emit('resize', clampHeight(newHeight))
  }
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
  display: flex;
  background: var(--bg-secondary, #fff);
}
/* Wide: right-side column taking full height, left border separator. */
.session-sidebar:not(.bottom-docked) {
  height: 100%;
  border-left: 1px solid var(--border-color, #e5e5e5);
}
/* Narrow: bottom-docked bar over the chat, right-left full width, top border. */
.session-sidebar.bottom-docked {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 30;
  border-top: 1px solid var(--border-color, #e5e5e5);
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.15);
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
