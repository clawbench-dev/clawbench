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

/** Composite identity of an entry: a path can carry multiple independent
 *  line-range references, so dedupe/merge by (path, startLine ?? 0, endLine ?? 0). */
function entryKey(f: FileEntry): string {
  return `${f.path}|${f.startLine ?? 0}|${f.endLine ?? 0}`
}

/**
 * Deduplicate file entries by their composite key (path + line range).
 * Distinct line ranges of the same path are all kept; exact duplicates collapse.
 */
export function dedupeFiles(files: FileEntry[]): FileEntry[] {
  const result: FileEntry[] = []
  const seen = new Set<string>()
  for (const f of files) {
    const key = entryKey(normalizeFileEntry(f))
    if (seen.has(key)) continue
    seen.add(key)
    result.push(normalizeFileEntry(f))
  }
  return result
}

/**
 * Split attachments into the two backend channels.
 *
 * The backend cross-deduplicates: a `Files` entry whose path is also in
 * `filePaths` is dropped (see internal/handler/chat.go), losing its line
 * range. So any path that carries a LINE-RANGE reference must go through
 * `entries` only — every entry for that path is moved there and the path is
 * excluded from `filePaths`. Paths attached only as whole files keep the
 * legacy `filePaths` channel.
 */
export function buildSendChannels(files: FileEntry[]): { filePaths: string[]; entries: FileEntry[] } {
  const rangedPaths = new Set(files.filter(f => f.startLine !== undefined).map(f => f.path))
  const entries: FileEntry[] = []
  const filePaths: string[] = []
  const seenFilePaths = new Set<string>()
  for (const f of files) {
    const norm = normalizeFileEntry(f)
    if (rangedPaths.has(norm.path)) {
      // A path that carries any line-range reference travels ENTIRELY through
      // the entries channel — never filePaths, or the backend's cross-dedup
      // would strip the ranged entries (internal/handler/chat.go).
      entries.push(norm)
      continue
    }
    if (!seenFilePaths.has(norm.path)) {
      seenFilePaths.add(norm.path)
      filePaths.push(norm.path)
    }
  }
  return { filePaths, entries }
}

/**
 * Extract the directory portion of a file's `webkitRelativePath`, including the
 * top-level folder (e.g. "src/utils/helper.ts" -> "src/utils", "src/helper.ts" -> "src").
 * Returns '' for files without a relative path (loose drag-drop or single-file picker),
 * which signals a flat upload. Path separators are normalized to '/'.
 */
export function folderRelPath(file: { webkitRelativePath?: string }): string {
  const rel = file?.webkitRelativePath || ''
  if (!rel) return ''
  const normalized = rel.replace(/\\/g, '/')
  const slashIdx = normalized.lastIndexOf('/')
  if (slashIdx <= 0) return ''
  return normalized.slice(0, slashIdx)
}

/** True if the file was picked/dropped as part of a directory (has a relative path). */
export function isDirUploadFile(file: { webkitRelativePath?: string }): boolean {
  return folderRelPath(file) !== ''
}
