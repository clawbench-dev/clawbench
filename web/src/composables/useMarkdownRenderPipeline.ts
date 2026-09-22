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
import { isThumbExtension, buildThumbUrl, getThumbWidth, markInlineSvgs } from '@/utils/chatRenderUtils.ts'
import { annotateMediaBlocks } from '@/utils/mediaBlockFactory.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'
import { isShareMode, shareApiUrl } from '@/share/shareMode'

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
            // Remote, embedded, or already-served srcs pass through untouched.
            // `/api/local-file/…` matters here: re-rendering markup that already
            // went through this step (or markup authored with a served URL) must
            // not be wrapped a second time into `?path=/api/local-file/…`.
            if (/^(https?:|\/\/|data:|\/api\/local-file\/|\/api\/file\/)/i.test(src)) return match
            // An absolute src is a real filesystem path (the AI writing
            // `![](/tmp/chart.png)`), NOT a site-root URL: resolve it through
            // the absolute form so it renders instead of 404-ing at the site
            // root. A leading "/" on a RELATIVE reference cannot occur here —
            // markdown image srcs are resolved against the document's own
            // directory, and a root-relative reference has no local meaning.
            const isAbsoluteSrc = isAbsolutePath(src)
            let resolved = isAbsoluteSrc ? normalizeSlashes(src) : joinPath(currentDir, src)
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
            // Segment-encoded project-relative form. Only used for RELATIVE srcs:
            // an absolute src is served through the ?path= forms below, where the
            // path must stay absolute (a rootless form would resolve against the
            // project root instead of the file's real location).
            const rel = normalized.join('/')
            const shareMode = isShareMode()
            if (shareMode) {
                // Share mode: no thumbnail endpoint (it requires the project
                // cookie) — serve the full-size image through the token scope.
                // When the markdown file path is absolute (share SPA always
                // passes absolute paths), resolve media to their absolute
                // target and use the ?path= form; otherwise the token endpoint
                // treats {rel} as relative to the shared file's directory.
                if (isAbsolutePath(currentDir) || isAbsoluteSrc) {
                    // Rebuild the absolute target from the raw (still-decoded)
                    // src so `..` segments survive for the backend to resolve.
                    const absTarget = isAbsoluteSrc
                        ? normalizeSlashes(src)
                        : normalizeSlashes(`${currentDir.replace(/\/+$/, '')}/${src}`)
                    const shareSrc = shareApiUrl('local') + '?path=' + encodeURIComponent(absTarget) + `&t=${imageTimestamp}`
                    return match.replace(`src="${src}"`, `src="${shareSrc}"`)
                }
                const shareSrc = buildLocalMediaUrl(rel, imageTimestamp)
                return match.replace(`src="${src}"`, `src="${shareSrc}"`)
            }
            // Project-external absolute path: served through the ?path= form
            // (the relative form would resolve against the project root and
            // silently point at a different — usually nonexistent — file).
            const fullSrc = isAbsoluteSrc
                ? `/api/local-file/?path=${encodeURIComponent(src)}&t=${imageTimestamp}`
                : buildLocalMediaUrl(rel, imageTimestamp)
            // Raster formats the thumb endpoint can decode → use a lightweight JPEG
            // thumbnail for the inline src (kept stable so ETag revalidation refreshes
            // it when the source file changes) and keep the full image for the lightbox.
            // Other formats (svg/webp/gif/… ) keep serving the original full-size file.
            // The thumb endpoint takes a path in its own right (absolute paths are
            // stat'd directly), so the absolute form is passed encoded as-is.
            const thumbSrc = isThumbExtension(src)
                ? buildThumbUrl(isAbsoluteSrc ? encodeURIComponent(src) : rel, getThumbWidth(isPC))
                : null
            // data-attach-src carries the DECODED project-relative file path (resolved
            // against the markdown file's dir) so the rendered view can re-drag the
            // image out onto the chat column as a reference attachment. The decoded
            // form matches the FileEntry.path convention used by the file manager /
            // file header drags — the src URLs above keep the segment-encoded form.
            // The decoded value is HTML-escaped before interpolation: decodeURIComponent
            // can surface quote/angle characters (e.g. a literal %22 filename) that
            // would otherwise break out of the attribute on the rendered page.
            //
            // An EXTERNAL absolute image gets no attach attribute: the attach flow
            // speaks project-relative paths, so advertising one would offer a drag /
            // attach affordance that cannot resolve. External images stay
            // view-only (the figure still renders and opens in the lightbox).
            let attachAttr = ''
            if (!isAbsoluteSrc) {
                let attachPath = rel
                try { attachPath = decodeURIComponent(rel) } catch { /* rel is always encodeURIComponent output, keep as-is */ }
                attachAttr = ` data-attach-src="${escapeHtml(attachPath)}"`
            }
            const replacement = thumbSrc
                ? `src="${thumbSrc}" data-full-src="${fullSrc}"${attachAttr}`
                : `src="${fullSrc}"${attachAttr}`
            return match.replace(`src="${src}"`, replacement)
        })
        // Mark bare inline <svg> so the media-block factory lifts it into the
        // unified figure, exactly as the chat pipeline does. This step is
        // skipped by the chat pipeline's `skipEnhancements`, but the file
        // preview needs it: an AI-written or hand-authored `<svg>` block in a
        // markdown file should render as a proper figure (and get the
        // proportional fill-width sizing), not as a stray inline element.
        // Runs AFTER the image-path rewrite above and BEFORE the media-block
        // lift below (the lift is what actually wraps the marked svg).
        result = markInlineSvgs(result)
        // Lift every <img> (and bare inline svg) into a block-level figure with a
        // header bar. Images are inline-flow in the marked output (inside <p>);
        // blockifying them requires DOM surgery (paragraph promotion / split), so
        // this runs on a parsed tree — shared with the chat pipeline via the
        // media-block factory (annotateMediaBlocks).
        result = annotateMediaBlocks(result)
        return result
    }
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
