<template>
  <div class="session-list" ref="listRef">
    <!-- Only show the full-screen spinner on first load / when the list is empty.
         On background refreshes the existing list stays visible so it can be
         swapped seamlessly to the new data (see loadSessions). -->
    <LoadingIndicator v-if="loading && sessions.length === 0" size="md" :label="t('common.loading')" />
    <div v-else-if="sessions.length === 0" class="session-empty">{{ t('session.noSessions') }}</div>
    <template v-else>
      <TransitionGroup name="session-list" tag="div" class="session-rows">
        <div
          v-for="(session, idx) in sessionsWithStatus"
          :key="session.id"
          class="session-row"
          :class="{ active: session.id === currentSessionId, running: session.running, 'session-row-active': listNav.activeIndex.value === idx }"
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
      </TransitionGroup>
      <div ref="sentinelRef" class="session-list-sentinel"></div>
      <LoadingIndicator v-if="loadingMore" size="sm" inline :label="t('common.loading')" />
      <div v-else-if="!hasMore && sessions.length > 0" class="session-list-end"></div>
    </template>
  </div>
</template>

<script setup>
import { ref, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Archive } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useAgents } from '@/composables/useAgents'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useDialog } from '@/composables/useDialog.ts'
import { useSessionIdentity, reconcileRunningSessions } from '@/composables/useSessionIdentity.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { formatRelativeTime } from '@/utils/format.ts'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'

const props = defineProps({
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
})

const emit = defineEmits(['select', 'archive', 'destroy'])

const { t } = useI18n()
const { getAgentBackend, getAgentName } = useAgents()
const dialog = useDialog()
const { runningSessionsVersion } = useSessionIdentity()

const sessions = ref([])
const loading = ref(false)
const loadingMore = ref(false)
const refreshing = ref(false) // a reload (loadSessions) is in flight
const hasMore = ref(false)
const listRef = ref(null)
const sentinelRef = ref(null)
let observer = null
const pageSize = computed(() => store.state.chatSessionPageSize || 10)
let reloadDebounce = null
let removeEventHandler = null

const sessionsWithStatus = computed(() => {
  void runningSessionsVersion.value
  return sessions.value.map(s => ({
    ...s,
    running: props.runningSessionIds.has(s.id),
  }))
})

async function loadSessions() {
  // Keep the existing list on screen during background refreshes — only show
  // the loading spinner when there is nothing to render yet. This prevents the
  // "clear then refill" flash when a WS-triggered reload fires.
  //
  // Depth preservation: a reload must NOT collapse the list back to the first
  // page when the user has already scrolled through more (loadMoreSessions
  // appended pages). If it did, the shorter list would re-expose the load-more
  // sentinel, the IntersectionObserver would immediately append again, and —
  // while a running session keeps emitting session_update events — the list
  // (and the auto-sized drawer around it) would oscillate in height forever.
  // Instead, re-fetch pages until the fresh list covers the rows the user has
  // already loaded, then swap in place.
  const keepDepth = sessions.value.length
  loading.value = keepDepth === 0
  hasMore.value = false
  refreshing.value = true
  try {
    const fresh = await fetchSessionsUpTo(keepDepth)
    sessions.value = fresh
    reconcileRunningSessions(sessions.value)
  } catch (err) {
    appLog.e('SessionList', 'Failed to load sessions:', err)
    sessions.value = []
  } finally {
    loading.value = false
    refreshing.value = false
    await nextTick()
    setupObserver()
  }
}

/**
 * Fetch session pages until at least `minCount` rows are covered (or the
 * server reports no more), returning the accumulated list. A plain first load
 * passes minCount=0 and behaves like before: one page of pageSize rows.
 * Each page is fetched after the previous one's last row (cursor semantics
 * identical to loadMoreSessions below).
 *
 * The cursor is the row's createdAt, NOT updatedAt: the backend orders and
 * filters paged sessions by created_at (GetSessionsPaged), so sending
 * updatedAt — which is >= createdAt and bumped on every message — makes the
 * `created_at < cursor` filter match rows already shown on the previous page,
 * duplicating the list.
 */
async function fetchSessionsUpTo(minCount) {
  const limit = pageSize.value
  const accumulated = []
  let cursorTime = null
  let cursorId = null
  // Always fetch at least one page; afterwards keep going only when a reload
  // must preserve a deeper list (minCount > pageSize). On a plain first load
  // (minCount = 0) one page is exactly right — the remaining pages are loaded
  // on demand by loadMoreSessions when the user scrolls.
  // Cap the loop so a pathological cursor never spins forever; pageSize is
  // small (default 10) and the depth to preserve is bounded by what the user
  // actually scrolled through.
  let serverHasMore
  let pages = 0
  for (;;) {
    let url = `/api/ai/sessions?limit=${limit}`
    if (cursorTime && cursorId) {
      url += `&cursor=${encodeURIComponent(cursorTime)}&cursor_id=${encodeURIComponent(cursorId)}`
    }
    const resp = await fetch(url)
    const data = await resp.json()
    const list = data.sessions || []
    accumulated.push(...list)
    serverHasMore = !!data.hasMore
    if (typeof data.totalCount === 'number') store.state.sessionCount = data.totalCount
    const last = list[list.length - 1]
    pages++
    if (!last || !serverHasMore || accumulated.length >= minCount || pages >= 20) break
    cursorTime = last.createdAt
    cursorId = last.id
  }
  hasMore.value = serverHasMore
  return accumulated
}

async function loadMoreSessions() {
  if (loadingMore.value || !hasMore.value || refreshing.value) return
  loadingMore.value = true
  try {
    const last = sessions.value[sessions.value.length - 1]
    if (!last) return
    // Cursor = createdAt (backend paginates by created_at, see fetchSessionsUpTo).
    const resp = await fetch(`/api/ai/sessions?limit=${pageSize.value}&cursor=${encodeURIComponent(last.createdAt)}&cursor_id=${encodeURIComponent(last.id)}`)
    const data = await resp.json()
    const more = data.sessions || []
    if (more.length > 0) sessions.value = [...sessions.value, ...more]
    hasMore.value = !!data.hasMore
    if (typeof data.totalCount === 'number') store.state.sessionCount = data.totalCount
  } catch (err) {
    appLog.e('SessionList', 'Failed to load more sessions:', err)
  } finally {
    loadingMore.value = false
  }
}

function setupObserver() {
  if (observer) { observer.disconnect(); observer = null }
  if (!sentinelRef.value || !listRef.value) return
  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMore.value && !loadingMore.value && !refreshing.value) loadMoreSessions()
  }, { threshold: 0.1, rootMargin: '100px', root: listRef.value })
  observer.observe(sentinelRef.value)
}

function selectSession(sessionId, backend) {
  emit('select', sessionId, backend)
}

async function archiveSession(sessionId) {
  const isRunning = props.runningSessionIds.has(sessionId)
  const confirmMsg = isRunning ? t('session.confirmArchiveRunning') : t('session.confirmArchive')
  const confirmed = await dialog.confirm(confirmMsg, {
    confirmText: t('chat.actions.archiveSession'),
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
  if (sessions.value.some(s => s.id === session.id)) return
  sessions.value = [session, ...sessions.value]
}

/** Debounced full reload so bursty WS events (running→completed etc.) coalesce. */
function scheduleReload() {
  if (reloadDebounce) clearTimeout(reloadDebounce)
  reloadDebounce = setTimeout(() => {
    reloadDebounce = null
    loadSessions()
  }, 400)
}

function reload() {
  if (reloadDebounce) clearTimeout(reloadDebounce)
  reloadDebounce = null
  loadSessions()
}

const listNav = useListNav({
  getCount: () => sessionsWithStatus.value.length,
  onConfirm: (idx) => {
    const s = sessionsWithStatus.value[idx]
    if (s) selectSession(s.id, s.backend)
  },
  onActiveChange: scrollActiveIntoView,
})
useListKeys({ isOpen: () => props.isActive, nav: listNav })

function scrollActiveIntoView(index) {
  const items = listRef.value?.querySelectorAll('.session-item') || []
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
}

watch(sessionsWithStatus, () => listNav.reset())

// Real-time sync: reload when the global session list version bumps. This fires
// after create/archive/destroy/read/completion — including cases that don't emit
// a WS session_update event (e.g. mark-as-read, archive). Combined with the WS
// subscription below, the drawer/sidebar list stays fresh without manual refresh.
watch(() => store.state.sessionListVersion, () => {
  reload()
})

defineExpose({ loadSessions, addSessionLocally, reload })

onMounted(() => {
  loadSessions()
  // Real-time: keep the list in sync with session lifecycle events (running,
  // completed, cancelled, permission, title updates). Debounced so a stream
  // of events (e.g. running→completed) triggers one refresh.
  const { onEvent } = useGlobalEvents()
  removeEventHandler = onEvent((event) => {
    if (event === 'session_update') scheduleReload()
  })
})
onUnmounted(() => {
  removeEventHandler?.()
  removeEventHandler = null
  if (reloadDebounce) { clearTimeout(reloadDebounce); reloadDebounce = null }
  if (observer) { observer.disconnect(); observer = null }
})
</script>

<style scoped>
.session-list {
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}

.session-rows {
  display: flex;
  flex-direction: column;
}

.session-list-enter-active,
.session-list-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.session-list-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}

.session-list-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

.session-list-move {
  transition: transform 0.2s ease;
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
}

/* Accent border lives on the row so it encloses the archive button too. */
.session-item.active {
  padding-left: 8px;
}

.session-row.active .session-item {
  border-top-color: transparent;
}

.session-row.session-row-active {
  background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border-radius: 0;
}

.session-row.active {
  border-left: 4px solid var(--accent-color, #0066cc);
  border-right: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  border-top: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  border-bottom: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  box-shadow: inset 0 0 8px color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
}

.session-row.running {
  background: rgba(34, 197, 94, 0.05);
  overflow: hidden;
}

.session-row.running::before {
  content: '';
  position: absolute;
  top: 0;
  left: -60%;
  width: 60%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(34, 197, 94, 0.14), transparent);
  animation: scan-bg 2s ease-in-out infinite;
  pointer-events: none;
  z-index: 0;
}

/* Bottom guide line — dimmed green, sweeps in sync with the full-row light (same
   keyframes/duration/easing so both bands move together). */
.session-running-line {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 1px;
  overflow: hidden;
  pointer-events: none;
  z-index: 1;
}

.session-running-line::after {
  content: '';
  position: absolute;
  top: 0;
  left: -60%;
  width: 60%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(34, 197, 94, 0.5), transparent);
  animation: scan-bg 2s ease-in-out infinite;
}

.session-row.active.running {
  background: rgba(34, 197, 94, 0.05);
}

@media (hover: hover) {
  .session-row:hover {
    background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  }
  .session-row.active.running:hover {
    background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  }
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
  flex-wrap: nowrap;
  overflow: hidden;
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
  animation: badge-breathe 1.2s ease-in-out infinite;
}

@keyframes badge-breathe {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.45; transform: scale(0.8); }
}

@keyframes scan-bg {
  0% { left: -60%; }
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
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
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
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-item-model {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 1;
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted, #999);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-list-sentinel {
  height: 1px;
}

.session-list-end {
  height: 0;
}
</style>
