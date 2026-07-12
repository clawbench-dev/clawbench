/**
 * Shared file icon helpers.
 * Centralised here so AttachDrawer, ChatInputBar, FileAttachmentList,
 * and FileManagerContent all use the same icon/color mapping.
 */

import { FileText, FileImage, FileVideo, FileMusic, Folder } from 'lucide-vue-next'
import { getFileType } from './fileType'

/** Lucide component to use for a given file path. */
export function getFileIcon(path: string) {
  const ft = getFileType(path)
  if (ft.isImage) return FileImage
  if (ft.isAudio) return FileMusic
  if (ft.isVideo) return FileVideo
  return FileText
}

/** Per-file-type accent colour for the icon. */
export function getFileIconColor(path: string): string | undefined {
  const ft = getFileType(path)
  if (ft.isImage) return '#a855f7'
  if (ft.isAudio) return '#22c55e'
  if (ft.isVideo) return '#ef4444'
  return ft.color
}

/** Build a thumbnail URL for an absolute file path. */
export function buildPathThumbUrl(path: string, width = 80): string {
  return `/api/file/thumb?path=${encodeURIComponent(path)}&w=${width}`
}

/** Re-export Lucide icon components for convenience. */
export { FileText, FileImage, FileVideo, FileMusic, Folder }
