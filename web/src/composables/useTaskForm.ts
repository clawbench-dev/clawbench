import { ref, type Ref } from 'vue'
import { apiPost, apiPut } from '@/utils/api'

interface UseTaskFormOptions {
  mode: Ref<string>
  onSuccess: (taskId: number) => void
}

/** Map a server error message to the correct form field, or return '' for formError */
function mapServerError(error: string): { field?: string; message: string } {
  const lower = error.toLowerCase()
  if (lower.includes('cron') || lower.includes('frequency') || lower.includes('schedule')) {
    return { field: 'cronExpr', message: error }
  }
  if (lower.includes('agent')) {
    return { field: 'agentId', message: error }
  }
  if (lower.includes('name')) {
    return { field: 'name', message: error }
  }
  if (lower.includes('prompt')) {
    return { field: 'prompt', message: error }
  }
  return { message: error }
}

export function useTaskForm(options: UseTaskFormOptions) {
  const { mode, onSuccess } = options

  const saving = ref(false)
  const formError = ref('')

  const form = ref({
    id: 0,
    name: '',
    cronExpr: '',
    agentId: '',
    prompt: '',
    repeatMode: 'unlimited',
    maxRuns: 0,
  })

  const errors = ref<Record<string, string>>({})

  /** Initialize form from task data (called on mount or when task changes) */
  function init(taskData?: Record<string, unknown>) {
    errors.value = {}
    formError.value = ''

    if (taskData) {
      form.value = {
        id: (taskData.id as number) || 0,
        name: (taskData.name as string) || '',
        cronExpr: (taskData.cronExpr as string) || '',
        agentId: (taskData.agentId as string) || '',
        prompt: (taskData.prompt as string) || '',
        repeatMode: (taskData.repeatMode as string) || 'unlimited',
        maxRuns: (taskData.maxRuns as number) || 0,
      }
    } else {
      form.value = {
        id: 0,
        name: '',
        cronExpr: '',
        agentId: '',
        prompt: '',
        repeatMode: 'unlimited',
        maxRuns: 0,
      }
    }
  }

  function validate(): boolean {
    const e: Record<string, string> = {}
    if (!form.value.name.trim()) e.name = 'task.form.nameRequired'
    if (!form.value.agentId) e.agentId = 'task.form.agentRequired'
    if (!form.value.prompt.trim()) e.prompt = 'task.form.promptRequired'
    errors.value = e
    return Object.keys(e).length === 0
  }

  async function submit(): Promise<void> {
    if (!validate()) return
    if (saving.value) return
    saving.value = true
    formError.value = ''

    const payload = {
      name: form.value.name,
      cron_expr: form.value.cronExpr || '0 9 * * *',
      agent_id: form.value.agentId,
      prompt: form.value.prompt,
      repeat_mode: form.value.repeatMode,
      max_runs: form.value.maxRuns,
    }

    try {
      let result: Record<string, unknown>
      if (mode.value === 'create') {
        result = await apiPost('/api/tasks', payload)
      } else {
        result = await apiPut(`/api/tasks/${form.value.id}`, payload)
      }
      const res = result as unknown as Record<string, unknown>
      const task = res.task as Record<string, unknown> | undefined
      onSuccess(task?.id as number)
    } catch (err: unknown) {
      const message = (err as { message?: string })?.message || 'common.networkError'
      const mapped = mapServerError(message)
      if (mapped.field) {
        errors.value = { ...errors.value, [mapped.field]: mapped.message }
      } else {
        formError.value = mapped.message
      }
    } finally {
      saving.value = false
    }
  }

  return { form, errors, formError, saving, validate, submit, init }
}
