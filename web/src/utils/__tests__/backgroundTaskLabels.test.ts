import { describe, expect, it } from 'vitest'
import { backgroundTaskLabels, shellLaunchTaskID } from '@/utils/backgroundTaskLabels'
import { toolCardSummary } from '@/utils/toolCardSummary'

/**
 * Issue #682: a TaskOutput / TaskStop card must say WHICH background task it is
 * about. Its own input carries nothing but a random id ("oAVlC6"), while the
 * Bash card that launched the task — in the same message — has the agent's
 * description and its result announces that id.
 */

/** A launcher Bash block exactly as the backend stores/streams it. */
function launchBlock(id: string, description: string | undefined, taskId: string, command = 'go test ./...') {
  return {
    type: 'tool_use',
    name: 'Bash',
    id: `call_${id}`,
    input: { ...(description ? { description } : {}), command, run_in_background: true },
    output: `Status: Running in background with task_id: ${taskId}\nReason: Launched directly in background (run_in_background=true).`,
    done: true,
  }
}

function lookupBlock(name: string, taskId: string): { type: string; name: string; id: string; input: Record<string, unknown>; summary: string; done: boolean } {
  return { type: 'tool_use', name, id: `call_${name}_${taskId}`, input: { task_id: taskId, block: true }, summary: taskId, done: true }
}

describe('shellLaunchTaskID', () => {
  it('reads the id a launcher result announces', () => {
    expect(shellLaunchTaskID('Status: Running in background with task_id: fvW2XB\nReason: …')).toBe('fvW2XB')
  })

  it('only accepts the phrase at the start of the output', () => {
    // A multi-line result whose second line happens to contain the phrase plus an
    // id is NOT a launcher; accepting it would attach that task's description to
    // an unrelated lookup.
    expect(shellLaunchTaskID('Stdout: ok\nStatus: Running in background with task_id: abc')).toBe('')
  })

  it('ignores results that are not launchers', () => {
    expect(shellLaunchTaskID('Stdout: all good')).toBe('')
    expect(shellLaunchTaskID('')).toBe('')
    expect(shellLaunchTaskID(undefined)).toBe('')
    // A command that merely echoes the phrase mid-text must not be mistaken for
    // a launcher, or it would steal the real task's description.
    expect(shellLaunchTaskID('grep -n "Running in background with task_id:" log.txt')).toBe('')
  })
})

describe('backgroundTaskLabels', () => {
  it('labels the lookup with the launcher description and demotes the id', () => {
    const blocks = [launchBlock('a', '跑全量测试落盘并取汇总行', 'Ooj0bz'), lookupBlock('TaskOutput', 'Ooj0bz')]
    expect(backgroundTaskLabels(blocks).get('call_TaskOutput_Ooj0bz')).toBe('跑全量测试落盘并取汇总行 · Ooj0bz')
  })

  it('falls back to the command when the launcher has no description', () => {
    const blocks = [launchBlock('a', undefined, 'abc123', 'cd web && npx vitest run'), lookupBlock('TaskStop', 'abc123')]
    expect(backgroundTaskLabels(blocks).get('call_TaskStop_abc123')).toBe('cd web && npx vitest run · abc123')
  })

  it('pairs each lookup with its OWN task when several ran in one message', () => {
    const blocks = [
      launchBlock('a', '第一条命令', 'aaa111'),
      launchBlock('b', '第二条命令', 'bbb222'),
      lookupBlock('TaskOutput', 'bbb222'),
      lookupBlock('TaskOutput', 'aaa111'),
    ]
    const labels = backgroundTaskLabels(blocks)
    expect(labels.get('call_TaskOutput_bbb222')).toBe('第二条命令 · bbb222')
    expect(labels.get('call_TaskOutput_aaa111')).toBe('第一条命令 · aaa111')
  })

  it('leaves a lookup alone when its launcher is not in the message', () => {
    // Historical turns, or a lookup of a task started in an earlier message:
    // showing the bare id beats borrowing another task's command.
    const blocks = [launchBlock('a', '某条命令', 'aaa111'), lookupBlock('TaskOutput', 'zzz999')]
    expect(backgroundTaskLabels(blocks).size).toBe(0)
  })

  it('reads the id from the summary of a slim (reloaded) lookup block', () => {
    // What the DB actually stores for a plain tool pill: no input at all, only
    // the extracted summary — which for these tools IS the task id.
    const slim = { type: 'tool_use', name: 'TaskOutput', id: 'call_out', summary: 'Ooj0bz' }
    const labels = backgroundTaskLabels([launchBlock('a', '跑全量测试落盘并取汇总行', 'Ooj0bz'), slim])
    expect(labels.get('call_out')).toBe('跑全量测试落盘并取汇总行 · Ooj0bz')
  })

  it('does not treat a non-Bash tool as a launcher', () => {
    // Only Bash launches background tasks. Another tool whose result happens to
    // carry the same announcement line must not borrow its description.
    const other = { ...launchBlock('a', undefined, 'Ooj0bz', ''), name: 'OtherTool', id: 'call_other' }
    expect(backgroundTaskLabels([other, lookupBlock('TaskOutput', 'Ooj0bz')]).size).toBe(0)
  })

  it('does not emit a label when the launcher has neither description nor command', () => {
    // Otherwise the card would read " · Ooj0bz" — a label whose only content is
    // the separator.
    const bare = launchBlock('a', undefined, 'Ooj0bz', '')
    expect(backgroundTaskLabels([bare, lookupBlock('TaskOutput', 'Ooj0bz')]).size).toBe(0)
  })

  it('ignores non-tool blocks and a missing block list', () => {
    expect(backgroundTaskLabels(undefined).size).toBe(0)
    expect(backgroundTaskLabels([{ type: 'text', text: 'hi' }]).size).toBe(0)
  })
})

describe('toolCardSummary', () => {
  const summarize = (b: { summary?: string }) => b.summary || ''

  it('prefers the borrowed label over the id-only summary', () => {
    // Real Path: the lookup block is part of the same block list the labels
    // come from, and its own summary is still the bare id.
    const lookup = lookupBlock('TaskOutput', 'Ooj0bz')
    const labels = backgroundTaskLabels([launchBlock('a', '跑全量测试落盘并取汇总行', 'Ooj0bz'), lookup])
    expect(summarize(lookup)).toBe('Ooj0bz')
    expect(toolCardSummary(lookup, summarize, labels)).toBe('跑全量测试落盘并取汇总行 · Ooj0bz')
  })

  it('keeps the existing summary for every other tool', () => {
    const bash: { type: string; name: string; id: string; input: Record<string, unknown>; summary: string } = { type: 'tool_use', name: 'Bash', id: 'call_x', input: { command: 'ls' }, summary: '列目录' }
    expect(toolCardSummary(bash, summarize, backgroundTaskLabels([]))).toBe('列目录')
    // Even a lookup falls back when no label was borrowed.
    expect(toolCardSummary(lookupBlock('TaskOutput', 'Ooj0bz'), summarize, new Map())).toBe('Ooj0bz')
  })
})
