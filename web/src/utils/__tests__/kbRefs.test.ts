import { describe, expect, it } from 'vitest'
import { parseKBRefs } from '../kbRefs'

describe('parseKBRefs', () => {
  it('normalizes valid entries', () => {
    expect(
      parseKBRefs([
        {
          documentId: 7,
          documentTitle: 'handbook.md',
          collectionName: '制度',
          location: 'L1-L3',
          text: '  报销流程：先提交申请。\n',
        },
      ]),
    ).toEqual([
      {
        documentId: 7,
        documentTitle: 'handbook.md',
        collectionName: '制度',
        location: 'L1-L3',
        text: '报销流程：先提交申请。',
      },
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

  it('keeps a valid entry alongside malformed ones and defaults missing fields', () => {
    expect(parseKBRefs([{ text: 'only text' }, null])).toEqual([
      {
        documentId: 0,
        documentTitle: '',
        collectionName: '',
        location: '',
        text: 'only text',
      },
    ])
  })
})
