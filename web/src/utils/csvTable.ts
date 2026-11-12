/**
 * CSV / TSV parsing and table helpers for the file preview.
 *
 * Kept out of the component so the parser is unit-testable without mounting
 * Vue: quoted delimiters, doubled quotes, embedded newlines, CRLF and ragged
 * rows are exactly the cases a naive `split(',')` gets wrong.
 */

export type SortDirection = 'asc' | 'desc'

export interface SortState {
    column: number
    direction: SortDirection
}

export interface ParsedTable {
    /** Raw header labels. Empty strings are expected — the UI supplies a fallback. */
    headers: string[]
    rows: string[][]
    /** Rendered column count (already capped at MAX_TABLE_COLUMNS). */
    columnCount: number
    delimiter: string
    /** True when the file has more columns than the viewer renders. */
    columnsTruncated: boolean
}

/**
 * Upper bound on rendered columns. A pathological single-line file can contain
 * hundreds of thousands of fields; without a cap one such file freezes the
 * renderer. Well past any real spreadsheet's visible width.
 */
export const MAX_TABLE_COLUMNS = 200

const DELIMITER_CANDIDATES = [',', '\t', ';', '|'] as const
/** Bytes sampled for delimiter sniffing — enough for a header row plus a few. */
const DETECT_SAMPLE_BYTES = 8192
const DETECT_SAMPLE_ROWS = 10

export function stripBom(text: string): string {
    return text.charCodeAt(0) === 0xfeff ? text.slice(1) : text
}

/**
 * RFC4180-style parser.
 *
 * Quotes only open a field at its start (leading spaces allowed). That
 * deliberate limitation keeps values like `5"` or `12" x 8"` from swallowing
 * the rest of the file, which is the common failure of "any quote toggles
 * quoting" state machines on real exports.
 */
export function parseDelimited(text: string, delimiter: string): string[][] {
    const rows: string[][] = []
    let row: string[] = []
    let field = ''
    let inQuotes = false
    let i = 0

    while (i < text.length) {
        const ch = text[i]

        if (inQuotes) {
            if (ch === '"') {
                if (text[i + 1] === '"') {
                    field += '"'
                    i += 2
                    continue
                }
                inQuotes = false
                i += 1
                continue
            }
            field += ch
            i += 1
            continue
        }

        if (ch === '"' && field.trim() === '') {
            field = ''
            inQuotes = true
            i += 1
            continue
        }

        if (ch === delimiter) {
            row.push(field)
            field = ''
            i += 1
            continue
        }

        if (ch === '\r' || ch === '\n') {
            if (ch === '\r' && text[i + 1] === '\n') i += 1
            row.push(field)
            rows.push(row)
            row = []
            field = ''
            i += 1
            continue
        }

        field += ch
        i += 1
    }

    // A file ending with a newline leaves nothing pending; anything else is the
    // last (unterminated) row.
    if (field !== '' || row.length > 0) {
        row.push(field)
        rows.push(row)
    }

    return rows
}

/**
 * Pick the separator by trying every candidate and keeping the one that yields
 * a stable, multi-column shape. Counting occurrences alone is not enough: a
 * semicolon-delimited file whose text fields contain commas would score higher
 * on commas.
 */
export function detectDelimiter(text: string, fileName = ''): string {
    if (/\.tsv$/i.test(fileName)) return '\t'

    const sample = text.slice(0, DETECT_SAMPLE_BYTES)
    let best = ','
    let bestScore = 0
    for (const candidate of DELIMITER_CANDIDATES) {
        const score = scoreDelimiter(sample, candidate)
        if (score > bestScore) {
            best = candidate
            bestScore = score
        }
    }
    return best
}

function scoreDelimiter(sample: string, delimiter: string): number {
    const rows = parseDelimited(sample, delimiter).slice(0, DETECT_SAMPLE_ROWS)
    if (rows.length === 0) return 0
    const width = rows[0].length
    if (width < 2) return 0
    // The header's width must repeat on most sampled rows; a separator that
    // only appears inside free text produces a ragged shape and scores 0.
    const agreeing = rows.filter(r => r.length === width).length
    if (agreeing < 2) return 0
    return width * agreeing
}

export function parseTable(content: string, fileName = ''): ParsedTable {
    const text = stripBom(content)
    const delimiter = detectDelimiter(text, fileName)
    const parsed = parseDelimited(text, delimiter)

    // Trailing blank lines are an artifact of the file ending, not data rows.
    while (parsed.length > 0) {
        const last = parsed[parsed.length - 1]
        if (last.length === 1 && last[0].trim() === '') parsed.pop()
        else break
    }

    if (parsed.length === 0) {
        return { headers: [], rows: [], columnCount: 0, delimiter, columnsTruncated: false }
    }

    let widest = 0
    for (const row of parsed) widest = Math.max(widest, row.length)
    const columnsTruncated = widest > MAX_TABLE_COLUMNS
    const width = Math.min(widest, MAX_TABLE_COLUMNS)

    const normalize = (row: string[]): string[] => {
        const out: string[] = new Array(width)
        for (let i = 0; i < width; i++) out[i] = row[i] ?? ''
        return out
    }

    return {
        headers: normalize(parsed[0]),
        rows: parsed.slice(1).map(normalize),
        columnCount: width,
        delimiter,
        columnsTruncated,
    }
}

/**
 * Numeric value of a cell, or null when it is not a number.
 *
 * 认得的写法（表里都当数字）：
 *   普通数字        42  -3.5  +7  1e3
 *   千分位 / 小数逗号 1,234  3450,75  1.234,56  1,234.56  1 234（空格分组）
 *   百分号 / 千分号   12.5%  0,5％  3‰
 *   货币前缀        ¥1,234  $12.50  CNY 88
 *   单位后缀        120元  42ms  3.2GB  5人  90分  1.5m
 *   万亿后缀        1.2万  3亿元  2亿
 *   括号负数        (1,234)  → -1234
 *   全角数字        １２３ → 123
 *
 * 认不出来就返回 null，不猜：`1-2`、`12pm`、`3D`、`暂无` 都不算。
 */
export function parseNumericCell(value: string): number | null {
    const text = normalizeWidth(value.trim())
    if (text === '') return null

    // 会计写法：(1,234) 表示 -1234
    const paren = /^\((.+)\)$/.exec(text)
    if (paren) {
        const inner = parsePlainNumber(paren[1])
        return inner === null ? null : -inner
    }

    const plain = parsePlainNumber(text)
    if (plain !== null) return plain

    return parseDecoratedNumber(text)
}

const FULLWIDTH: Record<string, string> = {
    '０': '0', '１': '1', '２': '2', '３': '3', '４': '4',
    '５': '5', '６': '6', '７': '7', '８': '8', '９': '9',
    '．': '.', '，': ',', '％': '%', '＋': '+', '－': '-',
    '（': '(', '）': ')', '　': ' ',
}

function normalizeWidth(text: string): string {
    return text.replace(/[０-９．，％＋－（）]/g, ch => FULLWIDTH[ch] ?? ch)
}

/** 纯数字（含千分位、小数逗号、空格分组）。 */
function parsePlainNumber(text: string): number | null {
    const trimmed = text.trim().replace(/[−]/g, '-')
    if (trimmed === '') return null
    const normalized = normalizeNumericText(trimmed)
    if (normalized === null) return null
    const n = Number(normalized)
    return Number.isFinite(n) ? n : null
}

/**
 * 带装饰的数字：货币前缀、单位后缀、百分号、万亿。
 *
 * 单位用白名单而不是「末尾任意字母」：`42ms` 要认，`12pm`、`3D` 这种不能认。
 */
const UNIT_PATTERN = [
    '毫秒', '分钟', '小时',
    '万元', '亿元',
    'GB', 'MB', 'KB', 'TB', 'GHz', 'MHz', 'mAh', 'kg', 'km', 'cm', 'mm',
    'ms', 'px', 'pt', 'Hz', 'kW', '℃', '°C',
    '万', '亿', '元', '人', '个', '件', '台', '次', '秒', '分', '时', '天', '月', '年', '度',
    '%', '‰',
    's', 'g', 'm', 'W', 'V', 'A', 'B',
].join('|')

const UNIT_RE = new RegExp(`^(.*?)\\s*(${UNIT_PATTERN})$`)
const CURRENCY_RE = /^(?:[¥￥$€£₩₽]|USD|CNY|RMB|EUR|GBP|JPY|HKD)\s*/i

function parseDecoratedNumber(text: string): number | null {
    let rest = text
    let sign = 1

    if (rest.startsWith('+')) rest = rest.slice(1)
    else if (rest.startsWith('-')) { sign = -1; rest = rest.slice(1) }

    const currency = CURRENCY_RE.exec(rest)
    if (currency) rest = rest.slice(currency[0].length)

    let multiplier = 1
    const unit = UNIT_RE.exec(rest)
    if (unit) {
        rest = unit[1]
        if (unit[2] === '万' || unit[2] === '万元') multiplier = 1e4
        else if (unit[2] === '亿' || unit[2] === '亿元') multiplier = 1e8
    }

    const core = parsePlainNumber(rest)
    if (core === null) return null
    return sign * core * multiplier
}

/** 把带逗号 / 空格分组的数字写法转成 `Number()` 能读的形式；认不出来返回 null。 */
function normalizeNumericText(text: string): string | null {
    if (/^[-+]?\d{1,3}([ \u00a0\u202f]\d{3})+([.,]\d+)?$/.test(text)) {
        return text.replace(/[ \u00a0\u202f]/g, '').replace(',', '.')
    }

    if (!text.includes(',')) {
        // 两个以上的点号只可能是千分位（1.234.567）；单个点号仍当小数点。
        if (/^[-+]?\d{1,3}(\.\d{3}){2,}$/.test(text)) return text.replace(/\./g, '')
        return text
    }

    const lastComma = text.lastIndexOf(',')
    const lastDot = text.lastIndexOf('.')

    if (lastDot > lastComma) {
        // 1,234.56 —— 逗号是千分位，点是小数点
        if (!/^[-+]?\d{1,3}(,\d{3})*(\.\d+)?$/.test(text)) return null
        return text.replace(/,/g, '')
    }

    if (lastDot >= 0) {
        // 1.234,56 —— 点是千分位，逗号是小数点
        if (!/^[-+]?\d{1,3}(\.\d{3})*(,\d+)?$/.test(text)) return null
        return text.replace(/\./g, '').replace(',', '.')
    }

    // 只有逗号：三位一组当千分位，否则当小数点（3450,75）
    if (/^[-+]?\d{1,3}(,\d{3})+$/.test(text)) return text.replace(/,/g, '')
    if (/^[-+]?\d+,\d+$/.test(text)) return text.replace(',', '.')
    return null
}

/**
 * Cell comparison: numbers before text, numbers numerically, text with
 * `numeric: true` so `item2` sorts before `item10`.
 */
export function compareCells(a: string, b: string): number {
    const na = parseNumericCell(a)
    const nb = parseNumericCell(b)
    if (na !== null && nb !== null) return na - nb
    if (na !== null) return -1
    if (nb !== null) return 1
    return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' })
}

export interface RowFilter {
    /** Free text matched against every cell of the row. */
    query: string
    /** Per-column free text; index matches the column. Empty = no filter. */
    columnFilters: string[]
    sort: SortState | null
}

/** Filter then sort without mutating the parsed rows. */
export function filterAndSortRows(rows: string[][], filter: RowFilter): string[][] {
    const needle = filter.query.trim().toLowerCase()
    const columnNeedles = filter.columnFilters.map(f => f.trim().toLowerCase())

    let result = rows
    if (needle) {
        result = result.filter(row => row.some(cell => cell.toLowerCase().includes(needle)))
    }
    if (columnNeedles.some(f => f)) {
        result = result.filter(row =>
            columnNeedles.every((f, i) => !f || (row[i] ?? '').toLowerCase().includes(f)),
        )
    }
    if (filter.sort) {
        const { column, direction } = filter.sort
        const sign = direction === 'desc' ? -1 : 1
        result = [...result].sort((a, b) => sign * compareCells(a[column] ?? '', b[column] ?? ''))
    }
    return result
}

/**
 * Is this table a "data table"?
 *
 * 产品定的规则：第一列通常是标签，不参与；**其余列里只要有一列是纯数字**
 * （非空的格子全是数字，空格子算缺失），整张表就默认按数据表展示。
 *
 * 之前是「除第一列外每一格都必须是数字」，太严：一张「名称 / 数量 / 备注」
 * 的表只要备注里有一个字，就整表退化成简单表，数量列明明能排序、能合计。
 */
export function isNumericTable(headers: string[], rows: string[][]): boolean {
    if (headers.length < 2 || rows.length < 1) return false
    for (let c = 1; c < headers.length; c++) {
        let seen = 0
        let allNumeric = true
        for (const row of rows) {
            const raw = (row[c] ?? '').trim()
            if (raw === '') continue
            seen++
            if (parseNumericCell(raw) === null) {
                allNumeric = false
                break
            }
        }
        if (seen > 0 && allNumeric) return true
    }
    return false
}

/**
 * Columns whose non-empty sample values are mostly numbers get right-aligned
 * and tabular figures, the way a spreadsheet shows them.
 */
export function detectNumericColumns(rows: string[][], columnCount: number): boolean[] {
    const result: boolean[] = new Array(columnCount).fill(false)
    const SAMPLE_LIMIT = 200
    for (let c = 0; c < columnCount; c++) {
        let seen = 0
        let numeric = 0
        for (const row of rows) {
            const value = row[c]
            if (value === undefined || value.trim() === '') continue
            seen++
            if (parseNumericCell(value) !== null) numeric++
            if (seen >= SAMPLE_LIMIT) break
        }
        result[c] = seen > 0 && numeric / seen >= 0.8
    }
    return result
}

/** What the column menu shows about one column. */
export interface ColumnStats {
    rows: number
    empty: number
    distinct: number
    /** Null for text columns — a numeric summary is meaningless there. */
    sum: number | null
    avg: number | null
    min: number | null
    max: number | null
}

/** Aggregate one column over every row (not the filtered subset, so the numbers
 *  do not move while the user is typing a filter). */
export function computeColumnStats(rows: string[][], column: number, numeric: boolean): ColumnStats {
    const values: string[] = []
    let empty = 0
    for (const row of rows) {
        const value = row[column] ?? ''
        if (value.trim() === '') empty++
        else values.push(value)
    }
    const distinct = new Set(values).size

    if (!numeric) {
        return { rows: rows.length, empty, distinct, sum: null, avg: null, min: null, max: null }
    }

    let sum = 0
    let min = Number.POSITIVE_INFINITY
    let max = Number.NEGATIVE_INFINITY
    let counted = 0
    for (const value of values) {
        const n = parseNumericCell(value)
        if (n === null) continue
        sum += n
        if (n < min) min = n
        if (n > max) max = n
        counted++
    }
    if (counted === 0) {
        return { rows: rows.length, empty, distinct, sum: null, avg: null, min: null, max: null }
    }
    return {
        rows: rows.length,
        empty,
        distinct,
        sum,
        avg: sum / counted,
        min,
        max,
    }
}

/** Distinct values of one column with their row counts, for the value filter.
 *  Sorted the same way the table sorts (numbers numerically, text naturally). */
export function columnValueCounts(rows: string[][], column: number): Array<{ value: string; count: number }> {
    const counts = new Map<string, number>()
    for (const row of rows) {
        const value = row[column] ?? ''
        counts.set(value, (counts.get(value) ?? 0) + 1)
    }
    return [...counts.entries()]
        .map(([value, count]) => ({ value, count }))
        .sort((a, b) => compareCells(a.value, b.value))
}
