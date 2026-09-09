/**
 * Shared Markdown → rendered HTML pipeline for FILE PREVIEW context.
 *
 * Both MarkdownPreview.vue (the on-screen file preview) and the HTML exporter
 * (exportMarkdownHtml.ts) render a markdown *file* the same way, so a file
 * exported to standalone HTML looks pixel-identical to the in-app preview.
 *
 * This differs from the chat-render pipeline in `useMarkdownRenderer.ts`:
 * - file previews pass `sanitize: false` (the file is a trusted local artifact)
 * - file previews skip the chat enhancement block (skipEnhancements: true):
 *   no audio/video conversion, no worktree/commit/localhost annotations, no
 *   `chat-img` classes. Local images are rewritten by a caller-supplied
 *   `fixImagePaths` function that resolves relative paths against the markdown
 *   file's own directory (see createFixLocalImagePaths).
 * - file previews then run `annotateFilePaths` with `baseDir = dirName(path)`
 *   (component-level, in the caller) so relative links resolve to the file's dir.
 */

import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { annotateFilePaths } from '@/composables/useFilePathAnnotation.ts'
import { dirName, joinPath, splitPath, isAbsolutePath, normalizeSlashes } from '@/utils/path.ts'
import { escapeHtml } from '@/utils/html.ts'
import { isThumbExtension, buildThumbUrl, getThumbWidth } from '@/utils/chatRenderUtils.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'
import { gt } from '@/composables/useLocale'
import { isShareMode, shareApiUrl } from '@/share/shareMode'
import { ATTACH_BADGE_SVG } from '@/utils/attachSvg'
import { FILE_OPEN_ICON_SVG } from '@/composables/useFilePathAnnotation'

/**
 * Build the served URL for a project-relative (already normalized, unencoded)
 * media path. Normal mode: /api/local-file/<rel>. Share mode: the token-scoped
 * /api/share/{token}/local/<rel> endpoint (relative refs resolve against the
 * shared file's directory). Share mode skips the thumbnail optimization — the
 * thumb endpoint is project-cookie based and would 401 anonymously.
 */
export function buildLocalMediaUrl(rel: string, imageTimestamp: number): string {
    const url = isShareMode() ? shareApiUrl('local/' + rel) : `/api/local-file/${rel}`
    return url + `?t=${imageTimestamp}`
}

/** Source markdown file to render (the file being previewed / exported). */
export interface MarkdownSource {
    /** Raw markdown content. */
    content: string
    /** File path (project-relative). Used to resolve relative image paths. */
    path: string
    /** Project root (project-relative prefix for file-path annotations). */
    projectRoot?: string
    /** User home directory (for ~/ path expansion). */
    homeDir?: string
}

/** Options controlling local-image URL rewriting. */
export interface FixLocalImagePathsOptions {
    /** Directory of the markdown file; relative image srcs resolve against it. */
    baseDir: string
    /** Cache-buster appended to /api/local-file/ URLs (?t=…). */
    imageTimestamp: number
    /** Desktop (true) uses a wider inline thumbnail. */
    isPC: boolean
}

/**
 * Build the `fixImagePaths` callback used by MarkdownPreview / the exporter.
 *
 * Resolves relative `<img src>` paths against the markdown file's directory:
 * - http(s)://, protocol-relative //, leading-/ and data: URIs are untouched;
 * - other relative paths are resolved against `baseDir`, normalized, and served
 *   as `/api/local-file/<rel>?t=<ts>` (cache-busted);
 * - raster formats the thumb endpoint can decode (png/jpg/jpeg) get a lightweight
 *   JPEG thumbnail inline src (`/api/file/thumb?path=…&w=…`) plus the original
 *   URL kept in `data-full-src` for the lightbox;
 * - every <img> is lifted into a block-level `.image-block-wrapper` figure with a
 *   header bar (view / attach / open buttons) — uniform across mobile and PC.
 */
export function createFixLocalImagePaths(opts: FixLocalImagePathsOptions): (html: string) => string {
    const { baseDir, imageTimestamp, isPC } = opts
    return function fixLocalImagePaths(html: string): string {
        const currentDir = baseDir
        let result = html.replace(/<img\s+([^>]*src=[^>]*)>/gi, (match: string, attrs: string) => {
            const srcMatch = attrs.match(/src="([^"]*)"/)
            if (!srcMatch) return match
            const src = srcMatch[1]
            if (/^(https?:|\/\/|^\/|data:)/i.test(src)) return match
            let resolved = joinPath(currentDir, src)
            try {
                resolved = decodeURIComponent(resolved)
            } catch { /* malformed encoding, use as-is */ }
            const parts = splitPath(resolved)
            const normalized = []
            for (const part of parts) {
                if (part === '.' || part === '') continue
                if (part === '..') { normalized.pop(); continue }
                normalized.push(encodeURIComponent(part))
            }
            const rel = normalized.join('/')
            const shareMode = isShareMode()
            if (shareMode) {
                // Share mode: no thumbnail endpoint (it requires the project
                // cookie) — serve the full-size image through the token scope.
                // When the markdown file path is absolute (share SPA always
                // passes absolute paths), resolve media to their absolute
                // target and use the ?path= form; otherwise the token endpoint
                // treats {rel} as relative to the shared file's directory.
                if (isAbsolutePath(currentDir)) {
                    // Rebuild the absolute target from the raw (still-decoded)
                    // src so `..` segments survive for the backend to resolve.
                    const absTarget = normalizeSlashes(`${currentDir.replace(/\/+$/, '')}/${src}`)
                    const shareSrc = shareApiUrl('local') + '?path=' + encodeURIComponent(absTarget) + `&t=${imageTimestamp}`
                    return match.replace(`src="${src}"`, `src="${shareSrc}"`)
                }
                const shareSrc = buildLocalMediaUrl(rel, imageTimestamp)
                return match.replace(`src="${src}"`, `src="${shareSrc}"`)
            }
            const fullSrc = buildLocalMediaUrl(rel, imageTimestamp)
            // Raster formats the thumb endpoint can decode → use a lightweight JPEG
            // thumbnail for the inline src (kept stable so ETag revalidation refreshes
            // it when the source file changes) and keep the full image for the lightbox.
            // Other formats (svg/webp/gif/… ) keep serving the original full-size file.
            const thumbSrc = isThumbExtension(src) ? buildThumbUrl(rel, getThumbWidth(isPC)) : null
            // data-attach-src carries the DECODED project-relative file path (resolved
            // against the markdown file's dir) so the rendered view can re-drag the
            // image out onto the chat column as a reference attachment. The decoded
            // form matches the FileEntry.path convention used by the file manager /
            // file header drags — the src URLs above keep the segment-encoded form.
            // The decoded value is HTML-escaped before interpolation: decodeURIComponent
            // can surface quote/angle characters (e.g. a literal %22 filename) that
            // would otherwise break out of the attribute on the rendered page.
            let attachPath = rel
            try { attachPath = decodeURIComponent(rel) } catch { /* rel is always encodeURIComponent output, keep as-is */ }
            const attachAttr = ` data-attach-src="${escapeHtml(attachPath)}"`
            const replacement = thumbSrc
                ? `src="${thumbSrc}" data-full-src="${fullSrc}"${attachAttr}`
                : `src="${fullSrc}"${attachAttr}`
            return match.replace(`src="${src}"`, replacement)
        })
        // Lift every <img> into a block-level figure with a header bar. Images are
        // inline-flow in the marked output (inside <p>); blockifying them requires
        // DOM surgery (paragraph promotion / split), so this runs on a parsed tree.
        result = annotateImageBlocks(result)
        return result
    }
}

/** Feather "maximize" glyph for the image header view button. */
const IMAGE_VIEW_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/></svg>'

/**
 * Lift every <img> in an HTML fragment into a block-level `.image-block-wrapper`
 * figure with a header row of action buttons:
 *   - view (`.image-block-view-btn`, data-action="view") — opens the lightbox
 *   - attach (`.image-block-attach-btn`) — only for LOCAL images (data-attach-src)
 *   - open  (`.image-block-open-btn`)  — only for LOCAL images; opens the file
 * Header buttons are skipped entirely in share mode (no chat / lightbox to reach).
 *
 * Images are inline-flow in the marked output; lifting them to block requires:
 *   - img alone in a <p> → the <p> is replaced by the figure;
 *   - img mid-paragraph → the paragraph is split (before text <p> + figure +
 *     after text <p>);
 *   - img in other containers (<li>, <td>, blockquote…) → the figure is inserted
 *     in place of the img.
 * The wrapped img keeps the `lightbox-img` class and `data-attach-src` so PC drag
 * (mdImageDrag) and Lightbox image collection keep working.
 */
function annotateImageBlocks(html: string): string {
    if (!html) return html
    const doc = new DOMParser().parseFromString(html, 'text/html')

    // Iterate a snapshot: each pass moves the img, so a live NodeList would skip.
    const images = Array.from(doc.querySelectorAll('img'))
    for (const img of images) {
        if (img.closest('.image-block-wrapper')) continue // idempotent guard

        const isLocal = img.hasAttribute('data-attach-src')
        const shareMode = isShareMode()

        // Capture where the img currently sits BEFORE any move.
        const host = img.parentNode as HTMLElement | null
        const hostIsP = !!host && host.tagName === 'P'

        // 1. Build the wrapper shell at the img's old position.
        const wrapper = doc.createElement('div')
        wrapper.className = 'image-block-wrapper'
        if (host) host.insertBefore(wrapper, img) // wrapper sits before img

        // 2. Header row. The view (lightbox) button is always present — even on
        // the share SPA every image opens full-size in the mounted Lightbox.
        // Attach / open buttons are local-app only: they reference the open
        // file's chat attachment and in-manager file location, which a public
        // share viewer (read-only, token-scoped) cannot use.
        {
            const header = doc.createElement('div')
            header.className = 'image-block-header'
            const actions = doc.createElement('span')
            actions.className = 'image-block-header-actions'

            const viewBtn = doc.createElement('button')
            viewBtn.type = 'button'
            viewBtn.className = 'image-block-view-btn'
            viewBtn.dataset.action = 'view'
            const viewLabel = gt('imageBlock.view')
            viewBtn.title = viewLabel
            viewBtn.setAttribute('aria-label', viewLabel)
            viewBtn.innerHTML = IMAGE_VIEW_ICON_SVG
            actions.appendChild(viewBtn)

            if (isLocal && !shareMode) {
                actions.appendChild(makeImageHeaderButton(doc, 'image-block-attach-btn', 'attach', 'chat.attach.attachImageToChat', ATTACH_BADGE_SVG))
                actions.appendChild(makeImageHeaderButton(doc, 'image-block-open-btn', 'open', 'imageBlock.openFile', FILE_OPEN_ICON_SVG))
            }

            header.appendChild(actions)
            wrapper.appendChild(header)
        }

        // 3. Image wrap keeps lightbox/drag activation classes.
        const imgWrap = doc.createElement('span')
        imgWrap.className = 'lightbox-img-wrap'
        const imgClasses = img.getAttribute('class')
        img.setAttribute('class', imgClasses ? `${imgClasses} lightbox-img` : 'lightbox-img')
        imgWrap.appendChild(img) // detaches img from host
        wrapper.appendChild(imgWrap)

        // 4. Paragraph promotion: a <p> may not contain a block div. When the
        // img was the paragraph's ONLY content, replace the <p> with the figure.
        // Mid-paragraph images (text both sides) are lifted out — trailing text
        // becomes its own <p> after the figure.
        if (hostIsP && host) {
            // Collect everything that followed the wrapper in host (originally
            // text after the img).
            const trailing: Node[] = []
            while (host.contains(wrapper) && wrapper.nextSibling) {
                const sib = wrapper.nextSibling
                host.removeChild(sib)
                trailing.push(sib)
            }
            const leadingText = host.textContent?.trim()
            if (!leadingText && trailing.length === 0) {
                // Empty <p> (img was its only child) — drop it.
                host.replaceWith(wrapper)
            } else {
                // Move the figure after the (still leading-text-bearing) <p>.
                host.after(wrapper)
                if (trailing.some(n => n.textContent?.trim())) {
                    const tailP = doc.createElement('p')
                    for (const t of trailing) tailP.appendChild(t)
                    wrapper.after(tailP)
                } else {
                    for (const t of trailing) host.appendChild(t)
                }
            }
        }
    }

    return doc.body.innerHTML
}

/** Build a small header action button (paperclip / open-file). */
function makeImageHeaderButton(
    doc: Document,
    cls: string,
    action: string,
    i18nKey: string,
    svg: string
): HTMLButtonElement {
    const btn = doc.createElement('button')
    btn.type = 'button'
    btn.className = cls
    btn.dataset.action = action
    const label = gt(i18nKey)
    btn.title = label
    btn.setAttribute('aria-label', label)
    btn.innerHTML = svg
    return btn
}

/** Result of rendering markdown source. */
export interface BuildMarkdownPreviewDomResult {
    /** Rendered + file-path-annotated HTML (for .markdown-content innerHTML). */
    html: string
    /** Detected file paths — pass to verifyFilePaths for disk-based correction. */
    detectedPaths: string[]
}

/**
 * Render a markdown *file* exactly like MarkdownPreview.vue does.
 *
 * Pipeline (mirrors MarkdownPreview.doRender):
 *   renderMarkdownHtml(content, { sanitize: false, skipEnhancements: true,
 *                                 fixImagePaths })   → annotated string
 *   annotateFilePaths(html, { projectRoot, baseDir: dirName(path), homeDir })
 *
 * @param source The markdown file to render.
 * @param opts   Runtime knobs (thumbnail width / cache-buster). Omitted in tests.
 */
export function buildMarkdownPreviewDom(
    source: MarkdownSource,
    opts: { isPC?: boolean; imageTimestamp?: number } = {}
): BuildMarkdownPreviewDomResult {
    const { content, path, projectRoot = '', homeDir = '' } = source
    const currentDir = path ? dirName(path) : ''
    const { isPC } = usePlatformDetect()

    const effectiveIsPC = opts.isPC ?? isPC.value
    const imageTimestamp = opts.imageTimestamp ?? Date.now()

    const html = renderMarkdownHtml(content, {
        sanitize: false,
        skipEnhancements: true,
        fixImagePaths: createFixLocalImagePaths({
            baseDir: currentDir,
            imageTimestamp,
            isPC: effectiveIsPC,
        }),
    })

    // Share mode: render the document read-only. File-path annotation is
    // skipped entirely — the shared view has no file navigation, and annotated
    // links would attempt auth-protected opens.
    if (isShareMode()) {
        return { html, detectedPaths: [] }
    }

    const { html: annotatedHtml, detectedPaths } = annotateFilePaths(html, {
        projectRoot,
        baseDir: currentDir,
        homeDir,
    })

    return { html: annotatedHtml, detectedPaths }
}
