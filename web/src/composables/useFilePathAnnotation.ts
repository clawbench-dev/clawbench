import { escapeHtml } from '@/utils/html.ts'
import { splitPath } from '@/utils/path.ts'
import { store } from '@/stores/app.ts'
import { gt } from '@/composables/useLocale'
import { clearCommitHashCache } from '@/composables/useCommitHashAnnotation.ts'
import { clearWorktreeCache } from '@/composables/useWorktreeAnnotation.ts'

/**
 * Try to decode a percent-encoded URI component.
 * Browsers/DOMPurify may encode non-ASCII chars (e.g. 中文 → %E4%B8%AD%E6%96%87)
 * in href attributes when HTML is inserted via innerHTML/v-html.
 * This ensures file paths with Chinese characters are decoded back to their
 * original form before being used as filesystem paths.
 */
function tryDecodeUri(uri: string): string {
    try {
        if (!uri.includes('%')) return uri
        return decodeURIComponent(uri)
    } catch {
        return uri
    }
}

/**
 * Resolve a file path to a project-relative path usable by store.selectFile().
 * Returns null if the path is not within the current project.
 * When projectRoot is empty, relative paths are returned as-is (best-effort).
 *
 * Supports tilde expansion: if homeDir is provided, ~/foo is expanded to
 * homeDir + /foo before checking against projectRoot. Without homeDir,
 * tilde-prefixed paths are rejected (we can't determine if they're in-project).
 */
export function resolveFilePath(path: string, projectRoot: string, homeDir?: string): string | null {
    // Reject paths containing glob wildcards, angle brackets, or double-star.
    // These are glob patterns or template variables, not real filesystem paths.
    if (/[*?\\[\]<>]/.test(path) || path.includes('**')) return null
    // Reject URLs (handled by localhost annotation, not file path annotation)
    if (/^https?:\/\//i.test(path)) return null
    // Reject environment variable paths (e.g. $HOME/.bashrc, ${HOME}/config)
    if (/\$/.test(path)) return null

    // Expand tilde (~/...) to absolute path using homeDir.
    // Only handle ~/ (current user's home) — ~username/ is not expanded.
    if (path.startsWith('~/') || path === '~') {
        if (!homeDir) return null // can't expand ~ without knowing home directory
        const expanded = homeDir + path.slice(1) // ~/foo → /home/user/foo
        // Now treat as absolute path
        if (!projectRoot) return null
        if (!expanded.startsWith(projectRoot + '/')) return null
        return expanded.slice(projectRoot.length + 1)
    }

    if (path.startsWith('/')) {
        // Absolute path: must be under projectRoot
        if (!projectRoot) return null
        if (!path.startsWith(projectRoot)) return null
        const absolutePath = path
        if (absolutePath.startsWith(projectRoot + '/')) {
            return absolutePath.slice(projectRoot.length + 1)
        }
        if (absolutePath === projectRoot) return null
        return null
    }

    // Relative path
    if (!projectRoot) {
        // No projectRoot — strip leading ./ and return as-is (best-effort)
        let clean = path.replace(/^\.\//, '')
        // Reject paths that go above root (../) when we can't verify
        if (clean.startsWith('../')) return null
        return clean
    }

    // Resolve ./ and ../ against projectRoot
    const parts = projectRoot.split('/').filter(Boolean)
    const segments = path.split('/')
    for (const seg of segments) {
        if (seg === '..') {
            if (parts.length > 0) parts.pop()
            else return null // goes above project root
        } else if (seg !== '.' && seg !== '') {
            parts.push(seg)
        }
    }
    const absolutePath = '/' + parts.join('/')
    if (absolutePath.startsWith(projectRoot + '/')) {
        return absolutePath.slice(projectRoot.length + 1)
    }
    return null
}

/**
 * SVG icon markup for the file-open button (external-link icon).
 * Shared constant so both fileOpenButtonHtml() and Vue templates use the same icon.
 */
export const FILE_OPEN_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>'

/**
 * Generate HTML for the small open-file button.
 * Optionally includes line range attributes for scrolling to specific lines.
 */
export function fileOpenButtonHtml(resolvedPath: string, lineStart?: number, lineEnd?: number): string {
    const lineAttrs = lineStart ? ` data-line-start="${lineStart}"${lineEnd ? ` data-line-end="${lineEnd}"` : ''}` : ''
    return `<button class="chat-file-open-btn" data-file-path="${escapeHtml(resolvedPath)}"${lineAttrs} title="${escapeHtml(gt('chat.attach.openFile'))}">${FILE_OPEN_ICON_SVG}</button>`
}

/**
 * Extract the bare file path and optional line range from a matched string.
 * E.g. "src/main.go:70-81" → { path: "src/main.go", lineStart: 70, lineEnd: 81 }
 * E.g. "src/main.go:42" → { path: "src/main.go", lineStart: 42, lineEnd: undefined }
 * E.g. "src/main.go" → { path: "src/main.go", lineStart: undefined, lineEnd: undefined }
 */
function extractLineInfo(matchStr: string, match: RegExpExecArray): { path: string; lineStart?: number; lineEnd?: number } {
    const lineStartStr = match[1]
    const lineEndStr = match[2]
    if (!lineStartStr) return { path: matchStr }
    // The full match includes the line suffix; strip it to get the bare path
    const colonIdx = matchStr.lastIndexOf(':')
    const path = colonIdx > 0 ? matchStr.slice(0, colonIdx) : matchStr
    return {
        path,
        lineStart: parseInt(lineStartStr, 10),
        lineEnd: lineEndStr ? parseInt(lineEndStr, 10) : undefined,
    }
}

/**
 * Extract bare path and optional line info from a plain text string
 * (not from a regex match). Used by Step 2 for <code> tag content.
 * E.g. "src/main.go:70-81" → { path: "src/main.go", lineStart: 70, lineEnd: 81 }
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

export interface AnnotateFilePathsOptions {
    projectRoot: string
    /** Base directory for resolving relative <a href="..."> links (e.g. the md file's dir) */
    baseDir?: string
    /** User's home directory (from backend), used to expand ~/ paths */
    homeDir?: string
}

/**
 * Regex that matches file paths in plain text, with optional line suffixes.
 * Three forms combined into one regex:
 *   1. Absolute/tilde paths: /home/user/project/src/main.go, ~/.config/nvim/init.lua
 *   2. Relative paths with ./ or ../:  ./lib/utils.ts, ../config/settings.json
 *   3. Bare relative paths: src/main.go (at least two segments + extension)
 *
 * Optional line suffix after the file extension:
 *   :42        → single line (captured in group 1)
 *   :70-81     → line range (captured in groups 1 and 2)
 *
 * Requirements: at least one '/' and a file extension (dot + alpha + optional alphanum).
 * Paths must not contain spaces, angle brackets, quotes, or closing parens/brackets.
 *
 * Because this regex only runs on text node content (never on HTML attributes or tags),
 * there is no risk of matching inside data-file-path or other generated attributes.
 */
const FILE_PATH_RE = /(?:~?\/[^\s<>"')\]]+(?:\/[^\s<>"')\]]+)+\.[a-zA-Z][a-zA-Z0-9]*|\.\.?\/[^\s<>"')\]]+(?:\/[^\s<>"')\]]+)*\.[a-zA-Z][a-zA-Z0-9]*|[a-zA-Z0-9_-]+(?:\/[a-zA-Z0-9_.-]+)+\.[a-zA-Z][a-zA-Z0-9]*)(?::(\d+)(?:-(\d+))?)?/g

/**
 * Check if a string looks like a file path that should be annotated.
 * - Contains at least one '/' (e.g. src/foo.ts, ./bar.go)
 * - Or has a short file extension (e.g. ChatPanel.vue, main.go)
 * - Optional line suffix: :42 or :70-81
 * Bare identifiers like `useAutoSpeech`, `onUnmounted`, `ref` should NOT match.
 * Rejects strings containing glob wildcards, angle brackets, or double-star
 * — these are glob patterns or template variables, not real file paths.
 */
function looksLikeFilePath(text: string): boolean {
    if (/[*?\\[\]<>]/.test(text) || text.includes('**')) return false
    // Exclude URLs (handled by localhost annotation)
    if (/^https?:\/\//i.test(text)) return false
    // Exclude environment variable paths (e.g. $HOME/.bashrc, ${HOME}/config)
    if (/\$/.test(text)) return false
    // Strip optional line suffix (:42 or :70-81) before checking
    const bare = text.replace(/:\d+(-\d+)?$/, '')
    return /\/|\.[a-zA-Z][a-zA-Z0-9]{0,3}$/.test(bare)
}

/**
 * Detect file paths in rendered HTML and insert open-file buttons after them.
 *
 * Design: DOM traversal, not regex-on-HTML-string.
 * We parse the HTML with DOMParser, then walk text nodes and apply path
 * regexes only to plain text content — never to HTML attributes or tags.
 * This eliminates the class of bugs where a later regex matches content
 * inside HTML attributes generated by an earlier regex pass.
 *
 * Processing order:
 *   1. <a href="..."> tags with local-file hrefs → append open button
 *   2. <code> tags whose text content looks like a path → add class + button
 *   3. Text nodes (outside a/code) → regex match paths → insert span + button
 *
 * Returns the annotated HTML and a list of detected (resolved) paths
 * for the caller to verify asynchronously.
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
        // Decode percent-encoded href (e.g. %E4%B8%AD%E6%96%87 → 中文)
        // Browsers/DOMPurify may encode non-ASCII chars when inserting HTML
        const href = tryDecodeUri(rawHref)
        // Skip external links, anchors, mailto, tel
        if (/^(https?:|\/\/|mailto:|tel:|#)/i.test(href)) continue
        const resolved = baseDir
            ? resolveRelativePath(href, baseDir)
            : resolveFilePath(href, projectRoot, homeDir)
        if (!resolved) continue
        detectedPaths.push(resolved)
        a.insertAdjacentHTML('afterend', fileOpenButtonHtml(resolved))
    }

    // ── Step 2: <code> tags whose content is purely a file path ──
    // Only handles the case where the entire <code> content is a single path.
    // Mixed content (e.g. `import "src/main.go"`) is handled by Step 3's
    // text-node walker, which now also enters <code> elements.
    for (const code of doc.querySelectorAll('code')) {
        // Skip <code> already annotated as worktree (worktree annotation runs first)
        if (code.classList.contains('chat-worktree-path')) continue
        const stripped = (code.textContent || '').trim()
        if (!looksLikeFilePath(stripped)) continue
        // Extract bare path and optional line info
        const { path: barePath, lineStart, lineEnd } = extractLineInfoFromText(stripped)
        const resolved = resolveFilePath(barePath, projectRoot, homeDir)
        if (!resolved || resolved.includes(' ') || resolved.includes('"')) continue
        // Entire <code> content is a valid file path — annotate the whole element
        detectedPaths.push(resolved)
        code.classList.add('chat-file-path')
        code.setAttribute('data-file-path', resolved)
        if (lineStart) code.setAttribute('data-line-start', String(lineStart))
        if (lineEnd) code.setAttribute('data-line-end', String(lineEnd))
        code.insertAdjacentHTML('afterend', fileOpenButtonHtml(resolved, lineStart, lineEnd))
    }

    // ── Step 3: Text nodes (outside a/worktree-annotated, but including inside <code>) → regex match paths ──
    const textNodes: Text[] = []
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
        acceptNode(node: Text) {
            const parent = node.parentElement
            if (!parent) return NodeFilter.FILTER_REJECT
            // Skip text inside <a> tags (handled in step 1)
            if (parent.tagName === 'A' || parent.closest('a')) return NodeFilter.FILTER_REJECT
            // Skip text inside <code> elements already annotated by step 2
            if (parent.classList.contains('chat-file-path')) return NodeFilter.FILTER_REJECT
            // Skip text inside worktree-annotated elements (worktree annotation runs first)
            if (parent.classList.contains('chat-worktree-path') || parent.closest('.chat-worktree-path')) return NodeFilter.FILTER_REJECT
            return NodeFilter.FILTER_ACCEPT
        }
    })
    while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

    // Process text nodes in reverse order so that DOM insertions
    // don't invalidate later node positions.
    for (let i = textNodes.length - 1; i >= 0; i--) {
        const textNode = textNodes[i]
        const text = textNode.textContent || ''
        FILE_PATH_RE.lastIndex = 0
        if (!FILE_PATH_RE.test(text)) continue

        // Re-run regex to collect matches (test() consumed lastIndex)
        FILE_PATH_RE.lastIndex = 0
        const parts: Array<{ text: string; resolved: string | null; lineStart?: number; lineEnd?: number }> = []
        let lastIndex = 0
        let match: RegExpExecArray | null
        while ((match = FILE_PATH_RE.exec(text)) !== null) {
            const pathStr = match[0]
            const { path: barePath, lineStart, lineEnd } = extractLineInfo(pathStr, match)
            let resolved = resolveFilePath(barePath, projectRoot, homeDir)
            // If the text immediately after this match continues with a path segment
            // (e.g. "/.worktrees" followed by "/gitgraph-fix"), the regex only matched
            // a directory prefix — skip it so worktree annotation can handle the full path.
            // Note: This may over-suppress in rare cases (e.g. "src/utils.ts/v2"),
            // but directory-prefix false matches are far more common and harmful.
            if (resolved) {
                const afterIdx = match.index + pathStr.length
                if (afterIdx < text.length && text[afterIdx] === '/') {
                    const rest = text.slice(afterIdx + 1)
                    // If there's a continuation that looks like a path segment, this
                    // match is incomplete (a directory prefix, not a full file path).
                    if (rest.length > 0 && /^[a-zA-Z0-9_.-]/.test(rest)) {
                        resolved = null // incomplete match — likely a directory prefix
                    }
                }
            }
            // Push the text before this match
            if (match.index > lastIndex) {
                parts.push({ text: text.slice(lastIndex, match.index), resolved: null })
            }
            parts.push({ text: pathStr, resolved, lineStart: resolved ? lineStart : undefined, lineEnd: resolved ? lineEnd : undefined })
            lastIndex = match.index + pathStr.length
        }
        // Push remaining text after last match
        if (lastIndex < text.length) {
            parts.push({ text: text.slice(lastIndex), resolved: null })
        }

        // Build replacement nodes
        const parent = textNode.parentNode!
        const frag = doc.createDocumentFragment()
        let hasAnnotation = false
        for (const part of parts) {
            if (part.resolved) {
                hasAnnotation = true
                detectedPaths.push(part.resolved)
                const span = doc.createElement('span')
                span.className = 'chat-file-path'
                span.setAttribute('data-file-path', part.resolved)
                if (part.lineStart) span.setAttribute('data-line-start', String(part.lineStart))
                if (part.lineEnd) span.setAttribute('data-line-end', String(part.lineEnd))
                span.textContent = part.text
                frag.appendChild(span)
                // Open-file button (as HTML snippet since it contains SVG)
                const btnContainer = doc.createElement('span')
                btnContainer.innerHTML = fileOpenButtonHtml(part.resolved, part.lineStart, part.lineEnd)
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

// Cache of verified paths: path -> true (exists) | false (not found)
const verifiedCache = new Map<string, boolean>()
// In-flight batch request to avoid duplicate calls
let batchInFlight: Promise<{ results: Record<string, string> }> | null = null

/**
 * Check which file paths actually exist on the server,
 * and hide buttons for files that don't exist.
 * Uses a single batch POST /api/file/batch-exists request instead of
 * per-path HEAD requests, dramatically reducing HTTP overhead.
 */
export async function verifyFilePaths(paths: string[], containerEl: HTMLElement): Promise<void> {
    const unique = [...new Set(paths)]
    if (unique.length === 0) return

    // Check cache first, collect uncached paths
    const uncached: string[] = []
    const results = new Map<string, boolean>()

    for (const p of unique) {
        if (verifiedCache.has(p)) {
            results.set(p, verifiedCache.get(p)!)
        } else {
            uncached.push(p)
        }
    }

    // Batch request for uncached paths
    if (uncached.length > 0) {
        try {
            // Reuse in-flight batch request if one is already running
            if (!batchInFlight) {
                batchInFlight = fetch('/api/file/batch-exists', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ paths: uncached }),
                }).then(r => r.json())
            }
            const resp = await batchInFlight
            batchInFlight = null

            for (const [path, type] of Object.entries(resp.results)) {
                const exists = type === 'file' || type === 'dir'
                results.set(path, exists)
                verifiedCache.set(path, exists)
            }
        } catch {
            // Network error — assume all exist (best effort)
            for (const p of uncached) {
                results.set(p, true)
            }
        }
    }

    // Remove non-existent path annotations from DOM
    for (const [path, exists] of results) {
        if (!exists) {
            containerEl.querySelectorAll(`.chat-file-open-btn[data-file-path="${CSS.escape(path)}"]`).forEach(btn => {
                btn.remove()
            })
            containerEl.querySelectorAll(`.chat-file-path[data-file-path="${CSS.escape(path)}"]`).forEach(span => {
                span.replaceWith(...span.childNodes)
            })
        }
    }
}

/**
 * Clear the verification cache (e.g. when switching projects).
 */
export function clearVerifiedCache(): void {
    verifiedCache.clear()
    batchInFlight = null
    clearCommitHashCache()
    clearWorktreeCache()
}

/**
 * Composable for file path annotation in rendered HTML (v-html content).
 */
export function useFilePathAnnotation() {
    return {
        resolveFilePath,
        fileOpenButtonHtml,
        annotateFilePaths,
        verifyFilePaths,
        resolveRelativePath,
        openFilePath,
        dispatchScrollToLine,
        clearVerifiedCache,
    }
}

/**
 * Resolve a relative href against a base directory.
 * Normalizes . and .. segments.
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
 * Open a file or directory path.
 * If the path is a directory, navigates to it and opens the file manager.
 * If it's a file, selects it in the store.
 * If the file doesn't exist, shows a toast and does not navigate to a potentially
 * non-existent directory (fixes issue #166).
 * If lineStart is provided, dispatches a scroll-to-line event after opening.
 */
export async function openFilePath(resolvedPath: string, lineStart?: number): Promise<boolean> {
    // Check if path is a directory
    try {
        const resp = await fetch(`/api/dir?path=${encodeURIComponent(resolvedPath)}`)
        if (resp.ok) {
            await store.navigateToDir(resolvedPath)
            window.dispatchEvent(new CustomEvent('open-file-manager'))
            return true
        }
    } catch {
        // Ignore, fall through to open as file
    }

    // Before selecting the file, verify it actually exists.
    // If it doesn't exist, avoid setting currentFile (which would trigger
    // syncToCurrentFile to navigate to a non-existent directory).
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
                // File doesn't exist — show toast and abort
                const { useToast } = await import('@/composables/useToast')
                const { gt } = await import('@/composables/useLocale')
                useToast().show(gt('file.toast.fileNotFound'), { type: 'error', icon: '⚠️', duration: 2000 })
                return false
            }
        }
    } catch {
        // Batch-exists check failed — proceed with selectFile as best-effort
    }

    const ok = await store.selectFile(resolvedPath)
    if (ok) {
        // 通知 App.vue 打开文件覆盖层
        window.dispatchEvent(new CustomEvent('open-file-overlay', { detail: { path: resolvedPath, lineStart } }))
    }
    return ok
}

/**
 * Dispatch a scroll-to-line event after a file has been opened.
 * The App.vue listens for this event to scroll the file viewer.
 */
export function dispatchScrollToLine(line: number): void {
    // Use a small delay to allow the file content to render
    setTimeout(() => {
        window.dispatchEvent(new CustomEvent('scroll-to-line', { detail: { line } }))
    }, 100)
}
