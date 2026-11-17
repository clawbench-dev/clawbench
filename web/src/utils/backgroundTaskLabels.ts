/**
 * Labels for the agent's background tasks (issue #682).
 *
 * Starting a background command and looking it up later are two DIFFERENT tool
 * calls that share nothing but a random id:
 *
 *   - Bash {run_in_background: true} — carries the human description; its
 *     result opens with "Status: Running in background with task_id: <id>".
 *   - TaskOutput / TaskStop {task_id: "<id>"} — carries only that id, so their
 *     card summary degraded to "oAVlC6" and the reader could not tell which
 *     background task the card was about (#645 gave them a readable TITLE, but
 *     the id still stood where the description belongs).
 *
 * While a turn streams, every block of the message is in hand, so the launcher
 * Bash block can be found by the id in its output and its description (or
 * command) borrowed as the summary. This module is the pure derivation for
 * that — no Vue, no store.
 */

/** Lowercased tool names whose summary is a background-task lookup. */
const BACKGROUND_TASK_LOOKUP_TOOLS = new Set(['taskoutput', 'taskstop'])

/**
 * A background-launching Bash result OPENS with this line (the agent's own
 * wording, kept verbatim); the task id follows the marker.
 */
const BACKGROUND_LAUNCH_LINE = /^Status: Running in background with task_id:\s*(\S+)/

/**
 * shellLaunchTaskID returns the background-task id a Bash RESULT announced, or
 * "" when that block is not a background launcher.
 *
 * Anchored to the START of the output: a command that merely echoes the phrase
 * further down (grep, cat of a log) is not a launcher and must not borrow — or
 * steal — another task's description.
 */
export function shellLaunchTaskID(output: unknown): string {
  if (typeof output !== 'string') return ''
  const m = BACKGROUND_LAUNCH_LINE.exec(output)
  return m ? m[1] : ''
}

/**
 * lookupTaskID returns the background-task id a TaskOutput/TaskStop block refers
 * to.
 *
 * Historical rows are stored slim (no `input`; only an extracted `summary`), and
 * nothing backfills the input of a plain tool pill — so the summary IS the id
 * there. Read both rather than trusting `input` alone, which would have made the
 * label unreachable for every message reloaded from the database.
 */
function lookupTaskID(block: Record<string, unknown>): string {
  const input = block.input as Record<string, unknown> | undefined
  if (typeof input?.task_id === 'string' && input.task_id) return input.task_id
  return typeof block.summary === 'string' ? block.summary.trim() : ''
}

/**
 * backgroundTaskLabels maps a TaskOutput/TaskStop block id to
 * "<description> · <task id>", borrowing the description from the Bash block
 * of the SAME message that launched that task.
 *
 * A lookup whose launcher is not in the message is left out: the card then
 * shows its bare id, which is what it showed before. Guessing from a different
 * task's command would be worse than saying nothing.
 */
export function backgroundTaskLabels(blocks: unknown): Map<string, string> {
  const labels = new Map<string, string>()
  if (!Array.isArray(blocks)) return labels

  const launched = new Map<string, string>()
  for (const b of blocks as Record<string, unknown>[]) {
    if (!b || b.type !== 'tool_use' || b.name !== 'Bash') continue
    const id = shellLaunchTaskID(b.output)
    if (!id) continue
    // Same priority the Bash card itself shows: the description the agent wrote
    // for the command, then the command itself when there is none.
    const input = b.input as Record<string, unknown> | undefined
    const desc = typeof input?.description === 'string' ? input.description.trim() : ''
    const cmd = typeof input?.command === 'string' ? input.command.trim() : ''
    const label = desc || cmd
    if (label) launched.set(id, label)
  }
  if (launched.size === 0) return labels

  for (const b of blocks as Record<string, unknown>[]) {
    if (!b || b.type !== 'tool_use' || !b.id) continue
    if (!BACKGROUND_TASK_LOOKUP_TOOLS.has(String(b.name || '').toLowerCase())) continue
    const taskId = lookupTaskID(b)
    const label = launched.get(taskId)
    if (label) labels.set(b.id as string, `${label} · ${taskId}`)
  }
  return labels
}
