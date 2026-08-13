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

import { folderRelPath } from '@/utils/fileAttachmentUtils'

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
): Promise<void> {
  if (entry.isFile) {
    const file = await fileFromEntry(entry as FileSystemFileEntry)
    files.push({ file, relPath: dirOf(entry.fullPath || '') })
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
      await walkEntry(e, files, emptyDirs)
    }
  }
}

/**
 * Expand a drop into files + empty directories.
 * Prefers `webkitGetAsEntry` traversal when available; otherwise falls back to
 * `dataTransfer.files` (the previous flat behavior).
 */
export async function expandDataTransfer(dataTransfer: DataTransfer): Promise<ExpandResult> {
  const files: DropFile[] = []
  const emptyDirs: string[] = []
  const items = dataTransfer?.items
  let handled = false

  if (items && items.length) {
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (typeof item.webkitGetAsEntry === 'function') {
        const entry = item.webkitGetAsEntry()
        if (entry) {
          handled = true
          await walkEntry(entry, files, emptyDirs)
        }
      }
    }
  }

  if (!handled) {
    for (const file of Array.from(dataTransfer?.files || [])) {
      files.push({ file, relPath: dirOf(file.webkitRelativePath || '') || folderRelPath(file) })
    }
  }

  return { files, emptyDirs }
}
