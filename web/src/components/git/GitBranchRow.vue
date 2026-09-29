<template>
  <div
    class="git-branch-row"
    :class="{ current: branch.isCurrent, switching }"
    @click="handleClick"
  >
    <div class="branch-main">
      <GitBranch :size="14" class="branch-icon" />
      <span class="branch-name">{{ branch.name }}</span>
    </div>
    <div class="branch-right">
      <span v-if="branch.isDefault" class="branch-default-badge">{{ t('git.manage.default') }}</span>
      <span v-if="branch.ahead > 0" class="track-ahead">{{ t('git.manage.ahead') }}{{ branch.ahead }}</span>
      <span v-if="branch.behind > 0" class="track-behind">{{ t('git.manage.behind') }}{{ branch.behind }}</span>
    </div>
    <div v-if="switching" class="branch-spinner">
      <LoadingIndicator size="sm" inline />
    </div>
    <button
      class="branch-action-btn"
      :class="{ 'is-disabled': deleteDisabled }"
      :disabled="deleteDisabled"
      :title="deleteDisabledReason"
      :aria-label="deleteDisabledReason"
      @click.stop="handleDelete"
    >
      <Trash2 :size="15" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { GitBranch, Trash2 } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

const { t } = useI18n()

const props = defineProps({
  branch: { type: Object, required: true },
  disabled: { type: Boolean, default: false },
})

const emit = defineEmits(['switch', 'delete'])

const switching = ref(false)

/**
 * The current branch and the repository's default branch cannot be deleted
 * (the backend answers `cannot_delete_current` / `cannot_delete_default`).
 * The button stays visible but disabled, so the action is discoverable and its
 * unavailability is explained — hiding it entirely made the row look like it
 * simply had no delete action.
 */
const deleteDisabled = computed(() => props.branch.isCurrent || props.branch.isDefault)

const deleteDisabledReason = computed(() => {
  if (props.branch.isCurrent) return t('git.manage.cannotDeleteCurrent')
  if (props.branch.isDefault) return t('git.manage.cannotDeleteDefault')
  return t('git.manage.deleteBranch')
})

/**
 * Guard the emit itself, not just the `disabled` attribute: a programmatic
 * `dispatchEvent('click')` still reaches the handler on a disabled button (the
 * browser only suppresses *user* clicks), and the backend would answer with an
 * error toast.
 */
function handleDelete() {
  if (deleteDisabled.value) return
  emit('delete', props.branch)
}

function handleClick() {
  if (props.branch.isCurrent || props.disabled || switching.value) return
  switching.value = true
  emit('switch', props.branch)
  setTimeout(() => { switching.value = false }, 5000)
}
</script>

<style scoped>
.git-branch-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-4);
  min-height: 44px;
  padding: var(--space-5) var(--space-6);
  border-bottom: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: background var(--duration-base);
}

@media (hover: hover) {
  .git-branch-row:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.git-branch-row.current {
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
  cursor: default;
}

.git-branch-row.current .branch-name {
  color: var(--accent-color, #4a90d9);
  font-weight: var(--font-weight-bold);
}

[data-app-mode] .git-branch-row.current .branch-name {
  text-shadow: 0 0 1px currentColor;
}

.git-branch-row.switching {
  opacity: var(--opacity-soft);
  pointer-events: none;
}

.branch-main {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
}

.branch-icon {
  color: var(--accent-color, #4a90d9);
  flex-shrink: 0;
}

.branch-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1a1a1a);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.branch-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  margin-left: var(--space-4);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
}

.branch-default-badge {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  background: var(--accent-color, #4a90d9);
  color: #fff;
  padding: 1px 5px;
  border-radius: var(--radius-xs);
  flex-shrink: 0;
}

.track-ahead {
  color: var(--color-green);
}

.track-behind {
  color: var(--color-orange);
}

.branch-spinner {
  margin-left: var(--space-3);
  flex-shrink: 0;
  display: flex;
  align-items: center;
}

.branch-action-btn {
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
  .branch-action-btn:hover:not(:disabled) {
    color: var(--color-red);
    background: color-mix(in srgb, var(--color-red) 10%, transparent);
  }
}

.branch-action-btn:active:not(:disabled) {
  background: color-mix(in srgb, var(--color-red) 15%, transparent);
}

/* Non-deletable branch (current / default): keep the icon visible as an
   affordance but make its unavailability obvious. The native `:disabled` still
   fires the button's `title` tooltip (verified in Chromium), so the reason
   stays discoverable. */
.branch-action-btn.is-disabled,
.branch-action-btn:disabled {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
  color: var(--text-muted, #999);
}

</style>
