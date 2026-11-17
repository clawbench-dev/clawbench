/**
 * Excel 工作表 → 表格行（喂给 DataTable，和 CSV 预览同一套交互）。
 *
 * 只取值：颜色、字体、合并单元格、列宽、图表、图片都不保留。公式取计算结果。
 *
 * 日期要按 Excel 里存的那个「墙上时间」还原：ExcelJS 把序列号当成 UTC 毫秒
 * 构造 Date（见其 `excelToDate`），所以格式化必须用 UTC 分量，用本地分量在
 * 东八区会整体偏 8 小时。
 *
 * 放在组件外面是为了能单测：公式 / 富文本 / 超链接 / 错误值 / 日期 / 行列上限
 * 这些规则都不是 Vue 的事。
 */
import type { Worksheet } from 'exceljs'

/** 单表最多展示的行数。整表塞进 DOM 会卡死渲染，和 CSV 一样要有上限。 */
export const MAX_SHEET_ROWS = 5000
/** 单表最多展示的列数，与 CSV 预览的 MAX_TABLE_COLUMNS 保持一致。 */
export const MAX_SHEET_COLUMNS = 200

export interface SheetTable {
  /** 工作表名（Excel 里页签上那个）。 */
  name: string
  /** 第一行当表头，和 CSV 一致。 */
  headers: string[]
  rows: string[][]
  rowsTruncated: boolean
  columnsTruncated: boolean
}

/** 一个单元格的显示文本。认不出来的一律 String()，绝不抛错。 */
export function cellToText(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (value instanceof Date) return formatSheetDate(value)

  switch (typeof value) {
    case 'string':
      return value
    case 'number':
      return String(value)
    case 'boolean':
      // Excel 自己显示 TRUE / FALSE，不是 true / false。
      return value ? 'TRUE' : 'FALSE'
    case 'bigint':
      return value.toString()
    default:
      break
  }
  if (typeof value !== 'object') return String(value)

  const cell = value as Record<string, unknown>

  // 富文本：一串 {text, font} 片段
  if (Array.isArray(cell.richText)) {
    return (cell.richText as Array<{ text?: unknown }>)
      .map(part => (typeof part?.text === 'string' ? part.text : ''))
      .join('')
  }
  // 公式 / 共享公式：{ formula, result } / { sharedFormula, result }
  if ('result' in cell) return cellToText(cell.result)
  // 超链接：{ text, hyperlink }
  if (typeof cell.text === 'string') return cell.text
  // 错误值：{ error: '#DIV/0!' }
  if (typeof cell.error === 'string') return cell.error

  return String(value)
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

/** 见文件头：ExcelJS 的 Date 用 UTC 分量承载原始时间。 */
function formatSheetDate(date: Date): string {
  const day = `${date.getUTCFullYear()}-${pad2(date.getUTCMonth() + 1)}-${pad2(date.getUTCDate())}`
  const hours = date.getUTCHours()
  const minutes = date.getUTCMinutes()
  const seconds = date.getUTCSeconds()
  if (hours === 0 && minutes === 0 && seconds === 0) return day
  const time = `${pad2(hours)}:${pad2(minutes)}`
  return seconds === 0 ? `${day} ${time}` : `${day} ${time}:${pad2(seconds)}`
}

/** 一张工作表 → 表头 + 行。 */
export function worksheetToTable(sheet: Worksheet, fallbackName = ''): SheetTable {
  const name = (sheet.name || fallbackName || '').trim()

  // 最后一个「有值」的行号：eachRow 会跳过空行，拿到的就是真实上界。
  // 不能用 sheet.rowCount —— 只剩格式的空行也会被算进去。
  let lastRow = 0
  sheet.eachRow({ includeEmpty: false }, row => {
    lastRow = row.number
  })

  const raw: string[][] = []
  const rowLimit = Math.min(lastRow, MAX_SHEET_ROWS)
  // columnCount 是「用过的最右一列」，超过它的一定是空格子，不必逐格读。
  const colLimit = Math.min(sheet.columnCount || 0, MAX_SHEET_COLUMNS)
  for (let r = 1; r <= rowLimit; r++) {
    const values = sheet.getRow(r).values as unknown[]
    const row: string[] = []
    for (let c = 1; c <= colLimit; c++) row.push(cellToText(values[c]))
    raw.push(row)
  }

  // 裁掉右侧一整列都空的列（模板里常见整列带格式但没内容）。
  let width = 0
  for (const row of raw) {
    for (let c = row.length - 1; c >= width; c--) {
      if (row[c] !== '') {
        width = c + 1
        break
      }
    }
  }

  const rows = raw.map(row => row.slice(0, width))
  if (rows.length === 0) {
    return { name, headers: [], rows: [], rowsTruncated: false, columnsTruncated: false }
  }

  return {
    name,
    headers: rows[0],
    rows: rows.slice(1),
    rowsTruncated: lastRow > MAX_SHEET_ROWS,
    columnsTruncated: sheet.columnCount > MAX_SHEET_COLUMNS,
  }
}

/**
 * 整个 xlsx 文件 → 每张工作表一张表。
 *
 * exceljs 只在真正打开 Excel 时才加载（动态 import），不进主包。
 */
export async function parseWorkbook(buffer: ArrayBuffer): Promise<SheetTable[]> {
  const { default: ExcelJS } = await import('exceljs')
  const workbook = new ExcelJS.Workbook()
  await workbook.xlsx.load(buffer)
  return workbook.worksheets.map((sheet, index) => worksheetToTable(sheet, `Sheet${index + 1}`))
}
