/**
 * Per-file scroll position cache.
 *
 * Lives at module scope so it survives FileViewer unmount/remount: closing the
 * file overlay (e.g. via Header → "open file manager") tears the viewer down
 * with v-if, which destroys instance state. Keeping the cache module-level
 * lets a later open from the recent-files list restore the exact position.
 *
 * The cache stores the LAST TRUSTED scroll offset — only values captured while
 * the scroll container was actually visible. A hidden CodeMirror has its
 * scrollTop reset to 0 by display:none, so callers must not overwrite a good
 * value with a 0 read from a hidden container (see FileViewer).
 */
export interface ScrollAnchorState {
    id: string
    line: number
    relTop: number
}

export interface BlockAnchorState {
    selector?: string
    index?: number
    tag?: string
    relTop: number
}

export interface FileScrollEntry {
    scrollTop: number
    anchor?: ScrollAnchorState | null
    blockAnchor?: BlockAnchorState | null
    ratio?: { ratio: number } | null
}

const fileScrollCache = new Map<string, FileScrollEntry>()

/** Maximum entries to keep (oldest evicted first) — prevents unbounded growth. */
const MAX_ENTRIES = 100

export function getFileScroll(path: string): number | undefined {
    return fileScrollCache.get(path)?.scrollTop
}

export function getFileScrollEntry(path: string): FileScrollEntry | undefined {
    return fileScrollCache.get(path)
}

export function setFileScroll(path: string, val: number | FileScrollEntry): void {
    const entry: FileScrollEntry = typeof val === 'number' ? { scrollTop: val } : val
    fileScrollCache.set(path, entry)
    if (fileScrollCache.size > MAX_ENTRIES) {
        const oldest = fileScrollCache.keys().next().value
        if (oldest !== undefined) fileScrollCache.delete(oldest)
    }
}

/** @internal Reset all state — for tests only. */
export function _resetFileScrollCache(): void {
    fileScrollCache.clear()
}
