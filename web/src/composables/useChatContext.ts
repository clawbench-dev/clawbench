import { ref } from 'vue'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

export interface QuoteData {
  text: string
  filePath: string
  language: string
  startLine: number
  endLine: number
}

// ───────────────────────────────────────────────────────────
// Module-level singleton state — shared across the whole app.
// useChatContext unifies "context sent to chat" from any tab:
//   - attachedFiles: files to include as context
//   - quoteData: code selection referenced from file preview
// ───────────────────────────────────────────────────────────

const attachedFiles = ref<FileEntry[]>([])
const quoteData = ref<QuoteData | null>(null)

function addAttachedFile(path: string, isDir: boolean = false, startLine?: number, endLine?: number) {
  if (path && !attachedFiles.value.some(f => f.path === path)) {
    attachedFiles.value.push({ path, isDir, startLine, endLine })
  }
}

function removeAttachedFile(index: number) {
  attachedFiles.value.splice(index, 1)
}

function removeAttachedFileByPath(path: string) {
  const idx = attachedFiles.value.findIndex(f => f.path === path)
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

function hasAttachedFile(path: string): boolean {
  return attachedFiles.value.some(f => f.path === path)
}

function setQuoteData(data: QuoteData | null) {
  quoteData.value = data
}

function clearAll() {
  attachedFiles.value = []
  quoteData.value = null
}

export function useChatContext() {
  return {
    attachedFiles,
    quoteData,
    addAttachedFile,
    removeAttachedFile,
    removeAttachedFileByPath,
    toggleAttachedFile,
    hasAttachedFile,
    setQuoteData,
    clearAll,
  }
}
