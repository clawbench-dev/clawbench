import { escapeHtml } from '@/utils/html.ts'
import { gt } from '@/composables/useLocale'

/**
 * SVG icon markup for the commit-open button (git-commit icon).
 * A small circle with lines — resembles a commit node in a graph.
 */
export const COMMIT_OPEN_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><circle cx="12" cy="12" r="4"/><line x1="1.05" y1="12" x2="7" y2="12"/><line x1="17.01" y1="12" x2="22.96" y2="12"/></svg>'

/**
 * Regex that matches potential git commit hashes in plain text.
 * Matches 7-40 character hex strings with at least one a-f letter.
 * Word-boundary delimited to avoid matching inside longer strings.
 * Pure-decimal 7-digit numbers (timestamps, byte counts) are excluded
 * because git commit hashes are SHA-1 values that virtually always
 * contain at least one hex letter.
 */
const COMMIT_HASH_RE = /\b([0-9a-f]{7,40})\b/gi

/**
 * Check if a string looks like a git commit hash.
 * Must be 7-40 hex chars and contain at least one a-f letter
 * (to exclude pure-decimal strings like timestamps and byte counts).
 */
export function looksLikeCommitHash(text: string): boolean {
    if (text.length < 7 || text.length > 40) return false
    if (!/^[0-9a-f]+$/i.test(text)) return false
    return /[a-f]/i.test(text)
}

/**
 * Generate HTML for the small commit-open button.
 */
export function commitOpenButtonHtml(sha: string): string {
    return `<button class="chat-commit-open-btn" data-commit-sha="${escapeHtml(sha)}" title="${escapeHtml(gt('chat.attach.openCommit'))}">${COMMIT_OPEN_ICON_SVG}</button>`
}

/**
 * Detect potential git commit hashes in rendered HTML and insert open-commit buttons after them.
 *
 * Processing order:
 *   1. <code> tags whose text content looks like a commit hash → add class + button
 *   2. Text nodes (outside a/code) → regex match hashes → insert span + button
 *
 * Returns the annotated HTML and a list of detected SHAs for the caller to verify asynchronously.
 */
export function annotateCommitHashes(
    html: string,
): { html: string; detectedSHAs: string[] } {
    if (!html) return { html: '', detectedSHAs: [] }

    const detectedSHAs: string[] = []

    const doc = new DOMParser().parseFromString(html, 'text/html')

    // ── Step 1: <code> tags whose content is purely a commit hash ──
    // Only handles the case where the entire <code> content is a single hash.
    // Mixed content is handled by Step 2's text-node walker, which now
    // also enters <code> elements.
    for (const code of doc.querySelectorAll('code')) {
        // Skip <code> already annotated as file path
        if (code.classList.contains('chat-file-path')) continue
        const stripped = (code.textContent || '').trim()
        if (!looksLikeCommitHash(stripped)) continue
        detectedSHAs.push(stripped)
        code.classList.add('chat-commit-hash')
        code.setAttribute('data-commit-sha', stripped)
        code.insertAdjacentHTML('afterend', commitOpenButtonHtml(stripped))
    }

    // ── Step 2: Text nodes (outside a, but including inside <code>) → regex match hashes ──
    const textNodes: Text[] = []
    const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT, {
        acceptNode(node: Text) {
            const parent = node.parentElement
            if (!parent) return NodeFilter.FILTER_REJECT
            // Skip text inside <a> tags
            if (parent.tagName === 'A' || parent.closest('a')) return NodeFilter.FILTER_REJECT
            // Skip <code> elements already annotated by step 1
            if (parent.classList.contains('chat-commit-hash')) return NodeFilter.FILTER_REJECT
            // Skip already-annotated spans
            if (parent.classList.contains('chat-file-path')) return NodeFilter.FILTER_REJECT
            return NodeFilter.FILTER_ACCEPT
        }
    })
    while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

    // Process text nodes in reverse order so that DOM insertions
    // don't invalidate later node positions.
    for (let i = textNodes.length - 1; i >= 0; i--) {
        const textNode = textNodes[i]
        const text = textNode.textContent || ''
        COMMIT_HASH_RE.lastIndex = 0
        if (!COMMIT_HASH_RE.test(text)) continue

        // Re-run regex to collect matches
        COMMIT_HASH_RE.lastIndex = 0
        const parts: Array<{ text: string; sha: string | null }> = []
        let lastIndex = 0
        let match: RegExpExecArray | null
        while ((match = COMMIT_HASH_RE.exec(text)) !== null) {
            const shaStr = match[1]
            const isCommit = looksLikeCommitHash(shaStr)
            // Push the text before this match
            if (match.index > lastIndex) {
                parts.push({ text: text.slice(lastIndex, match.index), sha: null })
            }
            parts.push({ text: shaStr, sha: isCommit ? shaStr : null })
            lastIndex = match.index + shaStr.length
        }
        // Push remaining text after last match
        if (lastIndex < text.length) {
            parts.push({ text: text.slice(lastIndex), sha: null })
        }

        // Build replacement nodes
        const parent = textNode.parentNode!
        const frag = doc.createDocumentFragment()
        let hasAnnotation = false
        for (const part of parts) {
            if (part.sha) {
                hasAnnotation = true
                detectedSHAs.push(part.sha)
                const span = doc.createElement('span')
                span.className = 'chat-commit-hash'
                span.setAttribute('data-commit-sha', part.sha)
                span.textContent = part.text
                frag.appendChild(span)
                // Commit-open button
                const btnContainer = doc.createElement('span')
                btnContainer.innerHTML = commitOpenButtonHtml(part.sha)
                while (btnContainer.firstChild) frag.appendChild(btnContainer.firstChild)
            } else {
                frag.appendChild(doc.createTextNode(part.text))
            }
        }

        if (hasAnnotation) {
            parent.replaceChild(frag, textNode)
        }
    }

    return { html: doc.body.innerHTML, detectedSHAs }
}

// Cache of verified commit SHAs: sha -> commit info object (or null if not a commit)
// LRU eviction: when cache exceeds MAX_CACHE_SIZE, the oldest entry is deleted.
const MAX_CACHE_SIZE = 500
const verifiedCommitCache = new Map<string, any>()

/** Set a cache entry with LRU eviction — deletes oldest entry when limit exceeded */
function commitCacheSet(key: string, value: any): void {
    if (verifiedCommitCache.size >= MAX_CACHE_SIZE && !verifiedCommitCache.has(key)) {
        const oldest = verifiedCommitCache.keys().next().value
        if (oldest !== undefined) verifiedCommitCache.delete(oldest)
    }
    verifiedCommitCache.set(key, value)
}
// In-flight verification requests to avoid duplicates
const commitInFlight = new Map<string, Promise<boolean>>()

/**
 * Check which commit SHAs are valid git commit objects,
 * and hide buttons/annotations for SHAs that aren't.
 * Also caches commit info for later use by navigateToCommit.
 */
export async function verifyCommitHashes(shas: string[], containerEl: HTMLElement): Promise<void> {
    const unique = [...new Set(shas)]
    if (unique.length === 0) return

    // Batch verify: send all SHAs in one request
    let results: Map<string, any>
    try {
        const resp = await fetch('/api/git/verify-commits', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ shas: unique }),
        })
        if (!resp.ok) return
        const data = await resp.json()
        results = new Map(Object.entries(data.results || {}))
        // Update cache — value is commit info object or null
        for (const [sha, info] of results) {
            commitCacheSet(sha, info)
        }
    } catch {
        return // Network error — leave buttons as-is
    }

    for (const [sha, info] of results) {
        if (!info) {
            containerEl.querySelectorAll(`.chat-commit-open-btn[data-commit-sha="${CSS.escape(sha)}"]`).forEach(btn => {
                btn.remove()
            })
            containerEl.querySelectorAll(`.chat-commit-hash[data-commit-sha="${CSS.escape(sha)}"]`).forEach(span => {
                span.replaceWith(...span.childNodes)
            })
        }
    }
}

/**
 * Get cached commit info for a SHA (populated by verifyCommitHashes).
 * Returns null if not cached or not a valid commit.
 */
export function getCachedCommitInfo(sha: string): any | null {
    return verifiedCommitCache.get(sha) || null
}

/**
 * Clear the commit verification cache (e.g. when switching projects).
 */
export function clearCommitHashCache(): void {
    verifiedCommitCache.clear()
    commitInFlight.clear()
}

/**
 * Composable for commit hash annotation in rendered HTML (v-html content).
 */
export function useCommitHashAnnotation() {
    return {
        annotateCommitHashes,
        verifyCommitHashes,
        getCachedCommitInfo,
        commitOpenButtonHtml,
    }
}
