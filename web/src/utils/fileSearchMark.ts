import type { FileSearchResult } from '@/composables/useFileSearch'

/**
 * Display entry shape used by the file manager list/grid rendering.
 *
 * Browse mode entries come from the server DirEntry listing (name/type/size/
 * modified/symlink). Search-mode results only carry name/path/type/matchedIndices
 * where `path` is project-relative and NOT derived from the current directory,
 * so toDisplayEntry() adapts them into the shared DisplayEntry shape that
 * FileManagerContent renders for both modes.
 */
export interface DisplayEntry {
  name: string
  type: 'dir' | 'file'
  /** Project-relative path. In search mode this is independent of currentDir. */
  path: string
  /** Parent directory portion of path (shown as meta row for search results). */
  parentDir: string
  /** Search-match character indices; browse entries have none. */
  matchedIndices?: number[]
  size?: number
  modified?: string
}

/** Adapt a live search result into the shared DisplayEntry shape. */
export function toDisplayEntry(r: FileSearchResult): DisplayEntry {
  const i = r.path.lastIndexOf('/')
  return {
    name: r.name,
    // Backend search may tag images as 'image'; the grid only distinguishes
    // dir vs file, and thumbnails are decided by extension anyway.
    type: r.type === 'dir' ? 'dir' : 'file',
    path: r.path,
    parentDir: i > 0 ? r.path.slice(0, i) : '',
    matchedIndices: r.matchedIndices,
    size: r.size,
    modified: r.modified,
  }
}

/** Escape a string for safe insertion into HTML markup. */
export function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

/**
 * SECURITY: Each character is individually escaped before HTML markup is
 * applied. This prevents XSS via filenames containing <, >, &, etc.
 */
export function highlightName(name: string, indices: number[] | undefined): string {
  if (!indices || indices.length === 0) return escapeHtml(name)
  const indexSet = new Set(indices)
  let result = ''
  for (let i = 0; i < name.length; i++) {
    const ch = escapeHtml(name[i])
    if (indexSet.has(i)) {
      result += `<mark>${ch}</mark>`
    } else {
      result += ch
    }
  }
  return result
}
