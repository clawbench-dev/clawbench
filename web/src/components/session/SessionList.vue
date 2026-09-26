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

    <!-- ── Project pane: one list, with derived sessions folded into groups ── -->
    <div v-show="activeTab === 'project'" ref="listRef" class="session-list-pane">
      <!-- Only show the full-screen spinner on first load / when the list is empty.
           On background refreshes the existing list stays visible so it can be
           swapped seamlessly to the new data (see loadSessions). -->
      <LoadingIndicator v-if="loading && sessions.length === 0" size="md" :label="t('common.loading')" />
      <div v-else-if="sessions.length === 0" class="session-empty">{{ t('session.noSessions') }}</div>
      <template v-else>
        <!-- Order is the backend's: pinned DESC first (a fixed block at the top),
             then the user's manual drag order (sort_order ASC).
             VueDraggable (SortableJS) owns the list root and reorders
             `sessions` in place via v-model. It replaces the former
             TransitionGroup: both drive the same DOM nodes, and running them
             together fights over the move animation. `animation` keeps the
             smooth slide.

             `sessions` holds only the TOP-LEVEL rows — a session that heads a
             fork group is one entry, and its group is rendered right after it.
             Dragging therefore moves the whole group as a unit, and a group can
             never be split by a drag. Two consequences that keep Sortable's
             index arithmetic honest: hidden rows are omitted from the DOM
             entirely (v-if, never v-show), and `draggable` admits only the
             top-level rows so a group member is never a drag source.

             Gesture: the drag starts ONLY from the row's ⋮ button (`handle`),
             identically on desktop and touch. Dragging the row body stays free
             for its normal jobs — selecting text on desktop, scrolling the list
             on touch — so no press delay is needed and the two can never fight.
             The button still opens the menu on a plain click; Sortable only
             takes over once the pointer actually moves.

             Menu entry point: the row's ⋮ button, on every device. There is
             deliberately no right-click and no long-press handler: a touch
             long-press synthesizes a `contextmenu` event on mobile (it is how
             the platform raises its selection menu), so a right-click binding
             would silently re-create a long-press menu there. The button is the
             single, identical gesture everywhere.

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
          draggable=".session-row.is-top"
          :prevent-on-filter="false"
          :animation="150"
          @move="onDragMove"
          @end="onDragEnd"
        >
          <template v-for="row in visibleRows" :key="row.session.id">
            <div
              :data-session-id="row.session.id"
              class="session-row"
              :class="rowClasses(row)"
            >
              <span v-if="row.running" class="session-running-line"><i v-running-sweep class="session-running-band"></i></span>
              <div
                class="session-item"
                :class="{ active: row.session.id === currentSessionId }"
                @click="selectSession(row.session.id, row.session.backend)"
              >
                <span v-if="row.session.unreadCount > 0 || row.session.pendingApproval" class="session-item-badge"></span>
                <div class="session-item-info">
                  <div class="session-item-header">
                    <span class="session-item-title">{{ row.session.title }}</span>
                    <span v-if="row.depth > 0" class="session-fork-gen" :title="t('session.forkGenerationTitle', { n: row.depth })">{{ t('session.forkGeneration', { n: row.depth }) }}</span>
                  </div>
                  <div class="session-item-meta">
                    <span class="session-item-time">{{ formatRelativeTime(row.session.updatedAt) }}</span>
                    <span class="session-item-agent"><AgentIcon :backend="getAgentBackend(row.session.agentId)" :name="getAgentName(row.session.agentId)" :size="12" /> {{ getAgentName(row.session.agentId) }}</span>
                    <span v-if="row.session.model" class="session-item-model">{{ row.session.model }}</span>
                  </div>
                  <!-- Fork-group toggle, inlined on the anchor row itself.
                       A separate header row made the group read as "a session,
                       then an unrelated section"; putting the control inside the
                       row means the anchor IS the group, so there is only one
                       thing on screen in both states.

                       It is a real <button> so it can be tabbed to, and
                       @click.stop keeps it from also selecting the session
                       underneath (the row's own click handler). It deliberately
                       carries no `.session-item`, because keyboard navigation
                       maps its index onto querySelectorAll('.session-item') —
                       an extra match here would shift every row after it. -->
                  <button
                    v-if="row.isAnchor"
                    class="session-fork-toggle"
                    :class="{ collapsed: isForkCollapsed(row.session.id) }"
                    :aria-expanded="!isForkCollapsed(row.session.id)"
                    :title="t('session.forkGroupTitle')"
                    @click.stop="toggleForkCollapsed(row.session.id)"
                  >
                    <ChevronDown :size="11" class="fork-toggle-chevron" />
                    <GitFork :size="11" />
                    <span>{{ t('session.forkCount', { n: row.childCount }) }}</span>
                  </button>
                  <div v-if="row.session.tags && row.session.tags.length" class="session-item-tags">
                    <span
                      v-for="tag in row.session.tags"
                      :key="tag.name"
                      class="session-tag"
                      :style="tagAccentStyle(tag.name)"
                    >{{ tag.name }}</span>
                  </div>
                </div>
              </div>
              <button class="session-more-btn" :title="t('common.moreActions')" @click.stop="showMenuFromButton($event, row.session)">
                <MoreVertical :size="15" />
              </button>
            </div>
          </template>
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
         button or a right-click on the row. Reuses the shared file-manager
         context menu (.context-menu / .context-menu-item in css/components.css)
         so positioning, styling and viewport clamping stay in one place. -->
    <Teleport to="body">
      <div v-if="contextMenu.visible" class="context-menu visible" :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }" @click.stop @contextmenu.prevent.stop>
        <div class="context-menu-item" @click.stop="togglePin(contextMenu.sessionId, contextMenu.pinned)">
          <component :is="contextMenu.pinned ? PinOff : Pin" :size="14" />
          {{ contextMenu.pinned ? t('common.unpin') : t('common.pin') }}
        </div>
        <div class="context-menu-item" @click.stop="renameSessionFromMenu(contextMenu.sessionId)">
          <PencilLine :size="14" />
          {{ t('common.rename') }}
        </div>
        <div class="context-menu-item" @click.stop="openTagDialogFromMenu(contextMenu.sessionId)">
          <Tags :size="14" />
          {{ t('common.setTags') }}
        </div>
        <!-- Doubles as the share-state indicator (mirrors the file header's
             "Share link" item): highlighted and relabelled when this
             conversation already has a live public link. -->
        <div
          class="context-menu-item"
          :class="{ active: isSessionShared(contextMenu.sessionId) }"
          @click.stop="openShareDialogFromMenu(contextMenu.sessionId)"
        >
          <MessageSquareShare :size="14" />
          {{ isSessionShared(contextMenu.sessionId) ? t('sessionShare.buttonActive') : t('sessionShare.button') }}
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
    <SessionShareDialog
      :open="shareDialog.open"
      :session-id="shareDialog.sessionId"
      @close="shareDialog.open = false"
    />
  </div>
</template>

<script setup>
import { ref, reactive, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { Archive, ChevronDown, Pin, PinOff, PencilLine, MessageSquareShare, Tags, Trash2, MoreVertical, GitFork } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import SessionGroupHeader from '@/components/session/SessionGroupHeader.vue'
import SessionTagDialog from '@/components/session/SessionTagDialog.vue'
import SessionShareDialog from '@/components/session/SessionShareDialog.vue'
import SessionTagFilterBar from '@/components/session/SessionTagFilterBar.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { tagAccentStyle } from '@/utils/tagColor.ts'
import { useAgents } from '@/composables/useAgents'
import { useSessionShare } from '@/composables/useSessionShare'
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
import { buildSessionForkTree } from '@/utils/sessionForkTree.ts'
import { toFixedCSS, getZoomedViewport } from '@/composables/useSettingsConfig'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'
import { buildRenameGenerateOptions } from '@/utils/sessionRename'

const props = defineProps({
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
  activeTab: { type: String, default: 'project' },
})

const emit = defineEmits(['select', 'archive', 'destroy', 'update:activeTab'])
const { t } = useI18n()
const { getAgentBackend, getAgentName } = useAgents()
const { isSessionShared, setSharedSessionIds } = useSessionShare()
const dialog = useDialog()
const toast = useToast()
const { runningSessionsVersion } = useSessionIdentity()
const { groups: crossGroups, loading: crossLoading, loaded: crossLoaded } = useCrossProjectSessions()

// The drag-bound array. It holds only TOP-LEVEL rows: a session that heads a
// fork group is one entry here, and its group members live in
// `membersByAnchor` and are rendered right after it. Dragging therefore moves
// the whole group, and a group can never be split by a drag.
const sessions = ref([])
// anchorId → its group members (shallowest generation first), and every session
// by id (members included). Both are rebuilt from the server response; the
// `byId` map is what the row menu resolves against, since a group member is not
// in `sessions`.
const membersByAnchor = ref(new Map())
const sessionsById = ref(new Map())
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

// Fork-group collapse state, keyed by anchor session id. In-memory only, same
// as the cross-project pane's: the pane is v-show'd (never unmounted) while the
// app runs, so it survives tab switches and list reloads, and resets on page
// reload. Default is EXPANDED — a freshly forked session is the one the user
// just created, so its group should be visible.
const collapsedAnchors = reactive(new Set())
function isForkCollapsed(anchorId) {
  return collapsedAnchors.has(anchorId)
}
function toggleForkCollapsed(anchorId) {
  if (collapsedAnchors.has(anchorId)) collapsedAnchors.delete(anchorId)
  else collapsedAnchors.add(anchorId)
}

/**
 * Expand the group holding `sessionId`, if any.
 *
 * A collapsed anchor must never hide the row the user is actually on: a
 * notification deep link or a cross-project jump can land on a group member
 * whose anchor was collapsed earlier. Auto-expanding (rather than overriding
 * the collapsed flag at render time) keeps the header's chevron truthful and
 * leaves the user free to collapse it again afterwards.
 */
function expandGroupOf(sessionId) {
  if (!sessionId) return
  for (const [anchorId, members] of membersByAnchor.value) {
    if (members.some(m => m.session.id === sessionId)) {
      collapsedAnchors.delete(anchorId)
      return
    }
  }
}

/**
 * The rows actually rendered, in order: each top-level row followed by its
 * group members when expanded. A collapsed group's members are omitted from
 * the DOM entirely (the template v-if's on `isForkCollapsed`), never merely
 * hidden — that keeps the draggable index arithmetic and the keyboard nav
 * index working on exactly the set of visible rows.
 */
const visibleRows = computed(() => {
  void runningSessionsVersion.value
  const out = []
  for (const top of sessions.value) {
    const members = membersByAnchor.value.get(top.id)
    const isAnchor = !!members && members.length > 0
    out.push({
      session: top,
      depth: 0,
      childCount: isAnchor ? members.length : 0,
      isAnchor,
      running: props.runningSessionIds.has(top.id),
    })
    if (!isAnchor || isForkCollapsed(top.id)) continue
    members.forEach((member, i) => {
      out.push({
        ...member,
        // The last member closes the tree rail (└─) instead of continuing it.
        // Explicit rather than CSS `:last-of-type`: the group's rows are
        // siblings of the rows around them, so the last member is not the last
        // child of its type.
        isLastInGroup: i === members.length - 1,
        running: props.runningSessionIds.has(member.session.id),
      })
    })
  }
  return out
})

/** Row modifiers, derived from the row plus the live running set. */
function rowClasses(row) {
  return {
    pinned: row.session.pinned,
    active: row.session.id === props.currentSessionId,
    running: row.running,
    'is-top': row.depth === 0,
    'is-fork-member': row.depth > 0,
    // Closes the tree rail on the last member of a fork group.
    'is-last-in-group': row.depth > 0 && row.isLastInGroup,
    // Heads a fork group. Shares its tint with the group header below it so the
    // two read as one block instead of "a session, then an unrelated section".
    'is-group-anchor': row.isAnchor,
    'session-row-active': visibleRows.value[listNav.activeIndex.value]?.session.id === row.session.id,
    'menu-open': contextMenu.visible && contextMenu.sessionId === row.session.id,
  }
}

/** Resolve a session by id across the whole list, group members included. */
function findSession(sessionId) {
  return sessionsById.value.get(sessionId)
}

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
    const fetched = data.sessions || []
    reconcileRunningSessions(fetched)
    // Group the server order into top-level rows + their groups. `sessions`
    // holds the top-level rows in server order; the members hang off the map.
    const grouping = buildSessionForkTree(fetched)
    sessions.value = grouping.topSessions
    membersByAnchor.value = grouping.membersByAnchor
    sessionsById.value = grouping.byId
    // A reload rebuilds the groups, so a member that is now the current session
    // (opened while the previous snapshot was on screen) needs its group open.
    expandGroupOf(props.currentSessionId)
    if (typeof data.totalCount === 'number') store.state.sessionCount = data.totalCount
  } catch (err) {
    appLog.e('SessionList', 'Failed to load sessions:', err)
    sessions.value = []
    membersByAnchor.value = new Map()
    sessionsById.value = new Map()
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
    const session = findSession(sessionId)
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
 * corner. This is the only menu entry point — see the template comment for why
 * there is no right-click / long-press binding.
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
// SortableJS reorders `sessions` (the top-level rows) in place via v-model, so
// by the time `end` fires the array already holds the new order. We only
// persist it. A drag that ends where it started never reaches here as a
// meaningful change — oldIndex equals newIndex, and the server write is skipped.
//
// A group member is not a drag source: `draggable=".session-row.is-top"` admits
// only top-level rows, so dragging always moves a whole group (members are
// rendered from `membersByAnchor`, which follows the anchor's position).

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

  // Persist the order of every visible session, groups flattened in place.
  //
  // The server numbers `sort_order` per session, and a group's members must
  // keep travelling with their anchor. Posting only the top-level rows would
  // leave the members' sort_order untouched while their anchor moved, so the
  // next load would rebuild the group in a different spot — the drag would
  // appear to undo itself. Flattening the visible order (anchor, its members,
  // next anchor, ...) is exactly the sequence the DOM shows, so the group
  // reassembles around its anchor afterwards.
  //
  // A collapsed group's members are not rendered, so they are not in this
  // list; the server leaves them after the posted prefix in their previous
  // relative order (see service.ReorderSessions), which keeps them adjacent to
  // their anchor because they were already numbered there.
  const visibleUnpinned = visibleRows.value.filter(r => !r.session.pinned)
  visibleUnpinned.forEach((r, i) => { r.session.sortOrder = i })
  const ids = visibleUnpinned.map(r => r.session.id)
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
  const session = findSession(sessionId)
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
  const session = findSession(sessionId)
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
      ...buildRenameGenerateOptions(sessionId),
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
  const session = findSession(sessionId)
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
  const session = findSession(sessionId)
  if (!session) return
  tagDialog.sessionId = sessionId
  tagDialog.initialTags = (session.tags || []).map(tag => tag.name)
  tagDialog.open = true
}

// Conversation share dialog. Only the session id is needed: the dialog loads
// the message list and the existing share state itself.
const shareDialog = reactive({ open: false, sessionId: '' })

function openShareDialogFromMenu(sessionId) {
  closeContextMenu()
  shareDialog.sessionId = sessionId
  shareDialog.open = true
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

// Keyboard navigation indexes visibleRows, whose order is exactly the rendered
// DOM order: each top-level row, then its group members when the group is
// expanded. A collapsed group contributes only its anchor row, and the group
// headers carry no `.session-item`, so neither is reachable by arrow keys.
const listNav = useListNav({
  getCount: () => visibleRows.value.length,
  onConfirm: (idx) => {
    const row = visibleRows.value[idx]
    if (row) selectSession(row.session.id, row.session.backend)
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

watch(visibleRows, () => listNav.reset())

// Reset to the project tab whenever the current project changes: the session we
// just opened belongs to the (new) current project and must be visible in the
// project list, not hidden behind the cross tab.
watch(() => store.state.projectRoot, () => {
  if (props.activeTab !== 'project') emit('update:activeTab', 'project')
  // Tags are per-project: a filter carried across a project switch would hide
  // the new project's sessions behind a tag it may not even have.
  activeTag.value = ''
  loadFilterTags()
  // Shares are project-scoped: the previous project's badges must go.
  void refreshSessionShares()
})

// Bring the active session into view after a cross-project jump. The row may
// not be loaded yet (pagination), so retry once after a reload settles.
watch(() => props.currentSessionId, async (id) => {
  if (!id) return
  // A group member can be hidden behind a collapsed anchor; expand first so the
  // scroll below has a row to find.
  expandGroupOf(id)
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

/**
 * Seed the shared-session set so row badges are correct without the user
 * having to open the shared-conversations drawer first. One list request;
 * failures are silent because a missing badge must never block the list.
 */
async function refreshSessionShares() {
  try {
    const resp = await fetch('/api/share/session/list')
    if (!resp.ok) return
    const data = await resp.json()
    setSharedSessionIds((data.shares || []).map((s) => s.sessionId))
  } catch (err) {
    appLog.w('SessionList', 'refresh session shares failed:', err)
  }
}

defineExpose({ loadSessions, addSessionLocally, reload })

onMounted(() => {
  loadSessions()
  loadFilterTags()
  // Seed the share badges for this project in one request.
  void refreshSessionShares()
  // The session opened before this list mounted (cold start, or a notification
  // deep link) may be a group member whose anchor was never expanded — there is
  // no currentSessionId *change* to observe, so expand once here too.
  expandGroupOf(props.currentSessionId)
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
  /* Width of the accent bar a selected row paints (`.session-row.active`).
     Declared once because two places must agree on it: the border itself, and
     the tree rail's compensation (an absolutely-positioned box is offset from
     the padding box, i.e. inside the border, so the rail would shift by exactly
     this much on the selected row). */
  --row-active-border: 4px;
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

/* ── Fork groups (issue #477) ──
   A derived-session group is its ANCHOR ROW plus the indented members under it.
   There is no separate group header: the anchor row carries the collapse control
   inline (see .session-fork-toggle), so the group is one row in both states —
   expanded it is a row followed by its members, collapsed it is just the row.
   An earlier version rendered a standalone header under the anchor, which made
   the pair read as "a session, then an unrelated section".

   Members are indented and joined to the anchor by a tree rail: a vertical line
   with a horizontal arm to each member, the last one closing it (`├─` / `└─`).
   See .session-row.is-fork-member for why it is drawn with backgrounds rather
   than borders. The indent zone is also tinted one step deeper.

   The indent lives on the row rather than .session-item so the running band and
   the selection tint — both painted on the row — are indented with it instead
   of bleeding back to the pane edge. */
.session-row.is-group-anchor {
  background-color: color-mix(in srgb, var(--text-primary) 4%, transparent);
}

/* The collapse control, inlined on the anchor row's third line.
   Reset from the browser's default button look so it sits in the row's text
   flow, but keep it a real button: it is focusable and reachable by keyboard.
   Accent-coloured so it reads as an affordance rather than more metadata. */
.session-fork-toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  align-self: flex-start;
  margin-top: 1px;
  padding: 0;
  font: inherit;
  font-size: var(--font-size-xs);
  color: var(--accent-color, #0066cc);
  background: none;
  border: none;
  border-radius: var(--radius-xs);
  cursor: pointer;
}
.session-fork-toggle:hover {
  text-decoration: underline;
}
.session-fork-toggle:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 2px;
}
/* Rotates to point right when collapsed, matching the chevron convention the
   cross-project headers use. */
.session-fork-toggle .fork-toggle-chevron {
  transition: transform var(--duration-slow) ease;
  flex-shrink: 0;
}
.session-fork-toggle.collapsed .fork-toggle-chevron {
  transform: rotate(-90deg);
}

/* Members span the FULL row width: the indent is `padding-left`, NOT
   `margin-left`. A margin sits outside the background box, so the indent zone
   was left unpainted and showed the pane's own background as a bright stripe
   down the left of every member row — and it made the members narrower than
   their anchor row.

   ── Tree rail (M1) ──
   Members are joined to their anchor by a tree: a vertical rail down the left,
   with a horizontal arm reaching each member (`├─`), and the last one closing
   the rail (`└─`). That arm on EVERY member is what makes it a tree rather than
   an elbow — an earlier version drew only the final corner, so the middle
   members were indented rows beside a stray vertical with nothing pointing at
   them.

   Drawn with ::before + background layers, NOT with a border. Two reasons:
     - A border always spans the full row height, so the last member's rail could
       not stop at its centre to turn; the corner then had to draw a second
       vertical alongside, producing two parallel lines.
     - ::after is taken by the pinned wedge (.session-row.pinned::after).
   Two background layers paint the rail (1px at x=0 of the box) and the arm (1px
   across the middle), so they meet exactly at the rail with no offset.

   The arm spans the full indent so it reaches the content; `padding-left` then
   starts the content just past the arm's tip. */
.session-row.is-fork-member {
  --tree-line: color-mix(in srgb, var(--text-primary) 22%, transparent);
  padding-left: calc(var(--space-6) + var(--space-4));
  /* `background-image` (not the shorthand) so background-color — set by the
     `active` / `session-row-active` / `menu-open` rules at the same specificity
     — is left alone. */
  background-image: linear-gradient(
    to right,
    color-mix(in srgb, var(--text-primary) 5%, transparent) 0 var(--space-6),
    color-mix(in srgb, var(--text-primary) 2%, transparent) var(--space-6) 100%
  );
}

/* The rail + arm. `left` is the arm's reach: it starts at the rail and stops
   where the content begins, so the corner lines up with the text. */
.session-row.is-fork-member::before {
  content: '';
  position: absolute;
  left: var(--space-6);
  top: 0;
  bottom: 0;
  width: var(--space-4);
  pointer-events: none;
  /* Layer 1: the arm, 1px tall at the row's vertical centre.
     Layer 2: the rail, 1px wide along the box's left edge, full height. */
  background-image:
    linear-gradient(var(--tree-line), var(--tree-line)),
    linear-gradient(var(--tree-line), var(--tree-line));
  background-size: 100% 1px, 1px 100%;
  background-position: 0 50%, 0 0;
  background-repeat: no-repeat;
}

/* Last member: the rail stops at the row's centre so the arm reads as └─
   rather than continuing past it. */
.session-row.is-fork-member.is-last-in-group::before {
  background-size: 100% 1px, 1px 50%;
}

/* Selected member: `.session-row.active` adds a `border-left`, and an
   absolutely-positioned box is offset from the PADDING box — i.e. inside the
   border — so the rail would jump right by the border's width on the selected
   row and no longer line up with the rows above and below it. (The row's text
   does not move: it is compensated by `.session-item.active { padding-left:
   8px }`. The rail needs the same compensation, which is what this is.)
   Both widths come from --row-active-border so they cannot drift apart. */
.session-row.is-fork-member.active::before {
  left: calc(var(--space-6) - var(--row-active-border));
}

/* Generation chip on a group member's title line ("Gen 2" / "第 2 代"). */
.session-fork-gen {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  line-height: 16px;
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  color: var(--text-muted, #999);
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  white-space: nowrap;
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

/* Accent border lives on the row so it encloses the archive button too.
   The padding is pulled in by exactly the border's width so the row's TEXT does
   not shift when the selection appears. The tree rail needs its own copy of this
   compensation — see `.session-row.is-fork-member.active::before`. */
.session-item.active {
  padding-left: calc(var(--space-6) - var(--row-active-border));
}

.session-row.session-row-active {
  background-color: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border-radius: 0;
}

/* Selected-row tint. Declared on the row (not on .session-item) so it fills the
   trailing button cell as well — when it lived on .session-item the cell kept
   showing the row's own background (green for a running session) and the
   selection looked cut short. Painted as a background-image rather than a
   background-color so a running row's green fill still shows through beneath
   the translucent tint instead of being replaced. */
.session-row.active {
  background-image: linear-gradient(
    color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent),
    color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent)
  );
  border-left: var(--row-active-border) solid var(--accent-color, #0066cc);
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

/* The row whose context menu is open keeps the hover tint it just lost.
   Opening the menu paints a full-viewport .ctx-overlay, which swallows :hover
   (measured: the row stops matching :hover while the menu is up), so without
   this the row goes plain at exactly the moment the user needs to see which row
   the menu targets — it read as worse than not right-clicking at all.
   Same 6% as :hover so the tint does not jump brighter on right-click; a
   stronger value would also blur the distinction from .active, which means
   "this is the open conversation", not "this is the row under the menu".
   Deliberately OUTSIDE the (hover: hover) block: touch has no hover to lose,
   and the ⋮ button opens this same menu there, so for touch this is the only
   cue. */
.session-row.menu-open {
  background-color: color-mix(in srgb, var(--text-primary) 6%, transparent);
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
   confirmed entry point for archiving.
   Kept visually flush with the row's right edge (as it was originally) with
   just a small 6px inset — enough that the tap target no longer merges into the
   panel border on mobile, without the icon drifting away from the edge it has
   always sat on. */
.session-more-btn {
  flex-shrink: 0;
  width: 34px;
  margin-right: var(--space-3);
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
   the trailing button cell begins — so it already sits clear of the wedge in
   the row's own top-right corner and needs no offset. */

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
