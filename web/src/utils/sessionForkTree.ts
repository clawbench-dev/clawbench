/**
 * Derived-session grouping for the session list (issue #477).
 *
 * A fork records its origin in `chat_sessions.source_session_id`, but the list
 * rendered that field as nothing: forks of one topic were scattered across the
 * list by creation time and their only lineage clue was the accumulating `🔀`
 * prefix in the title. This module turns the flat server order into a one-level
 * hierarchy — a root row plus a collapsible group of everything derived from
 * it — without touching the backend or adding a column.
 *
 * ── The rules, and why each one exists ──
 *
 * 1. ANCHOR = highest ancestor that is loaded AND shares the child's `pinned`.
 *
 *    Two constraints force this. Walking only the loaded set handles hard
 *    deletes for free: `HardDeleteSession` does not cascade, so a fork can
 *    outlive its parent, and a missing hop simply ends the walk. Confining the
 *    walk to one `pinned` value handles the other edge — `pinned DESC` leads
 *    the sort, so a pinned child would be lifted out of its unpinned parent's
 *    group and the group would silently lose a member (the count would lie).
 *    Staying inside a pinned value means a group can never be split by the
 *    sort.
 *
 * 2. A node whose immediate parent is missing becomes its own anchor.
 *
 *    This is the issue's own motivating case: a four-generation chain whose
 *    ROOT was deleted. Refusing to group it (treating the whole chain as
 *    orphaned) would leave the feature doing nothing on exactly the data that
 *    prompted it. The group header reads "forked from this session", so a
 *    mid-chain session heading the group is still accurate — it IS what the
 *    children were forked from. Nothing disappears either way: every session
 *    lands on a row, and the anchor row is a real row.
 *
 * 3. `acp:` sources are not session ids.
 *
 *    `ServeACPLoadSession` overloads the column with the marker string
 *    `"acp:{acpSessionId}"` to remember which ACP session a row was loaded
 *    from. It is not a session id, so it can never resolve to a parent — the
 *    lookup fails and rule 2 makes the row a normal, ungrouped row. That is the
 *    intended outcome; skipping the prefix explicitly just makes it deliberate
 *    rather than accidental.
 *
 * 4. Cycles cannot hang.
 *
 *    The column has no integrity constraints, so `a → b → a` is representable.
 *    The walk carries a visited set; a node that revisits itself is treated as
 *    having no anchor, which keeps it visible.
 *
 * ── Group placement ──
 *
 * The anchor row stays exactly where the server put it. Every descendant is
 * pulled up to sit directly beneath it, shallowest generation first. A
 * descendant that was not contiguous with its anchor therefore MOVES. That is
 * the point of the feature: the alternative — leaving the group's members
 * scattered and only drawing connector lines — does not solve the "list gets
 * buried under one topic" complaint the issue leads with.
 *
 * The anchor is the highest loaded ancestor, so an intermediate hop that is
 * absent can only mean the row no longer exists (rule 2). A group is therefore
 * always contiguous in the source array: the walk that reaches an anchor
 * passes through every loaded session between them, which is exactly the set
 * that gets pulled up.
 */

/** The subset of a session this module reads. */
export interface ForkTreeSession {
  id: string
  sourceSessionId?: string
  pinned?: boolean
}

/** One row in the rendered list. */
export interface ForkRow<T extends ForkTreeSession = ForkTreeSession> {
  session: T
  /**
   * 0 for a top-level row; the generation of the fork for a group member
   * (1 = forked from the anchor, 2 = forked from that fork, ...).
   */
  depth: number
  /** Number of sessions hanging off this row. Only set on top-level rows. */
  childCount: number
  /** True when this top-level row heads a non-empty group. */
  isAnchor: boolean
}

export interface ForkGrouping<T extends ForkTreeSession = ForkTreeSession> {
  /**
   * Top-level rows in server order. This is the array the draggable list binds
   * to: dragging a row moves its whole group, because the members are rendered
   * from `membersByAnchor` and always follow their anchor.
   */
  topSessions: T[]
  /** anchorId → its group members, shallowest generation first. */
  membersByAnchor: Map<string, ForkRow<T>[]>
  /** Every session by id, members included. */
  byId: Map<string, T>
}

/** The ACP marker prefix; see rule 3 in the module header. */
const ACP_SOURCE_PREFIX = 'acp:'

/**
 * Resolve the anchor of `session`: the highest ancestor reachable through the
 * loaded set without leaving `session`'s `pinned` value. Returns the session's
 * own id when it is its own anchor (no usable parent).
 */
export function resolveForkAnchor<T extends ForkTreeSession>(
  session: T,
  byId: Map<string, T>,
): string {
  const seen = new Set<string>([session.id])
  let current = session
  for (;;) {
    const sourceId = current.sourceSessionId
    if (!sourceId || sourceId.startsWith(ACP_SOURCE_PREFIX)) break
    const parent = byId.get(sourceId)
    // Rules 1 + 2: a missing parent (hard-deleted, or hidden by the tag
    // filter) ends the walk here, making `current` the anchor.
    if (!parent) break
    // Rule 1: never climb across the pinned boundary — the sort would split
    // the group.
    if (!!parent.pinned !== !!session.pinned) break
    // Rule 4: a cycle. No node in the loop can be "above" another, so the node
    // being resolved anchors itself. Every member of the loop resolves the same
    // way, which keeps the result consistent — and, the point, keeps them all
    // visible. Leaving `current` where the walk stopped would instead make each
    // node a member of the next one, so none would be a top-level row and the
    // whole loop would disappear from the list.
    if (seen.has(parent.id)) {
      current = session
      break
    }
    seen.add(parent.id)
    current = parent
  }
  return current.id
}

/**
 * Build the grouped view of a session list.
 *
 * `sessions` is the server's order (pinned DESC, sort_order ASC, created_at
 * DESC) and is never mutated. Callers pass the rows the user can actually see,
 * so a session hidden by the tag filter can neither anchor nor join a group.
 */
export function buildSessionForkTree<T extends ForkTreeSession>(
  sessions: readonly T[],
): ForkGrouping<T> {
  const byId = new Map<string, T>()
  for (const s of sessions) byId.set(s.id, s)

  const anchorOf = new Map<string, string>()
  const membersOf = new Map<string, T[]>()
  for (const session of sessions) {
    const anchorId = resolveForkAnchor(session, byId)
    anchorOf.set(session.id, anchorId)
    if (anchorId === session.id) continue
    const list = membersOf.get(anchorId)
    if (list) list.push(session)
    else membersOf.set(anchorId, [session])
  }

  // Generation is relative to the anchor: 1 for a direct fork, 2 for a fork of
  // that fork. The absolute chain depth is unknowable once a root is gone, and
  // the anchor is what the group header names anyway.
  //
  // Resolved by walking up per member rather than by iterating the members in
  // order: server order is the user's drag order, so a fresh fork routinely
  // sits ABOVE the session it was forked from (a new session gets sort_order 0)
  // and a single forward pass would read its parent's generation before
  // computing it.
  const generationOf = new Map<string, number>()
  const generationOfMember = (member: T, anchorId: string): number => {
    const known = generationOf.get(member.id)
    if (known !== undefined) return known
    // Guard: the parent chain inside a group is acyclic by construction (each
    // hop is a distinct id reached by an upward walk), but the column has no
    // integrity constraints, so stop rather than recurse forever.
    generationOf.set(member.id, 1)
    const parentId = member.sourceSessionId
    const parent = parentId ? byId.get(parentId) : undefined
    if (!parent || parent.id === anchorId) return 1
    const gen = generationOfMember(parent, anchorId) + 1
    generationOf.set(member.id, gen)
    return gen
  }

  const topSessions: T[] = []
  const membersByAnchor = new Map<string, ForkRow<T>[]>()
  for (const session of sessions) {
    if (anchorOf.get(session.id) !== session.id) continue
    topSessions.push(session)
    const members = membersOf.get(session.id)
    if (!members || members.length === 0) continue
    membersByAnchor.set(
      session.id,
      members
        // Shallowest first so the indent reads 1, 2, 3 down the group; ties
        // keep the server's relative order.
        .map((member, index) => ({ member, depth: generationOfMember(member, session.id), index }))
        .sort((a, b) => (a.depth - b.depth) || (a.index - b.index))
        .map(({ member, depth }) => ({ session: member, depth, childCount: 0, isAnchor: false })),
    )
  }

  return { topSessions, membersByAnchor, byId }
}

/**
 * The rows to render, given the top-level order and the collapsed anchors.
 *
 * A collapsed anchor renders its own row only: its members are omitted from
 * the DOM entirely (not merely hidden), so the draggable list's index
 * arithmetic never has to reason about invisible rows. The anchor's
 * `childCount` still reports the full size so the group header can show it.
 */
export function visibleForkRows<T extends ForkTreeSession>(
  topSessions: readonly T[],
  membersByAnchor: Map<string, ForkRow<T>[]>,
  isCollapsed: (anchorId: string) => boolean,
): ForkRow<T>[] {
  const out: ForkRow<T>[] = []
  for (const session of topSessions) {
    const members = membersByAnchor.get(session.id)
    const isAnchor = !!members && members.length > 0
    out.push({ session, depth: 0, childCount: isAnchor ? members.length : 0, isAnchor })
    if (!isAnchor || isCollapsed(session.id)) continue
    out.push(...members)
  }
  return out
}
