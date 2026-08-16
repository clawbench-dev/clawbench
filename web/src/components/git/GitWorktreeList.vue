<template>
  <div class="git-worktree-list" :class="{ collapsed, 'no-header': hideHeader }">
    <div v-if="!hideHeader" class="section-header" @click="toggleCollapse">
      <div class="section-left">
        <span class="section-title">{{ t('git.manage.worktrees') }}</span>
        <span v-if="worktrees.length > 0" class="section-count">{{ worktrees.length }}</span>
      </div>
      <ChevronDown v-if="!collapsed" :size="16" class="section-chevron" />
      <ChevronRight v-else :size="16" class="section-chevron" />
    </div>
    <div v-if="hideHeader || !collapsed" class="section-body">
      <div v-if="loading" class="section-loading">
        <LoadingIndicator size="sm" inline />
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
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import GitWorktreeCard from './GitWorktreeCard.vue'

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

const STORAGE_KEY = 'git-worktree-collapsed'
const collapsed = ref(false)

onMounted(() => {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored !== null) {
    collapsed.value = stored === 'true'
  } else {
    collapsed.value = props.initialCollapsed
  }
})

function toggleCollapse() {
  collapsed.value = !collapsed.value
  localStorage.setItem(STORAGE_KEY, String(collapsed.value))
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
  padding: 10px 12px;
  cursor: pointer;
  transition: background 0.15s;
}

@media (hover: hover) {
  .section-header:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.section-left {
  display: flex;
  align-items: center;
  gap: 6px;
}

.section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
}

.section-count {
  font-size: 10px;
  font-weight: 700;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #999);
  padding: 1px 6px;
  border-radius: 10px;
}

.section-chevron {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.section-body {
}

.section-loading {
  display: flex;
  justify-content: center;
  padding: 16px 0;
}

.section-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
  font-size: 13px;
  color: var(--danger-color, #dc3545);
}

.retry-btn {
  font-size: 12px;
  padding: 3px 10px;
  border: 1px solid var(--accent-color, #4a90d9);
  border-radius: 4px;
  background: transparent;
  color: var(--accent-color, #4a90d9);
  cursor: pointer;
}

.section-empty {
  font-size: 13px;
  color: var(--text-muted, #999);
  padding: 8px 0;
}

.wt-list-body {
  display: flex;
  flex-direction: column;
}

</style>
