<template>
  <div class="task-tab" v-show="active">
    <TaskListPage v-if="currentView === 'list' && !formViewOpen" ref="listPageRef" @create="onCreate" @select="onTaskSelect" />
    <TaskDetailPage v-else-if="currentView === 'settings' && !execDetailOpen && !formViewOpen && selectedTaskData" :task="selectedTaskData" @edit="onEdit" @deleted="onTaskDeleted" />
    <!-- Fallback for the combination the chain leaves uncovered: the settings
         view was requested for a task that is no longer in the list. The card /
         notification / deep link that led here can outlive the task (it was
         deleted after the entry point was rendered), and every branch below is
         gated on something derived from that task, so without this branch the
         panel renders as an empty shell with no way out.
         Ordered BEFORE the exec-detail branch on purpose: a run's output is
         anchored to a task that no longer exists, so showing the deleted
         notice is more truthful than leaving stale output on screen.
         Gated on tasksLoaded so a valid task does not flash this before the
         first list fetch lands. -->
    <div v-else-if="taskMissing" class="task-missing">
      <CalendarX class="missing-icon" :size="32" />
      <span class="missing-title">{{ t('task.notFound') }}</span>
      <span class="missing-hint">{{ t('task.notFoundHint') }}</span>
      <button class="fbtn fbtn-primary" @click="navigateToList">
        <ArrowLeft :size="14" />
        <span class="action-text">{{ t('task.backToList') }}</span>
      </button>
    </div>
    <TaskExecDetail v-else-if="execDetailOpen && !formViewOpen" :execDetail="selectedExecData" :taskName="selectedTaskData?.name" :taskId="selectedTaskId" @close="closeExecDetail" @open-file="onOpenFile" />
    <TaskFormPage v-else-if="formViewOpen" :mode="formMode" :task="(formMode === 'edit' ? selectedTaskData : null) as Record<string, unknown> | null" @close="closeForm" @saved="onFormSaved" />
    <!-- Last resort: the settings view was requested but the list has not been
         fetched yet, so the task's existence is still unknown. Showing the
         fallback here would be a lie; show a loader instead. -->
    <div v-else class="task-pending">
      <LoadingIndicator size="sm" :label="t('common.loading')" />
    </div>
    <!-- Advisory hint shown before the manual create form. Teleports to <body>,
         so its position here is for readability only. -->
    <TaskCreateHintDialog
      :open="createHintOpen"
      @close="closeCreateHint"
      @manual="onManualCreate"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, CalendarX } from 'lucide-vue-next'
import TaskListPage from '@/components/task/TaskListPage.vue'
import TaskDetailPage from '@/components/task/TaskDetailPage.vue'
import TaskExecDetail from '@/components/task/TaskExecDetail.vue'
import TaskFormPage from '@/components/task/TaskFormPage.vue'
import TaskCreateHintDialog from '@/components/task/TaskCreateHintDialog.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useTaskTab } from '@/composables/useTaskTab'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { isTaskCreateHintDismissed } from '@/composables/useTaskCreateHint'
import { store } from '@/stores/app'
import '@/assets/modal-footer-btn.css'

const { t } = useI18n()

const props = defineProps<{
  active: boolean
}>()

const emit = defineEmits<{
  'open-file': [filePath: string, lineStart?: number]
}>()

const { currentView, selectedTaskId, selectedExecData, execDetailOpen, formViewOpen, formMode, tasksLoaded, goBack, navigateToTaskSettings, navigateToList, closeExecDetail, openCreateForm, openEditForm, closeForm, loadTasks } = useTaskTab()

// Register back handler for task drill-down navigation
// canGoBack checks: only when this tab is active AND has a drill-down view
useFeatureBackHandler(
  'tasks',
  () => props.active && (currentView.value !== 'list' || execDetailOpen.value || formViewOpen.value),
  () => goBack(),
  PRIORITY_PAGE,
)

// Read from store directly — NOT from listPageRef (Vue refs don't expose internal computed)
const selectedTaskData = computed(() =>
  (store.state.tasks || []).find((t: Record<string, unknown>) => t.id === selectedTaskId.value) || null
)

// A task id that the loaded list does not contain means the task was deleted
// after the entry point that linked here was rendered. Requiring tasksLoaded
// keeps the first paint of a valid task from being reported as missing.
const taskMissing = computed(() =>
  currentView.value === 'settings' && !selectedTaskData.value && tasksLoaded.value
)

const listPageRef = ref<InstanceType<typeof TaskListPage> | null>(null)

// The "+" button leads to the manual form, but AI-managed tasks are the
// smoother route, so an advisory hint is shown first. It is skipped entirely
// once dismissed (localStorage), in which case "+" opens the form directly.
const createHintOpen = ref(false)

function onCreate() {
  if (isTaskCreateHintDismissed()) {
    openCreateForm()
    return
  }
  createHintOpen.value = true
}

function closeCreateHint() {
  createHintOpen.value = false
}

function onManualCreate() {
  createHintOpen.value = false
  openCreateForm()
}

function onEdit() {
  openEditForm()
}

async function onFormSaved(newTaskId: number) {
  await loadTasks()
  closeForm()
  if (formMode.value === 'create' && newTaskId) {
    navigateToTaskSettings(newTaskId)
  }
  listPageRef.value?.refresh?.()
}

function onTaskDeleted() {
  navigateToList()
  loadTasks()
}

function onOpenFile(filePath: string, lineStart?: number) {
  emit('open-file', filePath, lineStart)
}

function onTaskSelect(taskId: number) {
  navigateToTaskSettings(taskId)
}
</script>

<style scoped>
.task-tab {
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* Fallback shown when the requested task is gone. Mirrors the list page's own
   empty state (same icon size, muted text, centred block) so a stale link
   lands on something that reads as part of the task section, not a dead end. */
.task-missing,
.task-pending {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-6);
  height: 100%;
  color: var(--text-muted, #999);
  font-size: var(--font-size-lg);
}

.missing-icon {
  opacity: var(--opacity-muted);
}

.missing-title {
  font-size: var(--font-size-lg);
  color: var(--text-muted);
}

.missing-hint {
  font-size: var(--font-size-sm);
  color: var(--text-hint);
  text-align: center;
  max-width: 32em;
}
</style>
