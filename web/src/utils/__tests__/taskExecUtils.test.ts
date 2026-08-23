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

import { cancelExecution, terminateExecution } from '@/utils/taskExecUtils'

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

describe('terminateExecution (shared util)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockDialogConfirm.mockResolvedValue(true)
    mockApiPut.mockResolvedValue({ ok: true })
  })

  it('refreshes exactly once and stops the preview on a successful cancel', async () => {
    const onRefresh = vi.fn()
    const onStopPreview = vi.fn()
    const ok = await terminateExecution({ taskId: 7, executionId: 'session-123', onRefresh, onStopPreview })
    expect(ok).toBe(true)
    // The refresh must fire exactly once — no double-refresh from the caller
    // re-reading the returned boolean after onSuccess already refreshed.
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(onStopPreview).toHaveBeenCalledTimes(1)
    // Preview is stopped before the refresh happens
    expect(onStopPreview.mock.invocationCallOrder[0]).toBeLessThan(onRefresh.mock.invocationCallOrder[0])
  })

  it('does not refresh or stop the preview when the user dismisses the dialog', async () => {
    mockDialogConfirm.mockResolvedValue(false)
    const onRefresh = vi.fn()
    const onStopPreview = vi.fn()
    const ok = await terminateExecution({ taskId: 7, executionId: 'session-123', onRefresh, onStopPreview })
    expect(ok).toBe(false)
    expect(onRefresh).not.toHaveBeenCalled()
    expect(onStopPreview).not.toHaveBeenCalled()
  })

  it('does not refresh when the cancel fails (404 already-finished)', async () => {
    mockApiPut.mockRejectedValue(new Error('TaskExecutionNotFound'))
    const onRefresh = vi.fn()
    const onStopPreview = vi.fn()
    const ok = await terminateExecution({ taskId: 7, executionId: 'session-123', onRefresh, onStopPreview })
    expect(ok).toBe(false)
    expect(onRefresh).not.toHaveBeenCalled()
    expect(onStopPreview).not.toHaveBeenCalled()
  })

  it('refreshes only once even when the caller inspects the return value', async () => {
    // Reproduces the original double-refresh bug: the caller refreshing again
    // when the returned boolean is true must NOT be needed — onRefresh already
    // fired once on the success path.
    const onRefresh = vi.fn()
    const ok = await terminateExecution({ taskId: 7, executionId: 'session-123', onRefresh })
    expect(ok).toBe(true)
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })
})
