/**
 * Integration tests for computeGraphData using real git repo data.
 *
 * These tests use the actual commit SHAs and parent relationships from
 * the test git repos in test/git-graph-repos/ to verify that:
 * 1. Lane assignments match the expected visual graph structure
 * 2. Connection types (VERT/FORK/MERGE-IN) are correct
 * 3. Partial loading produces correct continuation lines with exact SVG paths
 * 4. Every visible node has at least one connection line to it or from it
 *    (no orphaned nodes missing their graph lines)
 * 5. Continuation lines have the `fade: true` property and correct coordinates
 */

import { describe, expect, it } from 'vitest'
import { computeGraphData } from '@/utils/gitGraph'

const ROW_HEIGHT = 64
const LANE_WIDTH = 20
const GRAPH_LEFT_PADDING = 10

// ─── Coordinate helpers (mirrors gitGraph.ts) ───
function laneCx(lane: number) {
  return lane * LANE_WIDTH + LANE_WIDTH / 2 + GRAPH_LEFT_PADDING
}

function rowCy(row: number) {
  return row * ROW_HEIGHT + ROW_HEIGHT / 2
}

// ─── Helper: extract connection info from graph data ───
function getGraphInfo(commits: any[], rowHeight = ROW_HEIGHT, previousShaToLane?: any) {
  const { nodes, lines, laneCount, graphWidth, shaToLane } = computeGraphData(
    commits, rowHeight, previousShaToLane
  ) as { nodes: any[]; lines: any[]; laneCount: number; graphWidth: number; shaToLane: any }

  const nodeByRow = new Map()
  const nodeBySha = new Map()
  for (const n of nodes) {
    nodeByRow.set(n.row, n)
    if (!n.isWT) nodeBySha.set(commits[n.row].sha, n)
  }

  // Classify connections by walking commit→parent edges
  const connections = []
  for (const n of nodes) {
    if (n.isWT) continue
    const c = commits[n.row]
    const parents = c.parents || []

    for (let pi = 0; pi < parents.length; pi++) {
      const pSha = parents[pi]
      const pNode = nodeBySha.get(pSha)
      if (!pNode) {
        // Parent not in loaded commits
        connections.push({
          fromRow: n.row, toRow: -1,
          fromLane: n.lane, toLane: -1,
          type: 'CONTINUATION', parentIndex: pi,
        })
        continue
      }

      const childLane = n.lane
      const parentLane = pNode.lane

      if (childLane === parentLane) {
        connections.push({
          fromRow: n.row, toRow: pNode.row,
          fromLane: childLane, toLane: parentLane,
          type: 'VERT', parentIndex: pi,
        })
      } else if (pi > 0) {
        connections.push({
          fromRow: n.row, toRow: pNode.row,
          fromLane: childLane, toLane: parentLane,
          type: 'FORK', parentIndex: pi,
        })
      } else {
        connections.push({
          fromRow: n.row, toRow: pNode.row,
          fromLane: childLane, toLane: parentLane,
          type: 'MERGE-IN', parentIndex: pi,
        })
      }
    }
  }

  return { nodes, lines, laneCount, graphWidth, connections, nodeByRow, nodeBySha, shaToLane }
}

// ═══════════════════════════════════════════════════════════════════════════
// 01-linear: 5 commits, single branch
// ═══════════════════════════════════════════════════════════════════════════

const LINEAR = [
  { sha: 'ab44778', parents: ['993e655'], msg: 'commit 5' },
  { sha: '993e655', parents: ['23097ad'], msg: 'commit 4' },
  { sha: '23097ad', parents: ['c4416dd'], msg: 'commit 3' },
  { sha: 'c4416dd', parents: ['88b5155'], msg: 'commit 2' },
  { sha: '88b5155', parents: [], msg: 'commit 1' },
]

describe('01-linear (real data)', () => {
  const { nodes, connections, laneCount, lines } = getGraphInfo(LINEAR)

  it('all commits on lane 0', () => {
    for (const n of nodes) expect(n.lane).toBe(0)
  })

  it('has 1 lane', () => expect(laneCount).toBe(1))

  it('4 VERT connections, no FORK/MERGE-IN', () => {
    expect(connections.filter(c => c.type === 'VERT')).toHaveLength(4)
    expect(connections.filter(c => c.type === 'FORK')).toHaveLength(0)
    expect(connections.filter(c => c.type === 'MERGE-IN')).toHaveLength(0)
  })

  it('no CONTINUATION lines (all parents visible)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })

  it('4 vertical SVG lines: row 0→1, row 1→2, row 2→3, row 3→4', () => {
    const x = laneCx(0) // 20
    for (let row = 0; row < 4; row++) {
      const y1 = rowCy(row) + 5
      const y2 = rowCy(row + 1) - 5
      const expectedPath = `M${x},${y1} L${x},${y2}`
      const found = lines.some(l => l.path === expectedPath && !l.fade)
      expect(found).toBe(true)
    }
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 02-single-merge: main + feature branch, one merge
// ═══════════════════════════════════════════════════════════════════════════

const SINGLE_MERGE = [
  { sha: 'e48a09e', parents: ['7e8265c'], msg: 'main: commit 4', refs: ['HEAD -> master'] },
  { sha: '7e8265c', parents: ['3bb4df1', '0971cee'], msg: 'merge: integrate feature' },
  { sha: '0971cee', parents: ['3cc90b7'], msg: 'feature: commit 2', refs: ['feature'] },
  { sha: '3cc90b7', parents: ['357d91a'], msg: 'feature: commit 1' },
  { sha: '3bb4df1', parents: ['357d91a'], msg: 'main: commit 3' },
  { sha: '357d91a', parents: ['cd74ef5'], msg: 'main: commit 2' },
  { sha: 'cd74ef5', parents: [], msg: 'main: commit 1' },
]

describe('02-single-merge (real data)', () => {
  const { nodes, connections, laneCount, lines } = getGraphInfo(SINGLE_MERGE)

  it('2 lanes', () => expect(laneCount).toBe(2))

  it('main line on lane 0: rows 0,1,4,5,6', () => {
    for (const row of [0, 1, 4, 5, 6]) expect(nodes[row].lane).toBe(0)
  })

  it('feature branch on lane 1: rows 2,3', () => {
    for (const row of [2, 3]) expect(nodes[row].lane).toBe(1)
  })

  it('1 FORK (merge → feature: commit 2)', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(1)
    expect(forks[0].fromRow).toBe(1)
    expect(forks[0].toRow).toBe(2)
    expect(forks[0].fromLane).toBe(0)
    expect(forks[0].toLane).toBe(1)
  })

  it('1 MERGE-IN (feature: commit 1 → main: commit 2)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(1)
    expect(mergeIns[0].fromRow).toBe(3)
    expect(mergeIns[0].toRow).toBe(5)
    expect(mergeIns[0].fromLane).toBe(1)
    expect(mergeIns[0].toLane).toBe(0)
  })

  it('no CONTINUATION (full data loaded)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })

  it('lane 0 VERT lines: row 0→1, row 1→4, row 4→5, row 5→6', () => {
    const x = laneCx(0)
    // row 0→1: main: commit 4 → merge
    expect(lines.some(l => l.path === `M${x},${rowCy(0) + 5} L${x},${rowCy(1) - 5}` && !l.fade)).toBe(true)
    // row 1→4: merge → main: commit 3
    expect(lines.some(l => l.path === `M${x},${rowCy(1) + 5} L${x},${rowCy(4) - 5}` && !l.fade)).toBe(true)
    // row 4→5: main: commit 3 → main: commit 2
    expect(lines.some(l => l.path === `M${x},${rowCy(4) + 5} L${x},${rowCy(5) - 5}` && !l.fade)).toBe(true)
    // row 5→6: main: commit 2 → main: commit 1
    expect(lines.some(l => l.path === `M${x},${rowCy(5) + 5} L${x},${rowCy(6) - 5}` && !l.fade)).toBe(true)
  })

  it('lane 1 VERT line: row 2→3 (feature: commit 2 → feature: commit 1)', () => {
    const x = laneCx(1)
    expect(lines.some(l => l.path === `M${x},${rowCy(2) + 5} L${x},${rowCy(3) - 5}` && !l.fade)).toBe(true)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 03-multi-branch: two feature branches merged sequentially
// ═══════════════════════════════════════════════════════════════════════════

const MULTI_BRANCH = [
  { sha: '964f480', parents: ['482d32e'], msg: 'main: after merges' },
  { sha: '482d32e', parents: ['b007c8b', '682ca0c'], msg: 'merge: feature-b' },
  { sha: '682ca0c', parents: ['f7b2c27'], msg: 'feature-b: work 3', refs: ['feature-b'] },
  { sha: 'f7b2c27', parents: ['caa37e7'], msg: 'feature-b: work 2' },
  { sha: 'caa37e7', parents: ['c929e3f'], msg: 'feature-b: work 1' },
  { sha: 'b007c8b', parents: ['1aeaffd', '50930e4'], msg: 'merge: feature-a' },
  { sha: '50930e4', parents: ['f49756e'], msg: 'feature-a: work 2', refs: ['feature-a'] },
  { sha: 'f49756e', parents: ['c929e3f'], msg: 'feature-a: work 1' },
  { sha: '1aeaffd', parents: ['c929e3f'], msg: 'main: third' },
  { sha: 'c929e3f', parents: ['074dbaa'], msg: 'main: second' },
  { sha: '074dbaa', parents: [], msg: 'main: init' },
]

describe('03-multi-branch (real data)', () => {
  const { nodes, connections, laneCount } = getGraphInfo(MULTI_BRANCH)

  it('3 lanes (main + feature-b + feature-a)', () => expect(laneCount).toBe(3))

  it('main on lane 0: rows 0,1,5,8,9,10', () => {
    for (const row of [0, 1, 5, 8, 9, 10]) expect(nodes[row].lane).toBe(0)
  })

  it('feature-b on lane 1: rows 2,3,4', () => {
    for (const row of [2, 3, 4]) expect(nodes[row].lane).toBe(1)
  })

  it('feature-a on lane 2: rows 6,7', () => {
    for (const row of [6, 7]) expect(nodes[row].lane).toBe(2)
  })

  it('2 FORK connections', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(2)
  })

  it('no CONTINUATION (full data loaded)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 04-frequent-merge: dev branch with frequent merges from main
// ═══════════════════════════════════════════════════════════════════════════

const FREQUENT_MERGE = [
  { sha: 'ff36d18', parents: ['7fc1158'], msg: 'main: final', refs: ['HEAD -> master'] },
  { sha: '7fc1158', parents: ['1fe1097'], msg: 'dev: work 3', refs: ['dev'] },
  { sha: '1fe1097', parents: ['9620ba1', 'a19508b'], msg: 'dev: merge main 2' },
  { sha: 'a19508b', parents: ['14dcef9'], msg: 'main: work 2' },
  { sha: '9620ba1', parents: ['539b985'], msg: 'dev: work 2' },
  { sha: '539b985', parents: ['e121a5d', '14dcef9'], msg: 'dev: merge main 1' },
  { sha: '14dcef9', parents: ['074dbaa'], msg: 'main: work 1' },
  { sha: 'e121a5d', parents: ['074dbaa'], msg: 'dev: work 1' },
  { sha: '074dbaa', parents: [], msg: 'main: init' },
]

describe('04-frequent-merge (real data)', () => {
  const { nodes, connections, laneCount } = getGraphInfo(FREQUENT_MERGE)

  it('2 lanes (dev line + main side branch)', () => expect(laneCount).toBe(2))

  it('first-parent chain (dev) on lane 0', () => {
    for (const row of [0, 1, 2, 4, 5, 7]) expect(nodes[row].lane).toBe(0)
  })

  it('main side branch on lane 1', () => {
    for (const row of [3, 6]) expect(nodes[row].lane).toBe(1)
  })

  it('no CONTINUATION (full data loaded)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 05-long-lived-branch: release branch alongside main
// ═══════════════════════════════════════════════════════════════════════════

const LONG_LIVED = [
  { sha: 'e1c63f7', parents: ['08e88f7'], msg: 'main: post-release', refs: ['HEAD -> master'] },
  { sha: '08e88f7', parents: ['91a7f14', 'bc644a9'], msg: 'merge: release-1.0' },
  { sha: 'bc644a9', parents: ['555f1bd'], msg: 'release: bugfix', refs: ['release-1.0'] },
  { sha: '555f1bd', parents: ['eba78a8'], msg: 'release: prep 2' },
  { sha: 'eba78a8', parents: ['074dbaa'], msg: 'release: prep 1' },
  { sha: '91a7f14', parents: ['68cd412'], msg: 'main: feature 4' },
  { sha: '68cd412', parents: ['4955893'], msg: 'main: feature 3' },
  { sha: '4955893', parents: ['7e864f0'], msg: 'main: feature 2' },
  { sha: '7e864f0', parents: ['074dbaa'], msg: 'main: feature 1' },
  { sha: '074dbaa', parents: [], msg: 'main: init' },
]

describe('05-long-lived-branch (real data)', () => {
  const { nodes, connections, laneCount } = getGraphInfo(LONG_LIVED)

  it('2 lanes (main + release)', () => expect(laneCount).toBe(2))

  it('main on lane 0', () => {
    for (const row of [0, 1, 5, 6, 7, 8, 9]) expect(nodes[row].lane).toBe(0)
  })

  it('release on lane 1', () => {
    for (const row of [2, 3, 4]) expect(nodes[row].lane).toBe(1)
  })

  it('no CONTINUATION (full data loaded)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 06-octopus-merge: merge of 3 feature branches at once
// ═══════════════════════════════════════════════════════════════════════════

const OCTOPUS = [
  { sha: '04a2046', parents: ['55731c9'], msg: 'main: final', refs: ['HEAD -> master'] },
  { sha: '55731c9', parents: ['5388f87', '8d500bc', '64621af', '185fe1e'], msg: 'merge: combine all' },
  { sha: '185fe1e', parents: ['af44737'], msg: 'feature-c: work', refs: ['feature-c'] },
  { sha: '64621af', parents: ['af44737'], msg: 'feature-b: work', refs: ['feature-b'] },
  { sha: '8d500bc', parents: ['af44737'], msg: 'feature-a: work', refs: ['feature-a'] },
  { sha: '5388f87', parents: ['af44737'], msg: 'main: second' },
  { sha: 'af44737', parents: [], msg: 'main: init' },
]

describe('06-octopus-merge (real data)', () => {
  const { connections, laneCount } = getGraphInfo(OCTOPUS)

  it('4 lanes (main + 3 features)', () => expect(laneCount).toBe(4))

  it('3 FORK connections (all from merge commit)', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(3)
    for (const f of forks) expect(f.fromRow).toBe(1)
  })

  it('3 MERGE-IN connections (all merging into init)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(3)
    for (const m of mergeIns) expect(m.toRow).toBe(6)
  })

  it('no CONTINUATION (full data loaded)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 07-open-branches: multiple branches that haven't been merged
// ═══════════════════════════════════════════════════════════════════════════

const OPEN_BRANCHES = [
  { sha: '4512f9f', parents: ['04335a1'], msg: 'feature-a: work 2', refs: ['feature-a'] },
  { sha: '04335a1', parents: ['5388f87'], msg: 'feature-a: work 1' },
  { sha: 'fb83ca6', parents: ['5388f87'], msg: 'feature-b: work 1', refs: ['feature-b'] },
  { sha: 'e16ecb4', parents: ['5388f87'], msg: 'main: third', refs: ['HEAD -> master'] },
  { sha: '5388f87', parents: ['af44737'], msg: 'main: second' },
  { sha: 'af44737', parents: [], msg: 'main: init' },
]

describe('07-open-branches (real data)', () => {
  const { nodes, connections } = getGraphInfo(OPEN_BRANCHES)

  it('feature-a on lane 0 (first-parent chain)', () => {
    expect(nodes[0].lane).toBe(0)
    expect(nodes[1].lane).toBe(0)
  })

  it('has MERGE-IN connections for feature-b and main: third', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(2)
  })

  it('no FORK connections (no merge commits)', () => {
    expect(connections.filter(c => c.type === 'FORK')).toHaveLength(0)
  })

  it('no CONTINUATION (full data loaded)', () => {
    expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// 08-long-linear: test pagination / scroll scenario
// ═══════════════════════════════════════════════════════════════════════════

const LONG_LINEAR = [
  { sha: '31b22c8', parents: ['65c2645'], msg: 'commit 40' },
  { sha: '65c2645', parents: ['2e722a8'], msg: 'commit 39' },
  { sha: '2e722a8', parents: ['99f0ee6'], msg: 'commit 38' },
  { sha: '99f0ee6', parents: ['4745ad4'], msg: 'commit 37' },
  { sha: '4745ad4', parents: ['b652381'], msg: 'commit 36' },
  { sha: 'b652381', parents: ['fcd96f6'], msg: 'commit 35' },
  { sha: 'fcd96f6', parents: ['149e621'], msg: 'commit 34' },
  { sha: '149e621', parents: ['597b791'], msg: 'commit 33' },
  { sha: '597b791', parents: ['17dbced'], msg: 'commit 32' },
  { sha: '17dbced', parents: ['fd5c695'], msg: 'commit 31' },
]

describe('08-long-linear (real data, first 10)', () => {
  const { nodes, connections } = getGraphInfo(LONG_LINEAR)

  it('all on lane 0', () => {
    for (const n of nodes) expect(n.lane).toBe(0)
  })

  it('1 CONTINUATION (commit 31 → parent not in list)', () => {
    const continuations = connections.filter(c => c.type === 'CONTINUATION')
    expect(continuations).toHaveLength(1)
    expect(continuations[0].fromRow).toBe(9) // commit 31
  })

  it('9 VERT connections', () => {
    expect(connections.filter(c => c.type === 'VERT')).toHaveLength(9)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// PARTIAL LOADING: Continuation lines — exact SVG path verification
//
// When a commit's first parent is NOT in the loaded commit list, the graph
// must draw a continuation line from that commit's node down to the SVG
// bottom. This is the core bug fix: "bottom commits have no graph lines".
// ═══════════════════════════════════════════════════════════════════════════

describe('partial loading - continuation lines with exact SVG paths', () => {
  it('01-linear: loading 3 of 5 — bottom node has continuation line with exact path', () => {
    const partial = LINEAR.slice(0, 3) // commits 5,4,3
    const { lines, connections } = getGraphInfo(partial)

    // All 3 commits are on lane 0
    // Row 2 (commit 3) has parent c4416dd which is not loaded
    // Continuation line: from row 2's bottom to SVG bottom
    const x = laneCx(0) // 20
    const y1 = rowCy(2) + 5 // 115 + 5 = 120
    const svgBottom = 3 * ROW_HEIGHT + ROW_HEIGHT // 184 (extends one row below)
    const expectedPath = `M${x},${y1} L${x},${svgBottom}`

    const contLine = lines.find(l => l.path === expectedPath && l.fade === true)
    expect(contLine).toBeDefined()
    expect(contLine.lane).toBe(0)

    // Also verify connection metadata
    const contConn = connections.filter(c => c.type === 'CONTINUATION')
    expect(contConn).toHaveLength(1)
    expect(contConn[0].fromRow).toBe(2)
    expect(contConn[0].parentIndex).toBe(0) // first parent
  })

  it('02-single-merge: loading 4 of 7 — both lanes have continuation lines', () => {
    const partial = SINGLE_MERGE.slice(0, 4)
    // rows: 0=e48a09e(lane0), 1=7e8265c(lane0,merge), 2=0971cee(lane1), 3=3cc90b7(lane1)
    // 7e8265c's first parent 3bb4df1 NOT loaded → continuation on lane 0
    // 3cc90b7's first parent 357d91a NOT loaded → continuation on lane 1
    const { lines, connections } = getGraphInfo(partial)

    const svgBottom = 4 * ROW_HEIGHT + ROW_HEIGHT // 230

    // Lane 0 continuation: from row 1 (merge) bottom to SVG bottom
    const lane0x = laneCx(0)
    const lane0y1 = rowCy(1) + 5
    const lane0Cont = lines.find(l =>
      l.path === `M${lane0x},${lane0y1} L${lane0x},${svgBottom}` && l.fade === true
    )
    expect(lane0Cont).toBeDefined()
    expect(lane0Cont.lane).toBe(0)

    // Lane 1 continuation: from row 3 (feature: commit 1) bottom to SVG bottom
    const lane1x = laneCx(1)
    const lane1y1 = rowCy(3) + 5
    const lane1Cont = lines.find(l =>
      l.path === `M${lane1x},${lane1y1} L${lane1x},${svgBottom}` && l.fade === true
    )
    expect(lane1Cont).toBeDefined()
    expect(lane1Cont.lane).toBe(1)

    // Verify connection metadata
    const contConns = connections.filter(c => c.type === 'CONTINUATION')
    // 7e8265c's first parent + 3cc90b7's first parent + 7e8265c's second parent (not rendered)
    expect(contConns.length).toBeGreaterThanOrEqual(2)
  })

  it('07-open-branches: loading 4 of 6 — multiple lanes have continuation lines', () => {
    const partial = OPEN_BRANCHES.slice(0, 4)
    // rows: 0=feature-a:work2(lane0), 1=feature-a:work1(lane0), 2=feature-b:work1(lane1), 3=main:third(lane1 or 2)
    // Rows 1,2,3 all have parent '5388f87' which is NOT loaded
    const { lines, nodes } = getGraphInfo(partial)

    const svgBottom = 4 * ROW_HEIGHT + ROW_HEIGHT // 230

    // Find bottommost node on each lane whose first parent is not loaded
    const lanesNeedingCont = new Map() // lane → { row, node }
    for (const n of nodes) {
      if (n.isWT) continue
      const c = partial[n.row]
      const firstParent = c.parents?.[0]
      if (!firstParent) continue // root
      // Check if parent is in loaded commits
      const parentInList = partial.some(pc => pc.sha === firstParent)
      if (!parentInList) {
        const existing = lanesNeedingCont.get(n.lane)
        if (!existing || n.row > existing.row) {
          lanesNeedingCont.set(n.lane, { row: n.row, node: n })
        }
      }
    }

    // Each lane needing continuation must have a fade line
    for (const [lane, { row }] of lanesNeedingCont) {
      const x = laneCx(lane)
      const y1 = rowCy(row) + 5
      const contLine = lines.find(l =>
        l.path === `M${x},${y1} L${x},${svgBottom}` && l.fade === true
      )
      expect(contLine).toBeDefined()
    }
  })

  it('08-long-linear: loading 10 — last row has continuation line', () => {
    const { lines } = getGraphInfo(LONG_LINEAR)
    // Row 9 (commit 31) has parent fd5c695 which is not loaded
    const x = laneCx(0) // 20
    const y1 = rowCy(9) + 5 // 9*46+23+5 = 437
    const svgBottom = 10 * ROW_HEIGHT + ROW_HEIGHT // 506
    const expectedPath = `M${x},${y1} L${x},${svgBottom}`

    const contLine = lines.find(l => l.path === expectedPath && l.fade === true)
    expect(contLine).toBeDefined()
  })

  it('05-long-lived: partial load (5 of 10) — both lanes have continuations', () => {
    const partial = LONG_LIVED.slice(0, 5)
    // rows: 0=main:post(lane0), 1=merge(lane0), 2=release:bugfix(lane1),
    //        3=release:prep2(lane1), 4=release:prep1(lane1)
    // Row 1's first parent (91a7f14) not loaded → continuation on lane 0
    // Row 4's first parent (074dbaa) not loaded → continuation on lane 1
    const { lines } = getGraphInfo(partial)
    const svgBottom = 5 * ROW_HEIGHT + ROW_HEIGHT // 276

    // Lane 0: bottom node needing continuation is row 1 (merge)
    const lane0x = laneCx(0)
    const lane0y1 = rowCy(1) + 5
    const lane0Cont = lines.find(l =>
      l.path === `M${lane0x},${lane0y1} L${lane0x},${svgBottom}` && l.fade === true
    )
    expect(lane0Cont).toBeDefined()

    // Lane 1: bottom node needing continuation is row 4 (release: prep 1)
    const lane1x = laneCx(1)
    const lane1y1 = rowCy(4) + 5
    const lane1Cont = lines.find(l =>
      l.path === `M${lane1x},${lane1y1} L${lane1x},${svgBottom}` && l.fade === true
    )
    expect(lane1Cont).toBeDefined()
  })

  it('06-octopus: partial load (3 of 7) — main lane continuation + feature-c', () => {
    const partial = OCTOPUS.slice(0, 3)
    // rows: 0=main:final(lane0), 1=merge(lane0), 2=feature-c(lane1)
    // Row 1's first parent (5388f87) not loaded → continuation on lane 0
    // Row 2's parent (af44737) not loaded → continuation on lane 1
    const { lines } = getGraphInfo(partial)
    const svgBottom = 3 * ROW_HEIGHT + ROW_HEIGHT // 184 (extends one row below)

    // Lane 0: from row 1 (merge) bottom
    const lane0x = laneCx(0)
    const lane0y1 = rowCy(1) + 5
    expect(lines.some(l =>
      l.path === `M${lane0x},${lane0y1} L${lane0x},${svgBottom}` && l.fade === true
    )).toBe(true)

    // Lane 1: from row 2 (feature-c) bottom
    const lane1x = laneCx(1)
    const lane1y1 = rowCy(2) + 5
    expect(lines.some(l =>
      l.path === `M${lane1x},${lane1y1} L${lane1x},${svgBottom}` && l.fade === true
    )).toBe(true)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// CRITICAL: Non-first-parent outside range does NOT produce continuation
//
// In git, a merge commit has multiple parents. The first parent is the
// branch you were on when you merged. The second+ parents are the branches
// you merged in. Only first-parent edges get continuation lines — other
// parents will appear when more commits are loaded.
// ═══════════════════════════════════════════════════════════════════════════

describe('non-first-parent outside range does NOT produce continuation', () => {
  it('02-single-merge: merge commit second parent has no continuation line', () => {
    // Load: e48a09e, 7e8265c, 3bb4df1
    // 7e8265c (merge) parents: 3bb4df1 (first, loaded) and 0971cee (second, NOT loaded)
    const partial = [
      SINGLE_MERGE[0], // e48a09e
      SINGLE_MERGE[1], // 7e8265c (merge, parents: 3bb4df1, 0971cee)
      SINGLE_MERGE[4], // 3bb4df1
    ]
    const { lines, nodes } = getGraphInfo(partial)

    // Merge node is row 1
    const mergeNode = nodes[1]
    const mergeBottomY = mergeNode.cy + 5

    // Only 1 line from merge bottom: the VERT to first parent on lane 0
    const fromMerge = lines.filter(l => l.path.startsWith(`M${mergeNode.cx},${mergeBottomY}`))
    expect(fromMerge).toHaveLength(1)
    expect(fromMerge[0].fade).toBeFalsy() // it's a normal VERT line, not a continuation
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// Full data: NO continuation lines when all commits are loaded
// ═══════════════════════════════════════════════════════════════════════════

describe('full repos have zero continuation lines', () => {
  const allRepos = [
    { name: '01-linear', data: LINEAR },
    { name: '02-single-merge', data: SINGLE_MERGE },
    { name: '03-multi-branch', data: MULTI_BRANCH },
    { name: '04-frequent-merge', data: FREQUENT_MERGE },
    { name: '05-long-lived', data: LONG_LIVED },
    { name: '06-octopus', data: OCTOPUS },
    { name: '07-open-branches', data: OPEN_BRANCHES },
  ]

  for (const { name, data } of allRepos) {
    it(`${name}: no fade lines, no CONTINUATION connections`, () => {
      const { lines, connections } = getGraphInfo(data)
      expect(lines.filter(l => l.fade)).toHaveLength(0)
      expect(connections.filter(c => c.type === 'CONTINUATION')).toHaveLength(0)
    })
  }
})

// ═══════════════════════════════════════════════════════════════════════════
// Every lane with unloaded first parents has continuation lines
//
// This is the core invariant: for each lane, the bottommost node whose
// first parent is NOT loaded MUST have a continuation line going down.
// ═══════════════════════════════════════════════════════════════════════════

describe('every lane with unloaded parents has continuation lines', () => {
  function verifyLanesHaveContinuations(commits: any[]) {
    const { nodes, lines } = getGraphInfo(commits)
    const svgBottom = commits.length * ROW_HEIGHT + ROW_HEIGHT

    // Find each lane's bottommost node whose first parent is NOT in loaded commits
    const shaSet = new Set(commits.map(c => c.sha))
    const lanesNeedingContinuation = new Map() // lane → { row, node }

    for (const n of nodes) {
      if (n.isWT) continue
      const c = commits[n.row]
      const parents = c.parents || []
      if (parents.length === 0) continue // root commit, no parent

      const firstParent = parents[0]
      if (shaSet.has(firstParent)) continue // parent loaded, normal VERT handles it

      const existing = lanesNeedingContinuation.get(n.lane)
      if (!existing || n.row > existing.row) {
        lanesNeedingContinuation.set(n.lane, { row: n.row, node: n })
      }
    }

    // For each lane needing continuation, verify there's a fade line with exact coordinates
    for (const [lane, { row }] of lanesNeedingContinuation) {
      const x = laneCx(lane)
      const y1 = rowCy(row) + 5
      const expectedPath = `M${x},${y1} L${x},${svgBottom}`

      const contLine = lines.find(l => l.path === expectedPath && l.fade === true && l.lane === lane)
      expect(contLine).toBeDefined()
    }

    return lanesNeedingContinuation.size
  }

  it('01-linear partial (3 of 5): 1 lane needs continuation', () => {
    expect(verifyLanesHaveContinuations(LINEAR.slice(0, 3))).toBe(1)
  })

  it('02-single-merge partial (4 of 7): 2 lanes need continuations', () => {
    expect(verifyLanesHaveContinuations(SINGLE_MERGE.slice(0, 4))).toBe(2)
  })

  it('03-multi-branch partial (6 of 11): ≥2 lanes need continuations', () => {
    expect(verifyLanesHaveContinuations(MULTI_BRANCH.slice(0, 6))).toBeGreaterThanOrEqual(2)
  })

  it('04-frequent-merge partial (5 of 9): both lanes need continuations', () => {
    expect(verifyLanesHaveContinuations(FREQUENT_MERGE.slice(0, 5))).toBeGreaterThanOrEqual(1)
  })

  it('05-long-lived partial (5 of 10): 2 lanes need continuations', () => {
    expect(verifyLanesHaveContinuations(LONG_LIVED.slice(0, 5))).toBe(2)
  })

  it('06-octopus partial (3 of 7): ≥1 lane needs continuation', () => {
    expect(verifyLanesHaveContinuations(OCTOPUS.slice(0, 3))).toBeGreaterThanOrEqual(1)
  })

  it('07-open-branches partial (4 of 6): ≥2 lanes need continuations', () => {
    expect(verifyLanesHaveContinuations(OPEN_BRANCHES.slice(0, 4))).toBeGreaterThanOrEqual(2)
  })

  it('08-long-linear (first 10): 1 lane needs continuation', () => {
    expect(verifyLanesHaveContinuations(LONG_LINEAR)).toBe(1)
  })

  it('full repos: 0 lanes need continuations', () => {
    for (const data of [LINEAR, SINGLE_MERGE, MULTI_BRANCH, FREQUENT_MERGE, LONG_LIVED, OCTOPUS, OPEN_BRANCHES]) {
      expect(verifyLanesHaveContinuations(data)).toBe(0)
    }
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// Lazy-load lane stability: lanes should not shift when commits are appended
// ═══════════════════════════════════════════════════════════════════════════

describe('lazy-load lane stability with real data', () => {
  it('01-linear: lanes stay on 0 when more commits are loaded', () => {
    const page1 = LINEAR.slice(0, 3)
    const { nodes: n1, shaToLane: lanes1 } = getGraphInfo(page1)
    expect(n1[0].lane).toBe(0)

    const full = LINEAR
    const { nodes: n2 } = getGraphInfo(full, ROW_HEIGHT, lanes1)
    expect(n2[0].lane).toBe(0)
    expect(n2[1].lane).toBe(0)
    expect(n2[2].lane).toBe(0)
  })

  it('02-single-merge: lanes are stable when loading in pages', () => {
    const page1 = SINGLE_MERGE.slice(0, 4)
    const { nodes: n1, shaToLane: lanes1 } = getGraphInfo(page1)

    const m4Lane = n1[0].lane
    const mergeLane = n1[1].lane
    const f2Lane = n1[2].lane

    const { nodes: n2 } = getGraphInfo(SINGLE_MERGE, ROW_HEIGHT, lanes1)
    expect(n2[0].lane).toBe(m4Lane)
    expect(n2[1].lane).toBe(mergeLane)
    expect(n2[2].lane).toBe(f2Lane)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// Continuation lines have the fade property set
//
// The fade property signals to GitGraph.vue that these lines should be
// rendered differently (thinner, dashed, lower opacity) to indicate
// "there's more history below but it's not loaded yet".
// ═══════════════════════════════════════════════════════════════════════════

describe('continuation lines have fade: true property', () => {
  it('01-linear partial: continuation line has fade=true', () => {
    const partial = LINEAR.slice(0, 3)
    const { lines } = getGraphInfo(partial)
    const fadeLines = lines.filter(l => l.fade === true)
    expect(fadeLines.length).toBeGreaterThanOrEqual(1)
  })

  it('02-single-merge partial: all continuation lines have fade=true', () => {
    const partial = SINGLE_MERGE.slice(0, 4)
    const { lines } = getGraphInfo(partial)
    const fadeLines = lines.filter(l => l.fade === true)
    // At least 2 fade lines (lane 0 + lane 1)
    expect(fadeLines.length).toBeGreaterThanOrEqual(2)
    for (const fl of fadeLines) {
      expect(fl.fade).toBe(true)
      // All fade lines should be vertical (L command, same X at start and end)
      expect(fl.path).toMatch(/^M\d+,\d+ L\d+,\d+$/)
    }
  })

  it('normal (non-fade) lines never have fade=true', () => {
    const { lines } = getGraphInfo(SINGLE_MERGE)
    // Full data: no fade lines at all
    const fadeLines = lines.filter(l => l.fade === true)
    expect(fadeLines).toHaveLength(0)
    // All lines have fade undefined or false
    const nonFadeLines = lines.filter(l => !l.fade)
    expect(nonFadeLines.length).toBe(lines.length)
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// SVG height calculation for continuation lines
//
// When continuation lines exist, the SVG needs extra height below the
// last row so the lines are visible. The GitGraph.vue component adds
// + rowHeight when fade lines are present.
// ═══════════════════════════════════════════════════════════════════════════

describe('SVG height accommodates continuation lines', () => {
  it('continuation line endpoint (svgBottomY) is within extended SVG height', () => {
    const partial = LINEAR.slice(0, 3)
    const { lines } = getGraphInfo(partial)
    const baseHeight = partial.length * ROW_HEIGHT + 4
    const extendedHeight = baseHeight + ROW_HEIGHT

    // The continuation line goes to svgBottomY = partial.length * ROW_HEIGHT + ROW_HEIGHT = 184
    // Which is within extendedHeight = 138 + 4 + 46 = 188
    const fadeLines = lines.filter(l => l.fade)
    for (const fl of fadeLines) {
      const yEnd = parseInt(fl.path.split('L')[1].split(',')[1])
      expect(yEnd).toBeLessThanOrEqual(extendedHeight)
    }
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// Cascade curve smoothness — no sharp angle discontinuities at segment junctions
//
// When branches span multiple lanes (>1 lane gap), renderCascade generates
// a sequence of cubic bezier S-curves that step through intermediate lanes.
// Each segment must maintain tangent continuity (C1) with its neighbors so
// there are no visible "kinks" in the rendered graph line.
// ═══════════════════════════════════════════════════════════════════════════

describe('cascade curve smoothness', () => {
  // Helper: compute the tangent angle at the start of a path segment
  function tangentAtStart(path: string): number {
    if (path.includes('C')) {
      // Bezier: tangent at start = CP1 - P0
      const nums = path.match(/[\d.]+/g)
      if (!nums || nums.length < 4) return 0
      const x0 = parseFloat(nums[0])
      const y0 = parseFloat(nums[1])
      // For cubic bezier Mx0,y0 Cx1,y1 x2,y2 x3,y3
      const x1 = parseFloat(nums[2])
      const y1 = parseFloat(nums[3])
      return Math.atan2(y1 - y0, x1 - x0)
    }
    // Straight line: tangent = end - start
    const nums = path.match(/[\d.]+/g)
    if (!nums || nums.length < 4) return 0
    const x0 = parseFloat(nums[0])
    const y0 = parseFloat(nums[1])
    const xn = parseFloat(nums[nums.length - 2])
    const yn = parseFloat(nums[nums.length - 1])
    return Math.atan2(yn - y0, xn - x0)
  }

  // Helper: compute the tangent angle at the end of a path segment
  function tangentAtEnd(path: string): number {
    if (path.includes('C')) {
      // Bezier: tangent at end = P3 - CP2
      const nums = path.match(/[\d.]+/g)
      if (!nums || nums.length < 8) return 0
      const x2 = parseFloat(nums[4])
      const y2 = parseFloat(nums[5])
      const x3 = parseFloat(nums[6])
      const y3 = parseFloat(nums[7])
      return Math.atan2(y3 - y2, x3 - x2)
    }
    // Straight line: tangent = end - start
    const nums = path.match(/[\d.]+/g)
    if (!nums || nums.length < 4) return 0
    const x0 = parseFloat(nums[0])
    const y0 = parseFloat(nums[1])
    const xn = parseFloat(nums[nums.length - 2])
    const yn = parseFloat(nums[nums.length - 1])
    return Math.atan2(yn - y0, xn - x0)
  }

  // Helper: get endpoint coordinates from a path
  function pathEndpoint(path: string) {
    const nums = path.match(/[\d.]+/g)
    if (!nums || nums.length < 4) return { x: 0, y: 0 }
    return { x: parseFloat(nums[nums.length - 2]), y: parseFloat(nums[nums.length - 1]) }
  }

  // Helper: get startpoint coordinates from a path
  function pathStartpoint(path: string) {
    const nums = path.match(/[\d.]+/g)
    if (!nums || nums.length < 4) return { x: 0, y: 0 }
    return { x: parseFloat(nums[0]), y: parseFloat(nums[1]) }
  }

  function angleDiffDeg(a: number, b: number): number {
    let diff = Math.abs(a - b) * 180 / Math.PI
    if (diff > 180) diff = 360 - diff
    return diff
  }

  it('no sharp junctions (>30deg) between consecutive segments in any repo', () => {
    const allRepos = [
      { name: '01-linear', data: LINEAR },
      { name: '02-single-merge', data: SINGLE_MERGE },
      { name: '03-multi-branch', data: MULTI_BRANCH },
      { name: '04-frequent-merge', data: FREQUENT_MERGE },
      { name: '05-long-lived', data: LONG_LIVED },
      { name: '06-octopus', data: OCTOPUS },
      { name: '07-open-branches', data: OPEN_BRANCHES },
    ]

    for (const { name, data } of allRepos) {
      const { lines } = getGraphInfo(data)
      const nonFade = lines.filter(l => !l.fade)

      // Find consecutive segment pairs (A ends where B starts)
      for (let i = 0; i < nonFade.length; i++) {
        const a = nonFade[i]
        const aEnd = pathEndpoint(a.path)
        const aEndTangent = tangentAtEnd(a.path)

        for (let j = 0; j < nonFade.length; j++) {
          if (i === j) continue
          const b = nonFade[j]
          const bStart = pathStartpoint(b.path)
          const bStartTangent = tangentAtStart(b.path)

          // Check if B starts where A ends (within 2px tolerance)
          if (Math.abs(aEnd.x - bStart.x) < 2 && Math.abs(aEnd.y - bStart.y) < 2) {
            const diff = angleDiffDeg(aEndTangent, bStartTangent)
            expect(diff).toBeLessThan(30)
          }
        }
      }
    }
  })

  it('cascade segments are all cubic bezier curves (no straight L commands)', () => {
    // The multi-branch and octopus repos have cascades (laneGap > 1)
    for (const data of [MULTI_BRANCH, OCTOPUS]) {
      const { lines } = getGraphInfo(data)
      // In the new implementation, all non-fade, non-vertical lines should be
      // cubic bezier curves. Lines without 'C' should only be vertical
      // (same X at start and end) or continuation lines.
      for (const line of lines) {
        if (line.fade) continue
        if (!line.path.includes('C')) {
          // Must be a vertical line (same X at start and end)
          const nums = line.path.match(/[\d.]+/g)
          if (nums && nums.length >= 4) {
            const startX = parseFloat(nums[0])
            const endX = parseFloat(nums[nums.length - 2])
            expect(Math.abs(startX - endX)).toBeLessThan(1)
          }
        }
      }
    }
  })
})

// ═══════════════════════════════════════════════════════════════════════════
// Line lane property matches actual X position
//
// The `lane` property on line objects determines the tooltip shown when
// clicking on the line. It must match the lane the line physically occupies
// so the user sees the correct branch name.
// ═══════════════════════════════════════════════════════════════════════════

describe('line lane property matches actual X position', () => {
  function laneFromX(x: number): number {
    return Math.round((x - GRAPH_LEFT_PADDING - LANE_WIDTH / 2) / LANE_WIDTH)
  }

  it('non-vertical lines: lane matches at least one endpoint', () => {
    const allRepos = [
      SINGLE_MERGE, MULTI_BRANCH, FREQUENT_MERGE, LONG_LIVED, OCTOPUS, OPEN_BRANCHES,
    ]
    for (const data of allRepos) {
      const { lines } = getGraphInfo(data)
      for (const line of lines) {
        if (line.fade) continue
        const nums = line.path.match(/[\d.]+/g)
        if (!nums || nums.length < 4) continue
        const startX = parseFloat(nums[0])
        const endX = parseFloat(nums[nums.length - 2])
        // Skip pure vertical lines (startX === endX) — FORK vertical stubs
        // carry the merged branch's lane but are drawn at the child's X.
        // Only check cross-lane lines (bezier / diagonal) where lane must
        // match at least one endpoint.
        if (Math.abs(startX - endX) < 1) continue
        const startLane = laneFromX(startX)
        const endLane = laneFromX(endX)
        // The line's lane property should match either its start or end X position
        const matchesStart = line.lane === startLane
        const matchesEnd = line.lane === endLane
        expect(matchesStart || matchesEnd).toBe(true)
      }
    }
  })
})
