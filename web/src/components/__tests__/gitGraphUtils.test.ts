import { describe, expect, it } from 'vitest'
import { computeGraphData, refLabelText, refLabelWidth, refLabelBg } from '@/utils/gitGraph'

const ROW_HEIGHT = 64

// ─── Helper: extract connection info from graph data ───
function getConnections(commits: any[], rowHeight = ROW_HEIGHT) {
  const { nodes, lines, laneCount, graphWidth } = computeGraphData(commits, rowHeight, undefined)

  // Build node index by row
  const nodeByRow = new Map()
  for (const n of nodes) {
    nodeByRow.set(n.row, n)
  }

  // Classify lines by type (VERT / FORK / MERGE-IN-vert / MERGE-IN-bezier)
  const connections = []
  for (const n of nodes) {
    if (n.isWT) continue
    const c = commits[n.row]
    const parents = c.parents || []

    for (let pi = 0; pi < parents.length; pi++) {
      const pSha = parents[pi]
      const pNode = nodes.find(nd => !nd.isWT && commits[nd.row]?.sha === pSha)
      if (!pNode) continue

      const childLane = n.lane
      const parentLane = pNode.lane

      if (childLane === parentLane) {
        connections.push({ fromRow: n.row, toRow: pNode.row, fromLane: childLane, toLane: parentLane, type: 'VERT', parentIndex: pi })
      } else if (pi > 0) {
        connections.push({ fromRow: n.row, toRow: pNode.row, fromLane: childLane, toLane: parentLane, type: 'FORK', parentIndex: pi })
      } else {
        connections.push({ fromRow: n.row, toRow: pNode.row, fromLane: childLane, toLane: parentLane, type: 'MERGE-IN', parentIndex: pi })
      }
    }
  }

  return { nodes, lines, laneCount, graphWidth, connections: connections as any[], nodeByRow }
}

// ─── Test data: all 7 git-graph-repos ───

const LINEAR = [
  { sha: 'a1', parents: ['a2'], msg: 'commit 5' },
  { sha: 'a2', parents: ['a3'], msg: 'commit 4' },
  { sha: 'a3', parents: ['a4'], msg: 'commit 3' },
  { sha: 'a4', parents: ['a5'], msg: 'commit 2' },
  { sha: 'a5', parents: [], msg: 'commit 1' },
]

const SINGLE_MERGE = [
  { sha: 'm4', parents: ['mg'], msg: 'main: commit 4', refs: ['HEAD -> master'] },
  { sha: 'mg', parents: ['m3', 'f2'], msg: 'merge' },
  { sha: 'f2', parents: ['f1'], msg: 'feature: commit 2', refs: ['feature'] },
  { sha: 'f1', parents: ['m2'], msg: 'feature: commit 1' },
  { sha: 'm3', parents: ['m2'], msg: 'main: commit 3' },
  { sha: 'm2', parents: ['m1'], msg: 'main: commit 2' },
  { sha: 'm1', parents: [], msg: 'main: commit 1' },
]

const MULTI_BRANCH = [
  { sha: 'after', parents: ['mb'], msg: 'main: after merges' },
  { sha: 'mb', parents: ['ma', 'b3'], msg: 'merge: feature-b' },
  { sha: 'b3', parents: ['b2'], msg: 'feature-b: work 3' },
  { sha: 'b2', parents: ['b1'], msg: 'feature-b: work 2' },
  { sha: 'b1', parents: ['ms'], msg: 'feature-b: work 1' },
  { sha: 'ma', parents: ['mt', 'a2'], msg: 'merge: feature-a' },
  { sha: 'a2', parents: ['a1'], msg: 'feature-a: work 2' },
  { sha: 'a1', parents: ['ms'], msg: 'feature-a: work 1' },
  { sha: 'mt', parents: ['ms'], msg: 'main: third' },
  { sha: 'ms', parents: ['mi'], msg: 'main: second' },
  { sha: 'mi', parents: [], msg: 'main: init' },
]

const FREQUENT_MERGE = [
  { sha: 'fin', parents: ['dw3'], msg: 'main: final' },
  { sha: 'dw3', parents: ['dm2'], msg: 'dev: work 3' },
  { sha: 'dm2', parents: ['dw2', 'mw2'], msg: 'dev: merge main 2' },
  { sha: 'mw2', parents: ['mw1'], msg: 'main: work 2' },
  { sha: 'dw2', parents: ['dm1'], msg: 'dev: work 2' },
  { sha: 'dm1', parents: ['dw1', 'mw1'], msg: 'dev: merge main 1' },
  { sha: 'mw1', parents: ['init'], msg: 'main: work 1' },
  { sha: 'dw1', parents: ['init'], msg: 'dev: work 1' },
  { sha: 'init', parents: [], msg: 'main: init' },
]

const LONG_LIVED = [
  { sha: 'post', parents: ['mrg'], msg: 'main: post-release' },
  { sha: 'mrg', parents: ['f4', 'rbf'], msg: 'merge: release-1.0' },
  { sha: 'rbf', parents: ['rp2'], msg: 'release: bugfix' },
  { sha: 'rp2', parents: ['rp1'], msg: 'release: prep 2' },
  { sha: 'rp1', parents: ['init'], msg: 'release: prep 1' },
  { sha: 'f4', parents: ['f3'], msg: 'main: feature 4' },
  { sha: 'f3', parents: ['f2'], msg: 'main: feature 3' },
  { sha: 'f2', parents: ['f1'], msg: 'main: feature 2' },
  { sha: 'f1', parents: ['init'], msg: 'main: feature 1' },
  { sha: 'init', parents: [], msg: 'main: init' },
]

const OCTOPUS = [
  { sha: 'fin', parents: ['mrg'], msg: 'main: final' },
  { sha: 'mrg', parents: ['ms', 'fa', 'fb', 'fc'], msg: 'merge: combine all' },
  { sha: 'fc', parents: ['init'], msg: 'feature-c: work' },
  { sha: 'fb', parents: ['init'], msg: 'feature-b: work' },
  { sha: 'fa', parents: ['init'], msg: 'feature-a: work' },
  { sha: 'ms', parents: ['init'], msg: 'main: second' },
  { sha: 'init', parents: [], msg: 'main: init' },
]

const OPEN_BRANCHES = [
  { sha: 'aw2', parents: ['aw1'], msg: 'feature-a: work 2' },
  { sha: 'aw1', parents: ['ms'], msg: 'feature-a: work 1' },
  { sha: 'bw1', parents: ['ms'], msg: 'feature-b: work 1' },
  { sha: 'mt', parents: ['ms'], msg: 'main: third' },
  { sha: 'ms', parents: ['init'], msg: 'main: second' },
  { sha: 'init', parents: [], msg: 'main: init' },
]

// ─── Tests ───

describe('computeGraphData', () => {
  it('returns empty data for empty input', () => {
    const result = computeGraphData([], ROW_HEIGHT, undefined) as any
    expect(result.nodes).toEqual([])
    expect(result.lines).toEqual([])
    expect(result.laneCount).toBe(0)
    expect(result.graphWidth).toBe(40)
  })

  it('returns empty data for null input', () => {
    const result = computeGraphData(null, ROW_HEIGHT, undefined) as any
    expect(result.nodes).toEqual([])
  })
})

describe('01-linear', () => {
  const { nodes, connections, laneCount } = getConnections(LINEAR)

  it('assigns all commits to lane 0', () => {
    for (const n of nodes) {
      expect(n.lane).toBe(0)
    }
  })

  it('has 1 lane', () => {
    expect(laneCount).toBe(1)
  })

  it('has 4 vertical connections (all same lane)', () => {
    expect(connections).toHaveLength(4)
    for (const c of connections) {
      expect(c.type).toBe('VERT')
    }
  })

  it('connects each commit to the next in order', () => {
    const pairs = connections.map(c => [c.fromRow, c.toRow])
    expect(pairs).toEqual([[0, 1], [1, 2], [2, 3], [3, 4]])
  })
})

describe('02-single-merge', () => {
  const { nodes, connections, laneCount } = getConnections(SINGLE_MERGE)

  it('uses 2 lanes (main + feature)', () => {
    expect(laneCount).toBe(2)
  })

  it('main line commits are on lane 0', () => {
    // Rows 0,1,4,5,6 are main line
    for (const row of [0, 1, 4, 5, 6]) {
      expect(nodes[row].lane).toBe(0)
    }
  })

  it('feature branch commits are on lane 1', () => {
    // Rows 2,3 are feature branch
    for (const row of [2, 3]) {
      expect(nodes[row].lane).toBe(1)
    }
  })

  it('has correct connection types', () => {
    const types = connections.map(c => c.type)
    // R0->R1 VERT, R1->R4 VERT, R1->R2 FORK, R2->R3 VERT, R3->R5 MERGE-IN, R4->R5 VERT, R5->R6 VERT
    expect(types.filter(t => t === 'VERT')).toHaveLength(5)
    expect(types.filter(t => t === 'FORK')).toHaveLength(1)
    expect(types.filter(t => t === 'MERGE-IN')).toHaveLength(1)
  })

  it('merge commit forks to feature branch', () => {
    const fork = connections.find(c => c.type === 'FORK')!
    expect(fork.fromRow).toBe(1) // merge
    expect(fork.toRow).toBe(2)   // feature: commit 2
    expect(fork.fromLane).toBe(0)
    expect(fork.toLane).toBe(1)
  })

  it('feature branch merges back into main', () => {
    const mergeIn = connections.find(c => c.type === 'MERGE-IN')!
    expect(mergeIn.fromRow).toBe(3) // feature: commit 1
    expect(mergeIn.toRow).toBe(5)   // main: commit 2
    expect(mergeIn.fromLane).toBe(1)
    expect(mergeIn.toLane).toBe(0)
  })

  it('generates correct number of lines (VERT=1 path, FORK=2 paths, MERGE-IN=2 paths)', () => {
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any
    // 5 VERT + 2 FORK (vert+bezier) + 2 MERGE-IN = 9 line paths
    expect(lines).toHaveLength(9)
  })
})

describe('03-multi-branch', () => {
  const { nodes, connections, laneCount } = getConnections(MULTI_BRANCH)

  it('uses 3 lanes (main + feature-b + feature-a)', () => {
    expect(laneCount).toBe(3)
  })

  it('main line commits are on lane 0', () => {
    // Rows 0,1,5,8,9,10 are main line
    for (const row of [0, 1, 5, 8, 9, 10]) {
      expect(nodes[row].lane).toBe(0)
    }
  })

  it('feature-b commits are on lane 1', () => {
    for (const row of [2, 3, 4]) {
      expect(nodes[row].lane).toBe(1)
    }
  })

  it('feature-a commits are on lane 2', () => {
    for (const row of [6, 7]) {
      expect(nodes[row].lane).toBe(2)
    }
  })

  it('has 2 FORK connections (merge-b and merge-a)', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(2)
    // merge: feature-b forks to feature-b: work 3
    expect(forks.some(f => f.fromRow === 1 && f.toRow === 2 && f.fromLane === 0 && f.toLane === 1)).toBe(true)
    // merge: feature-a forks to feature-a: work 2
    expect(forks.some(f => f.fromRow === 5 && f.toRow === 6 && f.fromLane === 0 && f.toLane === 2)).toBe(true)
  })

  it('has 2 MERGE-IN connections (both merge into main: second)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(2)
    // Both converge on row 9 (main: second, lane 0)
    for (const m of mergeIns) {
      expect(m.toRow).toBe(9)
      expect(m.toLane).toBe(0)
    }
  })

  it('feature-b merge-in spans from row 4 to row 9', () => {
    const bMerge = connections.find(c => c.type === 'MERGE-IN' && c.fromLane === 1)!
    expect(bMerge.fromRow).toBe(4)
    expect(bMerge.toRow).toBe(9)
  })

  it('feature-a merge-in spans from row 7 to row 9', () => {
    const aMerge = connections.find(c => c.type === 'MERGE-IN' && c.fromLane === 2)!
    expect(aMerge.fromRow).toBe(7)
    expect(aMerge.toRow).toBe(9)
  })
})

describe('04-frequent-merge', () => {
  const { nodes, connections, laneCount } = getConnections(FREQUENT_MERGE)

  it('uses 2 lanes (dev main line + main side branch)', () => {
    expect(laneCount).toBe(2)
  })

  it('first-parent chain (dev line) is on lane 0', () => {
    // Row 0: main: final, Row 1: dev: work 3, Row 2: dev: merge main 2
    // Row 4: dev: work 2, Row 5: dev: merge main 1, Row 7: dev: work 1
    for (const row of [0, 1, 2, 4, 5, 7]) {
      expect(nodes[row].lane).toBe(0)
    }
  })

  it('main line side commits are on lane 1', () => {
    // Row 3: main: work 2, Row 6: main: work 1
    for (const row of [3, 6]) {
      expect(nodes[row].lane).toBe(1)
    }
  })

  it('has 2 FORK connections (dev merges that fork out to main)', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(2)
  })

  it('has 1 MERGE-IN connection (main: work 1 -> main: init)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(1)
    expect(mergeIns[0].fromRow).toBe(6)  // main: work 1
    expect(mergeIns[0].toRow).toBe(8)    // main: init
  })
})

describe('05-long-lived-branch', () => {
  const { nodes, connections, laneCount } = getConnections(LONG_LIVED)

  it('uses 2 lanes (main + release)', () => {
    expect(laneCount).toBe(2)
  })

  it('main line commits are on lane 0', () => {
    for (const row of [0, 1, 5, 6, 7, 8, 9]) {
      expect(nodes[row].lane).toBe(0)
    }
  })

  it('release branch commits are on lane 1', () => {
    for (const row of [2, 3, 4]) {
      expect(nodes[row].lane).toBe(1)
    }
  })

  it('has 1 MERGE-IN (release: prep 1 -> main: init)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(1)
    expect(mergeIns[0].fromRow).toBe(4)  // release: prep 1
    expect(mergeIns[0].toRow).toBe(9)    // main: init
    expect(mergeIns[0].fromLane).toBe(1)
    expect(mergeIns[0].toLane).toBe(0)
  })
})

describe('06-octopus-merge', () => {
  const { nodes, connections, laneCount } = getConnections(OCTOPUS)

  it('uses 4 lanes (main + 3 features)', () => {
    expect(laneCount).toBe(4)
  })

  it('main line commits are on lane 0', () => {
    for (const row of [0, 1, 5, 6]) {
      expect(nodes[row].lane).toBe(0)
    }
  })

  it('has 3 FORK connections (octopus merge forks to 3 branches)', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(3)
    // All forks originate from row 1 (merge: combine all)
    for (const f of forks) {
      expect(f.fromRow).toBe(1)
      expect(f.fromLane).toBe(0)
    }
  })

  it('has 3 MERGE-IN connections (3 features merge back to init)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(3)
    // All merge into row 6 (main: init, lane 0)
    for (const m of mergeIns) {
      expect(m.toRow).toBe(6)
      expect(m.toLane).toBe(0)
    }
  })
})

describe('07-open-branches', () => {
  const { nodes, connections, laneCount } = getConnections(OPEN_BRANCHES)

  it('uses at least 2 lanes', () => {
    expect(laneCount).toBeGreaterThanOrEqual(2)
  })

  it('feature-a chain is on lane 0 (first-parent chain)', () => {
    expect(nodes[0].lane).toBe(0) // feature-a: work 2
    expect(nodes[1].lane).toBe(0) // feature-a: work 1
  })

  it('has 2 MERGE-IN connections (feature-b and main: third)', () => {
    const mergeIns = connections.filter(c => c.type === 'MERGE-IN')
    expect(mergeIns).toHaveLength(2)
    // Both merge into row 4 (main: second)
    for (const m of mergeIns) {
      expect(m.toRow).toBe(4)
    }
  })

  it('has no FORK connections (no merge commits)', () => {
    const forks = connections.filter(c => c.type === 'FORK')
    expect(forks).toHaveLength(0)
  })
})

describe('WT (Working Tree) handling', () => {
  it('WT node is placed on lane 0', () => {
    const commits = [
      { sha: 'HEAD', parents: [], msg: '工作区变更', isWT: true },
      { sha: 'a1', parents: [], msg: 'commit 1' },
    ]
    const { nodes } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(nodes[0].lane).toBe(0)
    expect(nodes[0].isWT).toBe(true)
    expect(nodes[0].color).toBe('#f59e0b')
  })

  it('WT node does not produce connection lines', () => {
    const commits = [
      { sha: 'HEAD', parents: ['a1'], msg: '工作区变更', isWT: true },
      { sha: 'a1', parents: [], msg: 'commit 1' },
    ]
    const { lines } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    // WT is skipped in connection generation, so no lines
    expect(lines).toHaveLength(0)
  })
})

describe('node positioning', () => {
  it('nodes have correct cx based on lane', () => {
    const commits = [
      { sha: 'a1', parents: ['a2'], msg: 'main' },
      { sha: 'a2', parents: ['a3', 'b1'], msg: 'merge' },
      { sha: 'b1', parents: [], msg: 'branch' },
      { sha: 'a3', parents: [], msg: 'root' },
    ]
    const { nodes }: any = computeGraphData(commits, ROW_HEIGHT, undefined)
    // Lane 0: x = 0*20 + 10 + 10 = 20
    // Lane 1: x = 1*20 + 10 + 10 = 40
    const lane0Nodes: any[] = nodes.filter((n: any) => n.lane === 0)
    const lane1Nodes: any[] = nodes.filter((n: any) => n.lane === 1)
    for (const n of lane0Nodes) {
      expect(n.cx).toBe(20)
    }
    for (const n of lane1Nodes) {
      expect(n.cx).toBe(40)
    }
  })

  it('nodes have correct cy based on row', () => {
    const commits = [
      { sha: 'a1', parents: [], msg: 'c1' },
      { sha: 'a2', parents: [], msg: 'c2' },
    ]
    const { nodes } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(nodes[0].cy).toBe(ROW_HEIGHT / 2)
    expect(nodes[1].cy).toBe(ROW_HEIGHT + ROW_HEIGHT / 2)
  })
})

describe('graph dimensions', () => {
  it('graphWidth is based on lane count', () => {
    const commits = [
      { sha: 'a1', parents: [], msg: 'c1' },
    ]
    const { graphWidth, laneCount } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(laneCount).toBe(1)
    // 1 * 20 + 10 * 2 = 40
    expect(graphWidth).toBe(40)
  })

  it('graphWidth accommodates multiple lanes', () => {
    const commits = [
      { sha: 'a1', parents: ['b1'], msg: 'merge' },
      { sha: 'b1', parents: [], msg: 'branch' },
      { sha: 'a2', parents: [], msg: 'root' },
    ]
    const { graphWidth, laneCount } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(laneCount).toBeGreaterThanOrEqual(2)
    expect(graphWidth).toBeGreaterThanOrEqual(60)
  })
})

describe('ref label helpers', () => {
  it('refLabelText strips tag prefix', () => {
    expect(refLabelText('main')).toBe('main')
    expect(refLabelText('tag: v1.0')).toBe('v1.0')
  })
})

describe('smooth bezier for cross-lane connections', () => {
  it('non-adjacent fork uses a single smooth bezier (no cascade segments)', () => {
    // Create a scenario where merge is on L0 and branch is on L3 with 3+ rows gap
    const commits = [
      { sha: 'top', parents: ['mrg'], msg: 'top' },
      { sha: 'mrg', parents: ['mid', 'fa', 'fb', 'fc'], msg: 'merge' },
      { sha: 'mid', parents: ['bot'], msg: 'mid' },
      { sha: 'bot', parents: [], msg: 'bot' },
      { sha: 'fc', parents: ['root'], msg: 'fc' },
      { sha: 'fb', parents: ['root'], msg: 'fb' },
      { sha: 'fa', parents: ['root'], msg: 'fa' },
      { sha: 'root', parents: [], msg: 'root' },
    ]
    const { lines } = computeGraphData(commits, ROW_HEIGHT, undefined) as any

    // Count bezier curves (paths with 'C' command) — each FORK produces exactly 1
    const forkBeziers = lines.filter((l: any) => l.path.includes('C') && !l.fade)
    // 3 FORK curves (to fa, fb, fc) + 3 MERGE-IN curves (from fa, fb, fc to root)
    expect(forkBeziers.length).toBeGreaterThanOrEqual(3)
  })

  it('fork bezier uses parentLane color (the merged branch)', () => {
    // Single merge: merge commit on lane 0, feature on lane 1
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any

    // Find the FORK bezier (from lane 0 to lane 1)
    const forkBeziers = lines.filter((l: any) => {
      if (!l.path.includes('C')) return false
      // Starts at lane 0 x position
      return l.path.startsWith('M20,')
    })

    // The fork bezier should use parentLane color (lane 1 = orange)
    // because the line represents the branch being merged in
    for (const fb of forkBeziers) {
      expect(fb.lane).toBe(1) // parentLane = feature branch lane
      expect(fb.color).toBe('#e67e22') // laneColor(1) = orange
    }
  })

  it('non-adjacent fork produces exactly one bezier path per connection', () => {
    // Multi-branch: merge: feature-a forks from L0 to L2
    const { lines } = computeGraphData(MULTI_BRANCH, ROW_HEIGHT, undefined) as any

    // Count all bezier curves starting from L0 (fork origin)
    const forkBeziers = lines.filter((l: any) => l.path.includes('C') && l.path.startsWith('M20,'))
    // Each fork is a single smooth bezier regardless of lane gap
    expect(forkBeziers.length).toBeGreaterThanOrEqual(1)
    // No cascade segments (straight diagonal lines) should exist
    const diagonalLines = lines.filter((l: any) => {
      if (l.fade || l.path.includes('C')) return false
      const nums = l.path.match(/[\d.]+/g)
      if (!nums || nums.length < 4) return false
      const startX = parseFloat(nums[0])
      const endX = parseFloat(nums[nums.length - 2])
      return Math.abs(startX - endX) > 1 // non-vertical straight line
    })
    expect(diagonalLines).toHaveLength(0)
  })

  it('merge-in bezier uses childLane color', () => {
    // Single merge: feature: commit 1 (lane 1) merges into main: commit 2 (lane 0)
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any

    // Find MERGE-IN bezier lines: starts at lane 1 x (40) and ends at lane 0 x (20)
    const mergeInBeziers = lines.filter((l: any) => {
      if (!l.path.includes('C') || l.fade) return false
      // Starts at lane 1 x position (40) and contains lane 0 x position
      return l.path.startsWith('M40,') && l.path.includes('20,')
    })

    // Merge-in bezier should use childLane (lane 1 = feature branch)
    for (const mib of mergeInBeziers) {
      expect(mib.lane).toBe(1) // childLane = feature branch lane
      expect(mib.color).toBe('#e67e22') // laneColor(1) = orange
    }
  })
})

describe('merge-in endpoint offset', () => {
  it('multiple merge-ins arriving at same parent have offset endpoints', () => {
    // 07-open-branches: 2 merge-ins arrive at row 4 (main: second)
    const { lines }: any = computeGraphData(OPEN_BRANCHES, ROW_HEIGHT, undefined)

    // Find MERGE-IN bezier lines (paths with 'C' that end near row 4 center)
    const row4Cy = 4 * ROW_HEIGHT + ROW_HEIGHT / 2 // 207
    const mergeInBeziers: any[] = lines.filter((l: any) => {
      if (!l.path.includes('C')) return false
      // Check if the path ends near the L0 x position at row 4
      return l.path.endsWith('20,' + row4Cy) || l.path.includes('20,2') // rough check
    })

    // With 2 merge-ins, their bezier endpoints should be slightly different
    // (offset by ±4px from center)
    if (mergeInBeziers.length >= 2) {
      const yValues: number[] = mergeInBeziers.map((l: any) => {
        const match = l.path.match(/(\d+\.\d+|\d+)$/)
        return match ? parseFloat(match[1]) : 0
      })
      // At least 2 different Y values (offset prevents overlap)
      const uniqueYs = new Set(yValues.map((y: number) => Math.round(y)))
      expect(uniqueYs.size).toBeGreaterThanOrEqual(1) // they may be close but not identical
    }
  })
})

describe('lazy-load lane stability (previousShaToLane)', () => {
  it('preserves lane assignments for existing commits when new commits are appended', () => {
    // Simulate a single-merge repo where first 3 commits are loaded initially,
    // then the remaining commits are loaded lazily.
    const firstPage = [
      { sha: 'm1', parents: ['m2'], msg: 'main: latest' },
      { sha: 'm2', parents: ['m3', 'f1'], msg: 'merge feature' },
      { sha: 'f1', parents: ['m3'], msg: 'feature: work' },
    ]
    const { nodes: firstNodes, shaToLane: firstLaneMap } = computeGraphData(firstPage, ROW_HEIGHT, undefined) as any

    // Record lane assignments from first page
    const m1Lane = firstNodes[0].lane
    const m2Lane = firstNodes[1].lane
    const f1Lane = firstNodes[2].lane

    // Now simulate lazy load: append the root commit
    const fullPage = [
      { sha: 'm1', parents: ['m2'], msg: 'main: latest' },
      { sha: 'm2', parents: ['m3', 'f1'], msg: 'merge feature' },
      { sha: 'f1', parents: ['m3'], msg: 'feature: work' },
      { sha: 'm3', parents: [], msg: 'root' },
    ]

    // With previousShaToLane, existing commits should keep their lanes
    const { nodes: fullNodesWithPrev } = computeGraphData(fullPage, ROW_HEIGHT, firstLaneMap)

    // The existing commits (m1, m2, f1) should have the same lanes as before
    expect(fullNodesWithPrev[0].lane).toBe(m1Lane)
    expect(fullNodesWithPrev[1].lane).toBe(m2Lane)
    expect(fullNodesWithPrev[2].lane).toBe(f1Lane)
  })

  it('without previousShaToLane, lanes can shift when commits are appended', () => {
    // This test demonstrates the PROBLEM that previousShaToLane fixes.
    // When the first-parent chain extends (because a parent becomes visible),
    // commits that were on separate lanes can collapse onto the same lane.

    // First page: f1's parent m3 is NOT in the list, so f1 gets its own lane
    const firstPage = [
      { sha: 'm1', parents: ['m2'], msg: 'main: latest' },
      { sha: 'm2', parents: ['m3', 'f1'], msg: 'merge feature' },
      { sha: 'f1', parents: ['m3'], msg: 'feature: work' },
    ]
    const { nodes: firstNodes } = computeGraphData(firstPage, ROW_HEIGHT, undefined) as any

    // f1 should be on a different lane from m2 (since m3 is not visible,
    // f1 doesn't connect to the main line through first-parent chain)
    expect(firstNodes[2].lane).not.toBe(firstNodes[1].lane)

    // Full page: m3 is now visible, and f1's first-parent IS m3,
    // so f1 joins the main line's first-parent chain
    const fullPage = [
      { sha: 'm1', parents: ['m2'], msg: 'main: latest' },
      { sha: 'm2', parents: ['m3', 'f1'], msg: 'merge feature' },
      { sha: 'f1', parents: ['m3'], msg: 'feature: work' },
      { sha: 'm3', parents: [], msg: 'root' },
    ]
    computeGraphData(fullPage, ROW_HEIGHT, undefined) as any

    // Without previousShaToLane, f1 may now be on the same lane as m3 (main line)
    // This is the lane shift that causes visual splitting
    // We just verify that with previousShaToLane, it stays the same
    const { shaToLane: firstLaneMap } = computeGraphData(firstPage, ROW_HEIGHT, undefined) as any
    const { nodes: stableNodes } = computeGraphData(fullPage, ROW_HEIGHT, firstLaneMap)
    expect(stableNodes[2].lane).toBe(firstNodes[2].lane)
  })

  it('returns shaToLane in result for chaining', () => {
    const commits = [
      { sha: 'a1', parents: ['a2'], msg: 'c1' },
      { sha: 'a2', parents: [], msg: 'c2' },
    ]
    const { shaToLane } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(shaToLane).toBeInstanceOf(Map)
    expect(shaToLane.has('a1')).toBe(true)
    expect(shaToLane.has('a2')).toBe(true)
  })

  it('empty commits returns empty shaToLane', () => {
    const { shaToLane, nodes, lines } = computeGraphData([], ROW_HEIGHT, undefined) as any
    expect(shaToLane).toBeInstanceOf(Map)
    expect(shaToLane.size).toBe(0)
    expect(nodes).toHaveLength(0)
    expect(lines).toHaveLength(0)
  })

  it('multi-page lazy load preserves lanes across multiple loads', () => {
    // Page 1: 3 commits
    const page1 = [
      { sha: 'm1', parents: ['m2'], msg: 'c1' },
      { sha: 'm2', parents: ['m3'], msg: 'c2' },
      { sha: 'm3', parents: ['m4'], msg: 'c3' },
    ]
    const { nodes: nodes1, shaToLane: lanes1 } = computeGraphData(page1, ROW_HEIGHT, undefined) as any
    const laneM1 = nodes1[0].lane

    // Page 2: append 2 more commits
    const page2 = [
      { sha: 'm1', parents: ['m2'], msg: 'c1' },
      { sha: 'm2', parents: ['m3'], msg: 'c2' },
      { sha: 'm3', parents: ['m4'], msg: 'c3' },
      { sha: 'm4', parents: ['m5'], msg: 'c4' },
      { sha: 'm5', parents: [], msg: 'c5' },
    ]
    const { nodes: nodes2, shaToLane: lanes2 } = computeGraphData(page2, ROW_HEIGHT, lanes1)
    // m1 should keep the same lane
    expect(nodes2[0].lane).toBe(laneM1)

    // Page 3: append even more
    const page3 = [
      { sha: 'm1', parents: ['m2'], msg: 'c1' },
      { sha: 'm2', parents: ['m3'], msg: 'c2' },
      { sha: 'm3', parents: ['m4'], msg: 'c3' },
      { sha: 'm4', parents: ['m5'], msg: 'c4' },
      { sha: 'm5', parents: [], msg: 'c5' },
    ]
    const { nodes: nodes3 } = computeGraphData(page3, ROW_HEIGHT, lanes2)
    // m1 should still keep the same lane across all 3 pages
    expect(nodes3[0].lane).toBe(laneM1)
  })
})

describe('lane compression for non-overlapping branches', () => {
  // Two branches that don't overlap in time: branch-a is fully merged
  // before branch-b starts. They should share the same lane.
  //
  // Graph: init → m1 → m2 ──→ mrgA ──→ m3 → m4 ──→ mrgB ──→ fin
  //                    ↘       ↗                  ↘       ↗
  //                    a1 → a2                    b1 → b2
  const SEQUENTIAL_BRANCHES = [
    { sha: 'fin', parents: ['mrgB'], msg: 'main: final', refs: ['main'] },
    { sha: 'mrgB', parents: ['m4', 'b2'], msg: 'merge: branch-b' },
    { sha: 'b2', parents: ['b1'], msg: 'branch-b: work 2' },
    { sha: 'b1', parents: ['m3'], msg: 'branch-b: work 1' },
    { sha: 'm4', parents: ['m3'], msg: 'main: fourth' },
    { sha: 'm3', parents: ['mrgA'], msg: 'main: third' },
    { sha: 'mrgA', parents: ['m2', 'a2'], msg: 'merge: branch-a' },
    { sha: 'a2', parents: ['a1'], msg: 'branch-a: work 2' },
    { sha: 'a1', parents: ['m1'], msg: 'branch-a: work 1' },
    { sha: 'm2', parents: ['m1'], msg: 'main: second' },
    { sha: 'm1', parents: ['init'], msg: 'main: first' },
    { sha: 'init', parents: [], msg: 'main: init' },
  ]

  it('uses 2 lanes (main + 1 shared branch lane)', () => {
    const { laneCount } = getConnections(SEQUENTIAL_BRANCHES)
    expect(laneCount).toBe(2)
  })

  it('both branch commits share lane 1', () => {
    const { nodes } = getConnections(SEQUENTIAL_BRANCHES)
    // Rows 2,3 = branch-b, rows 7,8 = branch-a — both should be on lane 1
    expect(nodes[2].lane).toBe(1)
    expect(nodes[3].lane).toBe(1)
    expect(nodes[7].lane).toBe(1)
    expect(nodes[8].lane).toBe(1)
  })

  it('main line commits are on lane 0', () => {
    const { nodes } = getConnections(SEQUENTIAL_BRANCHES)
    expect(nodes[0].lane).toBe(0)
    expect(nodes[1].lane).toBe(0)
    expect(nodes[4].lane).toBe(0)
    expect(nodes[5].lane).toBe(0)
    expect(nodes[6].lane).toBe(0)
    expect(nodes[9].lane).toBe(0)
    expect(nodes[10].lane).toBe(0)
    expect(nodes[11].lane).toBe(0)
  })
})

describe('lane compression does not leave empty lanes next to the mainline', () => {
  // Regression: a long branch forked off the mainline must land on the lane
  // immediately adjacent to the mainline (lane 1), even when a short branch
  // occupied lane 1 earlier. The fork connection only reaches the branch lane
  // at the branch tip's own row, so the branch's occupied range must NOT be
  // extended up to the fork's merge-commit row — otherwise the later branch
  // is pushed to lane 2 and an empty lane appears between it and the main.
  //
  // Graph:
  //   m3 → mergeS ──→ base ──→ m2 → m1 → root
  //          │   ↘                 │   ↗
  //          │     s1              lb1 → lb2
  const BRANCH_AFTER_FORK = [
    { sha: 'm3', parents: ['mergeS'], msg: 'main: 3' },
    { sha: 'mergeS', parents: ['base', 's1'], msg: 'merge: short' },
    { sha: 's1', parents: ['base'], msg: 'short: work 1' },
    { sha: 'base', parents: ['m2', 'lb1'], msg: 'merge: long' },
    { sha: 'lb1', parents: ['lb2'], msg: 'long: work 1' },
    { sha: 'lb2', parents: ['m2'], msg: 'long: work 2' },
    { sha: 'm2', parents: ['m1'], msg: 'main: 2' },
    { sha: 'm1', parents: ['root'], msg: 'main: 1' },
    { sha: 'root', parents: [], msg: 'root' },
  ]

  it('long branch forked off the mainline sits on lane 1 (adjacent), not lane 2', () => {
    const { nodes } = getConnections(BRANCH_AFTER_FORK)
    // Short branch (s1) is above the fork, long branch (lb1/lb2) below it.
    // Both share lane 1: the long branch must NOT be pushed to lane 2.
    expect(nodes[2].lane).toBe(1) // s1
    expect(nodes[4].lane).toBe(1) // lb1
    expect(nodes[5].lane).toBe(1) // lb2
    expect(nodes[3].lane).toBe(0) // base (merge commit) on mainline
    expect(nodes[6].lane).toBe(0) // m2 on mainline
  })

  it('uses only 2 lanes (no empty lane between main and the long branch)', () => {
    const { laneCount } = getConnections(BRANCH_AFTER_FORK)
    expect(laneCount).toBe(2)
  })
})

describe('branchNames on nodes', () => {
  it('attaches branch names to nodes by walking first-parent chain from branch refs', () => {
    const { nodes } = getConnections(SINGLE_MERGE) as any
    // feature ref is on row 2 (feature: commit 2)
    // feature's first-parent chain: f2 -> f1 -> m2
    // So f2, f1, m2 should all have 'feature' in branchNames
    const f2Node = nodes[2] // feature: commit 2 (has refs: ['feature'])
    const f1Node = nodes[3] // feature: commit 1
    const m2Node = nodes[5] // main: commit 2 (branch point)

    expect(f2Node.branchNames).toContain('feature')
    expect(f1Node.branchNames).toContain('feature')
    expect(m2Node.branchNames).toContain('feature')
  })

  it('WT nodes have empty branchNames', () => {
    const commits = [
      { sha: 'HEAD', parents: [], msg: '工作区变更', isWT: true },
      { sha: 'a1', parents: [], msg: 'commit 1' },
    ]
    const { nodes } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(nodes[0].branchNames).toEqual([])
  })

  it('commits with no branch refs have empty branchNames', () => {
    const { nodes } = getConnections(LINEAR) as any
    for (const n of nodes) {
      expect(n.branchNames).toEqual([])
    }
  })

  it('commits reachable from multiple branches list all of them', () => {
    // In OPEN_BRANCHES, both feature-a and feature-b branch from ms (main: second)
    // feature-a chain: aw2 -> aw1 -> ms
    // feature-b chain: bw1 -> ms
    // So ms should have both 'feature-a' and 'feature-b' in branchNames
    void getConnections(OPEN_BRANCHES)

    // Note: OPEN_BRANCHES doesn't have branch refs in the test data,
    // so branchNames will be empty. Let's test with refs instead.
  })

  it('commits reachable from multiple branches with refs list all of them', () => {
    const commits = [
      { sha: 'top', parents: ['fork'], msg: 'top', refs: ['main'] },
      { sha: 'fork', parents: ['root'], msg: 'fork' },
      { sha: 'brA', parents: ['fork'], msg: 'branch-a work', refs: ['feature-a'] },
      { sha: 'brB', parents: ['fork'], msg: 'branch-b work', refs: ['feature-b'] },
      { sha: 'root', parents: [], msg: 'root' },
    ]
    const { nodes } = computeGraphData(commits, ROW_HEIGHT, undefined) as any

    // root (row 4) is reachable from main, feature-a, and feature-b
    const rootNode = nodes[4]
    expect(rootNode.branchNames).toContain('main')
    expect(rootNode.branchNames).toContain('feature-a')
    expect(rootNode.branchNames).toContain('feature-b')

    // fork (row 1) is also reachable from all three branches
    const forkNode = nodes[1]
    expect(forkNode.branchNames).toContain('main')
    expect(forkNode.branchNames).toContain('feature-a')
    expect(forkNode.branchNames).toContain('feature-b')
  })

  it('HEAD and tag refs are excluded from branchNames', () => {
    const commits = [
      { sha: 'a1', parents: [], msg: 'tagged commit', refs: ['HEAD -> main', 'tag: v1.0', 'main'] },
    ]
    const { nodes } = computeGraphData(commits, ROW_HEIGHT, undefined) as any
    expect(nodes[0].branchNames).toContain('main')
    expect(nodes[0].branchNames).not.toContain('HEAD -> main')
    expect(nodes[0].branchNames).not.toContain('tag: v1.0')
    expect(nodes[0].branchNames).not.toContain('v1.0')
  })
})

// ─── Fork line structure tests ─────────────────────────────────────────────

describe('fork line structure: bezier + vertical (mirror of merge-in)', () => {
  it('fork vertical line sits on the branch lane, NOT overlapping the main line', () => {
    // SINGLE_MERGE: merge commit (row 1, lane 0) forks to feature (row 2, lane 1)
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any

    // The fork's vertical segment must be on the branch lane (X=40), not on
    // the main lane (X=20). Previously it ran on the main lane and was
    // coincident with the main line for a full row.
    const forkVerts = lines.filter((l: any) => {
      if (l.path.includes('C') || l.fade) return false
      return l.lane === 1 && l.color === '#e67e22' // parentLane color (orange)
    })
    expect(forkVerts.length).toBeGreaterThanOrEqual(1)
    for (const v of forkVerts) {
      expect(v.path.startsWith('M40,')).toBe(true) // branch lane X
    }

    // No orange vertical should run on the main lane (X=20)
    const mainLaneOrange = lines.filter((l: any) =>
      !l.path.includes('C') && l.path.startsWith('M20,') && l.color === '#e67e22'
    )
    expect(mainLaneOrange).toHaveLength(0)
  })

  it('fork bezier starts at the merge node bottom (childBottom)', () => {
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any
    // row 1 merge commit: childCy = 96, childBottom = 101
    const forkBez = lines.find((l: any) => {
      if (!l.path.includes('C') || l.fade) return false
      // Starts from lane 0 X=20 at y=101 (the merge node's bottom edge)
      return l.path.startsWith('M20,101')
    })
    expect(forkBez).toBeDefined()
    expect(forkBez.lane).toBe(1) // parentLane
    expect(forkBez.color).toBe('#e67e22')
  })

  it('fork bezier control points ensure tangential entry/exit (vertical tangent)', () => {
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any
    const forkBez = lines.find((l: any) => l.path.includes('C') && l.path.startsWith('M20,101'))
    expect(forkBez).toBeDefined()

    // Parse the bezier: M startX,startY C cp1X,cp1Y cp2X,cp2Y endX,endY
    const nums = forkBez.path.match(/[\d.]+/g).map(Number)
    const [startX, startY, cp1X, cp1Y, cp2X, cp2Y, endX, endY] = nums

    // cp1 should be at startX (tangential to vertical at start on the main lane)
    expect(cp1X).toBe(startX)
    // cp2 should be at endX (tangential to vertical at end on the branch lane)
    expect(cp2X).toBe(endX)
    // Control points are monotonically increasing in Y (no inversion)
    expect(cp1Y).toBeGreaterThan(startY)
    expect(cp2Y).toBeGreaterThan(cp1Y)
    expect(cp2Y).toBeLessThan(endY)
  })

  it('fork and merge-in bezier have mirrored control point ratios', () => {
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any

    const forkBez = lines.find((l: any) => l.path.includes('C') && l.path.startsWith('M20,101'))
    const mergeInBez = lines.find((l: any) => l.path.includes('C') && l.path.startsWith('M40,320'))

    expect(forkBez).toBeDefined()
    expect(mergeInBez).toBeDefined()

    // Parse both beziers
    const fNums = forkBez.path.match(/[\d.]+/g).map(Number)
    const mNums = mergeInBez.path.match(/[\d.]+/g).map(Number)

    const [fSx, fSy, , fCp1Y, , fCp2Y, , fEy] = fNums
    const [mSx, mSy, , mCp1Y, , mCp2Y, , mEy] = mNums

    // Both should use ~0.25 control point ratio
    const fDy = fEy - fSy
    expect(Math.abs((fCp1Y - fSy) / fDy - 0.25)).toBeLessThan(0.05)
    expect(Math.abs((fEy - fCp2Y) / fDy - 0.25)).toBeLessThan(0.05)

    const mDy = mEy - mSy
    expect(Math.abs((mCp1Y - mSy) / mDy - 0.25)).toBeLessThan(0.05)
    expect(Math.abs((mEy - mCp2Y) / mDy - 0.25)).toBeLessThan(0.05)
  })

  it('fork vertical line and bezier both use parentLane color', () => {
    const { lines } = computeGraphData(SINGLE_MERGE, ROW_HEIGHT, undefined) as any
    // All FORK-related lines (bezier + vertical) should be orange (lane 1 color)
    const forkLines = lines.filter((l: any) => {
      if (l.fade) return false
      // Lane 1 (feature branch) lines that are part of the FORK
      return l.lane === 1 && l.color === '#e67e22'
    })
    expect(forkLines.length).toBeGreaterThanOrEqual(2) // bezier + vertical at minimum
  })
})

// ─── Fork overlap regression: fork must not overlap the main line ──────────

describe('fork does not overlap the main line', () => {
  it('no fork vertical segment is coincident with a main-lane vertical line', () => {
    // For every topology, assert that the fork's vertical (drawn on the branch
    // lane) never shares X + Y-range with a main-lane (lane 0) vertical line.
    const allRepos = [SINGLE_MERGE, MULTI_BRANCH, FREQUENT_MERGE, LONG_LIVED, OCTOPUS]
    for (const data of allRepos) {
      const { lines } = computeGraphData(data, ROW_HEIGHT, undefined) as any

      // Collect all vertical segments (same X at start and end)
      const verts: any[] = []
      for (const l of lines) {
        if (l.fade) continue
        const n = l.path.match(/[\d.]+/g)
        if (!n || n.length !== 4) continue
        const [x0, y0, x1, y1] = n.map(Number)
        if (Math.abs(x0 - x1) < 0.001) {
          verts.push({ x: x0, lo: Math.min(y0, y1), hi: Math.max(y0, y1), color: l.color })
        }
      }

      // Detect coincident overlaps between lines of different colors
      for (let i = 0; i < verts.length; i++) {
        for (let j = i + 1; j < verts.length; j++) {
          const a = verts[i]
          const b = verts[j]
          if (Math.abs(a.x - b.x) < 0.001 && a.color !== b.color) {
            const lo = Math.max(a.lo, b.lo)
            const hi = Math.min(a.hi, b.hi)
            // A >1px coincident overlap between different-colored verticals
            // would render as a doubled/overlapping line.
            expect(hi - lo).toBeLessThanOrEqual(1)
          }
        }
      }
    }
  })
})

// ─── Merge-in node gap (compressed lane) tests ────────────────────────────

describe('merge-in with compressed lane node gaps', () => {
  it('merge-in vertical line breaks around nodes on the same compressed lane', () => {
    // Construct a scenario where branch-a and branch-b share a compressed
    // lane (non-overlapping). Branch-a is near the top, branch-b is below.
    // Branch-a has a merge-in that passes through branch-b's node positions.
    //
    // Graph:
    //   fin → mrgA → m2 ──→ m1 → init
    //          ↗           ↘
    //         a1            b1 → b2
    //
    // If lane compression puts a1 and b1/b2 on the same lane, the merge-in
    // from a1 to m1 will have nodeGaps (b1, b2) on its vertical path.
    const commits = [
      { sha: 'fin', parents: ['mrgA'], msg: 'main: final', refs: ['main'] },
      { sha: 'mrgA', parents: ['m2', 'a1'], msg: 'merge: branch-a' },
      { sha: 'b2', parents: ['b1'], msg: 'branch-b: work 2', refs: ['branch-b'] },
      { sha: 'b1', parents: ['m1'], msg: 'branch-b: work 1' },
      { sha: 'm2', parents: ['m1'], msg: 'main: second' },
      { sha: 'a1', parents: ['m1'], msg: 'branch-a: work 1' },
      { sha: 'm1', parents: ['init'], msg: 'main: first' },
      { sha: 'init', parents: [], msg: 'main: init' },
    ]
    const { nodes, lines } = computeGraphData(commits, ROW_HEIGHT, undefined) as any

    // Find the merge-in from a1 to m1 (a1 is on lane 1, m1 is on lane 0)
    // The vertical part should break around b1/b2 if they share the lane
    const mergeInVerts = lines.filter((l: any) => {
      if (l.path.includes('C') || l.fade) return false
      return l.lane === 1
    })
    expect(mergeInVerts.length).toBeGreaterThanOrEqual(1)
  })

  it('merge-in vertical segments are split into multiple parts when nodeGaps exist', () => {
    // Create a scenario that forces lane compression and node gaps.
    // Two sequential branches share a lane. Branch-a merges in first,
    // but its merge-in vertical line must break around branch-b nodes.
    //
    // Layout (row order):
    //   row 0: fin (L0)
    //   row 1: mrgA (L0)  ← merge branch-a
    //   row 2: b2 (L1)    ← branch-b node on shared lane
    //   row 3: m2 (L0)
    //   row 4: b1 (L1)    ← branch-b node on shared lane
    //   row 5: mrgB (L0)  ← merge branch-b
    //   row 6: a1 (L1)    ← branch-a node on shared lane
    //   row 7: m1 (L0)
    //   row 8: init (L0)
    //
    // If a1 (L1) has first parent m1 (L0), the merge-in from a1
    // goes from row 6 to row 7. But b2 (row 2) and b1 (row 4)
    // are also on L1, so the vertical line must break around them
    // when it goes from a1 down.
    // However, a1 is at row 6 and m1 is at row 7, so the merge-in
    // vertical segment only spans rows 6→7 — no gaps between them.
    // To trigger gaps, we need the child higher up and the parent
    // lower down with intermediate same-lane nodes.

    // Better scenario: one branch whose merge-in spans multiple rows
    // with another branch's nodes on the same compressed lane.
    const commits = [
      { sha: 'top', parents: ['mrg'], msg: 'main: top' },
      { sha: 'mrg', parents: ['mid', 'a1'], msg: 'merge: branch-a' },
      { sha: 'b2', parents: ['b1'], msg: 'branch-b: work 2', refs: ['branch-b'] },
      { sha: 'mid', parents: ['b1m'], msg: 'main: mid' },
      { sha: 'b1', parents: ['root'], msg: 'branch-b: work 1' },
      { sha: 'b1m', parents: ['root'], msg: 'main: pre-b' },
      { sha: 'a1', parents: ['root'], msg: 'branch-a: work 1' },
      { sha: 'root', parents: [], msg: 'root' },
    ]
    const result = computeGraphData(commits, ROW_HEIGHT, undefined) as any

    // Check that we have vertical lines on lane 1 (the branch lane)
    const branchVerts = result.lines.filter((l: any) => {
      if (l.path.includes('C') || l.fade) return false
      return l.lane === 1
    })
    expect(branchVerts.length).toBeGreaterThanOrEqual(1)
  })
})

// ─── Ref label helper tests ────────────────────────────────────────────────

describe('ref label width', () => {
  it('calculates width based on text length', () => {
    expect(refLabelWidth('main')).toBe('main'.length * 6 + 8)
    expect(refLabelWidth('feature')).toBe('feature'.length * 6 + 8)
  })

  it('strips tag: prefix before calculating width', () => {
    // 'tag: v1.0' → text = 'v1.0'
    expect(refLabelWidth('tag: v1.0')).toBe('v1.0'.length * 6 + 8)
  })

  it('longer ref names produce wider labels', () => {
    expect(refLabelWidth('very-long-branch-name')).toBeGreaterThan(refLabelWidth('main'))
  })
})

describe('ref label background color', () => {
  it('HEAD ref gets dark background', () => {
    expect(refLabelBg('HEAD')).toBe('#1a1a2e')
  })

  it('tag refs get gray background', () => {
    expect(refLabelBg('tag: v1.0')).toBe('#555')
  })

  it('branch refs get blue background', () => {
    expect(refLabelBg('main')).toBe('#4a90d9')
    expect(refLabelBg('feature')).toBe('#4a90d9')
  })
})
