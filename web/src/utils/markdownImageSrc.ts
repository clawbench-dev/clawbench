/**
 * Resolve a markdown image `src` to a URL the app can actually load.
 *
 * The rendered preview rewrites image paths in `createFixLocalImagePaths`
 * (useMarkdownRenderPipeline.ts). The visual editor needs the same answer for
 * its own <img> nodes, but it cannot import that module: the pipeline drags the
 * whole renderer (marked, DOMPurify, the annotators) into the editor's lazy
 * chunk. This is the subset that matters for an editor — full-size images only,
 * no thumbnails and no lightbox metadata.
 *
 * Rules (kept in step with the preview):
 * - remote / protocol-relative / data: / already-served URLs pass through;
 * - `src` is decoded once (marked percent-encodes CJK filenames, and encoding
 *   it again would look for a file literally named `%E5%9B%BE…`);
 * - a relative path resolves against the markdown file's own directory, with
 *   `.` / `..` segments collapsed, then each segment is URL-encoded;
 * - an absolute filesystem path goes through the `?target=` form, because the
 *   relative form would resolve against the project root instead.
 */
import { dirName, joinPath, splitPath, normalizeSlashes, isAbsolutePath } from '@/utils/path.ts'

export function resolveMarkdownImageSrc(src: string | null | undefined, filePath: string | null | undefined): string {
    const raw = (src || '').trim()
    if (!raw) return raw
    // Already usable as-is. `/api/fs/` matters: markup authored with a served
    // URL must not be rewritten a second time.
    if (/^(https?:|\/\/|data:|\/api\/fs\/)/i.test(raw)) return raw

    let decoded = raw
    try {
        decoded = decodeURIComponent(raw)
    } catch {
        // Malformed encoding — keep what the author wrote.
    }

    const stamp = Date.now()
    if (isAbsolutePath(decoded)) {
        return `/api/fs/raw/?target=${encodeURIComponent(decoded)}&t=${stamp}`
    }

    const baseDir = dirName(filePath || '')
    const resolved = normalizeSlashes(joinPath(baseDir, decoded))
    const parts = splitPath(resolved)
    const normalized: string[] = []
    for (const part of parts) {
        if (part === '.' || part === '') continue
        if (part === '..') {
            normalized.pop()
            continue
        }
        normalized.push(encodeURIComponent(part))
    }
    return `/api/fs/raw/${normalized.join('/')}?t=${stamp}`
}
