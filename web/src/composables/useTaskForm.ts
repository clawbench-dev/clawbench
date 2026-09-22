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
    // Trigger mode: 'cron' (default) or 'event'.
    triggerMode: 'cron',
    // Comma-separated forge event subscription (event mode). The watched
    // repository is always the project's binding, so it is not form state.
    eventTypes: '',
    // Optional pre-AI shell script (cron tasks only). Runs before the AI call;
    // exit 0 with no output skips the run entirely.
    script: '',
    // Script timeout in seconds; 0 means the backend default.
    scriptTimeout: 0,
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
        triggerMode: (taskData.triggerMode as string) || 'cron',
        eventTypes: (taskData.eventTypes as string) || '',
        script: (taskData.script as string) || '',
        scriptTimeout: (taskData.scriptTimeout as number) || 0,
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
        triggerMode: 'cron',
        eventTypes: '',
        script: '',
        scriptTimeout: 0,
      }
    }
  }

  function validate(): boolean {
    const e: Record<string, string> = {}
    if (!form.value.name.trim()) e.name = 'task.form.nameRequired'
    if (!form.value.agentId) e.agentId = 'task.form.agentRequired'
    if (!form.value.prompt.trim()) e.prompt = 'task.form.promptRequired'
    // A timeout is only meaningful as a non-negative whole number of seconds;
    // a negative or fractional value would be rejected (or silently truncated)
    // by the backend, so catch it here instead. An empty input coerces to 0,
    // which is valid and means "use the backend default". Checked only in cron
    // mode: the script section (and so the field) is hidden for event tasks, so
    // an error there would be unreachable.
    if (form.value.triggerMode !== 'event') {
      const timeout = Number(form.value.scriptTimeout)
      if (!Number.isInteger(timeout) || timeout < 0) {
        e.scriptTimeout = 'task.form.scriptTimeoutInvalid'
      }
    }
    errors.value = e
    return Object.keys(e).length === 0
  }

  async function submit(): Promise<void> {
    if (!validate()) return
    if (saving.value) return
    saving.value = true
    formError.value = ''

    const isEvent = form.value.triggerMode === 'event'
    const payload = {
      name: form.value.name,
      // An event task has no cron schedule; send an empty expression so the
      // server does not synthesize a meaningless one.
      cron_expr: isEvent ? '' : (form.value.cronExpr || '0 9 * * *'),
      agent_id: form.value.agentId,
      prompt: form.value.prompt,
      repeat_mode: form.value.repeatMode,
      max_runs: form.value.maxRuns,
      trigger_mode: form.value.triggerMode,
      event_types: isEvent ? form.value.eventTypes : '',
      // A script is a cron-task precondition: an event task's prompt is driven
      // by the injected event context, so the field is cleared like event_types
      // is for the cron mode.
      script: isEvent ? '' : form.value.script,
      // 0 means "use the backend default" (300s); an empty input coerces to 0.
      script_timeout: isEvent ? 0 : Number(form.value.scriptTimeout) || 0,
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
