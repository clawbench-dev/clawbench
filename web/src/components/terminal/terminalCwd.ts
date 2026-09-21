import { isAbsolutePath } from '@/utils/path'

export interface ResolveTerminalCwdInput {
  currentFilePath?: string | null
  currentDir?: string | null
  requestedCwd?: string | null
}

/**
 * Normalize a cwd to the form the terminal endpoint expects.
 *
 * A project-RELATIVE path is stripped of its leading/trailing separators (the
 * backend resolves it against the project root). An ABSOLUTE path is a
 * project-external directory — the file manager can browse those and offers
 * "open terminal here" — and must keep its root, or the terminal would open at
 * the project root instead of the requested directory.
 */
function normalizeCwd(path: string): string {
  if (isAbsolutePath(path)) return path.replace(/\/+$/, '') || path
  return path.replace(/^\/+/, '').replace(/\/+$/, '')
}

function dirname(path: string): string {
  const normalized = normalizeCwd(path)
  const slash = normalized.lastIndexOf('/')
  if (slash <= 0) return ''
  return normalized.slice(0, slash)
}

export function resolveTerminalCwd(input: ResolveTerminalCwdInput): string {
  if (input.requestedCwd) {
    return normalizeCwd(input.requestedCwd)
  }
  if (input.currentFilePath) {
    return dirname(input.currentFilePath)
  }
  if (input.currentDir) {
    return normalizeCwd(input.currentDir)
  }
  return ''
}

function normalizeAbsolutePath(path: string): string {
  if (!path) return ''
  return path.replace(/\/+$/, '')
}

export function shouldPromptForTerminalReopen(currentCwd: string, targetCwd: string): boolean {
  const current = normalizeAbsolutePath(currentCwd)
  const target = normalizeAbsolutePath(targetCwd)
  return Boolean(current && target && current !== target)
}
