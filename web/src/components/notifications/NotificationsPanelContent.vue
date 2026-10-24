<template>
  <div class="notif-panel">
    <!-- Header: mirrors TaskListPage's .list-header (breadcrumb + icon buttons). -->
    <div class="list-header">
      <div class="notif-breadcrumb">
        <Inbox class="crumb-icon" :size="14" />
        <span>{{ t('notifications.title') }}</span>
      </div>
      <RefreshButton
        :size="16"
        class="header-btn"
        :loading="loading"
        :disabled="loading"
        :title="t('notifications.refresh')"
        @click="refresh"
      />
      <button
        class="header-btn"
        :disabled="items.length === 0 || loading"
        :title="t('notifications.clear')"
        data-action="clear"
        @click="onClear"
      >
        <Trash2 :size="14" />
      </button>
    </div>

    <!-- Filters: three dropdowns on one row (project / type / time). -->
    <div class="notif-filters">
      <!-- Project filter mirrors the app-header project menu: an app-surface
           panel with a title, stacked two-line rows (name over path) and an
           accent-highlighted active row. "All projects" is a single-line row
           like the header menu's footer action. -->
      <MenuSelect
        :model-value="projectValue"
        :options="projectSelectOptions"
        :menu-width="300"
        app-surface
        :menu-title="t('notifications.projectFilter')"
        @update:model-value="onProjectChange"
      >
        <template #options>
          <button
            type="button"
            class="app-menu-item"
            :class="{ active: projectValue === '__all__' }"
            :title="t('notifications.allProjects')"
            @click="onProjectChange('__all__')"
          >
            <Layers :size="14" class="item-icon" />
            <span class="item-label">{{ t('notifications.allProjects') }}</span>
          </button>
          <div class="menu-divider"></div>
          <button
            v-for="item in projectMenuItems"
            :key="item.value"
            type="button"
            class="app-menu-item app-menu-item--stacked"
            :class="{ active: projectValue === item.value }"
            :title="item.path"
            @click="onProjectChange(item.value)"
          >
            <Folder :size="14" class="item-icon" />
            <span class="item-body">
              <span class="item-name">{{ item.name }}</span>
              <span class="item-path">{{ item.path }}</span>
            </span>
          </button>
        </template>
      </MenuSelect>
      <MenuSelect
        :model-value="kindValue"
        :options="kindOptions"
        @update:model-value="onKindChange"
      />
      <MenuSelect
        :model-value="rangeValue"
        :options="rangeOptions"
        @update:model-value="onRangeChange"
      />
      <span class="filter-count">{{ t('notifications.count', { n: total }) }}</span>
    </div>

    <!-- Body -->
    <div class="notif-body" @scroll="onScroll">
      <div v-if="loading && items.length === 0" class="notif-skeleton">
        <PanelSkeleton variant="list" :rows="8" :meta="true" :label="t('notifications.loading')" />
      </div>

      <div v-else-if="error" class="notif-state notif-error">
        <AlertCircle :size="28" />
        <span>{{ error }}</span>
      </div>

      <div v-else-if="items.length === 0" class="notif-state">
        <Inbox :size="30" />
        <span>{{ isFiltered ? t('notifications.emptyFiltered') : t('notifications.empty') }}</span>
        <span v-if="!isFiltered" class="notif-state-hint">{{ t('notifications.emptyHint') }}</span>
      </div>

      <template v-else>
        <!-- Grouped timeline: one bucket per local calendar day, rows carry
             only a clock. The date is spelled out exactly once, in the sticky
             header — repeating "10-19 14:32" on every row is what made the
             old flat list unscannable. -->
        <section
          v-for="group in dayGroups"
          :key="group.key"
          class="day-group"
          :data-day="group.key"
        >
          <div class="day-head">
            <span class="day-label">{{ group.label }}</span>
            <span class="day-rule"></span>
            <span class="day-count">{{ t('notifications.count', { n: group.items.length }) }}</span>
          </div>

          <div
            v-for="n in group.items"
            :key="n.id"
            class="notif-card"
            :class="['tone-' + eventTone(n), { 'is-missing': n.sourceMissing }]"
            :data-notif-id="n.id"
            :title="formatFullTimestamp(n.createdAt)"
            @click="onOpen(n)"
          >
            <!-- The rail + node live INSIDE the card's row wrapper so a hovered
                 row can light up its own segment without touching neighbours. -->
            <div class="nc-gutter">
              <span class="nc-time">{{ formatClockTime(n.createdAt) }}</span>
              <span class="nc-rail"><span class="nc-node">
                <component :is="kindIcon(n)" :size="11" />
              </span></span>
            </div>

            <div class="nc-main">
              <div class="nc-header">
                <span class="nc-kind"><component :is="kindIcon(n)" :size="13" /></span>
                <span class="nc-title">{{ n.title || fallbackTitle(n) }}</span>
                <!-- Narrow panel: the gutter is dropped and the clock returns
                     to the header. Hidden at full width (the gutter owns it). -->
                <span class="nc-time-inline">{{ formatClockTime(n.createdAt) }}</span>
              </div>
              <div v-if="n.body" class="nc-body">{{ n.body }}</div>
              <div class="nc-meta">
                <span class="evt" :class="eventTone(n)">
                  <component :is="eventIcon(n)" :size="11" />
                  {{ eventLabel(n) }}
                </span>
                <span v-if="n.sourceMissing" class="nc-gone" :title="t('notifications.sourceDeletedHint')">
                  <Unlink :size="11" />
                  {{ t('notifications.sourceDeleted') }}
                </span>
                <!-- Only OTHER projects are named, and the name is pinned to the
                     right edge (see .nc-proj). A row from the project you are
                     already in needs no label: the absence of a name is the
                     signal, and repeating your own project on every row is noise.
                     The tooltip spells out the "clicking switches projects"
                     consequence, so the row itself stays plain.
                     Placed AFTER the source-deleted chip so it is the rightmost
                     element in every combination. -->
                <span
                  v-if="isExternalProject(n)"
                  class="nc-proj"
                  :title="t('notifications.externalProjectTip', { name: projectName(n.projectPath) })"
                >
                  <Folder :size="11" />
                  {{ projectName(n.projectPath) }}
                </span>
              </div>
            </div>
          </div>
        </section>

        <button v-if="hasMore" class="load-more" :disabled="loadingMore" @click="onLoadMore">
          {{ loadingMore ? t('notifications.loading') : t('notifications.loadMore') }}
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Inbox, Folder, Unlink, Trash2, AlertCircle,
  Bot, Clock, GitMerge, Check, X, ShieldAlert, Layers, Gauge,
} from 'lucide-vue-next'
import MenuSelect, { type MenuSelectOption } from '@/components/common/MenuSelect.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import { useNotifications, ALL_PROJECTS } from '@/composables/useNotifications'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { store } from '@/stores/app'
import { formatClockTime, formatDayLabel, formatFullTimestamp, startOfLocalDay } from '@/utils/format'
import { normalizeSlashes, toProjectRelative } from '@/utils/path'
import type { NotificationItem, NotificationKind, NotificationRange } from '@/utils/notificationsApi'

const props = defineProps<{ active: boolean }>()

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()

const {
  items, total, hasMore, loading, loadingMore, error,
  filterKind, filterRange, filterProject, projectOptions,
  load, loadMore, loadProjects, setKind, setRange, setProject, clear,
} = useNotifications()

// The active project, offered as a shortcut option in the dropdown. It is NOT
// the default — scope is global (see useNotifications).
const activeProject = computed(() => store.state.projectRoot || '')
const isFiltered = computed(() =>
  filterKind.value !== '' || filterRange.value !== '' || filterProject.value !== ALL_PROJECTS,
)

// MenuSelect works in string values, so map the '' (all) sentinel explicitly.
const kindValue = computed(() => filterKind.value || '__all__')
const rangeValue = computed(() => filterRange.value || '__all__')
const projectValue = computed(() => filterProject.value || '__all__')

/**
 * Project rows for the dropdown: the active project first (the common case),
 * then the registry, each de-duplicated. Carries the path separately so the row
 * can render the app-header two-line shape (name over full path) instead of one
 * run-on label.
 */
const projectMenuItems = computed<{ value: string; name: string; path: string }[]>(() => {
  const out: { value: string; name: string; path: string }[] = []
  const seen = new Set<string>()
  const push = (path: string) => {
    if (!path || seen.has(path)) return
    seen.add(path)
    out.push({ value: path, name: projectName(path), path })
  }
  push(activeProject.value)
  for (const p of projectOptions.value) push(p.path)
  return out
})

// projectSelectOptions is what MenuSelect sees; the slot replaces the default
// rows, but the trigger still needs a matching label for the current value.
const projectSelectOptions = computed<MenuSelectOption[]>(() => [
  { value: '__all__', label: `${t('notifications.projectFilter')}: ${t('notifications.all')}` },
  ...projectMenuItems.value.map(i => ({ value: i.value, label: i.name })),
])

/**
 * Rows bucketed by LOCAL calendar day, newest day first.
 *
 * The server returns the log in descending `created_at` order, so a single
 * pass suffices — a Map would lose that order and a sort would re-derive what
 * the query already guarantees. The key is local midnight (not a `YYYY-MM-DD`
 * slice of the ISO string, which is UTC and would put a 23:30 event in the
 * next day for UTC+8 users).
 *
 * One label per bucket, computed here rather than per row: every row in a
 * bucket shares it, and `formatDayLabel` does a date-diff + weekday lookup.
 */
const dayGroups = computed(() => {
  const groups: { key: number; label: string; items: NotificationItem[] }[] = []
  for (const n of items.value) {
    const key = startOfLocalDay(n.createdAt)
    let group = groups[groups.length - 1]
    if (!group || group.key !== key) {
      group = { key, label: formatDayLabel(n.createdAt), items: [] }
      groups.push(group)
    }
    group.items.push(n)
  }
  return groups
})

const kindOptions = computed<MenuSelectOption[]>(() => [
  { value: '__all__', label: `${t('notifications.kindFilter')}: ${t('notifications.all')}` },
  { value: 'session', label: t('notifications.kind.session') },
  { value: 'task', label: t('notifications.kind.task') },
  { value: 'forge', label: t('notifications.kind.forge') },
  { value: 'usage', label: t('notifications.kind.usage') },
])

const rangeOptions = computed<MenuSelectOption[]>(() => [
  { value: '__all__', label: `${t('notifications.rangeFilter')}: ${t('notifications.all')}` },
  { value: 'today', label: t('notifications.range.today') },
  { value: '7d', label: t('notifications.range.7d') },
  { value: '30d', label: t('notifications.range.30d') },
])

function onKindChange(v: string | number) {
  void setKind(v === '__all__' ? '' : (v as NotificationKind))
}
function onRangeChange(v: string | number) {
  void setRange(v === '__all__' ? '' : (v as NotificationRange))
}
function onProjectChange(v: string | number) {
  void setProject(v === '__all__' ? ALL_PROJECTS : String(v))
}

/** Load when the tab becomes visible; the panel is v-show'd, so watch `active`. */
watch(
  () => props.active,
  (now) => {
    if (!now) return
    void loadProjects()
    void load()
  },
  { immediate: true },
)

function refresh() {
  void load()
}

async function onClear() {
  if (!await dialog.confirm(t('notifications.clearConfirm'), { dangerous: true })) return
  try {
    await clear()
    toast.show(t('notifications.cleared'), { icon: 'delete', type: 'success' })
  } catch {
    toast.show(t('common.operationFailed'), { type: 'error' })
  }
}

function onLoadMore() {
  void loadMore()
}

/** Infinite scroll: fetch the next page when near the bottom. */
function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 200) onLoadMore()
}

// ── Row rendering ───────────────────────────────────────────────────────────

function kindIcon(n: NotificationItem) {
  switch (n.kind) {
    case 'task': return Clock
    case 'forge': return GitMerge
    case 'usage': return Gauge
    default: return Bot
  }
}

/** Semantic tone for the event chip, keyed off kind + event type. */
function eventTone(n: NotificationItem): string {
  const s = n.eventType
  if (n.kind === 'usage') return s === 'full' ? 'danger' : 'warning'
  if (s === 'failed' || s === 'cancelled') return 'danger'
  if (s === 'permission_pending') return 'warning'
  if (n.kind === 'forge') return 'purple'
  if (s === 'running') return 'info'
  return 'success'
}

function eventIcon(n: NotificationItem) {
  const s = n.eventType
  if (n.kind === 'usage') return Gauge
  if (s === 'failed' || s === 'cancelled') return X
  if (s === 'permission_pending') return ShieldAlert
  if (n.kind === 'forge') return GitMerge
  return Check
}

function eventLabel(n: NotificationItem): string {
  const map: Record<string, string> = {
    completed: n.kind === 'task' ? 'notifications.event.taskCompleted' : 'notifications.event.sessionCompleted',
    cancelled: n.kind === 'task' ? 'notifications.event.taskCancelled' : 'notifications.event.sessionCancelled',
    permission_pending: 'notifications.event.permissionPending',
    running: 'notifications.event.taskRunning',
    failed: 'notifications.event.taskFailed',
    opened: 'notifications.event.forgeOpened',
    closed: 'notifications.event.forgeClosed',
    merged: 'notifications.event.forgeMerged',
    reopened: 'notifications.event.forgeReopened',
    commented: 'notifications.event.forgeCommented',
    pipeline_done: 'notifications.event.forgePipelineDone',
    over: 'notifications.event.usageOver',
    full: 'notifications.event.usageFull',
  }
  const key = map[n.eventType]
  // Fall back to the raw event type rather than an empty chip: an unknown event
  // must still be readable.
  return key ? t(key) : n.eventType
}

/** Last path segment, for the project chip. */
function projectName(path: string): string {
  const parts = path.replace(/[\\/]+$/, '').split(/[\\/]/)
  return parts[parts.length - 1] || path
}

/**
 * True when the row belongs to a project other than the active one.
 *
 * The log is global (all projects), so most rows on a fresh install belong to
 * somewhere else — and clicking one tears down and rebuilds the whole project
 * subtree. The row has to say so before the click, not after. Rows with no
 * attribution (projectId 0 sentinel, e.g. an unattributable share) are NOT
 * marked: "external" would be a claim about a project we cannot name.
 *
 * The comparison MUST be canonical, not `===`. The two sides come from
 * different producers with different normalization:
 *   - `n.projectPath` is read from the `projects` table, which stores
 *     `model.NormalizeProjectPath` output (filepath.Abs + EvalSymlinks +
 *     Clean) — no trailing slash, symlinks resolved;
 *   - `store.projectRoot` is whatever the last `/api/project` response said,
 *     and the POST branch returns the REQUEST path un-normalized. A path the
 *     user pasted with a trailing slash ("…/clawbench/") or a "./" segment is
 *     stored verbatim, so `===` reports the user's OWN project as external and
 *     labels every one of its rows.
 * `toProjectRelative` is the repo's canonical path comparator (case-insensitive
 * drive letters, either separator style); an empty result means "this IS the
 * root". Fall back to a normalized comparison when the row's path is not under
 * the root.
 */
function isSameProject(rowPath: string, root: string): boolean {
  if (!rowPath || !root) return false
  const normRow = normalizeSlashes(rowPath)
  const normRoot = normalizeSlashes(root)
  // Under the root (or identical) => toProjectRelative returns ''.
  if (toProjectRelative(normRow, normRoot) === '') return true
  // Not under the root: compare normalized forms so a trailing slash or a
  // differing separator still matches ("…/clawbench/" vs "…/clawbench").
  return stripTrailingSlash(normRow).toLowerCase() === stripTrailingSlash(normRoot).toLowerCase()
}

function stripTrailingSlash(p: string): string {
  return p.replace(/\/+$/, '')
}

function isExternalProject(n: NotificationItem): boolean {
  if (!n.projectPath) return false
  return !isSameProject(n.projectPath, activeProject.value)
}

/** Fallback title when the row has none: the localized kind name, never the raw id. */
function fallbackTitle(n: NotificationItem): string {
  if (n.kind === 'forge') return n.forgeItemKey || t('notifications.kind.forge')
  if (n.kind === 'task') return t('notifications.kind.task')
  if (n.kind === 'usage') return t('notifications.kind.usage')
  return t('notifications.kind.session')
}

/**
 * Open a notification's source.
 *
 * A row whose source is gone must NOT dispatch a navigation event: a deleted
 * session's own fallback silently redirects to the newest session, which would
 * drop the user somewhere unrelated. Show a hint instead.
 *
 * A row from ANOTHER project is confirmed first. That jump tears the whole
 * project subtree down and rebuilds it (hotSwitchProject), which is a much
 * bigger action than opening a session in the project you are already in — and
 * it is the one case that cannot offer a Back target (the switch deliberately
 * drops the previous project's navigation context, see hotSwitchProject). The
 * confirmation is what replaces Back there.
 *
 * EVERY row jump goes through `jumpTo`, which stamps `fromNotificationCenter`
 * so App.vue records the log as the jump origin and Back returns here. That is
 * not a per-kind courtesy: adding a row kind that navigates without it leaves
 * the user on a page Back cannot leave (see the wiring test's per-kind guard).
 */
async function onOpen(n: NotificationItem) {
  if (n.sourceMissing) {
    toast.show(t('notifications.sourceDeletedHint'), { icon: 'warning', type: 'info' })
    return
  }
  // Usage notifications are instance-wide: the row carries no project, and the
  // click does not switch projects. Short-circuit BEFORE the cross-project gate
  // so a future change that starts attributing a project to these rows cannot
  // make it ask "switch to project X?" for an action that never switches.
  //
  // Narrow screens land on the settings page, so this jump needs the origin as
  // much as the session/task/forge ones do — the press would otherwise only pop
  // the settings nav stack and the next one exits the app.
  if (n.kind === 'usage') {
    jumpTo('clawbench-open-usage-meter', {})
    return
  }

  // Same canonical comparison as isExternalProject — a raw-vs-canonical path
  // pair must not pop a "switch project?" dialog for the project you are in.
  const crossProject = isExternalProject(n)
  if (crossProject) {
    const ok = await dialog.confirm(
      t('notifications.crossProjectConfirm', { name: projectName(n.projectPath) }),
      {
        title: t('notifications.crossProjectConfirmTitle'),
        confirmText: t('notifications.crossProjectConfirmOk'),
      },
    )
    if (!ok) return
  }

  switch (n.kind) {
    case 'session':
      if (n.sessionId) {
        jumpTo('clawbench-open-session', {
          sessionId: n.sessionId,
          projectPath: n.projectPath,
        })
      }
      return
    case 'task':
      if (n.taskId) {
        jumpTo('clawbench-open-task', {
          taskId: n.taskId,
          executionId: n.executionId,
          projectPath: n.projectPath,
        })
      }
      return
    case 'forge':
      if (n.forgeItemKey) {
        jumpTo('clawbench-open-forge', {
          projectPath: n.projectPath,
          target: forgeTargetFromItemKey(n.forgeItemKey),
        })
      }
      return
    default: {
      // Compile-time exhaustiveness for `NotificationKind`. A new kind must be
      // given a destination (and go through `jumpTo`, so it is returnable); the
      // compiler rejects this file until it is.
      const unhandled: never = n.kind
      void unhandled
    }
  }
}

/**
 * Dispatch a row jump, always flagged as coming from the log.
 *
 * This is the ONLY place the panel may dispatch a `clawbench-open-*` event. It
 * exists so the origin flag cannot be forgotten per kind: Back has to return to
 * the notification center for every destination the log can open, and the flag
 * is what tells App.vue to record that origin.
 */
function jumpTo(event: string, detail: Record<string, unknown>) {
  window.dispatchEvent(new CustomEvent(event, {
    detail: { ...detail, fromNotificationCenter: true },
  }))
}

/**
 * Rebuild a forge deep-link target from the stored opaque key.
 *
 * The key is "issue/12" / "pr/34" / "pipeline/run:56". A pipeline's number is
 * always 0, so the run id is its identity — matching the writer's format.
 */
function forgeTargetFromItemKey(key: string) {
  if (key.startsWith('pipeline/run:')) {
    const runId = Number(key.slice('pipeline/run:'.length)) || 0
    return { type: 'pipeline', number: 0, runId, itemKey: key }
  }
  const [type, numStr] = key.split('/')
  const number = Number(numStr) || 0
  return { type, number, runId: 0, itemKey: key }
}
</script>

<style scoped>
.notif-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-primary);
}

/* Header — unified with the task/forge panel headers. */
.list-header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding: 0 var(--space-2) 0 var(--space-6);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color);
  gap: var(--space-3);
}
.notif-breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  color: var(--text-primary);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
  overflow: hidden;
}
.crumb-icon { color: var(--accent-color); flex-shrink: 0; }
.header-btn {
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all var(--duration-slow) ease;
}
@media (hover: hover) {
  .header-btn:hover:not(:disabled) {
    background: var(--bg-tertiary);
    color: var(--accent-color);
  }
}
.header-btn:active:not(:disabled) { transform: scale(0.9); }
.header-btn:disabled { opacity: var(--opacity-muted); cursor: not-allowed; }

/* Filter row — three dropdowns + the count.
 *
 * Must survive the left panel's 320px minimum width (the splitter can be
 * dragged that narrow) and small phones. Three content-sized triggers plus gaps
 * exceed that, so the row WRAPS instead of overflowing: with the old
 * non-wrapping row the third dropdown was clipped and the count was pushed
 * entirely off-screen (measured scrollWidth 355 vs clientWidth 320).
 *
 * Wrapping (not shrinking) is deliberate: a shrink would ellipsise the labels
 * ("项目: 全…"), which is worse than moving the count to a second line. This is
 * a container-relative fix — a viewport @media query would be wrong, since on
 * wide screens the panel's width comes from the split ratio, not the viewport. */
.notif-filters {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-6);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  flex-wrap: wrap;
}
/* Keep each trigger at its natural width so wrapping is chosen over ellipsis. */
.notif-filters :deep(.menu-select) { flex-shrink: 0; }
/* Pushed right on the dropdowns' line; wraps to its own line when they fill it. */
.filter-count {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  white-space: nowrap;
  margin-left: auto;
}

/* Body */
.notif-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  /* No top padding: the sticky day header would stick BELOW it, leaving an
     unpainted band above through which the previous card scrolls into view.
     The first header's own top padding provides the inset instead. */
  padding: 0 var(--space-6) var(--space-9);
  display: flex;
  flex-direction: column;
  /* The panel can be dragged narrow (the splitter goes to 320px) and is the
     whole screen on a phone. The narrow fallback below is CONTAINER-relative
     on purpose: on a wide screen the panel is still narrow, so a viewport
     @media query would answer the wrong question. */
  container-type: inline-size;
}

.notif-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-6);
  color: var(--text-muted);
  font-size: var(--font-size-lg);
  text-align: center;
}
.notif-state-hint { font-size: var(--font-size-sm); }
.notif-error { color: var(--color-red); }

/* First load: preview the row shape in place of a lone spinner. */
.notif-skeleton {
  padding: var(--space-2) 0;
}

/* ── Day bucket ──────────────────────────────────────────────────────────
 * The date is spelled out ONCE per bucket, in a sticky header, and rows carry
 * only a clock. That is the whole point of the grouping: a flat list repeating
 * "10-19 14:32" on every row cannot be scanned vertically. */
.day-group {
  display: flex;
  flex-direction: column;
  /* Same flex-item trap as the cards (see .notif-card): without this a long
     log compresses the buckets instead of scrolling the body. */
  flex-shrink: 0;
}
.day-group + .day-group { margin-top: var(--space-2); }

.day-head {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: var(--space-4);
  /* The body has no top padding (see .notif-body): this header's own padding is
     what separates it from the header bar above, and its opaque background has
     to reach the very top of the scrollport or the row it is covering would
     peek through as it scrolls under. */
  padding: var(--space-4) 0 var(--space-3);
  background: var(--bg-primary);
}
.day-label {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  letter-spacing: .04em;
  white-space: nowrap;
}
.day-rule {
  flex: 1;
  height: 1px;
  background: var(--border-color);
}
.day-count {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

/* ── Timeline row ──────────────────────────────────────────────────────────
 * [clock] [rail + node] [card]. The card box is `.nc-main`, NOT the row: the
 * row also owns the gutter, and a border around the gutter would draw the
 * clock inside the card. */
.notif-card {
  position: relative;
  display: grid;
  /* gutter (auto) then card (1fr) — the gutter sizes to its content so the
     clock column can shrink away at the narrow breakpoint. */
  grid-template-columns: auto 1fr;
  column-gap: 0;
  align-items: start;
  padding-bottom: var(--space-4);
  cursor: pointer;
  flex-shrink: 0;
}

/* Gutter = clock over rail. `align-self: stretch` makes the rail run the full
 * height of the row so consecutive rows form one continuous line. */
.nc-gutter {
  display: flex;
  align-items: flex-start;
  align-self: stretch;
  min-height: 0;
}
.nc-time {
  width: calc(38 * var(--dp));
  flex-shrink: 0;
  padding-top: calc(9 * var(--dp));
  font-size: var(--font-size-2xs);
  line-height: 1;
  /* NOT --text-hint: at 10px on a white card that is ~2.6:1. The clock is
     load-bearing information, so it clears the muted tier. */
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  letter-spacing: .02em;
  text-align: right;
  white-space: nowrap;
}
.nc-rail {
  position: relative;
  width: calc(26 * var(--dp));
  flex-shrink: 0;
  align-self: stretch;
  display: flex;
  justify-content: center;
}
/* Drawn per row (not once per bucket) so a hovered row can light up its own
   segment without touching its neighbours. `bottom` reaches into the row's
   padding so the line stays continuous across the gap. */
.nc-rail::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: calc(-1 * var(--space-4));
  width: 1px;
  background: var(--border-color);
  transition: background var(--duration-base) ease;
}
/* Start at the first node, stop at the last: a bucket's rail must not run off
   into the header above or the next bucket below. */
.day-head + .notif-card .nc-rail::before { top: 16px; }
.day-group > .notif-card:last-child .nc-rail::before { bottom: auto; height: 16px; }
/* A single-event day has nothing to connect — no rail at all. */
.day-head + .notif-card:last-child .nc-rail::before { display: none; }

.nc-node {
  position: relative;
  z-index: 1;
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
  margin-top: calc(6 * var(--dp));
  border-radius: var(--radius-full);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: var(--tone);
  background: color-mix(in srgb, var(--tone) 13%, var(--bg-primary));
  border: 1px solid color-mix(in srgb, var(--tone) 45%, transparent);
  transition: transform var(--duration-base) ease, box-shadow var(--duration-base) ease;
}
@media (hover: hover) {
  .notif-card:hover .nc-node {
    transform: scale(1.14);
    box-shadow: 0 0 0 calc(4 * var(--dp)) color-mix(in srgb, var(--tone) 12%, transparent);
  }
  .notif-card:hover .nc-rail::before {
    background: color-mix(in srgb, var(--tone) 40%, var(--border-color));
  }
}

/* One variable feeds the node, the rail and the hovered card edge. The tones
   mirror the `.evt.*` status colours below so a row reads as one colour. */
.notif-card.tone-success { --tone: var(--color-green); }
.notif-card.tone-danger { --tone: var(--color-red); }
.notif-card.tone-warning { --tone: var(--color-orange); }
.notif-card.tone-info { --tone: var(--color-info); }
.notif-card.tone-purple { --tone: var(--color-purple); }

/* ── Card ──────────────────────────────────────────────────────────────── */
.nc-main {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-5) var(--space-4);
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 0;
  min-width: 0;
  transition: background var(--duration-base) ease, border-color var(--duration-base) ease,
    box-shadow var(--duration-base) ease;
}
@media (hover: hover) {
  .notif-card:hover .nc-main {
    border-color: color-mix(in srgb, var(--tone) 55%, var(--border-color));
    background: color-mix(in srgb, var(--tone) 4%, var(--bg-secondary));
  }
}
.notif-card:active .nc-main { background: var(--bg-tertiary); }
/* A row whose source is gone is still readable, just dimmed. */
.notif-card.is-missing .nc-main { opacity: var(--opacity-soft); }

.nc-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}
/* The kind glyph moved to the timeline node; it comes back here only when the
   gutter is dropped (narrow panel). */
.nc-kind { display: none; color: var(--text-muted); flex-shrink: 0; }
.nc-title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  line-height: 1.35;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
/* The clock in the card header — only rendered when the gutter is dropped. */
.nc-time-inline {
  display: none;
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.nc-body {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-normal);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.nc-meta {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  flex-wrap: wrap;
}

/* Narrow panel: the clock would leave ~210px for the text at the splitter's
   320px minimum, so the gutter collapses and the clock returns to the card
   header. Container query, not @media — see .notif-body. */
@container (max-width: 380px) {
  .nc-time { display: none; }
  .nc-rail { width: 20px; }
  .nc-time-inline { display: inline; }
  .nc-kind { display: inline-flex; }
}
/* The project name on an OTHER-project row.
 *
 * Deliberately plain: no tint, no border, no badge. Naming the project IS the
 * distinction — a row with no name is one from the project you are in, and
 * that is the whole signal. An earlier version added an orange rail plus an
 * "Other project" pill, which read as a status/severity and outweighed the
 * event chip that owns that role. The name is muted like the rest of the meta
 * row; the tooltip carries the "clicking switches projects" consequence. */
.nc-proj {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  /* Pinned to the right edge of the meta row. The project is context about the
   * ROW, not another event attribute, so it reads better as a right-aligned
   * affordance than as one more chip in the left-hand run. `margin-left: auto`
   * (not `justify-content`) so it works whatever else the row holds — the
   * source-deleted chip, when present, stays next to the event chip. */
  margin-left: auto;
}
.nc-proj > svg { flex-shrink: 0; }
.nc-gone {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--text-hint);
}

/* Event chip — icon + label, tinted from the status tokens. */
.evt {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  padding: 1px var(--space-4);
  border-radius: var(--radius-full);
  border: 1px solid transparent;
  line-height: var(--line-height-normal);
  white-space: nowrap;
}
.evt.success {
  color: var(--color-green);
  background: color-mix(in srgb, var(--color-green) 16%, transparent);
  border-color: color-mix(in srgb, var(--color-green) 40%, transparent);
}
.evt.danger {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 16%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 40%, transparent);
}
.evt.warning {
  color: var(--color-orange);
  background: color-mix(in srgb, var(--color-orange) 16%, transparent);
  border-color: color-mix(in srgb, var(--color-orange) 40%, transparent);
}
.evt.info {
  color: var(--color-info);
  background: color-mix(in srgb, var(--color-info) 16%, transparent);
  border-color: color-mix(in srgb, var(--color-info) 40%, transparent);
}
.evt.purple {
  color: var(--color-purple);
  background: color-mix(in srgb, var(--color-purple) 16%, transparent);
  border-color: color-mix(in srgb, var(--color-purple) 40%, transparent);
}

.load-more {
  align-self: center;
  /* Same flex-item trap as .notif-card: without this the button is squashed
     when the list overflows. */
  flex-shrink: 0;
  border: 1px solid var(--border-color);
  background: var(--bg-secondary);
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  font-family: inherit;
  padding: var(--space-4) var(--space-7);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--duration-base);
}
@media (hover: hover) {
  .load-more:hover:not(:disabled) { background: var(--bg-tertiary); }
}
.load-more:disabled { opacity: var(--opacity-muted); cursor: not-allowed; }
</style>
