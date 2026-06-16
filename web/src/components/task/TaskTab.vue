<template>
  <div class="task-tab" v-show="active">
    <TaskListPage v-if="currentView === 'list' && !formViewOpen" ref="listPageRef" @create="onCreate" @select="onTaskSelect" @history="onTaskHistoryFromList" />
    <TaskDetailPage v-else-if="currentView === 'settings' && !execDetailOpen && !formViewOpen" :task="selectedTaskData" @edit="onEdit" @deleted="onTaskDeleted" @history="onTaskHistory" />
    <TaskHistoryTab v-else-if="currentView === 'history' && !execDetailOpen && !formViewOpen" :task="selectedTaskData" @open-file="onOpenFile" />
    <TaskExecDetail v-else-if="execDetailOpen && !formViewOpen" :execDetail="selectedExecData" :taskName="selectedTaskData?.name" :taskId="selectedTaskId" @close="closeExecDetail" @open-file="onOpenFile" />
    <TaskFormPage v-else-if="formViewOpen" :mode="formMode" :task="formMode === 'edit' ? selectedTaskData : null" @close="closeForm" @saved="onFormSaved" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import TaskListPage from '@/components/task/TaskListPage.vue'
import TaskDetailPage from '@/components/task/TaskDetailPage.vue'
import TaskHistoryTab from '@/components/task/TaskHistoryTab.vue'
import TaskExecDetail from '@/components/task/TaskExecDetail.vue'
import TaskFormPage from '@/components/task/TaskFormPage.vue'
import { useTaskTab } from '@/composables/useTaskTab'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { store } from '@/stores/app'

const props = defineProps<{
  active: boolean
}>()

const emit = defineEmits<{
  'open-file': [filePath: string, lineStart?: number]
}>()

const { currentView, selectedTaskId, selectedExecData, execDetailOpen, formViewOpen, formMode, goBack, navigateToTaskSettings, navigateToTaskHistory, navigateToList, closeExecDetail, openCreateForm, openEditForm, closeForm, loadTasks } = useTaskTab()

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
  (store.state.tasks || []).find((t: any) => t.id === selectedTaskId.value) || null
)

const listPageRef = ref<InstanceType<typeof TaskListPage> | null>(null)

function onCreate() {
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
  listPageRef.value?.refresh?.()
}

function onOpenFile(filePath: string, lineStart?: number) {
  emit('open-file', filePath, lineStart)
}

function onTaskSelect(taskId: number) {
  navigateToTaskSettings(taskId)
}

function onTaskHistory() {
  navigateToTaskHistory(selectedTaskId.value!)
}

function onTaskHistoryFromList(taskId: number) {
  navigateToTaskHistory(taskId)
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
