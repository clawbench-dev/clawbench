/* eslint-disable @typescript-eslint/no-explicit-any -- chat blocks are untyped wire payloads, exactly the shape ContentBlocks already passes around */
/**
 * Grouping for the "compact tool calls" chat mode (设置 → 聊天 → 消息).
 *
 * One assistant turn routinely runs a dozen tools, and every tool renders as a
 * full-width pill, so the transcript turns into a wall of bars. This folds a
 * run of tool calls into one summary row that reports how many ran.
 *
 * Thinking chips between two tool calls are part of the same run: real turns
 * arrive as `thinking, tool, tool, thinking, tool, tool, …`, so requiring
 * strictly adjacent tool blocks would leave the fold with almost nothing to
 * merge. A run therefore spans tool calls AND the thinking chips between them;
 * the whole run collapses into the single row, which reports both counts.
 *
 * Pure function on purpose — the grouping rules are testable without mounting
 * the (very large) ContentBlocks component.
 */

import { TOOL_STATUS_INTERRUPTED } from '@/utils/toolStatus'

export interface ToolCallGroup {
  /** Index of the first block of the run (a tool or a thinking chip). */
  start: number
  /** Index of the last block of the run. */
  end: number
  /** How many tool calls the run contains. */
  toolCount: number
  /** How many thinking chips the run contains. */
  thinkingCount: number
  /** How many of those tools finished with `status === 'error'`. */
  failedCount: number
  /** How many were closed without a result (turn cancelled / agent crash). */
  interruptedCount: number
  /** 1-based position of the running tool inside the run; 0 when none runs. */
  runningIndex: number
  /**
   * Whether a thinking chip in the run has not reported done yet. The caller
   * still gates this on the message being live — a finished turn can leave a
   * tail thinking block without `done`.
   */
  runningThinking: boolean
  /**
   * Icon/colour category of the running tool (`file` / `bash` / `search` …), or
   * `''` when nothing runs. The summary row wears this category's accent while
   * a tool runs, so it looks like the pill it is standing in for.
   */
  runningCategory: string
  /**
   * Raw tool name of the running tool, e.g. `Bash`. The row's leading icon is
   * resolved from it, so the row shows the tool actually running rather than a
   * generic "a run of tools" glyph. `''` when nothing runs.
   */
  runningToolName: string
  /** Display name of the running tool, e.g. "Bash". */
  runningName: string
  /** One-line summary of the running tool, e.g. "npm test". */
  runningSummary: string
}

export interface ToolCallGroupOptions {
  /** Whether this block is a tool call the user wants folded. */
  isTool: (block: any, index: number) => boolean
  /** Whether this block is a thinking chip that may sit inside a run. */
  isThinking: (block: any, index: number) => boolean
  /** Display name, summary, colour category and raw name of the running tool. */
  describe: (block: any) => { name: string; summary: string; category: string; toolName: string }
}

export interface ToolCallGroups {
  groups: ToolCallGroup[]
  /** Every index that belongs to a run → its run. */
  byIndex: Map<number, ToolCallGroup>
}

/**
 * A single step (one tool, or one thinking chip) is clearer left alone than
 * folded into "1 operation", so a run must hold at least this many steps before
 * it collapses. Thinking counts as a step: a run of "thinking → one tool" is a
 * run, and leaving the thinking chip outside would split the transcript again.
 */
export const MIN_GROUPED_STEPS = 2

export function buildToolCallGroups(blocks: any[], opts: ToolCallGroupOptions): ToolCallGroups {
  const groups: ToolCallGroup[] = []
  const byIndex = new Map<number, ToolCallGroup>()
  const list = Array.isArray(blocks) ? blocks : []

  let i = 0
  while (i < list.length) {
    const startsRun = opts.isTool(list[i], i) || opts.isThinking(list[i], i)
    if (!startsRun) {
      i++
      continue
    }

    const start = i
    const toolIndices: number[] = []
    const thinkingIndices: number[] = []
    while (i < list.length && (opts.isTool(list[i], i) || opts.isThinking(list[i], i))) {
      if (opts.isTool(list[i], i)) toolIndices.push(i)
      else thinkingIndices.push(i)
      i++
    }
    const end = i - 1

    if (toolIndices.length + thinkingIndices.length < MIN_GROUPED_STEPS) continue

    let failedCount = 0
    let interruptedCount = 0
    let runningIndex = 0
    let runningBlock: any = null
    toolIndices.forEach((idx, order) => {
      const block = list[idx]
      if (block?.status === 'error') failedCount++
      if (block?.status === TOOL_STATUS_INTERRUPTED) interruptedCount++
      if (block && !block.done) {
        runningIndex = order + 1
        runningBlock = block
      }
    })

    const described = runningBlock
      ? opts.describe(runningBlock)
      : { name: '', summary: '', category: '', toolName: '' }
    const group: ToolCallGroup = {
      start,
      end,
      toolCount: toolIndices.length,
      thinkingCount: thinkingIndices.length,
      failedCount,
      interruptedCount,
      runningIndex,
      runningThinking: thinkingIndices.some((idx) => list[idx] && !list[idx].done),
      runningCategory: described.category,
      runningToolName: described.toolName,
      runningName: described.name,
      runningSummary: described.summary,
    }
    groups.push(group)
    for (let idx = start; idx <= end; idx++) byIndex.set(idx, group)
  }

  return { groups, byIndex }
}
