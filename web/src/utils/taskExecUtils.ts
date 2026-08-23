import { apiPut } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { gt } from '@/composables/useLocale'

interface CancelExecutionOptions {
  taskId: number
  executionId: string
  /** Optional callback invoked after a successful cancel (e.g. stop WS preview, refresh list) */
  onSuccess?: () => void
}

/**
 * Cancel a running task execution (shared by the history list and the exec
 * detail toolbar). Shows a confirmation dialog, calls the backend, and reports
 * the outcome via toast.
 *
 * The backend runningExecutions map is keyed by session ID, so callers should
 * pass the session ID of a running execution (fall back to the DB id if absent).
 *
 * @returns true if the execution was successfully cancelled, false otherwise
 *         (user dismissed the dialog, or the execution was already finished).
 */
export async function cancelExecution(options: CancelExecutionOptions): Promise<boolean> {
  const { taskId, executionId, onSuccess } = options
  const toast = useToast()
  const dialog = useDialog()

  if (!await dialog.confirm(gt('task.exec.confirmCancel'))) return false
  try {
    await apiPut(`/api/tasks/${taskId}`, {
      action: 'cancel',
      executionId,
    })
    toast.show(gt('task.exec.cancelled'), { icon: '✅', type: 'success' })
    onSuccess?.()
    return true
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err)
    if (msg.includes('404') || msg.includes('TaskExecutionNotFound') || msg.includes('execution not found')) {
      toast.show(gt('task.exec.alreadyFinished'), { icon: 'ℹ️', type: 'info' })
    } else {
      toast.show(gt('task.exec.actionFailedDetail', { error: msg }), { icon: '⚠️', type: 'error' })
    }
    return false
  }
}
