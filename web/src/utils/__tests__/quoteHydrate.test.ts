import { describe, expect, it, vi } from 'vitest'
import {
  hydrateDraggedQuote,
  registerQuoteHydrator,
  resetQuoteHydratorsForTest,
} from '@/utils/quoteHydrate'
import type { QuoteData } from '@/composables/useChatContext'

/**
 * The drag-drop hydration registry.
 *
 * A drag can only carry IDENTITY (dragstart must write the DataTransfer
 * synchronously), so a source whose content lives behind a request — a meeting
 * token, a mail id — has to be filled in at the drop. That fill-in used to be
 * two hand-written `if` chains, one per drop target (the chat column in App.vue
 * and a session row in SessionList.vue). Only App.vue's was mentioned in the
 * spec doc, so a new source routinely worked on one target and silently did
 * nothing on the other.
 *
 * These tests pin the contract both drop sites now share.
 */

function payload(over: Partial<QuoteData> = {}): QuoteData {
  return {
    text: '', filePath: 'x', language: '', startLine: 0, endLine: 0,
    sourceKind: 'selection', ...over,
  }
}

describe('quote hydration registry', () => {
  it('returns a payload it has no hydrator for unchanged', async () => {
    resetQuoteHydratorsForTest()
    const q = payload({ filePath: 'a1b2c3d fix login', locators: { commit: 'a1b2c3d' } })

    // A commit/task drag already carries everything the card renders.
    expect(await hydrateDraggedQuote(q)).toBe(q)
  })

  it('runs the hydrator registered for the locator the payload carries', async () => {
    resetQuoteHydratorsForTest()
    const fetched = payload({ text: 'fetched minutes', filePath: '会议' })
    registerQuoteHydrator('meeting', async (token) => {
      expect(token).toBe('tok-9')
      return fetched
    })

    const got = await hydrateDraggedQuote(payload({ locators: { meeting: 'tok-9' } }))
    expect(got).toBe(fetched)
  })

  it('passes the raw payload through, so a hydrator can see what already travelled', async () => {
    resetQuoteHydratorsForTest()
    // A RUNNING meeting puts its transcript in the payload at dragstart; the
    // hydrator must be able to notice and skip the fetch.
    registerQuoteHydrator('meeting', async (_token, p) => p.text ? p : null)

    const live = payload({ text: 'live transcript', locators: { meeting: 'tok-1' } })
    expect(await hydrateDraggedQuote(live)).toBe(live)
  })

  it('treats a null result as "nothing to quote" and drops the drag', async () => {
    resetQuoteHydratorsForTest()
    registerQuoteHydrator('meeting', async () => null)

    expect(await hydrateDraggedQuote(payload({ locators: { meeting: 'tok-empty' } }))).toBeNull()
  })

  it('picks the first registered locator deterministically when several are present', async () => {
    resetQuoteHydratorsForTest()
    const calls: string[] = []
    registerQuoteHydrator('meeting', async () => { calls.push('meeting'); return payload() })
    registerQuoteHydrator('mail', async () => { calls.push('mail'); return payload() })

    // A payload carrying both is a bug upstream; the registry must not fetch
    // twice, and must not depend on object key order.
    await hydrateDraggedQuote(payload({ locators: { mail: '77', meeting: 'tok-9' } }))
    expect(calls).toEqual(['meeting'])
  })

  it('skips a hydrator whose locator is present but empty', async () => {
    resetQuoteHydratorsForTest()
    const spy = vi.fn()
    registerQuoteHydrator('mail', spy)

    const q = payload({ locators: { mail: '' } })
    expect(await hydrateDraggedQuote(q)).toBe(q)
    expect(spy).not.toHaveBeenCalled()
  })

  it('ignores an unknown locator key (an open bag must not break the drop)', async () => {
    resetQuoteHydratorsForTest()
    const q = payload({ locators: { deploy: 'd-42' } })

    expect(await hydrateDraggedQuote(q)).toBe(q)
  })
})
