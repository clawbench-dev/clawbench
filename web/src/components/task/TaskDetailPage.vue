<template>
  <div class="task-detail-page">
    <!-- Compact header: breadcrumb + refresh button -->
    <div class="detail-header">
      <TaskBreadcrumb />
      <button class="header-btn refresh-btn" :class="{ spinning: refreshing }" :disabled="refreshing" @click="onRefresh" :title="t('common.refresh')">
        <RefreshCw :size="14" />
      </button>
    </div>
    <!-- Settings content -->
    <div class="detail-content">
      <TaskOverviewTab :task="task" @deleted="$emit('deleted')" @edit="$emit('edit')" @history="$emit('history')" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import TaskOverviewTab from '@/components/task/TaskOverviewTab.vue'
import { useTaskTab } from '@/composables/useTaskTab'

const { t } = useI18n()
const { loadTasks } = useTaskTab()

defineProps<{
  task: Record<string, unknown>
}>()

defineEmits<{
  edit: []
  deleted: []
  history: []
}>()

const refreshing = ref(false)

async function onRefresh() {
  refreshing.value = true
  try {
    await loadTasks()
  } finally {
    refreshing.value = false
  }
}
</script>

<style scoped>
.task-detail-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.detail-header {
  display: flex;
  align-items: center;
  padding: 4px 8px;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  gap: 6px;
}

.header-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-secondary, #666);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all 0.2s ease;
}

.header-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .header-btn:hover:not(:disabled) {
    background: var(--bg-tertiary, #eef1f4);
    color: var(--accent-color, #0066cc);
  }
}

.header-btn:active:not(:disabled) {
  transform: scale(0.9);
}

.header-btn.spinning svg {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  100% { transform: rotate(360deg); }
}

.detail-content {
  flex: 1;
  overflow-y: hidden;
  display: flex;
  flex-direction: column;
}
</style>
