import { copyText } from '@/utils/clipboard.ts'
import { gt } from '@/composables/useLocale'
import { ATTACH_BADGE_SVG } from '@/utils/attachSvg'
import { isShareMode } from '@/share/shareMode'

// ── SVG icons (inline, same pattern as FILE_OPEN_ICON_SVG) ──────────────────

const COPY_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'

const WRAP_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-3"/><path d="M18 15l-3 3 3 3"/><path d="M3 18h7"/></svg>'

/** Build the "attach to chat" header button (paperclip). Skipped entirely in
 *  share mode (a public share has no chat to attach to); in chat the button is
 *  injected but hidden by CSS and inert because no .markdown-body[data-file-path]
 *  ancestor exists. */
function makeAttachButton(doc: Document, cls: string, i18nKey: string): HTMLButtonElement | null {
    if (isShareMode()) return null
    const btn = doc.createElement('button')
    btn.className = cls
    btn.setAttribute('data-action', 'attach')
    const label = gt(i18nKey)
    btn.setAttribute('title', label)
    btn.setAttribute('aria-label', label)
    btn.setAttribute('type', 'button')
    btn.innerHTML = ATTACH_BADGE_SVG
    return btn
}

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

        // Build wrapper div (default: word-wrap on)
        const wrapper = doc.createElement('div')
        wrapper.className = 'code-block-wrapper word-wrap'

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

        // Attach-to-chat button (referenced md line range; file-preview only)
        const attachBtn = makeAttachButton(doc, 'code-block-attach-btn', 'chat.attach.attachCodeToChat')
        if (attachBtn) actions.appendChild(attachBtn)

        // Wrap toggle button (default: wrap on, so button shows "wrapOn" state)
        const wrapBtn = doc.createElement('button')
        wrapBtn.className = 'code-block-wrap-btn is-wrapped'
        wrapBtn.setAttribute('data-action', 'wrap')
        wrapBtn.setAttribute('title', gt('codeBlock.wrapOn'))
        wrapBtn.setAttribute('aria-label', gt('codeBlock.wrapOn'))
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

    // Clicking a code block button dismisses any open table copy menus
    closeAllTableBlockMenus()

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

const TABLE_CHEVRON_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="10" height="10"><path d="M6 9l6 6 6-6"/></svg>'

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

        // Build wrapper div (default: word-wrap on)
        const wrapper = doc.createElement('div')
        wrapper.className = 'table-block-wrapper word-wrap'

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

        // Copy dropdown button (opens menu with Markdown/HTML/TSV options)
        const copyDropdown = doc.createElement('span')
        copyDropdown.className = 'table-block-copy-dropdown'
        const copyBtn = doc.createElement('button')
        copyBtn.className = 'table-block-copy-btn'
        copyBtn.setAttribute('data-action', 'open-copy-menu')
        copyBtn.setAttribute('title', gt('tableBlock.copy'))
        copyBtn.setAttribute('aria-label', gt('tableBlock.copy'))
        copyBtn.setAttribute('aria-haspopup', 'true')
        copyBtn.setAttribute('aria-expanded', 'false')
        copyBtn.setAttribute('type', 'button')
        copyBtn.innerHTML = TABLE_COPY_ICON_SVG + TABLE_CHEVRON_ICON_SVG
        copyDropdown.appendChild(copyBtn)

        // Copy menu (popup with three formats)
        const copyMenu = doc.createElement('div')
        copyMenu.className = 'table-block-copy-menu'
        copyMenu.setAttribute('role', 'menu')
        copyMenu.style.display = 'none'

        const copyMenuItems = [
            { action: 'copy-md', label: gt('tableBlock.copyMarkdown') },
            { action: 'copy-html', label: gt('tableBlock.copyHtml') },
            { action: 'copy-tsv', label: gt('tableBlock.copyTsv') },
        ]
        for (const item of copyMenuItems) {
            const menuItem = doc.createElement('button')
            menuItem.className = 'table-block-copy-menu-item'
            menuItem.setAttribute('data-action', item.action)
            menuItem.setAttribute('role', 'menuitem')
            menuItem.setAttribute('type', 'button')
            menuItem.textContent = item.label
            copyMenu.appendChild(menuItem)
        }

        copyDropdown.appendChild(copyMenu)
        actions.appendChild(copyDropdown)

        // Attach-to-chat button (referenced md line range; file-preview only)
        const attachBtn = makeAttachButton(doc, 'table-block-attach-btn', 'chat.attach.attachTableToChat')
        if (attachBtn) actions.appendChild(attachBtn)

        // Wrap toggle button (default: wrap on, so button shows "wrapOn" state)
        const wrapBtn = doc.createElement('button')
        wrapBtn.className = 'table-block-wrap-btn is-wrapped'
        wrapBtn.setAttribute('data-action', 'wrap')
        wrapBtn.setAttribute('title', gt('tableBlock.wrapOn'))
        wrapBtn.setAttribute('aria-label', gt('tableBlock.wrapOn'))
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
 * Extract table data: header row (first row / thead) and remaining data rows.
 */
function extractTableData(table: HTMLTableElement): { headers: string[]; rows: string[][] } {
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

/**
 * Extract table data as tab-separated values (TSV) for spreadsheet-pasteable copying.
 */
function tableToTSV(table: HTMLTableElement): string {
    const { headers, rows } = extractTableData(table)

    // Build TSV: header row + data rows
    const lines: string[] = []
    if (headers.length > 0) lines.push(headers.join('\t'))
    for (const row of rows) lines.push(row.join('\t'))
    return lines.join('\n')
}

/**
 * Escape a cell value for embedding in a GFM table:
 * pipes must be escaped and newlines collapsed.
 */
function escapeMarkdownCell(text: string): string {
    return text.replace(/\|/g, '\\|').replace(/\r?\n/g, '<br>')
}

/**
 * Extract table data as GitHub-Flavored Markdown table syntax.
 * Includes header separator row (:-:, :--, --: alignment) using the same
 * alignment hints as the rendered table's th elements when available.
 */
function tableToMarkdown(table: HTMLTableElement): string {
    const thead = table.querySelector('thead')
    const headers: string[] = []
    const aligns: string[] = []

    if (thead) {
        thead.querySelectorAll('th').forEach(th => {
            headers.push(escapeMarkdownCell((th.textContent || '').trim()))
            aligns.push(tableCellAlignment(th))
        })
    } else {
        const headerRow = table.querySelector('tr')
        if (headerRow) {
            headerRow.querySelectorAll('th, td').forEach(cell => {
                headers.push(escapeMarkdownCell((cell.textContent || '').trim()))
                aligns.push(tableCellAlignment(cell))
            })
        }
    }

    const { rows } = extractTableData(table)

    const lines: string[] = []
    if (headers.length > 0) {
        lines.push(`| ${headers.join(' | ')} |`)
        lines.push(`| ${aligns.join(' | ')} |`)
    }
    for (const row of rows) {
        lines.push(`| ${row.map(escapeMarkdownCell).join(' | ')} |`)
    }
    return lines.join('\n')
}

/**
 * Map a table cell's horizontal alignment (from inline style) to a GFM
 * separator marker: :--- (left), ---: (right), :---: (center), --- (default).
 */
function tableCellAlignment(cell: Element): string {
    const style = (cell as HTMLElement).style?.textAlign
    if (style === 'left') return ':---'
    if (style === 'right') return '---:'
    if (style === 'center') return ':---:'
    return '---'
}

/**
 * Copy table content to the clipboard in a specific format.
 * - markdown: GFM table syntax
 * - html: clean <table> HTML (rich paste into Word/Excel)
 * - tsv: tab-separated plain text
 */
function copyTableContent(table: HTMLTableElement, format: 'markdown' | 'html' | 'tsv'): void {
    if (format === 'markdown') {
        copyText(tableToMarkdown(table))
        return
    }
    if (format === 'tsv') {
        copyText(tableToTSV(table))
        return
    }
    // html: prefer ClipboardItem so Word/Excel paste a proper table
    const html = buildCleanTableHTML(table)
    if (navigator.clipboard?.write && typeof ClipboardItem !== 'undefined') {
        const clipboardItem = new ClipboardItem({
            'text/html': new Blob([html], { type: 'text/html' }),
            'text/plain': new Blob([tableToTSV(table)], { type: 'text/plain' }),
        })
        navigator.clipboard.write([clipboardItem]).catch(() => {
            // Fallback to plain TSV
            copyText(tableToTSV(table))
        })
    } else {
        copyText(html)
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
/**
 * Handle table block header button clicks via event delegation.
 * Call from any container click handler (ChatMessageList, MarkdownPreview, etc.).
 *
 * Supports:
 *  - data-action="open-copy-menu" → toggle the copy format dropdown
 *  - data-action="copy-md|copy-html|copy-tsv" → copy the table in that format
 *  - data-action="wrap" → toggle word-wrap
 *
 * @returns true if the click was handled (caller should return early)
 */
export function handleTableBlockClick(event: MouseEvent): boolean {
    const target = event.target as HTMLElement
    const btn = target.closest<HTMLElement>('.table-block-copy-btn, .table-block-wrap-btn, .table-block-copy-menu-item')
    if (!btn) return false

    event.preventDefault()
    event.stopPropagation()

    const wrapper = btn.closest('.table-block-wrapper')
    if (!wrapper) return true

    const action = btn.getAttribute('data-action')

    if (action === 'open-copy-menu') {
        // Toggle this block's copy menu; close all other blocks' menus first
        closeAllTableBlockMenus(btn)
        const menu = btn.nextElementSibling as HTMLElement | null
        const isOpen = menu && menu.classList.contains('is-open')
        setTableMenuOpen(btn, menu, !isOpen)
        return true
    }

    if (action === 'copy-md' || action === 'copy-html' || action === 'copy-tsv') {
        const table = wrapper.querySelector('table')
        if (!table) return true
        const format = action === 'copy-md' ? 'markdown' : action === 'copy-html' ? 'html' : 'tsv'
        copyTableContent(table as HTMLTableElement, format)
        // Show "Copied!" on the menu item briefly
        const originalText = btn.textContent || ''
        btn.innerHTML = `<span class="table-block-copied-text">${gt('tableBlock.copied')}</span>`
        btn.classList.add('is-copied')
        setTimeout(() => {
            btn.textContent = originalText
            btn.classList.remove('is-copied')
        }, 1500)
        // Close the menu after copying
        const menu = btn.closest<HTMLElement>('.table-block-copy-menu')
        if (menu) {
            const trigger = menu.previousElementSibling as HTMLElement | null
            setTableMenuOpen(trigger, menu, false)
        }
        return true
    }

    if (action === 'wrap') {
        // Wrap toggle dismisses any open copy menus
        closeAllTableBlockMenus()
        const isWrapped = wrapper.classList.contains('word-wrap')
        if (isWrapped) {
            wrapper.classList.remove('word-wrap')
            wrapper.classList.add('no-wrap')
        } else {
            wrapper.classList.remove('no-wrap')
            wrapper.classList.add('word-wrap')
        }
        btn.classList.toggle('is-wrapped')
        const nowWrapped = wrapper.classList.contains('word-wrap')
        btn.setAttribute('title', nowWrapped ? gt('tableBlock.wrapOn') : gt('tableBlock.wrapOff'))
        btn.setAttribute('aria-label', nowWrapped ? gt('tableBlock.wrapOn') : gt('tableBlock.wrapOff'))
    }

    return true
}

/**
 * Close every open table copy menu across the document. Called from
 * global click handlers so clicking elsewhere dismisses open menus.
 */
export function closeAllTableBlockMenus(exceptBtn?: HTMLElement | null): void {
    const menus = document.querySelectorAll('.table-block-copy-menu.is-open')
    for (const el of Array.from(menus)) {
        const menu = el as HTMLElement
        const trigger = menu.previousElementSibling as HTMLElement | null
        if (exceptBtn && trigger === exceptBtn) continue
        setTableMenuOpen(trigger, menu, false)
    }
}

function setTableMenuOpen(trigger: HTMLElement | null, menu: HTMLElement | null, open: boolean): void {
    if (!menu) return
    if (open) {
        menu.classList.add('is-open')
        menu.style.display = 'block'
    } else {
        menu.classList.remove('is-open')
        menu.style.display = 'none'
    }
    if (trigger) {
        trigger.setAttribute('aria-expanded', open ? 'true' : 'false')
    }
}

export function useCodeBlockHeader() {
    return { annotateCodeBlockHeaders, handleCodeBlockClick, annotateTableBlockHeaders, handleTableBlockClick, closeAllTableBlockMenus }
}
