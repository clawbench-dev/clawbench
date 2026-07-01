/**
 * Export rendered Markdown as a self-contained HTML file.
 *
 * Pipeline:
 * 1. Clone the .markdown-body DOM
 * 2. Inline images via /api/file/batch-base64
 * 3. Inline CSS via stylesheet serialization (freeze current theme)
 * 4. Handle failed Mermaid diagrams
 * 5. Build TOC (floating button + right drawer)
 * 6. Add code block copy/wrap interaction JS
 * 7. Assemble complete HTML document
 */

// ─── Types ──────────────────────────────────────────────────────────────────────

export interface ExportOptions {
    markdownBodyEl: HTMLElement
    filePath: string
    fileName: string
}

export interface ExportResult {
    html: string
    skippedImages: number
    externalImages: number
}

// ─── Image inlining ────────────────────────────────────────────────────────────

interface BatchBase64Result {
    mime: string
    data: string
}

interface BatchBase64Skipped {
    path: string
    reason: string
}

interface BatchBase64Response {
    results: Record<string, BatchBase64Result>
    skipped?: BatchBase64Skipped[]
}

/**
 * Extract image paths from /api/local-file/ URLs in the cloned DOM,
 * call batch-base64 API, and replace src with data URIs.
 */
async function inlineImages(clone: HTMLElement): Promise<{ skipped: number; external: number }> {
    const imgs = Array.from(clone.querySelectorAll('img')) as HTMLImageElement[]
    if (imgs.length === 0) return { skipped: 0, external: 0 }

    let external = 0
    const pathToImg: Map<string, HTMLImageElement[]> = new Map()

    for (const img of imgs) {
        const src = img.getAttribute('src') || ''

        // Skip data URIs (already self-contained)
        if (src.startsWith('data:')) continue

        // Skip external URLs (will need internet)
        if (/^(https?:|\/\/)/i.test(src)) {
            external++
            continue
        }

        // Extract path from /api/local-file/...?t=...
        const match = src.match(/^\/api\/local-file\/(.+?)(?:\?.*)?$/)
        if (!match) continue

        let imgPath: string
        try {
            imgPath = decodeURIComponent(match[1])
        } catch {
            imgPath = match[1]
        }

        const list = pathToImg.get(imgPath)
        if (list) list.push(img)
        else pathToImg.set(imgPath, [img])
    }

    if (pathToImg.size === 0) return { skipped: 0, external }

    // Batch fetch base64
    const paths = Array.from(pathToImg.keys())
    let skipped: number

    try {
        const resp = await fetch('/api/file/batch-base64', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths }),
        })

        if (!resp.ok) {
            // API failed — all local images keep original src
            return { skipped: paths.length, external }
        }

        const data: BatchBase64Response = await resp.json()

        // Apply results
        for (const [imgPath, result] of Object.entries(data.results || {})) {
            const imgsForPath = pathToImg.get(imgPath)
            if (!imgsForPath) continue
            for (const img of imgsForPath) {
                img.setAttribute('src', `data:${result.mime};base64,${result.data}`)
            }
            pathToImg.delete(imgPath)
        }

        // Remaining in pathToImg are paths that weren't in results (server skipped or failed)
        skipped = pathToImg.size
    } catch {
        // Network error — images keep original src
        skipped = paths.length
    }

    return { skipped, external }
}

// ─── CSS inlining (stylesheet serialization) ───────────────────────────────────

/**
 * Collect and serialize CSS rules that apply to .markdown-body and its descendants.
 * Freeze the current theme by resolving CSS custom properties to computed values.
 */
function serializeCss(_markdownBodyEl: HTMLElement): string {
    const rules: string[] = []
    const customProps: Map<string, string> = new Map()

    // 1. Resolve all custom properties for the current theme from :root
    const rootStyles = getComputedStyle(document.documentElement)
    // Collect all custom properties used in the markdown body
    for (const sheet of Array.from(document.styleSheets)) {
        let cssRules: CSSRuleList
        try {
            cssRules = sheet.cssRules
        } catch {
            // Cross-origin stylesheet — skip (would need async fetch)
            continue
        }

        for (const rule of Array.from(cssRules)) {
            const text = rule.cssText

            // Collect :root custom property definitions
            if (rule instanceof CSSStyleRule) {
                const sel = rule.selectorText
                if (sel === ':root' || sel.startsWith(':root ')) {
                    // Extract custom property names used in var() references
                    const varRefs = text.match(/var\(\s*(--[\w-]+)/g)
                    if (varRefs) {
                        for (const ref of varRefs) {
                            const name = ref.replace(/var\(\s*/, '')
                            const val = rootStyles.getPropertyValue(name).trim()
                            if (val) customProps.set(name, val)
                        }
                    }
                    // Also collect custom properties defined directly in :root
                    const propDefs = text.match(/(--[\w-]+)\s*:/g)
                    if (propDefs) {
                        for (const def of propDefs) {
                            const name = def.replace(/\s*:/, '')
                            const val = rootStyles.getPropertyValue(name).trim()
                            if (val) customProps.set(name, val)
                        }
                    }
                }
            }
        }
    }

    // 2. Serialize all rules relevant to .markdown-body
    for (const sheet of Array.from(document.styleSheets)) {
        let cssRules: CSSRuleList
        try {
            cssRules = sheet.cssRules
        } catch {
            continue
        }

        for (const rule of Array.from(cssRules)) {
            if (rule instanceof CSSStyleRule) {
                const sel = rule.selectorText
                // Include rules that target markdown-body or its descendants,
                // or :root / [data-theme] custom property blocks
                if (
                    sel.includes('.markdown-body') ||
                    sel === ':root' ||
                    sel.includes('.markdown-content') ||
                    sel.includes('.diff-marker') ||
                    sel.includes('.code-block-header') ||
                    sel.includes('.code-block-wrapper') ||
                    sel.includes('.code-block-copy-btn') ||
                    sel.includes('.code-block-wrap-btn') ||
                    sel.includes('.code-block-lang') ||
                    sel.includes('.code-block-header-actions') ||
                    sel.includes('.code-block-copied-text') ||
                    sel.includes('.table-block-wrapper') ||
                    sel.includes('.table-block-header') ||
                    sel.includes('.table-block-label') ||
                    sel.includes('.table-block-copy-btn') ||
                    sel.includes('.table-block-wrap-btn') ||
                    sel.includes('.table-block-header-actions') ||
                    sel.includes('.table-block-copied-text') ||
                    sel.includes('.table-wrap') ||
                    sel.includes('.line-flash')
                ) {
                    // Skip [data-theme] rules — they won't match in exported HTML
                    // (theme is frozen via resolved var() values instead)
                    // Replace var() references with resolved values
                    let resolvedText = rule.cssText
                    if (customProps.size > 0) {
                        resolvedText = resolveVarRefs(resolvedText, customProps)
                    }
                    rules.push(resolvedText)
                }
            } else if (rule instanceof CSSMediaRule) {
                // Include media rules that contain markdown-body rules
                const innerRules: string[] = []
                for (const inner of Array.from(rule.cssRules)) {
                    if (inner instanceof CSSStyleRule && inner.selectorText.includes('.markdown-body')) {
                        let resolvedText = inner.cssText
                        if (customProps.size > 0) {
                            resolvedText = resolveVarRefs(resolvedText, customProps)
                        }
                        innerRules.push(resolvedText)
                    }
                }
                if (innerRules.length > 0) {
                    rules.push(`@media ${rule.conditionText} { ${innerRules.join(' ')} }`)
                }
            }
        }
    }

    // 3. Add resolved :root custom properties as a :root block
    if (customProps.size > 0) {
        const propLines: string[] = []
        for (const [name, val] of customProps) {
            propLines.push(`  ${name}: ${val};`)
        }
        rules.unshift(`:root {\n${propLines.join('\n')}\n}`)
    }

    return rules.join('\n')
}

/**
 * Replace var(--xxx) references in CSS text with their resolved values.
 * Handles var(--xxx) and var(--xxx, fallback).
 */
function resolveVarRefs(cssText: string, props: Map<string, string>): string {
    // Replace var(--xxx) and var(--xxx, fallback) — iterative to handle nested var()
    let result = cssText
    for (let i = 0; i < 3; i++) { // max 3 passes for nested var()
        const prev = result
        result = result.replace(/var\(\s*(--[\w-]+)\s*(?:,\s*([^)]*))?\)/g, (match, name: string, fallback?: string) => {
            const resolved = props.get(name)
            if (resolved) return resolved
            if (fallback) return fallback.trim()
            return match
        })
        if (result === prev) break // no more changes
    }
    return result
}

// ─── Mermaid error handling ────────────────────────────────────────────────────

/**
 * Replace unrendered Mermaid blocks (pre.mermaid without SVG child)
 * with error indicators.
 */
function handleFailedMermaid(clone: HTMLElement, customProps: Map<string, string>): void {
    const mermaidBlocks = clone.querySelectorAll('pre.mermaid, div.mermaid, code.mermaid')
    const borderColor = customProps.get('--border-color') || '#d0d7de'
    const textColor = customProps.get('--text-muted') || '#6c757d'
    for (const block of Array.from(mermaidBlocks)) {
        // If it contains an SVG, Mermaid rendered successfully
        if (block.querySelector('svg')) continue

        // Mermaid failed — wrap in error div
        const errorDiv = document.createElement('div')
        errorDiv.className = 'mermaid-error'
        errorDiv.style.border = `1px dashed ${borderColor}`
        errorDiv.style.padding = '12px'
        errorDiv.style.margin = '8px 0'
        errorDiv.style.borderRadius = '6px'
        errorDiv.style.color = textColor
        errorDiv.style.fontSize = '13px'
        const em = document.createElement('em')
        em.textContent = 'Diagram failed to render'
        errorDiv.appendChild(em)
        block.parentNode?.replaceChild(errorDiv, block)
    }
}

// ─── Theme color helpers ───────────────────────────────────────────────────────

/**
 * Get current theme colors from computed styles for use in exported HTML.
 */
function getThemeColors(): Map<string, string> {
    const rootStyles = getComputedStyle(document.documentElement)
    const colors = new Map<string, string>()
    const keys = [
        '--bg-primary', '--bg-secondary', '--bg-tertiary',
        '--text-primary', '--text-secondary', '--text-muted', '--text-bold',
        '--border-color', '--accent-color', '--accent-hover',
        '--code-bg', '--blockquote-border', '--table-border',
        '--scrollbar-thumb', '--scrollbar-track',
    ]
    for (const key of keys) {
        const val = rootStyles.getPropertyValue(key).trim()
        if (val) colors.set(key, val)
    }
    return colors
}

// ─── TOC generation ────────────────────────────────────────────────────────────

/**
 * Build self-contained TOC HTML + JS for the exported document.
 * Uses theme colors for consistent appearance.
 */
function buildToc(clone: HTMLElement, theme: Map<string, string>): { tocButtonHtml: string; tocDrawerHtml: string; tocCss: string; tocJs: string } {
    // Extract headings from the cloned DOM
    const headings = Array.from(clone.querySelectorAll('h1, h2, h3, h4, h5, h6')) as HTMLHeadingElement[]
    if (headings.length === 0) return { tocButtonHtml: '', tocDrawerHtml: '', tocCss: '', tocJs: '' }

    interface TocEntry {
        level: number
        text: string
        id: string
    }

    const entries: TocEntry[] = []
    for (const h of headings) {
        const id = h.getAttribute('id')
        if (!id) continue
        entries.push({
            level: parseInt(h.tagName[1], 10),
            text: h.textContent || '',
            id,
        })
    }

    if (entries.length === 0) return { tocButtonHtml: '', tocDrawerHtml: '', tocCss: '', tocJs: '' }

    // Resolve theme colors for TOC
    const bgPrimary = theme.get('--bg-primary') || '#ffffff'
    const bgTertiary = theme.get('--bg-tertiary') || '#e9ecef'
    const textPrimary = theme.get('--text-primary') || '#212529'
    const textSecondary = theme.get('--text-secondary') || '#495057'
    const borderColor = theme.get('--border-color') || '#dee2e6'
    const accentColor = theme.get('--accent-color') || '#4a90d9'

    // Build TOC list HTML
    const tocItemsHtml = entries.map(e => {
        const indent = (e.level - 1) * 16
        return `<a class="toc-item" data-level="${e.level}" href="#${e.id}" style="padding-left: ${8 + indent}px">${escapeHtml(e.text)}</a>`
    }).join('\n')

    // Floating button (inline SVG list icon)
    const tocButtonHtml = `<button id="toc-toggle" style="position:fixed;bottom:20px;right:20px;z-index:1000;width:40px;height:40px;border-radius:50%;border:1px solid ${borderColor};background:${bgPrimary};color:${textSecondary};cursor:pointer;box-shadow:0 2px 8px rgba(0,0,0,0.12);display:flex;align-items:center;justify-content:center" title="Table of Contents"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg></button>`

    // TOC drawer
    const tocDrawerHtml = `<div id="toc-drawer" style="position:fixed;right:0;top:0;height:100%;width:280px;background:${bgPrimary};border-left:1px solid ${borderColor};box-shadow:-2px 0 12px rgba(0,0,0,0.08);transform:translateX(100%);transition:transform 0.3s ease;z-index:999;overflow-y:auto;padding:16px 8px;box-sizing:border-box"><div style="font-size:14px;font-weight:600;margin-bottom:12px;padding:0 8px;color:${textPrimary}">Table of Contents</div>${tocItemsHtml}</div>`

    // TOC JS — fix: use contains() check on the button element (not === target)
    const tocJs = `
(function() {
    var btn = document.getElementById('toc-toggle');
    var drawer = document.getElementById('toc-drawer');
    var open = false;
    btn.addEventListener('click', function(e) {
        e.stopPropagation();
        open = !open;
        drawer.style.transform = open ? 'translateX(0)' : 'translateX(100%)';
    });
    drawer.addEventListener('click', function(e) {
        var a = e.target.closest('a.toc-item');
        if (!a) return;
        e.preventDefault();
        var id = a.getAttribute('href').slice(1);
        var el = document.getElementById(id);
        if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
        open = false;
        drawer.style.transform = 'translateX(100%)';
    });
    document.addEventListener('click', function(e) {
        if (open && !drawer.contains(e.target) && !btn.contains(e.target)) {
            open = false;
            drawer.style.transform = 'translateX(100%)';
        }
    });
})();`

    // TOC item CSS — use theme colors
    const tocCss = `
.toc-item { display: block; padding: 6px 8px; border-radius: 4px; cursor: pointer; font-size: 13px; color: ${textSecondary}; text-decoration: none; transition: background 0.15s, color 0.15s; border-left: 2px solid transparent; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.toc-item:hover { background: ${bgTertiary}; color: ${accentColor}; }`

    return { tocButtonHtml, tocDrawerHtml, tocCss, tocJs }
}

// ─── Code block + Table block interaction JS ────────────────────────────────────

/**
 * Generate JS for code block and table block copy/wrap toggle buttons.
 * Code blocks: .code-block-wrapper with .code-block-copy-btn/.code-block-wrap-btn
 * Table blocks: .table-block-wrapper with .table-block-copy-btn/.table-block-wrap-btn
 * Both use data-action="copy"/"wrap" pattern from useCodeBlockHeader.ts.
 */
function buildCodeBlockJs(theme: Map<string, string>): string {
    const accentColor = theme.get('--accent-color') || '#4a90d9'

    return `
(function() {
    document.addEventListener('click', function(e) {
        // ─── Code block buttons ───
        var codeBtn = e.target.closest('.code-block-copy-btn, .code-block-wrap-btn');
        if (codeBtn) {
            e.preventDefault();
            e.stopPropagation();
            var wrapper = codeBtn.closest('.code-block-wrapper');
            if (!wrapper) return;
            var pre = wrapper.querySelector('pre');
            if (!pre) return;
            var action = codeBtn.getAttribute('data-action');
            if (action === 'copy') {
                if (codeBtn.classList.contains('is-copied')) return;
                var code = pre.querySelector('code');
                var text = (code || pre).textContent || '';
                copyText(text, codeBtn);
            } else if (action === 'wrap') {
                wrapper.classList.toggle('word-wrap');
                codeBtn.classList.toggle('is-wrapped');
                var isWrapped = wrapper.classList.contains('word-wrap');
                codeBtn.setAttribute('title', isWrapped ? 'Word wrap on' : 'Word wrap off');
            }
            return;
        }

        // ─── Table block buttons ───
        var tableBtn = e.target.closest('.table-block-copy-btn, .table-block-wrap-btn');
        if (tableBtn) {
            e.preventDefault();
            e.stopPropagation();
            var wrapper = tableBtn.closest('.table-block-wrapper');
            if (!wrapper) return;
            var action = tableBtn.getAttribute('data-action');
            if (action === 'copy') {
                if (tableBtn.classList.contains('is-copied')) return;
                var table = wrapper.querySelector('table');
                if (!table) return;
                var text = tableToText(table);
                copyText(text, tableBtn);
            } else if (action === 'wrap') {
                wrapper.classList.toggle('word-wrap');
                tableBtn.classList.toggle('is-wrapped');
                var isWrapped = wrapper.classList.contains('word-wrap');
                tableBtn.setAttribute('title', isWrapped ? 'Word wrap on' : 'Word wrap off');
            }
            return;
        }
    });

    function copyText(text, btn) {
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text);
        } else {
            var ta = document.createElement('textarea');
            ta.value = text;
            ta.style.cssText = 'position:fixed;left:-9999px';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
        }
        var orig = btn.innerHTML;
        var origTitle = btn.getAttribute('title') || '';
        btn.innerHTML = '<span style="font-size:11px;color:${accentColor}">Copied!</span>';
        btn.classList.add('is-copied');
        btn.setAttribute('title', 'Copied');
        setTimeout(function() {
            btn.innerHTML = orig;
            btn.classList.remove('is-copied');
            btn.setAttribute('title', origTitle);
        }, 1500);
    }

    function tableToText(table) {
        var rows = table.querySelectorAll('tr');
        var lines = [];
        for (var i = 0; i < rows.length; i++) {
            var cells = rows[i].querySelectorAll('th, td');
            var vals = [];
            for (var j = 0; j < cells.length; j++) {
                vals.push(cells[j].textContent.trim());
            }
            lines.push(vals.join('\\t'));
        }
        return lines.join('\\n');
    }
})();`
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

function escapeHtml(text: string): string {
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
}

// ─── Main export function ─────────────────────────────────────────────────────

export async function exportRenderedHtml(options: ExportOptions): Promise<ExportResult> {
    const { markdownBodyEl, fileName } = options
    // filePath is available for future use (resolving relative image paths)

    // Get current theme colors for consistent export
    const theme = getThemeColors()

    // 1. Clone DOM
    const clone = markdownBodyEl.cloneNode(true) as HTMLElement

    // 1b. Remove <script> tags from clone (Mermaid injects scripts into SVGs;
    //     these cause SyntaxError when opened as standalone HTML and are unnecessary
    //     since the SVGs are already rendered)
    for (const script of Array.from(clone.querySelectorAll('script'))) {
        script.remove()
    }

    // 2. Inline images
    const { skipped: skippedImages, external: externalImages } = await inlineImages(clone)

    // 3. Handle failed Mermaid diagrams
    handleFailedMermaid(clone, theme)

    // 4. Serialize CSS from stylesheets (freeze current theme)
    const css = serializeCss(markdownBodyEl)

    // 5. Build TOC
    const { tocButtonHtml, tocDrawerHtml, tocCss, tocJs } = buildToc(clone, theme)

    // 6. Build code block interaction JS
    const codeBlockJs = buildCodeBlockJs(theme)

    // 7. Assemble HTML
    const title = escapeHtml(fileName.replace(/\.md$/i, ''))
    const bodyContent = clone.outerHTML

    // Resolve theme-dependent values for the base styles
    const bgPrimary = theme.get('--bg-primary') || '#ffffff'
    const codeBg = theme.get('--code-bg') || '#f6f8fa'
    const borderColor = theme.get('--border-color') || '#dee2e6'
    const textMuted = theme.get('--text-muted') || '#6c757d'
    const bgTertiary = theme.get('--bg-tertiary') || '#e9ecef'
    const accentColor = theme.get('--accent-color') || '#4a90d9'
    const textPrimary = theme.get('--text-primary') || '#212529'

    const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>
/* ─── Resolved theme + markdown styles ─── */
${css}

/* ─── Base reset with theme colors ─── */
body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif; background: ${bgPrimary}; color: ${textPrimary}; }

/* ─── Mermaid error ─── */
.mermaid-error { border: 1px dashed ${borderColor}; padding: 12px; margin: 8px 0; border-radius: 6px; color: ${textMuted}; font-size: 13px; }

/* ─── Line flash animation ─── */
@keyframes line-flash-anim { 0% { background-color: rgba(74, 144, 217, 0.3); } 100% { background-color: transparent; } }
.line-flash { animation: line-flash-anim 0.8s ease-out; }

/* ─── Code block header ─── */
.code-block-header { display: flex; align-items: center; justify-content: space-between; padding: 4px 12px; background: ${codeBg}; border-bottom: 1px solid ${borderColor}; font-size: 12px; color: ${textMuted}; }
.code-block-header button { background: none; border: none; cursor: pointer; color: ${textMuted}; font-size: 12px; padding: 2px 8px; border-radius: 4px; }
.code-block-header button:hover { background: ${bgTertiary}; color: ${accentColor}; }

/* ─── Code block word wrap ─── */
.code-block-wrapper.word-wrap pre { white-space: pre-wrap; word-break: break-word; }
.code-block-wrapper.word-wrap .code-block-wrap-btn { color: ${accentColor}; }

/* ─── Code block copied text ─── */
.code-block-copied-text { font-size: 12px; }

/* ─── Table block word wrap ─── */
.table-block-wrapper.word-wrap .table-wrap { overflow-x: hidden !important; }
.table-block-wrapper.word-wrap table { display: table !important; table-layout: fixed !important; width: 100% !important; margin: 0 !important; }
.table-block-wrapper.word-wrap th, .table-block-wrapper.word-wrap td { white-space: normal; word-break: break-word; overflow-wrap: break-word; }
.table-block-wrapper.word-wrap .table-block-wrap-btn { color: ${accentColor}; }

/* ─── TOC items ─── */
${tocCss}
</style>
</head>
<body>
${tocButtonHtml}
${tocDrawerHtml}
${bodyContent}
<script>
${tocJs}
${codeBlockJs}
</script>
</body>
</html>`

    return { html, skippedImages, externalImages }
}
