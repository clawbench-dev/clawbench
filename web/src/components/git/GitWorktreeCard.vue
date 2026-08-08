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
  gap: 8px;
  min-height: 44px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: background 0.15s;
}

@media (hover: hover) {
  .git-worktree-row:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.git-worktree-row.current {
  background: var(--bg-accent-subtle, rgba(74, 144, 217, 0.08));
  cursor: default;
}

.git-worktree-row.current .wt-row-name {
  color: var(--accent-color, #4a90d9);
  font-weight: bold;
}

[data-app-mode] .git-worktree-row.current .wt-row-name {
  text-shadow: 0 0 1px currentColor;
}

.git-worktree-row.missing {
  opacity: 0.6;
}

.git-worktree-row.locked {
  opacity: 0.8;
}

.wt-row-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.wt-row-name {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
}

.wt-row-icon {
  flex-shrink: 0;
  color: var(--accent-color, #4a90d9);
}

.wt-row-path {
  font-size: 11px;
  color: var(--text-muted, #999);
  word-break: break-all;
  line-height: 1.4;
  padding-left: 19px; /* align with name text after icon */
}

.wt-row-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  flex-shrink: 0;
}

.wt-row-actions {
  display: flex;
  align-items: center;
  gap: 4px;
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
  border-radius: 6px;
  transition: background 0.15s, color 0.15s;
}

@media (hover: hover) {
  .wt-action-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-secondary, #e9ecef);
  }
  .wt-action-btn:hover.wt-action-delete {
    color: var(--danger-color, #dc3545);
    background: var(--danger-bg, rgba(220, 53, 69, 0.1));
  }
}

.wt-action-btn:active {
  background: var(--bg-tertiary, #e9ecef);
}

.wt-badge {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 4px;
  white-space: nowrap;
}

.wt-badge-dirty {
  background: var(--warning-bg, rgba(255, 159, 64, 0.15));
  color: var(--warning-color, #e67e22);
}

.wt-badge-clean {
  background: var(--success-bg, rgba(40, 167, 69, 0.12));
  color: var(--success-color, #28a745);
}

.wt-badge-locked {
  background: var(--bg-secondary, #e9ecef);
  color: var(--text-muted, #999);
}

.wt-badge-missing {
  background: var(--danger-bg, rgba(220, 53, 69, 0.12));
  color: var(--danger-color, #dc3545);
}
</style>
