import { describe, expect, it } from 'vitest'
import { fromStagedQuote, fromFileEntry, toFileEntry, materializeQuotes, quoteLabel, quoteLineRange, canJumpToSource, isQuoteFileEntry, type QuoteItem } from '@/utils/quoteItem.ts'

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

  it('refuses a forge quote with no address', () => {
    const q: QuoteItem = { id: '1', text: '', note: '', filePath: 'acme#7', language: '', startLine: 0, endLine: 0, sourceKind: 'url' }
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
