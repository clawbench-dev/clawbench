import { describe, expect, it } from 'vitest'
import { highlightTermsInElement, termsFromRunePositions } from '../searchHighlight'

describe('termsFromRunePositions', () => {
  it('slices by runes, not UTF-16 units', () => {
    // "报销" is 2 runes but 4 UTF-16 units: positions must refer to runes.
    expect(termsFromRunePositions('报销流程', [{ start: 0, end: 2 }])).toEqual(['报销'])
    expect(termsFromRunePositions('a finance term', [{ start: 2, end: 9 }])).toEqual(['finance'])
  })

  it('clamps out-of-range positions and drops empty terms', () => {
    expect(termsFromRunePositions('ab', [{ start: 0, end: 99 }, { start: 5, end: 7 }])).toEqual(['ab'])
    expect(termsFromRunePositions('ab', [])).toEqual([])
    expect(termsFromRunePositions('ab', undefined)).toEqual([])
    expect(termsFromRunePositions('', [{ start: 0, end: 1 }])).toEqual([])
  })

  it('deduplicates repeated terms', () => {
    expect(termsFromRunePositions('报销', [{ start: 0, end: 2 }, { start: 0, end: 2 }])).toEqual(['报销'])
  })
})

describe('highlightTermsInElement', () => {
  function el(html: string): HTMLElement {
    const host = document.createElement('div')
    host.innerHTML = html
    return host
  }

  it('wraps matched text across text nodes', () => {
    const host = el('<p>报销流程与年假制度</p>')
    highlightTermsInElement(host, ['报销', '年假'], 'kb-mark')
    const marks = host.querySelectorAll('mark.kb-mark')
    expect(marks).toHaveLength(2)
    expect(marks[0].textContent).toBe('报销')
    expect(marks[1].textContent).toBe('年假')
  })

  it('is case-insensitive and re-runnable without nesting', () => {
    const host = el('<p>The Quick Fox</p>')
    highlightTermsInElement(host, ['quick'], 'kb-mark')
    expect(host.querySelector('mark')?.textContent).toBe('Quick')

    // A second application must remove the old mark first, not nest marks.
    highlightTermsInElement(host, ['quick'], 'kb-mark')
    expect(host.querySelectorAll('mark')).toHaveLength(1)
    expect(host.querySelector('mark')?.textContent).toBe('Quick')
    expect(host.textContent).toBe('The Quick Fox')
  })

  it('leaves the text content intact outside matches', () => {
    const host = el('<p>one two three</p>')
    highlightTermsInElement(host, ['two'], 'kb-mark')
    expect(host.textContent).toBe('one two three')
  })

  it('does nothing when terms are empty', () => {
    const host = el('<p>text</p>')
    highlightTermsInElement(host, [], 'kb-mark')
    expect(host.querySelector('mark')).toBeNull()
  })
})
