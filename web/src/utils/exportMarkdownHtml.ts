/**
 * Export a rendered markdown FILE as a self-contained HTML document styled like
 * the public share page (ShareView): a top bar + scrollable content column + an
 * optional right TOC rail with a top-bar toggle.
 *
 * Unlike the legacy exportRenderedHtml (which cloned the live .markdown-body
 * DOM), this module re-runs the SAME render pipeline the preview uses
 * (buildMarkdownPreviewDom — shared with MarkdownPreview.vue) against a
 * detached container, so the export is independent of the on-screen preview
 * scroll / render state.
 *
 * Pipeline:
 *  1. buildMarkdownPreviewDom(content, path, projectRoot, homeDir) → annotated html
 *  2. Mount into a detached <div class="markdown-content">
 *  3. verifyFilePaths(detectedPaths) — disk-based annotation correction
 *  4. renderMermaidInElement(el, 'export') — render diagrams (keeps failed ones)
 *  5. Inline images via /api/file/batch-base64
 *  6. Replace failed Mermaid blocks with static error indicators
 *  7. Clean detached DOM (scripts/iframes/.katex-mathml/diff markers)
 *  8. Inline CSS — rules that HIT the exported DOM (serializeCss hit-detection),
 *     KaTeX fonts used by the document (data-URI), + base typography overrides
 *  9. Build a share-style chrome (top bar + scrollable content + right TOC rail,
 *     matching ShareView.vue / the public /share page)
 * 10. Assemble the standalone HTML document using the current app theme
 */

import { buildMarkdownPreviewDom } from '@/composables/useMarkdownRenderPipeline.ts'
import { stampSvgFiguresAsync } from '@/utils/svgMediaFit.ts'
import { verifyFilePaths } from '@/composables/useFilePathAnnotation.ts'
import { renderMermaidInElement } from '@/composables/useMarkdownRenderer.ts'
import { isDarkTheme } from '@/utils/themeMeta.ts'
import { escapeHtml } from '@/utils/html.ts'
import { buildKatexFontCss } from '@/utils/katexFontEmbed.ts'
// The share SPA (ShareView) and this export embed the SAME chrome stylesheet so
// the exported document keeps the exact look of the public share page.
import shareChromeCss from '../../css/share-chrome.css?raw'
// The scrollbar bar's single definition (css/scrollbar.css). Inlined for the
// same reason as the chrome above: the export is opened from file:// or any
// host, so it cannot link the app's stylesheets.
import scrollbarCss from '../../css/scrollbar.css?raw'
// Brand mark for the exported topbar. Inlined as a data URI (`?inline`) rather
// than referenced as `/logo-64.png`: the exported .html is opened from file://
// or any host, where a root-absolute URL would 404. Relative import (not the
// `/logo-64.png` public path) is required — Vite skips inlining for imports
// that resolve inside publicDir and would emit a copied asset URL instead.
import shareLogoDataUri from '../../../assets/logo-64.png?inline'
import { COPY_ICON_SVG, CHECK_ICON_SVG, COPY_FEEDBACK_MS } from '@/utils/copyButton.ts'

// ─── Types ──────────────────────────────────────────────────────────────────────

/**
 * Max paths per `/api/file/batch-base64` request. `ServeFileBatchBase64` rejects
 * more than 50 with 400 TooManyPaths, so image inlining is chunked to stay under
 * it. (Note: the sibling `/api/file/batch-exists` limit is 100, not 50.)
 */
const MAX_BATCH_BASE64_PATHS = 50

export interface ExportOptions {
    /** Raw markdown content of the file being exported. */
    content: string
    /** File path (project-relative). Used to resolve images + path annotations. */
    path: string
    /** Project root; defaults to the app store's current project root. */
    projectRoot?: string
    /** User home directory for ~/ path expansion in annotations. */
    homeDir?: string
    /** File name used for the HTML <title> (defaults to basename of path). */
    fileName?: string
    /** Current UI locale ('zh' | 'en' | ...). Localizes embedded labels. */
    locale?: string
    /** Desktop rendering mode (affects thumbnail width). Defaults to platform detect. */
    isWideScreen?: boolean
}

/** One image that could not be embedded into the exported HTML. */
export interface ImageIssue {
    /** Image path or URL as it appears in the markdown. */
    path: string
    /** Machine-readable reason code (see imageIssueReasonCode). */
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
 * Extract image paths from /api/fs/raw/ URLs in the container DOM, call
 * batch-base64 API, and replace src with data URIs. Prefers data-full-src
 * (full-size original) over an inline thumbnail src.
 */
async function inlineImages(container: HTMLElement): Promise<{ skipped: number; external: number; issues: ImageIssue[] }> {
    const imgs = Array.from(container.querySelectorAll('img')) as HTMLImageElement[]
    if (imgs.length === 0) return { skipped: 0, external: 0, issues: [] }

    const issues: ImageIssue[] = []
    let external = 0
    const pathToImg: Map<string, HTMLImageElement[]> = new Map()

    for (const img of imgs) {
        // Prefer the original full-size URL when present (inline src may be a
        // low-res /api/fs/thumb thumbnail); fall back to the visible src.
        const src = img.getAttribute('data-full-src') || img.getAttribute('src') || ''

        // Skip data URIs (already self-contained)
        if (src.startsWith('data:')) continue

        // External URLs (will need internet)
        if (/^(https?:|\/\/)/i.test(src)) {
            external++
            issues.push({ path: src, reason: 'external', kind: 'external' })
            continue
        }

        // Extract path from /api/fs/raw/...?t=...
        const match = src.match(/^\/api\/fs\/raw\/(.+?)(?:\?.*)?$/)
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

    // Batch fetch base64. Chunked because the endpoint rejects more than
    // MAX_BATCH_BASE64_PATHS with 400 TooManyPaths — and a document can easily
    // reference more images than that. Un-chunked, a single oversized request
    // failed the WHOLE batch, so every local image kept its `/api/fs/raw/...`
    // src and showed up broken in the standalone export.
    const paths = Array.from(pathToImg.keys())
    let skipped = 0

    const addSkipped = (path: string, reason: string) => {
        skipped++
        issues.push({ path, reason, kind: 'skipped' })
    }

    const applyResults = (results: BatchBase64Response['results']) => {
        for (const [imgPath, result] of Object.entries(results || {})) {
            const imgsForPath = pathToImg.get(imgPath)
            if (!imgsForPath) continue
            for (const img of imgsForPath) {
                img.setAttribute('src', `data:${result.mime};base64,${result.data}`)
                img.removeAttribute('data-full-src')
            }
            pathToImg.delete(imgPath)
        }
    }

    for (let i = 0; i < paths.length; i += MAX_BATCH_BASE64_PATHS) {
        const chunk = paths.slice(i, i + MAX_BATCH_BASE64_PATHS)
        try {
            const resp = await fetch('/api/file/batch-base64', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ paths: chunk }),
            })

            if (!resp.ok) {
                // API failed — this chunk's images keep their original src.
                for (const p of chunk) addSkipped(p, 'api_error')
                continue
            }

            const data: BatchBase64Response = await resp.json()
            applyResults(data.results)

            // Remaining in this chunk are paths the server skipped. Use the
            // server-reported reason when available.
            const skippedByPath = new Map((data.skipped || []).map(s => [s.path, s.reason]))
            for (const p of chunk) {
                if (pathToImg.has(p)) addSkipped(p, skippedByPath.get(p) || 'unknown')
            }
        } catch {
            // Network error — this chunk's images keep their original src.
            for (const p of chunk) {
                if (pathToImg.has(p)) addSkipped(p, 'network_error')
            }
        }
    }

    return { skipped, external, issues }
}

// ─── Mermaid error handling ────────────────────────────────────────────────────

/**
 * Replace unrendered Mermaid blocks (pre.mermaid / div.mermaid / code.mermaid
 * without an SVG child) with static error indicators, stripping any retry
 * button (meaningless in a standalone export).
 */
function handleFailedMermaid(container: HTMLElement): void {
    const mermaidBlocks = container.querySelectorAll('pre.mermaid, div.mermaid, code.mermaid')
    for (const block of Array.from(mermaidBlocks)) {
        // If it contains an SVG, Mermaid rendered successfully. (The loading
        // placeholder is an `<svg>` too, but it cannot reach here: the export
        // awaits `renderMermaidInElement`, so by this point every block is
        // either finished artwork or an error container.)
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

// ─── CSS serialization (hit-detection against the exported DOM) ────────────────

/**
 * Is this selector one whose CSS rules matter for rendered markdown content?
 * Used as a fallback for selectors that cannot be tested against the DOM via
 * querySelectorAll (pseudo-class/element selectors throw SyntaxError).
 */
function selectorReferencesContent(selector: string): boolean {
    const contentTokens = [
        '.markdown-body', '.markdown-content', '.katex', '.hljs', '.code-block',
        '.table-block', '.table-wrap', '.chat-file-path', '.chat-file-open-btn',
        '.chat-commit-hash', '.chat-commit-open-btn', '.lightbox', '.mermaid',
        '.mermaid-error', '.line-flash', '.copy-flash', '.char-flash',
        '.chat-audio-player', '.chat-audio-wrapper', '.chat-video-player',
        '.chat-video-wrapper', '.code-line', '.line-num', '.code-text',
        '.code-block-pre', '.toc-', '.export-lightbox',
        // Inline zoom: the figure's own classes are generic, but the state
        // markers only ever appear on a zoomed figure, and the toolbar buttons
        // are named after it. Without these the exported document would ship
        // the zoom SCRIPT but none of its styles.
        '.image-block-wrapper', '.image-block-',
    ]
    return contentTokens.some(tok => selector.includes(tok))
}

/**
 * Collect CSS rules that apply to the exported DOM.
 *
 * Instead of a hand-maintained selector whitelist (which drifts out of sync with
 * component CSS), test every rule's selector against the exported container: a
 * rule is included when `container.matches(sel) || container.querySelector(sel)`
 * finds a match. This is "what you see is what you get" — any rule that styles
 * an element present in the exported document is carried over, and app chrome
 * selectors (.app-container, .header, …) are naturally excluded because those
 * elements don't exist in the exported document.
 *
 * Selectors that cannot be evaluated against the container still need handling:
 * - dynamic state pseudo-classes (:hover/:active/:focus/…) DO NOT throw in
 *   matches()/querySelector() — they just resolve against the element's live
 *   state (false in a detached/static container). We strip the state pseudo
 *   from the selector and test the base selector so those interaction rules
 *   (hover underlines, copy-button hover states, table row hover…) are kept.
 * - pseudo-elements (::before/::after) DO throw in querySelector; those fall
 *   back to selectorReferencesContent().
 *
 * :root (static layout vars), the current theme's [data-theme="..."] variable
 * block, hljs selectors and content keyframes get special handling as before.
 */
function serializeCss(container: HTMLElement, themeId: string): string {
    const rules: string[] = []

    // Anchor the theme id at a value boundary so a prefix theme (e.g. "nord")
    // does not also match its longer sibling ("nord-light").
    const currentThemeRe = new RegExp(`data-theme=['"]?${escapeRegExp(themeId)}(?=['"\\s\\]]|$)`)

    const isCurrentThemeBlock = (sel: string): boolean => currentThemeRe.test(sel)

    // Dynamic state pseudo-classes that don't throw in matches()/querySelector()
    // but would resolve false against a detached container are stripped below so
    // the base rule still matches the exported element.

    // Does a selector match the exported container (or any descendant)?
    const selectorHits = (sel: string): boolean => {
        try {
            if (container.matches(sel)) return true
            if (container.querySelector(sel)) return true
        } catch {
            // Pseudo-element selector (::before/::after) — cannot querySelector.
            return selectorReferencesContent(sel)
        }
        // html/body level: the container is not <html>/<body>, but selectors
        // like `html, body { ... }` or `html { ... }` must still apply to the
        // exported document. Match the leading html/body part manually.
        if (/^(:root|html|body)/.test(sel)) return true
        // Universal / element selectors that affect typography broadly.
        if (sel === '*' || sel === 'body *') return true
        // State pseudo-class rules (e.g. ".markdown-body a:hover"): strip the
        // pseudo and test the base selector against the container. A fresh
        // (non-global) regex avoids lastIndex bleed across rules. When the base
        // does not match (e.g. jsdom cannot match table rows inside detached
        // subtrees) fall back to the token heuristic — the original selector
        // still names content classes, so this keeps the rule without leaking
        // app chrome (.app-container:hover has no content token).
        const statePseudoRe = /:(hover|active|focus|focus-within|focus-visible|visited|checked)\b/
        const stateMatch = sel.match(statePseudoRe)
        if (stateMatch) {
            const base = sel.replace(statePseudoRe, '')
            try {
                if (container.matches(base) || container.querySelector(base)) return true
            } catch {
                // The remaining selector still has pseudo-elements — token fallback.
            }
            return selectorReferencesContent(sel)
        }
        return false
    }

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
                if (
                    sel === ':root' ||
                    isCurrentThemeBlock(sel) ||
                    selectorHits(sel) ||
                    sel.includes('[data-hljs-theme') ||
                    sel.includes('.hljs')
                ) {
                    let text = rule.cssText

                    // hljs styles are loaded for both light + dark via
                    // [data-hljs-theme="light"/"dark"]. The exported doc is
                    // single-theme, so normalize the selector to the exported
                    // base attribute so the current theme's hljs colors apply.
                    text = text.replace(/\[data-hljs-theme=["']?light["']?\]/g, '[data-theme-base="light"]')
                    text = text.replace(/\[data-hljs-theme=["']?dark["']?\]/g, '[data-theme-base="dark"]')

                    rules.push(text)
                }
            } else if (rule instanceof CSSFontFaceRule) {
                // KaTeX fonts handled separately by buildKatexFontCss (data URI
                // embedding depends on which families the document actually uses).
                continue
            } else if (rule instanceof CSSKeyframesRule) {
                const name = rule.name
                // `line-flash` / `copy-flash` keyframes are NOT copied here: the
                // share chrome (share-chrome.css, inlined verbatim below) already
                // carries its own self-contained definitions, so re-serializing
                // them from the app stylesheets would duplicate them in the export.
                if (name.includes('char-flash') || name.includes('url-btn-spin') || name.includes('mermaid')) {
                    rules.push(rule.cssText)
                }
            } else if (rule instanceof CSSMediaRule) {
                // Include media rules that contain content-hitting rules.
                const innerRules: string[] = []
                for (const inner of Array.from(rule.cssRules)) {
                    if (inner instanceof CSSStyleRule) {
                        const sel = inner.selectorText
                        // Same two-tier test as the top-level branch: `selectorHits`
                        // cannot match a STATE selector (`.is-zooming` is only ever
                        // set at runtime, so nothing in the static export carries
                        // it), and the token fallback is what keeps those rules. The
                        // media branch used to lack the fallback, which silently
                        // dropped every `.is-zooming` rule — the exported document
                        // then shipped the zoom script but no styles for the states
                        // it toggles (invisible reset button, animated zoom steps).
                        if (selectorHits(sel) || sel.includes('.hljs') || selectorReferencesContent(sel)) {
                            let text = inner.cssText
                            text = text.replace(/\[data-hljs-theme=["']?light["']?\]/g, '[data-theme-base="light"]')
                            text = text.replace(/\[data-hljs-theme=["']?dark["']?\]/g, '[data-theme-base="dark"]')
                            innerRules.push(text)
                        }
                    } else if (inner instanceof CSSKeyframesRule) {
                        const name = inner.name
                        // `line-flash` / `copy-flash` come from the inlined
                        // share-chrome.css; only diff / spin / mermaid keyframes
                        // (which live solely in app stylesheets) are re-serialized.
                        if (name.includes('char-flash') || name.includes('diff-marker') || name.includes('url-btn-spin') || name.includes('mermaid')) {
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

// ─── Share-style chrome (aligned with the app's /share/{token} ShareView) ───

/**
 * Build the exported document shell by reusing the public share page's visual
 * language (ShareView.vue):
 *
 *   .share-topbar   — file name on the left + a TOC toggle button (no download;
 *                     the recipient already holds the .html offline).
 *   .share-body     — flex row that fills the viewport: the .markdown-body
 *                     content scrolls in its own column and the TOC sits in a
 *                     fixed-width right rail (240px) with a header + item list.
 *
 * The chrome CSS is NOT duplicated here: css/share-chrome.css is the single
 * shared source — ShareView.vue imports it and the caller inlines the same file
 * verbatim into the exported <style>. Only export-specific overrides live here.
 *
 * The TOC toggle lives in the top bar; it is the ONLY open/close control — the
 * TOC panel itself carries no collapse button. On narrow viewports the rail is
 * hidden and the button simply reveals/hides it.
 */
function buildTocStandalone(container: HTMLElement, locale: string, displayName: string): { viewOpenHtml: string; topbarHtml: string; shellOpenHtml: string; contentOpenHtml: string; contentCloseHtml: string; sidebarHtml: string; shellCloseHtml: string; viewCloseHtml: string; tocCss: string; tocJs: string } {
    const empty = { viewOpenHtml: '', topbarHtml: '', shellOpenHtml: '', contentOpenHtml: '', contentCloseHtml: '', sidebarHtml: '', shellCloseHtml: '', viewCloseHtml: '', tocCss: '', tocJs: '' }

    // Headings
    const headings = Array.from(container.querySelectorAll('h1, h2, h3, h4, h5, h6')) as HTMLHeadingElement[]
    if (headings.length === 0) return empty

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

    if (entries.length === 0) return empty

    const isZh = locale === 'zh'
    const tocTitle = isZh ? '目录' : 'Table of Contents'
    const tocToggleTitle = isZh ? '收起目录' : 'Collapse TOC'

    // Level-based indentation mirrors the share page (level 1 is flush).
    const tocItemsHtml = entries.map(e =>
        `<button type="button" class="share-toc-item" data-level="${e.level}" data-target="#${escapeHtml(e.id)}" style="padding-left:${8 + (e.level - 1) * 14}px">${escapeHtml(e.text)}</button>`
    ).join('\n')

    // Top bar — file name on the left + the sole TOC open/close toggle button
    // in .share-top-actions (same skeleton as ShareView.vue). The single list
    // icon is the share page's TOC affordance; no download action — the offline
    // .html already holds everything.
    const topbarHtml = `<div class="share-topbar"><img class="share-logo" src="${shareLogoDataUri}" alt="ClawBench"><span class="share-file-name">${escapeHtml(displayName)}</span><span class="share-spacer"></span><div class="share-top-actions"><button id="toc-toggle" class="share-btn" type="button" title="${tocToggleTitle}" aria-expanded="true" aria-controls="share-toc">
<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>
</button></div></div>`

    const viewOpenHtml = `<div class="share-view">`
    const shellOpenHtml = `<div class="share-body" data-toc-open="true">`
    const contentOpenHtml = `<div class="share-content">`
    const contentCloseHtml = `</div>`
    const sidebarHtml = `<aside id="share-toc" class="share-toc"><div class="share-toc-title">${tocTitle}</div>${tocItemsHtml}</aside>`
    const shellCloseHtml = `</div>`
    const viewCloseHtml = `</div>`

    // TOC JS: topbar toggle open/close + click-to-scroll with highlight-hold
    // window + IntersectionObserver scroll-follow (mirrors ShareView).
    const tocJs = `
(function() {
    var body = document.querySelector('.share-body');
    var toggle = document.getElementById('toc-toggle');
    var sidebar = document.getElementById('share-toc');
    if (!body || !toggle || !sidebar) return;

    var open = body.getAttribute('data-toc-open') !== 'false';
    // On first load match the share page: start collapsed on narrow viewports.
    if (window.innerWidth < 900 && open) setState(false);
    function setState(next) {
        open = next;
        body.setAttribute('data-toc-open', next ? 'true' : 'false');
        toggle.setAttribute('aria-expanded', next ? 'true' : 'false');
        toggle.setAttribute('title', next ? '${tocToggleTitle}' : '${isZh ? '展开目录' : 'Expand TOC'}');
        if (next) {
            var act = sidebar.querySelector('.share-toc-item.active');
            if (act) act.scrollIntoView({ behavior: 'auto', block: 'nearest' });
        }
    }
    toggle.addEventListener('click', function() { setState(!open); });
    // Click a TOC entry: smooth-scroll to the heading + keep it highlighted
    // for a short window so the scroll-follow observer does not steal it.
    var holdUntil = 0;
    function hold() { holdUntil = Date.now() + 1500; }
    function held() { return Date.now() < holdUntil; }
    function setActive(item) {
        var act = sidebar.querySelector('.share-toc-item.active');
        if (act) act.classList.remove('active');
        if (item) item.classList.add('active');
    }
    sidebar.addEventListener('click', function(e) {
        var btn = e.target.closest('.share-toc-item');
        if (!btn) return;
        e.preventDefault();
        setActive(btn);
        hold();
        var id = btn.getAttribute('data-target').slice(1);
        var el = document.getElementById(id);
        if (el) {
            el.scrollIntoView({ behavior: 'smooth', block: 'start' });
            // Flash the jumped heading (same .line-flash as the share SPA and the
            // in-app markdown preview anchor jump; keyframes come from the shared
            // share-chrome.css embedded below).
            el.classList.add('line-flash');
            el.addEventListener('animationend', function() { el.classList.remove('line-flash'); }, { once: true });
        }
    });

    // Scroll-follow: highlight the heading currently in the upper part of the viewport.
    if ('IntersectionObserver' in window) {
        var items = sidebar.querySelectorAll('.share-toc-item');
        var byId = {};
        for (var i = 0; i < items.length; i++) byId[items[i].getAttribute('data-target').slice(1)] = items[i];
        var observer = new IntersectionObserver(function(entries) {
            if (held()) return;
            for (var j = 0; j < entries.length; j++) {
                if (entries[j].isIntersecting) {
                    var link = byId[entries[j].target.id];
                    if (link) { setActive(link); break; }
                }
            }
        }, { rootMargin: '-60px 0px -70% 0px' });
        var headings = document.querySelectorAll('.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4, .markdown-body h5, .markdown-body h6');
        for (var k = 0; k < headings.length; k++) observer.observe(headings[k]);
    }
})();`

    // Export-only chrome CSS. The visual chrome (.share-topbar, .share-body,
    // .share-toc, .share-toc-item, .share-btn, …) is NOT duplicated here — it is
    // the shared css/share-chrome.css file that ShareView.vue also imports, and
    // gets inlined verbatim into the exported <style> in the caller. This block
    // only carries what is specific to a standalone document: the fixed-height
    // viewport shell, the markdown reading column, and the TOC open/close state
    // (the SPA hides the rail by unmounting it via v-if, a static export cannot).
    const tocCss = `
/* ─── Export-only chrome overrides on top of the shared share-chrome.css ─── */
html, body { height: 100%; margin: 0; }
.share-view { height: 100vh; }
.share-content .markdown-body {
    overflow: visible; overflow-x: hidden; flex: none; min-height: 100%;
    position: relative; width: 100%; box-sizing: border-box;
}
.share-body[data-toc-open="false"] .share-toc { display: none; }

/* Wide screens: cap the reading column (ShareView centers at 1080px) */
@media (min-width: 1100px) {
    .share-content .markdown-body { max-width: 1080px; }
}

/* Narrow viewport: default collapsed; topbar toggle reveals the rail */
@media (max-width: 899px) {
    .share-toc { display: none !important; }
    .share-body[data-toc-open="true"] .share-toc { display: block !important; }
}`

    return { viewOpenHtml, topbarHtml, shellOpenHtml, contentOpenHtml, contentCloseHtml, sidebarHtml, shellCloseHtml, viewCloseHtml, tocCss, tocJs }
}

// ─── Code block + table block interaction JS ──────────────────────────────────

function buildCodeBlockJs(locale: string): string {
    const isZh = locale === 'zh'
    const copiedText = isZh ? '已复制' : 'Copied'
    const wrapOnText = isZh ? '自动换行已开启' : 'Word wrap on'
    const wrapOffText = isZh ? '自动换行已关闭' : 'Word wrap off'
    // Same glyphs and timing as the in-app copy buttons (utils/copyButton.ts).
    // The exported .html is standalone — it cannot import the app — so the
    // values are inlined here rather than referenced.
    const copyIconSvg = COPY_ICON_SVG
    const checkIconSvg = CHECK_ICON_SVG
    const feedbackMs = COPY_FEEDBACK_MS
    return `
(function() {
    function closeAllTableMenus(except) {
        var menus = document.querySelectorAll('.table-block-copy-menu.is-open');
        for (var i = 0; i < menus.length; i++) {
            if (menus[i] === except) continue;
            menus[i].classList.remove('is-open');
            menus[i].style.display = 'none';
            var trigger = menus[i].previousElementSibling;
            if (trigger) trigger.setAttribute('aria-expanded', 'false');
        }
    }

    // The menu is position:fixed (css/code-block-header.css) so a short table's
    // overflow:hidden cannot clip its third item. Fixed coordinates must be
    // MEASURED, not assumed: CSS zoom on <html> or a transformed ancestor moves
    // the containing-block origin AND scales the offsets, so probe both.
    function positionTableMenu(trigger, menu) {
        var width = menu.offsetWidth;
        var height = menu.offsetHeight;
        var triggerRect = trigger.getBoundingClientRect();
        var PROBE = 100;
        menu.style.left = '0px'; menu.style.top = '0px';
        var atZero = menu.getBoundingClientRect();
        menu.style.left = PROBE + 'px'; menu.style.top = PROBE + 'px';
        var atProbe = menu.getBoundingClientRect();
        var scaleX = (atProbe.left - atZero.left) / PROBE || 1;
        var scaleY = (atProbe.top - atZero.top) / PROBE || 1;
        var margin = 8, gap = 4;
        var vw = window.innerWidth, vh = window.innerHeight;
        var left = triggerRect.right - width;
        left = Math.min(left, vw - width - margin);
        left = Math.max(margin, left);
        var top = triggerRect.bottom + gap;
        if (top + height > vh - margin) {
            var above = triggerRect.top - gap - height;
            if (above >= margin) top = above;
        }
        top = Math.max(margin, Math.min(top, vh - height - margin));
        menu.style.left = ((left - atZero.left) / scaleX) + 'px';
        menu.style.top = ((top - atZero.top) / scaleY) + 'px';
    }

    window.addEventListener('scroll', function() {
        var menu = document.querySelector('.table-block-copy-menu.is-open');
        if (!menu) return;
        var trigger = menu.previousElementSibling;
        if (!trigger) return;
        var rect = trigger.getBoundingClientRect();
        if (rect.bottom < 0 || rect.top > window.innerHeight) { closeAllTableMenus(); return; }
        positionTableMenu(trigger, menu);
    }, true);
    window.addEventListener('resize', function() {
        var menu = document.querySelector('.table-block-copy-menu.is-open');
        var trigger = menu && menu.previousElementSibling;
        if (trigger) positionTableMenu(trigger, menu);
    });

    document.addEventListener('click', function(e) {
        // Close table copy menus when clicking outside
        if (!e.target.closest('.table-block-copy-menu') && !e.target.closest('.table-block-copy-btn')) {
            closeAllTableMenus();
        }

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
        var tableBtn = e.target.closest('.table-block-copy-btn, .table-block-wrap-btn, .table-block-copy-menu-item');
        if (tableBtn) {
            e.preventDefault();
            e.stopPropagation();
            var wrapper = tableBtn.closest('.table-block-wrapper');
            if (!wrapper) return;
            var action = tableBtn.getAttribute('data-action');
            if (action === 'open-copy-menu') {
                var menu = tableBtn.nextElementSibling;
                closeAllTableMenus(menu);
                var isOpen = menu && menu.classList.contains('is-open');
                if (menu) {
                    if (isOpen) {
                        menu.classList.remove('is-open');
                        menu.style.display = 'none';
                        tableBtn.setAttribute('aria-expanded', 'false');
                    } else {
                        menu.classList.add('is-open');
                        menu.style.display = 'block';
                        tableBtn.setAttribute('aria-expanded', 'true');
                        positionTableMenu(tableBtn, menu);
                    }
                }
            } else if (action === 'copy-md' || action === 'copy-html' || action === 'copy-tsv') {
                var table = wrapper.querySelector('table');
                if (!table) return;
                var text;
                if (action === 'copy-md') {
                    text = tableToMarkdown(table);
                } else if (action === 'copy-html') {
                    text = tableToCleanHtml(table);
                } else {
                    text = tableToText(table);
                }
                copyText(text, tableBtn);
                var menu = tableBtn.closest('.table-block-copy-menu');
                if (menu) {
                    menu.classList.remove('is-open');
                    menu.style.display = 'none';
                    var trigger = menu.previousElementSibling;
                    if (trigger) trigger.setAttribute('aria-expanded', 'false');
                }
            } else if (action === 'wrap') {
                wrapper.classList.toggle('word-wrap');
                tableBtn.classList.toggle('is-wrapped');
                var isWrapped = wrapper.classList.contains('word-wrap');
                tableBtn.setAttribute('title', isWrapped ? '${wrapOnText}' : '${wrapOffText}');
            }
            return;
        }
    });

    var COPY_ICON_SVG = ${JSON.stringify(copyIconSvg)};
    var CHECK_ICON_SVG = ${JSON.stringify(checkIconSvg)};

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
        // Menu items carry a text label as their identity (Markdown / HTML /
        // TSV) — the tint alone is their feedback; replacing the label with a
        // check would hide which format was copied.
        if (btn.classList.contains('table-block-copy-menu-item')) {
            btn.classList.add('is-copied');
            setTimeout(function() { btn.classList.remove('is-copied'); }, ${feedbackMs});
            return;
        }
        if (btn.classList.contains('is-copied')) return;
        var origTitle = btn.getAttribute('title') || '';
        btn.innerHTML = CHECK_ICON_SVG;
        btn.classList.add('is-copied');
        btn.setAttribute('title', '${copiedText}');
        setTimeout(function() {
            btn.innerHTML = COPY_ICON_SVG;
            btn.classList.remove('is-copied');
            btn.setAttribute('title', origTitle);
        }, ${feedbackMs});
    }

    function tableRows(table) {
        var rows = [];
        var trs = table.querySelectorAll('tr');
        for (var i = 0; i < trs.length; i++) {
            var cells = trs[i].querySelectorAll('th, td');
            var vals = [];
            for (var j = 0; j < cells.length; j++) {
                vals.push(cells[j].textContent.trim());
            }
            rows.push(vals);
        }
        return rows;
    }

    function tableToText(table) {
        var rows = tableRows(table);
        var lines = [];
        for (var i = 0; i < rows.length; i++) lines.push(rows[i].join('\\t'));
        return lines.join('\\n');
    }

    function escapeMdCell(s) {
        return s.replace(/\\|/g, '\\\\|').replace(/\\r?\\n/g, '<br>');
    }

    function cellAlign(cell) {
        var align = (cell.style && cell.style.textAlign) || '';
        if (align === 'left') return ':---';
        if (align === 'right') return '---:';
        if (align === 'center') return ':---:';
        return '---';
    }

    function tableToMarkdown(table) {
        var lines = [];
        var thead = table.querySelector('thead');
        var headers = [];
        var aligns = [];
        if (thead) {
            var ths = thead.querySelectorAll('th');
            for (var i = 0; i < ths.length; i++) {
                headers.push(escapeMdCell(ths[i].textContent.trim()));
                aligns.push(cellAlign(ths[i]));
            }
        } else {
            var first = table.querySelector('tr');
            if (first) {
                var firstCells = first.querySelectorAll('th, td');
                for (var j = 0; j < firstCells.length; j++) {
                    headers.push(escapeMdCell(firstCells[j].textContent.trim()));
                    aligns.push(cellAlign(firstCells[j]));
                }
            }
        }
        if (headers.length > 0) {
            lines.push('| ' + headers.join(' | ') + ' |');
            lines.push('| ' + aligns.join(' | ') + ' |');
        }
        var rows = tableRows(table);
        // First row in the table is the header row when headers were extracted from it
        var start = headers.length > 0 ? 1 : 0;
        for (var r = start; r < rows.length; r++) {
            var cells = [];
            for (var c = 0; c < rows[r].length; c++) cells.push(escapeMdCell(rows[r][c]));
            lines.push('| ' + cells.join(' | ') + ' |');
        }
        return lines.join('\\n');
    }

    function escapeHtmlCell(s) {
        return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }

    function tableToCleanHtml(table) {
        var out = ['<table>'];
        var thead = table.querySelector('thead');
        if (thead) {
            out.push('<thead>');
            var trs1 = thead.querySelectorAll('tr');
            for (var i = 0; i < trs1.length; i++) {
                out.push('<tr>');
                var cells1 = trs1[i].querySelectorAll('th, td');
                for (var j = 0; j < cells1.length; j++) {
                    var tag1 = cells1[j].tagName === 'TH' ? 'th' : 'td';
                    out.push('<' + tag1 + '>' + escapeHtmlCell(cells1[j].textContent) + '</' + tag1 + '>');
                }
                out.push('</tr>');
            }
            out.push('</thead>');
        }
        var tbody = table.querySelector('tbody');
        if (tbody) {
            out.push('<tbody>');
            var trs2 = tbody.querySelectorAll('tr');
            for (var k = 0; k < trs2.length; k++) {
                out.push('<tr>');
                var cells2 = trs2[k].querySelectorAll('td, th');
                for (var m = 0; m < cells2.length; m++) {
                    var tag2 = cells2[m].tagName === 'TH' ? 'th' : 'td';
                    out.push('<' + tag2 + '>' + escapeHtmlCell(cells2[m].textContent) + '</' + tag2 + '>');
                }
                out.push('</tr>');
            }
            out.push('</tbody>');
        }
        if (!thead && !tbody) {
            var trs3 = table.querySelectorAll('tr');
            for (var n = 0; n < trs3.length; n++) {
                out.push('<tr>');
                var cells3 = trs3[n].querySelectorAll('td, th');
                for (var p = 0; p < cells3.length; p++) {
                    var tag3 = cells3[p].tagName === 'TH' ? 'th' : 'td';
                    out.push('<' + tag3 + '>' + escapeHtmlCell(cells3[p].textContent) + '</' + tag3 + '>');
                }
                out.push('</tr>');
            }
        }
        out.push('</table>');
        return out.join('');
    }
})();`
}

// ─── Lightbox JS ───────────────────────────────────────────────────────────────

/**
 * Standalone lightbox with the same zoom/pan interactions as the in-app
 * Lightbox.vue:
 *  - wheel zoom (clamped to [0.1, 10], anchored to the fit-to-screen scale)
 *  - drag to pan once zoomed beyond fit (mouse + single-touch)
 *  - pinch to zoom (touch)
 *  - clicking the overlay backdrop closes; Esc / × close
 * SVGs (mermaid) are wrapped in a container div carrying the transform, with
 * explicit width/height fit onto the viewport — mirroring Lightbox.vue's
 * onSvgMounted; <img> carries its own transform like onImageLoad.
 */
function buildLightboxJs(): string {
    return `
(function() {
    var bodyOverflow = '';

    function openLightbox(content, isSvg) {
        var overlay = document.createElement('div');
        overlay.className = 'export-lightbox';

        var view = document.createElement('div');
        view.className = 'export-lightbox-view';
        overlay.appendChild(view);

        // Track how many export lightboxes are open so body overflow restore is
        // balanced when several are opened in a row (the app uses a singleton).
        window.__exportLbCount = (window.__exportLbCount || 0) + 1;

        var isDragging = false;
        var scale = 1;
        var fitScale = 1;
        var tx = 0, ty = 0, lastTx = 0, lastTy = 0;
        var dragStartX = 0, dragStartY = 0;
        var contentEl = null;   // element receiving the transform

        function clamp(v, lo, hi) { return Math.min(Math.max(v, lo), hi); }

        function applyTransform() {
            if (!contentEl) return;
            contentEl.style.transform = 'translate(' + tx + 'px, ' + ty + 'px) scale(' + scale + ')';
        }
        function setScale(v, resetPan) {
            scale = clamp(v, 0.1, 10);
            if (resetPan) { tx = 0; ty = 0; lastTx = 0; lastTy = 0; }
            applyTransform();
        }
        // While dragging, kill the transition so panning tracks the pointer 1:1
        // (mirrors App imgStyle: transition none while isDragging).
        function setDragging(on) {
            isDragging = on;
            if (contentEl) contentEl.style.transition = on ? 'none' : '';
        }

        // Size an <img> explicitly (like App imgStyle) so scale() operates on a
        // real fitted dimension instead of CSS max-* constraints.
        function sizeImgToFit(img, nw, nh) {
            var pad = 56;
            var availW = overlay.clientWidth - pad * 2;
            var availH = overlay.clientHeight - pad * 2;
            var s = 1;
            if (nw > 0 && nh > 0 && availW > 0 && availH > 0) {
                s = Math.min(availW / nw, availH / nh, 1);
            }
            img.style.width = Math.round(nw * s) + 'px';
            img.style.height = Math.round(nh * s) + 'px';
            img.style.maxWidth = 'none';
            img.style.maxHeight = 'none';
            fitScale = 1;
            scale = 1;
            applyTransform();
        }

        if (isSvg) {
            var holder = document.createElement('div');
            holder.innerHTML = content;
            var svg = holder.querySelector('svg');
            if (svg) {
                svg.removeAttribute('width');
                svg.removeAttribute('height');
                svg.style.maxWidth = 'none';
                svg.style.maxHeight = 'none';
                view.appendChild(svg);
                contentEl = svg;
                // Fit the SVG onto the viewport after it is in the layout (like
                // Lightbox.vue onSvgMounted): measure via viewBox (fallback
                // getBBox) and set an explicit fitted width/height so scale=1
                // is the fully-visible baseline.
                requestAnimationFrame(function() {
                    var w = 0, h = 0;
                    if (svg.viewBox && svg.viewBox.baseVal && svg.viewBox.baseVal.width > 0) {
                        w = svg.viewBox.baseVal.width;
                        h = svg.viewBox.baseVal.height;
                    } else if (typeof svg.getBBox === 'function') {
                        try {
                            var bb = svg.getBBox();
                            w = bb.width; h = bb.height;
                        } catch (e) { w = 0; h = 0; }
                    }
                    if (w > 0 && h > 0) {
                        var pad = 56;
                        var availW = overlay.clientWidth - pad * 2;
                        var availH = overlay.clientHeight - pad * 2;
                        var s = Math.min(availW / w, availH / h);
                        svg.setAttribute('width', Math.round(w * s) + 'px');
                        svg.setAttribute('height', Math.round(h * s) + 'px');
                        // Baseline scale = 1 (the SVG is already sized to fit).
                        fitScale = 1;
                        scale = 1;
                        applyTransform();
                    }
                });
            } else {
                view.appendChild(holder);
                contentEl = holder;
            }
        } else {
            var img = document.createElement('img');
            img.src = content;
            img.draggable = false;
            view.appendChild(img);
            contentEl = img;
            if (img.complete && img.naturalWidth > 0) {
                sizeImgToFit(img, img.naturalWidth, img.naturalHeight);
            } else {
                img.addEventListener('load', function() {
                    sizeImgToFit(img, img.naturalWidth, img.naturalHeight);
                });
            }
        }

        // ── Close ──
        function closeLightbox() {
            overlay.remove();
            document.removeEventListener('keydown', onKey);
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup', onUp);
            window.__exportLbCount = (window.__exportLbCount || 1) - 1;
            if (window.__exportLbCount <= 0) {
                document.body.style.overflow = bodyOverflow;
            }
        }
        function onKey(e) { if (e.key === 'Escape') closeLightbox(); }

        var closeBtn = document.createElement('button');
        closeBtn.className = 'lb-close-btn';
        closeBtn.textContent = '\\u00d7';
        closeBtn.onclick = function(e) { e.stopPropagation(); closeLightbox(); };
        overlay.appendChild(closeBtn);

        overlay.addEventListener('click', function(e) {
            if (e.target === overlay || e.target === view) closeLightbox();
        });

        // ── Wheel zoom ──
        overlay.addEventListener('wheel', function(e) {
            e.preventDefault();
            var delta = e.deltaY > 0 ? 0.85 : 1.2;
            var newScale = scale * delta;
            if (newScale < fitScale && scale >= fitScale) {
                setScale(newScale, true);
            } else {
                setScale(newScale, false);
            }
        }, { passive: false });

        // ── Mouse pan (only when zoomed beyond fit) ──
        function canDrag() { return scale > fitScale + 0.001; }

        overlay.addEventListener('mousedown', function(e) {
            if (e.button !== 0) return;
            if (!canDrag()) return;
            e.preventDefault();
            setDragging(true);
            dragStartX = e.clientX - lastTx;
            dragStartY = e.clientY - lastTy;
        });
        function onMove(e) {
            if (!isDragging) return;
            tx = e.clientX - dragStartX;
            ty = e.clientY - dragStartY;
            applyTransform();
        }
        function onUp() {
            if (isDragging) {
                setDragging(false);
                lastTx = tx; lastTy = ty;
            }
        }
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);

        // ── Touch: pinch zoom + single-finger pan ──
        var touchStartScale = 1, touchStartDist = 0;
        overlay.addEventListener('touchstart', function(e) {
            if (e.touches.length === 2) {
                touchStartDist = Math.hypot(
                    e.touches[0].clientX - e.touches[1].clientX,
                    e.touches[0].clientY - e.touches[1].clientY
                );
                touchStartScale = scale;
                setDragging(false);
            } else if (e.touches.length === 1) {
                if (canDrag()) {
                    setDragging(true);
                    dragStartX = e.touches[0].clientX - lastTx;
                    dragStartY = e.touches[0].clientY - lastTy;
                }
            }
        }, { passive: true });
        overlay.addEventListener('touchmove', function(e) {
            if (e.touches.length === 2) {
                e.preventDefault();
                var dist = Math.hypot(
                    e.touches[0].clientX - e.touches[1].clientX,
                    e.touches[0].clientY - e.touches[1].clientY
                );
                if (touchStartDist > 0) {
                    setScale(touchStartScale * dist / touchStartDist, false);
                }
            } else if (e.touches.length === 1 && isDragging) {
                e.preventDefault();
                tx = e.touches[0].clientX - dragStartX;
                ty = e.touches[0].clientY - dragStartY;
                applyTransform();
            }
        }, { passive: false });
        overlay.addEventListener('touchend', function() {
            if (isDragging) {
                setDragging(false);
                lastTx = tx; lastTy = ty;
            }
            touchStartDist = 0;
        });

        document.addEventListener('keydown', onKey);
        if (window.__exportLbCount <= 1) {
            bodyOverflow = document.body.style.overflow;
            document.body.style.overflow = 'hidden';
        }
        document.body.appendChild(overlay);
    }

    document.addEventListener('click', function(e) {
        var expandAffordance = e.target.closest('.image-block-view-btn');
        if (expandAffordance) {
            e.preventDefault();
            // .image-block-view-btn sits in the figure header above the content:
            // the figure content may be an image, a rendered mermaid diagram or
            // a bare inline <svg>. Resolve in that order from the shared wrapper.
            var wrap = expandAffordance.closest('.image-block-wrapper');
            var img = wrap ? wrap.querySelector('.lightbox-img') : null;
            if (img) {
                var fullSrc = img.getAttribute('data-full-src') || img.src;
                openLightbox(fullSrc, false);
                return;
            }
            var svgEl = wrap ? wrap.querySelector('.mermaid svg, svg.lightbox-svg') : null;
            if (svgEl) {
                openLightbox(svgEl.outerHTML, true);
            }
        }
    });
})();`
}

/**
 * Inline zoom/pan for the exported document — the standalone mirror of
 * `inlineZoom.ts`.
 *
 * Kept in sync by hand with the module: the export is a single self-contained
 * HTML file with no bundler, so it cannot import the app's TypeScript. The
 * behaviour it reproduces is deliberately the narrow part of the contract —
 * pointer devices only, wheel zooms around the cursor, drag pans, reset button
 * returns to fit, Escape leaves the zoom. Touch is not handled here either, for
 * the same reason: a pinch would fight page scrolling.
 *
 * Because it is a hand-copied mirror it DRIFTS, and it has: the wheel rule, the
 * scale cap and the table-cell exclusion all lagged the app at once. Every one
 * of those is now pinned by a test in exportMarkdownHtml.test.ts that executes
 * this script against a real DOM (see "exported inline zoom"), so a future
 * divergence fails instead of shipping.
 */
export function buildInlineZoomJs(): string {
    return `
(function() {
    // Same gate as the app: precise pointer that can hover. Touch never gets
    // inline zoom (the lightbox stays the touch path).
    var mq = window.matchMedia && window.matchMedia('(hover: hover) and (pointer: fine)');
    if (!mq || !mq.matches) return;

    // MAX mirrors inlineZoom's MAX_SAFE_SCALE. A low cap made the gesture feel
    // broken ("放大到全宽就无法再放大") right after the artwork filled the frame,
    // which is the point a reader most wants to keep going. This is only a
    // guard against numeric runaway from a stray event storm.
    var MIN = 1, MAX = 64, STEP = 1.15, DRAG = 3;
    var active = null;   // { fig, media, scale, tx, ty }
    var panning = null;

    function mediaOf(fig) {
        if (!fig) return null;
        return fig.querySelector(':scope > .lightbox-img-wrap > img')
            || fig.querySelector(':scope > .lightbox-svg-wrap > svg.lightbox-svg')
            || fig.querySelector(':scope > .mermaid > svg');
    }

    function figFrom(target) {
        if (!target || !target.closest) return null;
        var fig = target.closest('.image-block-wrapper');
        if (!fig) return null;
        // Never hijack the floating toolbar.
        if (target.closest('.image-block-header')) return null;
        // Table cells opt out, exactly as in the app: a figure inside a cell is
        // a thumbnail in a grid, and zooming it grows the whole row. Checked
        // structurally (td/th) rather than by a marker class — the markdown
        // renderer emits no such class.
        if (fig.closest('td, th')) return null;
        return mediaOf(fig) ? fig : null;
    }

    // Natural (unzoomed) size of the media — the base the zoom multiplies.
    function naturalSize(s) {
        var m = s.media;
        if (m.tagName === 'IMG' && m.naturalWidth > 0) {
            return { w: m.naturalWidth, h: m.naturalHeight };
        }
        var vb = m.viewBox && m.viewBox.baseVal;
        if (vb && vb.width > 0) return { w: vb.width, h: vb.height };
        var r = m.getBoundingClientRect();
        return { w: r.width, h: r.height };
    }

    // Size at the current scale, in LAYOUT pixels. At rest the element is the
    // viewport box and the artwork is letterboxed inside it by object-fit:
    // contain (img) / preserveAspectRatio (svg) — so a diagram smaller than the
    // column still fills it. Writing width/height (not transform: scale) makes
    // the browser re-rasterise, keeping it sharp.
    function scaledSize(s) {
        var frame = s.fig.getBoundingClientRect();
        var cell = s.media.parentElement;
        var box = cell ? cell.getBoundingClientRect() : frame;
        var bw = box.width || frame.width;
        var bh = box.height || frame.height;
        // Scale on the ARTWORK's ratio, not the frame's: an <svg> keeps
        // preserveAspectRatio (meet), so a box whose ratio differs letterboxes
        // the content — the element grows but the drawing does not.
        var n = naturalSize(s);
        if (!(n.w > 0 && n.h > 0)) return { w: bw * s.scale, h: bh * s.scale };
        var fit = Math.min(bw / n.w, bh / n.h);
        return { w: n.w * fit * s.scale, h: n.h * fit * s.scale };
    }

    function clamp(s) {
        var frame = s.fig.getBoundingClientRect();
        var z = scaledSize(s);
        // The box may be larger than the artwork inside it (letterboxing), so
        // the travel limit comes from the ARTWORK's rendered size.
        var n = naturalSize(s);
        var ratio = (n.w > 0 && n.h > 0) ? n.w / n.h : 1;
        var boxRatio = z.h > 0 ? z.w / z.h : ratio;
        var artW = ratio > boxRatio ? z.w : z.h * ratio;
        var artH = ratio > boxRatio ? z.w / ratio : z.h;
        var maxX = Math.max(0, (artW - frame.width) / 2);
        var maxY = Math.max(0, (artH - frame.height) / 2);
        s.tx = Math.min(maxX, Math.max(-maxX, s.tx));
        s.ty = Math.min(maxY, Math.max(-maxY, s.ty));
    }

    function apply(s) {
        clamp(s);
        var z = scaledSize(s);
        s.media.style.width = z.w + 'px';
        s.media.style.height = z.h + 'px';
        s.media.style.maxWidth = 'none';
        s.media.style.maxHeight = 'none';
        // Mermaid writes width="100%" as an ATTRIBUTE; it pins the width while
        // the height grows, so the diagram stretches instead of zooming.
        if (s.media.tagName.toLowerCase() === 'svg') {
            if (s.savedW === undefined) s.savedW = s.media.getAttribute('width');
            s.media.removeAttribute('width');
        }
        // The resting CSS centres with translate(-50%,-50%); the pan offset is
        // added on top so the media stays on the frame's centre axis.
        s.media.style.transform = 'translate(calc(-50% + ' + s.tx + 'px), calc(-50% + ' + s.ty + 'px))';
    }

    // Two orthogonal states, mirroring the app's inlineZoom.ts:
    //   .is-zooming     HOLDS a zoom (persistent — survives clicking away)
    //   .is-zoom-active OWNS the wheel (transient — outside click releases it)
    // Resetting the artwork is the explicit act (reset button / Escape /
    // another figure); merely looking away must not undo the reader's zoom.
    function deactivate() {
        if (!active) return;
        var m = active.media;
        m.style.transform = '';
        m.style.transformOrigin = '';
        m.style.width = '';
        m.style.height = '';
        m.style.maxWidth = '';
        m.style.maxHeight = '';
        if (m.tagName.toLowerCase() === 'svg' && active.savedW !== undefined) {
            if (active.savedW === null) m.removeAttribute('width');
            else m.setAttribute('width', active.savedW);
        }
        active.fig.classList.remove('is-zoom-active');
        active.fig.classList.remove('is-zooming');
        active = null;
    }

    // Release the wheel but keep the zoom. A figure entered and never zoomed
    // has nothing to preserve, so it goes back to resting.
    function releaseInteraction() {
        if (!active) return;
        if (active.scale <= MIN) { deactivate(); return; }
        active.fig.classList.remove('is-zoom-active');
    }

    function activate(fig) {
        var media = mediaOf(fig);
        if (!media) return null;
        if (active && active.fig === fig) {
            fig.classList.add('is-zoom-active');
            return active;
        }
        deactivate();
        active = { fig: fig, media: media, scale: MIN, tx: 0, ty: 0 };
        fig.classList.add('is-zooming');
        fig.classList.add('is-zoom-active');
        media.style.transformOrigin = 'center center';
        apply(active);
        return active;
    }

    function zoomAt(s, next, cx, cy) {
        var frame = s.fig.getBoundingClientRect();
        var dx = cx - (frame.left + frame.width / 2);
        var dy = cy - (frame.top + frame.height / 2);
        var r = next / s.scale;
        s.tx = dx - (dx - s.tx) * r;
        s.ty = dy - (dy - s.ty) * r;
        s.scale = next;
        apply(s);
    }

    document.addEventListener('wheel', function(e) {
        var fig = figFrom(e.target);
        if (!fig) return;
        // Only the INTERACTIVE figure owns the wheel. One that merely holds a
        // zoom (the reader clicked away) leaves its notches to the page.
        if (!fig.classList.contains('is-zoom-active')) return;
        var s = active && active.fig === fig ? active : null;

        // At rest EVERY notch — up or down, any delta — belongs to the page.
        // This is the whole point of click-to-enter: scrolling a document with
        // the pointer drifting over a diagram must never zoom it. An earlier
        // version let an upward notch activate the zoom, which trapped the
        // reader scrolling back up; the app-side module already fixed this and
        // this mirror has to agree with it.
        if (!s) return;
        // Horizontal-only notches are not a zoom gesture.
        if (e.deltaY === 0) return;

        var next = Math.min(MAX, Math.max(MIN, s.scale * (e.deltaY > 0 ? 1 / STEP : STEP)));

        // Zooming out bottoms out at fit scale and STOPS there; it does not
        // leave the active state (the ring / reset button would vanish mid
        // gesture and the next notch would scroll the page instead).
        if (next === s.scale) { e.preventDefault(); return; }
        e.preventDefault();
        zoomAt(s, next, e.clientX, e.clientY);
    }, { passive: false });

    document.addEventListener('pointerdown', function(e) {
        if (e.pointerType === 'touch' || e.button !== 0) return;
        var fig = figFrom(e.target);
        if (!fig || !fig.classList.contains('is-zoom-active')) return;
        if (!active || active.fig !== fig || active.scale <= MIN) return;
        e.preventDefault();
        panning = { s: active, x: e.clientX, y: e.clientY, bx: active.tx, by: active.ty, moved: false };
    });

    document.addEventListener('pointermove', function(e) {
        if (!panning) return;
        var dx = e.clientX - panning.x, dy = e.clientY - panning.y;
        if (!panning.moved && Math.abs(dx) < DRAG && Math.abs(dy) < DRAG) return;
        panning.moved = true;
        e.preventDefault();
        panning.s.tx = panning.bx + dx;
        panning.s.ty = panning.by + dy;
        apply(panning.s);
        panning.s.fig.classList.add('is-panning');
    });

    function endPan() {
        if (!panning) return;
        panning.s.fig.classList.remove('is-panning');
        panning = null;
    }
    document.addEventListener('pointerup', endPan);
    document.addEventListener('pointercancel', endPan);

    // Escape leaves the zoom, from wherever focus happens to be (the media is
    // not focusable, so the target is usually <body>).
    document.addEventListener('keydown', function(e) {
        if (e.key !== 'Escape' || !active) return;
        e.preventDefault();
        deactivate();
    });

    document.addEventListener('click', function(e) {
        if (!e.target || !e.target.closest) return;

        var reset = e.target.closest('.image-block-reset-btn');
        if (reset) {
            e.preventDefault();
            // At fit scale there is nothing left to hold, so reset is a full
            // exit — parking at MIN would leave a full-column empty frame.
            deactivate();
            return;
        }

        // The toolbar's own buttons keep their handlers.
        if (e.target.closest('.image-block-header')) return;

        // Click a figure to take the wheel; click anywhere else to give it back
        // WITHOUT discarding the zoom (see releaseInteraction).
        var fig = figFrom(e.target);
        if (fig) {
            activate(fig);
            return;
        }
        releaseInteraction();
    });
})();`
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

/** Escape a string for safe use inside a RegExp. */
function escapeRegExp(text: string): string {
    return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Strip a leading directory path off a path (returns the basename). */
function baseName(p: string): string {
    const parts = p.split(/[/\\]/)
    for (let i = parts.length - 1; i >= 0; i--) {
        if (parts[i]) return parts[i]
    }
    return p
}

/**
 * Read user-selected font stacks from the live <html> inline style. fontConfig
 * sets --font-ui / --font-mono on documentElement.style, so the chosen fonts are
 * not present in any stylesheet rule and would otherwise be lost in the export.
 * Returns a CSS `:root { … }` block (empty when neither variable is set).
 */
function readInlineFontVars(): string {
    const root = document.documentElement
    let css = ''
    for (const prop of ['--font-ui', '--font-mono']) {
        const value = (root.style?.getPropertyValue(prop) || '').trim()
        if (!value) continue
        css += `${prop}: ${value};\n`
    }
    return css ? `:root {\n${css}}` : ''
}

// ─── Main export function ─────────────────────────────────────────────────────

export async function exportMarkdownToHtml(options: ExportOptions): Promise<ExportResult> {
    const locale = options.locale || 'en'
    const isZh = locale === 'zh'
    const path = options.path || ''
    const fileName = options.fileName || baseName(path)

    const projectRoot = options.projectRoot ?? ''
    const homeDir = options.homeDir ?? ''
    const content = options.content || ''

    // 1. Render through the shared file-preview pipeline.
    const { html: renderedHtml, detectedPaths } = buildMarkdownPreviewDom(
        { content, path, projectRoot, homeDir },
        { isWideScreen: options.isWideScreen }
    )

    // 2. Mount content into a HIDDEN host on the live document. Mermaid's
    //    layout engine and path verification rely on attached-DOM behavior
    //    (all other app call sites render attached); a fully detached subtree
    //    can produce wrong diagram layout. The host is positioned off-screen
    //    and removed at the end, so nothing is visible to the user.
    const contentEl = document.createElement('div')
    contentEl.className = 'markdown-content'
    contentEl.innerHTML = renderedHtml

    const host = document.createElement('div')
    host.style.cssText = 'position:fixed;left:-9999px;top:0;width:900px;visibility:hidden;pointer-events:none;'
    host.appendChild(contentEl)
    document.body.appendChild(host)

    try {
        // 3. Disk-based annotation correction (mirrors MarkdownPreview.doRender).
        if (detectedPaths.length > 0) {
            await verifyFilePaths([...new Set(detectedPaths)], contentEl)
        }

        // 4. Render Mermaid diagrams (attached to the hidden host; lazy import idempotent).
        await renderMermaidInElement(contentEl, 'export')

        // 5. Inline images as data URIs.
        const inlineResult = await inlineImages(contentEl)

        // 5b. Proportional fill-width sizing for SVG media. Must run AFTER
        //     inlining: an SVG file's intrinsic size is only known once the
        //     (now self-contained) image has decoded, so the async pass waits
        //     for that before stamping the ratio. Inline <svg> resolves
        //     synchronously. The exported markup therefore carries the same
        //     `.svg-fit` marker + `--svg-ar` the in-app preview computes.
        await stampSvgFiguresAsync(contentEl)

        // 6. Replace failed Mermaid blocks with static errors.
        handleFailedMermaid(contentEl)

        // 7. Clean the DOM (scripts/iframes/.katex-mathml/diff markers).
        for (const script of Array.from(contentEl.querySelectorAll('script'))) script.remove()
        for (const iframe of Array.from(contentEl.querySelectorAll('iframe'))) iframe.remove()
        for (const mathml of Array.from(contentEl.querySelectorAll('.katex-mathml'))) mathml.remove()
        for (const marker of Array.from(contentEl.querySelectorAll('.diff-marker'))) marker.remove()
        // "Attach to chat" header buttons are inert in a static export (no chat
        // to attach to) — drop them so the exported doc stays clean. Image
        // "open file" buttons are equally inert; local image paths in
        // data-attach-src must not leak into the standalone file.
        for (const btn of Array.from(contentEl.querySelectorAll(
            '.code-block-attach-btn, .table-block-attach-btn, .image-block-attach-btn, .image-block-open-btn'
        ))) btn.remove()
        for (const img of Array.from(contentEl.querySelectorAll('img[data-attach-src]'))) {
            img.removeAttribute('data-attach-src')
        }

        // The pending-media placeholder is driven by the ABSENCE of
        // `data-media-settled` (see media-block.css), which the app stamps from a
        // live `load` event. An exported document has no such listener, so every
        // image would keep the placeholder forever and the export would look
        // empty. Images are already inlined as data URIs by this point, so mark
        // them resolved unconditionally.
        for (const img of Array.from(contentEl.querySelectorAll('img'))) {
            img.setAttribute('data-media-settled', '1')
        }

        // Restore external links to real anchors.
        //
        // In the app they are <span data-href> so a long-press on Android selects
        // the text (WebView's link gesture suppresses the system context menu —
        // see utils/externalLinkNode.ts). A standalone exported file has no such
        // listener, so a span would render as link-coloured, underlined,
        // NON-clickable text. The export is a static document, so the anchor is
        // both correct and the only thing that works.
        for (const span of Array.from(contentEl.querySelectorAll<HTMLElement>('.md-external-link'))) {
            const href = span.getAttribute('data-href')
            if (!href) continue
            const a = document.createElement('a')
            a.setAttribute('href', href)
            a.setAttribute('target', '_blank')
            a.setAttribute('rel', 'noopener noreferrer')
            if (span.hasAttribute('title')) a.setAttribute('title', span.getAttribute('title') as string)
            while (span.firstChild) a.appendChild(span.firstChild)
            span.replaceWith(a)
        }

        // 8. Serialize CSS + KaTeX fonts + base typography.
        const currentThemeId = document.documentElement.getAttribute('data-theme') || 'github-light'
        const currentThemeBase = document.documentElement.getAttribute('data-theme-base') || (isDarkTheme(currentThemeId) ? 'dark' : 'light')

        // Wrap content in a .markdown-body root (same structure as MarkdownPreview).
        const bodyEl = document.createElement('div')
        bodyEl.className = 'markdown-body'
        bodyEl.setAttribute('data-file-path', path)
        bodyEl.appendChild(contentEl)

        const css = serializeCss(bodyEl, currentThemeId)
        // User-selected fonts live as inline <html> CSS variables (set by fontConfig),
        // so they never appear in any stylesheet rule. Carry the current values into
        // the exported :root so prose/code use the same fonts as the in-app preview.
        const fontVarsCss = readInlineFontVars()
        const katexFontCss = await buildKatexFontCss(contentEl)

        // 9. Build TOC (share-page shell fragments; empty when no headings).
        const { viewOpenHtml, topbarHtml, shellOpenHtml, contentOpenHtml, contentCloseHtml, sidebarHtml, shellCloseHtml, viewCloseHtml, tocCss, tocJs } = buildTocStandalone(bodyEl, locale, fileName)

        // 10. Build interaction JS.
        const codeBlockJs = buildCodeBlockJs(locale)
        const lightboxJs = buildLightboxJs()
        const inlineZoomJs = buildInlineZoomJs()

        const title = escapeHtml(fileName.replace(/\.md$/i, ''))
        const bodyContent = bodyEl.outerHTML
        const { skipped: skippedImages, external: externalImages, issues } = inlineResult

        const html = `<!DOCTYPE html>
<html lang="${isZh ? 'zh-CN' : 'en'}" data-theme="${escapeHtml(currentThemeId)}" data-theme-base="${escapeHtml(currentThemeBase)}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>
/* ─── Base document typography (mirrors app base.css html/body) ─── */
html, body { margin: 0; padding: 0; font-size: 15px; line-height: 1.6; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, 'Helvetica Neue', sans-serif; background: var(--bg-primary); color: var(--text-primary); }

/* ─── Universal box-sizing reset (matches app base.css) ─── */
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; -webkit-tap-highlight-color: transparent; }

/* ─── Scrollbar styling (the shared definition, css/scrollbar.css) ───
   Inlined rather than linked: this document is opened from file:// or any host,
   where a root-absolute URL would 404. Its colours come from the theme
   variables serialized into this document (--scrollbar-thumb / --scrollbar-track),
   with fallbacks baked into the shared file. */
${scrollbarCss}

/* ─── Theme variables + content styles (rules hitting the exported DOM) ─── */
${css}

/* ─── User-selected font stacks carried from the app (--font-ui/--font-mono) ─── */
${fontVarsCss}

/* ─── KaTeX fonts used by this document (data-URI embedded) ─── */
${katexFontCss}

/* ─── Mermaid error ─── */
.mermaid-error { border: 1px dashed var(--border-color); padding: 12px; margin: 8px 0; border-radius: 6px; color: var(--text-muted); font-size: 13px; }

/* ─── Copied feedback text ─── */

/* ─── Share chrome (shared stylesheet — same file ShareView.vue imports) ─── */
${shareChromeCss}

/* ─── Export-only chrome overrides (see buildTocStandalone tocCss) ─── */
${tocCss}

/* ─── Lightbox overlay ─── */
.export-lightbox { position: fixed; inset: 0; z-index: 9999; background: rgba(0,0,0,0.85); display: flex; align-items: center; justify-content: center; cursor: zoom-out; overflow: hidden; touch-action: none; }
.export-lightbox-view { display: flex; align-items: center; justify-content: center; width: 100%; height: 100%; cursor: inherit; }
.export-lightbox-view img, .export-lightbox-view svg { max-width: 95vw; max-height: 95vh; object-fit: contain; user-select: none; -webkit-user-drag: none; }
/* SVG (mermaid diagrams) gets the theme's content background — same as the app
   lightbox (Lightbox.vue .lightbox-content svg { background: var(--bg-primary) }) —
   otherwise the transparent diagram sits directly on the dark overlay. */
.export-lightbox-view svg { background: var(--bg-primary); }
.export-lightbox-view img, .export-lightbox-view svg { transform-origin: center center; transition: transform 0.1s ease-out; }
.export-lightbox .lb-close-btn { position: absolute; top: 16px; right: 16px; width: 36px; height: 36px; border-radius: 50%; border: none; background: rgba(255,255,255,0.2); color: #fff; font-size: 20px; cursor: pointer; display: flex; align-items: center; justify-content: center; z-index: 10; }
.export-lightbox .lb-close-btn:hover { background: rgba(255,255,255,0.4); }
</style>
</head>
<body>
${viewOpenHtml}
${topbarHtml}
${shellOpenHtml}
${contentOpenHtml}
${bodyContent}
${contentCloseHtml}
${sidebarHtml}
${shellCloseHtml}
${viewCloseHtml}
<script>
${tocJs}
${codeBlockJs}
${lightboxJs}
${inlineZoomJs}
</script>
</body>
</html>`

        return { html, skippedImages, externalImages, issues }
    } finally {
        // The host was only a render scaffold — remove it so the export never
        // leaves hidden DOM behind.
        host.remove()
    }
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
