/**
 * The single answer to "how does this file preview?".
 *
 * Three surfaces render files (the full-screen viewer, the share page and the
 * quick preview card). They used to each carry their own if/else chain over
 * `getFileType()` flags, so adding a type in one place silently left the other
 * two rendering it as raw text. This module is the classification those
 * surfaces share: `getFileType` says what a file IS, this says which body
 * renders it.
 *
 * `filePreviewKind` is deliberately extension/subtype only — no I/O, no
 * component state. The quick preview layers its own concerns (directory
 * targets, binary/too-large errors) on top.
 */
import { getFileType } from './fileType'

export type FilePreviewKind =
  | 'dir'
  | 'markdown'
  | 'html'
  | 'table'
  | 'office'
  | 'excalidraw'
  | 'drawio'
  | 'openapi'
  | 'image'
  | 'video'
  | 'audio'
  | 'pdf'
  | 'text'

/** Every kind the quick preview can render. The type-coverage guard iterates
 *  this list, so a new kind cannot ship without a body assertion. */
export const FILE_PREVIEW_KINDS: readonly FilePreviewKind[] = [
  'dir',
  'markdown',
  'html',
  'table',
  'office',
  'excalidraw',
  'drawio',
  'openapi',
  'image',
  'video',
  'audio',
  'pdf',
  'text',
]

/**
 * Every renderer flag on `FileType`, paired with the preview kind it must
 * produce. The drift guard iterates this table: a new `isX` flag on FileType
 * without an entry here fails the guard instead of silently falling back to
 * raw text in the quick preview.
 */
export const FILETYPE_FLAG_KINDS = {
    isMarkdown: 'markdown',
    isHtml: 'html',
    isTable: 'table',
    isOffice: 'office',
    isExcalidraw: 'excalidraw',
    isDrawio: 'drawio',
    isImage: 'image',
    isVideo: 'video',
    isAudio: 'audio',
    isPdf: 'pdf',
} as const satisfies Record<string, FilePreviewKind>

/**
 * Classify a file by name (and the backend's content subtype, when known).
 *
 * `subtype` wins over the extension because the backend is authoritative: a
 * `.json` file can be an OpenAPI spec, and a diagram's extension alone already
 * decides its subtype server-side.
 */
export function filePreviewKind(name: string, subtype?: string): FilePreviewKind {
    if (subtype === 'openapi') return 'openapi'

    const fileType = getFileType(name || '')
    if (fileType.isMarkdown) return 'markdown'
    if (fileType.isHtml) return 'html'
    if (fileType.isTable) return 'table'
    if (fileType.isOffice) return 'office'
    if (fileType.isExcalidraw || subtype === 'excalidraw') return 'excalidraw'
    if (fileType.isDrawio || subtype === 'drawio') return 'drawio'
    if (fileType.isImage) return 'image'
    if (fileType.isVideo) return 'video'
    if (fileType.isAudio) return 'audio'
    if (fileType.isPdf) return 'pdf'
    return 'text'
}

/** True for kinds that can switch between a rendered body and its source. */
export function kindHasRenderedView(kind: FilePreviewKind): boolean {
    return kind === 'markdown' || kind === 'html' || kind === 'table' || kind === 'openapi'
}
