import { describe, expect, it } from 'vitest'
import ExcelJS from 'exceljs'
import type { Worksheet } from 'exceljs'
import { cellToText, MAX_SHEET_COLUMNS, MAX_SHEET_ROWS, parseWorkbook, worksheetToTable } from '../sheetRows'

describe('cellToText', () => {
  it('formats dates from their UTC parts (ExcelJS stores wall-clock as UTC)', () => {
    expect(cellToText(new Date(Date.UTC(2024, 0, 2)))).toBe('2024-01-02')
    expect(cellToText(new Date(Date.UTC(2024, 0, 2, 13, 5)))).toBe('2024-01-02 13:05')
    expect(cellToText(new Date(Date.UTC(2024, 0, 2, 13, 5, 9)))).toBe('2024-01-02 13:05:09')
  })

  it('takes the computed result of a formula, not the formula text', () => {
    expect(cellToText({ formula: 'B2*2', result: 6 })).toBe('6')
    expect(cellToText({ sharedFormula: 'B2', result: 'abc' })).toBe('abc')
    expect(cellToText({ formula: 'A1/A2', result: { error: '#DIV/0!' } })).toBe('#DIV/0!')
  })

  it('reads rich text, hyperlinks and error values', () => {
    expect(cellToText({ richText: [{ text: 'Hello ' }, { text: 'world' }] })).toBe('Hello world')
    expect(cellToText({ text: '官网', hyperlink: 'https://example.com' })).toBe('官网')
    expect(cellToText({ error: '#N/A' })).toBe('#N/A')
  })

  it('renders booleans the way Excel does', () => {
    expect(cellToText(true)).toBe('TRUE')
    expect(cellToText(false)).toBe('FALSE')
  })

  it('renders empty cells as an empty string', () => {
    expect(cellToText(null)).toBe('')
    expect(cellToText(undefined)).toBe('')
    expect(cellToText('')).toBe('')
    expect(cellToText(0)).toBe('0')
  })
})

describe('worksheetToTable', () => {
  function fakeSheet(rowCount: number, columnCount: number): Worksheet {
    return {
      name: 'big',
      columnCount,
      eachRow: (_options: unknown, callback: (row: { number: number }) => void) => {
        callback({ number: rowCount })
      },
      getRow: (r: number) => ({
        values: Array.from({ length: columnCount + 1 }, (_, c) => (c === 0 ? undefined : `r${r}c${c}`)),
      }),
    } as unknown as Worksheet
  }

  it('marks the sheet as truncated past the row cap', () => {
    const table = worksheetToTable(fakeSheet(MAX_SHEET_ROWS + 1, 3))
    expect(table.rowsTruncated).toBe(true)
    expect(table.rows).toHaveLength(MAX_SHEET_ROWS - 1)
  })

  it('marks the sheet as truncated past the column cap', () => {
    const table = worksheetToTable(fakeSheet(2, MAX_SHEET_COLUMNS + 1))
    expect(table.columnsTruncated).toBe(true)
    expect(table.headers).toHaveLength(MAX_SHEET_COLUMNS)
  })
})

describe('parseWorkbook', () => {
  async function write(workbook: ExcelJS.Workbook): Promise<ArrayBuffer> {
    const buffer = await workbook.xlsx.writeBuffer()
    return buffer as unknown as ArrayBuffer
  }

  it('reads every worksheet into a header row plus data rows', async () => {
    const workbook = new ExcelJS.Workbook()
    const sales = workbook.addWorksheet('销售')
    sales.addRow(['名称', '数量', '日期', '合计'])
    sales.addRow(['苹果', 3, new Date(Date.UTC(2024, 0, 2)), { formula: 'B2*2', result: 6 }])
    sales.addRow(['香蕉', 5, new Date(Date.UTC(2024, 0, 3)), { formula: 'B3*2', result: 10 }])

    const notes = workbook.addWorksheet('备注')
    notes.addRow(['说明'])
    notes.addRow(['只有一行'])

    const sheets = await parseWorkbook(await write(workbook))

    expect(sheets.map(sheet => sheet.name)).toEqual(['销售', '备注'])
    expect(sheets[0].headers).toEqual(['名称', '数量', '日期', '合计'])
    expect(sheets[0].rows).toEqual([
      ['苹果', '3', '2024-01-02', '6'],
      ['香蕉', '5', '2024-01-03', '10'],
    ])
    expect(sheets[1].rows).toEqual([['只有一行']])
  })

  it('drops trailing empty columns', async () => {
    const workbook = new ExcelJS.Workbook()
    const sheet = workbook.addWorksheet('sheet')
    sheet.addRow(['a', 'b'])
    sheet.addRow(['1', '2'])
    // A styled-but-empty cell far to the right must not stretch the table.
    sheet.getCell('H1').fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: 'FFFF0000' } }

    const sheets = await parseWorkbook(await write(workbook))

    expect(sheets[0].headers).toEqual(['a', 'b'])
    expect(sheets[0].rows).toEqual([['1', '2']])
    expect(sheets[0].columnsTruncated).toBe(false)
  })

  it('returns an empty table for a sheet without values', async () => {
    const workbook = new ExcelJS.Workbook()
    workbook.addWorksheet('empty')

    const sheets = await parseWorkbook(await write(workbook))

    expect(sheets[0].headers).toEqual([])
    expect(sheets[0].rows).toEqual([])
  })
})
