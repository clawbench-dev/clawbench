<template>
  <!-- Read-state filter. Same chip affordance as the state/mine chips on the
       issues and PR tabs, and the status chips on the pipelines tab, so all four
       views of this panel are filtered the same way. -->
  <div class="forge-toolbar">
    <div class="forge-chips forge-chips-scroll">
      <button
        v-for="f in FORGE_ACTIVITY_FILTERS"
        :key="f"
        class="forge-chip"
        :class="{ active: unread.filter.value === f }"
        @click="unread.setFilter(f)"
      >{{ t(`forge.overview.filter.${f}`) }}</button>
    </div>
  </div>

  <div v-if="unread.error.value" class="forge-error-card">
    <AlertCircle :size="18" class="forge-error-icon" />
    <div class="forge-error-text">
      <div class="forge-error-title">{{ errorTitle(unread.error.value.code) }}</div>
      <div class="forge-error-body">{{ unread.error.value.message }}</div>
    </div>
    <button class="fbtn" @click="reload">{{ t('forge.retry') }}</button>
  </div>

  <div v-else-if="unread.loading.value && !unread.loaded.value" class="forge-loading">
    <LoadingIndicator size="md" :label="t('forge.loading')" />
  </div>

  <div v-else-if="unread.items.value.length === 0" class="forge-state">
    <div class="forge-empty-card">
      <Rss :size="34" :stroke-width="1.5" class="forge-empty-icon" />
      <!-- The empty state names the view it belongs to: "nothing unread" and
           "nothing read yet" are different facts, and one shared string would
           read as a bug in whichever view it did not describe. -->
      <div class="forge-empty-title">{{ t(`forge.overview.empty.${unread.filter.value}`) }}</div>
      <div class="forge-empty-hint">{{ t(`forge.overview.emptyHint.${unread.filter.value}`) }}</div>
    </div>
  </div>

  <div v-else class="forge-list">
    <div
      v-for="row in unread.items.value"
      :key="row.itemKey"
      class="forge-row forge-overview-row"
      :class="{ unread: !isRead(row), read: isRead(row) }"
      @click="onRowClick(row)"
    >
      <span class="forge-state-dot" :class="dotClass(row)"></span>
      <div class="forge-row-main">
        <div class="forge-row-title">
          <span class="forge-row-text">{{ label(row) }}</span>
          <span
            v-if="!isRead(row)"
            class="forge-unread-dot"
            :title="t('forge.unreadItem')"
            :aria-label="t('forge.unreadItem')"
          ></span>
        </div>
        <div class="forge-row-meta">
          <span v-if="row.eventCount > 1" class="forge-overview-count-badge">
            {{ t('forge.overview.eventCount', { count: row.eventCount }) }}
          </span>
          <span class="forge-row-time">{{ formatTime(row.updatedAt) }}</span>
        </div>
      </div>
      <ChevronRight :size="16" class="forge-row-chevron" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, ChevronRight, Rss } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgeUnreadItems, FORGE_ACTIVITY_FILTERS } from '@/composables/useForge'
import { forgeOverviewLabel } from '@/utils/forgeEventLabels'
import type { ForgeUnreadItem } from '@/utils/forgeApi'

/**
 * The unread list, rendered INSIDE the forge panel's own "unread" tab.
 *
 * Deliberately not a panel: the host owns the header (refresh + "mark all read"),
 * the binding/unbound states and the detail view. This component only owns the
 * three list states and the rows, so the two cannot disagree about read state or
 * duplicate the header chrome.
 */
const props = defineProps<{
  /**
   * Whether this list is on screen. The host passes a composite of its own
   * `active` prop AND the internal tab being selected — otherwise switching to
   * the Issues tab would leave this fetching in the background.
   */
  active: boolean
  projectPath: string
}>()

const emit = defineEmits<{
  (
    e: 'open-item',
    payload: { type: ForgeUnreadItem['type']; number: number; runId: number; itemKey: string },
  ): void
}>()

const { t } = useI18n()

const unread = useForgeUnreadItems(() => props.projectPath)

/** Load on first activation and whenever the project changes. */
watch(
  () => [props.active, props.projectPath] as const,
  ([active, path], prev) => {
    if (!active) return
    const pathChanged = prev && prev[1] !== path
    if (!unread.loaded.value || pathChanged) void unread.load()
  },
  { immediate: true },
)

/** Reload from the server. The badge re-derives server-side, never by decrement. */
function reload() {
  void unread.load()
}

function label(row: ForgeUnreadItem): string {
  return forgeOverviewLabel(row)
}

/**
 * Whether the row should render as read.
 *
 * Server state OR the local optimistic flag: the server flag is authoritative
 * for a freshly loaded row, while `locallyRead` covers the moment between the
 * click and the next reload — without it the dot would pop back on for a frame
 * after the user opened a row.
 */
function isRead(row: ForgeUnreadItem): boolean {
  return Boolean(row.read || row.locallyRead)
}

/** Status dot: a pipeline shows its run outcome, an item its state. */
function dotClass(row: ForgeUnreadItem): string {
  return row.type === 'pipeline' ? 'pipeline-unknown' : 'open'
}

function onRowClick(row: ForgeUnreadItem) {
  // Mark read (locally AND server-side) so the dot clears at once and the badge
  // settles, then open. The row stays in the list (greyed out) rather than being
  // spliced — removing it would shift every row below the cursor as the user
  // clicks. It disappears on the next reload.
  void unread.markRowRead(row.itemKey)
  emit('open-item', {
    type: row.type,
    number: row.number,
    runId: row.runId,
    itemKey: row.itemKey,
  })
}

/** Same rendering the other forge lists use (a local helper there, not a util). */
function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString()
}

function errorTitle(code: string): string {
  switch (code) {
    case 'ForgeAuthFailed': return t('forge.error.auth')
    case 'ForgeRateLimited': return t('forge.error.rateLimit')
    case 'ForgeNetworkError': return t('forge.error.network')
    case 'ForgeNoBinding': return t('forge.empty.noBindingHeader')
    default: return t('forge.error.generic')
  }
}

/**
 * Apply "mark all read" to the rows without a request.
 *
 * The host's "mark all read" already performs the repo-wide write through the
 * shared badge composable; calling the list's own markAllRead here would issue a
 * second identical POST.
 *
 * What "read" means for the LIST depends on the view: in the unread view the
 * rows no longer belong once they are read, so they go; in the all/read views
 * the same items are still valid, they are simply read now — dropping them there
 * would make "mark all read" look like "delete everything".
 */
function clearLocal() {
  if (unread.filter.value === 'unread') {
    unread.items.value = []
    return
  }
  for (const row of unread.items.value) row.locallyRead = true
}

/**
 * Exposed so the host's header refresh button can spin for this tab too.
 *
 * Each tab's list owns its own loading flag (this one, `items`, `pipelines`),
 * so the host derives the button's spin state from whichever list is on screen;
 * without exposing it, the header button would sit still on this tab.
 */
defineExpose({ reload, clearLocal, loading: unread.loading })
</script>

<style scoped>
.forge-overview-row.read {
  opacity: 0.55;
}

.forge-overview-count-badge {
  font-size: 11px;
  color: var(--text-secondary, #6b7280);
}

.forge-overview-row.read .forge-row-text {
  text-decoration: line-through;
  text-decoration-color: var(--text-secondary, #6b7280);
}
</style>
