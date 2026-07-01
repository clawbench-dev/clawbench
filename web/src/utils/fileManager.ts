/**
 * Pure utility functions extracted from FileManagerContent.vue.
 * These are independent of Vue reactivity and can be tested in isolation.
 */

import { reactive } from 'vue'
import { getFileType } from './fileType'
import { joinPath } from './path'

// ── View mode persistence ──

export type ViewMode = 'list' | 'grid'
const VIEW_MODE_KEY = 'clawbench-file-view'

export function loadViewMode(): ViewMode {
  const stored = localStorage.getItem(VIEW_MODE_KEY)
  if (stored === 'list' || stored === 'grid') return stored
  return 'list'
}

export function saveViewMode(mode: ViewMode): void {
  localStorage.setItem(VIEW_MODE_KEY, mode)
}

export { VIEW_MODE_KEY }

// ── Thumbnail URL generation ──

export function buildThumbUrl(currentDir: string, entryName: string, width = 200): string {
  const path = joinPath(currentDir, entryName)
  return `/api/file/thumb?path=${encodeURIComponent(path)}&w=${width}`
}

// ── File type detection helpers ──

export function isImage(entry: { type: string; name: string }): boolean {
  return entry.type === 'image' || !!getFileType(entry.name).isImage
}

export function isAudio(entry: { type: string; name: string }): boolean {
  return !!getFileType(entry.name).isAudio
}

export function isVideo(entry: { type: string; name: string }): boolean {
  return !!getFileType(entry.name).isVideo
}

// ── Thumbnail eligibility ──
// Extensions that the backend thumbnail API can decode (Go stdlib: png, jpg, gif).
// SVG, WebP, AVIF, PDF, BMP, TIFF are excluded — they'll cause a 404 round-trip if attempted.

export const THUMBABLE_EXTS = new Set(['.png', '.jpg', '.jpeg', '.gif'])

export function isThumbable(entry: { type: string; name: string }): boolean {
  if (entry.type !== 'image' && entry.type !== 'file') return false
  const name = entry.name.toLowerCase()
  for (const ext of THUMBABLE_EXTS) {
    if (name.endsWith(ext)) return true
  }
  return false
}

/** Check if a file path is thumbable based on its extension (path-based variant of isThumbable). */
export function isThumbableExt(path: string): boolean {
  const lower = path.toLowerCase()
  for (const ext of THUMBABLE_EXTS) {
    if (lower.endsWith(ext)) return true
  }
  return false
}

// ── File size formatting ──

export function formatSize(size: number | null | undefined): string {
  if (size == null) return ''
  if (size < 1024) return size + ' B'
  if (size < 1024 * 1024) return (size / 1024).toFixed(1) + ' K'
  return (size / (1024 * 1024)).toFixed(1) + ' M'
}

// ── Multi-select state factory ──

export interface MultiSelectState {
  active: boolean
  selected: Set<string>
}

export function createMultiSelect() {
  const state = reactive<MultiSelectState>({
    active: false,
    selected: new Set<string>(),
  })

  function enterMultiSelect() {
    state.active = true
    state.selected.clear()
  }

  function exitMultiSelect() {
    state.active = false
    state.selected.clear()
  }

  function toggleSelect(path: string) {
    if (state.selected.has(path)) {
      state.selected.delete(path)
    } else {
      state.selected.add(path)
    }
  }

  return { state, enterMultiSelect, exitMultiSelect, toggleSelect }
}

// ── Clipboard state factory ──

export interface ClipboardEntry {
  name: string
  path: string
  type: string
}

export interface ClipboardState {
  entries: ClipboardEntry[]
  isCut: boolean
}

export function createClipboard() {
  const clipboard = reactive<ClipboardState>({ entries: [], isCut: false })

  function copy(entries: ClipboardEntry[]) {
    clipboard.entries = entries
    clipboard.isCut = false
  }

  function cut(entries: ClipboardEntry[]) {
    clipboard.entries = entries
    clipboard.isCut = true
  }

  function clear() {
    clipboard.entries = []
    clipboard.isCut = false
  }

  return { clipboard, copy, cut, clear }
}

// ── Click handler logic (pure function) ──

export interface ClickResult {
  action: 'navigate' | 'select' | 'toggle'
  path: string
}

export function resolveClickAction(
  multiSelectActive: boolean,
  itemType: string,
  path: string,
): ClickResult {
  if (multiSelectActive) {
    return { action: 'toggle', path }
  }
  if (itemType === 'dir') {
    return { action: 'navigate', path }
  }
  return { action: 'select', path }
}
