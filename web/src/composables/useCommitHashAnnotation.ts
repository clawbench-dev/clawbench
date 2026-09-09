import { escapeHtml } from '@/utils/html.ts'
import { gt } from '@/composables/useLocale'

/**
 * SVG icon markup for the commit-open button (git-commit icon).
 * A small circle with lines — resembles a commit node in a graph.
 */
export const COMMIT_OPEN_ICON_SVG = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="12" height="12"><circle cx="12" cy="12" r="4"/><line x1="1.05" y1="12" x2="7" y2="12"/><line x1="17.01" y1="12" x2="22.96" y2="12"/></svg>'

/**
 * Regex that matches potential git commit hashes in plain text.
 * Matches only the two lengths git actually produces:
 *   - abbreviated SHA (7-12 hex chars; git starts at 7 and grows for
 *     uniqueness across all objects — e.g. 45131649, abc1234)
 *   - full SHA-1 (40 hex chars)
 * Any other length (13-39 hex chars) is not a form git prints or accepts as a
 * stable identifier, so it is left untouched.
 *
 * Exclusions via negative lookbehind:
 *   - # prefix → CSS color values (#ff0000, #abcdef0)
 *   - 0x/0X prefix → hex literals (0xabcdef0)
 *   - \u/\U prefix → Unicode escape sequences (\u00abcdef)
 *   - % suffix → URL-encoded segments (e.g., %2Fabcdef — rare but possible)
 *   - :// preceding → URL scheme hex (http://abcdef0)
 *
 * Note: single colon is NOT excluded — patterns like "commit:abc1234" are
 * legitimate git output and must match.
 *
 * Pure-decimal strings (e.g. 45131649) are intentionally NOT excluded here:
 * when a commit's SHA happens to start with digits, git's abbreviated form is
 * purely numeric (e.g. `git log --oneline` shows 45131649). Such a candidate
 * is passed to the backend verification endpoint, which resolves it against
 * the actual repo via `git log --ignore-missing` — non-commits (timestamps,
 * byte counts, IDs) come back null and their annotation is unwrapped. See
 * looksLikeCommitHash() for the only remaining heuristic (all-same-character).
 *
 * NOTE: This regex deliberately avoids regex lookbehind (Safari/iPadOS < 16.4
 * does not support it — a lookbehind literal throws SyntaxError at parse time
 * and white-screens the whole bundle). Prefix exclusions are enforced in
 * hasExcludedCommitPrefix() instead.
 */
export const COMMIT_HASH_RE = /(\b[0-9a-f]{40}\b|\b[0-9a-f]{7,12}\b)(?!%)/gi

/**
 * Check whether the text immediately before a candidate hash match is one of
 * the excluded prefixes (lookbehind emulation for Safari < 16.4):
 *   - # → CSS color values
 *   - \ → Unicode escape sequences
 *   - % → URL-encoded segments
 *   - :// → URL scheme hex
 *
 * Note: 0x/0X prefixes do not need a check here — \b already requires a
 * word boundary before the hash, and 'x'/'X' are word characters, so a hash
 * cannot directly follow "0x" anyway.
 */
export function hasExcludedCommitPrefix(text: string, index: number): boolean {
    if (index <= 0) return false
    const prev = text[index - 1]
    if (prev === '#' || prev === '\\' || prev === '%') return true
    return index >= 3 && text.slice(index - 3, index) === '://'
}

/**
 * Check if a string looks like a git commit hash.
 * Only the two lengths git actually produces are accepted:
 *   - 7-12 hex chars: abbreviated SHA (git starts at 7 and grows as needed
 *     for uniqueness across all objects — Linux-sized repos reach 12).
 *   - 40 hex chars: full SHA-1.
 * Anything else (e.g. 13-39 chars) is not a form git prints, so it is rejected.
 * Strings are accepted regardless of whether they contain an a-f letter —
 * abbreviated SHAs can be purely numeric (a commit whose full SHA starts with
 * digits, e.g. 45131649). Whether a candidate is a real commit is decided by
 * the backend verification round-trip (/api/git/verify-commits with
 * git log --ignore-missing); non-commits are unwrapped again. The only
 * remaining heuristic:
 *   - All same character (e.g., aaaaaaa, 0000000) — not real SHAs
 */
export function looksLikeCommitHash(text: string): boolean {
    if (text.length < 7 || text.length > 40) return false
    // Short (7-12) or full (40) — the only lengths git prints. Mid-range
    // 13-39-char strings are never abbreviated SHAs and are ignored.
    if (text.length !== 40 && text.length > 12) return false
    if (!/^[0-9a-f]+$/i.test(text)) return false
    // Exclude all-same-character strings (aaaaaaa, 0000000, etc.)
    if (/^(.)\1{6,}$/.test(text)) return false
    return true
}

/**
 * Generate HTML for the small commit-open button.
 */
export function commitOpenButtonHtml(sha: string): string {
    return `<button class="chat-commit-open-btn" data-commit-sha="${escapeHtml(sha)}" title="${escapeHtml(gt('chat.attach.openCommit'))}">${COMMIT_OPEN_ICON_SVG}</button>`
}

/**
 * Detect potential git commit hashes in rendered HTML and add pending annotations.
 *
 * Unlike the previous design, this function does NOT insert open-commit buttons.
 * Annotations start with class `chat-commit-hash-pending` (neutral styling, no cursor).
 * After async verification via `verifyCommitHashes()`, valid SHAs get their class
 * changed to `chat-commit-hash` (accent color + cursor) and an open button inserted.
 *
 * Processing order:
 *   1. <code> tags whose text content looks like a commit hash → add pending class
 *   2. Text nodes (outside a/code) → regex match hashes → insert pending span
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
        code.classList.add('chat-commit-hash-pending')
        code.setAttribute('data-commit-sha', stripped)
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
            if (parent.classList.contains('chat-commit-hash-pending')) return NodeFilter.FILTER_REJECT
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
            // Exclude hashes with disallowed prefixes (lookbehind emulation
            // for Safari < 16.4 — the regex itself has no lookbehind).
            const isCommit = looksLikeCommitHash(shaStr) && !hasExcludedCommitPrefix(text, match.index)
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
                span.className = 'chat-commit-hash-pending'
                span.setAttribute('data-commit-sha', part.sha)
                span.textContent = part.text
                frag.appendChild(span)
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
const verifiedCommitCache = new Map<string, Record<string, unknown> | null>()

/** Set a cache entry with LRU eviction — deletes oldest entry when limit exceeded */
function commitCacheSet(key: string, value: Record<string, unknown> | null): void {
    if (verifiedCommitCache.size >= MAX_CACHE_SIZE && !verifiedCommitCache.has(key)) {
        const oldest = verifiedCommitCache.keys().next().value
        if (oldest !== undefined) verifiedCommitCache.delete(oldest)
    }
    verifiedCommitCache.set(key, value)
}

/**
 * Check which commit SHAs are valid git commit objects.
 *
 * For valid SHAs: upgrades pending annotations to verified (changes class from
 * `chat-commit-hash-pending` to `chat-commit-hash`, adds accent color + cursor,
 * inserts open button after the element).
 *
 * For invalid SHAs: removes pending annotations entirely (unwraps span/code).
 *
 * On network error or non-ok response: also removes all pending annotations
 * for the requested SHAs (safer to remove than to leave unverified annotations
 * looking like real commits).
 *
 * Also caches commit info for later use by navigateToCommit.
 */
export async function verifyCommitHashes(shas: string[], containerEl: HTMLElement): Promise<void> {
    const unique = [...new Set(shas)]
    if (unique.length === 0) return

    // Batch verify: send all SHAs in one request
    let results: Map<string, Record<string, unknown> | null>
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
        return
    }

    for (const [sha, info] of results) {
        if (info) {
            // Valid commit — upgrade pending annotation to verified
            upgradeToVerified(sha, containerEl)
        } else {
            // Invalid commit — remove annotation entirely
            removePendingAnnotations([sha], containerEl)
        }
    }
}

/**
 * Upgrade a pending commit hash annotation to verified state.
 * Changes class from `chat-commit-hash-pending` to `chat-commit-hash`
 * and inserts the open button after the element.
 */
function upgradeToVerified(sha: string, containerEl: HTMLElement): void {
    const escaped = CSS.escape(sha)
    // Handle <span class="chat-commit-hash-pending"> elements
    containerEl.querySelectorAll(`span.chat-commit-hash-pending[data-commit-sha="${escaped}"]`).forEach(el => {
        el.classList.remove('chat-commit-hash-pending')
        el.classList.add('chat-commit-hash')
        // Insert open button after the span
        el.insertAdjacentHTML('afterend', commitOpenButtonHtml(sha))
    })
    // Handle <code class="chat-commit-hash-pending"> elements
    containerEl.querySelectorAll(`code.chat-commit-hash-pending[data-commit-sha="${escaped}"]`).forEach(el => {
        el.classList.remove('chat-commit-hash-pending')
        el.classList.add('chat-commit-hash')
        // Insert open button after the code element
        el.insertAdjacentHTML('afterend', commitOpenButtonHtml(sha))
    })
}

/**
 * Remove pending commit hash annotations for the given SHAs.
 * Unwraps the span/code element, leaving only the text content.
 */
function removePendingAnnotations(shas: string[], containerEl: HTMLElement): void {
    for (const sha of shas) {
        const escaped = CSS.escape(sha)
        // Remove pending span annotations (unwrap, keep text)
        containerEl.querySelectorAll(`span.chat-commit-hash-pending[data-commit-sha="${escaped}"]`).forEach(span => {
            span.replaceWith(...span.childNodes)
        })
        // Remove pending code annotations (just remove the class and attribute)
        containerEl.querySelectorAll(`code.chat-commit-hash-pending[data-commit-sha="${escaped}"]`).forEach(code => {
            code.classList.remove('chat-commit-hash-pending')
            code.removeAttribute('data-commit-sha')
        })
    }
}

/**
 * Get cached commit info for a SHA (populated by verifyCommitHashes).
 * Returns null if not cached or not a valid commit.
 */
export function getCachedCommitInfo(sha: string): Record<string, unknown> | null {
    return verifiedCommitCache.get(sha) || null
}

/**
 * Clear the commit verification cache (e.g. when switching projects).
 */
export function clearCommitHashCache(): void {
    verifiedCommitCache.clear()
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
