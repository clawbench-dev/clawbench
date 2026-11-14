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

export function statusKind(doc: KBDocument): StatusKind {
  if (doc.status === 'failed') return 'failed'
  if (doc.status === 'pending') return 'pending'
  if (doc.status === 'extracting') return 'working'
  if (!doc.vector_ready) return 'fts'
  return 'ready'
}

export function statusLabel(doc: KBDocument, t: TFn): string {
  if (doc.status === 'failed') return t('kb.statusFailed')
  if (doc.status === 'pending') return t('kb.statusPending')
  if (doc.status === 'extracting') return t('kb.statusExtracting')
  if (!doc.vector_ready) return t('kb.statusReadyFts')
  return t('kb.statusReady')
}

export function formatSize(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
