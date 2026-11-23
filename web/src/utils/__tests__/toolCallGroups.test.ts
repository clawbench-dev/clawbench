import { describe, expect, it } from 'vitest'
import { buildToolCallGroups, MIN_GROUPED_STEPS } from '@/utils/toolCallGroups'

const tool = (id: string, extra: Record<string, any> = {}) => ({ type: 'tool_use', id, done: true, status: 'success', ...extra })
const thinking = (id: string) => ({ type: 'thinking', think_id: id, done: true })
const text = (t: string) => ({ type: 'text', text: t })

const opts = {
  isTool: (b: any) => b?.type === 'tool_use',
  isThinking: (b: any) => b?.type === 'thinking',
  describe: (b: any) => ({ name: b.id, summary: '', category: b.category ?? 'fallback', toolName: b.name ?? b.id }),
}

describe('buildToolCallGroups', () => {
  it('folds consecutive tool calls into one run', () => {
    const { groups, byIndex } = buildToolCallGroups([tool('a'), tool('b'), tool('c')], opts)
    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({ start: 0, end: 2, toolCount: 3, thinkingCount: 0, failedCount: 0, runningIndex: 0 })
    expect([...byIndex.keys()].sort()).toEqual([0, 1, 2])
  })

  it('spans the thinking chips between tool calls and counts them', () => {
    // Real turns arrive as thinking, tool, tool, thinking, tool, … — requiring
    // strictly adjacent tool blocks would leave nothing to fold.
    const { groups } = buildToolCallGroups([thinking('t1'), tool('a'), thinking('t2'), tool('b')], opts)
    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({ start: 0, end: 3, toolCount: 2, thinkingCount: 2 })
  })

  it('folds thinking into a run even before a second tool arrives', () => {
    // thinking + one tool is already two steps: leaving the chip outside would
    // split one stretch of work into chip / pill / chip.
    const { groups } = buildToolCallGroups([thinking('t1'), tool('a')], opts)
    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({ toolCount: 1, thinkingCount: 1 })
  })

  it('leaves a single step alone', () => {
    expect(MIN_GROUPED_STEPS).toBe(2)
    expect(buildToolCallGroups([tool('a')], opts).groups).toHaveLength(0)
    expect(buildToolCallGroups([thinking('t1')], opts).groups).toHaveLength(0)
  })

  it('breaks the run on a text block', () => {
    const { groups } = buildToolCallGroups([tool('a'), tool('b'), text('done'), tool('c'), tool('d')], opts)
    expect(groups).toHaveLength(2)
    expect(groups.map((g) => [g.start, g.end])).toEqual([[0, 1], [3, 4]])
  })

  it('counts failed tools without excluding them', () => {
    const { groups } = buildToolCallGroups([tool('a'), tool('b', { status: 'error' }), tool('c', { status: 'error' })], opts)
    expect(groups[0]).toMatchObject({ toolCount: 3, failedCount: 2 })
  })

  it('counts interrupted tools separately from failures', () => {
    // Nothing failed here — the turn stopped waiting for them. The strip must
    // not report them as failures, and must not report them as success either.
    const { groups } = buildToolCallGroups(
      [tool('a'), tool('b', { status: 'interrupted' }), tool('c', { status: 'error' })],
      opts,
    )
    expect(groups[0]).toMatchObject({ toolCount: 3, failedCount: 1, interruptedCount: 1 })
  })

  it('reports the last running tool with its 1-based position', () => {
    const { groups } = buildToolCallGroups(
      [tool('a'), tool('b', { done: false, status: '' }), tool('c', { done: false, status: '' })],
      opts,
    )
    expect(groups[0]).toMatchObject({ runningIndex: 3, runningName: 'c' })
  })

  it('carries the running tool category, and none once the run settles', () => {
    // The summary row wears the running tool's accent, so the category has to
    // travel with the group — and it must be empty when nothing runs, otherwise
    // a finished run would stay tinted with whatever ran last.
    const running = buildToolCallGroups(
      [tool('a', { category: 'file' }), tool('b', { category: 'bash', name: 'Bash', done: false, status: '' })],
      opts,
    )
    expect(running.groups[0].runningCategory).toBe('bash')
    // The raw name travels too: the row's leading icon is resolved from it, so
    // it must be the tool that is running (not the display name, not a generic
    // run glyph).
    expect(running.groups[0].runningToolName).toBe('Bash')

    const settled = buildToolCallGroups([tool('a', { category: 'file' }), tool('b', { category: 'bash' })], opts)
    expect(settled.groups[0].runningIndex).toBe(0)
    expect(settled.groups[0].runningCategory).toBe('')
    expect(settled.groups[0].runningToolName).toBe('')
  })

  it('ignores blocks the caller does not classify as foldable', () => {
    // An AskUserQuestion card is never foldable — it must not be counted, and
    // it breaks the run into two (each of which still needs MIN_GROUPED_STEPS).
    const isTool = (b: any) => b?.type === 'tool_use' && b.id !== 'ask'
    const { groups } = buildToolCallGroups(
      [tool('a'), tool('b'), tool('ask'), tool('c'), tool('d')],
      { ...opts, isTool },
    )
    expect(groups).toHaveLength(2)
    expect(groups.map((g) => [g.start, g.end, g.toolCount])).toEqual([[0, 1, 2], [3, 4, 2]])
  })

  it('returns nothing for an empty or non-array input', () => {
    expect(buildToolCallGroups([], opts).groups).toEqual([])
    expect(buildToolCallGroups(null as any, opts).groups).toEqual([])
  })

  describe('lastStepDetail', () => {
    // Folded into the group so the transcript row does not rescan the run on
    // every streamed frame (it used to, twice per frame: once for the v-if and
    // once for the text).
    const withSummary = {
      ...opts,
      displayName: (b: any) => `Display:${b.id}`,
      summarize: (b: any) => b.summary ?? '',
    }

    it('reports the most recent step that HAS a summary', () => {
      const { groups } = buildToolCallGroups(
        [
          tool('a', { summary: 'run.go' }),
          tool('b', { summary: 'npm test' }),
          tool('c', { summary: '' }),
        ],
        withSummary,
      )
      expect(groups[0].lastStepDetail).toBe('Display:b · npm test')
    })

    it('falls back to the summary alone when the block has no display name', () => {
      const { groups } = buildToolCallGroups(
        [tool('a', { summary: 'one' }), tool('b', { summary: 'two' })],
        { ...withSummary, displayName: () => '' },
      )
      expect(groups[0].lastStepDetail).toBe('two')
    })

    it('skips thinking chips and is empty when no step has a summary', () => {
      const { groups } = buildToolCallGroups(
        [tool('a'), thinking('t1'), tool('b')],
        withSummary,
      )
      expect(groups[0].lastStepDetail).toBe('')
    })

    it('still resolves one when the caller supplies only `describe`', () => {
      // `describe` is the pre-existing option; a caller that never passes the
      // new namers must keep a working (if empty) line rather than throw.
      const { groups } = buildToolCallGroups([tool('a'), tool('b')], opts)
      expect(groups[0].lastStepDetail).toBe('')
    })
  })

  describe('runningDetail', () => {
    // The builtin agent announces a tool by name and delivers its arguments
    // afterwards, so the live row is built before the summary exists. The
    // group is NOT rebuilt when the summary lands (nothing structural
    // changed), so reading this at build time pinned the row to "" for the
    // whole execution — the reported bug.
    const withSummary = {
      ...opts,
      displayName: (b: any) => `Display:${b.id}`,
      summarize: (b: any) => b.summary ?? '',
    }

    it('picks up a summary that lands AFTER the group was built', () => {
      const running = tool('b', { done: false })
      const { groups } = buildToolCallGroups([tool('a'), running], withSummary)

      // Born before the arguments arrived: nothing to show yet.
      expect(groups[0].runningDetail()).toBe('')
      // `runningSummary`, the build-time copy, stays empty — that is exactly
      // why the row could not use it.
      expect(groups[0].runningSummary).toBe('')

      // The description arrives on the SAME block object (the stream mutates
      // it in place), and no group rebuild happens.
      running.summary = '搜索胶囊相关组件'
      expect(groups[0].runningDetail()).toBe('搜索胶囊相关组件')
    })

    it('carries no name prefix — the live row already prints the tool name', () => {
      // Unlike `lastStepDetail` ("Display:b · now"), which labels a settled
      // run's last step. The live row mirrors that tool's own pill, whose name
      // sits on the left already.
      const running = tool('b', { done: false })
      const { groups } = buildToolCallGroups([tool('a'), running], withSummary)
      running.summary = 'now'
      expect(groups[0].runningDetail()).toBe('now')
      expect(groups[0].runningDetail()).not.toContain('Display:b')
    })

    it('reads only the running step, not an earlier finished one', () => {
      const running = tool('b', { done: false })
      const { groups } = buildToolCallGroups(
        [tool('a', { summary: 'earlier' }), running],
        withSummary,
      )
      expect(groups[0].runningDetail()).toBe('')
      running.summary = 'now'
      expect(groups[0].runningDetail()).toBe('now')
    })

    it('is empty when nothing is running', () => {
      const { groups } = buildToolCallGroups([tool('a', { summary: 'x' }), tool('b', { summary: 'y' })], withSummary)
      expect(groups[0].runningDetail()).toBe('')
    })
  })
})
