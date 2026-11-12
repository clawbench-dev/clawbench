import { describe, expect, it } from 'vitest'
import { splitTableBlocks } from '../tableBlockSplit'

function tableWrapper(inner: string, wrapperAttrs = 'word-wrap'): string {
  return `<div class="table-block-wrapper ${wrapperAttrs}">`
    + '<div class="table-block-header"><span class="table-block-label">Table</span></div>'
    + `<div class="table-wrap">${inner}</div></div>`
}

const NUMERIC_TABLE = '<table><thead><tr><th>region</th><th>q1</th></tr></thead>'
  + '<tbody><tr><td>East</td><td>10</td></tr><tr><td>West</td><td>5</td></tr></tbody></table>'

const TEXT_TABLE = '<table><thead><tr><th>name</th><th>city</th></tr></thead>'
  + '<tbody><tr><td>ann</td><td>paris</td></tr><tr><td>bob</td><td>berlin</td></tr></tbody></table>'

describe('splitTableBlocks', () => {
  it('keeps HTML without tables as one segment', () => {
    const segments = splitTableBlocks('<p>hello</p>', true)
    expect(segments).toHaveLength(1)
    expect(segments[0].kind).toBe('html')
  })

  it('does nothing while streaming', () => {
    const segments = splitTableBlocks(`<p>a</p>${tableWrapper(NUMERIC_TABLE)}`, false)
    expect(segments).toHaveLength(1)
    expect(segments[0].kind).toBe('html')
  })

  it('pulls a numeric table out of the surrounding HTML', () => {
    const html = `<p>before</p>${tableWrapper(NUMERIC_TABLE)}<p>after</p>`
    const segments = splitTableBlocks(html, true)

    expect(segments.map(s => s.kind)).toEqual(['html', 'table', 'html'])
    const table = segments[1]
    if (table.kind !== 'table') throw new Error('expected table segment')
    expect(table.headers).toEqual(['region', 'q1'])
    expect(table.rows).toEqual([['East', '10'], ['West', '5']])

    const before = segments[0]
    const after = segments[2]
    expect(before.kind === 'html' && before.html).toContain('before')
    expect(after.kind === 'html' && after.html).toContain('after')
  })

  it('pulls a text table out too — every table can switch modes', () => {
    const segments = splitTableBlocks(`<p>a</p>${tableWrapper(TEXT_TABLE)}`, true)
    expect(segments.map(s => s.kind)).toEqual(['html', 'table'])
  })

  it('leaves a nested table in the HTML (splitting it would break the tree)', () => {
    const segments = splitTableBlocks(`<blockquote>${tableWrapper(NUMERIC_TABLE)}</blockquote>`, true)
    expect(segments).toHaveLength(1)
    expect(segments[0].kind).toBe('html')
  })

  it('escapes text nodes when it re-serializes the remaining HTML', () => {
    const segments = splitTableBlocks(`a &lt; b${tableWrapper(NUMERIC_TABLE)}`, true)
    const first = segments[0]
    if (first.kind !== 'html') throw new Error('expected html segment')
    expect(first.html).toBe('a &lt; b')
  })
})
