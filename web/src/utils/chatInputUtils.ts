/**
 * Pure functions extracted from ChatInputBar.vue for testability.
 */

/**
 * Extract recently referenced files from message history.
 * Counts occurrences, excludes current file and already-attached files,
 * returns top 5 by frequency.
 */
export function computeRecentReferencedFiles(
  messages: { role: string; files?: (string | { path: string })[] }[] | null,
  attachedFiles: string[],
  currentFilePath: string | null | undefined
): { path: string; count: number }[] {
  if (!messages || messages.length === 0) return []
  const countMap = new Map<string, number>()
  for (const msg of messages) {
    if (msg.role !== 'user' || !msg.files) continue
    for (const f of msg.files) {
      const p = typeof f === 'string' ? f : f?.path
      if (!p) continue
      countMap.set(p, (countMap.get(p) || 0) + 1)
    }
  }
  const exclude = new Set([...attachedFiles])
  if (currentFilePath) exclude.add(currentFilePath)
  return [...countMap.entries()]
    .filter(([path]) => !exclude.has(path))
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)
    .map(([path, count]) => ({ path, count }))
}

/**
 * Check if any file groups (current file, current dir, recent shares, or recent references) should be shown.
 */
export function computeHasFileGroups(
  currentFilePath: string | null | undefined,
  currentDir: string | null | undefined,
  attachedFiles: string[],
  recentReferencedFiles: { path: string; count: number }[],
  recentShareCount: number = 0
): boolean {
  const hasCurrent = currentFilePath && !attachedFiles.includes(currentFilePath)
  const hasDir = currentDir && !attachedFiles.includes(currentDir)
  return !!hasCurrent || !!hasDir || recentShareCount > 0 || recentReferencedFiles.length > 0
}

/**
 * Compute the number of items in the attach menu for layout purposes.
 */
export function computeAttachMenuItemCount(
  currentFilePath: string | null | undefined,
  currentDir: string | null | undefined,
  attachedFiles: string[],
  recentReferencedFiles: { path: string; count: number }[],
  recentShareCount: number = 0
): number {
  let count = recentReferencedFiles.length + recentShareCount
  if (currentFilePath && !attachedFiles.includes(currentFilePath)) count++
  if (currentDir && !attachedFiles.includes(currentDir)) count++
  count++ // Upload file button
  return count
}
