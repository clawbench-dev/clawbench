/**
 * 从 <table> DOM 里取表头与数据行。
 *
 * 单独放一个叶子模块：聊天侧的拆块逻辑要用它，而 useCodeBlockHeader 会
 * 间接引入 app 的 i18n 单例，测试里对 vue-i18n 做局部 mock 时会炸。
 */

import katex from 'katex'
import { escapeHtml } from './html'

/** Display markup keyed by the original row, independent of sorting and paging. */
export type TableMathCells = Map<string[], string[]>

function readCell(cell: Element, row: string[], mathCells?: TableMathCells): void {
    if (!cell.querySelector('.katex')) {
        row.push((cell.textContent ?? '').trim())
        return
    }

    let html = ''
    function read(node: Node): string {
        if (node.nodeType === Node.TEXT_NODE) {
            const text = node.textContent ?? ''
            if (mathCells) html += escapeHtml(text)
            return text
        }
        if (node instanceof Element && node.classList.contains('katex')) {
            const math = node.querySelector('.katex-mathml math')
            // DOMPurify unwraps the annotation but retains its TeX as the
            // final text node. File previews can still have the annotation.
            const source = math?.querySelector('annotation[encoding="application/x-tex"]')?.textContent
                ?? (math?.lastChild?.nodeType === Node.TEXT_NODE ? math.lastChild.textContent : null)
            if (source != null) {
                if (mathCells) html += katex.renderToString(source, {
                    displayMode: node.parentElement?.classList.contains('katex-display') ?? false,
                    throwOnError: false,
                    trust: false,
                })
                return source
            }
        }
        return Array.from(node.childNodes, read).join('')
    }
    const text = read(cell).trim()
    if (mathCells) {
        const display = mathCells.get(row) ?? []
        display[row.length] = html
        mathCells.set(row, display)
    }
    row.push(text)
}

export function extractTableData(table: HTMLTableElement, mathCells?: TableMathCells): { headers: string[]; rows: string[][] } {
    const rows: string[][] = []

    // Determine header row and data rows
    const thead = table.querySelector('thead')
    const headers: string[] = []
    // eslint-disable-next-line no-useless-assignment -- headerRow is used below in the for-loop skip check
    let headerRow: HTMLTableRowElement | null = null

    if (thead) {
        headerRow = thead.querySelector('tr')
        thead.querySelectorAll('th').forEach(th => readCell(th, headers, mathCells))
    } else {
        // No <thead>, use first <tr> as header
        headerRow = table.querySelector('tr')
        if (headerRow) {
            headerRow.querySelectorAll('th, td').forEach(cell => readCell(cell, headers, mathCells))
        }
    }

    // Extract data rows: all <tr> except the header row
    const allRows = table.querySelectorAll('tr')
    for (const tr of allRows) {
        if (tr === headerRow) continue
        const cells: string[] = []
        tr.querySelectorAll('td, th').forEach(cell => readCell(cell, cells, mathCells))
        rows.push(cells)
    }

    return { headers, rows }
}

