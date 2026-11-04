// Shared "does this file support a TOC" decision, used by both the file-header
// TOC button (FileHeader.vue) and the App-level TOC dock/drawer gating.
//
// Two separate consumers must agree on which file types/views expose a TOC:
//   - button visibility (FileHeader `hasToc`)
//   - whether an already-open TOC panel may keep showing (App)
// Keeping this in one function avoids drift between them.

import { getFileType } from '@/utils/fileType'

export interface TocSupportFile {
    name?: string
    content?: string | null
    subtype?: string
    isPdf?: boolean
    isOffice?: boolean
    isImage?: boolean
    isAudio?: boolean
    isVideo?: boolean
    isExcalidraw?: boolean
    isBinary?: boolean
    tooLarge?: boolean
}

/**
 * Whether `file` (in `viewMode`: 'rendered' | 'raw') supports a TOC panel.
 *
 * Supported:
 *  - PDF — outline driven by the PDF engine (content not required).
 *  - Markdown — headings, in rendered and raw/editing views alike.
 *  - Code/text files — code symbols via tree-sitter / regex.
 *  - HTML / OpenAPI **raw/source** view — the CodeMirror source is navigable.
 *
 * Not supported:
 *  - Media/binary/Excalidraw/Office — no text outline (or content is null).
 *  - HTML rendered preview — sandboxed iframe has no heading DOM to jump to.
 *  - OpenAPI rendered preview — ReDoc ships its own sidebar.
 */
export function fileSupportsToc(file: TocSupportFile | null | undefined, viewMode = 'rendered'): boolean {
    if (!file) return false
    const ft = getFileType(file.name || '')
    // Media / Excalidraw never have a document outline.
    if (file.isImage || file.isAudio || file.isVideo || file.isExcalidraw) return false
    if (ft.isImage || ft.isAudio || ft.isVideo || ft.isExcalidraw) return false
    // PDF outline comes from the PDF engine, not from content text.
    if (file.isPdf || ft.isPdf) return true
    // Office binaries (xlsx/docx/pptx/xls) render through OfficePreview —
    // content is null and there is nothing to outline.
    if (file.isOffice || ft.isOffice) return false
    if (typeof file.content !== 'string' || file.content === '') return false
    if (file.isBinary || file.tooLarge) return false

    if (ft.isMarkdown) return true
    // Rendered previews with their own navigation / no heading DOM.
    if (viewMode === 'rendered') {
        if (ft.isHtml) return false
        if (file.subtype === 'openapi') return false
        // The CSV/TSV table has no headings to outline; the source view still
        // gets the code-symbol TOC.
        if (ft.isTable) return false
    }
    return true
}
