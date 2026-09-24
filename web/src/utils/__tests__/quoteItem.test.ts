import { describe, expect, it } from 'vitest'
import { fromStagedQuote, fromFileEntry, toFileEntry, materializeQuotes, quoteItemFromTarget, quoteLabel, quoteLineRange, canJumpToSource, isQuoteFileEntry, resolveQuoteType, type QuoteItem } from '@/utils/quoteItem.ts'

describe('fromStagedQuote', () => {
  it('maps a file quote and infers sourceKind=file', () => {
    const got = fromStagedQuote({
      id: 'q1', text: 'x := 1', filePath: 'src/a.go', language: 'go',
      startLine: 10, endLine: 20, note: 'why?',
    })

    expect(got).toMatchObject({
      id: 'q1', text: 'x := 1', filePath: 'src/a.go', language: 'go',
      startLine: 10, endLine: 20, note: 'why?', sourceKind: 'file',
    })
  })

  it('infers sourceKind=url when a url is present', () => {
    const got = fromStagedQuote({
      id: 'q2', text: 'body', filePath: 'acme/widgets#7', language: 'issue',
      startLine: 0, endLine: 0, url: 'https://github.com/acme/widgets/issues/7',
    })

    expect(got.sourceKind).toBe('url')
    expect(got.url).toBe('https://github.com/acme/widgets/issues/7')
  })

  it('infers sourceKind=message when there is neither path nor url', () => {
    // A quote taken from a chat message has no file and no address.
    const got = fromStagedQuote({
      id: 'q3', text: 'chat text', filePath: '', language: '', startLine: 0, endLine: 0, messageId: 42,
    })

    expect(got.sourceKind).toBe('message')
    expect(got.messageId).toBe(42)
  })

  it('honours an explicit sourceKind over inference', () => {
    const got = fromStagedQuote({
      id: 'q4', text: 't', filePath: 'src/a.go', language: 'go',
      startLine: 1, endLine: 1, sourceKind: 'message',
    })
    expect(got.sourceKind).toBe('message')
  })

  it('defaults note to an empty string when absent', () => {
    const got = fromStagedQuote({ id: 'q5', text: 't', filePath: 'a', language: '', startLine: 0, endLine: 0 })
    expect(got.note).toBe('')
  })
})

describe('fromFileEntry', () => {
  it('maps a persisted quote entry', () => {
    const got = fromFileEntry({
      path: 'src/a.go', kind: 'quote', id: 'q1', text: 'x := 1',
      note: 'why?', language: 'go', startLine: 3, endLine: 3,
    })

    expect(got).toMatchObject({
      id: 'q1', text: 'x := 1', note: 'why?', filePath: 'src/a.go',
      language: 'go', startLine: 3, endLine: 3, sourceKind: 'file',
    })
  })

  it('maps a chat-message quote with an empty path', () => {
    const got = fromFileEntry({ path: '', kind: 'quote', id: 'q2', text: 'chat text' })

    expect(got.sourceKind).toBe('message')
    expect(got.filePath).toBe('')
  })
})

describe('toFileEntry', () => {
  it('produces a kind=quote entry carrying the payload and id', () => {
    const item: QuoteItem = {
      id: 'q1', text: 'x := 1', note: 'why?', filePath: 'src/a.go',
      language: 'go', startLine: 10, endLine: 20, sourceKind: 'file',
    }

    expect(toFileEntry(item)).toEqual({
      path: 'src/a.go', kind: 'quote', id: 'q1', text: 'x := 1',
      note: 'why?', language: 'go', startLine: 10, endLine: 20,
      // Persisted so a reloaded quote keeps its real source. It cannot be
      // re-derived: a 'selection' quote has no url and no path, exactly like a
      // 'message' one.
      sourceKind: 'file',
    })
  })

  it('omits zero line numbers so they are not sent as 0', () => {
    const item: QuoteItem = {
      id: 'q2', text: 'chat', note: '', filePath: '', language: '',
      startLine: 0, endLine: 0, sourceKind: 'message',
    }

    const entry = toFileEntry(item)
    expect(entry.startLine).toBeUndefined()
    expect(entry.endLine).toBeUndefined()
    expect(entry.kind).toBe('quote')
  })

  it('carries a forge url through', () => {
    const item: QuoteItem = {
      id: 'q3', text: 'body', note: '', filePath: 'acme/widgets#7', language: 'issue',
      startLine: 0, endLine: 0, url: 'https://github.com/acme/widgets/issues/7', sourceKind: 'url',
    }

    expect(toFileEntry(item).url).toBe('https://github.com/acme/widgets/issues/7')
  })

  it('round-trips staged -> entry -> item', () => {
    const staged = {
      id: 'q1', text: 'x := 1', filePath: 'src/a.go', language: 'go',
      startLine: 10, endLine: 20, note: 'why?',
    }
    const roundTripped = fromFileEntry(toFileEntry(fromStagedQuote(staged)))

    expect(roundTripped.text).toBe('x := 1')
    expect(roundTripped.note).toBe('why?')
    expect(roundTripped.filePath).toBe('src/a.go')
    expect(roundTripped.startLine).toBe(10)
    expect(roundTripped.endLine).toBe(20)
    expect(roundTripped.sourceKind).toBe('file')
  })

  // The regression this guards: a terminal quote ('selection') has no url and
  // no path, exactly like a quote taken from a chat message. Without persisting
  // the kind, inference would label it 'message' after a reload and the drawer
  // would claim the user quoted a chat message they never quoted.
  it('round-trips a selection quote without degrading it to a chat message', () => {    const item: QuoteItem = {
      id: 'q9', text: 'npm run build', note: '', filePath: '', language: '',
      startLine: 0, endLine: 0, sourceKind: 'selection',
    }

    const entry = toFileEntry(item)
    expect(entry.sourceKind).toBe('selection')

    const back = fromFileEntry(entry)
    expect(back.sourceKind).toBe('selection')
    // And inference alone would NOT have produced this — proving the field is
    // what carries the meaning.
    expect(fromFileEntry({ path: '', kind: 'quote', text: 'npm run build' }).sourceKind).toBe('message')
  })

  it('keeps the payload of a selection quote intact through the round trip', () => {
    const item: QuoteItem = {
      id: 'q10', text: 'line one\nline two', note: 'note', filePath: '', language: '',
      startLine: 0, endLine: 0, sourceKind: 'selection',
    }
    const back = fromFileEntry(toFileEntry(item))

    expect(back.text).toBe('line one\nline two')
    expect(back.note).toBe('note')
    expect(back.id).toBe('q10')
  })

  // Every source locator must survive the round trip. These are the ONLY route
  // back to the origin (the quoted text carries no trace of them), so losing one
  // makes the quote silently unjumpable — no error, the button just does nothing.
  it('round-trips every source locator', () => {
    const item: QuoteItem = {
      id: 'q11', text: '构建失败', note: '', filePath: '每日构建 (#12)', language: '',
      startLine: 0, endLine: 0, sourceKind: 'file',
      commitSha: 'a1b2c3d4e5', taskId: 12, sessionId: 'sess-abc',
      messageId: 42, executionId: 'exec-7',
    }

    const entry = toFileEntry(item)
    expect(entry.commitSha).toBe('a1b2c3d4e5')
    expect(entry.taskId).toBe(12)
    expect(entry.sessionId).toBe('sess-abc')
    expect(entry.messageId).toBe(42)
    expect(entry.executionId).toBe('exec-7')

    const back = fromFileEntry(entry)
    expect(back.commitSha).toBe('a1b2c3d4e5')
    expect(back.taskId).toBe(12)
    expect(back.sessionId).toBe('sess-abc')
    expect(back.messageId).toBe(42)
    expect(back.executionId).toBe('exec-7')
  })

  it('omits absent locators rather than writing undefined keys', () => {
    const item: QuoteItem = {
      id: 'q12', text: 'x', note: '', filePath: 'a.go', language: 'go',
      startLine: 0, endLine: 0, sourceKind: 'file',
    }
    const entry = toFileEntry(item)

    expect('commitSha' in entry).toBe(false)
    expect('taskId' in entry).toBe(false)
    expect('sessionId' in entry).toBe(false)
    expect('messageId' in entry).toBe(false)
    expect('executionId' in entry).toBe(false)
  })
})

describe('quoteLabel', () => {
  it('uses the basename for a file quote', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'src/utils/a.go', language: '', startLine: 0, endLine: 0, sourceKind: 'file' }
    expect(quoteLabel(q)).toBe('a.go')
  })

  it('uses the label verbatim for a forge quote', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'acme/widgets#7', language: '', startLine: 0, endLine: 0, sourceKind: 'url' }
    expect(quoteLabel(q)).toBe('acme/widgets#7')
  })

  it('is empty for a chat quote', () => {
    const q: QuoteItem = { id: '1', text: 'chat', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'message' }
    expect(quoteLabel(q)).toBe('')
  })
})

describe('quoteLineRange', () => {
  it('formats a single line', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'a', language: '', startLine: 10, endLine: 10, sourceKind: 'file' }
    expect(quoteLineRange(q)).toBe(':10')
  })

  it('formats a range', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'a', language: '', startLine: 10, endLine: 20, sourceKind: 'file' }
    expect(quoteLineRange(q)).toBe(':10-20')
  })

  it('is empty with no line info', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'a', language: '', startLine: 0, endLine: 0, sourceKind: 'file' }
    expect(quoteLineRange(q)).toBe('')
  })
})

describe('canJumpToSource', () => {
  it('allows a file quote', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'src/a.go', language: '', startLine: 1, endLine: 1, sourceKind: 'file' }
    expect(canJumpToSource(q)).toBe(true)
  })

  it('allows a forge quote with an address', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'acme#7', language: '', startLine: 0, endLine: 0, url: 'https://e.com', sourceKind: 'url' }
    expect(canJumpToSource(q)).toBe(true)
  })

  it('refuses a chat-message quote — there is nothing to open', () => {
    const q: QuoteItem = { id: '1', text: 'chat', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'message' }
    expect(canJumpToSource(q)).toBe(false)
  })

  // A terminal selection has no file and no address, so there is nothing to
  // open. The drawer must not offer a button that does nothing.
  it('refuses a free-form selection quote', () => {
    const q: QuoteItem = { id: '1', text: 'npm run build', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'selection' }
    expect(canJumpToSource(q)).toBe(false)
  })

  it('refuses a forge quote with no address', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'acme#7', language: '', startLine: 0, endLine: 0, sourceKind: 'url' }
    expect(canJumpToSource(q)).toBe(false)
  })

  // Each locator alone is enough to offer the jump — jumpToQuoteSource has a
  // branch for it, so hiding the button here would strand a quote that could in
  // fact be opened.
  it('allows a commit quote', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'file', commitSha: 'abc123' }
    expect(canJumpToSource(q)).toBe(true)
  })

  it('allows a task quote', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'selection', taskId: 12 }
    expect(canJumpToSource(q)).toBe(true)
  })

  // A chat quote is only jumpable when it carries a session id; without one
  // there is no session to reopen, so the button stays hidden.
  it('allows a chat quote that carries a session id', () => {
    const q: QuoteItem = { id: '1', text: 'chat', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'message', sessionId: 'sess-abc', messageId: 42 }
    expect(canJumpToSource(q)).toBe(true)
  })

  it('still refuses a chat quote with no session id', () => {
    const q: QuoteItem = { id: '1', text: 'chat', note: '', filePath: '', language: '', startLine: 0, endLine: 0, sourceKind: 'message' }
    expect(canJumpToSource(q)).toBe(false)
  })
})

describe('isQuoteFileEntry', () => {
  it('is true only for kind=quote', () => {
    expect(isQuoteFileEntry({ path: '', kind: 'quote' })).toBe(true)
    expect(isQuoteFileEntry({ path: 'x', kind: 'url', url: 'https://e.com' })).toBe(false)
    expect(isQuoteFileEntry({ path: '/a.go' })).toBe(false)
  })
})

describe('materializeQuotes', () => {
  it('turns staged quotes into kind=quote entries', () => {
    // Both send paths (direct and enqueue) go through this one helper, so a
    // regression here drops quotes on BOTH — and the enqueue path is the one
    // that used to rely on the quotes being baked into the message text.
    const got = materializeQuotes([
      { id: 'q1', text: 'x := 1', filePath: 'src/a.go', language: 'go', startLine: 10, endLine: 20, note: 'why?' },
      { id: 'q2', text: 'chat text', filePath: '', language: '', startLine: 0, endLine: 0, messageId: 42 },
    ])

    expect(got).toHaveLength(2)
    expect(got[0]).toMatchObject({ kind: 'quote', id: 'q1', text: 'x := 1', note: 'why?', startLine: 10, endLine: 20 })
    expect(got[1]).toMatchObject({ kind: 'quote', id: 'q2', text: 'chat text', path: '' })
  })

  it('returns an empty array for no quotes', () => {
    expect(materializeQuotes([])).toEqual([])
  })
})

describe('quoteItemFromTarget', () => {
  it('builds a whole-file quote with no content', () => {
    // The entry-point buttons reference the object; they do not inline content
    // the user never selected.
    const got = quoteItemFromTarget({ filePath: '/proj/src/main.ts', label: 'main.ts' })

    expect(got).toMatchObject({
      filePath: '/proj/src/main.ts', text: '', note: '', sourceKind: 'file',
    })
    expect(got.url).toBeUndefined()
  })

  it('builds a whole-issue quote carrying the address', () => {
    const got = quoteItemFromTarget({
      url: 'https://github.com/acme/widgets/issues/7', label: 'acme/widgets#7',
    })

    expect(got).toMatchObject({
      filePath: 'acme/widgets#7', url: 'https://github.com/acme/widgets/issues/7',
      text: '', sourceKind: 'url',
    })
  })

  it('falls back to the label when there is no path', () => {
    const got = quoteItemFromTarget({ label: 'acme/widgets#7' })
    expect(got.filePath).toBe('acme/widgets#7')
  })

  it('survives the round trip to a send entry', () => {
    // The whole point of the empty text: it must reach the backend as an entry
    // with a path and no content, so the prompt renders a path reference rather
    // than an empty fence.
    const entry = toFileEntry(quoteItemFromTarget({ filePath: '/proj/src/main.ts' }))

    expect(entry.kind).toBe('quote')
    expect(entry.path).toBe('/proj/src/main.ts')
    expect(entry.text).toBe('')
    expect(entry.startLine).toBeUndefined()
  })
})

describe('resolveQuoteType', () => {
  function q(over: Partial<QuoteItem> = {}): QuoteItem {
    return {
      id: 'q1', text: 'x', note: '', filePath: '', language: '',
      startLine: 0, endLine: 0, sourceKind: 'file', ...over,
    }
  }

  // The surface's own marker states the intent, so it wins over inference.
  it.each([
    ['pipeline', 'pipeline'],
    ['task-exec', 'exec'],
    ['task', 'task'],
    ['diff', 'diff'],
    ['pr', 'pr'],
    ['issue', 'issue'],
  ])('honours the %s type marker', (language, expected) => {
    expect(resolveQuoteType(q({ language }))).toBe(expected)
  })

  // A real code fence language is not a type marker, so it falls through to the
  // locator/path logic rather than being misread as a type.
  it('ignores a real code language', () => {
    expect(resolveQuoteType(q({ language: 'go', filePath: 'src/a.go' }))).toBe('file')
  })

  // Most-specific-first: a git-diff quote has BOTH a path and a commit, and the
  // user who selected a hunk means the commit.
  it('prefers the commit over the file path', () => {
    expect(resolveQuoteType(q({ commitSha: 'abc123', filePath: 'src/a.go', sourceKind: 'file' }))).toBe('diff')
  })

  // A CI run carries its address AND the commit it built; a git diff only has
  // the commit. That is the only thing separating them.
  it('calls a commit with an address a pipeline run', () => {
    expect(resolveQuoteType(q({ commitSha: 'abc123', url: 'https://ci/run/1', sourceKind: 'url' }))).toBe('pipeline')
  })

  it('prefers the execution over the task', () => {
    expect(resolveQuoteType(q({ executionId: 'exec-1', taskId: 12 }))).toBe('exec')
  })

  it('resolves a task quote', () => {
    expect(resolveQuoteType(q({ taskId: 12, filePath: '每日构建 (#12)' }))).toBe('task')
  })

  it('falls back to a generic link for a bare url', () => {
    expect(resolveQuoteType(q({ url: 'https://example.com', sourceKind: 'url' }))).toBe('link')
  })

  // The whole reason 'terminal' exists as its own kind: without it a terminal
  // selection and a chat quote are indistinguishable (both have no path/url).
  it('distinguishes a terminal selection from a plain one', () => {
    expect(resolveQuoteType(q({ sourceKind: 'terminal' }))).toBe('terminal')
    expect(resolveQuoteType(q({ sourceKind: 'selection' }))).toBe('selection')
  })

  it('resolves a chat message quote', () => {
    expect(resolveQuoteType(q({ sourceKind: 'message', sessionId: 's1' }))).toBe('chat')
  })

  it('resolves a file quote', () => {
    expect(resolveQuoteType(q({ filePath: 'src/a.go', sourceKind: 'file' }))).toBe('file')
  })

  // A legacy row with no sourceKind and no locator is a bare selection.
  it('defaults to selection when nothing identifies the source', () => {
    expect(resolveQuoteType(q({ sourceKind: undefined }))).toBe('selection')
  })
})
