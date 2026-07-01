/**
 * Export rendered Markdown as a self-contained HTML file.
 *
 * Pipeline:
 * 1. Clone the .markdown-body DOM
 * 2. Inline images via /api/file/batch-base64
 * 3. Inline CSS via stylesheet serialization (preserve var() for theme switching)
 * 4. Handle failed Mermaid diagrams
 * 5. Render Mermaid for both light + dark themes (dual-theme SVGs)
 * 6. Build TOC (floating button + right drawer)
 * 7. Add code block copy/wrap interaction JS
 * 8. Add theme toggle button (light ↔ dark)
 * 9. Assemble complete HTML document
 */

import { mermaid } from './globals.ts'

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
 * Preserves var() references and exports both light + dark theme variable blocks
 * so the exported HTML supports theme toggling via data-theme attribute.
 */
function serializeCss(_markdownBodyEl: HTMLElement): string {
    const rules: string[] = []

    for (const sheet of Array.from(document.styleSheets)) {
        let cssRules: CSSRuleList
        try {
            cssRules = sheet.cssRules
        } catch {
            // Cross-origin stylesheet — skip (would need async fetch)
            continue
        }

        for (const rule of Array.from(cssRules)) {
            if (rule instanceof CSSStyleRule) {
                const sel = rule.selectorText
                // Include rules that target markdown-body or its descendants,
                // :root custom property blocks, or [data-theme="dark"] variable blocks
                if (
                    sel.includes('.markdown-body') ||
                    sel === ':root' ||
                    sel.startsWith('[data-theme') ||
                    sel.includes('.markdown-content') ||
                    sel.includes('.diff-marker') ||
                    sel.includes('.hljs') ||
                    sel.includes('.katex') ||
                    sel.includes('.code-line') ||
                    sel.includes('.line-num') ||
                    sel.includes('.code-text') ||
                    sel.includes('.code-block-pre') ||
                    sel.includes('.code-block-header') ||
                    sel.includes('.code-block-wrapper') ||
                    sel.includes('.code-block-copy-btn') ||
                    sel.includes('.code-block-wrap-btn') ||
                    sel.includes('.code-block-lang') ||
                    sel.includes('.code-block-header-actions') ||
                    sel.includes('.code-block-copied-text') ||
                    sel.includes('.code-file-path') ||
                    sel.includes('.table-block-wrapper') ||
                    sel.includes('.table-block-header') ||
                    sel.includes('.table-block-label') ||
                    sel.includes('.table-block-copy-btn') ||
                    sel.includes('.table-block-wrap-btn') ||
                    sel.includes('.table-block-header-actions') ||
                    sel.includes('.table-block-copied-text') ||
                    sel.includes('.table-wrap') ||
                    sel.includes('.line-flash') ||
                    sel.includes('.copy-flash') ||
                    sel.includes('.char-flash-delete') ||
                    sel.includes('.char-flash-add') ||
                    sel.includes('.chat-file-path') ||
                    sel.includes('.chat-file-open-btn') ||
                    sel.includes('.chat-commit-hash') ||
                    sel.includes('.chat-commit-open-btn') ||
                    sel.includes('.chat-url-open-btn') ||
                    sel.includes('.chat-worktree-btn') ||
                    sel.includes('.mermaid')
                ) {
                    let text = rule.cssText

                    // Rewrite [data-hljs-theme="light"] → [data-theme="light"]
                    // Rewrite [data-hljs-theme="dark"]  → [data-theme="dark"]
                    // This allows hljs overrides to respond to the theme toggle
                    text = text.replace(/\[data-hljs-theme="light"\]/g, '[data-theme="light"]')
                    text = text.replace(/\[data-hljs-theme="dark"\]/g, '[data-theme="dark"]')

                    rules.push(text)
                }
            } else if (rule instanceof CSSKeyframesRule) {
                // Include @keyframes used by animations in exported content
                const name = rule.name
                if (
                    name.includes('line-flash') ||
                    name.includes('copy-flash') ||
                    name.includes('char-flash') ||
                    name.includes('diff-marker') ||
                    name.includes('url-btn-spin')
                ) {
                    rules.push(rule.cssText)
                }
            } else if (rule instanceof CSSMediaRule) {
                // Include media rules that contain markdown-body rules
                const innerRules: string[] = []
                for (const inner of Array.from(rule.cssRules)) {
                    if (inner instanceof CSSStyleRule) {
                        const sel = inner.selectorText
                        if (sel.includes('.markdown-body') || sel.includes('.hljs') || sel.includes('.katex')) {
                            let text = inner.cssText
                            text = text.replace(/\[data-hljs-theme="light"\]/g, '[data-theme="light"]')
                            text = text.replace(/\[data-hljs-theme="dark"\]/g, '[data-theme="dark"]')
                            innerRules.push(text)
                        }
                    } else if (inner instanceof CSSKeyframesRule) {
                        const name = inner.name
                        if (name.includes('line-flash') || name.includes('copy-flash') || name.includes('char-flash') || name.includes('diff-marker') || name.includes('url-btn-spin')) {
                            innerRules.push(inner.cssText)
                        }
                    }
                }
                if (innerRules.length > 0) {
                    rules.push(`@media ${rule.conditionText} { ${innerRules.join(' ')} }`)
                }
            }
        }
    }

    return rules.join('\n')
}

// ─── Mermaid error handling ────────────────────────────────────────────────────

/**
 * Replace unrendered Mermaid blocks (pre.mermaid without SVG child)
 * with error indicators.
 */
function handleFailedMermaid(clone: HTMLElement): void {
    const mermaidBlocks = clone.querySelectorAll('pre.mermaid, div.mermaid, code.mermaid')
    for (const block of Array.from(mermaidBlocks)) {
        // If it contains an SVG, Mermaid rendered successfully
        if (block.querySelector('svg')) continue

        // Mermaid failed — wrap in error div
        const errorDiv = document.createElement('div')
        errorDiv.className = 'mermaid-error'
        const em = document.createElement('em')
        em.textContent = 'Diagram failed to render'
        errorDiv.appendChild(em)
        block.parentNode?.replaceChild(errorDiv, block)
    }
}

// ─── Dual-theme Mermaid rendering ─────────────────────────────────────────────

/**
 * Render Mermaid diagrams for both light and dark themes so the exported HTML
 * can switch themes without needing the Mermaid JS library.
 *
 * For each div.mermaid that has a rendered SVG:
 * 1. Extract the Mermaid source from data-mermaid attribute
 * 2. Render the SVG for the OPPOSITE theme using mermaid.render()
 * 3. Wrap both SVGs in a container with .mermaid-light / .mermaid-dark classes
 *
 * CSS rules then show/hide the correct version based on [data-theme].
 */
async function renderDualThemeMermaid(clone: HTMLElement): Promise<void> {
    const currentTheme = document.documentElement.getAttribute('data-theme') || 'light'
    const oppositeTheme = currentTheme === 'dark' ? 'default' : 'dark'

    const mermaidBlocks = Array.from(clone.querySelectorAll('div.mermaid'))
    if (mermaidBlocks.length === 0) return

    // Re-initialize mermaid with the opposite theme for rendering
    // Keep securityLevel:'loose' so ER diagrams etc. can use <foreignObject>
    // for HTML labels (we sanitize via DOMPurify below to strip scripts/iframes)
    mermaid.initialize({
        startOnLoad: false,
        theme: oppositeTheme,
        securityLevel: 'loose',
        fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    })

    let mermaidCounter = 0

    try {
        for (const block of mermaidBlocks) {
            // Only process blocks that have a rendered SVG
            const existingSvg = block.querySelector('svg')
            if (!existingSvg) continue

            // Get the Mermaid source text
            const source = (block as HTMLElement).dataset.mermaid
            if (!source) continue

            try {
                // Render with the opposite theme (counter-based ID to avoid collisions)
                const id = `mermaid-export-${mermaidCounter++}`
                const result = await mermaid.render(id, source)

                // Create a wrapper div with both theme versions
                const wrapper = clone.ownerDocument.createElement('div')
                wrapper.className = 'mermaid-dual'

                // Current theme SVG — insert directly (content is from trusted
                // Mermaid renderer; we already stripped <script>/<iframe> from clone)
                const currentDiv = clone.ownerDocument.createElement('div')
                currentDiv.className = currentTheme === 'dark' ? 'mermaid-dark' : 'mermaid-light'
                currentDiv.innerHTML = existingSvg.outerHTML

                // Opposite theme SVG — also insert directly, then strip
                // <script> and <iframe> to prevent execution in standalone HTML
                const oppositeDiv = clone.ownerDocument.createElement('div')
                oppositeDiv.className = oppositeTheme === 'dark' ? 'mermaid-dark' : 'mermaid-light'
                oppositeDiv.innerHTML = result.svg
                for (const s of Array.from(oppositeDiv.querySelectorAll('script'))) s.remove()
                for (const f of Array.from(oppositeDiv.querySelectorAll('iframe'))) f.remove()

                wrapper.appendChild(currentDiv)
                wrapper.appendChild(oppositeDiv)

                block.parentNode?.replaceChild(wrapper, block)
            } catch {
                // Failed to render opposite theme — keep only the current theme SVG
            }
        }
    } finally {
        // Always restore mermaid to the current app theme
        mermaid.initialize({
            startOnLoad: false,
            theme: currentTheme === 'dark' ? 'dark' : 'default',
            securityLevel: 'loose',
            fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        })
    }
}

// ─── TOC generation ────────────────────────────────────────────────────────────

/**
 * Build self-contained TOC HTML + JS for the exported document.
 * Uses var() references so colors respond to theme changes.
 */
function buildToc(clone: HTMLElement): { tocButtonHtml: string; tocDrawerHtml: string; tocCss: string; tocJs: string } {
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

    // Build TOC list HTML
    const tocItemsHtml = entries.map(e => {
        const indent = (e.level - 1) * 16
        return `<a class="toc-item" data-level="${e.level}" href="#${escapeHtml(e.id)}" style="padding-left: ${8 + indent}px">${escapeHtml(e.text)}</a>`
    }).join('\n')

    // Floating button (inline SVG list icon) — uses CSS class for theme-aware colors
    const tocButtonHtml = `<button id="toc-toggle" class="fab-btn" title="Table of Contents"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg></button>`

    // TOC drawer
    const tocDrawerHtml = `<div id="toc-drawer"><div class="toc-drawer-title">Table of Contents</div>${tocItemsHtml}</div>`

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

    // TOC + floating button CSS — all via var() for theme support
    const tocCss = `
.fab-btn { position: fixed; width: 40px; height: 40px; border-radius: 50%; border: 1px solid var(--border-color); background: var(--bg-primary); color: var(--text-secondary); cursor: pointer; box-shadow: 0 2px 8px rgba(0,0,0,0.12); display: flex; align-items: center; justify-content: center; padding: 0; }
.fab-btn:hover { color: var(--accent-color); }
#theme-toggle { bottom: 68px; right: 20px; z-index: 1000; }
#toc-toggle { bottom: 20px; right: 20px; z-index: 1000; }
#toc-drawer { position: fixed; right: 0; top: 0; height: 100%; width: 280px; background: var(--bg-primary); border-left: 1px solid var(--border-color); box-shadow: -2px 0 12px rgba(0,0,0,0.08); transform: translateX(100%); transition: transform 0.3s ease; z-index: 999; overflow-y: auto; padding: 16px 8px; box-sizing: border-box; }
.toc-drawer-title { font-size: 14px; font-weight: 600; margin-bottom: 12px; padding: 0 8px; color: var(--text-primary); }
.toc-item { display: block; padding: 6px 8px; border-radius: 4px; cursor: pointer; font-size: 13px; color: var(--text-secondary); text-decoration: none; transition: background 0.15s, color 0.15s; border-left: 2px solid transparent; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.toc-item:hover { background: var(--bg-tertiary); color: var(--accent-color); }`

    return { tocButtonHtml, tocDrawerHtml, tocCss, tocJs }
}

// ─── Code block + Table block interaction JS ────────────────────────────────────

/**
 * Generate JS for code block and table block copy/wrap toggle buttons.
 * Code blocks: .code-block-wrapper with .code-block-copy-btn/.code-block-wrap-btn
 * Table blocks: .table-block-wrapper with .table-block-copy-btn/.table-block-wrap-btn
 * Both use data-action="copy"/"wrap" pattern from useCodeBlockHeader.ts.
 */
function buildCodeBlockJs(): string {
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
        btn.innerHTML = '<span class="copied-feedback">Copied!</span>';
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

    // 1. Clone DOM
    const clone = markdownBodyEl.cloneNode(true) as HTMLElement

    // 1b. Remove <script> tags from clone (Mermaid injects scripts into SVGs;
    //     these cause SyntaxError when opened as standalone HTML and are unnecessary
    //     since the SVGs are already rendered)
    for (const script of Array.from(clone.querySelectorAll('script'))) {
        script.remove()
    }

    // 1c. Remove <iframe> elements from clone (some Mermaid rendering modes or
    //     browser MathML handling can inject iframes; these cause "Unsafe attempt
    //     to load URL" cross-origin errors when opened as file:// URLs)
    for (const iframe of Array.from(clone.querySelectorAll('iframe'))) {
        iframe.remove()
    }

    // 1d. Remove KaTeX MathML annotations (screen-reader-only <span class="katex-mathml">
    //     containing <math> tags). Chrome tries to process MathML in a separate
    //     security origin, triggering cross-origin errors on file:// URLs.
    //     The visual rendering is handled by <span class="katex-html"> which remains.
    for (const mathml of Array.from(clone.querySelectorAll('.katex-mathml'))) {
        mathml.remove()
    }

    // 1e. Note: <foreignObject> elements in Mermaid SVGs are kept as-is.
    //     Chrome may log "Unsafe attempt to load URL" warnings on file:// URLs,
    //     but this does NOT affect rendering — the content displays correctly.
    //     Converting foreignObject HTML to SVG <text> breaks text layout,
    //     so we leave them untouched.

    // 2. Inline images
    const { skipped: skippedImages, external: externalImages } = await inlineImages(clone)

    // 3. Handle failed Mermaid diagrams
    handleFailedMermaid(clone)

    // 3b. Render Mermaid for both light + dark themes
    await renderDualThemeMermaid(clone)

    // 4. Serialize CSS from stylesheets (preserves var() for theme switching)
    const css = serializeCss(markdownBodyEl)

    // 5. Build TOC
    const { tocButtonHtml, tocDrawerHtml, tocCss, tocJs } = buildToc(clone)

    // 6. Build code block interaction JS
    const codeBlockJs = buildCodeBlockJs()

    // 7. Assemble HTML
    const title = escapeHtml(fileName.replace(/\.md$/i, ''))
    const bodyContent = clone.outerHTML

    // Use current app theme as default (avoids flash of wrong theme)
    const currentAppTheme = document.documentElement.getAttribute('data-theme') || 'light'

    // Theme toggle button (sun/moon icon)
    const themeToggleHtml = `<button id="theme-toggle" class="fab-btn" title="Toggle theme"><svg id="theme-icon-moon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg><svg id="theme-icon-sun" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="display:none"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg></button>`

    // Theme toggle JS (localStorage restore is in <head> script to avoid FOWT)
    const themeToggleJs = `
(function() {
    var btn = document.getElementById('theme-toggle');
    var moonIcon = document.getElementById('theme-icon-moon');
    var sunIcon = document.getElementById('theme-icon-sun');
    updateIcon();
    btn.addEventListener('click', function(e) {
        e.stopPropagation();
        var current = document.documentElement.getAttribute('data-theme') || 'light';
        var next = current === 'dark' ? 'light' : 'dark';
        document.documentElement.setAttribute('data-theme', next);
        localStorage.setItem('exported-html-theme', next);
        updateIcon();
    });
    function updateIcon() {
        var isDark = (document.documentElement.getAttribute('data-theme') || 'light') === 'dark';
        moonIcon.style.display = isDark ? 'none' : 'block';
        sunIcon.style.display = isDark ? 'block' : 'none';
    }
})();`

    const html = `<!DOCTYPE html>
<html lang="en" data-theme="${currentAppTheme}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<script>/* Restore saved theme before CSS renders to prevent flash */(function(){var t=localStorage.getItem('exported-html-theme');if(t)document.documentElement.setAttribute('data-theme',t)})()</script>
<style>
/* ─── Universal box-sizing reset (matches app base.css) ─── */
*, *::before, *::after { box-sizing: border-box; }

/* ─── Theme variables + markdown styles (preserves var() for theme switching) ─── */
${css}

/* ─── Base reset with theme colors ─── */
body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif; background: var(--bg-primary); color: var(--text-primary); }

/* ─── Scrollbar styling (matches app base.css) ─── */
::-webkit-scrollbar { width: 4px; height: 4px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 4px; }
::-webkit-scrollbar-thumb:hover { background: var(--text-muted); }
::-webkit-scrollbar-button { display: none; }
::-webkit-scrollbar-corner { background: transparent; }
* { scrollbar-color: var(--scrollbar-thumb) transparent; }

/* ─── Mermaid error ─── */
.mermaid-error { border: 1px dashed var(--border-color); padding: 12px; margin: 8px 0; border-radius: 6px; color: var(--text-muted); font-size: 13px; }

/* ─── Dual-theme Mermaid: show/hide based on data-theme ─── */
.mermaid-dual { background: var(--bg-secondary); padding: 20px; border-radius: var(--radius-md); margin: 1em 0; overflow-x: auto; }
.mermaid-dual svg { max-width: 100%; height: auto; }
.mermaid-dual .mermaid-light { display: block; }
.mermaid-dual .mermaid-dark { display: none; }
[data-theme="dark"] .mermaid-dual .mermaid-light { display: none; }
[data-theme="dark"] .mermaid-dual .mermaid-dark { display: block; }

/* ─── Copied feedback text ─── */
.copied-feedback { font-size: 11px; color: var(--accent-color); }

/* ─── TOC + FAB buttons ─── */
${tocCss}
</style>
</head>
<body>
${themeToggleHtml}
${tocButtonHtml}
${tocDrawerHtml}
${bodyContent}
<script>
${themeToggleJs}
${tocJs}
${codeBlockJs}
</script>
</body>
</html>`

    return { html, skippedImages, externalImages }
}
