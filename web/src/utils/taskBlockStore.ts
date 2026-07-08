import { reactive } from 'vue'
import { apiGet } from '@/utils/api'

interface TaskBlockEntry {
  taskId: number
  task: Record<string, unknown> | null
  loading: boolean
  deleted: boolean
  error?: boolean
}

interface TaskKey {
  key: string
  taskId: number
}

export function createTaskBlockStore() {
  const blocks: Record<string, TaskBlockEntry> = reactive({})

  async function fetchBatchData(taskKeys: TaskKey[]): Promise<void> {
    const pending = taskKeys.filter(({ key }) =>
      !blocks[key]?.task && !blocks[key]?.loading
    )
    if (pending.length === 0) return

    // Mark all as loading
    for (const { key, taskId } of pending) {
      blocks[key] = { taskId, task: null, loading: true, deleted: false, error: false }
    }

    try {
      const data = await apiGet<{ tasks: Record<string, unknown>[] }>('/api/tasks')
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const taskMap = new Map((data.tasks || []).map((t: any) => [t.id, t]))

      for (const { key, taskId } of pending) {
        const task = taskMap.get(taskId)
        if (task) {
          blocks[key].task = task
        } else {
          blocks[key].deleted = true
        }
        blocks[key].loading = false
      }
    } catch {
      // ISS-013 fix: network errors should NOT mark tasks as deleted.
      // Only clear loading and set error flag.
      for (const { key } of pending) {
        blocks[key].loading = false
        blocks[key].error = true
      }
    }
  }

  return { blocks, fetchBatchData }
}
