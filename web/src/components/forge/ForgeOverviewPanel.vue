<template>
  <div class="forge-panel">
    <!-- No project selected: same question-card layout as the other forge views. -->
    <div v-if="!projectPath" class="forge-state">
      <div class="forge-card">
        <div class="forge-card-icon">
          <Inbox :size="30" />
        </div>
        <div class="forge-card-header">{{ t('forge.empty.noProjectHeader') }}</div>
        <div class="forge-card-body">{{ t('forge.empty.noProjectBody') }}</div>
        <div class="forge-card-options">
          <button class="fbtn fbtn-primary" @click="emit('request-project')">
            {{ t('forge.empty.chooseProject') }}
          </button>
        </div>
      </div>
    </div>

    <template v-else>
      <div class="forge-toolbar">
        <span class="forge-toolbar-title">{{ t('forge.overview.title') }}</span>
        <span class="forge-overview-count" v-if="unread.items.value.length > 0">
          {{ unread.items.value.length }}
        </span>
        <button
          class="forge-header-btn clear-unread-btn"
          :class="{ active: unread.items.value.length > 0 }"
          :disabled="unread.items.value.length === 0"
          :title="t('forge.overview.markAllRead')"
          :aria-label="t('forge.overview.markAllRead')"
          @click="onMarkAllRead"
        >
          <CheckCheck :size="14" />
        </button>
        <RefreshButton
          class="forge-header-btn"
          :loading="unread.loading.value"
          :title="t('nav.refresh')"
          @click="onRefreshClick"
        />
      </div>

      <div v-if="unread.error.value" class="forge-error-card">
        <AlertCircle :size="18" class="forge-error-icon" />
        <div class="forge-error-text">
          <div class="forge-error-title">{{ errorTitle(unread.error.value.code) }}</div>
          <div class="forge-error-body">{{ unread.error.value.message }}</div>
        </div>
        <button class="fbtn" @click="onRefreshClick">{{ t('forge.retry') }}</button>
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
          :class="{ unread: !row.read }"
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
  </div>
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, CheckCheck, ChevronRight, Inbox } from 'lucide-vue-next'
import RefreshButton from '@/components/common/RefreshButton.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useForgeUnreadItems } from '@/composables/useForge'
import { forgeOverviewLabel } from '@/utils/forgeEventLabels'
import type { ForgeUnreadItem } from '@/utils/forgeApi'

const props = defineProps<{
  active: boolean
  projectPath: string
}>()

const emit = defineEmits<{
  (e: 'request-project'): void
  (e: 'open-item', payload: { type: ForgeUnreadItem['type']; number: number; runId: number; itemKey: string }): void
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

/** Reload on demand. The badge re-derives from the server, not from a decrement. */
function onRefreshClick() {
  void unread.load()
}

function label(row: ForgeUnreadItem): string {
  return forgeOverviewLabel(row)
}

/** Status dot: a pipeline shows its run outcome, an item its state. */
function dotClass(row: ForgeUnreadItem): string {
  return row.type === 'pipeline' ? 'pipeline-unknown' : 'open'
}

async function onMarkAllRead() {
  await unread.markAllRead()
}

function onRowClick(row: ForgeUnreadItem) {
  // Mark read locally so the dot clears at once, then open. The row stays in the
  // list (greyed out) rather than being spliced — removing it would shift every
  // row below the cursor as the user clicks. It disappears on the next load.
  unread.markRowRead(row.itemKey)
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
</script>

<style scoped>
.forge-overview-row.read {
  opacity: 0.55;
}

.forge-overview-count {
  margin-left: var(--space-2);
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary, #6b7280);
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
