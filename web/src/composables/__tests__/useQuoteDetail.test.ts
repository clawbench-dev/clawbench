import { describe, expect, it, beforeEach } from 'vitest'
import { useQuoteDetail } from '../useQuoteDetail'
import type { QuoteItem } from '@/utils/quoteItem'

const QUOTE: QuoteItem = {
  id: 'q1', text: 'x := 1', note: '', filePath: 'src/a.go', language: 'go',
  startLine: 1, endLine: 2, sourceKind: 'file',
}

describe('useQuoteDetail', () => {
  beforeEach(() => {
    useQuoteDetail()._reset()
  })

  it('starts closed with no quote', () => {
    const d = useQuoteDetail()
    expect(d.open.value).toBe(false)
    expect(d.quote.value).toBeNull()
  })

  it('opens with the given quote and mode', () => {
    const d = useQuoteDetail()
    d.openQuoteDetail(QUOTE, { mode: 'staged' })

    expect(d.open.value).toBe(true)
    expect(d.quote.value).toEqual(QUOTE)
    expect(d.mode.value).toBe('staged')
  })

  it('tracks the sent mode', () => {
    const d = useQuoteDetail()
    d.openQuoteDetail(QUOTE, { mode: 'sent' })
    expect(d.mode.value).toBe('sent')
  })

  it('shares state across callers (module singleton)', () => {
    // The drawer is mounted once, so the opener and the mount site must see the
    // same state even though they call the composable separately.
    useQuoteDetail().openQuoteDetail(QUOTE, { mode: 'sent' })

    expect(useQuoteDetail().open.value).toBe(true)
    expect(useQuoteDetail().quote.value?.id).toBe('q1')
  })

  it('closes without discarding the quote, so a re-open keeps the payload', () => {
    const d = useQuoteDetail()
    d.openQuoteDetail(QUOTE, { mode: 'staged' })
    d.close()

    expect(d.open.value).toBe(false)
    expect(d.quote.value).toEqual(QUOTE)
  })

  it('is editable whenever a quote is loaded', () => {
    const d = useQuoteDetail()
    expect(d.editable.value).toBe(false)
    d.openQuoteDetail(QUOTE, { mode: 'staged' })
    expect(d.editable.value).toBe(true)
  })

  it('replaces the quote when opened again', () => {
    const d = useQuoteDetail()
    d.openQuoteDetail(QUOTE, { mode: 'staged' })
    d.openQuoteDetail({ ...QUOTE, id: 'q2' }, { mode: 'sent' })

    expect(d.quote.value?.id).toBe('q2')
    expect(d.mode.value).toBe('sent')
  })
})
