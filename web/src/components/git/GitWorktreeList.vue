<template>
  <div class="git-worktree-list" :class="{ collapsed, 'no-header': hideHeader }">
    <div v-if="!hideHeader" class="section-header" @click="toggleCollapse">
      <div class="section-left">
        <span class="section-title">{{ t('git.manage.worktrees') }}</span>
        <span v-if="worktrees.length > 0" class="section-count count-badge">{{ worktrees.length }}</span>
      </div>
      <ChevronDown v-if="!collapsed" :size="16" class="section-chevron" />
      <ChevronRight v-else :size="16" class="section-chevron" />
    </div>
    <div v-if="hideHeader || !collapsed" class="section-body">
      <div v-if="loading" class="section-skeleton">
        <PanelSkeleton variant="list" :rows="3" :meta="true" :label="t('common.loading')" />
      </div>
      <div v-else-if="error" class="section-error">
        <span>{{ t('git.manage.loadError') }}</span>
        <button class="retry-btn" @click="$emit('retry')">{{ t('git.manage.retry') }}</button>
      </div>
      <div v-else-if="worktrees.length === 0" class="section-empty">{{ t('git.manage.noWorktrees') }}</div>
      <div v-else class="wt-list-body">
        <GitWorktreeCard
          v-for="wt in worktrees"
          :key="wt.path"
          :worktree="wt"
          @switch="$emit('switch-worktree', $event)"
          @delete="$emit('delete-worktree', $event)"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import GitWorktreeCard from './GitWorktreeCard.vue'
import { layoutEpoch, readPanelState, writePanelState } from '@/utils/panelState'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  worktrees: Array<Record<string, unknown> & { path: string }>
  loading?: boolean
  error?: boolean
  initialCollapsed?: boolean
  hideHeader?: boolean
}>(), {
  worktrees: () => [],
  loading: false,
  error: false,
  initialCollapsed: false,
  hideHeader: false,
})

defineEmits(['switch-worktree', 'delete-worktree', 'retry'])

const collapsed = ref(false)

/** `null` = the user has never chosen, so the component's own default applies. */
function loadCollapsed() {
  const stored = readPanelState<boolean | null>('gitWorktreeCollapsed')
  collapsed.value = stored === null ? props.initialCollapsed : stored
}

onMounted(loadCollapsed)
// Layout-memory scope / project root changed → the stored value now comes from
// a different key.
watch(layoutEpoch, loadCollapsed)

function toggleCollapse() {
  collapsed.value = !collapsed.value
  writePanelState('gitWorktreeCollapsed', collapsed.value)
}
</script>

<style scoped>
.git-worktree-list {
  flex: 1;
  min-height: 0;
  border-bottom: 1px solid var(--border-color, #dee2e6);
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

.git-worktree-list.no-header {
  border-bottom: none;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-5) var(--space-6);
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .section-header:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.section-left {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.section-title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
}

.section-count {
  font-weight: var(--font-weight-bold);
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #999);
}

.section-chevron {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.section-skeleton {
  /* 12dp is the row card's inset (GitWorktreeCard). */
  --panel-skeleton-inset: calc(12 * var(--dp));
  padding: var(--space-2) 0;
}

.section-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-4) 0;
  font-size: var(--font-size-md);
  color: var(--color-red);
}

.retry-btn {
  font-size: var(--font-size-sm);
  padding: calc(3 * var(--dp)) var(--space-5);
  border: 1px solid var(--accent-color, #4a90d9);
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
}

.section-empty {
  font-size: var(--font-size-md);
  color: var(--text-muted, #999);
  padding: var(--space-4) 0;
}

.wt-list-body {
  display: flex;
  flex-direction: column;
}

</style>
