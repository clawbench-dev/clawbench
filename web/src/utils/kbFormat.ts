/**
 * Presentation helpers for the knowledge base panel.
 *
 * The server deliberately returns language-neutral location labels (`p.12`,
 * `sheet 报价 · rows 1-50`, `¶1-¶6`) so the API and the AI prompt do not depend
 * on the UI language. These helpers turn them into the current locale's wording.
 */
import type { KBDocument } from '@/composables/useKnowledgeBase'

/** Minimal translate signature, so this module stays a leaf (no vue-i18n import). */
export type TFn = (key: string, named?: Record<string, unknown>) => string

export function formatLocation(loc: string | undefined, t: TFn): string {
  if (!loc) return ''
  let m = /^p\.(\d+)$/.exec(loc)
  if (m) return t('kb.locationPage', { n: m[1] })
  m = /^slide (\d+)$/.exec(loc)
  if (m) return t('kb.locationSlide', { n: m[1] })
  m = /^sheet (.+) · row (\d+)$/.exec(loc)
  if (m) return t('kb.locationSheetRow', { name: m[1], n: m[2] })
  m = /^sheet (.+) · rows (\d+)-(\d+)$/.exec(loc)
  if (m) return t('kb.locationSheetRows', { name: m[1], start: m[2], end: m[3] })
  m = /^L(\d+)-L(\d+)$/.exec(loc)
  if (m) return t('kb.locationLines', { start: m[1], end: m[2] })
  m = /^L(\d+)$/.exec(loc)
  if (m) return t('kb.locationLine', { n: m[1] })
  m = /^¶(\d+)-¶(\d+)$/.exec(loc)
  if (m) return t('kb.locationParagraphs', { start: m[1], end: m[2] })
  m = /^¶(\d+)$/.exec(loc)
  if (m) return t('kb.locationParagraph', { n: m[1] })
  if (loc === 'title') return t('kb.locationTitle')
  return loc
}

export type StatusKind = 'failed' | 'pending' | 'working' | 'fts' | 'ready'

/**
 * `vectorOn` tells whether the semantic index is configured at all. When it is
 * not, `vector_ready` never becomes true, so a "ready but un-vector" document
 * would otherwise be labelled "indexing" forever.
 */
export function statusKind(doc: KBDocument, vectorOn = true): StatusKind {
  if (doc.status === 'failed') return 'failed'
  if (doc.status === 'pending') return 'pending'
  if (doc.status === 'extracting') return 'working'
  if (vectorOn && !doc.vector_ready) return 'fts'
  return 'ready'
}

export function statusLabel(doc: KBDocument, t: TFn, vectorOn = true): string {
  if (doc.status === 'failed') return t('kb.statusFailed')
  if (doc.status === 'pending') return t('kb.statusPending')
  if (doc.status === 'extracting') return t('kb.statusExtracting')
  if (vectorOn && !doc.vector_ready) return t('kb.statusReadyFts')
  return t('kb.statusReady')
}

export function formatSize(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/**
 * Stable failure reasons produced by the extractor (`FileError.Error()` is
 * `code` or `code: detail`). The indexer stores that string as-is, so the code
 * can be recovered without a schema change; unknown errors fall back to the
 * raw message instead of pretending to understand them.
 */
const KB_ERROR_REASONS = new Set([
  'legacy_doc',
  'legacy_xls',
  'legacy_ppt',
  'image',
  'svg',
  'audio_video',
  'archive',
  'executable',
  'binary',
  'font',
  'pdf_encrypted',
  'pdf_no_text',
  'no_text',
  'file_too_large',
  'not_regular_file',
  'unknown_format',
])

export function errorReason(error: string | undefined): string {
  if (!error) return ''
  const code = error.split(':', 1)[0].trim()
  return KB_ERROR_REASONS.has(code) ? code : ''
}

/** Localized failure text, falling back to the server's raw message. */
export function documentErrorText(error: string | undefined, t: TFn): string {
  if (!error) return ''
  const reason = errorReason(error)
  return reason ? t(`kb.reason.${reason}`) : error
}

/**
 * One sentence explaining why a folder import skipped files. The server counts
 * them by reason; the dominant one is the one worth naming (a folder of legacy
 * .doc should say "save as .docx", not just "3 skipped"). Unknown codes are
 * ignored so a future reason cannot leak an i18n key to the user.
 */
export function skipReasonHint(
  reasons: Record<string, number> | undefined,
  t: TFn,
): string {
  if (!reasons) return ''
  let best = ''
  let bestCount = 0
  for (const [code, count] of Object.entries(reasons)) {
    if (count > bestCount && KB_ERROR_REASONS.has(code)) {
      best = code
      bestCount = count
    }
  }
  return best ? t('kb.skippedReasonHint', { count: bestCount, reason: t(`kb.reason.${best}`) }) : ''
}
