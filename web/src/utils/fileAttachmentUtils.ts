/**
 * Pure functions extracted from FileAttachmentList.vue for testability.
 */

/** FileEntry represents a file or directory attachment with metadata. */
export interface FileEntry {
  path: string
  isDir?: boolean
  startLine?: number
  endLine?: number
}

/** Normalize a file entry to FileEntry format.
 *  Backend returns FileEntry[] (new) or string[] (legacy), local push uses [{path: "..."}]. */
export function normalizeFileEntry(f: string | FileEntry): FileEntry {
  if (typeof f === 'string') return { path: f, isDir: false }
  return { path: f.path || '', isDir: f.isDir ?? false, startLine: f.startLine, endLine: f.endLine }
}

/** Check if a path points to an uploaded file (in .clawbench/uploads/). */
export function isUploadPath(path: string): boolean {
  return path.startsWith('.clawbench/uploads/') || path.startsWith('.clawbench\\uploads\\')
}

/** Common image file extensions. */
const IMAGE_EXTENSIONS = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp', '.ico', '.tiff', '.tif', '.avif']

/** Check if a path points to an image file based on its extension. */
export function isImageFile(path: string | null | undefined): boolean {
  if (!path) return false
  const lower = path.toLowerCase()
  return IMAGE_EXTENSIONS.some(ext => lower.endsWith(ext))
}

/** Deduplicate file entries by path, preferring entries with richer metadata (line ranges).
 *  When two entries share the same path, the one with startLine/endLine is kept. */
export function dedupeFiles(files: FileEntry[]): FileEntry[] {
  const result: FileEntry[] = []
  const byPath = new Map<string, FileEntry>()
  for (const f of files) {
    const existing = byPath.get(f.path)
    if (!existing) {
      byPath.set(f.path, f)
      result.push(f)
    } else if (f.startLine !== undefined && existing.startLine === undefined) {
      result[result.indexOf(existing)] = f
      byPath.set(f.path, f)
    }
  }
  return result
}
