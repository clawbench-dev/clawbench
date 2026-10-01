/**
 * useFileChangeBaseline — accumulated baseline storage.
 *
 * When a file changes on disk, the content as it was the FIRST time we observed
 * a change becomes the baseline; the marker is always diff(baseline, current).
 * Only the baseline content is stored, not marker arrays: the same file has
 * different marker shapes per surface (markdown keys by block index, code by
 * line number), so caching two arrays would raise a "which one is authoritative"
 * sync problem. Storing only the baseline lets both surfaces derive from one
 * source of truth.
 *
 * Memory bounds: LRU cap of MAX_BASELINE_ENTRIES; a single entry over
 * MAX_BASELINE_BYTES is not stored (baselines are large strings in a long
 * session and would otherwise pile up).
 */

/** LRU cap. The least-recently-read entry is evicted past this. */
export const MAX_BASELINE_ENTRIES = 20

/** Per-entry byte cap (counted in UTF-16 code units). Over this, no baseline. */
export const MAX_BASELINE_BYTES = 2 * 1024 * 1024

// Map insertion order IS the LRU order: delete-then-set moves an entry to the
// "most recently used" end.
const baselines = new Map<string, string>()

/**
 * Record a baseline. Only written when the path has none yet (accumulated
 * semantics).
 *
 * - Content equal to the baseline → the file returned to its baseline; drop it.
 * - Content over the size cap → no baseline.
 */
export function recordBaseline(path: string, content: string): void {
  const existing = baselines.get(path)
  if (existing !== undefined) {
    if (existing === content) baselines.delete(path)
    return
  }
  if (content.length > MAX_BASELINE_BYTES) return
  baselines.set(path, content)
  if (baselines.size > MAX_BASELINE_ENTRIES) {
    const oldest = baselines.keys().next().value
    if (oldest !== undefined) baselines.delete(oldest)
  }
}

/** Read the baseline (refreshes LRU position on hit). Returns null if none. */
export function getBaseline(path: string): string | null {
  const value = baselines.get(path)
  if (value === undefined) return null
  // Re-insert to mark as most recently used.
  baselines.delete(path)
  baselines.set(path, value)
  return value
}

/** Delete a single baseline. */
export function clearBaseline(path: string): void {
  baselines.delete(path)
}

/** Clear everything (project switch / manual clear). */
export function clearAllBaselines(): void {
  baselines.clear()
}

/** Current baseline count (tests + memory observation). */
export function baselineCount(): number {
  return baselines.size
}
