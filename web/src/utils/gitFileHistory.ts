export interface GitCommitLite {
  sha: string
  msg: string
  date: string
  author: string
  isWT?: boolean
  fileCount?: number
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
      { sha: 'HEAD', msg: workingTreeMsg, date: '', author: '', isWT: true, fileCount: 1 },
      ...histCommits,
    ]
  }
  return histCommits
}
