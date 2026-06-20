import { escapeHtml } from '@/utils/html.ts'
import { gt } from '@/composables/useLocale'
import { resolveFilePath, registerWorktreeCacheClearter } from '@/composables/useFilePathAnnotation.ts'

// Register the clearWorktreeCache function to break circular dependency.
// useFilePathAnnotation calls this instead of importing clearWorktreeCache directly.
registerWorktreeCacheClearter(() => { worktreeListCache.clear() })

/**
 * SVG icon markup for the worktree button (GitBranch icon from lucide).
 */
export const WORKTREE_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/></svg>'

/**
 * Generate HTML for a single worktree action button.
 * Clicking it shows a modal with "Switch to worktree" or "Open directory" options.
 */
export function worktreeButtonHtml(absPath: string, resolvedPath?: string): string {
    let attrs = `class="chat-worktree-btn" data-worktree-path="${escapeHtml(absPath)}"`
    if (resolvedPath) {
        attrs += ` data-file-path="${escapeHtml(resolvedPath)}"`
    }
    return `<button ${attrs} title="${escapeHtml(gt('chat.attach.openWorktree'))}">${WORKTREE_ICON_SVG}</button>`
}

// ── Worktree list cache ──

interface WorktreeInfo {
    path: string
    displayPath: string
    branch: string
    isCurrent: boolean
    dirty: boolean
    changeCount: number
    untrackedCount: number
    locked: boolean
    missing: boolean
}

/** Cache: projectRoot → worktree list (from GET /api/git/worktrees) */
const worktreeListCache = new Map<string, WorktreeInfo[]>()

/** In-flight dedup: projectRoot → pending promise (prevents thundering herd) */
const pendingWorktreeFetches = new Map<string, Promise<WorktreeInfo[]>>()

/**
 * Fetch the worktree list for a project and cache it.
 * Returns the cached list if available, otherwise fetches and caches.
 * In-flight requests are deduplicated — concurrent callers share the same promise.
 */
async function fetchWorktreeList(projectRoot: string): Promise<WorktreeInfo[]> {
    const cached = worktreeListCache.get(projectRoot)
    if (cached) return cached

    // Return existing in-flight promise if one exists
    const pending = pendingWorktreeFetches.get(projectRoot)
    if (pending) return pending

    // Start a new fetch and store the promise for concurrent callers
    const fetchPromise = (async () => {
        try {
            const resp = await fetch('/api/git/worktrees')
            if (!resp.ok) return []
            const data = await resp.json()
            const worktrees: WorktreeInfo[] = data.worktrees || []
            worktreeListCache.set(projectRoot, worktrees)
            return worktrees
        } catch {
            return []
        } finally {
            pendingWorktreeFetches.delete(projectRoot)
        }
    })()

    pendingWorktreeFetches.set(projectRoot, fetchPromise)
    return fetchPromise
}

/**
 * Pre-warm the worktree list cache for a project.
 * Call this before rendering messages (e.g. in loadHistory) so that
 * annotateWorktreePaths has cached data available synchronously.
 * If the cache is already populated, this is a no-op.
 */
export async function warmWorktreeCache(projectRoot: string): Promise<void> {
    if (!projectRoot) return
    await fetchWorktreeList(projectRoot)
}

// ── Search entry building ──

interface SearchEntry {
    /** The search keyword (exact string to find in text) */
    keyword: string
    /** Absolute path of the worktree (for data-worktree-path) */
    absPath: string
}

/**
 * Build search entries from a worktree list.
 * Each worktree generates up to 3 keywords: path, displayPath, displayPath without ./
 * Entries are sorted by keyword length descending (longest first) to avoid
 * shorter entries matching substrings of longer ones.
 * Excludes worktrees where isCurrent is true.
 */
export function buildSearchEntries(worktrees: WorktreeInfo[]): SearchEntry[] {
    const entries: SearchEntry[] = []
    for (const wt of worktrees) {
        if (wt.isCurrent) continue
        // Absolute path
        if (wt.path) {
            entries.push({ keyword: wt.path, absPath: wt.path })
        }
        // displayPath (e.g. "./.worktrees/feature-x")
        if (wt.displayPath && wt.displayPath !== '.') {
            entries.push({ keyword: wt.displayPath, absPath: wt.path })
            // displayPath without leading "./" (e.g. ".worktrees/feature-x")
            const noDotSlash = wt.displayPath.replace(/^\.\//, '')
            if (noDotSlash !== wt.displayPath) {
                entries.push({ keyword: noDotSlash, absPath: wt.path })
            }
        }
    }
    // Sort by keyword length descending (longest first for greedy matching)
    entries.sort((a, b) => b.keyword.length - a.keyword.length)
    return entries
}

/**
 * Find a matching worktree search entry for a given text string.
 * Returns the first (longest) matching entry, or null.
 */
function findWorktreeMatch(text: string, searchEntries: SearchEntry[]): SearchEntry | null {
    const trimmed = text.trim()
    for (const entry of searchEntries) {
        if (trimmed === entry.keyword) return entry
    }
    return null
}

/**
 * Detect worktree paths in rendered HTML and insert action buttons after them.
 * Uses the cached worktree list (from GET /api/git/worktrees) for matching.
 *
 * Requires warmWorktreeCache() to be called before rendering messages
 * (done automatically in loadHistory). On cache miss, triggers background
 * fetch but returns un-annotated HTML (annotations will appear on next render).
 *
 * Processing order:
 *   1. <a href> tags matching a worktree path → append action button
 *   2. <code> and <span class="chat-file-path"> tags matching → add class + button
 *   3. Text nodes (outside a/code) → search worktree paths → insert span + button
 *
 * Returns the annotated HTML and a list of detected absolute worktree paths.
 */
export function annotateWorktreePaths(
    html: string,
    { projectRoot }: { projectRoot: string },
): { html: string; detectedWorktreePaths: string[] } {
    if (!html) return { html: '', detectedWorktreePaths: [] }

    const worktrees = worktreeListCache.get(projectRoot)
    if (!worktrees || worktrees.length === 0) {
        // Cache miss — trigger background fetch, return empty for now
        fetchWorktreeList(projectRoot)
        return { html, detectedWorktreePaths: [] }
    }

    const searchEntries = buildSearchEntries(worktrees)
    if (searchEntries.length === 0) return { html, detectedWorktreePaths: [] }

    const detectedWorktreePaths: string[] = []
    const doc = new DOMParser().parseFromString(html, 'text/html')

    // ── Step 1: <a href> tags matching a worktree path ──
    for (const a of doc.querySelectorAll('a[href]')) {
        const href = a.getAttribute('href') || ''
        const match = findWorktreeMatch(href, searchEntries)
        if (!match) continue
        detectedWorktreePaths.push(match.absPath)
        const resolved = resolveFilePath(match.absPath, projectRoot)
        a.insertAdjacentHTML('afterend', worktreeButtonHtml(match.absPath, resolved || undefined))
    }

    // ── Step 2: <code> tags whose content is purely a worktree path ──
    // Only handles the case where the entire element content matches a worktree.
    // Mixed content is handled by Step 3's text-node walker, which now
    // also enters <code> elements.
    // Note: we only query <code> tags here, not span.chat-file-path from prior
    // renders. In the same render pipeline, worktree annotation runs BEFORE
    // file-path annotation, so chat-file-path elements don't exist yet. If a
    // stale chat-file-path from a previous render matches a worktree, Step 3's
    // text-node walker will handle it (the walker skips chat-file-path parents,
    // but the stale span's text node would be inside it — this is acceptable
    // because on re-render the full pipeline replaces the HTML from scratch).
    for (const el of doc.querySelectorAll('code')) {
        if (el.classList.contains('chat-commit-hash')) continue
        if (el.classList.contains('chat-worktree-path')) continue
        const stripped = (el.textContent || '').trim()
        const match = findWorktreeMatch(stripped, searchEntries)
        if (!match) continue
        detectedWorktreePaths.push(match.absPath)
        el.classList.add('chat-worktree-path')
        el.setAttribute('data-worktree-path', match.absPath)
        const resolved = resolveFilePath(match.absPath, projectRoot)
        if (resolved) {
            el.setAttribute('data-file-path', resolved)
        }
        el.insertAdjacentHTML('afterend', worktreeButtonHtml(match.absPath, resolved || undefined))
    }

    // ── Step 3: Text nodes (outside a, but including inside <code>) → search worktree paths ──
    const textNodes: Text[] = []
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
        acceptNode(node: Text) {
            const parent = node.parentElement
            if (!parent) return NodeFilter.FILTER_REJECT
            if (parent.tagName === 'A' || parent.closest('a')) return NodeFilter.FILTER_REJECT
            // Skip elements already annotated by step 2
            if (parent.classList.contains('chat-worktree-path')) return NodeFilter.FILTER_REJECT
            if (parent.classList.contains('chat-file-path') || parent.classList.contains('chat-commit-hash')) return NodeFilter.FILTER_REJECT
            return NodeFilter.FILTER_ACCEPT
        }
    })
    while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

    // Process text nodes in reverse order so that DOM insertions
    // don't invalidate later node positions.
    for (let i = textNodes.length - 1; i >= 0; i--) {
        const textNode = textNodes[i]
        const text = textNode.textContent || ''

        // Collect all matches from search entries
        const matches: Array<{ index: number; length: number; entry: SearchEntry }> = []

        for (const entry of searchEntries) {
            let pos = 0
            while ((pos = text.indexOf(entry.keyword, pos)) !== -1) {
                matches.push({ index: pos, length: entry.keyword.length, entry })
                pos += entry.keyword.length
            }
        }

        if (matches.length === 0) continue

        // Sort by index, then deduplicate overlapping matches (longer entry wins)
        matches.sort((a, b) => a.index - b.index || b.length - a.length)
        const filtered: typeof matches = []
        let lastEnd = 0
        for (const m of matches) {
            if (m.index >= lastEnd) {
                filtered.push(m)
                lastEnd = m.index + m.length
            }
        }

        // Build replacement nodes
        const parent = textNode.parentNode!
        const frag = doc.createDocumentFragment()
        let hasAnnotation = false
        let lastIndex = 0

        for (const m of filtered) {
            // Push text before this match
            if (m.index > lastIndex) {
                frag.appendChild(doc.createTextNode(text.slice(lastIndex, m.index)))
            }
            hasAnnotation = true
            detectedWorktreePaths.push(m.entry.absPath)
            const span = doc.createElement('span')
            span.className = 'chat-worktree-path'
            span.setAttribute('data-worktree-path', m.entry.absPath)
            const resolved = resolveFilePath(m.entry.absPath, projectRoot)
            if (resolved) {
                span.setAttribute('data-file-path', resolved)
            }
            span.textContent = m.entry.keyword
            frag.appendChild(span)
            // Single action button
            const btnContainer = doc.createElement('span')
            btnContainer.innerHTML = worktreeButtonHtml(m.entry.absPath, resolved || undefined)
            while (btnContainer.firstChild) frag.appendChild(btnContainer.firstChild)
            lastIndex = m.index + m.length
        }
        // Push remaining text after last match
        if (lastIndex < text.length) {
            frag.appendChild(doc.createTextNode(text.slice(lastIndex)))
        }

        if (hasAnnotation) {
            parent.replaceChild(frag, textNode)
        }
    }

    return { html: doc.body.innerHTML, detectedWorktreePaths }
}

/**
 * Clear the worktree list cache (e.g. when switching projects).
 */
export function clearWorktreeCache(): void {
    worktreeListCache.clear()
}

/**
 * Composable for worktree path annotation in rendered HTML (v-html content).
 */
export function useWorktreeAnnotation() {
    return {
        annotateWorktreePaths,
        warmWorktreeCache,
        clearWorktreeCache,
    }
}
