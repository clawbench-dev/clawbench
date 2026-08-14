<template>
  <div ref="rootRef" class="session-sidebar" :style="{ width: `${width}px` }">
    <div
      ref="dividerRef"
      class="sidebar-divider"
      role="separator"
      aria-orientation="vertical"
      @pointerdown="onDividerPointerDown"
    />
    <div class="sidebar-inner">
      <SessionListHeader
        :session-count="sessionCount"
        :session-max-count="sessionMaxCount"
        @open-search="$emit('open-session-search')"
        @create="handleCreateClick"
      >
        <template #actions>
          <button class="header-action-btn sidebar-unpin-btn" @click.stop="$emit('unpin')" :title="t('session.unpinToSidebar')">
            <Pin :size="16" />
          </button>
          <button class="header-action-btn sidebar-close-btn" @click.stop="$emit('close')" :title="t('session.closeSidebar')">
            <PanelLeftClose :size="16" />
          </button>
        </template>
      </SessionListHeader>
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
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pin, PanelLeftClose } from 'lucide-vue-next'
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

const emit = defineEmits(['select', 'archive', 'destroy', 'unpin', 'close', 'resize', 'open-session-search', 'create', 'create-agent-select'])

const { t } = useI18n()
const { agents, loadAgents } = useAgents()

const dividerRef = ref(null)
const listRef = ref(null)
const rootRef = ref(null)
let dragging = false

const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)

function clampWidth(w) {
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, w))
}

function onDividerPointerDown(e) {
  if (e.button !== 0) return
  dragging = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('session-sidebar-dragging')
}

function onPointerMove(e) {
  if (!dragging) return
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect) return
  // Sidebar grows to the right; divider sits at its left edge.
  // Width = distance from sidebar's right edge to the pointer.
  const rightEdge = rect.right
  const newWidth = rightEdge - e.clientX
  emit('resize', clampWidth(newWidth))
}

function onPointerUp(e) {
  if (!dragging) return
  dragging = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('session-sidebar-dragging')
}

async function handleCreateClick() {
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
  } else {
    emit('create-agent-select')
  }
}

function handleSelect(sessionId, backend) {
  emit('select', sessionId, backend)
}

function handleArchive(sessionId, backend) {
  emit('archive', sessionId, backend)
}

onMounted(() => {
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
})
onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
  document.body.classList.remove('session-sidebar-dragging')
})

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
.sidebar-divider {
  position: relative;
  flex: 0 0 auto;
  width: 1px;
  cursor: col-resize;
  touch-action: none;
  z-index: 2;
  background: var(--border-color, #e5e5e5);
  transition: background 0.15s, width 0.15s;
}
.sidebar-divider::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -6px;
  right: -6px;
}
.sidebar-divider:hover,
.sidebar-divider:active {
  width: 12px;
  margin-left: -6px;
  background: var(--accent-color, #0066cc);
}
:global(body.session-sidebar-dragging) {
  user-select: none;
  cursor: col-resize;
}
</style>
