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
 * merge. A run therefore spans tool calls AND the collapsed thinking chips
 * between them; the whole run collapses into the single row.
 *
 * Pure function on purpose — the grouping rules are testable without mounting
 * the (very large) ContentBlocks component.
 */

export interface ToolCallGroup {
  /** Index of the first block of the run (a tool or a thinking chip). */
  start: number
  /** Index of the last block of the run. */
  end: number
  /** How many tool calls the run contains (thinking chips are not counted). */
  toolCount: number
  /** How many of those tools finished with `status === 'error'`. */
  failedCount: number
  /** 1-based position of the running tool inside the run; 0 when none runs. */
  runningIndex: number
  /** Display name of the running tool, e.g. "Bash". */
  runningName: string
  /** One-line summary of the running tool, e.g. "npm test". */
  runningSummary: string
}

export interface ToolCallGroupOptions {
  /** Whether this block is a tool call the user wants folded. */
  isTool: (block: any, index: number) => boolean
  /** Whether this block is a collapsed thinking chip that may sit inside a run. */
  isThinking: (block: any, index: number) => boolean
  /** Display name + summary for the running-tool line. */
  describe: (block: any) => { name: string; summary: string }
}

export interface ToolCallGroups {
  groups: ToolCallGroup[]
  /** Every index that belongs to a run → its run. */
  byIndex: Map<number, ToolCallGroup>
}

/**
 * A single tool call is clearer left alone than folded into "ran 1 operation",
 * so a run must hold at least this many tools before it collapses.
 */
export const MIN_GROUPED_TOOLS = 2

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
    while (i < list.length && (opts.isTool(list[i], i) || opts.isThinking(list[i], i))) {
      if (opts.isTool(list[i], i)) toolIndices.push(i)
      i++
    }
    const end = i - 1

    if (toolIndices.length < MIN_GROUPED_TOOLS) continue

    let failedCount = 0
    let runningIndex = 0
    let runningBlock: any = null
    toolIndices.forEach((idx, order) => {
      const block = list[idx]
      if (block?.status === 'error') failedCount++
      if (block && !block.done) {
        runningIndex = order + 1
        runningBlock = block
      }
    })

    const described = runningBlock ? opts.describe(runningBlock) : { name: '', summary: '' }
    const group: ToolCallGroup = {
      start,
      end,
      toolCount: toolIndices.length,
      failedCount,
      runningIndex,
      runningName: described.name,
      runningSummary: described.summary,
    }
    groups.push(group)
    for (let idx = start; idx <= end; idx++) byIndex.set(idx, group)
  }

  return { groups, byIndex }
}
