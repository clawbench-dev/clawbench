/**
 * Pure functions extracted from ChatInputBar.vue for testability.
 */

import type { FileEntry } from '@/utils/fileAttachmentUtils'

/**
 * Detect whether a keydown event belongs to an active IME composition session
 * (e.g. Chinese pinyin candidate selection). During composition the browser
 * fires keydown events with `isComposing === true`, and some WebKit-based
 * engines (macOS Safari, Android WebView) additionally pin `keyCode` to 229.
 *
 * Return true to let the IME own the keystroke (Enter commits the candidate to
 * the input instead of submitting the message), false for a normal keystroke.
 */
export function isImeCompositionEvent(e: { isComposing?: boolean; keyCode?: number }): boolean {
  return !!e.isComposing || e.keyCode === 229
}

/**
 * Extract recently referenced files from message history.
 * Counts occurrences, excludes current file and already-attached files,
 * returns top 5 by frequency.
 */
export function computeRecentReferencedFiles(
  messages: { role: string; files?: (string | FileEntry)[] }[] | null,
  attachedFiles: FileEntry[],
  currentFilePath: string | null | undefined
): { path: string; count: number; isDir: boolean }[] {
  if (!messages || messages.length === 0) return []
  const countMap = new Map<string, number>()
  // Whether any occurrence of the path was a directory. A path's entries are
  // normally all the same kind; OR-ing keeps the flag if a legacy string entry
  // (no isDir) is mixed with a directory entry.
  const dirMap = new Map<string, boolean>()
  for (const msg of messages) {
    if (msg.role !== 'user' || !msg.files) continue
    for (const f of msg.files) {
      const p = typeof f === 'string' ? f : f?.path
      if (!p) continue
      countMap.set(p, (countMap.get(p) || 0) + 1)
      if (typeof f !== 'string' && f?.isDir) dirMap.set(p, true)
    }
  }
  const exclude = new Set(attachedFiles.map(f => f.path))
  if (currentFilePath) exclude.add(currentFilePath)
  return [...countMap.entries()]
    .filter(([path]) => !exclude.has(path))
    .sort((a, b) => b[1] - a[1])
    .slice(0, 20)
    .map(([path, count]) => ({ path, count, isDir: dirMap.get(path) === true }))
}

/**
 * Check if any file groups (current file, current dir, recent shares, or recent references) should be shown.
 */
export function computeHasFileGroups(
  currentFilePath: string | null | undefined,
  currentDir: string | null | undefined,
  attachedFiles: FileEntry[],
  recentReferencedFiles: { path: string; count: number }[],
  recentShareCount: number = 0
): boolean {
  const attachedPaths = attachedFiles.map(f => f.path)
  const hasCurrent = currentFilePath && !attachedPaths.includes(currentFilePath)
  const hasDir = currentDir && !attachedPaths.includes(currentDir)
  return !!hasCurrent || !!hasDir || recentShareCount > 0 || recentReferencedFiles.length > 0
}

/**
 * Compute the number of items in the attach menu for layout purposes.
 */
export function computeAttachMenuItemCount(
  currentFilePath: string | null | undefined,
  currentDir: string | null | undefined,
  attachedFiles: FileEntry[],
  recentReferencedFiles: { path: string; count: number }[],
  recentShareCount: number = 0
): number {
  const attachedPaths = attachedFiles.map(f => f.path)
  let count = recentReferencedFiles.length + recentShareCount
  if (currentFilePath && !attachedPaths.includes(currentFilePath)) count++
  if (currentDir && !attachedPaths.includes(currentDir)) count++
  count++ // Upload file button
  return count
}
