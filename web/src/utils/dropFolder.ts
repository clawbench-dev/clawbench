/**
 * Folder drop expansion.
 *
 * When a user drags a folder into ClawBench, `dataTransfer.files` only yields
 * a single empty File named after the folder — the nested files and empty
 * subdirectories are invisible. This module uses `webkitGetAsEntry()` to
 * recursively traverse dropped directory entries, producing:
 *   - every file paired with its directory-relative path, and
 *   - the relative paths of empty directories (which `webkitdirectory` can
 *     never report).
 */

export interface DropFile {
  file: File
  /** Directory portion relative to the drop target, e.g. "新建文件夹/src" ('' for loose files). */
  relPath: string
}

export interface ExpandResult {
  files: DropFile[]
  /** Relative paths of empty directories found in the drop, e.g. "新建文件夹/empty". */
  emptyDirs: string[]
}

/** Normalize a slash path: strip leading/trailing slashes and return '' for root. */
function normRelPath(p: string): string {
  const s = p.replace(/\\/g, '/').replace(/^\/+/, '').replace(/\/+$/, '')
  return s === '.' ? '' : s
}

/** Directory portion of a file's fullPath ('' when the file sits at the drop root). */
function dirOf(fullPath: string): string {
  const norm = fullPath.replace(/\\/g, '/')
  const i = norm.lastIndexOf('/')
  if (i <= 0) return ''
  return normRelPath(norm.slice(0, i))
}

function fileFromEntry(entry: FileSystemFileEntry): Promise<File> {
  return new Promise((resolve, reject) => entry.file(resolve, reject))
}

function readAllEntries(reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> {
  return new Promise((resolve, reject) => {
    const all: FileSystemEntry[] = []
    const next = () => {
      reader.readEntries((batch) => {
        if (batch.length === 0) {
          resolve(all)
          return
        }
        all.push(...batch)
        next()
      }, reject)
    }
    next()
  })
}

async function walkEntry(
  entry: FileSystemEntry,
  files: DropFile[],
  emptyDirs: string[],
  addFile: (file: File, relPath: string) => void,
): Promise<void> {
  if (entry.isFile) {
    const file = await fileFromEntry(entry as FileSystemFileEntry)
    addFile(file, dirOf(entry.fullPath || ''))
    return
  }
  if (entry.isDirectory) {
    const dirEntry = entry as FileSystemDirectoryEntry
    const entries = await readAllEntries(dirEntry.createReader())
    if (entries.length === 0) {
      emptyDirs.push(normRelPath(entry.fullPath || ''))
      return
    }
    for (const e of entries) {
      await walkEntry(e, files, emptyDirs, addFile)
    }
  }
}

/**
 * Expand a drop into files + empty directories.
 *
 * `webkitGetAsEntry` traversal handles folders (their nested files and empty
 * subdirectories are invisible to `dataTransfer.files`). However, in real
 * browser/WebView drops of *multiple loose files* the `items` list may only
 * expose part of the selection while `dataTransfer.files` carries the complete
 * list. Using one source exclusively drops the other, so both are merged:
 *   - items entries are distinct dragged things → collected as-is, never deduped;
 *   - `dataTransfer.files` entries that were already gathered from the items
 *     traversal are skipped (same physical file appears in both sources).
 */
export async function expandDataTransfer(dataTransfer: DataTransfer): Promise<ExpandResult> {
  const files: DropFile[] = []
  const emptyDirs: string[] = []
  const seen = new Set<string>()
  const topDirs = new Set<string>()
  const items = dataTransfer?.items

  const keyOf = (file: File, relPath: string) => `${relPath}/${file.name}`

  if (items && items.length) {
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (typeof item.webkitGetAsEntry === 'function') {
        const entry = item.webkitGetAsEntry()
        if (entry) {
          if (entry.isDirectory) topDirs.add(entry.name)
          await walkEntry(entry, files, emptyDirs, (file, relPath) => {
            files.push({ file, relPath })
            seen.add(keyOf(file, relPath))
          })
        }
      }
    }
  }

  // dataTransfer.files always lists the full selection. Skip folder placeholders
  // (a 0-byte File named after a dropped directory that Chrome/Electron include)
  // and entries already gathered from the items traversal.
  for (const file of Array.from(dataTransfer?.files || [])) {
    if (file.size === 0 && topDirs.has(file.name)) continue
    const rel = dirOf(file.webkitRelativePath || '')
    if (seen.has(keyOf(file, rel))) continue
    files.push({ file, relPath: rel })
  }

  return { files, emptyDirs }
}
