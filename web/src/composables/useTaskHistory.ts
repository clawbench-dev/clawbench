import { ref, reactive, computed, type Ref } from 'vue'
import { apiGet, apiPut } from '@/utils/api'
import { useToast } from '@/composables/useToast.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { useTaskTab } from '@/composables/useTaskTab.ts'
import { useChatRender } from '@/composables/useChatRender.ts'
import { gt } from '@/composables/useLocale'
import { stripMarkdownPreview } from '@/utils/format.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'TaskHistory'

interface UseTaskHistoryOptions {
  task: Ref<any>
}

const PAGE_SIZE = 10

export function useTaskHistory(options: UseTaskHistoryOptions) {
  const { task } = options
  const toast = useToast()
  const dialog = useDialog()
  const { openExecDetail } = useTaskTab()

  const chatRender = useChatRender({ messages: ref([]), theme: ref('light'), currentSessionId: ref('') })

  const loading = ref(false)
  const loadingMore = ref(false)
  const hasMore = ref(false)
  const executions = ref<any[]>([])
  const runningExecutions = ref<any[]>([])

  // Unified list: running executions first (normalized to same shape), then completed
  // Filter out running records from DB list to avoid duplication with runningExecutions
  // (runningExecutions comes from in-memory map via GET /api/tasks/{id},
  //  executions comes from DB via GET /api/tasks/{id}/executions which also includes running records)
  const allExecutions = computed(() => {
    const running = runningExecutions.value.map(exec => ({
      ...exec,
      status: 'running',
      createdAt: exec.startedAt,
    }))
    const completed = executions.value.filter(exec => exec.status !== 'running')
    return [...running, ...completed]
  })

  function isRunning(exec: any): boolean {
    return exec.status === 'running'
  }

  // Track previous running count to detect completions
  let prevRunningCount = 0

  // Track just-completed execution IDs for entry animation
  const justCompletedIds = reactive(new Set<string>())

  // Timers for auto-clearing just-completed IDs
  const justCompletedTimers = new Map<string, ReturnType<typeof setTimeout>>()

  // ISS-015: Track locally-read execution IDs to prevent unread flash-back
  const locallyReadIds = reactive(new Set<number>())

  // ISS-016: AbortController for cancelling in-flight requests on task change/unmount
  let abortController = new AbortController()

  // Concurrency guard: prevent overlapping loadExecutions() calls
  let loadExecutionsInFlight = false

  function getSignal(): AbortSignal {
    return abortController.signal
  }

  /** Called when task ID changes — aborts in-flight requests and resets state */
  function onTaskChange(): void {
    abortController.abort()
    abortController = new AbortController()
    prevRunningCount = 0
    loadExecutionsInFlight = false
    // Clear just-completed tracking
    for (const timer of justCompletedTimers.values()) {
      clearTimeout(timer)
    }
    justCompletedTimers.clear()
    justCompletedIds.clear()
  }

  function isUnreadDisplay(exec: any): boolean {
    return exec.isUnread && !locallyReadIds.has(exec.id)
  }

  function isJustCompleted(exec: any): boolean {
    // Running executions use 'id' (session ID), completed use 'sessionId'
    const sessionId = exec.sessionId || exec.id
    return !!sessionId && justCompletedIds.has(sessionId)
  }

  function parseExecution(exec: any): any {
    const { blocks, metadata } = chatRender.parseAssistantContent(exec.content)
    const preview = extractPreview(exec)
    return { ...exec, blocks, metadata, preview }
  }

  /** Initial load: fetch the first page of executions */
  async function loadExecutions(): Promise<void> {
    if (!task.value?.id) return
    if (loadExecutionsInFlight) return
    loadExecutionsInFlight = true
    loading.value = true
    try {
      const data = await apiGet<{ executions: any[]; hasMore?: boolean }>(
        `/api/tasks/${task.value.id}/executions?limit=${PAGE_SIZE}`,
        { signal: abortController.signal },
      )
      const rawExecutions = data.executions || []
      executions.value = rawExecutions.map(parseExecution)
      hasMore.value = !!data.hasMore
    } catch (err: any) {
      // Don't report AbortError (expected when switching tasks)
      if (err?.name !== 'AbortError') {
        appLog.e(TAG, 'Failed to load executions:', err)
      }
    } finally {
      loading.value = false
      loadExecutionsInFlight = false
    }
  }

  /** Load the next page of executions (appended to existing list) */
  async function loadMoreExecutions(): Promise<void> {
    if (!task.value?.id || loadingMore.value || !hasMore.value) return
    loadingMore.value = true
    try {
      // Derive cursor from the last completed execution
      const completed = executions.value.filter(exec => exec.status !== 'running')
      const last = completed[completed.length - 1]
      if (!last) return
      const cursor = encodeURIComponent(last.createdAt)
      const cursorId = encodeURIComponent(last.id)
      const data = await apiGet<{ executions: any[]; hasMore?: boolean }>(
        `/api/tasks/${task.value.id}/executions?limit=${PAGE_SIZE}&cursor=${cursor}&cursor_id=${cursorId}`,
        { signal: abortController.signal },
      )
      const more = (data.executions || []).map(parseExecution)
      if (more.length > 0) {
        // Only append non-running completed executions (running come from in-memory map)
        executions.value = [...executions.value, ...more.filter(e => e.status !== 'running')]
      }
      hasMore.value = !!data.hasMore
    } catch (err: any) {
      if (err?.name !== 'AbortError') {
        appLog.e(TAG, 'Failed to load more executions:', err)
      }
    } finally {
      loadingMore.value = false
    }
  }

  /** Full reload: fetch all executions from scratch (used after delete/cancel) */
  async function reloadExecutions(): Promise<void> {
    if (!task.value?.id) return
    try {
      const data = await apiGet<{ executions: any[]; hasMore?: boolean }>(
        `/api/tasks/${task.value.id}/executions?limit=${PAGE_SIZE}`,
        { signal: abortController.signal },
      )
      const rawExecutions = data.executions || []
      executions.value = rawExecutions.map(parseExecution)
      hasMore.value = !!data.hasMore
    } catch (err: any) {
      if (err?.name !== 'AbortError') {
        appLog.e(TAG, 'Failed to reload executions:', err)
      }
    }
  }

  async function loadRunningStatus(): Promise<void> {
    if (!task.value?.id) return
    try {
      const data = await apiGet<{ runningExecutions: any[] }>(
        `/api/tasks/${task.value.id}`,
        { signal: abortController.signal },
      )
      const newRunning = data.runningExecutions || []
      const newCount = newRunning.length
      // When running count decreases, an execution just completed — refresh the completed list
      if (prevRunningCount > 0 && newCount < prevRunningCount) {
        // Mark previous running executions as just-completed for entry animation
        for (const exec of runningExecutions.value) {
          const execId = exec.id || exec.ID
          if (execId && !justCompletedIds.has(execId)) {
            justCompletedIds.add(execId)
            // Auto-clear after 3s
            const timer = setTimeout(() => {
              justCompletedIds.delete(execId)
              justCompletedTimers.delete(execId)
            }, 3000)
            justCompletedTimers.set(execId, timer)
          }
        }
        reloadExecutions()
      }
      prevRunningCount = newCount
      runningExecutions.value = newRunning
    } catch {
      // Silently ignore — polling will retry
    }
  }

  async function cancelExecution(execId: string): Promise<void> {
    if (!task.value?.id) return
    if (!await dialog.confirm(gt('task.exec.confirmCancel'))) return
    try {
      await apiPut(`/api/tasks/${task.value.id}`, {
        action: 'cancel',
        executionId: execId,
      })
      toast.show(gt('task.exec.cancelled'), { type: 'success' })
    } catch (err: any) {
      if (err?.message?.includes('404')) {
        toast.show(gt('task.exec.alreadyFinished'), { type: 'info' })
      }
    }
    await loadRunningStatus()
  }

  async function deleteExecution(execId: number): Promise<void> {
    if (!task.value?.id) return
    if (!await dialog.confirm(gt('task.exec.confirmDeleteExecution'))) return
    try {
      await apiPut(`/api/tasks/${task.value.id}`, {
        action: 'deleteExecution',
        executionId: String(execId),
      })
      toast.show(gt('task.exec.executionDeleted'), { type: 'success' })
    } catch (err: any) {
      toast.show(gt('task.exec.actionFailed'), { type: 'error' })
      return
    }
    await reloadExecutions()
  }

  async function deleteAllExecutions(): Promise<void> {
    if (!task.value?.id) return
    if (!await dialog.confirm(gt('task.exec.confirmDeleteAll'), { dangerous: true })) return
    try {
      await apiPut(`/api/tasks/${task.value.id}`, {
        action: 'deleteAllExecutions',
      })
      toast.show(gt('task.exec.allExecutionsDeleted'), { type: 'success' })
    } catch (err: any) {
      toast.show(gt('task.exec.actionFailed'), { type: 'error' })
      return
    }
    await reloadExecutions()
  }

  async function markExecRead(execId: string): Promise<void> {
    if (!task.value?.id || !execId) return
    try {
      await apiPut(`/api/tasks/${task.value.id}`, {
        action: 'read',
        executionId: execId,
      })
    } catch {
      // Silently ignore read-mark failures
    }
  }

  function openDetail(exec: any): void {
    if (exec.isUnread && !locallyReadIds.has(exec.id)) {
      locallyReadIds.add(exec.id)
      markExecRead(exec.id)
    }
    openExecDetail(exec.id, exec)
  }

  /** Extract a short preview string for the execution list.
   *  Keeps the original `summary` field intact for the detail view. */
  function extractPreview(exec: any): string {
    // Prefer backend-provided summary (truncated for list preview)
    if (exec.summary != null && exec.summary !== '') {
      return stripMarkdownPreview(exec.summary, 120)
    }
    // Fallback: extract first text block from content
    const { blocks } = chatRender.parseAssistantContent(exec.content)
    for (const block of blocks) {
      if (block.type === 'text' && block.text) {
        const clean = block.text
          .replace(/<scheduled-task\s+id="[^"]+"\s*\/>/g, '')
          .replace(/[#*`_~[\]()]/g, '')
          .trim()
        if (clean) {
          return clean.length > 120 ? clean.substring(0, 120) + '...' : clean
        }
      }
    }
    return ''
  }

  return {
    loading,
    loadingMore,
    hasMore,
    executions,
    runningExecutions,
    allExecutions,
    isRunning,
    isJustCompleted,
    locallyReadIds,
    loadExecutions,
    loadMoreExecutions,
    reloadExecutions,
    loadRunningStatus,
    cancelExecution,
    deleteExecution,
    deleteAllExecutions,
    markExecRead,
    openDetail,
    isUnreadDisplay,
    getSignal,
    onTaskChange,
  }
}
