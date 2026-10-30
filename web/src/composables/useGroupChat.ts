import { appLog } from '@/utils/appLog'
import type { UsageSnapshot } from '@/utils/usageDisplay'

/**
 * Frontend API client for AI group chat. Mirrors the endpoints registered in
 * internal/handler/group.go.
 */

/** MemberUsage is one member's own context window, same shape as the single
 *  chat's `usageState` (both are `ai.UsageState` on the wire). */
export type MemberUsage = UsageSnapshot

export interface GroupMemberInfo {
  id: string
  agentId: string
  name: string
  backend: string
  /** 成员自身的头像 SVG（agent_avatar）；空 = 回退到智能体头像。 */
  avatar: string
  /** 底层真实智能体名；与 name 相同时也带回，供 UI 判定是否披露。 */
  agentName: string
  /** 禁用：仍是在群成员，但调度时不安排它发言。启用后能看到禁用期间的全部内容。 */
  disabled: boolean
  isHost: boolean
  /**
   * 该成员**自己**的上下文用量（成员行 context_state.usage，或活连接的缓存）。
   * null/undefined = 从未上报（CLI 传输的智能体不上报），与「已用 0%」不同。
   */
  usage?: MemberUsage | null
  /** 成员行的模型覆盖；空串 = 用智能体默认模型。 */
  model?: string
}

export interface CreateGroupResult {
  groupId: string
  hostMemberId: string
  /** "host" (a host routes) or "free" (no host, @-relay). */
  mode: 'host' | 'free'
}

async function postJSON(path: string, body: unknown): Promise<Record<string, unknown>> {
  const resp = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!resp.ok) {
    appLog.w('Group', `POST ${path} failed: ${resp.status}`)
    throw new Error(`request failed: ${resp.status}`)
  }
  return resp.json()
}

/** createGroup creates a group together with ALL its members in one call
 *  (design §7.1, decision #25). When hostAgentId is empty the group is created
 *  in FREE mode (no host; participants relay via @-mentions) — see design §13.
 *  When present, hostAgentId must be in memberAgentIds and the group is HOST
 *  mode. The backend creates the group and every member atomically. */
export async function createGroup(hostAgentId: string, memberAgentIds: string[]): Promise<CreateGroupResult> {
  const data = await postJSON('/api/group/create', { hostAgentId, memberAgentIds })
  return {
    groupId: String(data.groupId ?? ''),
    hostMemberId: String(data.hostMemberId ?? ''),
    mode: data.mode === 'free' ? 'free' : 'host',
  }
}

/** listGroupMembers fetches the group roster (including disabled members) plus the
 *  group's mode ("host" | "free"), its parallelDefault (the free-mode
 *  action-bar "并发执行" switch's server value) and its cruise switch (auto-cruise,
 *  design §15). The member-speech cap is a global setting
 *  (chat.group_max_speeches), not a per-group one, so it is not returned here. */
export async function listGroupMembers(groupId: string): Promise<{ members: GroupMemberInfo[]; mode: 'host' | 'free'; parallelDefault: boolean; cruise: boolean }> {
  const resp = await fetch(`/api/group/members?groupId=${encodeURIComponent(groupId)}`)
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
  const data = await resp.json()
  return {
    // `usage` is passed through verbatim (null included): the roster must be
    // able to tell "never reported" from "0% used", so no `?? {}` fallback.
    members: data.members ?? [],
    mode: data.mode === 'free' ? 'free' : 'host',
    parallelDefault: data.parallelDefault === true,
    cruise: data.cruise === true,
  }
}

/** compactGroupMember asks ONE member's agent to compact its own context.
 *  Runs outside the group timeline (no user bubble, no reply is written there).
 *  Throws with the server's message on refusal (busy group, CLI member, …). */
export async function compactGroupMember(groupId: string, memberId: string): Promise<MemberUsage | null> {
  const resp = await fetch('/api/group/member-compact', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, memberId }),
  })
  const data = await resp.json().catch(() => ({} as Record<string, unknown>))
  if (!resp.ok) {
    appLog.w('Group', `compact member ${memberId} failed: ${resp.status}`)
    throw new Error(String(data.error ?? `request failed: ${resp.status}`))
  }
  return (data.usage as MemberUsage | null) ?? null
}

/** addGroupMembers adds one or more agents to the group. */
export async function addGroupMembers(groupId: string, agentIds: string[]): Promise<string[]> {
  const data = await postJSON('/api/group/members', { groupId, agentIds })
  return (data.memberIds as string[]) ?? []
}

/** setMemberAvatars 批量写回成员头像（DiceBear 生成，落库以对齐分享/导出）。 */
export async function setMemberAvatars(groupId: string, avatars: { memberId: string; avatar: string }[]): Promise<void> {
  await postJSON('/api/group/member-avatars', { groupId, avatars })
}

/**
 * Generate + persist avatars for the group members that need their OWN one.
 *
 * Only a REPEATED agent gets generated avatars: when the same agent is added to
 * the group more than once, every instance past the first is given a DiceBear
 * avatar (seeded by its unique name) so the copies are visually distinct. A
 * LONE agent keeps the agent's own configured avatar — there is nothing to tell
 * apart, so no generated avatar is written (the icon falls back to
 * getAgentAvatar).
 *
 * "First instance" is decided over SPEAKING (enabled) members in roster order
 * (which is creation order): a disabled member does not count, since the group
 * shows a single speaking copy. A member that already has an avatar is never
 * re-generated (idempotent, so a partial failure can be retried by a later add).
 *
 * Called right after a create/add. onlyMemberIds, when given, scopes the
 * backfill to the freshly added members; the first-instance decision still sees
 * the whole roster, so a newly added SECOND instance is generated while a
 * pre-existing first is left on the agent avatar.
 *
 * The DiceBear runtime is imported lazily so the generator chunk never enters
 * the main bundle and is only fetched on the (rare) create/add path.
 */
export async function generateMemberAvatars(groupId: string, onlyMemberIds?: string[]): Promise<void> {
  const { members } = await listGroupMembers(groupId)
  const seenAgents = new Set<string>()
  const targets = members.filter(m => {
    if (m.disabled) return false
    // Roster order is creation order, so the first SPEAKING member of an agent
    // is its first instance; it keeps the agent avatar. Later instances are the
    // repeated copies that need their own generated avatar.
    const isFirstInstance = !seenAgents.has(m.agentId)
    seenAgents.add(m.agentId)
    if (isFirstInstance) return false
    if (m.avatar) return false
    return !onlyMemberIds || onlyMemberIds.includes(m.id)
  })
  if (!targets.length) return
  const { generateAvatarSvg } = await import('@/utils/lazyAvatar')
  const avatars = await Promise.all(targets.map(async m => ({ memberId: m.id, avatar: await generateAvatarSvg(m.name) })))
  await setMemberAvatars(groupId, avatars)
}

/** setGroupMemberDisabled disables (禁用) or re-enables (启用) a member.
 *
 *  Muting is NOT removal: the member row, its context window and its whole
 *  history stay intact, and unmuting is a plain flag flip — so a member disabled
 *  for a while comes back with everything that happened in the meantime. */
export async function setGroupMemberDisabled(groupId: string, memberId: string, disabled: boolean): Promise<void> {
  const resp = await fetch('/api/group/members', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, memberId, disabled }),
  })
  // Consume the body: an unread response stream keeps the request "in flight"
  // for the browser, which stalls `networkidle` waits. Same reason as
  // utils/sessionRead.ts — this one is user-triggered rather than on page load,
  // so it could not fail a reload, but the leak is identical.
  await resp.body?.cancel().catch(() => {})
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

/** setGroupHost makes another member the group's host (更换主持人).
 *
 *  The host is a pointer on the group row, not a property of the member: every
 *  member keeps its own row, ACP connection and context, so nothing is
 *  reconnected and no history is touched. Takes effect from the NEXT turn; a
 *  turn already running finishes under the old host. */
export async function setGroupHost(groupId: string, memberId: string): Promise<void> {
  const resp = await fetch('/api/group/host', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, memberId }),
  })
  // Consume the body for the same reason as setGroupMemberDisabled: an unread
  // response stream keeps the request "in flight" and stalls networkidle waits.
  await resp.body?.cancel().catch(() => {})
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

/** setGroupParallelDefault updates the free-mode "并发执行" switch: when on, the
 *  user's @-mentions run concurrently. The member-speech cap is global
 *  (chat.group_max_speeches). */
export async function setGroupParallelDefault(groupId: string, enabled: boolean): Promise<void> {
  const resp = await fetch('/api/group/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, parallelDefault: enabled }),
  })
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

/** setGroupCruise updates the auto-cruise switch (design §15): when on, the turn
 *  keeps the participants working until they declare the goal met instead of
 *  handing the floor back to the human.
 *
 *  This is the pure API call. The cruise → auto-approve hard default (enabling
 *  cruise forces auto-approve on) is applied by the CALLERS, which already hold
 *  the session-identity ref — see `applyCruiseAutoApproveDefault`. It is NOT
 *  applied here: importing useSessionIdentity would couple this leaf API module
 *  to the identity singleton and break the many narrow `useAgents` mocks. */
export async function setGroupCruise(groupId: string, enabled: boolean): Promise<void> {
  const resp = await fetch('/api/group/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, cruise: enabled }),
  })
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}
