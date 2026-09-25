<template>
  <div class="task-breadcrumb" data-horizontal-scroll="true">
    <!-- Root crumb: 任务列表. The glyph is the section's own dock icon, so the
         panel header reads as the same section the dock button stands for
         (same rule as the git history and forge headers). -->
    <span
      class="crumb"
      :class="{ current: isList, clickable: !isList }"
      @click="!isList && navigate('list')"
    >
      <Clock :size="14" class="crumb-icon" />
      <span>{{ t('task.title') }}</span>
    </span>

    <template v-if="taskName">
      <span class="crumb-sep">›</span>

      <!-- Task name crumb -->
      <span
        class="crumb"
        :class="{ current: isSettings, clickable: !isSettings }"
        @click="!isSettings && navigate('settings')"
      >{{ taskName }}</span>
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
import { Clock } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { useTaskTab } from '@/composables/useTaskTab'
import { store } from '@/stores/app'

const { t } = useI18n()
const { currentView, selectedTaskId, execDetailOpen, formViewOpen, formMode, navigateToList, navigateToTaskSettings } = useTaskTab()

// Derive task name from store (same pattern as TaskTab)
const taskName = computed(() => {
  if (!selectedTaskId.value) return null
  return (store.state.tasks || []).find(t => t.id === selectedTaskId.value)?.name || null
})

// Derived state
const isList = computed(() => currentView.value === 'list' && !formViewOpen.value)
const isSettings = computed(() => currentView.value === 'settings' && !execDetailOpen.value && !formViewOpen.value)

// Centralized navigation
function navigate(target) {
  if (target === 'list') {
    navigateToList()
  } else if (target === 'settings') {
    const tid = selectedTaskId.value
    if (tid) navigateToTaskSettings(tid)
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
  font-size: var(--font-size-md);
  color: var(--text-muted, #6c757d);
}

.task-breadcrumb::-webkit-scrollbar {
  display: none;
}

/* ── Crumb item ── */
.crumb {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  padding:3px var(--space-3);
  border-radius: var(--radius-xs);
  white-space: nowrap;
  cursor: default;
  transition: background var(--duration-base), color var(--duration-base);
}

/* Leading glyph on the root crumb. Holds its size so a long task name in the
   next crumb cannot squeeze it (the crumbs share one scrolling flex row). */
.crumb-icon {
  flex-shrink: 0;
}

/* ── Clickable crumb ── */
.crumb.clickable {
  cursor: pointer;
  color: var(--text-secondary, #495057);
}

@media (hover: hover) {
  .crumb.clickable:hover {
    background: var(--bg-secondary, #f8f9fa);
    color: var(--accent-color, #4a90d9);
  }
}

.crumb.clickable:active {
  background: var(--bg-tertiary, #e9ecef);
}

/* ── Current (active) crumb ── */
.crumb.current {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #212529);
  cursor: default;
}

@media (hover: hover) {
  .crumb.current:hover {
    background: none;
    color: var(--text-primary, #212529);
  }
}

/* ── Separator ── */
.crumb-sep {
  color: var(--text-muted, #6c757d);
  font-size: var(--font-size-xs);
  margin: 0 1px;
  user-select: none;
}
</style>
