import { describe, expect, it } from 'vitest'
import { suggestCommands } from '../commandSuggestion'

const cmds = (...names: string[]) => names.map(command => ({ command }))

describe('suggestCommands', () => {
  it('returns the newest commands when nothing is typed', () => {
    const list = cmds('a', 'b', 'c', 'd', 'e', 'f')
    expect(suggestCommands(list, '').map(c => c.command)).toEqual(['a', 'b', 'c', 'd', 'e'])
  })

  it('prefers prefix matches', () => {
    const list = cmds('docker ps', 'git status', 'git log', 'echo git')
    expect(suggestCommands(list, 'git').map(c => c.command)).toEqual([
      'git status',
      'git log',
      'echo git',
    ])
  })

  it('falls back to substring matches', () => {
    const list = cmds('npm run build', 'git commit')
    expect(suggestCommands(list, 'commit').map(c => c.command)).toEqual(['git commit'])
  })

  it('drops the line being typed', () => {
    // Offering back exactly what is already in the box is pure noise.
    expect(suggestCommands(cmds('git status'), 'git status')).toEqual([])
  })

  it('de-duplicates repeated commands', () => {
    const list = cmds('git status', 'git status', 'git log')
    expect(suggestCommands(list, 'git').map(c => c.command)).toEqual(['git status', 'git log'])
  })

  it('matches the last line of multi-line input', () => {
    const list = cmds('git status', 'make all')
    expect(suggestCommands(list, 'cd /tmp\nmake').map(c => c.command)).toEqual(['make all'])
  })

  it('returns nothing when no command matches', () => {
    expect(suggestCommands(cmds('git status'), 'zzz')).toEqual([])
  })

  it('honours the limit', () => {
    const list = cmds('a1', 'a2', 'a3', 'a4')
    expect(suggestCommands(list, 'a', 2)).toHaveLength(2)
  })

  it('keeps prefix matches ahead of substring matches within the limit', () => {
    const list = cmds('x-git', 'git a', 'git b', 'git c')
    expect(suggestCommands(list, 'git', 2).map(c => c.command)).toEqual(['git a', 'git b'])
  })
})
