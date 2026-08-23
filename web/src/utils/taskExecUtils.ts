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

export interface TerminateExecutionOptions {
  taskId: number
  executionId: string
  /** Stop the live preview stream (the execution is no longer running) */
  onStopPreview?: () => void
  /** Refresh the execution detail after a successful cancel */
  onRefresh: () => void
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

/**
 * Cancel a running execution and coordinate the post-cancel UI updates.
 *
 * Wraps `cancelExecution` and guarantees the detail refresh runs exactly once
 * on the success path (via cancelExecution's onSuccess). Callers must NOT also
 * refresh off the returned boolean — doing so would trigger a redundant second
 * refresh (the success path fires onSuccess and returns true).
 *
 * @returns true if the execution was successfully cancelled, false otherwise.
 */
export async function terminateExecution(options: TerminateExecutionOptions): Promise<boolean> {
  const { taskId, executionId, onStopPreview, onRefresh } = options
  return cancelExecution({
    taskId,
    executionId,
    onSuccess: () => {
      onStopPreview?.()
      onRefresh()
    },
  })
}
