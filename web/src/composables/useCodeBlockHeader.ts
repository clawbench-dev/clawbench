import { copyText } from '@/utils/clipboard.ts'
import { COPY_ICON_SVG, copyWithFlash } from '@/utils/copyButton.ts'
import { placeFixedAt } from '@/utils/fixedPosition.ts'
import { gt } from '@/composables/useLocale'
import { ATTACH_BADGE_SVG } from '@/utils/attachSvg'
import { isShareMode } from '@/share/shareMode'
import { openTableLandscape } from '@/composables/useTableLandscape.ts'
import { getDefaultTableWrap } from '@/utils/tableWrapPref.ts'

// ── SVG icons (inline, same pattern as FILE_OPEN_ICON_SVG) ──────────────────
// The copy/check glyphs are shared with every other hand-built copy button and
// with components/common/CopyButton.vue — see utils/copyButton.ts. Do not
// re-declare them here; a second copy is how they drift apart.

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
    annotateCodeBlockHeadersIn(doc)
    return doc.body.innerHTML
}

/**
 * Annotate code-block headers inside an already-parsed Document, in place.
 *
 * Split out so the markdown pipeline can run several annotation steps over ONE
 * parsed document instead of each step paying its own `parseFromString` +
 * `body.innerHTML` round trip.
 */
export function annotateCodeBlockHeadersIn(doc: Document): void {

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
    const btn = target.closest<HTMLElement>('.code-block-copy-btn, .code-block-wrap-btn')
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
        const code = pre.querySelector('code')
        const text = (code || pre).textContent || ''
        // Shared path: guards re-entry, copies, and swaps the glyph for a check
        // (see utils/copyButton.ts).
        copyWithFlash(btn, text)
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

const TABLE_CHEVRON_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="10" height="10"><path d="M6 9l6 6 6-6"/></svg>'

const TABLE_WRAP_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14"><path d="M3 6h18"/><path d="M3 12h15a3 3 0 1 1 0 6h-3"/><path d="M18 15l-3 3 3 3"/><path d="M3 18h7"/></svg>'

/**
 * A portrait phone inside a three-quarter rotation arc — the "auto-rotate"
 * convention Android uses for the same gesture. Drawn with the same stroke
 * weight / caps as the lucide glyphs beside it (see utils/copyButton.ts for why
 * hand-built markup mirrors lucide paths), so it does not stand out as a
 * different icon family.
 */
const TABLE_LANDSCAPE_ICON_SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="5.5" height="12.5" x="9.25" y="5.75" rx="1.6"/><path d="M16.4 4.3A9 9 0 1 0 16.4 19.7"/><path d="M14.2 1.9 16.7 4.3 14.3 6.7"/></svg>'

/**
 * Annotate table blocks in an HTML string with header bars containing
 * "Table" label, copy button, and word-wrap toggle.
 *
 * Must run AFTER table-wrap insertion and injectTableRowAttrs().
 */
export function annotateTableBlockHeaders(html: string): string {
    if (!html) return html

    const doc = new DOMParser().parseFromString(html, 'text/html')
    annotateTableBlockHeadersIn(doc)
    return doc.body.innerHTML
}

/**
 * Annotate table-block headers inside an already-parsed Document, in place.
 *
 * Split out so the markdown pipeline can run several annotation steps over ONE
 * parsed document instead of each step paying its own `parseFromString` +
 * `body.innerHTML` round trip.
 */
export function annotateTableBlockHeadersIn(doc: Document): void {

    const tableWraps = doc.querySelectorAll('.table-wrap')
    for (const tableWrap of tableWraps) {
        // Skip if already wrapped (idempotent guard)
        if (tableWrap.parentElement?.classList.contains('table-block-wrapper')) continue

        // Build wrapper div. The starting wrap mode is the user's default
        // (设置 → 外观 → 显示); the per-table toggle still overrides it.
        const wrapDefault = getDefaultTableWrap()
        const wrapper = doc.createElement('div')
        wrapper.className = `table-block-wrapper ${wrapDefault ? 'word-wrap' : 'no-wrap'}`

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
        copyBtn.innerHTML = COPY_ICON_SVG + TABLE_CHEVRON_ICON_SVG
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

        // Wrap toggle button, starting in the state the default gave it.
        const wrapBtn = doc.createElement('button')
        wrapBtn.className = `table-block-wrap-btn${wrapDefault ? ' is-wrapped' : ''}`
        wrapBtn.setAttribute('data-action', 'wrap')
        wrapBtn.setAttribute('title', gt(wrapDefault ? 'tableBlock.wrapOn' : 'tableBlock.wrapOff'))
        wrapBtn.setAttribute('aria-label', gt(wrapDefault ? 'tableBlock.wrapOn' : 'tableBlock.wrapOff'))
        wrapBtn.setAttribute('type', 'button')
        wrapBtn.innerHTML = TABLE_WRAP_ICON_SVG
        actions.appendChild(wrapBtn)

        // Landscape (full-screen) view — touch/narrow only; the CSS hides it on
        // a desktop where the table is already readable in place.
        const landscapeBtn = doc.createElement('button')
        landscapeBtn.className = 'table-block-landscape-btn'
        landscapeBtn.setAttribute('data-action', 'landscape')
        landscapeBtn.setAttribute('title', gt('tableBlock.landscape'))
        landscapeBtn.setAttribute('aria-label', gt('tableBlock.landscape'))
        landscapeBtn.setAttribute('type', 'button')
        landscapeBtn.innerHTML = TABLE_LANDSCAPE_ICON_SVG
        actions.appendChild(landscapeBtn)

        header.appendChild(actions)

        // Insert wrapper before .table-wrap, move .table-wrap inside wrapper
        tableWrap.parentNode?.insertBefore(wrapper, tableWrap)
        wrapper.appendChild(header)
        wrapper.appendChild(tableWrap)
    }
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
    const btn = target.closest<HTMLElement>('.table-block-copy-btn, .table-block-wrap-btn, .table-block-landscape-btn, .table-block-copy-menu-item')
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
        // Place it only when opening — a closing menu keeps its stale
        // coordinates harmlessly (it is display:none).
        if (!isOpen && menu) positionTableMenu(btn, menu)
        return true
    }

    if (action === 'copy-md' || action === 'copy-html' || action === 'copy-tsv') {
        const table = wrapper.querySelector('table')
        if (!table) return true
        const format = action === 'copy-md' ? 'markdown' : action === 'copy-html' ? 'html' : 'tsv'
        copyTableContent(table as HTMLTableElement, format)
        // Menu items keep their text — the label IS the format identity
        // (Markdown / HTML / TSV), so replacing it with a check would hide
        // which format was copied. The tint alone carries the feedback, and
        // the menu closes immediately anyway.
        btn.classList.add('is-copied')
        setTimeout(() => {
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

    if (action === 'landscape') {
        // The overlay is a separate layer, so an open copy menu would be left
        // floating behind it with nothing to anchor to.
        closeAllTableBlockMenus()
        openTableLandscape(wrapper, btn)
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
    syncTableMenuRepositionListener()
}

/**
 * Place an open copy menu under its trigger.
 *
 * The menu is `position: fixed` (see css/code-block-header.css) because both
 * `.table-block-wrapper` and the chat bubble clip overflow: an absolutely
 * positioned menu was cut off below a 1–2 row table, hiding its third item
 * (Copy TSV) entirely.
 *
 * `placeFixedAt` does the measuring (see utils/fixedPosition.ts for why a fixed
 * element's CSS offsets are NOT viewport offsets here). This function only
 * decides WHERE in viewport space the menu belongs: right-aligned to the
 * trigger, below it, flipped above when it would spill past the bottom edge,
 * and clamped into the viewport.
 */
export function positionTableMenu(trigger: HTMLElement, menu: HTMLElement): void {
    // A landscape overlay rotates its panel, which makes that panel the
    // containing block for this fixed menu — viewport offsets would be read as
    // panel-local ones. See positionTableMenuInRotatedPanel.
    const overlay = menu.closest<HTMLElement>('.table-landscape')
    if (overlay?.classList.contains('is-rotated')) {
        const panel = overlay.firstElementChild as HTMLElement | null
        if (panel) {
            positionTableMenuInRotatedPanel(trigger, menu, panel)
            return
        }
    }

    // Measure while laid out: display is already 'block' (setTableMenuOpen ran).
    const width = menu.offsetWidth
    const height = menu.offsetHeight
    const triggerRect = trigger.getBoundingClientRect()

    const margin = 8
    const gap = 4
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight

    // Right-align to the trigger, then keep it on screen.
    let left = triggerRect.right - width
    left = Math.min(left, viewportWidth - width - margin)
    left = Math.max(margin, left)

    // Prefer below the trigger; flip above when there is no room.
    let top = triggerRect.bottom + gap
    if (top + height > viewportHeight - margin) {
        const above = triggerRect.top - gap - height
        // Only flip when the flipped position actually fits; otherwise keep it
        // below and let the clamp below keep it on screen.
        if (above >= margin) top = above
    }
    top = Math.max(margin, Math.min(top, viewportHeight - height - margin))

    placeFixedAt(menu, left, top)
}

/**
 * Place the copy menu inside a landscape overlay whose panel is rotated 90°.
 *
 * The panel is a transformed ancestor, so it IS the containing block for the
 * menu's `position: fixed`. Offsets computed in viewport space would be read as
 * panel-local ones and drop the menu somewhere unrelated (a 90° turn maps local
 * (x, y) to viewport (right - y, top + x), so the mapping is not a translation).
 *
 * So: map the trigger's viewport rect into panel-local space, run the same
 * below/flip/clamp logic there, and write `left`/`top` directly — no
 * `placeFixedAt`, whose origin/scale probe cannot represent a rotation.
 */
export function positionTableMenuInRotatedPanel(trigger: HTMLElement, menu: HTMLElement, panel: HTMLElement): void {
    const panelRect = panel.getBoundingClientRect()
    const localWidth = panel.offsetWidth
    const localHeight = panel.offsetHeight
    const triggerRect = trigger.getBoundingClientRect()

    // viewport → panel-local: local x = vy - panelTop, local y = panelRight - vx.
    // Only the edges the placement below actually reads are converted.
    const localRight = triggerRect.bottom - panelRect.top
    const localTop = panelRect.right - triggerRect.right
    const localBottom = panelRect.right - triggerRect.left

    const width = menu.offsetWidth
    const height = menu.offsetHeight
    const margin = 8
    const gap = 4

    let left = localRight - width
    left = Math.min(left, localWidth - width - margin)
    left = Math.max(margin, left)

    let top = localBottom + gap
    if (top + height > localHeight - margin) {
        const above = localTop - gap - height
        if (above >= margin) top = above
    }
    top = Math.max(margin, Math.min(top, localHeight - height - margin))

    menu.style.right = ''
    menu.style.bottom = ''
    menu.style.left = `${left}px`
    menu.style.top = `${top}px`
}

// ── Keep an open menu glued to its trigger ──────────────────────────────────
// A fixed menu does not travel with the content it belongs to, so any scroll
// (the chat list, a preview pane) would leave it floating over unrelated
// content. Reposition on scroll/resize; close once the trigger itself has
// scrolled out of view, where "glued to the trigger" stops being meaningful.
// Bound only while a menu is open.

let _menuRepositionBound = false

function onTableMenuViewportChange(): void {
    const menu = document.querySelector<HTMLElement>('.table-block-copy-menu.is-open')
    if (!menu) return
    const trigger = menu.previousElementSibling as HTMLElement | null
    if (!trigger) return
    const rect = trigger.getBoundingClientRect()

    // In a rotated landscape panel the content scrolls ALONG the viewport's
    // horizontal axis, so the vertical test below would never fire and the menu
    // would hang in place after its trigger scrolled away. Compare against the
    // panel's box instead, on both axes.
    const overlay = menu.closest<HTMLElement>('.table-landscape')
    if (overlay?.classList.contains('is-rotated')) {
        const panelRect = overlay.firstElementChild?.getBoundingClientRect()
        if (panelRect && (
            rect.bottom < panelRect.top || rect.top > panelRect.bottom ||
            rect.right < panelRect.left || rect.left > panelRect.right
        )) {
            closeAllTableBlockMenus()
            return
        }
    } else if (rect.bottom < 0 || rect.top > window.innerHeight) {
        closeAllTableBlockMenus()
        return
    }

    positionTableMenu(trigger, menu)
}

function syncTableMenuRepositionListener(): void {
    const anyOpen = !!document.querySelector('.table-block-copy-menu.is-open')
    if (anyOpen && !_menuRepositionBound) {
        window.addEventListener('scroll', onTableMenuViewportChange, true)
        window.addEventListener('resize', onTableMenuViewportChange)
        _menuRepositionBound = true
    } else if (!anyOpen && _menuRepositionBound) {
        window.removeEventListener('scroll', onTableMenuViewportChange, true)
        window.removeEventListener('resize', onTableMenuViewportChange)
        _menuRepositionBound = false
    }
}

export function useCodeBlockHeader() {
    return { annotateCodeBlockHeaders, handleCodeBlockClick, annotateTableBlockHeaders, handleTableBlockClick, closeAllTableBlockMenus }
}
