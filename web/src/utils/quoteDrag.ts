/**
 * Internal drag-and-drop payload for quoting an OBJECT into the chat.
 *
 * Dragging a git commit, a scheduled task, an issue/PR or a CI run onto the chat
 * column stages it as a quote card — the same result as the "quote" button in
 * those views, but with no annotation and no composer detour. The user is
 * already expressing "put this in the chat", so stopping to ask for a note would
 * be friction.
 *
 * This is deliberately NOT `attachDrag`'s payload: that one carries a file path
 * and ends up in the send's `filePaths`, where an empty path is rejected and
 * 404s the whole send. A quote is addressed by its own locators (commit sha,
 * task id, URL), never by a path.
 *
 * HTML5 DnD only carries a restricted set of types across the app, so a custom
 * MIME holds the JSON. The four drag sources build the payload through the
 * helpers below — one place per item type, so the card's identity cannot drift
 * between the row and whatever else might later quote it.
 */

import type { QuoteData } from '@/composables/useChatContext'
import { buildAttachDragImage } from '@/utils/attachDrag'

export const QUOTE_DRAG_MIME = 'application/x-clawbench-quote'

/**
 * The subset of `QuoteData` a drag carries.
 *
 * Everything optional is omitted when absent rather than written as an empty
 * string/zero: the quote's TYPE is resolved from which locators are present
 * (`resolveQuoteType`), so `taskId: 0` or `commitSha: ''` would not merely be
 * noise — `taskId: 0` is falsy so it happens to be safe, but `commitSha: ''`
 * combined with a url would flip a plain link into a "pipeline". Keeping absent
 * fields absent is what makes the type resolution correct.
 */
export type QuoteDragPayload = QuoteData

/** Write a quote payload into a drag event's dataTransfer. */
export function setQuoteDragData(dt: DataTransfer | null, payload: QuoteDragPayload): boolean {
  if (!dt) return false
  try {
    dt.setData(QUOTE_DRAG_MIME, JSON.stringify(payload))
    // A text/plain fallback keeps the drag meaningful if it is dropped on a
    // plain text target (and makes the drag visible to the browser at all).
    dt.setData('text/plain', payload.filePath || payload.url || '')
    return true
  } catch {
    // dataTransfer may be unavailable in synthetic events — ignore.
    return false
  }
}

/** Whether a drag event carries our internal quote payload. */
export function hasQuoteDragData(dt: DataTransfer | null | undefined): boolean {
  if (!dt) return false
  try {
    return !!dt.types?.includes?.(QUOTE_DRAG_MIME)
  } catch {
    return false
  }
}

/**
 * Read the internal quote payload, or null when this is not an internal quote
 * drag (or the payload is malformed).
 */
export function readQuoteDragData(dt: DataTransfer | null | undefined): QuoteDragPayload | null {
  if (!dt) return null
  try {
    const raw = dt.getData(QUOTE_DRAG_MIME)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const data = parsed as QuoteDragPayload
    // A quote is addressed by SOMETHING; without a label there is nothing to
    // render on the card, and without a locator it could not be jumped to.
    if (typeof data.filePath !== 'string' || !data.filePath) return null
    return data
  } catch {
    // malformed payload — treat as non-internal
    return null
  }
}

/**
 * Build the payload for a git commit.
 *
 * `commitSha` is the locator: it is what the drawer's jump uses to open the git
 * history at that commit. The label pairs the short sha with the subject so the
 * card is recognisable; `language: 'diff'` marks it as a commit quote (a bare
 * sha + url would resolve to 'pipeline').
 */
export function commitDragPayload(commit: { sha: string; msg?: string }): QuoteDragPayload | null {
  const sha = (commit?.sha || '').trim()
  if (!sha) return null
  const short = sha.slice(0, 7)
  const subject = (commit.msg || '').trim()
  return {
    text: '',
    filePath: subject ? `${short} ${subject}` : short,
    language: 'diff',
    startLine: 0,
    endLine: 0,
    sourceKind: 'selection',
    commitSha: sha,
  }
}

/**
 * Build the payload for a scheduled task.
 *
 * `taskId` is the locator (opens the task detail). The label is the task name,
 * which is what the user recognises in the list.
 */
export function taskDragPayload(task: { id: number; name?: string }): QuoteDragPayload | null {
  const id = Number(task?.id)
  if (!id) return null
  const name = (task?.name || '').trim()
  return {
    text: '',
    filePath: name || `#${id}`,
    language: 'task',
    startLine: 0,
    endLine: 0,
    sourceKind: 'selection',
    taskId: id,
  }
}

/**
 * Build the payload for an issue / pull request.
 *
 * `url` is the locator and also the drawer's jump target (the forge object lives
 * outside the app). `language` carries the item type so the card's badge says
 * "issue" vs "PR" rather than a generic link.
 */
export function forgeItemDragPayload(item: {
  type?: 'issue' | 'pr'
  number?: number
  title?: string
  url?: string
  slug?: string
}): QuoteDragPayload | null {
  if (!item) return null
  const label = forgeLabel(item)
  if (!label) return null
  return {
    text: '',
    filePath: label,
    language: item.type === 'pr' ? 'pr' : 'issue',
    startLine: 0,
    endLine: 0,
    sourceKind: 'url',
    ...(item.url ? { url: item.url } : {}),
  }
}

/**
 * Build the payload for a CI pipeline run.
 *
 * Carries BOTH the address and the commit it built, which is what distinguishes
 * a pipeline quote from a plain commit quote. The label names the run rather
 * than using `slug#number`, which would read like a pull-request number.
 */
export function pipelineDragPayload(run: {
  number?: number
  name?: string
  url?: string
  slug?: string
  sha?: string
}): QuoteDragPayload | null {
  if (!run) return null
  const name = (run.name || '').trim()
  const label = name
    ? `${name} #${run.number}`
    : run.slug
      ? `${run.slug} #${run.number}`
      : ''
  if (!label) return null
  const sha = (run.sha || '').trim()
  return {
    text: '',
    filePath: label,
    language: 'pipeline',
    startLine: 0,
    endLine: 0,
    sourceKind: 'url',
    ...(run.url ? { url: run.url } : {}),
    ...(sha ? { commitSha: sha } : {}),
  }
}

/** `owner/repo#123` — the shared label shape for a forge item. */
function forgeLabel(item: { number?: number; slug?: string; title?: string }): string {
  if (item.slug && item.number) return `${item.slug}#${item.number}`
  const title = (item.title || '').trim()
  if (title) return title
  return item.number ? `#${item.number}` : ''
}

/**
 * Build the payload for a row in the forge ACTIVITY list, which mixes issues,
 * PRs and pipelines in one list.
 *
 * Its label is deliberately rebuilt rather than taken from the row: the row's
 * visible text appends the unread REASON ("opened", "pipeline_done", …), which is
 * transient notification state. Quoting it would bake "· merged" into a card
 * that outlives the unread state.
 */
export function forgeUnreadDragPayload(row: {
  type?: 'issue' | 'pr' | 'pipeline'
  number?: number
  runId?: number
  url?: string
  slug?: string
}): QuoteDragPayload | null {
  if (!row) return null
  const slug = (row.slug || '').trim()
  if (row.type === 'pipeline') {
    // A pipeline is addressed by its run id; its number is always 0.
    const label = slug ? `${slug} #${row.runId}` : `#${row.runId}`
    if (!row.runId && !slug) return null
    return {
      text: '',
      filePath: label,
      language: 'pipeline',
      startLine: 0,
      endLine: 0,
      sourceKind: 'url',
      ...(row.url ? { url: row.url } : {}),
    }
  }
  const label = slug && row.number ? `${slug}#${row.number}` : ''
  if (!label) return null
  return {
    text: '',
    filePath: label,
    language: row.type === 'pr' ? 'pr' : 'issue',
    startLine: 0,
    endLine: 0,
    sourceKind: 'url',
    ...(row.url ? { url: row.url } : {}),
  }
}

/**
 * Begin an internal quote drag: write the payload, set effectAllowed and install
 * the drag ghost.
 *
 * The ghost is the file-manager one, reused rather than duplicated — a second
 * ghost implementation would drift visually. Callers must call
 * `cleanupDragGhost()` from their `dragend`.
 *
 * Returns false when the event carries no dataTransfer (synthetic events) or the
 * payload is unusable, so the caller can skip the drag entirely instead of
 * starting one that would drop nothing.
 */
export function startQuoteDrag(e: DragEvent, payload: QuoteDragPayload | null): boolean {
  const dt = e.dataTransfer
  if (!dt || !payload) return false
  if (!setQuoteDragData(dt, payload)) return false
  dt.effectAllowed = 'copy'
  dt.setDragImage(buildAttachDragImage(payload.filePath, false), 14, 16)
  return true
}
