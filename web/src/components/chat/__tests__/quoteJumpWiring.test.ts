import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'
import { canJumpToSource, type QuoteItem } from '@/utils/quoteItem'

/**
 * Every locator the drawer ACCEPTS must have a branch in the jump handler.
 *
 * `canJumpToSource` decides whether "跳转来源" is shown; `jumpToQuoteSource`
 * decides where it goes. If one accepts a locator the other ignores, the button
 * appears and does nothing — a silent dead end the file's own comment warns
 * about ("must stay in sync with the dispatch order").
 *
 * A behavioural test cannot catch it: the jump dispatches a window event or
 * navigates, so "nothing happened" is indistinguishable from "went somewhere
 * the test does not look". Only asserting the two lists together catches a
 * locator that was added to one side.
 */
const CASES: Array<[string, Partial<QuoteItem>]> = [
  ['commitSha', { commitSha: 'abc1234' }],
  ['taskId', { taskId: 3 }],
  ['meetingToken', { meetingToken: 'tok-9' }],
  ['mailId', { mailId: 77 }],
  ['sessionId', { sessionId: 'sess-1' }],
  ['url', { url: 'https://example.com/x', sourceKind: 'url' }],
  ['filePath', { filePath: '/proj/a.go', sourceKind: 'file' }],
]

/** Body of a named function, brace-balanced (nested blocks included). */
function functionBody(src: string, name: string): string {
  const start = src.indexOf(`function ${name}(`)
  if (start < 0) throw new Error(`function ${name} not found`)
  const open = src.indexOf('{', start)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open + 1, i)
    }
  }
  throw new Error(`unbalanced braces in ${name}`)
}

describe('quote jump wiring', () => {
  const src = readWebFile('src/components/chat/ChatPanelContent.vue')
  const body = functionBody(src, 'jumpToQuoteSource')

  it.each(CASES)('%s is accepted by canJumpToSource AND handled by the jump', (key, over) => {
    const q: QuoteItem = {
      id: 'q1', text: 'x', note: '', filePath: '', language: '',
      startLine: 0, endLine: 0, sourceKind: 'file', ...over,
    }

    expect(canJumpToSource(q), `canJumpToSource must accept ${key}`).toBe(true)
    expect(body, `jumpToQuoteSource must handle ${key}`).toContain(`q.${key}`)
  })

  it('a mail quote without its id offers no jump (the label is not a path)', () => {
    // Same trap as a meeting quote with a lost token: falling through to the
    // file branch would open the subject line as a path and 404.
    const q: QuoteItem = {
      id: 'q1', text: '', note: '', filePath: '周报 · 张三', language: 'mail',
      startLine: 0, endLine: 0, sourceKind: 'mail',
    }
    expect(canJumpToSource(q)).toBe(false)
  })
})
