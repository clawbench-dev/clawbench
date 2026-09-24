/**
 * Pure functions extracted from FileAttachmentList.vue for testability.
 */

/** FileEntry represents a file, directory, external URL, or quoted snippet.
 *
 * `kind` distinguishes three shapes:
 * - "file" (default): a local path;
 * - "url": an external address in `url`, never treated as a filesystem path by
 *   the backend;
 * - "quote": a quoted snippet — its payload is `text` + `note`, and `path` is
 *   only a human-readable label (empty for a quote taken from a chat message),
 *   so it is likewise never resolved as a path. */
export interface FileEntry {
  path: string
  isDir?: boolean
  startLine?: number
  endLine?: number
  kind?: 'file' | 'url' | 'quote'
  url?: string
  /** Quote-only: stable identity, used to edit the annotation after sending. */
  id?: string
  /** Quote-only: the quoted content, verbatim. */
  text?: string
  /** Quote-only: the user's annotation. */
  note?: string
  /** Quote-only: fence language (e.g. "go", "issue"). */
  language?: string
  /**
   * Quote-only: where the quote came from ('file' | 'url' | 'message' |
   * 'selection'). Persisted so the detail drawer can label a sent quote
   * correctly after a reload — inference alone cannot distinguish a terminal
   * quote from a chat quote (both carry no url and no path).
   */
  sourceKind?: 'file' | 'url' | 'message' | 'selection'
  /**
   * Quote-only source locators. Each is a MACHINE-readable key that both the
   * jump handler and the AI prompt use to find the origin; the human-readable
   * name goes in `path` (which is already documented as a label, not a path).
   *
   * They are persisted rather than re-derived because the quoted text carries
   * no trace of them: after a reload nothing else identifies which commit,
   * task or message a sent quote came from.
   */
  /** Commit SHA a git-history or CI-pipeline quote came from. */
  commitSha?: string
  /** Scheduled task id a task quote came from. */
  taskId?: number
  /** Chat session id a message/session quote came from. */
  sessionId?: string
  /** DB chat-message id the quote was taken from (for scroll-to-message). */
  messageId?: number
  /** Task execution id, when the quote came from one run's detail view. */
  executionId?: string
}

/** Whether an entry is a quoted snippet rather than a file or URL. */
export function isQuoteEntry(f: FileEntry): boolean {
  return f.kind === 'quote'
}

/** Whether an entry is an external URL rather than a local path. */
export function isUrlEntry(f: FileEntry): boolean {
  return f.kind === 'url' && !!f.url
}

/**
 * Whether a URL is safe to bind to an anchor href.
 *
 * URL entries are persisted and re-rendered after a reload, so the address is
 * data rather than something the user just typed. Only http(s) is allowed:
 * `javascript:`/`data:` in an href would execute on click. Anything else is
 * rendered as inert text instead of a link.
 */
export function isSafeExternalUrl(url: string | undefined): boolean {
  if (!url) return false
  try {
    const parsed = new URL(url, window.location.origin)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

/** Normalize a file entry to FileEntry format.
 *  Backend returns FileEntry[] (new) or string[] (legacy), local push uses [{path: "..."}]. */
export function normalizeFileEntry(f: string | FileEntry): FileEntry {
  if (typeof f === 'string') return { path: f, isDir: false }
  return {
    path: f.path || '',
    isDir: f.isDir ?? false,
    startLine: f.startLine,
    endLine: f.endLine,
    // Preserve URL attachments: dropping kind/url here would silently turn a
    // URL into a bogus local path.
    ...(f.kind ? { kind: f.kind } : {}),
    ...(f.url ? { url: f.url } : {}),
    // Quote payloads must survive too. This function runs on EVERY render and
    // dedupe pass, so dropping them here loses the quoted text and annotation
    // silently — the card would render blank and the AI would receive nothing.
    ...(f.id ? { id: f.id } : {}),
    ...(f.text !== undefined ? { text: f.text } : {}),
    ...(f.note !== undefined ? { note: f.note } : {}),
    ...(f.language ? { language: f.language } : {}),
    // The source kind decides how the drawer labels the quote. Dropping it here
    // (this runs on every render and dedupe pass) silently degrades a terminal
    // quote to a "chat message" label after a reload.
    ...(f.sourceKind ? { sourceKind: f.sourceKind } : {}),
    // Source locators. Same hazard as above, one step worse: these are the ONLY
    // way back to the commit/task/message, so dropping one here silently makes
    // the quote unjumpable with no visible symptom until the user clicks.
    ...(f.commitSha ? { commitSha: f.commitSha } : {}),
    ...(f.taskId ? { taskId: f.taskId } : {}),
    ...(f.sessionId ? { sessionId: f.sessionId } : {}),
    ...(f.messageId ? { messageId: f.messageId } : {}),
    ...(f.executionId ? { executionId: f.executionId } : {}),
  }
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
  // URL entries dedupe by their address; local entries by path + line range.
  if (isUrlEntry(f)) return `url|${f.url}`
  // Quotes dedupe by their stable id. Two quotes of the SAME range with
  // different text/notes are genuinely different attachments and must both
  // survive, so path+range would be wrong here.
  if (isQuoteEntry(f)) return `quote|${f.id ?? ''}|${f.path}|${f.startLine ?? 0}|${f.endLine ?? 0}|${f.text ?? ''}`
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
 * Build the attachment payload for a send from the two attachment sources.
 *
 * `uploaded` are in-flight uploads (path only); `attached` are context
 * attachments, which may carry line ranges OR be URL entries (kind='url').
 * Entries must be spread through unchanged — rebuilding them field-by-field
 * drops kind/url and turns a URL into a bogus local path that the backend
 * rejects with 404.
 *
 * Returns the deduped entry list plus the legacy filePaths channel.
 */
export function buildSendPayload(
  uploaded: FileEntry[],
  attached: FileEntry[],
): { allFiles: FileEntry[]; filePaths: string[] } {
  const uploadedEntries = uploaded.filter(f => f.path).map(f => ({ path: f.path, isDir: false }))
  const attachedEntries = attached.map(f => ({ ...f, isDir: f.isDir ?? false }))
  const allFiles = dedupeFiles([...uploadedEntries, ...attachedEntries])
  const { filePaths } = buildSendChannels(attachedEntries)
  return { allFiles, filePaths }
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
  // URL entries are not filesystem paths: they always travel through the
  // entries channel so the backend sees kind/url and skips path resolution.
  const rangedPaths = new Set(files.filter(f => !isUrlEntry(f) && !isQuoteEntry(f) && f.startLine !== undefined).map(f => f.path))
  const entries: FileEntry[] = []
  const filePaths: string[] = []
  const seenFilePaths = new Set<string>()
  for (const f of files) {
    const norm = normalizeFileEntry(f)
    // Quotes must be routed to the entries channel BEFORE the filePaths
    // fallback below. A chat-message quote has path === '' and no line range,
    // so it would otherwise be pushed as an empty string into filePaths, and
    // the backend would reject the whole send resolving "" as a path.
    if (isQuoteEntry(norm)) {
      entries.push(norm)
      continue
    }
    if (isUrlEntry(norm)) {
      entries.push(norm)
      continue
    }
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
