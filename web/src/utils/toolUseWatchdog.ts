/**
 * ToolUseWatchdog — per-tool-call timeout management for streaming tool_use
 * blocks.
 *
 * Semantics: a tool call that keeps producing progress events (e.g. ACP
 * ToolCallUpdate, Claude content_block_delta) must NOT be marked as finished
 * just because it has been running for a while. Only a tool call that goes
 * silent for `timeoutMs` is considered stalled and should fall back to done.
 *
 * `start()` therefore RESETS the timer every time it is called — call it on
 * every progress event for the tool id, not just on first sight.
 */
export class ToolUseWatchdog {
  private timers = new Map<string, ReturnType<typeof setTimeout>>()

  /**
   * Start (or reset) the watchdog timer for a tool call.
   *
   * @param toolId     Stable tool call id (matches ContentBlock.id).
   * @param timeoutMs  How long the tool may be silent before onTimeout fires.
   * @param onTimeout  Called once when the tool goes silent for too long.
   */
  start(toolId: string, timeoutMs: number, onTimeout: () => void): void {
    const existing = this.timers.get(toolId)
    if (existing) {
      clearTimeout(existing)
    }
    const timer = setTimeout(() => {
      this.timers.delete(toolId)
      onTimeout()
    }, timeoutMs)
    this.timers.set(toolId, timer)
  }

  /** Cancel the watchdog for a single tool call (e.g. it finished). */
  clear(toolId: string): void {
    const timer = this.timers.get(toolId)
    if (timer) {
      clearTimeout(timer)
      this.timers.delete(toolId)
    }
  }

  /** Cancel all watchdog timers (e.g. stream disconnected, component unmounted). */
  clearAll(): void {
    for (const timer of this.timers.values()) {
      clearTimeout(timer)
    }
    this.timers.clear()
  }
}
