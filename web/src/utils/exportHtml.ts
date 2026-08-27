/**
 * Export rendered Markdown as a self-contained HTML file.
 *
 * Pipeline:
 * 1. Clone the .markdown-body DOM
 * 2. Inline images via /api/file/batch-base64
 * 3. Handle failed Mermaid diagrams (keeps the already-rendered theme SVG)
 * 4. Inline CSS via stylesheet serialization (current theme variables only)
 * 5. Build TOC (floating button + right drawer)
 * 6. Add code block copy/wrap interaction JS
 * 7. Assemble complete HTML document using the current app theme
 */

import { isDarkTheme } from '@/utils/themeMeta'

// ─── Types ──────────────────────────────────────────────────────────────────────

export interface ExportOptions {
    markdownBodyEl: HTMLElement
    filePath: string
    fileName: string
    /**
     * Current UI locale ('zh' | 'en' | ...). Used to localize the interactive
     * labels embedded in the exported standalone HTML (copy feedback, TOC
     * title, word-wrap tooltips). Falls back to 'en' when omitted.
     */
    locale?: string
}

/** One image that could not be embedded into the exported HTML. */
export interface ImageIssue {
    /** Image path or URL as it appears in the markdown. */
    path: string
    /** Machine-readable reason code (see reasonText for display). */
    reason: string
    /** Whether it was a skipped local file or an external URL. */
    kind: 'skipped' | 'external'
}

export interface ExportResult {
    html: string
    skippedImages: number
    externalImages: number
    /** Per-image embed failure details. */
    issues: ImageIssue[]
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
async function inlineImages(clone: HTMLElement): Promise<{ skipped: number; external: number; issues: ImageIssue[] }> {
    const imgs = Array.from(clone.querySelectorAll('img')) as HTMLImageElement[]
    if (imgs.length === 0) return { skipped: 0, external: 0, issues: [] }

    const issues: ImageIssue[] = []
    let external = 0
    const pathToImg: Map<string, HTMLImageElement[]> = new Map()

    for (const img of imgs) {
        // Prefer the original full-size URL when present (inline src may be a
        // low-res /api/file/thumb thumbnail); fall back to the visible src.
        const src = img.getAttribute('data-full-src') || img.getAttribute('src') || ''

        // Skip data URIs (already self-contained)
        if (src.startsWith('data:')) continue

        // External URLs (will need internet)
        if (/^(https?:|\/\/)/i.test(src)) {
            external++
            issues.push({ path: src, reason: 'external', kind: 'external' })
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

    if (pathToImg.size === 0) return { skipped: 0, external, issues }

    // Batch fetch base64
    const paths = Array.from(pathToImg.keys())
    let skipped = 0

    const addSkipped = (path: string, reason: string) => {
        skipped++
        issues.push({ path, reason, kind: 'skipped' })
    }

    try {
        const resp = await fetch('/api/file/batch-base64', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths }),
        })

        if (!resp.ok) {
            // API failed — all local images keep original src
            for (const p of paths) addSkipped(p, 'api_error')
            return { skipped, external, issues }
        }

        const data: BatchBase64Response = await resp.json()

        // Apply results
        for (const [imgPath, result] of Object.entries(data.results || {})) {
            const imgsForPath = pathToImg.get(imgPath)
            if (!imgsForPath) continue
            for (const img of imgsForPath) {
                img.setAttribute('src', `data:${result.mime};base64,${result.data}`)
                img.removeAttribute('data-full-src')
            }
            pathToImg.delete(imgPath)
        }

        // Remaining in pathToImg are paths that weren't in results (server skipped).
        // Use the server-reported reason when available.
        const skippedByPath = new Map((data.skipped || []).map(s => [s.path, s.reason]))
        for (const p of pathToImg.keys()) {
            addSkipped(p, skippedByPath.get(p) || 'unknown')
        }
    } catch {
        // Network error — images keep original src
        for (const p of paths) addSkipped(p, 'network_error')
    }

    return { skipped, external, issues }
}

// ─── CSS inlining (stylesheet serialization) ───────────────────────────────────

/**
 * Collect and serialize CSS rules that apply to .markdown-body and its descendants.
 *
 * The exported document is static (no theme switching), so only the CSS variable
 * block for the CURRENT theme is exported, not all 26 themes. Rules are matched
 * by selector: rules targeting markdown-body/its widgets, the exported `:root`,
 * and the current theme's `[data-theme="..."]` block.
 */
function serializeCss(_markdownBodyEl: HTMLElement): string {
    const rules: string[] = []
    const themeId = document.documentElement.getAttribute('data-theme') || 'github-light'

    // Selector for the current theme's CSS variable block. Match both quoted
    // and unquoted forms — CSSOM serializes [data-theme=github-dark] to
    // [data-theme="github-dark"] in real browsers, but jsdom and minified CSS
    // may keep either form. Anchor the theme id at a value boundary so a
    // prefix theme (e.g. "nord") does not also match its longer sibling
    // ("nord-light").
    const currentThemeRe = new RegExp(`data-theme=['"]?${escapeRegExp(themeId)}(?=['"\\s\\]]|$)`)

    // Whether a selector refers to the current theme's variable block.
    const isCurrentThemeBlock = (sel: string): boolean => currentThemeRe.test(sel)

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
                // the exported :root block, the current theme's variable block,
                // or hljs rules (wrapped by [data-hljs-theme="..."]).
                if (
                    sel.includes('.markdown-body') ||
                    sel === ':root' ||
                    isCurrentThemeBlock(sel) ||
                    sel.includes('[data-hljs-theme') ||
                    sel.includes('.markdown-content') ||
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
                    sel.includes('.chat-commit-hash-pending') ||
                    sel.includes('.chat-commit-open-btn') ||
                    sel.includes('.chat-url-open-btn') ||
                    sel.includes('.chat-worktree-btn') ||
                    sel.includes('.mermaid')
                ) {
                    let text = rule.cssText

                    // hljs styles are loaded for both light + dark via
                    // [data-hljs-theme="light"/"dark"] (see hljsThemeWrapper vite
                    // plugin). The exported doc is single-theme, so normalize the
                    // selector to the exported base attribute so the current
                    // theme's hljs colors apply. Match quoted/unquoted forms.
                    text = text.replace(/\[data-hljs-theme=["']?light["']?\]/g, '[data-theme-base="light"]')
                    text = text.replace(/\[data-hljs-theme=["']?dark["']?\]/g, '[data-theme-base="dark"]')

                    rules.push(text)
                }
            } else if (rule instanceof CSSKeyframesRule) {
                // Include @keyframes used by animations in exported content
                const name = rule.name
                if (
                    name.includes('line-flash') ||
                    name.includes('copy-flash') ||
                    name.includes('char-flash') ||
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
                            text = text.replace(/\[data-hljs-theme=["']?light["']?\]/g, '[data-theme-base="light"]')
                            text = text.replace(/\[data-hljs-theme=["']?dark["']?\]/g, '[data-theme-base="dark"]')
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
 * with error indicators. Also handles data-mermaid-error containers
 * (from retry-enabled error fallbacks) by replacing them with static
 * error divs (stripping the retry button which is meaningless in export).
 */
function handleFailedMermaid(clone: HTMLElement): void {
    const mermaidBlocks = clone.querySelectorAll('pre.mermaid, div.mermaid, code.mermaid')
    for (const block of Array.from(mermaidBlocks)) {
        // If it contains an SVG, Mermaid rendered successfully
        if (block.querySelector('svg')) continue

        // Already an error container with data-mermaid-error — replace with
        // a static error div (strip retry button, avoid double-nesting)
        if ((block as HTMLElement).dataset.mermaidError) {
            const errorDiv = document.createElement('div')
            errorDiv.className = 'mermaid-error'
            const em = document.createElement('em')
            em.textContent = 'Diagram failed to render'
            errorDiv.appendChild(em)
            block.parentNode?.replaceChild(errorDiv, block)
            continue
        }

        // Mermaid failed — wrap in error div
        const errorDiv = document.createElement('div')
        errorDiv.className = 'mermaid-error'
        const em = document.createElement('em')
        em.textContent = 'Diagram failed to render'
        errorDiv.appendChild(em)
        block.parentNode?.replaceChild(errorDiv, block)
    }
}

// ─── TOC generation ────────────────────────────────────────────────────────────

/**
 * Build self-contained TOC HTML + JS for the exported document.
 * Uses var() references so colors come from the exported theme variables.
 */
function buildToc(clone: HTMLElement, locale: string): { tocButtonHtml: string; tocDrawerHtml: string; tocCss: string; tocJs: string } {
    // Headings
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

    const isZh = locale === 'zh'
    const tocTitle = isZh ? '目录' : 'Table of Contents'

    // Build TOC list HTML
    const tocItemsHtml = entries.map(e => {
        const indent = (e.level - 1) * 16
        return `<a class="toc-item" data-level="${e.level}" href="#${escapeHtml(e.id)}" style="padding-left: ${8 + indent}px">${escapeHtml(e.text)}</a>`
    }).join('\n')

    // Floating button (inline SVG list icon) — uses CSS class for theme-aware colors
    const tocButtonHtml = `<button id="toc-toggle" class="fab-btn" title="${tocTitle}"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg></button>`

    // TOC drawer
    const tocDrawerHtml = `<div id="toc-drawer"><div class="toc-drawer-title">${tocTitle}</div>${tocItemsHtml}</div>`

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
function buildCodeBlockJs(locale: string): string {
    const isZh = locale === 'zh'
    const copiedText = isZh ? '已复制' : 'Copied'
    const wrapOnText = isZh ? '自动换行已开启' : 'Word wrap on'
    const wrapOffText = isZh ? '自动换行已关闭' : 'Word wrap off'
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
                codeBtn.setAttribute('title', isWrapped ? '${wrapOnText}' : '${wrapOffText}');
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
                tableBtn.setAttribute('title', isWrapped ? '${wrapOnText}' : '${wrapOffText}');
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
        btn.innerHTML = '<span class="copied-feedback">${copiedText}</span>';
        btn.classList.add('is-copied');
        btn.setAttribute('title', '${copiedText}');
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

/** Escape a string for safe use inside a RegExp. */
function escapeRegExp(text: string): string {
    return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// ─── Main export function ─────────────────────────────────────────────────────

export async function exportRenderedHtml(options: ExportOptions): Promise<ExportResult> {
    const { markdownBodyEl, fileName } = options

    // Current UI locale — used to localize labels embedded in the exported
    // standalone HTML (copy feedback, TOC title, word-wrap tooltips).
    const locale = options.locale || 'en'
    const isZh = locale === 'zh'

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

    // 1e. Remove diff markers — interactive UI elements (colored side-bar buttons)
    //     that open the diff drawer in the app. Meaningless in a standalone export.
    for (const marker of Array.from(clone.querySelectorAll('.diff-marker'))) {
        marker.remove()
    }

    // 1e. Note: <foreignObject> elements in Mermaid SVGs are kept as-is.
    //     Chrome may log "Unsafe attempt to load URL" warnings on file:// URLs,
    //     but this does NOT affect rendering — the content displays correctly.
    //     Converting foreignObject HTML to SVG <text> breaks text layout,
    //     so we leave them untouched.

    // 2. Inline images
    const { skipped: skippedImages, external: externalImages, issues } = await inlineImages(clone)

    // 3. Handle failed Mermaid diagrams
    handleFailedMermaid(clone)

    // 4. Serialize CSS from stylesheets (current theme variables only)
    const css = serializeCss(markdownBodyEl)

    // 5. Build TOC
    const { tocButtonHtml, tocDrawerHtml, tocCss, tocJs } = buildToc(clone, locale)

    // 6. Build code block interaction JS
    const codeBlockJs = buildCodeBlockJs(locale)

    // 7. Lightbox JS for exported HTML — opens full-screen image/SVG viewer
    const lightboxJs = `
(function() {
    function openLightbox(content, isSvg) {
        var overlay = document.createElement('div');
        overlay.className = 'export-lightbox';
        if (isSvg) {
            var div = document.createElement('div');
            div.innerHTML = content;
            var svg = div.querySelector('svg');
            if (svg) { svg.style.maxWidth = '95vw'; svg.style.maxHeight = '95vh'; }
            overlay.appendChild(svg || div);
        } else {
            var img = document.createElement('img');
            img.src = content;
            overlay.appendChild(img);
        }
        var closeBtn = document.createElement('button');
        closeBtn.className = 'lb-close-btn';
        closeBtn.textContent = '\\u00d7';
        closeBtn.onclick = function(e) { e.stopPropagation(); overlay.remove(); };
        overlay.appendChild(closeBtn);
        overlay.onclick = function(e) { if (e.target === overlay) overlay.remove(); };
        document.addEventListener('keydown', function handler(e) {
            if (e.key === 'Escape') { overlay.remove(); document.removeEventListener('keydown', handler); }
        });
        document.body.appendChild(overlay);
    }
    document.addEventListener('click', function(e) {
        var expandIcon = e.target.closest('.lightbox-expand-icon');
        if (expandIcon) {
            // Check if the expand icon is inside a mermaid container
            var mermaidContainer = expandIcon.closest('.mermaid');
            if (mermaidContainer) {
                var svg = mermaidContainer.querySelector('svg');
                if (svg) { e.preventDefault(); openLightbox(svg.outerHTML, true); }
                return;
            }
            // Otherwise, it's an image expand icon
            var wrap = expandIcon.closest('.lightbox-img-wrap');
            var img = wrap ? wrap.querySelector('.lightbox-img') : null;
            if (img) { e.preventDefault(); openLightbox(img.src, false); }
            return;
        }
    });
})();`

    // 7. Assemble HTML
    const title = escapeHtml(fileName.replace(/\.md$/i, ''))
    const bodyContent = clone.outerHTML

    // Use the CURRENT app theme for the exported document. The theme CSS
    // variables are scoped to [data-theme="<id>"], so both attributes are
    // required for the markdown styles (tables, code blocks, etc.) to resolve.
    const currentThemeId = document.documentElement.getAttribute('data-theme') || 'github-light'
    const currentThemeBase = document.documentElement.getAttribute('data-theme-base') || (isDarkTheme(currentThemeId) ? 'dark' : 'light')

    const html = `<!DOCTYPE html>
<html lang="${isZh ? 'zh-CN' : 'en'}" data-theme="${escapeHtml(currentThemeId)}" data-theme-base="${escapeHtml(currentThemeBase)}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>
/* ─── Universal box-sizing reset (matches app base.css) ─── */
*, *::before, *::after { box-sizing: border-box; }

/* ─── Theme variables + markdown styles (current theme) ─── */
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

/* ─── Copied feedback text ─── */
.copied-feedback { font-size: 11px; color: var(--accent-color); }

/* ─── TOC + FAB buttons ─── */
${tocCss}

/* ─── Lightbox expand icon (hover overlay on images/mermaid) ─── */
.markdown-body .lightbox-img-wrap { position: relative; display: inline-block; }
.markdown-body .lightbox-img-wrap .lightbox-img { cursor: default; }
.markdown-body .lightbox-img-wrap .lightbox-expand-icon { display: none; position: absolute; top: 4px; right: 4px; width: 24px; height: 24px; border-radius: 4px; background: rgba(0,0,0,0.5); color: #fff; cursor: pointer; z-index: 2; pointer-events: auto; }
@media (hover: hover) { .markdown-body .lightbox-img-wrap:hover .lightbox-expand-icon { display: flex; align-items: center; justify-content: center; } }
.markdown-body .lightbox-img-wrap .lightbox-expand-icon::after { content: '\\2922'; font-size: 14px; line-height: 1; }
.markdown-body .mermaid { position: relative; }
.markdown-body .mermaid .lightbox-expand-icon { display: none; position: absolute; top: 4px; right: 4px; width: 24px; height: 24px; border-radius: 4px; background: rgba(0,0,0,0.5); color: #fff; font-size: 14px; line-height: 24px; text-align: center; cursor: pointer; z-index: 2; align-items: center; justify-content: center; }
.markdown-body .mermaid .lightbox-expand-icon::after { content: '\\2922'; }
@media (hover: hover) { .markdown-body .mermaid:hover .lightbox-expand-icon { display: flex; } }

/* ─── Lightbox overlay ─── */
.export-lightbox { position: fixed; inset: 0; z-index: 9999; background: rgba(0,0,0,0.85); display: flex; align-items: center; justify-content: center; cursor: zoom-out; }
.export-lightbox img, .export-lightbox svg { max-width: 95vw; max-height: 95vh; object-fit: contain; }
.export-lightbox .lb-close-btn { position: absolute; top: 16px; right: 16px; width: 36px; height: 36px; border-radius: 50%; border: none; background: rgba(255,255,255,0.2); color: #fff; font-size: 20px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.export-lightbox .lb-close-btn:hover { background: rgba(255,255,255,0.4); }
</style>
</head>
<body>
${tocButtonHtml}
${tocDrawerHtml}
${bodyContent}
<script>
${tocJs}
${codeBlockJs}
${lightboxJs}
</script>
</body>
</html>`

    return { html, skippedImages, externalImages, issues }
}

/**
 * Map a machine-readable embed-failure reason to a stable i18n reason code.
 * Frontend-generated reasons use their own codes; backend `reason` strings
 * (from /api/file/batch-base64 `skipped[].reason`) are mapped to known codes.
 * Returns a code that resolves under `file.header.exportHtmlReason.<code>`.
 */
export function imageIssueReasonCode(reason: string): string {
    const backendToCode: Record<string, string> = {
        'exceeds 2MB limit': 'too_large',
        'total size exceeded': 'total_too_large',
        'not an image file': 'not_image',
        'access denied': 'access_denied',
        'read error': 'read_error',
        'file not found': 'not_found',
    }
    if (backendToCode[reason]) return backendToCode[reason]
    // Frontend-generated codes (external, api_error, network_error, unknown) pass through.
    return reason
}

/** Resolve an issue reason to a display i18n key. */
export function imageIssueReasonKey(reason: string): string {
    return `file.header.exportHtmlReason.${imageIssueReasonCode(reason)}`
}
