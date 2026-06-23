// ─── Git Graph computation utilities ────────────────────────────────────────

const LANE_WIDTH = 20
const GRAPH_LEFT_PADDING = 10
const LANE_COLORS = [
  '#4a90d9', // blue
  '#e67e22', // orange
  '#2ecc71', // green
  '#9b59b6', // purple
  '#e74c3c', // red
  '#1abc9c', // teal
  '#f39c12', // yellow
  '#34495e', // dark gray
]



interface GitCommit {
  sha: string
  parents: string[]
  refs?: string[]
  isWT?: boolean
}

interface GraphNode {
  cx: number
  cy: number
  color: string
  isWT: boolean
  refs: string[]
  lane: number
  row: number
  branchNames: string[]
}

function laneCx(lane: number): number {
  return lane * LANE_WIDTH + LANE_WIDTH / 2 + GRAPH_LEFT_PADDING
}

/**
 * Compute full graph data for a list of commits.
 *
 * Two-phase algorithm:
 * 1. Lane assignment — walk first-parent chains first (main line = same lane),
 *    then assign remaining commits and merge-parent lanes. Lanes are recycled
 *    when branches merge back into the main line, so non-overlapping branches
 *    share the same lane instead of each taking a separate one.
 * 2. Connection generation — for each commit→parent edge:
 *    - Same-lane parent → vertical line from child to parent
 *    - Cross-lane "fork" (merge commit's non-first parent) → bezier curve
 *    - Cross-lane "merge-in" (branch commit whose parent is on another lane) →
 *      vertical line on branch lane + short bezier at merge point
 */
export function computeGraphData(commits: GitCommit[], rowHeight: number, previousShaToLane: Map<string, number> | null) {
  if (!commits || !commits.length) {
    return { nodes: [], lines: [], laneCount: 0, graphWidth: 40, shaToLane: new Map() }
  }

  const laneColor = (lane: number): string => LANE_COLORS[lane % LANE_COLORS.length]

  // ── Pre-build SHA-to-row index (O(1) lookup) ──
  const shaToRow = new Map()
  for (let row = 0; row < commits.length; row++) {
    if (!commits[row].isWT) {
      shaToRow.set(commits[row].sha, row)
    }
  }

  // ── Phase 1: Lane assignment ──
  //
  // When previousShaToLane is provided (lazy-load scenario), preserve lane
  // assignments for SHAs that were already visible. This prevents visual line
  // splitting when new commits are appended.
  //
  // Step A: For each commit, walk its first-parent chain and assign the same
  //         lane to all ancestors until hitting an already-assigned SHA.
  //         If a SHA was previously assigned, reuse that lane.
  // Step B: Assign lanes to any remaining unassigned commits (branch tips).
  // Step C: Assign lanes to merge parents (non-first-parent) that aren't assigned.
  // Step D: Compress lanes — merge non-overlapping lanes so that branches
  //         that don't coexist in time share the same visual track.

  const shaToLane = new Map()
  const laneOfRow = new Array(commits.length)
  let nextLane = 0
  let maxLane = 0

  // Seed from previous lane assignments so existing commits keep their lanes
  if (previousShaToLane) {
    for (const [sha, lane] of previousShaToLane) {
      if (shaToRow.has(sha)) {
        shaToLane.set(sha, lane)
        if (lane >= nextLane) nextLane = lane + 1
        if (lane > maxLane) maxLane = lane
      }
    }
  }

  // Step A: Walk first-parent chains
  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    if (c.isWT) {
      laneOfRow[row] = 0
      continue
    }

    const sha = c.sha
    let lane
    if (shaToLane.has(sha)) {
      lane = shaToLane.get(sha)
    } else {
      lane = nextLane++
      shaToLane.set(sha, lane)
    }

    laneOfRow[row] = lane
    if (lane > maxLane) maxLane = lane

    // Walk first-parent chain, assigning same lane to ancestors
    let cur = sha
    while (true) {
      const curRow = shaToRow.get(cur)
      if (curRow === undefined) break
      const parents = commits[curRow].parents || []
      if (parents.length === 0) break
      const firstParent = parents[0]
      if (shaToLane.has(firstParent)) break
      if (!shaToRow.has(firstParent)) break
      shaToLane.set(firstParent, lane)
      cur = firstParent
    }
  }

  // Step B: Assign remaining unassigned commits (branch tips not on first-parent chain)
  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    if (c.isWT) continue
    if (shaToLane.has(c.sha)) {
      laneOfRow[row] = shaToLane.get(c.sha)
      continue
    }
    const lane = nextLane++
    shaToLane.set(c.sha, lane)
    laneOfRow[row] = lane
    if (lane > maxLane) maxLane = lane
  }

  // Step C: Assign lanes for merge parents (non-first-parent) that aren't assigned
  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    if (c.isWT) continue
    const parents = c.parents || []
    for (let pi = 1; pi < parents.length; pi++) {
      const pSha = parents[pi]
      if (!shaToRow.has(pSha)) continue
      if (!shaToLane.has(pSha)) {
        const lane = nextLane++
        shaToLane.set(pSha, lane)
        if (lane > maxLane) maxLane = lane
      }
    }
  }

  // Step D: Compress lanes by merging non-overlapping ones.
  // Compute the visual row range for each lane — including not just node rows
  // but also the rows where connection lines (forks and merge-ins) start/end.
  // Lane 0 (main) is never merged. For other lanes, if two lanes have
  // non-overlapping visual ranges, they can share the same compressed lane.
  if (maxLane > 0) {
    // Compute visual row range per lane
    const laneFirstRow = new Map()
    const laneLastRow = new Map()

    for (let row = 0; row < commits.length; row++) {
      const lane = laneOfRow[row]
      if (lane === undefined) continue
      if (!laneFirstRow.has(lane) || row < laneFirstRow.get(lane)) {
        laneFirstRow.set(lane, row)
      }
      if (!laneLastRow.has(lane) || row > laneLastRow.get(lane)) {
        laneLastRow.set(lane, row)
      }

      // Extend visual range to include cross-lane connection endpoints.
      // This ensures lanes with overlapping connections don't get compressed.
      const c = commits[row]
      if (c.isWT) continue
      const rowLane = lane
      const parents = c.parents || []
      for (let pi = 0; pi < parents.length; pi++) {
        const pSha = parents[pi]
        if (!shaToRow.has(pSha) || !shaToLane.has(pSha)) continue
        const parentRow = shaToRow.get(pSha)
        const parentLane = shaToLane.get(pSha)
        if (rowLane !== parentLane) {
          // Extend child lane to include parent row
          if (!laneFirstRow.has(rowLane) || parentRow < laneFirstRow.get(rowLane)) {
            laneFirstRow.set(rowLane, parentRow)
          }
          if (!laneLastRow.has(rowLane) || parentRow > laneLastRow.get(rowLane)) {
            laneLastRow.set(rowLane, parentRow)
          }
          // Extend parent lane to include child row
          if (!laneFirstRow.has(parentLane) || row < laneFirstRow.get(parentLane)) {
            laneFirstRow.set(parentLane, row)
          }
          if (!laneLastRow.has(parentLane) || row > laneLastRow.get(parentLane)) {
            laneLastRow.set(parentLane, row)
          }
        }
      }
    }

    // Greedy interval coloring: sort non-main lanes by firstRow, assign each
    // to the lowest compressed lane that doesn't overlap.
    const nonMainLanes = []
    for (const lane of laneFirstRow.keys()) {
      if (lane !== 0) {
        nonMainLanes.push(lane)
      }
    }
    nonMainLanes.sort((a, b) => laneFirstRow.get(a) - laneFirstRow.get(b))

    // For each compressed lane > 0, track the lastRow of the last assigned lane.
    // A new lane can be assigned to compressed lane C if its firstRow > lastRowOf[C].
    const compressedLastRow = new Map()  // compressedLane -> lastRow
    let nextCompressed = 1
    const oldToCompressed = new Map()
    oldToCompressed.set(0, 0)  // lane 0 stays lane 0

    for (const oldLane of nonMainLanes) {
      const firstRow = laneFirstRow.get(oldLane)
      const lastRow = laneLastRow.get(oldLane)
      let assigned = -1
      // Try existing compressed lanes (1, 2, ...) in order
      for (const [compLane, compLastRow] of compressedLastRow) {
        if (firstRow > compLastRow) {
          assigned = compLane
          compressedLastRow.set(compLane, lastRow)
          break
        }
      }
      if (assigned === -1) {
        assigned = nextCompressed++
        compressedLastRow.set(assigned, lastRow)
      }
      oldToCompressed.set(oldLane, assigned)
    }

    // Apply compressed lane mapping
    for (let row = 0; row < commits.length; row++) {
      if (laneOfRow[row] !== undefined) {
        laneOfRow[row] = oldToCompressed.get(laneOfRow[row]) ?? laneOfRow[row]
      }
    }
    for (const [sha, oldLane] of shaToLane) {
      shaToLane.set(sha, oldToCompressed.get(oldLane) ?? oldLane)
    }
    maxLane = nextCompressed - 1
  }

  // ── Phase 2: Generate nodes and connection lines ──
  const nodes: GraphNode[] = []
  const lines = []

  // Nodes
  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    const cy = row * rowHeight + rowHeight / 2
    const lane = laneOfRow[row] as number
    const cx = laneCx(lane)
    if (c.isWT) {
      nodes.push({ cx, cy, color: '#f59e0b', isWT: true, refs: [] as string[], lane, row, branchNames: [] })
    } else {
      nodes.push({ cx, cy, color: laneColor(lane), isWT: false, refs: c.refs || [], lane, row, branchNames: [] })
    }
  }

  // ── Pre-compute merge-in counts per parent row ──
  // Used to offset merge-in bezier endpoints when multiple arrive at same parent
  const mergeInCount = new Map()  // parentRow -> count of merge-ins
  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    if (c.isWT) continue
    const childLane = laneOfRow[row]
    const parents = c.parents || []
    const pSha = parents[0]
    if (!pSha || !shaToRow.has(pSha) || !shaToLane.has(pSha)) continue
    const parentRow = shaToRow.get(pSha)
    const parentLane = shaToLane.get(pSha)
    if (childLane !== parentLane) {
      if (!mergeInCount.has(parentRow)) mergeInCount.set(parentRow, 0)
      mergeInCount.set(parentRow, mergeInCount.get(parentRow) + 1)
    }
  }
  // Second pass: assign indices
  const mergeInSeen = new Map()  // parentRow -> count seen so far
  const mergeInOffsets = new Map() // "parentRow:fromLane" -> y-offset
  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    if (c.isWT) continue
    const childLane = laneOfRow[row]
    const parents = c.parents || []
    const pSha = parents[0]
    if (!pSha || !shaToRow.has(pSha) || !shaToLane.has(pSha)) continue
    const parentRow = shaToRow.get(pSha)
    const parentLane = shaToLane.get(pSha)
    if (childLane !== parentLane) {
      if (!mergeInSeen.has(parentRow)) mergeInSeen.set(parentRow, 0)
      const idx = mergeInSeen.get(parentRow)
      mergeInSeen.set(parentRow, idx + 1)
      const total = mergeInCount.get(parentRow)
      // Spread offsets evenly: center around 0, step 4px
      const offset = (idx - (total - 1) / 2) * 4
      mergeInOffsets.set(parentRow + ':' + childLane, offset)
    }
  }

  // Connection lines: for each commit→parent edge
  // Continuation lines extend beyond the last row by one rowHeight so they
  // are visually obvious (not just a tiny 18px stub).
  const svgBottomY = commits.length * rowHeight + rowHeight

  for (let row = 0; row < commits.length; row++) {
    const c = commits[row]
    if (c.isWT) continue

    const childLane = laneOfRow[row]
    const childCy = row * rowHeight + rowHeight / 2
    const parents = c.parents || []

    for (let pi = 0; pi < parents.length; pi++) {
      const pSha = parents[pi]

      // Parent not in loaded commits — draw continuation line to SVG bottom
      if (!shaToRow.has(pSha)) {
        if (pi === 0) {
          // First parent: vertical continuation on same lane
          const x = laneCx(childLane)
          const y1 = childCy + 5
          const y2 = svgBottomY
          if (y2 > y1) {
            lines.push({
              path: `M${x},${y1} L${x},${y2}`,
              color: laneColor(childLane),
              lane: childLane,
              fade: true,
            })
          }
        }
        // Non-first parents outside visible range are not drawn;
        // they will appear when more commits are loaded.
        continue
      }

      if (!shaToLane.has(pSha)) continue

      const parentRow = shaToRow.get(pSha)
      const parentLane = shaToLane.get(pSha)
      const parentCy = parentRow * rowHeight + rowHeight / 2

      if (childLane === parentLane) {
        // Same-lane connection: vertical line from child bottom to parent top
        const x = laneCx(childLane)
        const y1 = childCy + 5
        const y2 = parentCy - 5
        lines.push({
          path: `M${x},${y1} L${x},${y2}`,
          color: laneColor(childLane),
          lane: childLane,
        })
      } else if (pi > 0) {
        // Cross-lane fork: merge commit's non-first parent (the branch tip).
        // Mirror of merge-in: short vertical line from the child node, then a
        // smooth S-curve bezier to the parent lane, then a vertical line to the
        // parent.  This is the exact mirror of merge-in's (vertical + bezier)
        // structure, so both look equally smooth.
        // Color uses the parent lane (the branch being merged in), not the child
        // lane, so the line visually belongs to the merged branch.
        const childX = laneCx(childLane)
        const parentX = laneCx(parentLane)
        const childBottom = childCy + 5
        const parentTop = parentCy - 5

        // Mirror of merge-in: vertical line goes from child down to one row
        // below, then bezier takes over (merge-in: vertical to parentRowTop, then
        // bezier; fork: vertical to childRowBottom, then bezier).
        const childRowBottom = (row + 1) * rowHeight
        const bezierFromY = childRowBottom

        // Draw vertical line from child to one row below
        if (bezierFromY > childBottom) {
          lines.push({
            path: `M${childX},${childBottom} L${childX},${bezierFromY}`,
            color: laneColor(parentLane),
            lane: parentLane,
          })
        }

        // Short bezier from child lane to parent lane — control points
        // mirror merge-in: cp1 near start (stays on childX), cp2 near end
        // (stays on parentX), keeping the curve tangential to vertical at
        // both ends.
        const dy = parentTop - bezierFromY
        const cp1Y = bezierFromY + dy * 0.25
        const cp2Y = parentTop - dy * 0.25
        lines.push({
          path: `M${childX},${bezierFromY} C${childX},${cp1Y} ${parentX},${cp2Y} ${parentX},${parentTop}`,
          color: laneColor(parentLane),
          lane: parentLane,
        })
      } else {
        // Cross-lane merge-in: child's first parent is on a different lane.
        // Render as: vertical line(s) on branch lane from child down toward
        // the parent row, then (if non-adjacent) cascade through intermediate
        // lanes, then a short bezier into the parent node.
        // When the branch lane is shared (compressed), the vertical line must
        // break around any nodes on the same lane to avoid visual overlap.
        const branchX = laneCx(childLane)
        const parentX = laneCx(parentLane)
        const childBottom = childCy + 5
        const parentRowTop = parentRow * rowHeight

        // Find rows on the same lane between child and parent that have nodes
        // (these belong to other branches sharing this compressed lane).
        const nodeGaps = []
        for (let r = row + 1; r < parentRow; r++) {
          if (laneOfRow[r] === childLane) {
            const gapCy = r * rowHeight + rowHeight / 2
            nodeGaps.push(gapCy)
          }
        }

        // Y endpoint offset for multiple merge-ins at the same parent
        const yKey = parentRow + ':' + childLane
        const yOffset = mergeInOffsets.get(yKey) || 0
        const toY = parentCy + yOffset

        // Vertical line on branch lane ends at parent row top where bezier begins
        const bezierStartY = parentRowTop

        // Draw vertical line(s) on branch lane, breaking around same-lane nodes
        let vertStart = childBottom
        for (const gapCy of nodeGaps) {
          const gapTop = gapCy - rowHeight * 0.4
          const gapBottom = gapCy + rowHeight * 0.4
          if (gapTop > vertStart) {
            lines.push({
              path: `M${branchX},${vertStart} L${branchX},${gapTop}`,
              color: laneColor(childLane),
              lane: childLane,
            })
          }
          vertStart = gapBottom
        }
        if (vertStart < bezierStartY) {
          lines.push({
            path: `M${branchX},${vertStart} L${branchX},${bezierStartY}`,
            color: laneColor(childLane),
            lane: childLane,
          })
        }

        // Single smooth bezier from branch lane into parent node (any lane gap)
        const fromY = bezierStartY
        const dy = toY - fromY
        const cp1Y = fromY + dy * 0.25
        const cp2Y = toY - dy * 0.25
        lines.push({
          path: `M${branchX},${fromY} C${branchX},${cp1Y} ${parentX},${cp2Y} ${parentX},${toY}`,
          color: laneColor(childLane),
          lane: childLane,
        })
      }
    }
  }

  // ── Build SHA-to-branch-names mapping ──
  // For each branch ref, walk the first-parent chain backwards and mark all
  // ancestors as belonging to that branch. This lets us show which branch
  // a commit belongs to when clicking on it.
  const shaToBranchNames = new Map() // sha -> Set of branch names
  for (const node of nodes) {
    if (node.isWT) continue
    const c = commits[node.row]
    const branchRefs = (c.refs || []).filter((r: string) => !r.startsWith('HEAD') && !r.startsWith('tag: '))
    if (branchRefs.length === 0) continue

    for (const ref of branchRefs) {
      // Walk first-parent chain from this ref tip backwards
      const visited = new Set()
      let curSha = c.sha
      while (curSha && !visited.has(curSha)) {
        visited.add(curSha)
        if (!shaToBranchNames.has(curSha)) {
          shaToBranchNames.set(curSha, new Set())
        }
        shaToBranchNames.get(curSha).add(ref)

        const curRow = shaToRow.get(curSha)
        if (curRow === undefined) break
        const parents = commits[curRow].parents || []
        if (parents.length === 0) break
        curSha = parents[0]
        if (!shaToRow.has(curSha)) break
      }
    }
  }

  // Attach branch names to nodes
  for (const node of nodes) {
    if (node.isWT) {
      node.branchNames = []
      continue
    }
    const names = shaToBranchNames.get(commits[node.row].sha)
    node.branchNames = names ? [...names] : []
  }

  // ── Build lane-to-branch-name mapping ──
  // Scan nodes for branch refs (excluding HEAD and tags) to determine
  // which branch each lane visually represents.
  const laneBranchNames = new Map()  // lane -> Set of branch names
  for (const node of nodes) {
    if (node.refs) {
      for (const ref of node.refs) {
        if (ref === 'HEAD' || ref.startsWith('tag: ')) continue
        if (!laneBranchNames.has(node.lane)) {
          laneBranchNames.set(node.lane, new Set())
        }
        laneBranchNames.get(node.lane).add(ref)
      }
    }
  }
  // Convert to simple map: lane -> primary branch name (first from the most recent commit)
  const laneBranchName = new Map()
  for (const [lane, names] of laneBranchNames) {
    laneBranchName.set(lane, [...names][0])
  }

  const graphWidth = Math.max(40, (maxLane + 1) * LANE_WIDTH + GRAPH_LEFT_PADDING * 2)
  return { nodes, lines, laneCount: maxLane + 1, graphWidth, shaToLane, laneBranchName }
}

// ─── Ref label helpers ─────────────────────────────────────────────────────

export function refLabelWidth(ref: string): number {
  const text = ref.startsWith('tag: ') ? ref.slice(5) : ref
  return text.length * 6 + 8
}

export function refLabelText(ref: string): string {
  if (ref.startsWith('tag: ')) return ref.slice(5)
  return ref
}

export function refLabelBg(ref: string): string {
  if (ref === 'HEAD') return '#1a1a2e'
  if (ref.startsWith('tag: ')) return '#555'
  return '#4a90d9'
}
