import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const h = vi.hoisted(() => ({
  apiGet: vi.fn(),
  show: vi.fn(),
}))

vi.mock('@/utils/api', () => ({ apiGet: h.apiGet }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: h.show, visible: { value: false } }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${String(params.count)}` : key,
    },
  },
}))

import {
  _resetKBIndexWatchForTest,
  startKBIndexWatch,
  useKBIndexStatus,
} from '@/composables/useKBIndexStatus'

function status(pending: number, failed = 0) {
  return {
    available: true,
    stats: { pending_docs: pending, failed_docs: failed },
  }
}

describe('useKBIndexStatus', () => {
  beforeEach(() => {
    h.apiGet.mockReset()
    h.show.mockReset()
    _resetKBIndexWatchForTest()
    vi.useFakeTimers()
  })

  afterEach(() => {
    _resetKBIndexWatchForTest()
    vi.useRealTimers()
  })

  it('reports one completion toast after the queue drains', async () => {
    h.apiGet.mockResolvedValueOnce(status(2)).mockResolvedValueOnce(status(0))
    startKBIndexWatch()
    await vi.advanceTimersByTimeAsync(0)
    expect(useKBIndexStatus().pending.value).toBe(2)

    await vi.advanceTimersByTimeAsync(3000)
    expect(h.show).toHaveBeenCalledTimes(1)
    expect(h.show).toHaveBeenCalledWith('kb.indexDone', expect.objectContaining({ type: 'success' }))

    // The watcher stops itself; no further polls or toasts.
    await vi.advanceTimersByTimeAsync(9000)
    expect(h.apiGet).toHaveBeenCalledTimes(2)
    expect(h.show).toHaveBeenCalledTimes(1)
  })

  it('reports failures that appeared during the batch', async () => {
    h.apiGet.mockResolvedValueOnce(status(2, 0)).mockResolvedValueOnce(status(0, 1))
    startKBIndexWatch()
    await vi.advanceTimersByTimeAsync(0)
    await vi.advanceTimersByTimeAsync(3000)

    expect(h.show).toHaveBeenCalledWith(
      'kb.indexDoneFailed:1',
      expect.objectContaining({ type: 'error' }),
    )
  })

  it('stays silent when it never saw work', async () => {
    h.apiGet.mockResolvedValue(status(0))
    startKBIndexWatch()
    await vi.advanceTimersByTimeAsync(0)

    expect(h.show).not.toHaveBeenCalled()
    expect(h.apiGet).toHaveBeenCalledTimes(1)
  })

  it('keeps polling through a transient status failure', async () => {
    h.apiGet
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce(status(1))
      .mockResolvedValueOnce(status(0))
    startKBIndexWatch()
    await vi.advanceTimersByTimeAsync(0)
    await vi.advanceTimersByTimeAsync(3000)
    expect(useKBIndexStatus().pending.value).toBe(1)

    await vi.advanceTimersByTimeAsync(3000)
    expect(h.show).toHaveBeenCalledWith('kb.indexDone', expect.anything())
  })
})
