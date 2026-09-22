import { describe, expect, it, vi, beforeEach } from 'vitest'

// ────────────────────────────────────────────────────────────
// useTaskForm composable tests
// Tests ISS-011 (raw fetch → apiPost/apiPut) and ISS-012
// (error mapping to correct field + formError ref)
// ────────────────────────────────────────────────────────────

// Mock i18n
vi.mock('@/i18n', () => ({
  default: {
    global: {
      locale: { value: 'en' },
      t: (key: string) => key,
    },
  },
}))

// Mock useI18n
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

// Mock useAgents
vi.mock('@/composables/useAgents.ts', () => ({
  useAgents: () => ({
    agents: { value: [{ id: 'agent-1', backend: 'codebuddy', name: 'TestAgent' }] },
    loadAgents: vi.fn(),
  }),
}))

// Mock API helpers
const mockApiPost = vi.fn()
const mockApiPut = vi.fn()
vi.mock('@/utils/api.ts', () => ({
  apiPost: (...args: unknown[]) => mockApiPost(...args),
  apiPut: (...args: unknown[]) => mockApiPut(...args),
}))

// Import after mocks
import { useTaskForm } from '@/composables/useTaskForm.ts'
import { ref } from 'vue'

beforeEach(() => {
  mockApiPost.mockReset()
  mockApiPut.mockReset()
})

// ── Helper ──

function createForm(options: { mode?: string; task?: any } = {}) {
  const mode = ref(options.mode || 'create')
  const task = ref(options.task || null)
  const saved = vi.fn()
  const closed = vi.fn()

  const form = useTaskForm({
    mode,
    task,
    onSuccess: saved,
    onClose: closed,
  })

  return { form, saved, closed }
}

// ── Tests ──

describe('useTaskForm', () => {
  // ── Submit in create mode ──

  describe('submit (create mode)', () => {
    it('calls apiPost with correct payload', async () => {
      const { form, saved } = createForm({ mode: 'create' })

      // Fill form
      form.form.value.name = 'Daily Report'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Generate daily report'

      mockApiPost.mockResolvedValue({ task: { id: 'task-1' } })

      await form.submit()

      expect(mockApiPost).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({
        name: 'Daily Report',
        agent_id: 'agent-1',
        prompt: 'Generate daily report',
      }))
      expect(saved).toHaveBeenCalledWith('task-1')
    })

    it('does not call apiPost when validation fails', async () => {
      const { form } = createForm({ mode: 'create' })

      // Leave form empty
      await form.submit()

      expect(mockApiPost).not.toHaveBeenCalled()
      expect(Object.keys(form.errors.value).length).toBeGreaterThan(0)
    })

    it('sends script and script_timeout in the payload', async () => {
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'Daily'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Report'
      form.form.value.script = 'git diff --quiet && exit 0'
      form.form.value.scriptTimeout = 45

      mockApiPost.mockResolvedValue({ task: { id: 'task-11' } })
      await form.submit()

      expect(mockApiPost).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({
        script: 'git diff --quiet && exit 0',
        script_timeout: 45,
      }))
    })

    it('sends script_timeout 0 (a number) when the timeout is left untouched', async () => {
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'Daily'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Report'
      form.form.value.script = 'echo hi'
      // The field starts empty (the 300s default is a placeholder, not a
      // value), so it is deliberately NOT assigned here. The empty state must
      // reach the server as the number 0 — the backend's "use the default"
      // sentinel — not as the string ''.
      expect(form.form.value.scriptTimeout).toBe('')

      mockApiPost.mockResolvedValue({ task: { id: 'task-12' } })
      await form.submit()

      const payload = mockApiPost.mock.calls[0][1] as { script_timeout: unknown }
      expect(payload.script_timeout).toBe(0)
      expect(typeof payload.script_timeout).toBe('number')
      // An empty timeout is the default, not an error.
      expect(form.errors.value.scriptTimeout).toBeFalsy()
    })

    it('rejects a negative or fractional script timeout', async () => {
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'Daily'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Report'

      for (const bad of [-1, 1.5]) {
        form.form.value.scriptTimeout = bad
        await form.submit()
        expect(form.errors.value.scriptTimeout, `timeout=${bad}`).toBeTruthy()
      }
      expect(mockApiPost).not.toHaveBeenCalled()
    })
  })

  // ── Submit in edit mode ──

  describe('submit (edit mode)', () => {
    it('calls apiPut with correct payload', async () => {
      const { form, saved } = createForm({
        mode: 'edit',
        task: {
          id: 'task-42',
          name: 'Old Name',
          cronExpr: '0 9 * * *',
          agentId: 'agent-1',
          prompt: 'Old prompt',
          repeatMode: 'unlimited',
          maxRuns: 0,
        },
      })

      // Initialize form from task data (normally called in onMounted)
      form.init({
        id: 'task-42',
        name: 'Old Name',
        cronExpr: '0 9 * * *',
        agentId: 'agent-1',
        prompt: 'Old prompt',
        repeatMode: 'unlimited',
        maxRuns: 0,
      })

      form.form.value.name = 'Updated Name'

      mockApiPut.mockResolvedValue({ task: { id: 'task-42' } })

      await form.submit()

      expect(mockApiPut).toHaveBeenCalledWith('/api/tasks/task-42', expect.objectContaining({
        name: 'Updated Name',
      }))
      expect(saved).toHaveBeenCalledWith('task-42')
    })
  })

  // ── ISS-012: Error mapping ──

  describe('error mapping (ISS-012)', () => {
    it('sets formError on network error (not cronExpr)', async () => {
      const { form } = createForm({ mode: 'create' })

      form.form.value.name = 'Test'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Do something'

      mockApiPost.mockRejectedValue(new Error('Network error'))

      await form.submit()

      // formError should be set, NOT errors.cronExpr
      expect(form.formError.value).toBeTruthy()
      expect(form.errors.value.cronExpr).toBeFalsy()
    })

    it('maps server error with "cron" keyword to errors.cronExpr', async () => {
      const { form } = createForm({ mode: 'create' })

      form.form.value.name = 'Test'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Do something'

      mockApiPost.mockRejectedValue(new Error('Invalid cron expression'))

      await form.submit()

      expect(form.errors.value.cronExpr).toBe('Invalid cron expression')
      expect(form.formError.value).toBeFalsy()
    })

    it('maps server error with "agent" keyword to errors.agentId', async () => {
      const { form } = createForm({ mode: 'create' })

      form.form.value.name = 'Test'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Do something'

      mockApiPost.mockRejectedValue(new Error('Agent not found'))

      await form.submit()

      expect(form.errors.value.agentId).toBe('Agent not found')
      expect(form.formError.value).toBeFalsy()
    })

    it('maps unknown server error to formError', async () => {
      const { form } = createForm({ mode: 'create' })

      form.form.value.name = 'Test'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Do something'

      mockApiPost.mockRejectedValue(new Error('Internal server error'))

      await form.submit()

      expect(form.formError.value).toBe('Internal server error')
    })
  })

  // ── Validation ──

  describe('validate', () => {
    it('returns false for empty name', () => {
      const { form } = createForm()
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Do something'

      const result = form.validate()

      expect(result).toBe(false)
      expect(form.errors.value.name).toBeTruthy()
    })

    it('returns false for empty agentId', () => {
      const { form } = createForm()
      form.form.value.name = 'Test'
      form.form.value.prompt = 'Do something'

      const result = form.validate()

      expect(result).toBe(false)
      expect(form.errors.value.agentId).toBeTruthy()
    })

    it('returns false for empty prompt', () => {
      const { form } = createForm()
      form.form.value.name = 'Test'
      form.form.value.agentId = 'agent-1'

      const result = form.validate()

      expect(result).toBe(false)
      expect(form.errors.value.prompt).toBeTruthy()
    })

    it('returns true for valid form', () => {
      const { form } = createForm()
      form.form.value.name = 'Test'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Do something'

      const result = form.validate()

      expect(result).toBe(true)
      expect(Object.keys(form.errors.value)).toHaveLength(0)
    })
  })

  // ── Event-triggered tasks ──

  describe('event trigger payload', () => {
    it('sends trigger_mode and event fields, and no cron expression', async () => {
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'On new PR'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Review it'
      form.form.value.triggerMode = 'event'
      form.form.value.eventTypes = 'opened,commented'
      // A leftover cron value must NOT be sent for an event task.
      form.form.value.cronExpr = '0 9 * * *'

      mockApiPost.mockResolvedValue({ task: { id: 'task-9' } })
      await form.submit()

      // No repository is sent: the task always watches its project's binding.
      expect(mockApiPost).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({
        trigger_mode: 'event',
        event_types: 'opened,commented',
        cron_expr: '',
      }))
      const payload = mockApiPost.mock.calls[0][1] as Record<string, unknown>
      expect(payload).not.toHaveProperty('event_repo')
    })

    it('cron task omits event fields and keeps its cron expression', async () => {
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'Daily'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Report'
      form.form.value.triggerMode = 'cron'
      form.form.value.cronExpr = '0 9 * * *'
      form.form.value.eventTypes = 'opened' // stale value must be cleared

      mockApiPost.mockResolvedValue({ task: { id: 'task-10' } })
      await form.submit()

      expect(mockApiPost).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({
        trigger_mode: 'cron',
        cron_expr: '0 9 * * *',
        event_types: '',
      }))
    })

    it('init reads trigger fields from task data', () => {
      const { form } = createForm({ mode: 'edit' })
      form.init({
        id: 5,
        name: 'Event task',
        cronExpr: '',
        agentId: 'agent-1',
        prompt: 'go',
        repeatMode: 'unlimited',
        maxRuns: 0,
        triggerMode: 'event',
        eventTypes: 'merged',
      })

      expect(form.form.value.triggerMode).toBe('event')
      expect(form.form.value.eventTypes).toBe('merged')
    })

    it('defaults triggerMode to cron when task data omits it', () => {
      const { form } = createForm({ mode: 'edit' })
      form.init({ id: 6, name: 'Legacy', cronExpr: '0 9 * * *', agentId: 'a', prompt: 'p' })
      expect(form.form.value.triggerMode).toBe('cron')
    })
  })

  // ── Pre-AI script ──

  describe('script (pre-AI precondition)', () => {
    it('init reads script fields from task data', () => {
      const { form } = createForm({ mode: 'edit' })
      form.init({
        id: 7,
        name: 'Watch',
        cronExpr: '0 9 * * *',
        agentId: 'agent-1',
        prompt: 'p',
        script: 'make check',
        scriptTimeout: 120,
      })

      expect(form.form.value.script).toBe('make check')
      expect(form.form.value.scriptTimeout).toBe(120)
    })

    it('init defaults script fields when the task has none', () => {
      const { form } = createForm({ mode: 'edit' })
      form.init({ id: 8, name: 'Legacy', cronExpr: '0 9 * * *', agentId: 'a', prompt: 'p' })

      expect(form.form.value.script).toBe('')
      // No stored timeout means "use the backend default", rendered as an
      // empty field with the 300s placeholder rather than a literal 0.
      expect(form.form.value.scriptTimeout).toBe('')
    })

    it('clears the script when the task is switched to event mode', async () => {
      // An event task's prompt is driven by the injected event context, so the
      // script is not applicable — sending a stale one would persist a script
      // the backend ignores.
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'Event task'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Review'
      form.form.value.triggerMode = 'event'
      form.form.value.eventTypes = 'pr.opened'
      form.form.value.script = 'leftover script'
      form.form.value.scriptTimeout = 60

      mockApiPost.mockResolvedValue({ task: { id: 'task-13' } })
      await form.submit()

      expect(mockApiPost).toHaveBeenCalledWith('/api/tasks', expect.objectContaining({
        script: '',
        script_timeout: 0,
      }))
    })

    it('does not validate the timeout in event mode (the field is hidden)', async () => {
      // The script section only renders for cron tasks, so an invalid leftover
      // timeout must not block an event task's save with an unreachable error.
      const { form } = createForm({ mode: 'create' })
      form.form.value.name = 'Event task'
      form.form.value.agentId = 'agent-1'
      form.form.value.prompt = 'Review'
      form.form.value.triggerMode = 'event'
      form.form.value.eventTypes = 'pr.opened'
      form.form.value.scriptTimeout = -5

      mockApiPost.mockResolvedValue({ task: { id: 'task-14' } })
      await form.submit()

      expect(form.errors.value.scriptTimeout).toBeFalsy()
      expect(mockApiPost).toHaveBeenCalled()
    })
  })
})
