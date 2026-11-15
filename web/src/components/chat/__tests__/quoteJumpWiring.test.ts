import { describe, expect, it, vi } from 'vitest'
import {
  canJumpQuoteToSource,
  jumpToQuoteSource,
  quoteJumpLocator,
  quoteJumperTable,
  type QuoteJumperContext,
} from '@/utils/quoteJump'
import { locatorOf, type QuoteItem } from '@/utils/quoteItem'

/**
 * The jump registry is the SINGLE answer to both "is there a jump button" and
 * "where does it go".
 *
 * Those used to be two separate `if` chains in two files — `canJumpToSource` in
 * `quoteItem.ts` decided visibility, `jumpToQuoteSource` in `ChatPanelContent.vue`
 * decided the destination — kept aligned by a test that compared the files as
 * text. A locator accepted by one and ignored by the other produced a button
 * that appeared and did nothing.
 *
 * These tests are behavioural now: they drive the real registry through a fake
 * context, so a locator can only be "supported" if a jumper actually consumes it.
 */

function quote(over: Partial<QuoteItem> = {}): QuoteItem {
  return {
    id: 'q1', text: 'x', note: '', filePath: '', language: '',
    startLine: 0, endLine: 0, sourceKind: 'file', ...over,
  }
}

/** A context that records what each jump did, so assertions read the EFFECT. */
function makeCtx(over: Partial<QuoteJumperContext> = {}) {
  const calls = {
    events: [] as Array<{ event: string; detail: Record<string, unknown> }>,
    urls: [] as string[],
    files: [] as Array<{ path: string; startLine: number; endLine: number }>,
    sessions: [] as Array<{ sessionId: string; messageId?: number }>,
    disabled: 0,
  }
  const ctx: QuoteJumperContext = {
    openFile: (path, startLine, endLine) => { calls.files.push({ path, startLine, endLine }) },
    openUrl: (url) => { calls.urls.push(url) },
    forgeEnabled: () => true,
    tasksEnabled: () => true,
    notifyDisabled: () => { calls.disabled++ },
    openSession: (sessionId, messageId) => { calls.sessions.push({ sessionId, messageId }) },
    dispatch: (event, detail) => { calls.events.push({ event, detail }) },
    ...over,
  }
  return { ctx, calls }
}

describe('quote jump registry', () => {
  it('offers the button exactly for the quotes it can actually jump', async () => {
    // Every entry that claims a locator must also perform a jump for it: if the
    // visibility answer and the destination disagree, this fails by name.
    const cases: Array<[string, QuoteItem, (calls: ReturnType<typeof makeCtx>['calls']) => void]> = [
      ['commit', quote({ locators: { commit: 'abc1234' } }), (c) => {
        expect(c.events).toEqual([{ event: 'navigate-to-commit', detail: { sha: 'abc1234' } }])
      }],
      ['task', quote({ locators: { task: '12' } }), (c) => {
        expect(c.events[0].event).toBe('clawbench-open-task')
        expect(c.events[0].detail).toMatchObject({ taskId: 12 })
      }],
      ['task + execution', quote({ locators: { task: '12', execution: 'exec-7' } }), (c) => {
        expect(c.events[0].detail).toMatchObject({ taskId: 12, executionId: 'exec-7' })
      }],
      ['meeting', quote({ locators: { meeting: 'tok-9' } }), (c) => {
        expect(c.events).toEqual([{ event: 'clawbench-open-meeting', detail: { token: 'tok-9' } }])
      }],
      ['mail', quote({ locators: { mail: '77' } }), (c) => {
        expect(c.events).toEqual([{ event: 'clawbench-open-mail', detail: { messageId: 77 } }])
      }],
      ['session + message', quote({ locators: { session: 'sess-1', message: '42' } }), (c) => {
        expect(c.sessions).toEqual([{ sessionId: 'sess-1', messageId: 42 }])
      }],
      ['session only', quote({ locators: { session: 'sess-1' } }), (c) => {
        expect(c.sessions).toEqual([{ sessionId: 'sess-1', messageId: undefined }])
      }],
      ['url', quote({ url: 'https://e.com/x', sourceKind: 'url' }), (c) => {
        expect(c.urls).toEqual(['https://e.com/x'])
      }],
      ['file', quote({ filePath: '/proj/a.go', startLine: 3, endLine: 9 }), (c) => {
        expect(c.files).toEqual([{ path: '/proj/a.go', startLine: 3, endLine: 9 }])
      }],
    ]

    for (const [name, q, assert] of cases) {
      const { ctx, calls } = makeCtx()
      expect(canJumpQuoteToSource(q), `${name} must be offered by canJumpQuoteToSource`).toBe(true)
      const handled = await jumpToQuoteSource(q, ctx)
      expect(handled, `${name} must be handled by a jumper`).toBe(true)
      assert(calls)
    }
  })

  // Most-specific-first: a git-diff quote carries BOTH a path and a commit, and
  // the user who selected a hunk means the commit.
  it('prefers the commit over the file path', async () => {
    const { ctx, calls } = makeCtx()
    await jumpToQuoteSource(quote({ locators: { commit: 'abc' }, filePath: '/a.go' }), ctx)

    expect(calls.events).toHaveLength(1)
    expect(calls.files).toHaveLength(0)
  })

  // A meeting quote whose token did not survive (an old row) must NOT fall
  // through to opening its human-readable label as a path: that would 404 and
  // look like a broken file.
  it('refuses a meeting quote with no token', async () => {
    const q = quote({ sourceKind: 'meeting', filePath: '产品评审会 · 纪要', language: 'meeting' })
    const { ctx, calls } = makeCtx()

    expect(canJumpQuoteToSource(q)).toBe(false)
    expect(await jumpToQuoteSource(q, ctx)).toBe(false)
    expect(calls.files).toHaveLength(0)
  })

  it('refuses a mail quote with no id', () => {
    expect(canJumpQuoteToSource(quote({ sourceKind: 'mail', filePath: '周报 · 张三' }))).toBe(false)
  })

  it('refuses a forge quote whose address was lost', async () => {
    const q = quote({ sourceKind: 'url', filePath: 'acme/widgets#7' })
    const { ctx, calls } = makeCtx()

    expect(canJumpQuoteToSource(q)).toBe(false)
    expect(await jumpToQuoteSource(q, ctx)).toBe(false)
    expect(calls.urls).toHaveLength(0)
  })

  it('refuses a bare selection and a chat quote with no session', () => {
    expect(canJumpQuoteToSource(quote({ sourceKind: 'selection', text: 'x' }))).toBe(false)
    expect(canJumpQuoteToSource(quote({ sourceKind: 'message', text: 'x' }))).toBe(false)
  })

  // A forge reference is not just a link: it points into the integration that
  // `forge.enabled` turns off. A plain external link is not the integration and
  // still opens.
  it('refuses a forge reference while forge is off, and only a forge reference', async () => {
    const forgeOff = makeCtx({ forgeEnabled: () => false })

    const issue = quote({ language: 'issue', url: 'https://github.com/a/b/issues/7', sourceKind: 'url' })
    expect(await jumpToQuoteSource(issue, forgeOff.ctx)).toBe(true)
    expect(forgeOff.calls.disabled).toBe(1)
    expect(forgeOff.calls.urls).toHaveLength(0)

    const plain = quote({ url: 'https://example.com/x', sourceKind: 'url' })
    expect(await jumpToQuoteSource(plain, forgeOff.ctx)).toBe(true)
    expect(forgeOff.calls.urls).toEqual(['https://example.com/x'])
  })

  // Same shape for tasks: the feature switch stops the jump, and it must not
  // fall through to opening the task NAME as a file path.
  it('refuses a task jump while tasks are off, without falling through to a file', async () => {
    const tasksOff = makeCtx({ tasksEnabled: () => false })
    const q = quote({ locators: { task: '12' }, filePath: '每日构建 (#12)' })

    expect(await jumpToQuoteSource(q, tasksOff.ctx)).toBe(true)
    expect(tasksOff.calls.disabled).toBe(1)
    expect(tasksOff.calls.files).toHaveLength(0)
  })
})

describe('quote jump registry — shape', () => {
  it('pins the ordering: specific locators before the url/file fallbacks', () => {
    const table = quoteJumperTable()
    const locators = table.map(t => t.locator)

    // The two `null`-free tail entries must stay last: `url` before the file
    // fallback, and the file fallback absolutely last.
    expect(locators[locators.length - 1]).toBeNull()
    expect(locators[locators.length - 2]).toBe('url')
    // Commit is the most specific, so it leads.
    expect(locators[0]).toBe('commit')
  })

  it('reports which locator drove the jump', () => {
    expect(quoteJumpLocator(quote({ locators: { commit: 'a' }, filePath: '/x' }))).toBe('commit')
    expect(quoteJumpLocator(quote({ locators: { meeting: 't' } }))).toBe('meeting')
    expect(quoteJumpLocator(quote({ url: 'https://e.com', sourceKind: 'url' }))).toBe('url')
    expect(quoteJumpLocator(quote({ filePath: '/x' }))).toBeNull()
  })

  // The bag is open, so an unknown key must not be mistaken for a known source.
  it('ignores a locator key it has no jumper for', async () => {
    const q = quote({ locators: { deploy: 'd-42' }, filePath: '/a.go' })
    const { ctx, calls } = makeCtx()

    expect(quoteJumpLocator(q)).toBeNull()
    expect(canJumpQuoteToSource(q)).toBe(true) // via the file fallback
    await jumpToQuoteSource(q, ctx)
    expect(calls.files).toHaveLength(1)
  })
})

describe('locatorOf', () => {
  it('reads a present key and returns "" for an absent one', () => {
    expect(locatorOf({ locators: { task: '12' } }, 'task')).toBe('12')
    expect(locatorOf({ locators: { task: '12' } }, 'mail')).toBe('')
    expect(locatorOf({}, 'task')).toBe('')
  })

  it('does not blow up on an empty bag', () => {
    expect(locatorOf({ locators: {} }, 'task')).toBe('')
  })
})

describe('jump context is supplied by the host, not imported', () => {
  it('never touches navigation singletons directly', async () => {
    // The registry must stay testable without the app: if it imported
    // useFilePathAnnotation / useMessageNavigation, driving it here would drag
    // in the whole session graph. A spy that records calls proves the effect
    // comes from the injected context.
    const spy = vi.fn()
    const { ctx } = makeCtx({ openUrl: spy })
    await jumpToQuoteSource(quote({ url: 'https://e.com', sourceKind: 'url' }), ctx)
    expect(spy).toHaveBeenCalledWith('https://e.com')
  })
})
