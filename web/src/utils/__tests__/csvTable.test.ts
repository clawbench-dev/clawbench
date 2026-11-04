import { describe, expect, it } from 'vitest'
import {
  MAX_TABLE_COLUMNS,
  compareCells,
  detectDelimiter,
  detectNumericColumns,
  filterAndSortRows,
  parseDelimited,
  parseTable,
  stripBom,
} from '@/utils/csvTable'

describe('parseDelimited', () => {
  it('parses plain comma-separated rows', () => {
    expect(parseDelimited('a,b,c\n1,2,3', ',')).toEqual([
      ['a', 'b', 'c'],
      ['1', '2', '3'],
    ])
  })

  it('keeps delimiters inside quoted fields', () => {
    expect(parseDelimited('a,"b,c",d', ',')).toEqual([['a', 'b,c', 'd']])
  })

  it('unescapes doubled quotes', () => {
    expect(parseDelimited('a,"say ""hi""",c', ',')).toEqual([['a', 'say "hi"', 'c']])
  })

  it('keeps newlines inside quoted fields', () => {
    expect(parseDelimited('a,"line1\nline2",c\n1,2,3', ',')).toEqual([
      ['a', 'line1\nline2', 'c'],
      ['1', '2', '3'],
    ])
  })

  it('handles CRLF and a trailing newline without inventing a row', () => {
    expect(parseDelimited('a,b\r\n1,2\r\n', ',')).toEqual([
      ['a', 'b'],
      ['1', '2'],
    ])
  })

  it('does not treat a quote inside a value as an opening quote', () => {
    // 5" is an inch mark, not the start of a quoted field — the classic case
    // where "any quote toggles quoting" swallows the rest of the file.
    expect(parseDelimited('size\n5"\n6"', ',')).toEqual([['size'], ['5"'], ['6"']])
  })

  it('allows leading spaces before an opening quote', () => {
    expect(parseDelimited('a, "b,c"', ',')).toEqual([['a', 'b,c']])
  })
})

describe('detectDelimiter', () => {
  it('always uses tab for .tsv', () => {
    expect(detectDelimiter('a,b\n1,2', 'x.tsv')).toBe('\t')
  })

  it('detects tab-separated content in a .csv file', () => {
    expect(detectDelimiter('a\tb\tc\n1\t2\t3')).toBe('\t')
  })

  it('prefers semicolons when commas only appear inside quoted text', () => {
    const text = 'name;note\n"Smith, John";ok\n"Doe, Jane";no'
    expect(detectDelimiter(text, 'people.csv')).toBe(';')
  })

  it('falls back to a comma for single-column content', () => {
    expect(detectDelimiter('just one column\nsecond line')).toBe(',')
  })
})

describe('parseTable', () => {
  it('takes the first row as headers and pads ragged rows', () => {
    const table = parseTable('a,b,c\n1,2\n3,4,5,6')
    expect(table.headers).toEqual(['a', 'b', 'c', ''])
    expect(table.rows).toEqual([
      ['1', '2', '', ''],
      ['3', '4', '5', '6'],
    ])
    expect(table.columnCount).toBe(4)
  })

  it('strips a UTF-8 BOM from the first header', () => {
    expect(stripBom('\uFEFFa,b')).toBe('a,b')
    expect(parseTable('\uFEFFid,name\n1,x').headers).toEqual(['id', 'name'])
  })

  it('drops trailing blank lines', () => {
    const table = parseTable('a,b\n1,2\n\n\n')
    expect(table.rows).toEqual([['1', '2']])
  })

  it('caps pathological column counts', () => {
    const wide = Array.from({ length: MAX_TABLE_COLUMNS + 20 }, (_, i) => `c${i}`).join(',')
    const table = parseTable(`${wide}\n${wide}`)
    expect(table.columnCount).toBe(MAX_TABLE_COLUMNS)
    expect(table.columnsTruncated).toBe(true)
  })

  it('returns an empty table for empty content', () => {
    expect(parseTable('')).toEqual({
      headers: [],
      rows: [],
      columnCount: 0,
      delimiter: ',',
      columnsTruncated: false,
    })
  })

  it('keeps a header-only file as headers with no rows', () => {
    const table = parseTable('a,b')
    expect(table.headers).toEqual(['a', 'b'])
    expect(table.rows).toEqual([])
  })
})

describe('compareCells', () => {
  it('compares numeric cells numerically, not lexically', () => {
    expect(compareCells('9', '10')).toBeLessThan(0)
  })

  it('sorts text with embedded numbers naturally', () => {
    expect(compareCells('item2', 'item10')).toBeLessThan(0)
  })

  it('puts numbers before text', () => {
    expect(compareCells('2', 'apple')).toBeLessThan(0)
    expect(compareCells('apple', '2')).toBeGreaterThan(0)
  })
})

describe('filterAndSortRows', () => {
  const rows = [
    ['b', '2', 'x'],
    ['a', '10', 'y'],
    ['c', '1', 'x'],
  ]

  it('matches the query against any cell, case-insensitively', () => {
    expect(filterAndSortRows(rows, { query: 'X', columnFilters: [], sort: null })).toEqual([
      ['b', '2', 'x'],
      ['c', '1', 'x'],
    ])
  })

  it('applies per-column filters', () => {
    expect(filterAndSortRows(rows, { query: '', columnFilters: ['', '', 'x'], sort: null })).toEqual([
      ['b', '2', 'x'],
      ['c', '1', 'x'],
    ])
  })

  it('sorts numerically in both directions without mutating the input', () => {
    const asc = filterAndSortRows(rows, { query: '', columnFilters: [], sort: { column: 1, direction: 'asc' } })
    expect(asc.map(r => r[1])).toEqual(['1', '2', '10'])
    const desc = filterAndSortRows(rows, { query: '', columnFilters: [], sort: { column: 1, direction: 'desc' } })
    expect(desc.map(r => r[1])).toEqual(['10', '2', '1'])
    expect(rows.map(r => r[1])).toEqual(['2', '10', '1'])
  })
})

describe('detectNumericColumns', () => {
  it('marks mostly-numeric columns and leaves text columns alone', () => {
    const rows = [
      ['Alice', '1'],
      ['Bob', '2'],
      ['Carol', ''],
    ]
    expect(detectNumericColumns(rows, 2)).toEqual([false, true])
  })

  it('treats a column with no values as non-numeric', () => {
    expect(detectNumericColumns([['', '']], 2)).toEqual([false, false])
  })
})
