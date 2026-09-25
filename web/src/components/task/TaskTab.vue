<template>
  <div class="task-tab" v-show="active">
    <TaskListPage v-if="currentView === 'list' && !formViewOpen" ref="listPageRef" @create="onCreate" @select="onTaskSelect" />
    <TaskDetailPage v-else-if="currentView === 'settings' && !execDetailOpen && !formViewOpen && selectedTaskData" :task="selectedTaskData" @edit="onEdit" @deleted="onTaskDeleted" />
    <TaskExecDetail v-else-if="execDetailOpen && !formViewOpen" :execDetail="selectedExecData" :taskName="selectedTaskData?.name" :taskId="selectedTaskId" @close="closeExecDetail" @open-file="onOpenFile" />
    <TaskFormPage v-else-if="formViewOpen" :mode="formMode" :task="(formMode === 'edit' ? selectedTaskData : null) as Record<string, unknown> | null" @close="closeForm" @saved="onFormSaved" />
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
import TaskListPage from '@/components/task/TaskListPage.vue'
import TaskDetailPage from '@/components/task/TaskDetailPage.vue'
import TaskExecDetail from '@/components/task/TaskExecDetail.vue'
import TaskFormPage from '@/components/task/TaskFormPage.vue'
import TaskCreateHintDialog from '@/components/task/TaskCreateHintDialog.vue'
import { useTaskTab } from '@/composables/useTaskTab'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { isTaskCreateHintDismissed } from '@/composables/useTaskCreateHint'
import { store } from '@/stores/app'

const props = defineProps<{
  active: boolean
}>()

const emit = defineEmits<{
  'open-file': [filePath: string, lineStart?: number]
}>()

const { currentView, selectedTaskId, selectedExecData, execDetailOpen, formViewOpen, formMode, goBack, navigateToTaskSettings, navigateToList, closeExecDetail, openCreateForm, openEditForm, closeForm, loadTasks } = useTaskTab()

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
</style>
