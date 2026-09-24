<template>
  <div class="session-list">
    <!-- Tag filter bar. Sits above the scroll area (not inside the pane) so it
         stays visible while the list scrolls, and is shared by both hosts
         (pinned sidebar and mobile drawer) since they both render SessionList.
         Only meaningful on the project pane — the cross pane lists other
         projects' sessions, which this project's tags do not describe. -->
    <SessionTagFilterBar
      v-if="activeTab === 'project'"
      :tags="filterTags"
      :active-tag="activeTag"
      @toggle="toggleTagFilter"
    />

    <!-- ── Project pane: one flat, infinite-scrolling list ── -->
    <div v-show="activeTab === 'project'" ref="listRef" class="session-list-pane">
      <!-- Only show the full-screen spinner on first load / when the list is empty.
           On background refreshes the existing list stays visible so it can be
           swapped seamlessly to the new data (see loadSessions). -->
      <LoadingIndicator v-if="loading && sessions.length === 0" size="md" :label="t('common.loading')" />
      <div v-else-if="sessions.length === 0" class="session-empty">{{ t('session.noSessions') }}</div>
      <template v-else>
        <!-- Single flat list. Order is the backend's: pinned DESC first (a fixed
             block at the top), then the user's manual drag order (sort_order
             ASC). This array order is authoritative — see GetSessions.
             VueDraggable (SortableJS) owns the list root and reorders `sessions`
             in place via v-model. It replaces the former TransitionGroup: both
             drive the same DOM nodes, and running them together fights over the
             move animation. `animation` keeps the smooth slide.
             Gesture: the drag starts ONLY from the row's ⋮ button (`handle`),
             identically on desktop and touch. Dragging the row body stays free
             for its normal jobs — selecting text on desktop, scrolling the list
             on touch — so no press delay is needed and the two can never fight.
             The button still opens the menu on a plain click; Sortable only
             takes over once the pointer actually moves.
             Pinned rows are protected on the frontend, not just by the backend:
             `filter` keeps them from being dragged (with preventOnFilter off, so
             their ⋮ menu still opens), and the end-of-drag re-partition in
             pinPinnedRowsToTop is the hard guarantee that a plain row can never
             be left above them. onDragMove only smooths the interaction. -->
        <VueDraggable
          v-model="sessions"
          tag="div"
          class="session-rows"
          handle=".session-more-btn"
          filter=".session-row.pinned"
          :prevent-on-filter="false"
          :animation="150"
          @move="onDragMove"
          @end="onDragEnd"
        >
          <div
            v-for="(session, idx) in sessionsWithStatus"
            :key="session.id"
            :data-session-id="session.id"
            class="session-row"
            :class="{ pinned: session.pinned, active: session.id === currentSessionId, running: session.running, 'session-row-active': listNav.activeIndex.value === idx, 'menu-open': contextMenu.visible && contextMenu.sessionId === session.id }"
          >
            <span v-if="session.running" class="session-running-line"><i v-running-sweep class="session-running-band"></i></span>
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
                <div v-if="session.tags && session.tags.length" class="session-item-tags">
                  <span
                    v-for="tag in session.tags"
                    :key="tag.name"
                    class="session-tag"
                    :style="tagAccentStyle(tag.name)"
                  >{{ tag.name }}</span>
                </div>
              </div>
            </div>
            <button class="session-more-btn" :title="t('common.moreActions')" @click.stop="showMenuFromButton($event, session)">
              <MoreVertical :size="15" />
            </button>
          </div>
        </VueDraggable>
      </template>
    </div>

    <!-- ── Cross-project pane: active sessions in OTHER projects ── -->
    <div v-show="activeTab === 'cross'" class="session-list-pane session-list-pane--cross">
      <LoadingIndicator v-if="crossLoading && !crossLoaded" size="md" :label="t('common.loading')" />
      <div v-else-if="crossGroups.length === 0" class="session-empty">{{ t('session.crossEmpty') }}</div>
      <template v-else>
        <div v-for="group in crossGroups" :key="group.name" class="cross-group">
          <SessionGroupHeader
            :title="group.displayName"
            :count="group.sessions.length"
            :subtitle="group.displayPath"
            :subtitle-title="group.name"
            :collapsed="isCrossCollapsed(group.name)"
            @toggle="toggleCrossCollapsed(group.name)"
          />
          <div v-show="!isCrossCollapsed(group.name)" class="cross-group-rows">
            <div
              v-for="session in group.sessions"
              :key="group.name + '/' + session.id"
              class="cross-session-row"
              :class="{ running: session.running }"
            >
              <span v-if="session.running" class="session-running-line"><i v-running-sweep class="session-running-band"></i></span>
              <div class="cross-session-item" @click="selectCrossSession(session, group.name)">
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
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- Session menu (pin/rename/tags/archive/remove), opened from the row's ⋮
         button. Reuses the shared file-manager context menu (.context-menu /
         .context-menu-item in css/components.css) so positioning, styling and
         viewport clamping stay in one place. -->
    <Teleport to="body">
      <div v-if="contextMenu.visible" class="context-menu visible" :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }" @click.stop @contextmenu.prevent.stop>
        <div class="context-menu-item" @click.stop="togglePin(contextMenu.sessionId, contextMenu.pinned)">
          <component :is="contextMenu.pinned ? PinOff : Pin" :size="14" />
          {{ contextMenu.pinned ? t('common.unpin') : t('common.pin') }}
        </div>
        <div class="context-menu-item" @click.stop="renameSessionFromMenu(contextMenu.sessionId)">
          <PencilLine :size="14" />
          {{ t('common.renameSession') }}
        </div>
        <div class="context-menu-item" @click.stop="openTagDialogFromMenu(contextMenu.sessionId)">
          <Tags :size="14" />
          {{ t('common.setTags') }}
        </div>
        <div class="context-menu-item" @click.stop="archiveFromMenu(contextMenu.sessionId)">
          <Archive :size="14" />
          {{ t('common.archive') }}
        </div>
        <div class="context-menu-divider" />
        <div class="context-menu-item danger" @click.stop="destroyFromMenu(contextMenu.sessionId)">
          <Trash2 :size="14" />
          {{ t('common.remove') }}
        </div>
      </div>
      <!-- Full-viewport click-catcher: one tap/click anywhere dismisses the menu. -->
      <div v-if="contextMenu.visible" class="ctx-overlay" @click="closeContextMenu" />
    </Teleport>

    <SessionTagDialog
      :open="tagDialog.open"
      :session-id="tagDialog.sessionId"
      :initial-tags="tagDialog.initialTags"
      @close="tagDialog.open = false"
    />
  </div>
</template>

<script setup>
import { ref, reactive, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { Archive, Pin, PinOff, PencilLine, Tags, Trash2, MoreVertical } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import SessionGroupHeader from '@/components/session/SessionGroupHeader.vue'
import SessionTagDialog from '@/components/session/SessionTagDialog.vue'
import SessionTagFilterBar from '@/components/session/SessionTagFilterBar.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { tagAccentStyle } from '@/utils/tagColor.ts'
import { useAgents } from '@/composables/useAgents'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useDialog } from '@/composables/useDialog.ts'
import { useToast } from '@/composables/useToast'
import { useSessionIdentity, reconcileRunningSessions } from '@/composables/useSessionIdentity.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { useCrossProjectSessions } from '@/composables/useCrossProjectSessions.ts'
import { formatRelativeTime } from '@/utils/format.ts'
import { apiPatch, apiPut } from '@/utils/api.ts'
import { coalescedJson } from '@/utils/inflightGet.ts'
import { toFixedCSS, getZoomedViewport } from '@/composables/useSettingsConfig'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'

const props = defineProps({
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
  activeTab: { type: String, default: 'project' },
})

const emit = defineEmits(['select', 'archive', 'destroy', 'update:activeTab'])
const { t } = useI18n()
const { getAgentBackend, getAgentName } = useAgents()
const dialog = useDialog()
const toast = useToast()
const { runningSessionsVersion } = useSessionIdentity()
const { groups: crossGroups, loading: crossLoading, loaded: crossLoaded } = useCrossProjectSessions()

const sessions = ref([])
const loading = ref(false)
const refreshing = ref(false) // a reload (loadSessions) is in flight
// Tag filter. Single-select: '' means no filter. Sent to the server so the
// returned set matches what the chips promise.
const activeTag = ref('')
// Tags in use in the current project (with session counts), for the filter bar.
const filterTags = ref([])
const listRef = ref(null)
let reloadDebounce = null
let removeEventHandler = null

const sessionsWithStatus = computed(() => {
  void runningSessionsVersion.value
  return sessions.value.map(s => ({
    ...s,
    running: props.runningSessionIds.has(s.id),
  }))
})

// Display order is the user's manual drag order — matching the backend's
// `ORDER BY sort_order ASC, created_at DESC`. Rendered as one flat list, so the
// rendered DOM order equals sessionsWithStatus order, which is what useListNav
// indexes into.

// Cross-project group collapse state, keyed by absolute project path. In-memory
// only: the pane is v-show'd (never unmounted) while the app runs, so the state
// survives tab switches and list reloads, and resets on page reload.
const crossCollapsed = reactive(new Set())
function isCrossCollapsed(name) {
  return crossCollapsed.has(name)
}
function toggleCrossCollapsed(name) {
  if (crossCollapsed.has(name)) crossCollapsed.delete(name)
  else crossCollapsed.add(name)
}

async function loadSessions() {
  // Keep the existing list on screen during background refreshes — only show
  // the loading spinner when there is nothing to render yet. This prevents the
  // "clear then refill" flash when a WS-triggered reload fires.
  //
  // The whole list is fetched in one request (no pagination): a project has
  // few enough sessions that paging only added cursor bookkeeping, and it made
  // drag-reorder ambiguous (the client could not know about rows it had not
  // loaded). Omitting `limit` selects the backend's full-list path.
  loading.value = sessions.value.length === 0
  refreshing.value = true
  try {
    // Coalesced: this component is mounted twice (pinned sidebar + mobile
    // drawer), and both reload on the same signals, so without this the list is
    // fetched twice for identical data.
    const data = await coalescedJson(`/api/ai/sessions${buildTagQuery()}`)
    sessions.value = data.sessions || []
    if (typeof data.totalCount === 'number') store.state.sessionCount = data.totalCount
    reconcileRunningSessions(sessions.value)
  } catch (err) {
    appLog.e('SessionList', 'Failed to load sessions:', err)
    sessions.value = []
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

/** The tag filter query string, or '' when no filter is applied. */
function buildTagQuery() {
  return activeTag.value ? `&tag=${encodeURIComponent(activeTag.value)}` : ''
}

/**
 * Load the tags in use in the current project for the filter bar. Tags that no
 * session here uses are excluded server-side, so every chip yields a result.
 *
 * Also clears the applied filter when its tag is no longer in use (deleted, or
 * its last session dropped it) — otherwise the list stays constrained by a
 * condition whose chip no longer exists, which the user cannot clear. Callers
 * that may have already issued a request with the old tag must await this
 * before fetching (see the sessionListVersion watcher).
 */
async function loadFilterTags() {
  let tags
  try {
    // Coalesced: both SessionList instances (sidebar + drawer) load tags on
    // mount and on every sessionListVersion bump.
    const res = await coalescedJson('/api/ai/session/tags?inUse=1')
    tags = res.tags || []
  } catch (err) {
    // Keep the previous chips on failure. Treating an error as "no tags" would
    // both hide the bar and drop the user's filter, so a transient failure
    // would silently widen the list with no way back to the filter.
    appLog.e('SessionList', 'Failed to load project tags:', err)
    return
  }
  filterTags.value = tags
  if (activeTag.value && !tags.some(tg => tg.name === activeTag.value)) {
    activeTag.value = ''
  }
}

/** Apply/clear the tag filter from a chip click (single-select toggle). */
function toggleTagFilter(name) {
  activeTag.value = activeTag.value === name ? '' : name
  loadSessions()
}

function selectSession(sessionId, backend) {
  emit('select', sessionId, backend)
}

/**
 * Cross-project row click. Emits the owning project path as a third argument so
 * App.vue can hot-switch projects before opening the session. The row's class is
 * deliberately `.cross-session-item` (NOT `.session-item`): scrollActiveIntoView
 * maps useListNav's index onto `querySelectorAll('.session-item')`, and the nav
 * count only covers project rows — sharing the class would silently corrupt
 * keyboard navigation.
 */
function selectCrossSession(session, projectPath) {
  emit('select', session.id, session.backend, projectPath)
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

/** Resolve a session by id, open the menu anchored at viewport coords. */
function openContextMenu(x, y, sessionId, pinned) {
  contextMenu.visible = true
  // Viewport (getBoundingClientRect) space → position:fixed CSS space: under
  // the app's CSS UI-zoom the raw clientX/clientY would be over-scaled and push
  // the menu past the right/bottom edge. toFixedCSS applies the inverse scale.
  contextMenu.x = toFixedCSS(x)
  contextMenu.y = toFixedCSS(y)
  contextMenu.sessionId = sessionId
  contextMenu.pinned = !!pinned
  nextTick(() => clampContextMenu())
}

/**
 * Open the session menu from the row's ⋮ button, anchored under its bottom-right
 * corner. This is the only menu entry point — the long-press and right-click
 * gestures were removed so the menu behaves the same on every device.
 */
function showMenuFromButton(event, session) {
  const btn = event.currentTarget
  const rect = btn?.getBoundingClientRect?.()
  // Right-align the menu with the button and drop it just below.
  const x = rect ? rect.right : (event.clientX ?? 0)
  const y = rect ? rect.bottom : (event.clientY ?? 0)
  openContextMenu(x, y, session.id, session.pinned)
}

// ── Manual drag reordering (issue #492) ──
// SortableJS reorders `sessions` in place via v-model, so by the time `end`
// fires the array already holds the new order. We only persist it. A drag that
// ends where it started never reaches here as a meaningful change — oldIndex
// equals newIndex, and the server write is skipped.

/**
 * Force the pinned block back to the front of `sessions`.
 *
 * This is the frontend guarantee that a pinned row can never be pushed down —
 * it does NOT rely on Sortable's onMove, which is only advisory and is bypassed
 * whenever the drop lands on the list container rather than a row (the gap
 * above the first row resolves to the container, whose element carries no
 * `pinned` marker, so the live guard waves it through). Whatever order Sortable
 * left the array in, pinned rows are lifted back to the top in their existing
 * relative order, so the DOM can never show a plain row above a pinned one.
 *
 * Returns the same array instance when nothing needed moving, so the caller can
 * skip a redundant reactive write.
 */
function pinPinnedRowsToTop(list) {
  const pinnedCount = list.filter(s => s.pinned).length
  if (pinnedCount === 0 || pinnedCount === list.length) return list
  // Already partitioned? Then the drag stayed out of the pinned block.
  if (list.slice(0, pinnedCount).every(s => s.pinned)) return list
  const pinned = list.filter(s => s.pinned)
  const rest = list.filter(s => !s.pinned)
  return [...pinned, ...rest]
}

/**
 * Live guard: refuse a drop that would place the dragged row at an index inside
 * the pinned block, i.e. above a pinned row.
 *
 * Best-effort only — see pinPinnedRowsToTop for why the end-of-drag
 * re-partition is the actual guarantee. This one just keeps the row from
 * visibly jumping above the block and snapping back on release. It computes the
 * prospective index from the row Sortable would insert next to plus
 * `willInsertAfter`, rather than inspecting that row's class: the target can
 * also be the list container (the gap above the first row), which carries no
 * `pinned` marker and would otherwise be waved through.
 */
function onDragMove(evt) {
  const pinnedCount = sessions.value.filter(s => s.pinned).length
  if (pinnedCount === 0) return true
  const relatedId = evt.related?.dataset?.sessionId
  if (!relatedId) {
    // Container target: appending at the end is fine, inserting at the top is not.
    return evt.willInsertAfter === true
  }
  const idx = sessions.value.findIndex(s => s.id === relatedId)
  if (idx < 0) return true
  return idx + (evt.willInsertAfter ? 1 : 0) >= pinnedCount
}

async function onDragEnd(evt) {
  if (evt.oldIndex === evt.newIndex) return
  // Lift pinned rows back to the top before anything else: Sortable may have
  // inserted the dragged row above them (it only consults onMove when the drop
  // target is a row, so the container gap is unprotected).
  const ordered = pinPinnedRowsToTop(sessions.value)
  if (ordered !== sessions.value) sessions.value = ordered

  // Only the unpinned rows participate: pinned rows are positioned by pinned
  // DESC, so renumbering them would be meaningless (the server ignores their
  // ids anyway — see service.ReorderSessions). The whole visible list is
  // loaded, so the posted ids are exactly the rows the server will renumber,
  // and mirroring that locally keeps the array in sync with storage.
  const unpinned = ordered.filter(s => !s.pinned)
  unpinned.forEach((s, i) => { s.sortOrder = i })
  const ids = unpinned.map(s => s.id)
  try {
    await apiPut('/api/ai/sessions/reorder', { ids })
  } catch (err) {
    appLog.e('SessionList', 'Failed to persist session order:', err)
    toast.show(t('session.reorderFailed'), { icon: '❌', type: 'error' })
    // Roll back to the server's order rather than leaving the DOM diverged.
    loadSessions()
  }
}

function closeContextMenu() {
  contextMenu.visible = false
}

// Clamp menu position to stay within the viewport on all sides. Mirrors
// FileManagerContent.clampCtxMenu: viewport dims and the stored coords are both
// in getBoundingClientRect() space, so the comparison is zoom-consistent.
function clampContextMenu() {
  const menu = document.querySelector('.context-menu.visible')
  if (!menu) return
  const pad = 8
  const vp = getZoomedViewport()
  const vpW = toFixedCSS(vp.width)
  const vpH = toFixedCSS(vp.height)
  const maxX = vpW - menu.offsetWidth - pad
  const maxY = vpH - menu.offsetHeight - pad
  contextMenu.x = Math.max(pad, Math.min(contextMenu.x, maxX))
  contextMenu.y = Math.max(pad, Math.min(contextMenu.y, maxY))
}

async function togglePin(sessionId, currentPinned) {
  closeContextMenu()
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
  closeContextMenu()
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

// Archive goes through archiveSession so the confirmation dialog is preserved.
// The standalone archive button used to be the only confirmed entry point;
// removing it in favour of this menu would otherwise drop the confirmation
// entirely (the menu's sibling "remove" item still confirms, so this keeps the
// two destructive actions consistent).
function archiveFromMenu(sessionId) {
  closeContextMenu()
  return archiveSession(sessionId)
}

async function destroyFromMenu(sessionId) {
  closeContextMenu()
  const session = sessions.value.find(s => s.id === sessionId)
  if (!session) return
  const title = session.title || t('session.unnamed')
  const isRunning = props.runningSessionIds.has(sessionId)
  const confirmed = await dialog.confirm(
    t(isRunning ? 'session.confirmDestroyRunning' : 'session.confirmDestroy', { title }),
    { confirmText: t('common.remove'), title: t('common.remove'), dangerous: true }
  )
  if (confirmed) emit('destroy', sessionId)
}

// Tag dialog state. `initialTags` seeds the checkboxes from the already-loaded
// list so the dialog paints instantly; the PATCH on save is authoritative.
const tagDialog = reactive({ open: false, sessionId: '', initialTags: [] })

function openTagDialogFromMenu(sessionId) {
  closeContextMenu()
  const session = sessions.value.find(s => s.id === sessionId)
  if (!session) return
  tagDialog.sessionId = sessionId
  tagDialog.initialTags = (session.tags || []).map(tag => tag.name)
  tagDialog.open = true
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

// Keyboard navigation indexes sessionsWithStatus, whose order (pinned first,
// then newest-first) is exactly the rendered DOM order. The list is flat, so
// the nav index maps straight onto the rows — no section offset to apply.
const listNav = useListNav({
  getCount: () => sessionsWithStatus.value.length,
  onConfirm: (idx) => {
    const s = sessionsWithStatus.value[idx]
    if (s) selectSession(s.id, s.backend)
  },
  onActiveChange: scrollActiveIntoView,
})
// Keyboard navigation is scoped to the project tab: cross-project rows are
// deliberately outside useListNav (click/tap only), so leaving the nav active
// while the cross pane is shown would let arrow keys scroll invisible rows.
useListKeys({ isOpen: () => props.isActive && props.activeTab === 'project', nav: listNav })

function scrollActiveIntoView(index) {
  const items = listRef.value?.querySelectorAll('.session-item') || []
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
}

watch(sessionsWithStatus, () => listNav.reset())

// Reset to the project tab whenever the current project changes: the session we
// just opened belongs to the (new) current project and must be visible in the
// project list, not hidden behind the cross tab.
watch(() => store.state.projectRoot, () => {
  if (props.activeTab !== 'project') emit('update:activeTab', 'project')
  // Tags are per-project: a filter carried across a project switch would hide
  // the new project's sessions behind a tag it may not even have.
  activeTag.value = ''
  loadFilterTags()
})

// Bring the active session into view after a cross-project jump. The row may
// not be loaded yet (pagination), so retry once after a reload settles.
watch(() => props.currentSessionId, async (id) => {
  if (!id) return
  await nextTick()
  if (scrollActiveRowIntoView()) return
  await nextTick()
  scrollActiveRowIntoView()
})

/** Scroll the row carrying .session-item.active into view. */
function scrollActiveRowIntoView() {
  const rows = listRef.value?.querySelectorAll('.session-row') || []
  for (const row of rows) {
    if (row.querySelector('.session-item.active')) {
      if (typeof row.scrollIntoView === 'function') row.scrollIntoView({ behavior: 'auto', block: 'nearest' })
      return true
    }
  }
  return false
}

// Real-time sync: reload when the global session list version bumps. This fires
// after create/archive/destroy/read/completion — including cases that don't emit
// a WS session_update event (e.g. mark-as-read, archive). Combined with the WS
// subscription below, the drawer/sidebar list stays fresh without manual refresh.
//
// Tag edits and archive/destroy also change which tags are still in use, so the
// chip set is refreshed FIRST and awaited. Order matters: if that refresh clears
// the applied filter (its tag was deleted, or its last session dropped it), the
// reload must observe the cleared value — reloading first would send the dead
// tag and leave the list showing nothing behind a filter bar that has already
// disappeared, with no chip left to clear it.
watch(() => store.state.sessionListVersion, async () => {
  await loadFilterTags()
  reload()
})

defineExpose({ loadSessions, addSessionLocally, reload })

onMounted(() => {
  loadSessions()
  loadFilterTags()
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
  contextMenu.visible = false
})
</script>

<style scoped>
/* Root is a flex column hosting the two mutually-exclusive panes. The tab bar
   itself is rendered by the wrapper (sidebar bottom / BottomSheet #footer) so
   it can stay pinned outside the scroll area. */
.session-list {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

/* Each pane owns its own scrolling so tab switches preserve scroll position. */
.session-list-pane {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.session-rows {
  display: flex;
  flex-direction: column;
}

/* Drag feedback (SortableJS classes). `.sortable-ghost` is the placeholder left
   in the list at the drop position; `.sortable-chosen` is the row being held;
   `.sortable-drag` is the floating element under the pointer. The drag starts
   from the row's ⋮ button, so the grabbed cursor lives on the button. */
.session-row.sortable-ghost {
  opacity: 0.4;
  background-color: color-mix(in srgb, var(--accent-color, #0066cc) 8%, transparent);
}

.session-row.sortable-chosen .session-more-btn {
  cursor: grabbing;
}

.session-row.sortable-drag {
  opacity: 0.9;
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.28);
  border-radius: var(--radius-sm, 6px);
}

.session-empty {
  min-height: 40vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: var(--font-size-md);
}

.session-item {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: var(--space-5) var(--space-6);
  cursor: pointer;
}

/* Accent border lives on the row so it encloses the archive button too. */
.session-item.active {
  padding-left: var(--space-4);
}

.session-row.session-row-active {
  background-color: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border-radius: 0;
}

/* Selected-row tint. Declared on the row (not on .session-item) so it fills the
   34px archive-button cell as well — when it lived on .session-item the archive
   cell kept showing the row's own background (green for a running session) and
   the selection looked cut short. Painted as a background-image rather than a
   background-color so a running row's green fill still shows through beneath
   the translucent tint instead of being replaced. */
.session-row.active {
  background-image: linear-gradient(
    color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent),
    color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent)
  );
  border-left: 4px solid var(--accent-color, #0066cc);
  border-right: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  border-top: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  border-bottom: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  box-shadow: inset 0 0 8px color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
}

/* Running row: the signal is a light band along the bottom edge — a 2px line
   with a soft glow bleeding upward from it. No full-row fill: an earlier
   design tinted the whole row, and on dark themes the accent sits far above
   the row background, so any usable alpha washed the row milky and the band
   on top of it read as a grey smudge. Confining the light to the bottom edge
   removes that trade-off — it carries real colour without touching the row's
   own background (1.44:1 worst case across all 36 themes, vs 1.10:1 for the
   original fixed green).
   The glow is what gives it presence; without it a bare 2px line reads as a
   hairline and is easy to miss while scanning. */
.session-row.running {
  overflow: hidden;
}

/* The band sits on the row's bottom edge. Shared by the local rows and the
   cross-project rows (same class). The chat input button uses its own
   `sweep-light` keyframes instead — it is a chip, not a full-width row, so the
   two are deliberately not the same animation. */
.session-running-line {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 14px;
  overflow: hidden;
  pointer-events: none;
  z-index: 1;
}

/* Static base glow: the full row width stays faintly lit even where the
   travelling band is not. Without this the edge went completely dark between
   passes, so the indicator blinked off and on instead of reading as a
   continuously running session. It shares the band's mask so both layers have
   the same 2px line + upward falloff. */
.session-running-line::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 100%;
  -webkit-mask-image: linear-gradient(to top, #000 0, #000 2px, rgba(0, 0, 0, 0.45) 6px, transparent 14px);
  mask-image: linear-gradient(to top, #000 0, #000 2px, rgba(0, 0, 0, 0.45) 6px, transparent 14px);
  background: var(--running-glow);
}

/* The travelling band. A real element (not `::after`) because its travel is
   driven by v-running-sweep through the Web Animations API, which can only
   target a real node. */
.session-running-band {
  display: block;
  position: absolute;
  top: 0;
  left: 0;
  /* One band, 80% of the row wide, travelling from fully off the left edge to
     fully off the right. The layer is exactly one band — no tile, no repeat —
     so the next pass starts the instant this one clears the right edge: bands
     never double up, and there is no gap between them either.
     The old version tiled two bands across a 200%-wide layer and scrolled it,
     which read as a marquee: a new band entered from the left while the
     previous one was still crossing the middle. */
  width: 80%;
  height: 100%;
  /* Solid 2px at the very bottom, then a fast falloff so the glow stays a
     halo around the line rather than a wash up the row. A plain linear ramp
     to 14px spread the light too thinly and lost the crisp edge. */
  -webkit-mask-image: linear-gradient(to top, #000 0, #000 2px, rgba(0, 0, 0, 0.45) 6px, transparent 14px);
  mask-image: linear-gradient(to top, #000 0, #000 2px, rgba(0, 0, 0, 0.45) 6px, transparent 14px);
  /* Soft shoulders so the band reads as light rather than a solid bar. */
  background-image: linear-gradient(
    90deg,
    transparent 0%,
    var(--running-line) 50%,
    transparent 100%
  );
  /* The travel itself is driven by v-running-sweep (Web Animations API), NOT a
     CSS animation. A CSS animation starts when its element first matches the
     rule, so each row would run at its own phase and lose that phase whenever
     Vue's TransitionGroup reorders rows (which restarts the animation). The
     directive pins every band to the shared document timeline instead, so all
     running sessions sweep in step. This base transform parks the band off the
     left edge, where the directive's first keyframe also holds it. */
  transform: translateX(-100%);
}

/* Hover must still work on a running row — there is no fill to preserve now,
   so the plain hover rule below covers it. */

@media (hover: hover) {
  .session-row:hover {
    background-color: color-mix(in srgb, var(--text-primary) 6%, transparent);
  }
  .session-row.active.running:hover {
    background-color: color-mix(in srgb, var(--text-primary) 8%, transparent);
  }
}

.session-item-info {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
  flex: 1;
}

.session-item-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
}

.session-item-meta {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  flex-wrap: nowrap;
  overflow: hidden;
}

.session-item-title {
  font-size: var(--font-size-md);
  color: var(--text-primary, #1a1a1a);
  font-weight: var(--font-weight-medium);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Tag row: the bottom line of each session entry ──
   Accent comes from --tag-accent-{light,dark}, set inline per tag by
   tagAccentStyle() (same palette as tool calls). --tag-accent resolves the
   theme-appropriate one; color-mix tints background/border from it.

   Wraps onto extra lines rather than the old nowrap + overflow:hidden, which
   silently clipped every tag past the row's width — invisible AND unclickable.
   The row is auto-height inside a scrolling list, so growing is safe; no cap is
   needed here because a session carries only its own handful of tags.

   The extra top margin is what separates the tag row from the meta line above.
   The parent's uniform gap is var(--space-1) (2px), which reads correctly
   between the title and the meta line because both are text with line-height
   half-leading padding them out — but the chips are bordered boxes with no such
   leading, so the same 2px put them visibly flush against the meta line and
   they read as a continuation of it rather than their own row. Adding
   var(--space-2) brings the visual gap to roughly the title→meta one, so all
   three lines share a rhythm. Whitespace alone: a divider here would double up
   with the row separator ~10px below it, and since tags are optional the list
   would alternate between one and two rules per row. */
.session-item-tags {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex-wrap: wrap;
  margin-top: var(--space-2);
}

.session-tag {
  --tag-accent: var(--tag-accent-light);
  /* shrink allowed so a very long name ellipsises on its own line instead of
     forcing the row wider than the pane */
  flex-shrink: 1;
  min-width: 0;
  max-width: 100%;
  padding: 0 var(--space-3);
  border-radius: 999px;
  font-size: var(--font-size-xs);
  line-height: 16px;
  color: var(--tag-accent);
  background: color-mix(in srgb, var(--tag-accent) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--tag-accent) 30%, transparent);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:root[data-theme-base="dark"] .session-tag {
  --tag-accent: var(--tag-accent-dark);
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
  50% { opacity: var(--opacity-disabled); transform: scale(0.8); }
}

/* The sweep's keyframes now live in v-running-sweep (directives/runningSweep.ts)
   rather than here: it drives them through the Web Animations API so every
   running session's band shares one phase (see the directive header for why a
   CSS animation could not do that). The geometry — -100% to 125% of the band's
   own width, `linear` so it flows instead of stuttering into each end — is
   unchanged; it just lives where it can be phase-locked. */

.session-row {
  display: flex;
  align-items: stretch;
  position: relative;
  cursor: pointer;
  /* Row separator lives here (not on .session-item / .session-more-btn) so it
     spans the full row width. Drawn on the two cells it stopped short of the
     trailing button cell, leaving a gap. */
  border-top: 1px solid var(--border-color, #dee2e6);
}

/* Trailing action cell: opens the session menu (pin/rename/tags/archive/
   remove). It replaced the standalone archive button, which was the only
   confirmed entry point for archiving. */
.session-more-btn {
  flex-shrink: 0;
  width: 34px;
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .session-more-btn:hover {
    color: var(--accent-color, #0066cc);
  }
}

.session-more-btn:active {
  color: var(--accent-color, #0066cc);
}

.session-item-time {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-item-agent {
  font-size: var(--font-size-2xs);
  padding:1px var(--space-2);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-medium);
  flex-shrink: 0;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-item-model {
  font-size: var(--font-size-2xs);
  padding:1px var(--space-2);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-medium);
  flex-shrink: 1;
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted, #999);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Pinned marker ──
   Pinned sessions are no longer split into their own section; the only marker
   is a wedge in the row's top-right corner.

   Drawn as a clip-path triangle rather than the old border trick: a border
   triangle can only be one flat colour, while a real box supports the gradient
   + drop shadow that give the wedge its depth. The clip shape is exactly the
   triangle the border version produced — vertices at top-left, top-right and
   bottom-right.

   The gradient runs 225deg, i.e. from the outer tip (top-right) toward the
   hypotenuse, so the facet reads as lit from outside and darkening into the
   crease; the drop shadow lifts it off the row. Both shades are mixed from the
   theme accent, so the whole thing follows the user's colour. The shadow is
   offset inward (down-left) because the pane clips overflow at the right edge.
   `.session-row` is already position:relative, so the wedge anchors to it. */
.session-row.pinned::after {
  content: '';
  position: absolute;
  top: 0;
  right: 0;
  width: 12px;
  height: 12px;
  background: linear-gradient(
    225deg,
    color-mix(in srgb, var(--accent-color, #0066cc) 60%, #fff) 0%,
    var(--accent-color, #0066cc) 55%,
    color-mix(in srgb, var(--accent-color, #0066cc) 78%, #000) 100%
  );
  clip-path: polygon(0 0, 100% 0, 100% 100%);
  filter: drop-shadow(-1px 1px 1.5px rgba(0, 0, 0, 0.35));
  pointer-events: none;
  z-index: 1;
}

/* The unread badge lives at the top-right of `.session-item`, which ends where
   the 34px archive cell begins — so it already sits clear of the wedge in the
   row's own top-right corner and needs no offset. */

/* The context menu itself uses the shared .context-menu / .context-menu-item
   styles from css/components.css (same as the file manager). */

/* ── Cross-project pane ── */

.cross-group + .cross-group {
  border-top: 1px solid var(--border-color, #dee2e6);
}

/* The group header (shared SessionGroupHeader) is the primary visual distinction
   from project rows: it names the owning project so a row can never be mistaken
   for a local one. */
.cross-session-row {
  display: flex;
  align-items: stretch;
  position: relative;
}

.cross-session-row.running {
  overflow: hidden;
}

.cross-session-item {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: var(--space-5) var(--space-6);
  border-top: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  /* Subtle left rail marks rows as belonging to another project. */
  box-shadow: inset 2px 0 0 color-mix(in srgb, var(--text-primary) 12%, transparent);
}

@media (hover: hover) {
  .cross-session-item:hover {
    background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  }
  .cross-session-row.running .cross-session-item:hover {
    background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  }
}
</style>
