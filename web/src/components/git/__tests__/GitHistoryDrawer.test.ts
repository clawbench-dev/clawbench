import { describe, expect, it } from 'vitest'
import { buildFileHistoryCommits } from '@/utils/gitFileHistory.ts'

const commitA = { sha: 'abc123', msg: 'Commit A', date: '2025-01-01', author: 'T' }
const workingTreeMsg = 'Working tree changes'

describe('buildFileHistoryCommits', () => {
  it('prepends a working-tree entry when the file has uncommitted changes', () => {
    const result = buildFileHistoryCommits([commitA], true, workingTreeMsg)
    expect(result).toHaveLength(2)
    expect(result[0]).toMatchObject({ sha: 'HEAD', msg: workingTreeMsg, isWT: true, fileCount: 1 })
    expect(result[1]).toBe(commitA)
  })

  it('returns the tracked commits unchanged when the file has no uncommitted changes', () => {
    const result = buildFileHistoryCommits([commitA], false, workingTreeMsg)
    expect(result).toHaveLength(1)
    expect(result[0]).toBe(commitA)
  })

  it('prepends a working-tree entry for an untracked file with no commits', () => {
    const result = buildFileHistoryCommits([], true, workingTreeMsg)
    expect(result).toHaveLength(1)
    expect(result[0]).toMatchObject({ sha: 'HEAD', isWT: true })
  })
})
