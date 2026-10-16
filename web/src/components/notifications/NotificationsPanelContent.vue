<template>
  <div class="notif-panel">
    <!-- Header: mirrors TaskListPage's .list-header (breadcrumb + icon buttons). -->
    <div class="list-header">
      <div class="notif-breadcrumb">
        <Bell class="crumb-icon" :size="14" />
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
      <MenuSelect
        :model-value="projectValue"
        :options="projectSelectOptions"
        @update:model-value="onProjectChange"
      />
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
      <div class="filter-spacer"></div>
      <span class="filter-count">{{ t('notifications.count', { n: total }) }}</span>
    </div>

    <!-- Body -->
    <div class="notif-body" @scroll="onScroll">
      <div v-if="loading && items.length === 0" class="notif-state">
        <LoadingIndicator size="sm" inline />
        <span>{{ t('notifications.loading') }}</span>
      </div>

      <div v-else-if="error" class="notif-state notif-error">
        <AlertCircle :size="28" />
        <span>{{ error }}</span>
      </div>

      <div v-else-if="items.length === 0" class="notif-state">
        <BellOff :size="30" />
        <span>{{ isFiltered ? t('notifications.emptyFiltered') : t('notifications.empty') }}</span>
        <span v-if="!isFiltered" class="notif-state-hint">{{ t('notifications.emptyHint') }}</span>
      </div>

      <template v-else>
        <div
          v-for="n in items"
          :key="n.id"
          class="notif-card"
          :class="{ 'is-missing': n.sourceMissing }"
          :data-notif-id="n.id"
          @click="onOpen(n)"
        >
          <div class="nc-header">
            <component :is="kindIcon(n)" class="nc-icon" :size="16" />
            <span class="nc-title">{{ n.title || fallbackTitle(n) }}</span>
            <span class="nc-time">{{ formatTime(n.createdAt) }}</span>
          </div>
          <div v-if="n.body" class="nc-body">{{ n.body }}</div>
          <div class="nc-meta">
            <span class="evt" :class="eventTone(n)">
              <component :is="eventIcon(n)" :size="11" />
              {{ eventLabel(n) }}
            </span>
            <span v-if="n.projectPath" class="nc-proj">
              <Folder :size="11" />
              {{ projectName(n.projectPath) }}
            </span>
            <span v-if="n.sourceMissing" class="nc-gone" :title="t('notifications.sourceDeletedHint')">
              <Unlink :size="11" />
              {{ t('notifications.sourceDeleted') }}
            </span>
          </div>
        </div>

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
  Bell, BellOff, Folder, Unlink, Trash2, AlertCircle,
  Bot, Clock, GitMerge, Check, X, ShieldAlert,
} from 'lucide-vue-next'
import MenuSelect, { type MenuSelectOption } from '@/components/common/MenuSelect.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useNotifications, ALL_PROJECTS } from '@/composables/useNotifications'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { store } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
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

const projectSelectOptions = computed<MenuSelectOption[]>(() => {
  const opts: MenuSelectOption[] = [
    { value: '__all__', label: `${t('notifications.projectFilter')}: ${t('notifications.all')}` },
  ]
  // Offer the active project first (the common case), then the rest.
  const active = activeProject.value
  const seen = new Set<string>()
  if (active) {
    opts.push({ value: active, label: projectLabel(active) })
    seen.add(active)
  }
  for (const p of projectOptions.value) {
    if (seen.has(p.path)) continue
    seen.add(p.path)
    opts.push({ value: p.path, label: projectLabel(p.path) })
  }
  return opts
})

/** Compact project label: "name — /full/path", so same-named projects differ. */
function projectLabel(path: string): string {
  const norm = path.replace(/[\\/]+$/, '')
  const idx = Math.max(norm.lastIndexOf('/'), norm.lastIndexOf('\\'))
  const name = idx >= 0 ? norm.slice(idx + 1) || norm : norm
  return name === norm ? name : `${name} — ${norm}`
}

const kindOptions = computed<MenuSelectOption[]>(() => [
  { value: '__all__', label: `${t('notifications.kindFilter')}: ${t('notifications.all')}` },
  { value: 'session', label: t('notifications.kind.session') },
  { value: 'task', label: t('notifications.kind.task') },
  { value: 'forge', label: t('notifications.kind.forge') },
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
    default: return Bot
  }
}

/** Semantic tone for the event chip, keyed off kind + event type. */
function eventTone(n: NotificationItem): string {
  const s = n.eventType
  if (s === 'failed' || s === 'cancelled') return 'danger'
  if (s === 'permission_pending') return 'warning'
  if (n.kind === 'forge') return 'purple'
  if (s === 'running') return 'info'
  return 'success'
}

function eventIcon(n: NotificationItem) {
  const s = n.eventType
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

/** Fallback title when the row has none: the localized kind name, never the raw id. */
function fallbackTitle(n: NotificationItem): string {
  if (n.kind === 'forge') return n.forgeItemKey || t('notifications.kind.forge')
  if (n.kind === 'task') return t('notifications.kind.task')
  return t('notifications.kind.session')
}

function formatTime(iso: string): string {
  return iso ? formatDateTime(iso) : ''
}

/**
 * Open a notification's source.
 *
 * A row whose source is gone must NOT dispatch a navigation event: a deleted
 * session's own fallback silently redirects to the newest session, which would
 * drop the user somewhere unrelated. Show a hint instead.
 */
function onOpen(n: NotificationItem) {
  if (n.sourceMissing) {
    toast.show(t('notifications.sourceDeletedHint'), { icon: 'warning', type: 'info' })
    return
  }
  if (n.kind === 'session' && n.sessionId) {
    window.dispatchEvent(new CustomEvent('clawbench-open-session', {
      detail: { sessionId: n.sessionId, projectPath: n.projectPath },
    }))
  } else if (n.kind === 'task' && n.taskId) {
    window.dispatchEvent(new CustomEvent('clawbench-open-task', {
      detail: { taskId: n.taskId, executionId: n.executionId, projectPath: n.projectPath },
    }))
  } else if (n.kind === 'forge' && n.forgeItemKey) {
    window.dispatchEvent(new CustomEvent('clawbench-open-forge', {
      detail: { projectPath: n.projectPath, target: forgeTargetFromItemKey(n.forgeItemKey) },
    }))
  }
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
  width: 28px;
  height: 28px;
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

/* Filter row */
.notif-filters {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-6);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.filter-spacer { flex: 1; }
.filter-count { font-size: var(--font-size-2xs); color: var(--text-muted); }

/* Body */
.notif-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
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

/* Card — mirrors TaskListPage's .task-item. */
.notif-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-5);
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 0;
  cursor: pointer;
  transition: all var(--duration-slow) ease;
  overflow: hidden;
}
@media (hover: hover) {
  .notif-card:hover {
    border-color: var(--accent-color);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  }
}
.notif-card:active { background: var(--bg-tertiary); }
/* A row whose source is gone is still readable, just dimmed. */
.notif-card.is-missing { opacity: var(--opacity-soft); }

.nc-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}
.nc-icon { flex-shrink: 0; color: var(--text-secondary); }
.nc-title {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.nc-time {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  color: var(--text-hint);
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
.nc-proj {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  background: var(--bg-primary);
  padding: 2px var(--space-4);
  border-radius: var(--radius-xs);
  border: 1px solid var(--border-color);
}
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
