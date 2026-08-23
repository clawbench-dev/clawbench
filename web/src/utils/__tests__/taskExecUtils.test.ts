import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockApiPut = vi.fn()
vi.mock('@/utils/api', () => ({
  apiPut: (...args: unknown[]) => mockApiPut(...args),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

const mockDialogConfirm = vi.fn()
vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string, params?: Record<string, unknown>) => {
    const map: Record<string, string> = {
      'task.exec.confirmCancel': 'Cancel this execution?',
      'task.exec.cancelled': 'Execution cancelled',
      'task.exec.alreadyFinished': 'Execution already finished',
      'task.exec.actionFailedDetail': 'Action failed: {error}',
    }
    let s = map[key] ?? key
    if (params) {
      for (const [k, v] of Object.entries(params)) s = s.replace(`{${k}}`, String(v))
    }
    return s
  },
}))

import { cancelExecution } from '@/utils/taskExecUtils'

describe('cancelExecution (shared util)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockDialogConfirm.mockResolvedValue(true)
    mockApiPut.mockResolvedValue({ ok: true })
  })

  it('calls the backend with action=cancel and the given execution id', async () => {
    const ok = await cancelExecution({ taskId: 7, executionId: 'session-123' })
    expect(ok).toBe(true)
    expect(mockApiPut).toHaveBeenCalledWith('/api/tasks/7', {
      action: 'cancel',
      executionId: 'session-123',
    })
    expect(mockToastShow).toHaveBeenCalledWith('Execution cancelled', expect.objectContaining({ type: 'success' }))
  })

  it('does not call the backend when the user dismisses the confirm dialog', async () => {
    mockDialogConfirm.mockResolvedValue(false)
    const ok = await cancelExecution({ taskId: 7, executionId: 'session-123' })
    expect(ok).toBe(false)
    expect(mockApiPut).not.toHaveBeenCalled()
  })

  it('invokes onSuccess only after a successful cancel', async () => {
    const onSuccess = vi.fn()
    await cancelExecution({ taskId: 7, executionId: 'session-123', onSuccess })
    expect(onSuccess).toHaveBeenCalledTimes(1)
  })

  it('reports "already finished" for a 404 TaskExecutionNotFound error', async () => {
    mockApiPut.mockRejectedValue(new Error('TaskExecutionNotFound'))
    const ok = await cancelExecution({ taskId: 7, executionId: 'session-123' })
    expect(ok).toBe(false)
    expect(mockToastShow).toHaveBeenCalledWith('Execution already finished', expect.objectContaining({ type: 'info' }))
  })

  it('reports a generic error message for unexpected failures', async () => {
    mockApiPut.mockRejectedValue(new Error('boom'))
    const ok = await cancelExecution({ taskId: 7, executionId: 'session-123' })
    expect(ok).toBe(false)
    expect(mockToastShow).toHaveBeenCalledWith(
      expect.stringContaining('boom'),
      expect.objectContaining({ type: 'error' }),
    )
  })
})
