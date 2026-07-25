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
})
