import { describe, expect, it } from 'vitest'
import {
  QUOTE_DRAG_MIME,
  setQuoteDragData,
  readQuoteDragData,
  hasQuoteDragData,
  commitDragPayload,
  taskDragPayload,
  forgeItemDragPayload,
  pipelineDragPayload,
  forgeUnreadDragPayload,
} from '@/utils/quoteDrag'
import { resolveQuoteType } from '@/utils/quoteItem'

/** Minimal DataTransfer stand-in: the real one is not constructible in jsdom. */
function makeDT(initial: Record<string, string> = {}) {
  const store = new Map(Object.entries(initial))
  return {
    setData: (type: string, value: string) => { store.set(type, value) },
    getData: (type: string) => store.get(type) || '',
    get types() { return [...store.keys()] },
    _store: store,
  }
}

describe('quote drag payload round-trip', () => {
  it('writes and reads a payload', () => {
    const dt = makeDT()
    const payload = commitDragPayload({ sha: 'a1b2c3d4e5f6', msg: 'fix login' })!

    expect(setQuoteDragData(dt as unknown as DataTransfer, payload)).toBe(true)
    expect(readQuoteDragData(dt as unknown as DataTransfer)).toEqual(payload)
  })

  it('reports the payload as present so dragover can allow the drop', () => {
    const dt = makeDT()
    expect(hasQuoteDragData(dt as unknown as DataTransfer)).toBe(false)

    setQuoteDragData(dt as unknown as DataTransfer, taskDragPayload({ id: 3, name: 'Nightly' })!)

    expect(hasQuoteDragData(dt as unknown as DataTransfer)).toBe(true)
  })

  it('ignores a malformed payload rather than throwing', () => {
    const dt = makeDT({ [QUOTE_DRAG_MIME]: '{not json' })
    expect(readQuoteDragData(dt as unknown as DataTransfer)).toBeNull()
  })

  it('rejects a payload with no label (nothing to render on the card)', () => {
    const dt = makeDT({ [QUOTE_DRAG_MIME]: JSON.stringify({ filePath: '', text: '' }) })
    expect(readQuoteDragData(dt as unknown as DataTransfer)).toBeNull()
  })

  it('returns null for a foreign drag', () => {
    const dt = makeDT({ 'text/plain': 'hello' })
    expect(readQuoteDragData(dt as unknown as DataTransfer)).toBeNull()
    expect(hasQuoteDragData(dt as unknown as DataTransfer)).toBe(false)
  })

  it('tolerates a missing dataTransfer', () => {
    expect(setQuoteDragData(null, commitDragPayload({ sha: 'abc1234' })!)).toBe(false)
    expect(readQuoteDragData(null)).toBeNull()
    expect(hasQuoteDragData(null)).toBe(false)
  })

  // The payload must never look like a file attachment: an empty path in the
  // send's filePaths 404s the whole send (a bug this codebase has hit before).
  it('never writes the attach MIME', () => {
    const dt = makeDT()
    setQuoteDragData(dt as unknown as DataTransfer, taskDragPayload({ id: 1, name: 'T' })!)

    expect(dt._store.has('application/x-clawbench-attach')).toBe(false)
    expect(dt._store.get('text/plain')).toBe('T')
  })
})

describe('commitDragPayload', () => {
  it('carries the sha as the locator so the card can jump back to it', () => {
    const p = commitDragPayload({ sha: 'a1b2c3d4e5f6a7b8', msg: 'fix login' })!

    expect(p.commitSha).toBe('a1b2c3d4e5f6a7b8')
    expect(p.text).toBe('')
    expect(p.filePath).toBe('a1b2c3d fix login')
    expect(resolveQuoteType(p)).toBe('diff')
  })

  it('labels a message-less commit with just the short sha', () => {
    expect(commitDragPayload({ sha: 'a1b2c3d4' })!.filePath).toBe('a1b2c3d')
  })

  // A commit drag must not be mistaken for a pipeline: resolveQuoteType treats
  // commitSha + url as a pipeline, so a commit must carry no url.
  it('carries no url, so it is not resolved as a pipeline', () => {
    const p = commitDragPayload({ sha: 'a1b2c3d4', msg: 'x' })!

    expect(p.url).toBeUndefined()
    expect(resolveQuoteType(p)).not.toBe('pipeline')
  })

  it('refuses a commit with no sha (the working-tree row)', () => {
    expect(commitDragPayload({ sha: '' })).toBeNull()
  })
})

describe('taskDragPayload', () => {
  it('carries the task id as the locator', () => {
    const p = taskDragPayload({ id: 42, name: 'Nightly build' })!

    expect(p.taskId).toBe(42)
    expect(p.filePath).toBe('Nightly build')
    expect(resolveQuoteType(p)).toBe('task')
  })

  it('falls back to the id when the task has no name', () => {
    expect(taskDragPayload({ id: 7 })!.filePath).toBe('#7')
  })

  it('refuses a task with no id', () => {
    expect(taskDragPayload({ id: 0, name: 'x' })).toBeNull()
  })
})

describe('forgeItemDragPayload', () => {
  it('carries the url and the issue type', () => {
    const p = forgeItemDragPayload({
      type: 'issue', number: 12, title: 'Crash', url: 'https://x/1', slug: 'o/r',
    })!

    expect(p.url).toBe('https://x/1')
    expect(p.filePath).toBe('o/r#12')
    expect(resolveQuoteType(p)).toBe('issue')
  })

  it('marks a pull request as a PR, not an issue', () => {
    const p = forgeItemDragPayload({ type: 'pr', number: 3, url: 'https://x/3', slug: 'o/r' })!

    expect(p.language).toBe('pr')
    expect(resolveQuoteType(p)).toBe('pr')
  })

  it('falls back to the title when there is no slug/number', () => {
    expect(forgeItemDragPayload({ title: 'Something', url: 'https://x' })!.filePath).toBe('Something')
  })

  it('refuses an item with nothing to label it', () => {
    expect(forgeItemDragPayload({ url: 'https://x' })).toBeNull()
  })
})

describe('pipelineDragPayload', () => {
  it('names the run rather than using slug#number (which reads as a PR)', () => {
    const p = pipelineDragPayload({
      number: 7, name: 'CI', url: 'https://x/run/7', slug: 'o/r', sha: 'deadbeef',
    })!

    expect(p.filePath).toBe('CI #7')
    expect(p.language).toBe('pipeline')
  })

  // A pipeline is distinguished from a plain commit by carrying BOTH an address
  // and the commit it built — that is exactly what resolveQuoteType keys on.
  it('carries both the address and the commit, resolving as a pipeline', () => {
    const p = pipelineDragPayload({ number: 7, name: 'CI', url: 'https://x/7', sha: 'deadbeef' })!

    expect(p.url).toBe('https://x/7')
    expect(p.commitSha).toBe('deadbeef')
    expect(resolveQuoteType(p)).toBe('pipeline')
  })

  it('omits the sha when the run does not report one', () => {
    const p = pipelineDragPayload({ number: 7, name: 'CI', url: 'https://x/7' })!

    expect(p.commitSha).toBeUndefined()
    // Still a pipeline: the explicit language marker is authoritative, so a run
    // that reports no sha does not silently degrade into a plain link.
    expect(resolveQuoteType(p)).toBe('pipeline')
  })

  it('refuses a run with no name and no slug', () => {
    expect(pipelineDragPayload({ number: 7 })).toBeNull()
  })
})

describe('forgeUnreadDragPayload', () => {
  it('builds an issue payload from an activity row', () => {
    const p = forgeUnreadDragPayload({
      type: 'issue', number: 5, url: 'https://x/5', slug: 'o/r',
    })!

    expect(p.filePath).toBe('o/r#5')
    expect(resolveQuoteType(p)).toBe('issue')
  })

  it('addresses a pipeline by run id, since its number is always 0', () => {
    const p = forgeUnreadDragPayload({
      type: 'pipeline', number: 0, runId: 99, url: 'https://x/99', slug: 'o/r',
    })!

    expect(p.filePath).toBe('o/r #99')
    expect(resolveQuoteType(p)).toBe('pipeline')
  })

  // The row's VISIBLE label appends the unread reason ("· merged"), which is
  // transient notification state — baking it into a card would outlive it.
  it('does not bake the unread reason into the label', () => {
    const p = forgeUnreadDragPayload({ type: 'pr', number: 2, url: 'https://x/2', slug: 'o/r' })!

    expect(p.filePath).toBe('o/r#2')
    expect(p.filePath).not.toContain('·')
  })

  it('refuses an issue row with no number', () => {
    expect(forgeUnreadDragPayload({ type: 'issue', number: 0, slug: 'o/r' })).toBeNull()
  })
})
