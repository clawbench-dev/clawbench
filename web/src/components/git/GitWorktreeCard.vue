<template>
  <div
    class="git-worktree-row"
    :class="{ current: worktree.isCurrent, locked: worktree.locked, missing: worktree.missing }"
    @click="!worktree.isCurrent && !worktree.missing && $emit('switch', worktree)"
  >
    <div class="wt-row-main">
      <div class="wt-row-name">
        <FolderTree :size="14" class="wt-row-icon" />
        <span>{{ worktree.branch || '—' }}</span>
        <span v-if="worktree.isMain" class="wt-badge wt-badge-main">{{ t('git.manage.main') }}</span>
      </div>
      <div class="wt-row-path">{{ worktree.path }}</div>
    </div>
    <div class="wt-row-badges">
      <span v-if="worktree.dirty" class="wt-badge wt-badge-dirty">{{ t('git.manage.dirty', { count: worktree.changeCount || worktree.untrackedCount }) }}</span>
      <span v-else class="wt-badge wt-badge-clean">{{ t('git.manage.clean') }}</span>
      <span v-if="worktree.locked" class="wt-badge wt-badge-locked">{{ t('git.manage.locked') }}</span>
      <span v-if="worktree.missing" class="wt-badge wt-badge-missing">{{ t('git.manage.pathMissing') }}</span>
    </div>
    <div class="wt-row-actions">
      <button
        v-if="!worktree.isCurrent"
        class="wt-action-btn wt-action-delete"
        :title="t('git.manage.deleteWorktree')"
        @click.stop="$emit('delete', worktree)"
      >
        <Trash2 :size="15" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { FolderTree, Trash2 } from 'lucide-vue-next'

const { t } = useI18n()

defineProps({
  worktree: { type: Object, required: true },
})

defineEmits(['switch', 'delete'])
</script>

<style scoped>
.git-worktree-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-4);
  min-height: 44px;
  padding: var(--space-5) var(--space-6);
  border-bottom: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .git-worktree-row:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.git-worktree-row.current {
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
  cursor: default;
}

.git-worktree-row.current .wt-row-name {
  color: var(--accent-color, #4a90d9);
  font-weight: var(--font-weight-bold);
}

[data-app-mode] .git-worktree-row.current .wt-row-name {
  text-shadow: 0 0 1px currentColor;
}

.git-worktree-row.missing {
  opacity: var(--opacity-muted);
}

.git-worktree-row.locked {
  opacity: var(--opacity-hover);
}

.wt-row-main {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.wt-row-name {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
}

.wt-row-icon {
  flex-shrink: 0;
  color: var(--accent-color, #4a90d9);
}

.wt-row-path {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  word-break: break-all;
  line-height: var(--line-height-snug);
  padding-left: 19px; /* align with name text after icon */
}

.wt-row-badges {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  flex-shrink: 0;
}

.wt-row-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}

.wt-action-btn {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-sm);
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .wt-action-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-secondary, #e9ecef);
  }
  .wt-action-btn:hover.wt-action-delete {
    color: var(--color-red);
    background: color-mix(in srgb, var(--color-red) 10%, transparent);
  }
}

.wt-action-btn:active {
  background: var(--bg-tertiary, #e9ecef);
}

.wt-badge {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  padding:1px var(--space-3);
  border-radius: var(--radius-xs);
  white-space: nowrap;
}

.wt-badge-dirty {
  background: color-mix(in srgb, var(--color-orange) 15%, transparent);
  color: var(--color-orange);
}

.wt-badge-main {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  color: var(--accent-color, #4a90d9);
}

.wt-badge-clean {
  background: color-mix(in srgb, var(--color-green) 12%, transparent);
  color: var(--color-green);
}

.wt-badge-locked {
  background: var(--bg-secondary, #e9ecef);
  color: var(--text-muted, #999);
}

.wt-badge-missing {
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  color: var(--color-red);
}
</style>
