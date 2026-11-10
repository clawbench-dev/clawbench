import { describe, expect, it, beforeEach } from 'vitest'
import {
  readTerminalTabs,
  saveTerminalTabs,
  type SavedTerminalTabs,
} from '@/utils/terminalProjectTabs'

const PROJECT_A = '/work/alpha'
const PROJECT_B = '/work/beta'
const STORAGE_KEY_A = 'clawbench-terminal-tabs:' + PROJECT_A

function snapshot(sessionIds: string[]): SavedTerminalTabs {
  return {
    tabs: sessionIds.map((sessionId, i) => ({
      sessionId,
      cwd: `${PROJECT_A}/sub${i}`,
      title: `sub${i}`,
    })),
    activeIndex: 0,
  }
}

beforeEach(() => {
  localStorage.clear()
})

describe('terminalProjectTabs', () => {
  it('returns null for a project that has no record', () => {
    expect(readTerminalTabs(PROJECT_A)).toBeNull()
  })

  it('writes through to storage, so the record outlives this module', () => {
    // The point of the whole module: a page reload throws away every JS object
    // but not localStorage. Asserting the raw key is what proves the record is
    // not sitting in module memory (which a reload would drop).
    saveTerminalTabs(PROJECT_A, snapshot(['a1', 'a2']))

    const raw = localStorage.getItem(STORAGE_KEY_A)
    expect(raw).toBeTruthy()
    expect(JSON.parse(raw!)).toEqual({
      tabs: [
        { sessionId: 'a1', cwd: '/work/alpha/sub0', title: 'sub0' },
        { sessionId: 'a2', cwd: '/work/alpha/sub1', title: 'sub1' },
      ],
      activeIndex: 0,
    })
  })

  it('reads a record written by an earlier page load', () => {
    // Same shape a previous build would have left behind: nothing in memory.
    localStorage.setItem(STORAGE_KEY_A, JSON.stringify({ ...snapshot(['old-1']), activeIndex: 0 }))

    const saved = readTerminalTabs(PROJECT_A)
    expect(saved?.tabs.map((t) => t.sessionId)).toEqual(['old-1'])
  })

  it('keeps each project\'s tabs separate', () => {
    saveTerminalTabs(PROJECT_A, snapshot(['a1', 'a2']))
    saveTerminalTabs(PROJECT_B, snapshot(['b1']))

    expect(readTerminalTabs(PROJECT_A)?.tabs.map((t) => t.sessionId)).toEqual(['a1', 'a2'])
    expect(readTerminalTabs(PROJECT_B)?.tabs.map((t) => t.sessionId)).toEqual(['b1'])
  })

  it('remembers which tab was active', () => {
    saveTerminalTabs(PROJECT_A, { ...snapshot(['a1', 'a2']), activeIndex: 1 })
    expect(readTerminalTabs(PROJECT_A)?.activeIndex).toBe(1)
  })

  it('overwrites the previous record of the same project', () => {
    saveTerminalTabs(PROJECT_A, snapshot(['a1', 'a2']))
    saveTerminalTabs(PROJECT_A, snapshot(['a3']))

    expect(readTerminalTabs(PROJECT_A)?.tabs.map((t) => t.sessionId)).toEqual(['a3'])
  })

  it('stores an empty tab list as a real record (user closed every tab)', () => {
    saveTerminalTabs(PROJECT_A, snapshot([]))

    // Distinguishable from "no record": the panel must show its empty state
    // instead of spawning a fresh shell.
    const saved = readTerminalTabs(PROJECT_A)
    expect(saved).not.toBeNull()
    expect(saved?.tabs).toEqual([])
  })

  it('ignores an empty project path instead of keying on it', () => {
    // Before the first project resolves there is nothing to key a record by;
    // storing it would leak one project's shells into whichever project mounts
    // next with an empty path.
    saveTerminalTabs('', snapshot(['ghost']))
    expect(readTerminalTabs('')).toBeNull()
  })

  it('ignores an unparsable record instead of breaking the panel', () => {
    localStorage.setItem(STORAGE_KEY_A, '{not json')
    expect(readTerminalTabs(PROJECT_A)).toBeNull()
  })

  it('ignores a record whose shape does not match (older build / manual edit)', () => {
    // A `tabs` that is not an array would throw on the panel's first render, so
    // shape is validated rather than trusted.
    localStorage.setItem(STORAGE_KEY_A, JSON.stringify({ tabs: 'nope', activeIndex: 0 }))
    expect(readTerminalTabs(PROJECT_A)).toBeNull()

    localStorage.setItem(STORAGE_KEY_A, JSON.stringify({ tabs: [{ sessionId: 1 }], activeIndex: 0 }))
    expect(readTerminalTabs(PROJECT_A)).toBeNull()

    localStorage.setItem(STORAGE_KEY_A, JSON.stringify({ tabs: [] }))
    expect(readTerminalTabs(PROJECT_A)).toBeNull()
  })
})
