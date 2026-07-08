import { ref, type Ref } from 'vue'
import { apiPut, apiDelete } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { useTaskTab } from '@/composables/useTaskTab.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { gt } from '@/composables/useLocale'

interface UseTaskOverviewOptions {
  task: Ref<Record<string, unknown>>
  emit: {
    deleted: () => void
    edit: () => void
    history: () => void
  }
}

export function useTaskOverview(options: UseTaskOverviewOptions) {
  const { task, emit } = options
  const toast = useToast()
  const { loadTasks } = useTaskTab()
  const dialog = useDialog()

  const actionLoading = ref(false)

  /**
   * Runs a task action (trigger/pause/resume) or deletes the task.
   * `method` is the HTTP verb: 'put' for trigger/pause/resume, 'delete' for delete.
   * `action` is passed as the body for PUT calls, or omitted for DELETE.
   */
  async function runAction(
    method: 'put' | 'delete',
    action?: string,
  ): Promise<void> {
    actionLoading.value = true
    try {
      const taskId = task.value.id
      if (method === 'delete') {
        await apiDelete(`/api/tasks/${taskId}`)
        await loadTasks()
        emit.deleted()
      } else {
        await apiPut(`/api/tasks/${taskId}`, { action })
        await loadTasks()
      }
    } catch (err: unknown) {
      const _msg = (err as { message?: string })?.message || ''
      toast.show(_msg ? gt('task.actionFailedDetail', { error: _msg }) : gt('task.actionFailed'), { icon: '⚠️', type: 'error' })
    } finally {
      actionLoading.value = false
    }
  }

  async function triggerTask(): Promise<void> {
    await runAction('put', 'trigger')
  }

  async function pauseTask(): Promise<void> {
    await runAction('put', 'pause')
  }

  async function resumeTask(): Promise<void> {
    await runAction('put', 'resume')
  }

  async function deleteTask(): Promise<void> {
    if (!await dialog.confirm(gt('task.confirmDelete'), { dangerous: true })) return
    await runAction('delete')
  }

  return { actionLoading, triggerTask, pauseTask, resumeTask, deleteTask }
}
