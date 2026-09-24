import { ref } from 'vue'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

export interface QuoteData {
  text: string
  filePath: string
  language: string
  startLine: number
  endLine: number
  /** External address when the quote came from a forge object. */
  url?: string
  /** Where the quote came from; drives the drawer's jump affordance. */
  sourceKind?: 'file' | 'url' | 'message' | 'selection'
  /** DB message id when the quote was taken from a chat message. */
  messageId?: number
  /** Commit SHA when the quote came from a git-history or CI-pipeline view. */
  commitSha?: string
  /** Scheduled task id when the quote came from a task view. */
  taskId?: number
  /** Chat session id when the quote came from a chat message. */
  sessionId?: string
  /** Task execution id when the quote came from one run's detail view. */
  executionId?: string
}

export interface StagedQuote extends QuoteData {
  id: string
  note: string
}

// ───────────────────────────────────────────────────────────
// Module-level singleton state — shared across the whole app.
// useChatContext unifies "context sent to chat" from any tab:
//   - attachedFiles: files to include as context
//   - quoteData: code selection referenced from file preview
// ───────────────────────────────────────────────────────────

const attachedFiles = ref<FileEntry[]>([])
const quoteData = ref<QuoteData | null>(null)
const stagedQuotes = ref<StagedQuote[]>([])
let quoteId = 0

// ── Per-session attachment draft ──
// Mirrors ChatInputBar's draftCache for text: when switching sessions, the
// current session's attached files + staged quotes are snapshotted here and
// restored when the user switches back. Without this, attachments vanish on
// session switch while the typed text survives (see ChatInputBar draftCache).
interface AttachmentSnapshot {
  files: FileEntry[]
  quotes: StagedQuote[]
  quote: QuoteData | null
}

const attachmentDrafts = new Map<string, AttachmentSnapshot>()

/**
 * Identity of an attached file entry. A path may carry MULTIPLE independent
 * references: a plain whole-file attach (no lines), and one or more distinct
 * line ranges (e.g. quoting a Mermaid diagram's source fence). The composite
 * key (path, startLine ?? 0, endLine ?? 0) keeps them separate, so attaching a
 * diagram range to a file that is already attached as a whole keeps both.
 */
function sameEntry(a: FileEntry, b: FileEntry): boolean {
  // URL attachments are identified by their address, not a path.
  if (a.kind === 'url' || b.kind === 'url') return a.kind === b.kind && a.url === b.url
  return a.path === b.path
    && (a.startLine ?? 0) === (b.startLine ?? 0)
    && (a.endLine ?? 0) === (b.endLine ?? 0)
}

/**
 * Attach a file entry. Returns whether it was actually added — false when the
 * path is empty or the exact same entry is already attached. Callers that
 * report the outcome to the user (a multi-item drop) need this to distinguish
 * "added" from "already there".
 */
function addAttachedFile(path: string, isDir: boolean = false, startLine?: number, endLine?: number): boolean {
  if (!path) return false
  const candidate: FileEntry = { path, isDir, startLine, endLine }
  if (attachedFiles.value.some(f => sameEntry(f, candidate))) return false
  attachedFiles.value.push(candidate)
  return true
}

/**
 * Attach an external URL (e.g. a GitHub issue or PR) to the chat context.
 *
 * The entry carries kind="url" so the backend never tries to resolve it as a
 * local path. `label` is the chip text (e.g. "owner/repo#123"); the URL itself
 * is what gets sent.
 */
function addUrlAttachment(url: string, label: string) {
  if (!url) return
  const candidate: FileEntry = { path: label || url, kind: 'url', url }
  if (attachedFiles.value.some(f => f.kind === 'url' && f.url === url)) return
  attachedFiles.value.push(candidate)
}

function removeAttachedFile(index: number) {
  attachedFiles.value.splice(index, 1)
}

/**
 * Remove an attached file entry.
 * With a line range, only the entry whose range matches exactly is removed
 * (a whole-file entry for the same path stays). Without lines, the first
 * path match is removed (legacy whole-file semantics; a path shown as
 * "attached" via hasAttachedFile(path) is detached here even when what is
 * attached is actually a diagram line-range).
 */
function removeAttachedFileByPath(path: string, startLine?: number, endLine?: number) {
  if (!path) return
  const idx = attachedFiles.value.findIndex(f =>
    startLine === undefined
      ? f.path === path
      : sameEntry(f, { path, startLine, endLine }))
  if (idx >= 0) attachedFiles.value.splice(idx, 1)
}

function toggleAttachedFile(path: string, isDir: boolean = false) {
  if (!path) return
  const idx = attachedFiles.value.findIndex(f => f.path === path)
  if (idx >= 0) {
    attachedFiles.value.splice(idx, 1)
  } else {
    attachedFiles.value.push({ path, isDir })
  }
}

/**
 * Whether a path (or a specific line range of it) is attached.
 * With a range, only an exact range match counts; without, any entry for the
 * path counts (a whole-file / image attach remains truthy when only a diagram
 * range is attached).
 */
function hasAttachedFile(path: string, startLine?: number, endLine?: number): boolean {
  return attachedFiles.value.some(f =>
    startLine === undefined
      ? f.path === path
      : sameEntry(f, { path, startLine, endLine }))
}

function setQuoteData(data: QuoteData | null) {
  quoteData.value = data
}

function sameQuote(a: QuoteData, b: QuoteData): boolean {
  // messageId is part of the identity: quoting the same sentence out of two
  // different chat messages produces two genuinely different quotes, and
  // collapsing them would silently keep only the first.
  return a.filePath === b.filePath
    && a.startLine === b.startLine
    && a.endLine === b.endLine
    && a.text === b.text
    && (a.messageId ?? 0) === (b.messageId ?? 0)
}

function addStagedQuote(data: QuoteData, note = ''): StagedQuote {
  const normalizedNote = note.trim()
  const existing = stagedQuotes.value.find(item => sameQuote(item, data))
  if (existing) {
    if (normalizedNote) existing.note = normalizedNote
    return existing
  }

  const item: StagedQuote = {
    ...data,
    id: `quote-${Date.now()}-${++quoteId}`,
    note: normalizedNote,
  }
  stagedQuotes.value.push(item)
  return item
}

function removeStagedQuote(id: string) {
  const index = stagedQuotes.value.findIndex(item => item.id === id)
  if (index >= 0) stagedQuotes.value.splice(index, 1)
}

/**
 * Replace the annotation on a staged quote. No-op when the id is unknown.
 * Used by the quote detail drawer while the quote is still un-sent.
 */
function updateStagedQuoteNote(id: string, note: string) {
  const item = stagedQuotes.value.find(q => q.id === id)
  if (item) item.note = note.trim()
}

function clearQuotes() {
  quoteData.value = null
  stagedQuotes.value = []
}

function clearAll() {
  attachedFiles.value = []
  clearQuotes()
}

/**
 * Snapshot the current input attachments + staged quotes under a session id.
 * Called before switching away — the snapshot is restored when the user
 * switches back to that session.
 */
function snapshotAttachments(sessionId: string) {
  if (!sessionId) return
  attachmentDrafts.set(sessionId, {
    files: attachedFiles.value.map(f => ({ ...f })),
    quotes: stagedQuotes.value.map(q => ({ ...q })),
    quote: quoteData.value ? { ...quoteData.value } : null,
  })
}

/**
 * Restore a previously snapshotted attachment draft for the given session.
 * Overwrites the current input state (call right after switching in).
 */
function restoreAttachments(sessionId: string) {
  if (!sessionId) return
  const snap = attachmentDrafts.get(sessionId)
  if (!snap) return
  attachedFiles.value = snap.files.map(f => ({ ...f }))
  stagedQuotes.value = snap.quotes.map(q => ({ ...q }))
  quoteData.value = snap.quote ? { ...snap.quote } : null
}

/** Drop the attachment draft for a session (e.g. when the session is destroyed). */
function discardAttachmentDraft(sessionId: string) {
  if (sessionId) attachmentDrafts.delete(sessionId)
}

export function useChatContext() {
  return {
    attachedFiles,
    quoteData,
    stagedQuotes,
    addAttachedFile,
    addUrlAttachment,
    removeAttachedFile,
    removeAttachedFileByPath,
    toggleAttachedFile,
    hasAttachedFile,
    setQuoteData,
    addStagedQuote,
    removeStagedQuote,
    updateStagedQuoteNote,
    clearQuotes,
    clearAll,
    snapshotAttachments,
    restoreAttachments,
    discardAttachmentDraft,
  }
}
