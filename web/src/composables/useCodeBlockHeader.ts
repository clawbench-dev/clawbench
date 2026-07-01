import { copyText } from '@/utils/clipboard.ts'
import { gt } from '@/composables/useLocale'

// ── SVG icons (inline, same pattern as FILE_OPEN_ICON_SVG) ──────────────────

const COPY_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'

const WRAP_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-3"/><path d="M18 15l-3 3 3 3"/><path d="M3 18h7"/></svg>'

// ── String-level annotation ──────────────────────────────────────────────────

/**
 * Annotate code blocks in an HTML string with header bars containing
 * language label, copy button, and word-wrap toggle.
 *
 * Uses DOMParser + TreeWalker (same pattern as useFilePathAnnotation).
 * Must run AFTER DOMPurify.sanitize() and BEFORE file path annotation.
 */
export function annotateCodeBlockHeaders(html: string): string {
    if (!html) return html

    const doc = new DOMParser().parseFromString(html, 'text/html')

    // Find all <pre> elements that contain a <code> child
    const pres = doc.querySelectorAll('pre')
    for (const pre of pres) {
        // Skip mermaid blocks
        if (pre.classList.contains('mermaid')) continue
        // Skip if already wrapped (idempotent guard)
        if (pre.parentElement?.classList.contains('code-block-wrapper')) continue

        const code = pre.querySelector('code')
        if (!code) continue

        // Extract language from class="language-X"
        let lang = ''
        for (const cls of code.classList) {
            if (cls.startsWith('language-')) {
                lang = cls.slice(9)
                break
            }
        }

        // Build wrapper div
        const wrapper = doc.createElement('div')
        wrapper.className = 'code-block-wrapper'

        // Build header div
        const header = doc.createElement('div')
        header.className = 'code-block-header'

        // Language label
        const langSpan = doc.createElement('span')
        langSpan.className = 'code-block-lang'
        langSpan.textContent = lang || ''
        header.appendChild(langSpan)

        // Actions group (right side)
        const actions = doc.createElement('span')
        actions.className = 'code-block-header-actions'

        // Copy button
        const copyBtn = doc.createElement('button')
        copyBtn.className = 'code-block-copy-btn'
        copyBtn.setAttribute('data-action', 'copy')
        copyBtn.setAttribute('title', gt('common.copy'))
        copyBtn.setAttribute('aria-label', gt('common.copy'))
        copyBtn.setAttribute('type', 'button')
        copyBtn.innerHTML = COPY_ICON_SVG
        actions.appendChild(copyBtn)

        // Wrap toggle button
        const wrapBtn = doc.createElement('button')
        wrapBtn.className = 'code-block-wrap-btn'
        wrapBtn.setAttribute('data-action', 'wrap')
        wrapBtn.setAttribute('title', gt('codeBlock.wrapOff'))
        wrapBtn.setAttribute('aria-label', gt('codeBlock.wrapOff'))
        wrapBtn.setAttribute('type', 'button')
        wrapBtn.innerHTML = WRAP_ICON_SVG
        actions.appendChild(wrapBtn)

        header.appendChild(actions)

        // Insert wrapper before <pre>, move <pre> inside wrapper
        pre.parentNode?.insertBefore(wrapper, pre)
        wrapper.appendChild(header)
        wrapper.appendChild(pre)
    }

    return doc.body.innerHTML
}

// ── Event delegation handler ────────────────────────────────────────────────

/**
 * Handle code block header button clicks via event delegation.
 * Call from any container click handler (ChatMessageList, MarkdownPreview, TaskOverviewTab).
 *
 * @returns true if the click was handled (caller should return early)
 */
export function handleCodeBlockClick(event: MouseEvent): boolean {
    const target = event.target as HTMLElement
    const btn = target.closest('.code-block-copy-btn, .code-block-wrap-btn')
    if (!btn) return false

    event.preventDefault()
    event.stopPropagation()

    const wrapper = btn.closest('.code-block-wrapper')
    if (!wrapper) return true

    const pre = wrapper.querySelector('pre')
    if (!pre) return true

    const action = btn.getAttribute('data-action')

    if (action === 'copy') {
        if (btn.classList.contains('is-copied')) return true // already showing feedback
        const code = pre.querySelector('code')
        const text = (code || pre).textContent || ''
        copyText(text)
        // Show "Copied!" on the button briefly
        const originalTitle = btn.getAttribute('title') || ''
        const originalAriaLabel = btn.getAttribute('aria-label') || ''
        btn.innerHTML = `<span class="code-block-copied-text">${gt('common.copied')}</span>`
        btn.classList.add('is-copied')
        btn.setAttribute('title', gt('common.copied'))
        btn.setAttribute('aria-label', gt('common.copied'))
        setTimeout(() => {
            btn.innerHTML = COPY_ICON_SVG // restore from constant to avoid race condition
            btn.classList.remove('is-copied')
            btn.setAttribute('title', originalTitle)
            btn.setAttribute('aria-label', originalAriaLabel)
        }, 1500)
    } else if (action === 'wrap') {
        wrapper.classList.toggle('word-wrap')
        btn.classList.toggle('is-wrapped')
        const isWrapped = wrapper.classList.contains('word-wrap')
        btn.setAttribute('title', isWrapped ? gt('codeBlock.wrapOn') : gt('codeBlock.wrapOff'))
        btn.setAttribute('aria-label', isWrapped ? gt('codeBlock.wrapOn') : gt('codeBlock.wrapOff'))
    }

    return true
}

// ── Table block header (same pattern as code block header) ──────────────────

const TABLE_COPY_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'

const TABLE_WRAP_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-3"/><path d="M18 15l-3 3 3 3"/><path d="M3 18h7"/></svg>'

/**
 * Annotate table blocks in an HTML string with header bars containing
 * "Table" label, copy button, and word-wrap toggle.
 *
 * Must run AFTER table-wrap insertion and injectTableRowAttrs().
 */
export function annotateTableBlockHeaders(html: string): string {
    if (!html) return html

    const doc = new DOMParser().parseFromString(html, 'text/html')

    const tableWraps = doc.querySelectorAll('.table-wrap')
    for (const tableWrap of tableWraps) {
        // Skip if already wrapped (idempotent guard)
        if (tableWrap.parentElement?.classList.contains('table-block-wrapper')) continue

        // Build wrapper div
        const wrapper = doc.createElement('div')
        wrapper.className = 'table-block-wrapper'

        // Build header div
        const header = doc.createElement('div')
        header.className = 'table-block-header'

        // Label
        const label = doc.createElement('span')
        label.className = 'table-block-label'
        label.textContent = gt('tableBlock.label')
        header.appendChild(label)

        // Actions group (right side)
        const actions = doc.createElement('span')
        actions.className = 'table-block-header-actions'

        // Copy button
        const copyBtn = doc.createElement('button')
        copyBtn.className = 'table-block-copy-btn'
        copyBtn.setAttribute('data-action', 'copy')
        copyBtn.setAttribute('title', gt('common.copy'))
        copyBtn.setAttribute('aria-label', gt('common.copy'))
        copyBtn.setAttribute('type', 'button')
        copyBtn.innerHTML = TABLE_COPY_ICON_SVG
        actions.appendChild(copyBtn)

        // Wrap toggle button
        const wrapBtn = doc.createElement('button')
        wrapBtn.className = 'table-block-wrap-btn'
        wrapBtn.setAttribute('data-action', 'wrap')
        wrapBtn.setAttribute('title', gt('tableBlock.wrapOff'))
        wrapBtn.setAttribute('aria-label', gt('tableBlock.wrapOff'))
        wrapBtn.setAttribute('type', 'button')
        wrapBtn.innerHTML = TABLE_WRAP_ICON_SVG
        actions.appendChild(wrapBtn)

        header.appendChild(actions)

        // Insert wrapper before .table-wrap, move .table-wrap inside wrapper
        tableWrap.parentNode?.insertBefore(wrapper, tableWrap)
        wrapper.appendChild(header)
        wrapper.appendChild(tableWrap)
    }

    return doc.body.innerHTML
}

/**
 * Extract table data as tab-separated values (TSV) for Word-pasteable copying.
 */
function tableToTSV(table: HTMLTableElement): string {
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

    // Build TSV: header row + data rows
    const lines: string[] = []
    if (headers.length > 0) lines.push(headers.join('\t'))
    for (const row of rows) lines.push(row.join('\t'))
    return lines.join('\n')
}

/**
 * Copy table to clipboard in both HTML and plain-text formats.
 * HTML format ensures Word/Excel/Google Sheets paste as a proper table.
 * Falls back to plain TSV if ClipboardItem is unavailable.
 */
function copyTableToClipboard(table: HTMLTableElement): void {
    const tsv = tableToTSV(table)

    // Try ClipboardItem with text/html for rich paste into Word/Excel
    if (navigator.clipboard?.write && typeof ClipboardItem !== 'undefined') {
        // Build a clean HTML table from the DOM table's text content
        const html = buildCleanTableHTML(table)
        const clipboardItem = new ClipboardItem({
            'text/html': new Blob([html], { type: 'text/html' }),
            'text/plain': new Blob([tsv], { type: 'text/plain' }),
        })
        navigator.clipboard.write([clipboardItem]).catch(() => {
            // Fallback to plain TSV
            copyText(tsv)
        })
    } else {
        copyText(tsv)
    }
}

/**
 * Build a clean, self-contained HTML table string from a DOM table.
 * Strips classes, data attributes, event hooks — only text content and basic structure.
 */
function buildCleanTableHTML(table: HTMLTableElement): string {
    const lines: string[] = ['<table>']
    const thead = table.querySelector('thead')
    if (thead) {
        lines.push('<thead>')
        for (const tr of thead.querySelectorAll('tr')) {
            lines.push('<tr>')
            for (const cell of tr.querySelectorAll('th, td')) {
                const tag = cell.tagName === 'TH' ? 'th' : 'td'
                lines.push(`<${tag}>${escapeCellText(cell.textContent || '')}</${tag}>`)
            }
            lines.push('</tr>')
        }
        lines.push('</thead>')
    }
    const tbody = table.querySelector('tbody')
    if (tbody) {
        lines.push('<tbody>')
        for (const tr of tbody.querySelectorAll('tr')) {
            lines.push('<tr>')
            for (const cell of tr.querySelectorAll('td, th')) {
                const tag = cell.tagName === 'TH' ? 'th' : 'td'
                lines.push(`<${tag}>${escapeCellText(cell.textContent || '')}</${tag}>`)
            }
            lines.push('</tr>')
        }
        lines.push('</tbody>')
    }
    // If no thead/tbody, iterate all rows directly
    if (!thead && !tbody) {
        for (const tr of table.querySelectorAll('tr')) {
            lines.push('<tr>')
            for (const cell of tr.querySelectorAll('th, td')) {
                const tag = cell.tagName === 'TH' ? 'th' : 'td'
                lines.push(`<${tag}>${escapeCellText(cell.textContent || '')}</${tag}>`)
            }
            lines.push('</tr>')
        }
    }
    lines.push('</table>')
    return lines.join('')
}

function escapeCellText(text: string): string {
    return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/**
 * Handle table block header button clicks via event delegation.
 * Call from any container click handler (ChatMessageList, MarkdownPreview, etc.).
 *
 * @returns true if the click was handled (caller should return early)
 */
export function handleTableBlockClick(event: MouseEvent): boolean {
    const target = event.target as HTMLElement
    const btn = target.closest('.table-block-copy-btn, .table-block-wrap-btn')
    if (!btn) return false

    event.preventDefault()
    event.stopPropagation()

    const wrapper = btn.closest('.table-block-wrapper')
    if (!wrapper) return true

    const action = btn.getAttribute('data-action')

    if (action === 'copy') {
        if (btn.classList.contains('is-copied')) return true
        const table = wrapper.querySelector('table')
        if (!table) return true
        copyTableToClipboard(table as HTMLTableElement)
        // Show "Copied!" on the button briefly
        const originalTitle = btn.getAttribute('title') || ''
        const originalAriaLabel = btn.getAttribute('aria-label') || ''
        btn.innerHTML = `<span class="table-block-copied-text">${gt('common.copied')}</span>`
        btn.classList.add('is-copied')
        btn.setAttribute('title', gt('common.copied'))
        btn.setAttribute('aria-label', gt('common.copied'))
        setTimeout(() => {
            btn.innerHTML = TABLE_COPY_ICON_SVG
            btn.classList.remove('is-copied')
            btn.setAttribute('title', originalTitle)
            btn.setAttribute('aria-label', originalAriaLabel)
        }, 1500)
    } else if (action === 'wrap') {
        wrapper.classList.toggle('word-wrap')
        btn.classList.toggle('is-wrapped')
        const isWrapped = wrapper.classList.contains('word-wrap')
        btn.setAttribute('title', isWrapped ? gt('tableBlock.wrapOn') : gt('tableBlock.wrapOff'))
        btn.setAttribute('aria-label', isWrapped ? gt('tableBlock.wrapOn') : gt('tableBlock.wrapOff'))
    }

    return true
}

export function useCodeBlockHeader() {
    return { annotateCodeBlockHeaders, handleCodeBlockClick, annotateTableBlockHeaders, handleTableBlockClick }
}
