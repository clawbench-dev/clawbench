<template>
  <div class="session-list" ref="listRef">
    <!-- Only show the full-screen spinner on first load / when the list is empty.
         On background refreshes the existing list stays visible so it can be
         swapped seamlessly to the new data (see loadSessions). -->
    <LoadingIndicator v-if="loading && sessions.length === 0" size="md" :label="t('common.loading')" />
    <div v-else-if="sessions.length === 0" class="session-empty">{{ t('session.noSessions') }}</div>
    <template v-else>
      <!-- Pinned section: only shown when there are pinned sessions -->
      <section v-if="pinnedSessions.length > 0" class="session-section">
        <div class="session-section-header" @click="pinnedCollapsed = !pinnedCollapsed">
          <Pin :size="12" class="session-section-pin-icon" />
          <span class="session-section-title">{{ t('common.pinnedSection') }}</span>
          <span class="session-section-count">{{ pinnedSessions.length }}</span>
          <ChevronDown :size="14" class="session-section-chevron" :class="{ collapsed: pinnedCollapsed }" />
        </div>
        <TransitionGroup v-show="!pinnedCollapsed" name="session-list" tag="div" class="session-rows">
          <div
            v-for="session in pinnedSessions"
            :key="session.id"
            :data-session-id="session.id"
            class="session-row pinned"
            :class="{ active: session.id === currentSessionId, running: session.running, 'menu-open': contextMenu.visible && contextMenu.sessionId === session.id }"
            @contextmenu.prevent="showContextMenu($event, session)"
            v-long-press="onSessionLongPress"
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
      </section>

      <!-- Recent (unpinned) section -->
      <section class="session-section">
        <div v-if="pinnedSessions.length > 0" class="session-section-header" @click="recentCollapsed = !recentCollapsed">
          <span class="session-section-title">{{ t('common.recentSection') }}</span>
          <span class="session-section-count">{{ unpinnedSessions.length }}</span>
          <ChevronDown :size="14" class="session-section-chevron" :class="{ collapsed: recentCollapsed }" />
        </div>
        <TransitionGroup v-show="!recentCollapsed" name="session-list" tag="div" class="session-rows">
          <div
            v-for="(session, idx) in unpinnedSessionsWithStatus"
            :key="session.id"
            :data-session-id="session.id"
            class="session-row"
            :class="{ active: session.id === currentSessionId, running: session.running, 'session-row-active': listNav.activeIndex.value === idx, 'menu-open': contextMenu.visible && contextMenu.sessionId === session.id }"
            @contextmenu.prevent="showContextMenu($event, session)"
            v-long-press="onSessionLongPress"
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
      </section>

      <div ref="sentinelRef" class="session-list-sentinel"></div>
      <LoadingIndicator v-if="loadingMore" size="sm" inline :label="t('common.loading')" />
      <div v-else-if="!hasMore && sessions.length > 0" class="session-list-end"></div>
    </template>
    <!-- Context menu for pin/unpin & rename -->
    <Teleport to="body">
      <div v-if="contextMenu.visible" class="session-context-menu" :style="{ top: contextMenu.y + 'px', left: contextMenu.x + 'px' }" @click="contextMenu.visible = false">
        <button class="session-context-menu-item" @click="togglePin(contextMenu.sessionId, contextMenu.pinned)">
          <span>{{ contextMenu.pinned ? t('common.unpin') : t('common.pin') }}</span>
          <component :is="contextMenu.pinned ? PinOff : Pin" :size="14" class="session-context-menu-icon" />
        </button>
        <button class="session-context-menu-item" @click="renameSessionFromMenu(contextMenu.sessionId)">
          <span>{{ t('common.editSessionName') }}</span>
          <PencilLine :size="14" class="session-context-menu-icon" />
        </button>
        <button class="session-context-menu-item" @click="archiveFromMenu(contextMenu.sessionId)">
          <span>{{ t('common.removeFromList') }}</span>
          <ListX :size="14" class="session-context-menu-icon" />
        </button>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, reactive, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Archive, Pin, PinOff, ChevronDown, PencilLine, ListX } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useAgents } from '@/composables/useAgents'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useDialog } from '@/composables/useDialog.ts'
import { useSessionIdentity, reconcileRunningSessions } from '@/composables/useSessionIdentity.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { formatRelativeTime } from '@/utils/format.ts'
import { apiPatch } from '@/utils/api.ts'
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

const pinnedSessions = computed(() => sessionsWithStatus.value.filter(s => s.pinned))
const unpinnedSessions = computed(() => sessionsWithStatus.value.filter(s => !s.pinned))
const unpinnedSessionsWithStatus = computed(() => unpinnedSessions.value)

const pinnedCollapsed = ref(false)
const recentCollapsed = ref(false)

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
    // A missing createdAt means we cannot form a safe cursor. encodeURIComponent
    // would stringify it to "undefined", and the server filter
    // `created_at < 'undefined'` is lexically true for every date — re-returning
    // page 1 (the duplicate bug). Stop instead of looping.
    if (!last.createdAt) {
      appLog.w('SessionList', 'session missing createdAt; stopping pagination')
      serverHasMore = false
      break
    }
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
    // A missing createdAt cannot form a valid cursor (see fetchSessionsUpTo) —
    // bail out rather than sending cursor=undefined and re-fetching page 1.
    if (!last.createdAt) {
      appLog.w('SessionList', 'last session missing createdAt; stopping pagination')
      hasMore.value = false
      return
    }
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

const contextMenu = reactive({ visible: false, x: 0, y: 0, sessionId: '', pinned: false })

function showContextMenu(event, session) {
  contextMenu.visible = true
  contextMenu.x = event.clientX
  contextMenu.y = event.clientY
  contextMenu.sessionId = session.id
  contextMenu.pinned = !!session.pinned
}

function onSessionLongPress(e, capturedSessionId) {
  // Prefer the session id captured at touchstart time by the directive — it is
  // the id of the row that was actually pressed. Important: inside the
  // directive's setTimeout callback `e.currentTarget` is null (the touch event
  // has already finished dispatching), so reading the DOM attribute from the
  // event target at fire-time can return a different row when TransitionGroup
  // has moved/reused DOM nodes (e.g. after pinning reorders the list).
  let sessionId = capturedSessionId
  if (!sessionId) {
    const el = e.currentTarget || e.target
    sessionId = el?.dataset?.sessionId || el?.closest('[data-session-id]')?.dataset?.sessionId
  }
  if (!sessionId) return
  const session = sessionsWithStatus.value.find(s => s.id === sessionId)
  if (!session) return
  const touch = e.touches[0]
  contextMenu.visible = true
  contextMenu.x = touch.clientX
  contextMenu.y = touch.clientY + 10
  contextMenu.sessionId = sessionId
  contextMenu.pinned = !!session.pinned
  nextTick(() => clampContextMenu())
}

function clampContextMenu() {
  const menu = document.querySelector('.session-context-menu')
  if (!menu) return
  const pad = 8
  const maxX = window.innerWidth - menu.offsetWidth - pad
  const maxY = window.innerHeight - menu.offsetHeight - pad
  contextMenu.x = Math.max(pad, Math.min(contextMenu.x, maxX))
  contextMenu.y = Math.max(pad, Math.min(contextMenu.y, maxY))
}

async function togglePin(sessionId, currentPinned) {
  const newPinned = !currentPinned
  // Optimistic update
  const session = sessions.value.find(s => s.id === sessionId)
  if (session) session.pinned = newPinned
  try {
    await apiPatch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, { pinned: newPinned })
    // Refresh list to ensure correct sort order
    store.state.sessionListVersion++
  } catch (err) {
    // Rollback on failure
    if (session) session.pinned = currentPinned
    appLog.e('SessionList', 'Failed to toggle pin:', err)
  }
}

async function renameSessionFromMenu(sessionId) {
  const session = sessions.value.find(s => s.id === sessionId)
  if (!session) return
  const current = session.title || ''
  const newTitle = await dialog.prompt(
    t('chat.sessionRename.prompt'),
    {
      title: t('chat.sessionRename.title'),
      value: current,
      placeholder: t('chat.sessionRename.placeholder'),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
    }
  )
  if (newTitle === null || newTitle.trim() === '' || newTitle.trim() === current) return
  try {
    await apiPatch(`/api/ai/session/update?session_id=${encodeURIComponent(sessionId)}`, { title: newTitle.trim() })
    session.title = newTitle.trim()
    store.state.sessionListVersion++
  } catch (err) {
    appLog.e('SessionList', 'Failed to rename session:', err)
  }
}

function archiveFromMenu(sessionId) {
  const session = sessions.value.find(s => s.id === sessionId)
  contextMenu.visible = false
  emit('archive', sessionId, session?.backend)
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

watch(sessionsWithStatus, () => listNav.reset())

function scrollActiveIntoView(index) {
  const items = listRef.value?.querySelectorAll('.session-item') || []
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
}

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
  contextMenu.visible = false
  document.removeEventListener('click', closeContextMenuOnOutside)
})

// Close context menu on click outside
function closeContextMenuOnOutside() {
  contextMenu.visible = false
}
onMounted(() => {
  document.addEventListener('click', closeContextMenuOnOutside)
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
  -webkit-touch-callout: none;
  -webkit-user-select: none;
  user-select: none;
}

.session-row.long-pressing .session-item,
.session-row.menu-open .session-item {
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
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

.session-row.pinned .session-item {
  border-top: 1px solid color-mix(in srgb, #f59e0b 15%, var(--border-color, #dee2e6));
}

.session-row.pinned.active .session-item {
  background: var(--accent-bg, rgba(0, 102, 204, 0.1));
}

.session-context-menu {
  position: fixed;
  z-index: 1000;
  background: var(--bg-primary, #fff);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.12);
  padding: 4px 0;
  width: 180px;
}

.session-context-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  padding: 7px 14px;
  border: none;
  background: transparent;
  color: var(--text-primary, #1a1a1a);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.session-context-menu-icon {
  flex-shrink: 0;
  color: var(--text-secondary, #495057);
}

.session-context-menu-item:hover {
  background: color-mix(in srgb, var(--text-primary) 6%, transparent);
}

/* Section groups */
.session-section {
  display: flex;
  flex-direction: column;
}

.session-section-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px 4px;
  cursor: pointer;
  user-select: none;
  -webkit-user-select: none;
}

.session-section-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary, #495057);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.session-section-count {
  font-size: 10px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #e9ecef);
  border-radius: 8px;
  padding: 0 5px;
  line-height: 16px;
}

.session-section-pin-icon {
  color: #f59e0b;
  flex-shrink: 0;
}

.session-section-chevron {
  margin-left: auto;
  color: var(--text-muted, #999);
  transition: transform 0.2s ease;
  flex-shrink: 0;
}

.session-section-chevron.collapsed {
  transform: rotate(-90deg);
}
</style>
