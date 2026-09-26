import { describe, expect, it, beforeEach } from 'vitest'
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
