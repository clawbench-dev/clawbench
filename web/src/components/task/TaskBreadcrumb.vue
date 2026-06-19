<template>
  <div class="task-breadcrumb">
    <!-- Root crumb: 任务列表 -->
    <span
      class="crumb"
      :class="{ current: isList, clickable: !isList, first: true, alt: !isList }"
      @click="!isList && navigate('list')"
    >{{ t('task.title') }}</span>

    <!-- Task name crumb -->
    <span
      v-if="taskName"
      class="crumb"
      :class="{ current: isSettings, clickable: !isSettings, alt: !isSettings && isList }"
      @click="!isSettings && navigate('settings')"
    >{{ taskName }}</span>

    <!-- History crumb -->
    <span
      v-if="showHistoryCrumb"
      class="crumb"
      :class="{ current: isHistory, clickable: !isHistory, alt: !isHistory && !isSettings }"
      @click="!isHistory && navigate('history')"
    >{{ t('task.exec.title') }}</span>

    <!-- Exec detail crumb -->
    <span
      v-if="execDetailOpen"
      class="crumb current"
    >{{ t('task.exec.detail') }}</span>

    <!-- Form crumb -->
    <span
      v-if="formViewOpen"
      class="crumb current"
    >{{ formMode === 'create' ? t('task.form.createTitle') : t('task.form.editTitle') }}</span>
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
}

.task-breadcrumb::-webkit-scrollbar {
  display: none;
}

/* ── Chevron crumb base ── */
.crumb {
  position: relative;
  display: flex;
  align-items: center;
  height: 20px;
  padding: 0 12px 0 14px;
  font-size: 11px;
  font-weight: 500;
  white-space: nowrap;
  cursor: default;
  color: var(--text-secondary, #666);
  background: var(--bg-tertiary, #e9ecef);
  transition: background 0.15s, color 0.15s;
}

/* First crumb: flat left, no rounding */
.crumb.first {
  padding-left: 8px;
}


/* Right arrow — same color as crumb body, overlaps 1px to prevent gap */
.crumb::after {
  content: '';
  position: absolute;
  right: -7px;
  top: 0;
  width: 0;
  height: 0;
  border-style: solid;
  border-width: 10px 0 10px 7px;
  border-color: transparent transparent transparent var(--bg-tertiary, #e9ecef);
  transition: border-color 0.15s;
  z-index: 1;
}

/* Alternate shade for non-current crumbs — slightly darker than base */
.crumb.alt {
  background: color-mix(in srgb, var(--bg-tertiary, #e9ecef) 88%, #000);
}

.crumb.alt::after {
  border-left-color: color-mix(in srgb, var(--bg-tertiary, #e9ecef) 88%, #000);
}

/* ── Clickable crumb ── */
.crumb.clickable {
  cursor: pointer;
}

@media (hover: hover) {
  .crumb.clickable:hover {
    background: color-mix(in srgb, var(--bg-tertiary, #e9ecef) 80%, var(--accent-color, #4a90d9));
    color: var(--accent-color, #4a90d9);
  }

  .crumb.clickable:hover::after {
    border-left-color: color-mix(in srgb, var(--bg-tertiary, #e9ecef) 80%, var(--accent-color, #4a90d9));
  }
}

.crumb.clickable:active {
  background: color-mix(in srgb, var(--bg-tertiary, #e9ecef) 82%, #000);
}

.crumb.clickable:active::after {
  border-left-color: color-mix(in srgb, var(--bg-tertiary, #e9ecef) 82%, #000);
}

/* ── Current (active) crumb — accent color darkened ── */
.crumb.current {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 75%, #000);
  color: #fff;
  font-weight: 600;
}

.crumb.current::after {
  border-left-color: color-mix(in srgb, var(--accent-color, #0066cc) 75%, #000);
}


@media (hover: hover) {
  .crumb.current:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 75%, #000);
    color: #fff;
  }

  .crumb.current:hover::after {
    border-left-color: color-mix(in srgb, var(--accent-color, #0066cc) 75%, #000);
  }
}
</style>
