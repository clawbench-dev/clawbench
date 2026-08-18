/**
 * Stream frame scheduler — batches multiple streaming-related callbacks
 * into the same requestAnimationFrame to avoid overlapping setTimeout
 * handlers that cause "[Violation] 'setTimeout' handler took Nms" warnings.
 *
 * Usage:
 *   const scheduler = new StreamFrameScheduler()
 *   scheduler.schedule('render', onRenderNeeded)
 *   scheduler.schedule('scroll', onScrollBottom)
 *   // Both run in the same rAF frame, sequentially, no overlap.
 */

export class StreamFrameScheduler {
  private _queued = new Map<string, () => void>()
  private _rafId: number | null = null

  /** Schedule a named callback. If a callback with the same name already
   *  exists, it is replaced. All queued callbacks run in the next rAF. */
  schedule(name: string, fn: () => void): void {
    this._queued.set(name, fn)
    if (this._rafId === null) {
      this._rafId = requestAnimationFrame(() => this._flush())
    }
  }

  /** Cancel a named callback. */
  cancel(name: string): void {
    this._queued.delete(name)
    if (this._queued.size === 0 && this._rafId !== null) {
      cancelAnimationFrame(this._rafId)
      this._rafId = null
    }
  }

  /** Cancel all pending callbacks. */
  cancelAll(): void {
    this._queued.clear()
    if (this._rafId !== null) {
      cancelAnimationFrame(this._rafId)
      this._rafId = null
    }
  }

  /** Whether a named callback is pending. */
  has(name: string): boolean {
    return this._queued.has(name)
  }

  /** Whether any callbacks are pending. */
  get pending(): boolean {
    return this._queued.size > 0
  }

  private _flush(): void {
    this._rafId = null
    // Copy to avoid mutations during iteration
    const fns = [...this._queued.values()]
    this._queued.clear()
    for (const fn of fns) {
      fn()
    }
  }
}
