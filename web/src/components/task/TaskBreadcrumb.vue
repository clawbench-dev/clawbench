<template>
  <div class="task-breadcrumb">
    <!-- Root crumb: 任务列表 -->
    <span
      class="crumb"
      :class="{ current: isList, clickable: !isList }"
      @click="!isList && navigate('list')"
    >{{ t('task.title') }}</span>

    <template v-if="taskName">
      <span class="crumb-sep">›</span>

      <!-- Task name crumb -->
      <span
        class="crumb"
        :class="{ current: isSettings, clickable: !isSettings }"
        @click="!isSettings && navigate('settings')"
      >{{ taskName }}</span>
    </template>

    <template v-if="showHistoryCrumb">
      <span class="crumb-sep">›</span>

      <!-- History crumb -->
      <span
        class="crumb"
        :class="{ current: isHistory, clickable: !isHistory }"
        @click="!isHistory && navigate('history')"
      >{{ t('task.exec.title') }}</span>
    </template>

    <template v-if="execDetailOpen">
      <span class="crumb-sep">›</span>

      <!-- Exec detail crumb -->
      <span class="crumb current">{{ t('task.exec.detail') }}</span>
    </template>

    <template v-if="formViewOpen">
      <span class="crumb-sep">›</span>

      <!-- Form crumb -->
      <span class="crumb current">{{ formMode === 'create' ? t('task.form.createTitle') : t('task.form.editTitle') }}</span>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTaskTab } from '@/composables/useTaskTab'
import { store } from '@/stores/app'

const { t } = useI18n()
const { currentView, selectedTaskId, execDetailOpen, formViewOpen, formMode, navigateToList, navigateToTaskSettings, navigateToTaskHistory } = useTaskTab()

// Derive task name from store (same pattern as TaskTab)
const taskName = computed(() => {
  if (!selectedTaskId.value) return null
  return (store.state.tasks || []).find(t => t.id === selectedTaskId.value)?.name || null
})

// Derived state
const isList = computed(() => currentView.value === 'list' && !formViewOpen.value)
const isSettings = computed(() => currentView.value === 'settings' && !execDetailOpen.value && !formViewOpen.value)
const isHistory = computed(() => currentView.value === 'history' && !execDetailOpen.value && !formViewOpen.value)

const showHistoryCrumb = computed(() => {
  if (formViewOpen.value) return false
  return currentView.value === 'history'
})

// Centralized navigation
function navigate(target) {
  if (target === 'list') {
    navigateToList()
  } else if (target === 'settings') {
    const tid = selectedTaskId.value
    if (tid) navigateToTaskSettings(tid)
  } else if (target === 'history') {
    const tid = selectedTaskId.value
    if (tid) navigateToTaskHistory(tid)
  }
}
</script>

<style scoped>
.task-breadcrumb {
  display: flex;
  align-items: center;
  overflow-x: auto;
  scrollbar-width: none;
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: var(--text-muted, #6c757d);
}

.task-breadcrumb::-webkit-scrollbar {
  display: none;
}

/* ── Crumb item ── */
.crumb {
  padding: 3px 6px;
  border-radius: 4px;
  white-space: nowrap;
  cursor: default;
  transition: background 0.15s, color 0.15s;
}

/* ── Clickable crumb ── */
.crumb.clickable {
  cursor: pointer;
  color: var(--text-secondary, #495057);
}

.crumb.clickable:hover {
  background: var(--bg-secondary, #f8f9fa);
  color: var(--accent-color, #4a90d9);
}

.crumb.clickable:active {
  background: var(--bg-tertiary, #e9ecef);
}

/* ── Current (active) crumb ── */
.crumb.current {
  font-weight: 600;
  color: var(--text-primary, #212529);
  cursor: default;
}

.crumb.current:hover {
  background: none;
  color: var(--text-primary, #212529);
}

/* ── Separator ── */
.crumb-sep {
  color: var(--text-muted, #6c757d);
  font-size: 11px;
  margin: 0 1px;
  user-select: none;
}
</style>
