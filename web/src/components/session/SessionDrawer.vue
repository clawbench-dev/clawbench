<template>
  <BottomSheet ref="bottomSheetRef" :open="open" auto :title="t('session.title')" @close="$emit('close')">
    <template #header>
      <Bot :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('session.title') }}</span>
      <div v-if="sessionMaxCount > 0" class="session-counter">
        <div class="session-counter-bar">
          <div class="session-counter-fill" :style="{ width: sessionPct + '%', background: sessionBarColor }"></div>
          <span class="session-counter-text">{{ sessionCount }}/{{ sessionMaxCount }}</span>
        </div>
      </div>
      <button class="header-action-btn" @click.stop="$emit('open-session-search')" :title="t('sessionSearch.title')">
        <Search :size="16" />
      </button>
      <button class="header-action-btn" @click.stop="handleCreateClick" :title="t('session.newSession')">
        <Plus :size="16" />
      </button>
    </template>

    <div class="session-list" ref="listRef">
      <div v-if="loading" class="session-loading">{{ t('common.loading') }}</div>
      <div v-else-if="sessions.length === 0" class="session-empty">{{ t('session.noSessions') }}</div>
      <template v-else>
        <div
          v-for="session in sessionsWithStatus"
          :key="session.id"
          class="session-row"
          :class="{ active: session.id === currentSessionId, running: session.running }"
        >
          <span v-if="session.running" class="session-running-line"></span>
          <div
            class="session-item"
            :class="{ active: session.id === currentSessionId }"
            @click="selectSession(session.id, session.backend)"
          >
            <span v-if="session.unreadCount > 0 || session.pendingApproval" class="session-item-badge"></span>
            <div class="session-item-info">
              <div class="session-item-header">
                <span class="session-item-title">{{ session.title }}</span>
              </div>
              <div class="session-item-meta">
                <span class="session-item-time">{{ formatRelativeTime(session.updatedAt) }}</span>
                <span class="session-item-agent"><AgentIcon :backend="getAgentBackend(session.agentId)" :name="getAgentName(session.agentId)" :size="12" /> {{ getAgentName(session.agentId) }}</span>
                <span v-if="session.model" class="session-item-model">{{ session.model }}</span>
              </div>
            </div>
          </div>
          <button class="session-archive-btn" :title="t('common.archive')" @click.stop="archiveSession(session.id)">
            <Archive :size="15" />
          </button>
        </div>
        <div ref="sentinelRef" class="session-list-sentinel"></div>
        <div v-if="loadingMore" class="session-loading-more">{{ t('common.loading') }}</div>
        <div v-else-if="!hasMore && sessions.length > 0" class="session-list-end"></div>
      </template>
    </div>
  </BottomSheet>

  <!-- Agent selector drawer -->
  <AgentSelectorDrawer
    ref="agentSelectorRef"
    :open="agentSelectorDrawer.effectiveOpen.value"
    :title="t('session.selectAgent')"
    :default-badge="t('chat.sessionSetting.defaultBadge')"
    :set-default-title="t('session.setAsDefaultAgent')"
    @update:open="v => v ? agentSelectorDrawer.open() : agentSelectorDrawer.close()"
    @select="createSession"
  />
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { appLog } from '@/utils/appLog'
import { Plus, Search, Archive } from 'lucide-vue-next'
import { ref, watch, computed, onUnmounted, nextTick } from 'vue'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import { useAgents } from '@/composables/useAgents'
import { useDialog } from '@/composables/useDialog.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { formatRelativeTime } from '@/utils/format.ts'
import { store } from '@/stores/app.ts'

const { t } = useI18n()
const TAG = 'SessionDrawer'
const props = defineProps({
  open: Boolean,
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  currentAgentId: String,
})

const emit = defineEmits(['close', 'select', 'create', 'archive', 'destroy', 'open-session-search'])

const bottomSheetRef = ref(null)
const agentSelectorRef = ref(null)
const sessions = ref([])
const loading = ref(false)
const loadingMore = ref(false)
const hasMore = ref(false)
const listRef = ref(null)
const sentinelRef = ref(null)
let observer = null
const pageSize = computed(() => store.state.chatSessionPageSize || 10)
const { agents, loadAgents, getAgentBackend, getAgentName } = useAgents()
const dialog = useDialog()
const { runningSessionsVersion } = useSessionIdentity()

// Session count indicator
const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)
const sessionPct = computed(() => sessionMaxCount.value > 0 ? Math.min((sessionCount.value / sessionMaxCount.value) * 100, 100) : 0)
const sessionBarColor = computed(() => {
  if (sessionCount.value >= sessionMaxCount.value && sessionMaxCount.value > 0) return '#ef4444'
  if (sessionPct.value >= 80) return '#f59e0b'
  return 'var(--accent-color, #0066cc)'
})

const agentSelectorDrawer = useTabDrawer('chat', { autoRestore: false })

const sessionsWithStatus = computed(() => {
  // Access runningSessionsVersion to establish reactive dependency
  // so the computed re-evaluates when sessions start/stop running
  void runningSessionsVersion.value
  return sessions.value.map(s => ({
    ...s,
    // WS-maintained runningSessionIds is the authoritative source of truth.
    // It is initialized from API on app start (loadSessionsOnce) and updated
    // in real-time via WS session_update events. The API snapshot s.running
    // is stale once the drawer is open — do NOT fall back to it.
    running: props.runningSessionIds.has(s.id)
  }))
})

defineExpose({ loadSessions, openAgentSelector, addSessionLocally })

async function openAgentSelector() {
  await loadAgents()
  // If only one agent exists, skip the selector and create directly
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
    bottomSheetRef.value?.close()
    return
  }
  agentSelectorDrawer.open()
}

async function handleCreateClick() {
  await loadAgents()
  // If only one agent exists, skip the selector and create directly
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
    bottomSheetRef.value?.close()
    return
  }
  agentSelectorDrawer.open()
}

async function loadSessions() {
  loading.value = true
  hasMore.value = false
  try {
    const resp = await fetch(`/api/ai/sessions?limit=${pageSize.value}`)
    const data = await resp.json()
    sessions.value = data.sessions || []
    hasMore.value = !!data.hasMore
    if (typeof data.totalCount === 'number') store.state.sessionCount = data.totalCount
  } catch (err) {
    appLog.e(TAG, 'Failed to load sessions:', err)
    sessions.value = []
  } finally {
    loading.value = false
    await nextTick()
    setupObserver()
  }
}

async function loadMoreSessions() {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    const last = sessions.value[sessions.value.length - 1]
    if (!last) return
    const cursor = last.updatedAt
    const cursorId = last.id
    const resp = await fetch(`/api/ai/sessions?limit=${pageSize.value}&cursor=${encodeURIComponent(cursor)}&cursor_id=${encodeURIComponent(cursorId)}`)
    const data = await resp.json()
    const more = data.sessions || []
    if (more.length > 0) {
      sessions.value = [...sessions.value, ...more]
    }
    hasMore.value = !!data.hasMore
  } catch (err) {
    appLog.e(TAG, 'Failed to load more sessions:', err)
  } finally {
    loadingMore.value = false
  }
}

function setupObserver() {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinelRef.value || !listRef.value) return
  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMore.value && !loadingMore.value) {
      loadMoreSessions()
    }
  }, { threshold: 0.1, rootMargin: '100px', root: listRef.value })
  observer.observe(sentinelRef.value)
}

function selectSession(sessionId, backend) {
  emit('select', sessionId, backend)
  bottomSheetRef.value?.close()
}

function createSession(agentId) {
  agentSelectorDrawer.close()
  emit('create', agentId)
  bottomSheetRef.value?.close()
}

async function archiveSession(sessionId) {
  const isRunning = props.runningSessionIds.has(sessionId)
  const confirmMsg = isRunning ? t('session.confirmArchiveRunning') : t('session.confirmArchive')
  const confirmed = await dialog.confirm(confirmMsg, {
    dangerous: true,
    extraText: t('chat.archive.destroyBtn'),
    extraPrimedText: t('chat.archive.destroyBtnPrimed'),
    onExtraAction: () => emit('destroy', sessionId),
  })
  if (confirmed) {
    const session = sessions.value.find(s => s.id === sessionId)
    emit('archive', sessionId, session?.backend)
  }
}

function addSessionLocally(session) {
  if (!session) return
  // Prepend to list, avoid duplicate if already present
  if (sessions.value.some(s => s.id === session.id)) return
  sessions.value = [session, ...sessions.value]
}

// Load from API when the drawer opens. Also reload when sessionCount changes
// while the drawer is open (e.g. after a successful archive).
watch(() => props.open, async (val) => {
  if (val) {
    await Promise.all([loadSessions(), loadAgents()])
  }
})
watch(() => store.state.sessionCount, async () => {
  if (props.open) {
    await loadSessions()
  }
})

onUnmounted(() => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
})

</script>

<style scoped>
.session-list {
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}

.session-loading {
  min-height: 40vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.session-empty {
  min-height: 40vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: 13px;
}

.session-item {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: 10px 12px;
  border-top: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: background 0.15s;
}

@media (hover: hover) {
  .session-item:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.session-item.active {
  border-left: 3px solid var(--accent-color, #0066cc);
  padding-left: 9px;
}

.session-row.active {
  background: var(--accent-bg, rgba(0, 102, 204, 0.1));
}

.session-row.running {
  background: rgba(34, 197, 94, 0.05);
}

.session-item-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.session-item-header {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
}

.session-item-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.session-item-title {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-item.active .session-item-title {
  color: var(--accent-color, #0066cc);
}

.session-item-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent-color, #0066cc);
}

.session-running-line {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 1px;
  overflow: hidden;
}

.session-running-line::after {
  content: '';
  position: absolute;
  top: 0;
  left: -40%;
  width: 40%;
  height: 100%;
  background: linear-gradient(90deg, transparent, #22c55e, transparent);
  animation: scan-line 2s ease-in-out infinite;
}

@keyframes scan-line {
  0% { left: -40%; }
  100% { left: 100%; }
}

.session-row {
  display: flex;
  align-items: stretch;
  position: relative;
}

.session-archive-btn {
  flex-shrink: 0;
  width: 34px;
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-top: 1px solid var(--border-color, #dee2e6);
  transition: background 0.15s, color 0.15s;
}

@media (hover: hover) {
  .session-archive-btn:hover {
    color: var(--accent-color, #0066cc);
  }
}

.session-archive-btn:active {
  color: var(--accent-color, #0066cc);
}

.session-item-time {
  font-size: 11px;
  color: var(--text-muted, #999);
}

.session-item-agent {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 0;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.session-item-model {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 0;
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted, #999);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-counter {
  margin-left: auto;
  flex-shrink: 0;
}

.session-counter-bar {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 42px;
  height: 16px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
}

.session-counter-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: 8px;
  transition: width 0.3s ease, background 0.3s ease;
}

.session-counter-text {
  position: relative;
  z-index: 1;
  font-size: 9px;
  font-weight: 600;
  color: #fff;
  line-height: 1;
  letter-spacing: 0.3px;
  text-shadow: 0 0 2px rgba(0, 0, 0, 0.3);
}

.header-action-btn {
  margin-left: 6px;
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: background 0.15s;
}

.header-action-btn:hover {
  background: rgba(0, 102, 204, 0.1);
}

.session-list-sentinel {
  height: 1px;
}

.session-loading-more {
  padding: 12px;
  text-align: center;
  color: var(--text-muted, #999);
  font-size: 12px;
}

.session-list-end {
  height: 0;
}
</style>
