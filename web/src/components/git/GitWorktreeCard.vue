<template>
  <div
    class="git-worktree-row"
    :class="{ current: worktree.isCurrent, locked: worktree.locked, missing: worktree.missing }"
    @click="handleRowClick"
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
        class="wt-action-btn wt-action-delete"
        :class="{ 'is-disabled': deleteDisabled }"
        :disabled="deleteDisabled"
        :title="deleteDisabledReason"
        :aria-label="deleteDisabledReason"
        @click.stop="handleDelete"
      >
        <Trash2 :size="15" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { FolderTree, Trash2 } from 'lucide-vue-next'
import { hasActiveTextSelection } from '@/utils/textSelection'

const { t } = useI18n()

const props = defineProps({
  worktree: { type: Object, required: true },
})

const emit = defineEmits(['switch', 'delete'])

/**
 * A drag-select inside the row ends with a click on the row (mousedown and
 * mouseup share it as common ancestor); that must not be treated as a switch.
 */
function handleRowClick() {
  if (hasActiveTextSelection()) return
  if (props.worktree.isCurrent || props.worktree.missing) return
  emit('switch', props.worktree)
}

/**
 * Worktrees that git refuses to remove, verified against real repositories:
 *  - `isCurrent`: the backend answers `cannot_delete_current`.
 *  - `isMain`: git fails with "is a main working tree" — and `--force` does not
 *    help, so there is no path to deleting it.
 *  - `locked`: git requires `remove -f -f`; the backend only ever sends a single
 *    `-f`, so the request can never succeed.
 *
 * Dirty and missing worktrees stay deletable: a missing one removes cleanly, and
 * a dirty one goes through the existing force-confirmation flow.
 */
const deleteDisabled = computed(
  () => !!(props.worktree.isCurrent || props.worktree.isMain || props.worktree.locked),
)

const deleteDisabledReason = computed(() => {
  if (props.worktree.isCurrent) return t('git.manage.cannotDeleteCurrentWorktree')
  if (props.worktree.isMain) return t('git.manage.cannotDeleteMainWorktree')
  if (props.worktree.locked) return t('git.manage.cannotDeleteLockedWorktree')
  return t('git.manage.deleteWorktree')
})

/**
 * Guard the emit itself, not just the `disabled` attribute: a programmatic
 * `dispatchEvent('click')` still reaches the handler on a disabled button (the
 * browser only suppresses *user* clicks), and the backend would answer with an
 * error toast.
 */
function handleDelete() {
  if (deleteDisabled.value) return
  emit('delete', props.worktree)
}
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
  .wt-action-btn:hover:not(:disabled) {
    color: var(--accent-color, #4a90d9);
    background: var(--bg-secondary, #e9ecef);
  }
  .wt-action-btn:hover.wt-action-delete:not(:disabled) {
    color: var(--color-red);
    background: color-mix(in srgb, var(--color-red) 10%, transparent);
  }
}

.wt-action-btn:active:not(:disabled) {
  background: var(--bg-tertiary, #e9ecef);
}

/* Non-removable worktree (current / main / locked): keep the icon visible as an
   affordance but make its unavailability obvious. The native `:disabled` still
   fires the button's `title` tooltip (verified in Chromium), so the reason
   stays discoverable. */
.wt-action-delete.is-disabled,
.wt-action-delete:disabled {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
  color: var(--text-muted, #999);
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
