/**
 * 从 <table> DOM 里取表头与数据行。
 *
 * 单独放一个叶子模块：聊天侧的拆块逻辑要用它，而 useCodeBlockHeader 会
 * 间接引入 app 的 i18n 单例，测试里对 vue-i18n 做局部 mock 时会炸。
 */

/**
 * Extract table data: header row (first row / thead) and remaining data rows.
 */
export function extractTableData(table: HTMLTableElement): { headers: string[]; rows: string[][] } {
    const rows: string[][] = []

    // Determine header row and data rows
    const thead = table.querySelector('thead')
    const headers: string[] = []
    // eslint-disable-next-line no-useless-assignment -- headerRow is used below in the for-loop skip check
    let headerRow: HTMLTableRowElement | null = null

    if (thead) {
        headerRow = thead.querySelector('tr')
        thead.querySelectorAll('th').forEach(th => headers.push((th.textContent || '').trim()))
    } else {
        // No <thead>, use first <tr> as header
        headerRow = table.querySelector('tr')
        if (headerRow) {
            headerRow.querySelectorAll('th, td').forEach(cell => headers.push((cell.textContent || '').trim()))
        }
    }

    // Extract data rows: all <tr> except the header row
    const allRows = table.querySelectorAll('tr')
    for (const tr of allRows) {
        if (tr === headerRow) continue
        const cells: string[] = []
        tr.querySelectorAll('td, th').forEach(cell => cells.push((cell.textContent || '').trim()))
        rows.push(cells)
    }

    return { headers, rows }
}

