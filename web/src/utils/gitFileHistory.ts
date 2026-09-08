export interface GitCommitLite {
  sha: string
  msg: string
  date: string
  author: string
  isWT?: boolean
}

/**
 * Split a git file path into its bare file name and parent directory.
 *
 * Git change lists carry full repo-relative paths (`web/src/foo.ts`). The
 * file-list UI renders them as two lines — the bare name on top, the parent
 * directory (without the file name) below. Root-level files have no directory.
 */
export function splitGitFilePath(path: string): { name: string; dir: string } {
  // git status --porcelain renders untracked directories as "dir/"; strip the
  // trailing slash so the directory itself is treated as a leaf entry.
  const trimmed = path.length > 1 && path.endsWith('/') ? path.slice(0, -1) : path
  const idx = trimmed.lastIndexOf('/')
  if (idx < 0) return { name: trimmed, dir: '' }
  return { name: trimmed.slice(idx + 1), dir: trimmed.slice(0, idx) }
}

/**
 * Build the commit list for a single-file history (file mode).
 *
 * A working-tree entry is prepended only when the file itself has uncommitted
 * changes. Otherwise the file history shows tracked commits only — a file
 * without workspace changes must not show a working-tree entry.
 */
export function buildFileHistoryCommits(histCommits: GitCommitLite[], hasUncommitted: boolean, workingTreeMsg: string): GitCommitLite[] {
  if (hasUncommitted) {
    return [
      { sha: 'HEAD', msg: workingTreeMsg, date: '', author: '', isWT: true },
      ...histCommits,
    ]
  }
  return histCommits
}

/**
 * Whether a full history reload should show the full-screen loading spinner.
 *
 * Background refreshes keep the existing commit list visible (and the refresh
 * button mounted so its spin feedback is visible); the full-screen spinner is
 * only shown when there is nothing to render yet — first load or after an error.
 */
export function shouldShowFullLoading(commits: unknown[], error: unknown): boolean {
  return commits.length === 0 && !error
}
