import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import {
  beginDownload, cancelDownload, endDownload, isCurrentDownload,
  nextDownloadId, reportDownloadProgress, setDownloadCancel,
  setDownloadTotal, useDownloadProgress,
} from '@/composables/useDownloadProgress'

describe('useDownloadProgress', () => {
  beforeEach(() => {
    // Leave no bar visible between tests; ids stay monotonic by design.
    cancelDownload()
  })

  it('shows the bar on begin and hides it on end', () => {
    const { downloadVisible, downloadFileName, downloadReceived, downloadTotal } = useDownloadProgress()
    const id = nextDownloadId()

    expect(beginDownload(id, 'report.pdf', 0)).toBe(true)
    expect(downloadVisible.value).toBe(true)
    expect(downloadFileName.value).toBe('report.pdf')
    expect(downloadReceived.value).toBe(0)
    // 0 means "unknown size" (no Content-Length) — the bar goes indeterminate.
    expect(downloadTotal.value).toBe(0)

    endDownload(id)
    expect(downloadVisible.value).toBe(false)
    expect(downloadFileName.value).toBe('')
  })

  it('refuses a second concurrent download so the single bar is not hijacked', () => {
    const first = nextDownloadId()
    const second = nextDownloadId()

    expect(beginDownload(first, 'a.bin', 10)).toBe(true)
    expect(beginDownload(second, 'b.bin', 10)).toBe(false)
    expect(useDownloadProgress().downloadFileName.value).toBe('a.bin')
  })

  it('records progress and total', () => {
    const { downloadReceived, downloadTotal } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'big.bin', 0)

    setDownloadTotal(id, 4096)
    expect(downloadTotal.value).toBe(4096)

    reportDownloadProgress(id, 1024)
    expect(downloadReceived.value).toBe(1024)

    // A later report can also carry the total (native events do both at once).
    reportDownloadProgress(id, 2048, 4096)
    expect(downloadReceived.value).toBe(2048)
    expect(downloadTotal.value).toBe(4096)
  })

  it('ignores progress from a stale id', () => {
    const { downloadReceived, downloadTotal } = useDownloadProgress()
    const current = nextDownloadId()
    beginDownload(current, 'current.bin', 100)
    reportDownloadProgress(current, 50)
    expect(downloadReceived.value).toBe(50)

    // A superseded download's late event must not move the bar.
    reportDownloadProgress(current - 1, 99, 999)
    expect(downloadReceived.value).toBe(50)
    expect(downloadTotal.value).toBe(100)
  })

  it('ignores progress after the download ended', () => {
    const { downloadVisible, downloadReceived } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'done.bin', 100)
    endDownload(id)

    reportDownloadProgress(id, 42, 100)
    expect(downloadVisible.value).toBe(false)
    expect(downloadReceived.value).toBe(0)
  })

  it('isCurrentDownload reflects the visible download only', () => {
    const id = nextDownloadId()
    expect(isCurrentDownload(id)).toBe(false)
    beginDownload(id, 'x.bin', 1)
    expect(isCurrentDownload(id)).toBe(true)
    endDownload(id)
    expect(isCurrentDownload(id)).toBe(false)
  })

  it('invokes the registered cancel callback and resets the bar', () => {
    const { downloadVisible } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'cancel.bin', 100)

    let cancelled = false
    setDownloadCancel(id, () => { cancelled = true })

    cancelDownload()
    expect(cancelled).toBe(true)
    expect(downloadVisible.value).toBe(false)
  })

  it('does not throw when the cancel callback itself throws', () => {
    const { downloadVisible } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'boom.bin', 100)
    setDownloadCancel(id, () => { throw new Error('transport gone') })

    expect(() => cancelDownload()).not.toThrow()
    // The UI must still reset even though the transport cancel failed.
    expect(downloadVisible.value).toBe(false)
  })

  it('cancelDownload is a no-op when nothing is running', () => {
    expect(() => cancelDownload()).not.toThrow()
  })
})

describe('download speed', () => {
  beforeEach(() => {
    cancelDownload()
    vi.useFakeTimers()
    vi.setSystemTime(0)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('stays 0 until two samples have arrived', () => {
    const { downloadSpeed } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'big.bin', 10_000_000)

    expect(downloadSpeed.value).toBe(0)
    reportDownloadProgress(id, 1_000_000)
    expect(downloadSpeed.value).toBe(0)
  })

  it('reports bytes per second between samples', () => {
    const { downloadSpeed } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'big.bin', 10_000_000)

    vi.setSystemTime(1000)
    reportDownloadProgress(id, 1_000_000)
    expect(downloadSpeed.value).toBeCloseTo(1_000_000, 0)

    vi.setSystemTime(2000)
    reportDownloadProgress(id, 3_000_000)
    expect(downloadSpeed.value).toBeCloseTo(1_500_000, 0)
  })

  it('averages over the recent window instead of the last instant', () => {
    const { downloadSpeed } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'big.bin', 100_000_000)

    vi.setSystemTime(1000)
    reportDownloadProgress(id, 1_000_000)
    vi.setSystemTime(2000)
    reportDownloadProgress(id, 2_000_000)
    // Four seconds later only one 1 MB step arrived. Samples older than the
    // window are dropped, but the newest one before the boundary stays as the
    // baseline, so the rate is 1 MB over those 4 s rather than a spike.
    vi.setSystemTime(6000)
    reportDownloadProgress(id, 3_000_000)

    expect(downloadSpeed.value).toBeCloseTo(250_000, 0)
  })

  it('ignores stale ids and resets when the transfer ends', () => {
    const { downloadSpeed } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'big.bin', 10_000_000)

    vi.setSystemTime(1000)
    reportDownloadProgress(id, 1_000_000)
    expect(downloadSpeed.value).toBeCloseTo(1_000_000, 0)

    vi.setSystemTime(5000)
    reportDownloadProgress(id - 1, 9_000_000)
    expect(downloadSpeed.value).toBeCloseTo(1_000_000, 0)

    endDownload(id)
    expect(downloadSpeed.value).toBe(0)
  })

  it('restarts the measurement when the byte count goes backwards', () => {
    const { downloadSpeed } = useDownloadProgress()
    const id = nextDownloadId()
    beginDownload(id, 'big.bin', 10_000_000)

    vi.setSystemTime(1000)
    reportDownloadProgress(id, 5_000_000)
    expect(downloadSpeed.value).toBeCloseTo(5_000_000, 0)

    // A superseded/restarted counter must not yield a negative rate.
    vi.setSystemTime(2000)
    reportDownloadProgress(id, 0)
    expect(downloadSpeed.value).toBe(0)

    vi.setSystemTime(3000)
    reportDownloadProgress(id, 1_000_000)
    expect(downloadSpeed.value).toBeCloseTo(1_000_000, 0)
  })
})
