import { ref } from 'vue'

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

const attachedFiles = ref<string[]>([])
const quoteData = ref<QuoteData | null>(null)

function addAttachedFile(path: string) {
  if (path && !attachedFiles.value.includes(path)) {
    attachedFiles.value.push(path)
  }
}

function removeAttachedFile(index: number) {
  attachedFiles.value.splice(index, 1)
}

function removeAttachedFileByPath(path: string) {
  const idx = attachedFiles.value.indexOf(path)
  if (idx >= 0) attachedFiles.value.splice(idx, 1)
}

function toggleAttachedFile(path: string) {
  if (!path) return
  const idx = attachedFiles.value.indexOf(path)
  if (idx >= 0) {
    attachedFiles.value.splice(idx, 1)
  } else {
    attachedFiles.value.push(path)
  }
}

function hasAttachedFile(path: string): boolean {
  return attachedFiles.value.includes(path)
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
