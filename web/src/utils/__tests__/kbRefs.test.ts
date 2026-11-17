import { describe, expect, it } from 'vitest'
import { parseKBRefs } from '../kbRefs'

/**
 * `parseKBRefs` normalizes the knowledge-base references a turn carried.
 *
 * Every assertion here names the full object: the parser's job is to produce a
 * stable shape (missing fields defaulted, text trimmed, a chunk id always
 * present), so `toEqual` on a partial object is what let the chunk id drift out
 * of the expectations unnoticed. A partial object also cannot express "the text
 * was trimmed", which is half of what this function does.
 */
function ref(overrides: Record<string, unknown> = {}) {
  return {
    documentId: 0,
    chunkId: 0,
    documentTitle: '',
    collectionName: '',
    location: '',
    text: '',
    ...overrides,
  }
}

describe('parseKBRefs', () => {
  it('keeps every field of a valid entry and trims its text', () => {
    const parsed = parseKBRefs([
      {
        documentId: 7,
        chunkId: 42,
        documentTitle: 'handbook.md',
        collectionName: '制度',
        location: 'L1-L3',
        text: '  报销流程：先提交申请。\n',
      },
    ])
    expect(parsed).toEqual([
      ref({
        documentId: 7,
        chunkId: 42,
        documentTitle: 'handbook.md',
        collectionName: '制度',
        location: 'L1-L3',
        // The source passage is quoted into a card: leading/trailing whitespace
        // would show up as a stray blank line there.
        text: '报销流程：先提交申请。',
      }),
    ])
  })

  it('returns an empty list for non-arrays', () => {
    expect(parseKBRefs(undefined)).toEqual([])
    expect(parseKBRefs(null)).toEqual([])
    expect(parseKBRefs({ text: 'x' })).toEqual([])
    expect(parseKBRefs('x')).toEqual([])
  })

  it('drops entries without usable text', () => {
    expect(
      parseKBRefs([
        { documentTitle: 'a', text: '' },
        { documentTitle: 'b' },
        { documentTitle: 'c', text: '   ' },
        null,
        'nope',
      ]),
    ).toEqual([])
  })

  it('keeps a valid entry alongside malformed ones and defaults the rest', () => {
    // One bad element must not throw away the good ones — this is the shape the
    // sidecar writes, and a single malformed row used to blank the whole card.
    expect(parseKBRefs([{ text: 'only text' }, null])).toEqual([ref({ text: 'only text' })])
  })

  it('normalizes a missing chunk id to 0 rather than dropping the reference', () => {
    // Older payloads predate chunkId; the card still needs the text and the
    // document it came from, and "no chunk" must be distinguishable from a
    // chunk numbered zero without losing the entry.
    const parsed = parseKBRefs([{ documentId: 3, documentTitle: 'a.md', text: 'hi' }])
    expect(parsed).toHaveLength(1)
    expect(parsed[0]).toEqual(ref({ documentId: 3, documentTitle: 'a.md', text: 'hi' }))
    expect(parsed[0].chunkId).toBe(0)
  })
})
