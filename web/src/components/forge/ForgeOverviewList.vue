<template>
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
      <Inbox :size="34" :stroke-width="1.5" class="forge-empty-icon" />
      <div class="forge-empty-title">{{ t('forge.overview.empty') }}</div>
      <div class="forge-empty-hint">{{ t('forge.overview.emptyHint') }}</div>
    </div>
  </div>

  <div v-else class="forge-list">
    <div
      v-for="row in unread.items.value"
      :key="row.itemKey"
      class="forge-row forge-overview-row"
      :class="{ unread: !row.read, read: row.read }"
      @click="onRowClick(row)"
    >
      <span class="forge-state-dot" :class="dotClass(row)"></span>
      <div class="forge-row-main">
        <div class="forge-row-title">
          <span class="forge-row-text">{{ label(row) }}</span>
          <span
            v-if="!row.read"
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
import { AlertCircle, ChevronRight, Inbox } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgeUnreadItems } from '@/composables/useForge'
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
 * Drop the rows without a request.
 *
 * The host's "mark all read" already performs the repo-wide write through the
 * shared badge composable; calling the list's own markAllRead here would issue a
 * second identical POST.
 */
function clearLocal() {
  unread.items.value = []
}

defineExpose({ reload, clearLocal })
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
