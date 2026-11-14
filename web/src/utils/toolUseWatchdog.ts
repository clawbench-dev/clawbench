/**
 * ToolUseWatchdog — per-tool-call timeout management for streaming tool_use
 * blocks.
 *
 * Only a tool whose completion can NEVER arrive needs this: a native
 * AskUserQuestion (answered by a chat message, so no tool_result is emitted) and
 * an ACP elicitation's exemption aside. An ordinary tool going silent is not
 * evidence that it stopped — a build can run for minutes without a progress
 * frame — and the turn's terminal paths close whatever is still open. Do not
 * arm this for them; doing so showed 已中断 on tools that then finished fine.
 *
 * `start()` RESETS the timer every time it is called — call it on every progress
 * event for the tool id, not just on first sight.
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
