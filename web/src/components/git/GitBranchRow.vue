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
      v-if="!branch.isCurrent && !branch.isDefault"
      class="branch-action-btn"
      :title="t('git.manage.deleteBranch')"
      @click.stop="$emit('delete', branch)"
    >
      <Trash2 :size="15" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
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
  background: var(--bg-accent-subtle, rgba(74, 144, 217, 0.08));
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
  opacity: 0.7;
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
  color: var(--success-color, #28a745);
}

.track-behind {
  color: var(--warning-color, #e67e22);
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
  .branch-action-btn:hover {
    color: var(--danger-color, #dc3545);
    background: var(--danger-bg, rgba(220, 53, 69, 0.1));
  }
}

.branch-action-btn:active {
  background: var(--danger-bg, rgba(220, 53, 69, 0.15));
}

</style>
