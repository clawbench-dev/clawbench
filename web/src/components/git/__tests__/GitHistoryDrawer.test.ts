import { describe, expect, it } from 'vitest'
import { buildFileHistoryCommits, shouldShowFullLoading } from '@/utils/gitFileHistory.ts'

const commitA = { sha: 'abc123', msg: 'Commit A', date: '2025-01-01', author: 'T' }
const workingTreeMsg = 'Working tree changes'

describe('buildFileHistoryCommits', () => {
  it('prepends a working-tree entry when the file has uncommitted changes', () => {
    const result = buildFileHistoryCommits([commitA], true, workingTreeMsg)
    expect(result).toHaveLength(2)
    expect(result[0]).toMatchObject({ sha: 'HEAD', msg: workingTreeMsg, isWT: true })
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

describe('shouldShowFullLoading', () => {
  it('shows the full-screen spinner on first load (empty list, no error)', () => {
    expect(shouldShowFullLoading([], '')).toBe(true)
    expect(shouldShowFullLoading([], undefined)).toBe(true)
  })

  it('keeps the existing list visible during a background refresh', () => {
    expect(shouldShowFullLoading([commitA], '')).toBe(false)
    expect(shouldShowFullLoading([commitA, commitA], undefined)).toBe(false)
  })

  it('does not show the loading spinner when an error is present (error branch wins)', () => {
    // The template renders the error branch before the loading branch, so the
    // full-screen spinner must not engage while an error is shown.
    expect(shouldShowFullLoading([commitA], 'boom')).toBe(false)
    expect(shouldShowFullLoading([], 'boom')).toBe(false)
  })
})
