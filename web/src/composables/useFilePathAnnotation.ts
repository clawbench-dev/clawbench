import { escapeHtml } from '@/utils/html.ts'
import { splitPath } from '@/utils/path.ts'
import { store } from '@/stores/app.ts'
import { gt } from '@/composables/useLocale'
import { clearCommitHashCache } from '@/composables/useCommitHashAnnotation.ts'
// NOTE: do NOT import clearWorktreeCache from useWorktreeAnnotation here —
// that creates a circular dependency (useFilePathAnnotation ↔ useWorktreeAnnotation).
// Instead, we use a lazy indirection registered at init time.
let _clearWorktreeCache: (() => void) | null = null

export function registerWorktreeCacheClearter(fn: () => void) {
  _clearWorktreeCache = fn
}

// ── Dual-candidate resolution types ─────────────────────────────────────────────

/**
 * Result of dual-candidate file path resolution.
 * - primary: preferred path (baseDir-relative if applicable, else projectRoot-relative)
 * - fallback: alternative path (projectRoot-relative). Same as primary when no baseDir
 *   or when both resolutions produce the same result.
 */
export interface ResolveResult {
    primary: string
    fallback: string
}

// ── URI decoding ────────────────────────────────────────────────────────────────

/**
 * Try to decode a percent-encoded URI component.
 * Browsers/DOMPurify may encode non-ASCII chars (e.g. 中文 → %E4%B8%AD%E6%96%87)
 * in href attributes when HTML is inserted via innerHTML/v-html.
 */
function tryDecodeUri(uri: string): string {
    try {
        if (!uri.includes('%')) return uri
        return decodeURIComponent(uri)
    } catch {
        return uri
    }
}

// ── Path resolution helpers ────────────────────────────────────────────────────

/**
 * Resolve a relative path against a base directory.
 * Returns project-relative path if within project, absolute path if outside,
 * or null if resolution fails.
 */
function resolveRelativePathAgainstBase(path: string, baseDir: string, projectRoot: string): string | null {
    const parts = baseDir.split('/').filter(Boolean)
    const segments = path.split('/')
    for (const seg of segments) {
        if (seg === '..') {
            if (parts.length > 0) parts.pop()
            else return null
        } else if (seg !== '.' && seg !== '') {
            parts.push(seg)
        }
    }
    const absolutePath = '/' + parts.join('/')
    if (projectRoot && absolutePath.startsWith(projectRoot + '/')) {
        return absolutePath.slice(projectRoot.length + 1)
    }
    if (projectRoot && absolutePath === projectRoot) return null
    return absolutePath
}

/**
 * Resolve a relative path against projectRoot only.
 * Returns ResolveResult where primary === fallback (single candidate).
 */
function resolveAgainstProjectRoot(path: string, projectRoot: string): ResolveResult | null {
    if (!projectRoot) return null
    const parts = projectRoot.split('/').filter(Boolean)
    const segments = path.split('/')
    for (const seg of segments) {
        if (seg === '..') {
            if (parts.length > 0) parts.pop()
            else return null
        } else if (seg !== '.' && seg !== '') {
            parts.push(seg)
        }
    }
    const absolutePath = '/' + parts.join('/')
    if (absolutePath.startsWith(projectRoot + '/')) {
        const rel = absolutePath.slice(projectRoot.length + 1)
        return { primary: rel, fallback: rel }
    }
    if (absolutePath === projectRoot) return null
    return { primary: absolutePath, fallback: absolutePath }
}

// ── Core dual-candidate resolution ─────────────────────────────────────────────

/**
 * Rejection checks shared by resolveFilePathDual and looksLikeFilePath.
 * Returns true if the path should be rejected (glob, URL, env var, bare identifier).
 */
function shouldRejectPath(path: string): boolean {
    if (/[*?\\[\]<>]/.test(path) || path.includes('**')) return true
    if (/^https?:\/\//i.test(path)) return true
    if (/\$/.test(path)) return true
    return false
}

/**
 * Resolve a file path with dual-candidate support.
 *
 * Returns a ResolveResult with:
 * - primary: the preferred resolution (baseDir-relative if available and project-internal)
 * - fallback: the projectRoot-relative resolution (for async verification fallback)
 *
 * When there is no baseDir or baseDir === projectRoot, primary === fallback.
 * When baseDir resolves to a project-external absolute path, primary === fallback (projectRoot wins).
 * When baseDir resolves to a different project-internal path, primary = baseDir result, fallback = projectRoot result.
 */
export function resolveFilePathDual(path: string, projectRoot: string, homeDir?: string, baseDir?: string): ResolveResult | null {
    // Reject glob patterns, URLs, env vars
    if (shouldRejectPath(path)) return null
    // Reject bare identifiers without / or file extension
    if (!/\//.test(path) && !/\.[a-zA-Z][a-zA-Z0-9]{0,3}$/.test(path.replace(/:\d+(-\d+)?$/, ''))) return null

    // ── Tilde expansion ──
    if (path.startsWith('~/') || path === '~') {
        if (!homeDir) return null
        const expanded = homeDir + path.slice(1)
        if (!projectRoot) return { primary: expanded, fallback: expanded }
        if (expanded.startsWith(projectRoot + '/')) {
            const rel = expanded.slice(projectRoot.length + 1)
            return { primary: rel, fallback: rel }
        }
        if (expanded === projectRoot) return null
        return { primary: expanded, fallback: expanded }
    }

    // ── Absolute path ──
    if (path.startsWith('/')) {
        if (!projectRoot) return { primary: path, fallback: path }
        if (path.startsWith(projectRoot + '/')) {
            const rel = path.slice(projectRoot.length + 1)
            return { primary: rel, fallback: rel }
        }
        if (path === projectRoot) return null
        return { primary: path, fallback: path }
    }

    // ── Relative path without any root ──
    if (!projectRoot && !baseDir) {
        const clean = path.replace(/^\.\//, '')
        if (clean.startsWith('../')) return null
        return { primary: clean, fallback: clean }
    }

    // ── Relative path: compute projectRoot candidate (always the fallback) ──
    const projectResult = resolveAgainstProjectRoot(path, projectRoot)

    // No separate baseDir → single candidate
    if (!baseDir || baseDir === projectRoot) {
        return projectResult
    }

    // Normalize baseDir: if project-relative, convert to absolute
    const absBaseDir = baseDir.startsWith('/') ? baseDir : (projectRoot + '/' + baseDir)

    // Compute baseDir candidate
    const baseDirResult = resolveRelativePathAgainstBase(path, absBaseDir, projectRoot)

    // baseDir failed or resolved to project-external absolute → projectRoot wins
    if (!baseDirResult || baseDirResult.startsWith('/')) {
        return projectResult
    }

    // baseDir resolved to project-internal path → use as primary, projectRoot as fallback
    // If projectResult is project-external (e.g. ../README.md walks above projectRoot),
    // try a stripped fallback: resolve the path without leading ../ segments against projectRoot.
    // This handles the common pattern where ../README.md from a subdirectory is intended
    // to mean the project root's README.md.
    if (!projectResult) return { primary: baseDirResult, fallback: baseDirResult }

    // projectResult is project-external → try stripped fallback
    if (projectResult.primary.startsWith('/')) {
        const stripped = path.replace(/^(?:\.\.\/)+/, '')
        if (stripped !== path) {
            const strippedResult = resolveAgainstProjectRoot(stripped, projectRoot)
            if (strippedResult && !strippedResult.primary.startsWith('/')) {
                if (baseDirResult === strippedResult.primary) {
                    return strippedResult
                }
                return {
                    primary: baseDirResult,
                    fallback: strippedResult.primary,
                }
            }
        }
        // No valid stripped fallback → single candidate
        return { primary: baseDirResult, fallback: baseDirResult }
    }

    // Same path — no fallback needed
    if (baseDirResult === projectResult.primary) {
        return projectResult
    }

    return {
        primary: baseDirResult,
        fallback: projectResult.primary,
    }
}

/**
 * Convenience wrapper: resolve a file path and return only the primary candidate.
 * Used by renderToolDetail.ts (8 call sites) and other callers that don't need fallback.
 */
export function resolveFilePath(path: string, projectRoot: string, homeDir?: string, baseDir?: string): string | null {
    const result = resolveFilePathDual(path, projectRoot, homeDir, baseDir)
    return result?.primary ?? null
}

// ── SVG icon & button HTML ─────────────────────────────────────────────────────

export const FILE_OPEN_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>'

/**
 * Generate HTML for the small open-file button.
 * Optionally includes line range attributes and a fallback path for dual-candidate verification.
 */
export function fileOpenButtonHtml(resolvedPath: string, lineStart?: number, lineEnd?: number, fallbackPath?: string): string {
    const isExternal = resolvedPath.startsWith('/')
    const lineAttrs = lineStart ? ` data-line-start="${lineStart}"${lineEnd ? ` data-line-end="${lineEnd}"` : ''}` : ''
    const externalClass = isExternal ? ' external' : ''
    const fallbackAttr = fallbackPath && fallbackPath !== resolvedPath ? ` data-fallback-path="${escapeHtml(fallbackPath)}"` : ''
    return `<button class="chat-file-open-btn${externalClass}" data-file-path="${escapeHtml(resolvedPath)}"${fallbackAttr}${lineAttrs} title="${escapeHtml(gt('chat.attach.openFile'))}">${FILE_OPEN_ICON_SVG}</button>`
}

// ── Line info extraction ────────────────────────────────────────────────────────

/**
 * Extract the bare file path and optional line range from a regex match.
 * E.g. "src/main.go:70-81" → { path: "src/main.go", lineStart: 70, lineEnd: 81 }
 */
function extractLineInfo(matchStr: string, match: RegExpExecArray): { path: string; lineStart?: number; lineEnd?: number } {
    const lineStartStr = match[1]
    const lineEndStr = match[2]
    if (!lineStartStr) return { path: matchStr }
    const lineSuffix = matchStr.match(/:\d+(-\d+)?$/)
    const path = lineSuffix ? matchStr.slice(0, matchStr.length - lineSuffix[0].length) : matchStr
    return {
        path,
        lineStart: parseInt(lineStartStr, 10),
        lineEnd: lineEndStr ? parseInt(lineEndStr, 10) : undefined,
    }
}

/**
 * Extract bare path and optional line info from a plain text string.
 * Used by Step 2 for <code> tag content.
 */
function extractLineInfoFromText(text: string): { path: string; lineStart?: number; lineEnd?: number } {
    const m = text.match(/:\d+(-\d+)?$/)
    if (!m) return { path: text }
    const colonIdx = text.lastIndexOf(':')
    const path = text.slice(0, colonIdx)
    const linePart = text.slice(colonIdx + 1)
    const [startStr, endStr] = linePart.split('-')
    return {
        path,
        lineStart: parseInt(startStr, 10),
        lineEnd: endStr ? parseInt(endStr, 10) : undefined,
    }
}

// ── Path detection regex & helper ───────────────────────────────────────────────

const FILE_PATH_RE = /(?:~?\/[^\s<>"')\]]+(?:\/[^\s<>"')\]]+)+\.[a-zA-Z][a-zA-Z0-9]*|\.\.?\/[^\s<>"')\]]+(?:\/[^\s<>"')\]]+)*\.[a-zA-Z][a-zA-Z0-9]*|[a-zA-Z0-9_-]+(?:\/[a-zA-Z0-9_.-]+)+\.[a-zA-Z][a-zA-Z0-9]*)(?::(\d+)(?:-(\d+))?)?/g

/**
 * Check if a string looks like a file path that should be annotated.
 * Rejects bare identifiers like `useAutoSpeech`, `onUnmounted`, `ref`.
 */
export function looksLikeFilePath(text: string): boolean {
    if (shouldRejectPath(text)) return false
    const bare = text.replace(/:\d+(-\d+)?$/, '')
    return /\/|\.[a-zA-Z][a-zA-Z0-9]{0,3}$/.test(bare)
}

// ── HTML annotation ────────────────────────────────────────────────────────────

export interface AnnotateFilePathsOptions {
    projectRoot: string
    /** Base directory for resolving relative paths (e.g. the md file's dir) */
    baseDir?: string
    /** User's home directory (from backend), used to expand ~/ paths */
    homeDir?: string
}

/**
 * Helper: push primary and fallback paths to detectedPaths list.
 * Always pushes primary; pushes fallback only if it differs from primary.
 */
function pushDetectedPaths(detectedPaths: string[], result: ResolveResult): void {
    detectedPaths.push(result.primary)
    if (result.fallback !== result.primary) {
        detectedPaths.push(result.fallback)
    }
}

/**
 * Detect file paths in rendered HTML and insert open-file buttons after them.
 *
 * Uses DOMParser + TreeWalker for robust HTML traversal. Dual-candidate resolution
 * stores both primary (baseDir-relative) and fallback (projectRoot-relative) paths,
 * enabling verifyFilePaths to swap to the fallback when the primary doesn't exist.
 *
 * Processing order:
 *   1. <a href="..."> tags with local-file hrefs → append open button
 *   2. <code> tags whose text content looks like a path → add class + button
 *   3. Text nodes (outside a/code) → regex match paths → insert span + button
 */
export function annotateFilePaths(
    html: string,
    options: AnnotateFilePathsOptions
): { html: string; detectedPaths: string[] } {
    if (!html) return { html: '', detectedPaths: [] }

    const { projectRoot, baseDir, homeDir } = options
    const detectedPaths: string[] = []

    const doc = new DOMParser().parseFromString(html, 'text/html')

    // ── Step 1: <a> tags with local-file hrefs ──
    for (const a of doc.querySelectorAll('a[href]')) {
        const rawHref = a.getAttribute('href')!
        const href = tryDecodeUri(rawHref)
        if (/^(https?:|\/\/|mailto:|tel:|#)/i.test(href)) continue
        const resolved = baseDir
            ? resolveRelativePath(href, baseDir)
            : resolveFilePath(href, projectRoot, homeDir)
        if (!resolved) continue
        detectedPaths.push(resolved)
        a.insertAdjacentHTML('afterend', fileOpenButtonHtml(resolved))
    }

    // ── Step 2: <code> tags whose content is purely a file path ──
    for (const code of doc.querySelectorAll('code')) {
        if (code.classList.contains('chat-worktree-path')) continue
        const stripped = (code.textContent || '').trim()
        if (!looksLikeFilePath(stripped)) continue
        const { path: barePath, lineStart, lineEnd } = extractLineInfoFromText(stripped)
        const result = resolveFilePathDual(barePath, projectRoot, homeDir, baseDir)
        if (!result || result.primary.includes(' ') || result.primary.includes('"')) continue
        pushDetectedPaths(detectedPaths, result)
        code.classList.add('chat-file-path')
        code.setAttribute('data-file-path', result.primary)
        if (result.fallback !== result.primary) code.setAttribute('data-fallback-path', result.fallback)
        if (result.primary.startsWith('/')) code.setAttribute('data-external', 'true')
        if (lineStart) code.setAttribute('data-line-start', String(lineStart))
        if (lineEnd) code.setAttribute('data-line-end', String(lineEnd))
        code.insertAdjacentHTML('afterend', fileOpenButtonHtml(result.primary, lineStart, lineEnd, result.fallback !== result.primary ? result.fallback : undefined))
    }

    // ── Step 3: Text nodes → regex match paths ──
    const textNodes: Text[] = []
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
        acceptNode(node: Text) {
            const parent = node.parentElement
            if (!parent) return NodeFilter.FILTER_REJECT
            if (parent.tagName === 'A' || parent.closest('a')) return NodeFilter.FILTER_REJECT
            if (parent.classList.contains('chat-file-path')) return NodeFilter.FILTER_REJECT
            if (parent.classList.contains('chat-worktree-path') || parent.closest('.chat-worktree-path')) return NodeFilter.FILTER_REJECT
            return NodeFilter.FILTER_ACCEPT
        }
    })
    while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

    for (let i = textNodes.length - 1; i >= 0; i--) {
        const textNode = textNodes[i]
        const text = textNode.textContent || ''
        FILE_PATH_RE.lastIndex = 0
        if (!FILE_PATH_RE.test(text)) continue

        FILE_PATH_RE.lastIndex = 0
        const parts: Array<{ text: string; result: ResolveResult | null; lineStart?: number; lineEnd?: number }> = []
        let lastIndex = 0
        let match: RegExpExecArray | null
        while ((match = FILE_PATH_RE.exec(text)) !== null) {
            const pathStr = match[0]
            const { path: barePath, lineStart, lineEnd } = extractLineInfo(pathStr, match)
            let result = resolveFilePathDual(barePath, projectRoot, homeDir, baseDir)
            // Directory-prefix suppression: if match is followed by /segment, skip it
            if (result) {
                const afterIdx = match.index + pathStr.length
                if (afterIdx < text.length && text[afterIdx] === '/') {
                    const rest = text.slice(afterIdx + 1)
                    if (rest.length > 0 && /^[a-zA-Z0-9_.-]/.test(rest)) {
                        result = null
                    }
                }
            }
            if (match.index > lastIndex) {
                parts.push({ text: text.slice(lastIndex, match.index), result: null })
            }
            parts.push({ text: pathStr, result, lineStart: result ? lineStart : undefined, lineEnd: result ? lineEnd : undefined })
            lastIndex = match.index + pathStr.length
        }
        if (lastIndex < text.length) {
            parts.push({ text: text.slice(lastIndex), result: null })
        }

        // Build replacement nodes
        const parent = textNode.parentNode!
        const frag = doc.createDocumentFragment()
        let hasAnnotation = false
        for (const part of parts) {
            if (part.result) {
                hasAnnotation = true
                pushDetectedPaths(detectedPaths, part.result)
                const span = doc.createElement('span')
                span.className = 'chat-file-path'
                span.setAttribute('data-file-path', part.result.primary)
                if (part.result.fallback !== part.result.primary) span.setAttribute('data-fallback-path', part.result.fallback)
                if (part.result.primary.startsWith('/')) span.setAttribute('data-external', 'true')
                if (part.lineStart) span.setAttribute('data-line-start', String(part.lineStart))
                if (part.lineEnd) span.setAttribute('data-line-end', String(part.lineEnd))
                span.textContent = part.text
                frag.appendChild(span)
                const btnContainer = doc.createElement('span')
                btnContainer.innerHTML = fileOpenButtonHtml(part.result.primary, part.lineStart, part.lineEnd, part.result.fallback !== part.result.primary ? part.result.fallback : undefined)
                while (btnContainer.firstChild) frag.appendChild(btnContainer.firstChild)
            } else {
                frag.appendChild(doc.createTextNode(part.text))
            }
        }

        if (hasAnnotation) {
            parent.replaceChild(frag, textNode)
        }
    }

    return { html: doc.body.innerHTML, detectedPaths }
}

// ── Async verification with fallback swap ──────────────────────────────────────

const MAX_CACHE_SIZE = 500
const verifiedCache = new Map<string, boolean>()

function cacheSet(key: string, value: boolean): void {
    if (verifiedCache.size >= MAX_CACHE_SIZE && !verifiedCache.has(key)) {
        const oldest = verifiedCache.keys().next().value
        if (oldest !== undefined) verifiedCache.delete(oldest)
    }
    verifiedCache.set(key, value)
}

let pendingPaths: string[] = []
let batchInFlight: Promise<void> | null = null

async function drainBatch(): Promise<void> {
    const paths = [...new Set(pendingPaths)]
    pendingPaths = []

    try {
        const resp = await fetch('/api/file/batch-exists', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths }),
        })
        const data = await resp.json() as { results: Record<string, string> }
        for (const [path, type] of Object.entries(data.results)) {
            const exists = type === 'file' || type === 'dir'
            cacheSet(path, exists)
        }
    } catch {
        for (const p of paths) {
            cacheSet(p, true)
        }
    }
}

/**
 * Verify which file paths actually exist on the server.
 * For non-existent paths with a data-fallback-path, swap to the fallback
 * (if it exists) instead of removing the annotation entirely.
 */
export async function verifyFilePaths(paths: string[], containerEl: HTMLElement): Promise<void> {
    const unique = [...new Set(paths)]
    if (unique.length === 0) return

    const uncached: string[] = []
    const results = new Map<string, boolean>()

    for (const p of unique) {
        if (verifiedCache.has(p)) {
            results.set(p, verifiedCache.get(p)!)
        } else {
            uncached.push(p)
        }
    }

    if (uncached.length > 0) {
        pendingPaths.push(...uncached)

        if (!batchInFlight) {
            batchInFlight = (async () => {
                while (pendingPaths.length > 0) {
                    await drainBatch()
                }
                batchInFlight = null
            })()
        }

        await batchInFlight

        for (const p of uncached) {
            if (verifiedCache.has(p)) {
                results.set(p, verifiedCache.get(p)!)
            }
        }
    }

    // Process non-existent paths: try fallback swap before removing
    for (const [path, exists] of results) {
        if (exists) continue

        // Check if any element with this primary path has a fallback that exists
        const els = containerEl.querySelectorAll(`[data-file-path="${CSS.escape(path)}"]`)
        let swapped = false
        for (const el of els) {
            const fallback = el.getAttribute('data-fallback-path')
            if (fallback && results.get(fallback)) {
                // Swap data-file-path to fallback
                el.setAttribute('data-file-path', fallback)
                el.removeAttribute('data-fallback-path')
                // Update external status
                const isNowExternal = fallback.startsWith('/')
                if (isNowExternal) {
                    el.setAttribute('data-external', 'true')
                    el.classList.add('external')
                } else {
                    el.removeAttribute('data-external')
                    el.classList.remove('external')
                }
                swapped = true
            }
        }
        if (swapped) continue

        // No fallback available — remove annotation
        containerEl.querySelectorAll(`.chat-file-open-btn[data-file-path="${CSS.escape(path)}"]`).forEach(btn => {
            btn.remove()
        })
        containerEl.querySelectorAll(`.chat-file-path[data-file-path="${CSS.escape(path)}"]`).forEach(span => {
            span.replaceWith(...span.childNodes)
        })
        containerEl.querySelectorAll(`.code-file-path[data-file-path="${CSS.escape(path)}"]`).forEach(span => {
            span.replaceWith(...span.childNodes)
        })
    }
}

export function clearVerifiedCache(): void {
    verifiedCache.clear()
    pendingPaths = []
    batchInFlight = null
    clearCommitHashCache()
    _clearWorktreeCache?.()
}

// ── Composable ─────────────────────────────────────────────────────────────────

export function useFilePathAnnotation() {
    return {
        resolveFilePath,
        resolveFilePathDual,
        fileOpenButtonHtml,
        annotateFilePaths,
        verifyFilePaths,
        resolveRelativePath,
        tryResolveCodeString,
        stripCodeString,
        openFilePath,
        dispatchScrollToLine,
        clearVerifiedCache,
    }
}

// ── Shared helpers (used by CodePreview.vue) ───────────────────────────────────

/**
 * Resolve a relative href against a base directory.
 * Returns the resolved project-relative path.
 */
export function resolveRelativePath(href: string, baseDir: string): string {
    if (!baseDir) return href
    const parts = splitPath(baseDir + '/' + href)
    const normalized: string[] = []
    for (const part of parts) {
        if (part === '.' || part === '') continue
        if (part === '..') { normalized.pop(); continue }
        normalized.push(part)
    }
    return normalized.join('/')
}

/**
 * Strip surrounding quotes from a code string.
 * E.g. '"src/main.go"' → 'src/main.go'
 */
export function stripCodeString(rawText: string): string {
    return rawText.replace(/^['"`](.*)['"`]$/, '$1').trim()
}

/**
 * Try to resolve a code string (e.g. from a .hljs-string span) as a file path.
 * Returns ResolveResult with dual candidates for verification fallback.
 */
export function tryResolveCodeString(
    rawText: string,
    projectRoot: string,
    homeDir?: string,
    baseDir?: string,
): ResolveResult | null {
    const stripped = stripCodeString(rawText)
    if (!stripped || stripped.length < 3) return null
    if (!looksLikeFilePath(stripped)) return null
    return resolveFilePathDual(stripped, projectRoot, homeDir, baseDir)
}

// ── File opening ───────────────────────────────────────────────────────────────

/**
 * Open a file or directory path.
 * If the path is a directory, navigates to it and opens the file manager.
 * If it's a file, selects it in the store.
 * If the file doesn't exist, shows a toast and does not navigate.
 */
export async function openFilePath(resolvedPath: string, lineStart?: number, lineEnd?: number): Promise<boolean> {
    const isExternal = resolvedPath.startsWith('/')

    if (!isExternal) {
        try {
            const resp = await fetch(`/api/dir?path=${encodeURIComponent(resolvedPath)}`)
            if (resp.ok) {
                await store.navigateToDir(resolvedPath)
                window.dispatchEvent(new CustomEvent('close-file-overlay'))
                window.dispatchEvent(new CustomEvent('open-file-manager'))
                return true
            }
        } catch {
            // Ignore, fall through to open as file
        }
    }

    try {
        const resp = await fetch(`/api/file/batch-exists`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [resolvedPath] }),
        })
        if (resp.ok) {
            const data = await resp.json() as { results: Record<string, string> }
            const type = data.results?.[resolvedPath]
            if (type !== 'file' && type !== 'dir') {
                const { useToast } = await import('@/composables/useToast')
                const { gt } = await import('@/composables/useLocale')
                useToast().show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
                return false
            }
            if (isExternal && type === 'dir') {
                const { useToast } = await import('@/composables/useToast')
                const { gt } = await import('@/composables/useLocale')
                useToast().show(gt('file.toast.externalDirNotSupported'), { type: 'info', icon: '📁', duration: 2000 })
                return false
            }
            if (type === 'dir') {
                // Path is a directory — navigate into it instead of opening as file
                await store.navigateToDir(resolvedPath)
                window.dispatchEvent(new CustomEvent('close-file-overlay'))
                window.dispatchEvent(new CustomEvent('open-file-manager'))
                return true
            }
        }
    } catch {
        // Batch-exists check failed — proceed with selectFile as best-effort
    }

    const ok = await store.selectFile(resolvedPath)
    if (ok) {
        window.dispatchEvent(new CustomEvent('open-file-overlay', { detail: { path: resolvedPath, lineStart, lineEnd } }))
        if (isExternal) {
            const { useToast } = await import('@/composables/useToast')
            useToast().show(gt('file.toast.externalFile'), { type: 'info', duration: 2000 })
        }
    }
    return ok
}

/**
 * Dispatch a scroll-to-line event after a file has been opened.
 */
export function dispatchScrollToLine(line: number, lineEnd?: number): void {
    setTimeout(() => {
        window.dispatchEvent(new CustomEvent('scroll-to-line', { detail: { line, lineEnd } }))
    }, 100)
}
